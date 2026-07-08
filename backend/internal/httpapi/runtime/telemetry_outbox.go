package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	defaultRuntimeTelemetryOutboxWorkerCount     = 1
	defaultRuntimeTelemetryOutboxPollInterval    = 250 * time.Millisecond
	defaultRuntimeTelemetryOutboxShutdownTimeout = 3 * time.Second
	defaultRuntimeTelemetryOutboxWakeupBuffer    = 1
)

type TelemetryOutboxOptions struct {
	WorkerCount     int
	PollInterval    time.Duration
	ShutdownTimeout time.Duration
	WakeupBuffer    int
	Hooks           *TelemetryOutboxHooks
	Scheduler       *background.Scheduler
}

type TelemetryOutboxCloseResult struct {
	Drained     bool
	TimedOut    bool
	PendingRows int
	Inflight    int
	Elapsed     time.Duration
}

type TelemetryOutboxHooks struct {
	EnqueueError      func() error
	BeforeMaterialize func(context.Context) error
	AfterClose        func(TelemetryOutboxCloseResult)
}

type runtimeTelemetryOutbox struct {
	telemetryPool    *pgxpool.Pool
	now              func() time.Time
	dashboardUpdates DashboardPublisher
	analyticsUpdates AnalyticsUpdatePublisher
	logPartitions    *runtimeLogPartitionCache
	pollInterval     time.Duration
	shutdownTimeout  time.Duration
	hooks            TelemetryOutboxHooks
	wake             chan struct{}
	scheduler        *background.Scheduler
	ownsScheduler    bool
	closeOnce        sync.Once
	mu               sync.Mutex
	closed           bool
	inflight         int
	closeResult      TelemetryOutboxCloseResult
}

type runtimeTelemetryOutboxRow struct {
	ID      int64
	Payload []byte
}

type runtimeTelemetryMaterializationResult struct {
	Processed    bool
	RequestLogID int
	ProfileID    int
}

type runtimeTelemetryDrainState struct {
	PendingRows int
	Inflight    int
}

func (state runtimeTelemetryDrainState) drained() bool {
	return state.PendingRows == 0 && state.Inflight == 0
}

func newRuntimeTelemetryOutbox(telemetryPool *pgxpool.Pool, now func() time.Time, dashboardUpdates DashboardPublisher, analyticsUpdates AnalyticsUpdatePublisher, logPartitions *runtimeLogPartitionCache, options TelemetryOutboxOptions) *runtimeTelemetryOutbox {
	normalized := normalizeTelemetryOutboxOptions(options)
	outbox := &runtimeTelemetryOutbox{
		telemetryPool:    telemetryPool,
		now:              now,
		dashboardUpdates: dashboardUpdates,
		analyticsUpdates: analyticsUpdates,
		logPartitions:    logPartitions,
		pollInterval:     normalized.PollInterval,
		shutdownTimeout:  normalized.ShutdownTimeout,
		hooks:            normalized.hooks(),
		wake:             make(chan struct{}, normalized.WakeupBuffer),
		scheduler:        normalized.Scheduler,
	}
	if outbox.scheduler == nil {
		outbox.scheduler = background.NewScheduler(background.Config{})
		outbox.ownsScheduler = true
		_ = outbox.RegisterBackgroundWorker(outbox.scheduler)
		_ = outbox.scheduler.Start(context.Background())
	}
	outbox.signal()
	return outbox
}

func (o *runtimeTelemetryOutbox) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if o == nil || scheduler == nil {
		return nil
	}
	o.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             background.WorkerName("runtime_telemetry_outbox"),
		Priority:         background.PriorityLowBackground,
		MaxPriority:      background.PriorityLowBackground,
		QueueLimit:       128,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 3, Delay: o.pollInterval},
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: o.pollInterval},
		Timeout:          o.shutdownTimeout,
	}, o.handleScheduledTelemetry)
}

func (o *runtimeTelemetryOutbox) Enqueue(ctx context.Context, envelope runtimeTelemetryEnvelope) error {
	_, err := o.enqueue(ctx, envelope)
	return err
}

func (o *runtimeTelemetryOutbox) EnqueueStreamingAccepted(ctx context.Context, envelope runtimeTelemetryEnvelope) (int64, error) {
	envelope.HandoffPhase = runtimeTelemetryHandoffPhaseStreamAccepted
	return o.enqueue(ctx, envelope)
}

func (o *runtimeTelemetryOutbox) FinalizeStreamingAccepted(ctx context.Context, rowID int64, envelope runtimeTelemetryEnvelope) error {
	if rowID <= 0 {
		return fmt.Errorf("runtime streaming telemetry accepted row id required")
	}
	if envelope.TraceContext.empty() {
		envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if o == nil || o.telemetryPool == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	if err := o.enqueueError(); err != nil {
		return err
	}
	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal runtime telemetry envelope: %w", err)
	}
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	if closed {
		return fmt.Errorf("runtime telemetry outbox closed")
	}
	commandTag, err := o.telemetryPool.Exec(
		ctx,
		`UPDATE runtime_telemetry_outbox SET payload = $1, created_at = $2 WHERE id = $3 AND payload->>'handoff_phase' = $4`,
		rawEnvelope,
		envelope.UsageEvent.CreatedAt,
		rowID,
		runtimeTelemetryHandoffPhaseStreamAccepted,
	)
	if err != nil {
		return fmt.Errorf("finalize runtime streaming telemetry envelope: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("runtime streaming telemetry accepted row %d unavailable", rowID)
	}
	o.signal()
	return nil
}

func (o *runtimeTelemetryOutbox) enqueue(ctx context.Context, envelope runtimeTelemetryEnvelope) (int64, error) {
	if envelope.TraceContext.empty() {
		envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if o == nil || o.telemetryPool == nil {
		return 0, fmt.Errorf("runtime telemetry outbox unavailable")
	}
	if err := o.enqueueError(); err != nil {
		return 0, err
	}
	rawEnvelope, err := json.Marshal(envelope)
	if err != nil {
		return 0, fmt.Errorf("marshal runtime telemetry envelope: %w", err)
	}
	o.mu.Lock()
	closed := o.closed
	o.mu.Unlock()
	if closed {
		return 0, fmt.Errorf("runtime telemetry outbox closed")
	}
	var rowID int64
	if err := o.telemetryPool.QueryRow(
		ctx,
		`INSERT INTO runtime_telemetry_outbox (profile_id, ingress_request_id, payload, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		envelope.UsageEvent.ProfileID,
		envelope.UsageEvent.IngressRequestID,
		rawEnvelope,
		envelope.UsageEvent.CreatedAt,
	).Scan(&rowID); err != nil {
		return 0, fmt.Errorf("enqueue runtime telemetry envelope: %w", err)
	}
	o.signal()
	return rowID, nil
}

func (o *runtimeTelemetryOutbox) Close() TelemetryOutboxCloseResult {
	if o == nil {
		return TelemetryOutboxCloseResult{Drained: true}
	}
	o.closeOnce.Do(func() {
		startedAt := time.Now()
		o.mu.Lock()
		o.closed = true
		o.mu.Unlock()
		deadline := startedAt.Add(o.shutdownTimeout)
		o.signal()
		result := TelemetryOutboxCloseResult{}
		for time.Now().Before(deadline) {
			state, err := o.drainState()
			if err == nil && state.drained() {
				result.Drained = true
				result.PendingRows = state.PendingRows
				result.Inflight = state.Inflight
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if o.scheduler != nil && o.ownsScheduler {
			ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
			_ = o.scheduler.Stop(ctx, deadline)
			cancel()
		}
		if state, err := o.drainState(); err == nil {
			result.Drained = state.drained()
			result.PendingRows = state.PendingRows
			result.Inflight = state.Inflight
		}
		result.TimedOut = !result.Drained
		result.Elapsed = time.Since(startedAt)
		o.mu.Lock()
		o.closeResult = result
		o.mu.Unlock()
		if o.hooks.AfterClose != nil {
			o.hooks.AfterClose(result)
		}
	})
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closeResult
}

func (o *runtimeTelemetryOutbox) handleScheduledTelemetry(ctx context.Context, _ background.Job) background.JobResult {
	for {
		processed, err := o.processNext(ctx)
		if err != nil {
			return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
		}
		if !processed {
			return background.JobResult{Status: background.JobSucceeded}
		}
	}
}

func (o *runtimeTelemetryOutbox) processNext(ctx context.Context) (bool, error) {
	o.beginInflight()
	result, err := pgxutil.InTxValue(ctx, o.telemetryPool, "runtime_telemetry", func(tx pgx.Tx) (runtimeTelemetryMaterializationResult, error) {
		row, found, err := loadNextRuntimeTelemetryOutboxRow(ctx, tx)
		if err != nil {
			return runtimeTelemetryMaterializationResult{}, err
		}
		if !found {
			return runtimeTelemetryMaterializationResult{}, nil
		}
		var envelope runtimeTelemetryEnvelope
		if err := json.Unmarshal(row.Payload, &envelope); err != nil {
			return runtimeTelemetryMaterializationResult{}, fmt.Errorf("decode runtime telemetry outbox row %d: %w", row.ID, err)
		}
		materializeCtx := envelope.TraceContext.context(ctx)
		if o.hooks.BeforeMaterialize != nil {
			if err := o.hooks.BeforeMaterialize(materializeCtx); err != nil {
				return runtimeTelemetryMaterializationResult{}, err
			}
		}
		requestLogID, err := materializeRuntimeTelemetryEnvelopeTx(materializeCtx, tx, o.logPartitions, envelope)
		if err != nil {
			return runtimeTelemetryMaterializationResult{}, err
		}
		if _, err := tx.Exec(materializeCtx, `DELETE FROM runtime_telemetry_outbox WHERE id = $1`, row.ID); err != nil {
			return runtimeTelemetryMaterializationResult{}, fmt.Errorf("delete runtime telemetry outbox row %d: %w", row.ID, err)
		}
		return runtimeTelemetryMaterializationResult{Processed: true, RequestLogID: requestLogID, ProfileID: envelope.UsageEvent.ProfileID}, nil
	})
	o.finishInflight()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return false, nil
		}
		return false, err
	}
	if !result.Processed {
		return false, nil
	}
	if o.dashboardUpdates != nil && result.RequestLogID > 0 && result.ProfileID > 0 {
		_, _ = o.dashboardUpdates.PublishDashboardActivity(ctx, result.RequestLogID, result.ProfileID)
	}
	if o.analyticsUpdates != nil && result.ProfileID > 0 {
		_, _ = o.analyticsUpdates.PublishAnalyticsUpdates(ctx, result.ProfileID)
	}
	return true, nil
}

func (o *runtimeTelemetryOutbox) drainState() (runtimeTelemetryDrainState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	var pendingRows int
	if err := o.telemetryPool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_telemetry_outbox`).Scan(&pendingRows); err != nil {
		return runtimeTelemetryDrainState{}, err
	}
	o.mu.Lock()
	inflight := o.inflight
	o.mu.Unlock()
	return runtimeTelemetryDrainState{PendingRows: pendingRows, Inflight: inflight}, nil
}

func (o *runtimeTelemetryOutbox) enqueueError() error {
	if o.hooks.EnqueueError == nil {
		return nil
	}
	return o.hooks.EnqueueError()
}

func (o *runtimeTelemetryOutbox) signal() {
	if o.scheduler != nil {
		_ = o.scheduler.Submit(context.Background(), background.JobRequest{Worker: background.WorkerName("runtime_telemetry_outbox"), CoalesceKey: "runtime_telemetry_outbox"})
	}
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *runtimeTelemetryOutbox) beginInflight() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.inflight++
}

func (o *runtimeTelemetryOutbox) finishInflight() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.inflight > 0 {
		o.inflight--
	}
}

func loadNextRuntimeTelemetryOutboxRow(ctx context.Context, tx pgx.Tx) (runtimeTelemetryOutboxRow, bool, error) {
	var row runtimeTelemetryOutboxRow
	err := tx.QueryRow(
		ctx,
		`SELECT id, payload FROM runtime_telemetry_outbox WHERE COALESCE(payload->>'handoff_phase', '') <> $1 ORDER BY id ASC LIMIT 1 FOR UPDATE SKIP LOCKED`,
		runtimeTelemetryHandoffPhaseStreamAccepted,
	).Scan(&row.ID, &row.Payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeTelemetryOutboxRow{}, false, nil
	}
	if err != nil {
		return runtimeTelemetryOutboxRow{}, false, fmt.Errorf("load runtime telemetry outbox row: %w", err)
	}
	return row, true, nil
}

func normalizeTelemetryOutboxOptions(options TelemetryOutboxOptions) TelemetryOutboxOptions {
	normalized := options
	if normalized.WorkerCount <= 0 {
		normalized.WorkerCount = defaultRuntimeTelemetryOutboxWorkerCount
	}
	if normalized.PollInterval <= 0 {
		normalized.PollInterval = defaultRuntimeTelemetryOutboxPollInterval
	}
	if normalized.ShutdownTimeout <= 0 {
		normalized.ShutdownTimeout = defaultRuntimeTelemetryOutboxShutdownTimeout
	}
	if normalized.WakeupBuffer <= 0 {
		normalized.WakeupBuffer = defaultRuntimeTelemetryOutboxWakeupBuffer
	}
	return normalized
}

func (o TelemetryOutboxOptions) hooks() TelemetryOutboxHooks {
	if o.Hooks == nil {
		return TelemetryOutboxHooks{}
	}
	return *o.Hooks
}
