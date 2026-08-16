package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	telemetryPool   *pgxpool.Pool
	now             func() time.Time
	logPartitions   *runtimeLogPartitionCache
	pollInterval    time.Duration
	shutdownTimeout time.Duration
	hooks           TelemetryOutboxHooks
	wake            chan struct{}
	scheduler       *background.Scheduler
	ownsScheduler   bool
	closeOnce       sync.Once
	mu              sync.Mutex
	closed          bool
	inflight        int
	closeResult     TelemetryOutboxCloseResult
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

func newRuntimeTelemetryOutbox(telemetryPool *pgxpool.Pool, now func() time.Time, logPartitions *runtimeLogPartitionCache, options TelemetryOutboxOptions) *runtimeTelemetryOutbox {
	normalized := normalizeTelemetryOutboxOptions(options)
	outbox := &runtimeTelemetryOutbox{
		telemetryPool:   telemetryPool,
		now:             now,
		logPartitions:   logPartitions,
		pollInterval:    normalized.PollInterval,
		shutdownTimeout: normalized.ShutdownTimeout,
		hooks:           normalized.hooks(),
		wake:            make(chan struct{}, normalized.WakeupBuffer),
		scheduler:       normalized.Scheduler,
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
	return o.enqueueV2(ctx, envelope, "provisional_stream")
}

func (o *runtimeTelemetryOutbox) FinalizeStreamingAccepted(ctx context.Context, rowID int64, envelope runtimeTelemetryEnvelope) error {
	return o.finalizeV2StreamingAccepted(ctx, rowID, envelope)
}

func (o *runtimeTelemetryOutbox) enqueue(ctx context.Context, envelope runtimeTelemetryEnvelope) (int64, error) {
	if envelope.TraceContext.empty() {
		envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	return o.enqueueV2(ctx, envelope, "finalized")
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
			slog.Error("runtime telemetry materialization failed; will retry", "error", err)
			return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
		}
		if !processed {
			return background.JobResult{Status: background.JobSucceeded}
		}
	}
}

func (o *runtimeTelemetryOutbox) processNext(ctx context.Context) (bool, error) {
	o.beginInflight()
	// The claimed row has to escape the transaction as a Go value: when
	// materialization aborts, any accounting written inside that transaction
	// rolls back with it, and a row that is never accounted for is retried
	// forever ahead of everything behind it.
	var claimed *v2MetadataRow
	result, err := pgxutil.InTxValue(ctx, o.telemetryPool, "runtime_telemetry", func(tx pgx.Tx) (runtimeTelemetryMaterializationResult, error) {
		claimed = nil
		// Prefer the v2 metadata item; fall back to the legacy v1 envelope.
		v2Row, v2Found, v2Err := loadNextV2MetadataRow(ctx, tx)
		if v2Err != nil {
			return runtimeTelemetryMaterializationResult{}, v2Err
		}
		if v2Found {
			claimed = &v2Row
			return o.materializeV2MetadataRow(ctx, tx, v2Row)
		}
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
		if claimed != nil {
			// context.WithoutCancel: during shutdown the attempt's context may
			// already be cancelled, and skipping the accounting is exactly how
			// a row becomes immortal.
			if accountErr := o.recordMaterializationFailure(context.WithoutCancel(ctx), *claimed, err); accountErr != nil {
				slog.Error("could not account for a telemetry materialization failure",
					"row_id", claimed.ID, "error", accountErr)
				return false, err
			}
			// The row is now either backed off or quarantined, so the queue can
			// advance. Report progress rather than an error: aborting the drain
			// here is what let one bad row block every later one.
			return true, nil
		}
		return false, err
	}
	if !result.Processed {
		return false, nil
	}
	return true, nil
}

// materializeV2MetadataRow materializes a v2 metadata item: decode the core
// payload, reattach the scrubbed header/body artifacts by stable key,
// materialize request/usage/audit/accounting, then ACK the metadata row and
// its artifacts (Requests SPEC §5.1 idempotent merge).
func (o *runtimeTelemetryOutbox) materializeV2MetadataRow(ctx context.Context, tx pgx.Tx, row v2MetadataRow) (runtimeTelemetryMaterializationResult, error) {
	var metadata v2MetadataPayload
	if err := json.Unmarshal(row.CorePayload, &metadata); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("decode v2 metadata payload row %d: %w", row.ID, err)
	}
	if metadata.Envelope.TraceContext.empty() {
		metadata.Envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	materializeCtx := metadata.Envelope.TraceContext.context(ctx)
	if o.hooks.BeforeMaterialize != nil {
		if err := o.hooks.BeforeMaterialize(materializeCtx); err != nil {
			return runtimeTelemetryMaterializationResult{}, err
		}
	}
	artifacts, err := loadArtifactsForIngress(materializeCtx, tx, row.ProfileID, row.IngressID)
	if err != nil {
		return runtimeTelemetryMaterializationResult{}, err
	}
	mergeArtifactsIntoEnvelope(&metadata.Envelope, artifacts)
	requestLogID, err := materializeRuntimeTelemetryEnvelopeTx(materializeCtx, tx, o.logPartitions, metadata.Envelope)
	if err != nil {
		return runtimeTelemetryMaterializationResult{}, err
	}
	// ACK the metadata row and its artifacts together; core state advanced
	// first so extension-only retries never re-report usage as pending.
	if _, err := tx.Exec(materializeCtx, `UPDATE runtime_telemetry_outbox SET core_state = 'materialized', core_materialized_at = now(), core_payload = NULL WHERE id = $1`, row.ID); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("ack v2 metadata row %d: %w", row.ID, err)
	}
	if _, err := tx.Exec(materializeCtx, `DELETE FROM runtime_telemetry_artifacts WHERE profile_id = $1 AND ingress_request_id = $2`, row.ProfileID, row.IngressID); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("ack v2 artifacts for ingress %s: %w", row.IngressID, err)
	}
	if _, err := tx.Exec(materializeCtx, `DELETE FROM runtime_telemetry_outbox WHERE id = $1 AND core_state = 'materialized'`, row.ID); err != nil {
		return runtimeTelemetryMaterializationResult{}, fmt.Errorf("delete v2 metadata row %d: %w", row.ID, err)
	}
	return runtimeTelemetryMaterializationResult{Processed: true, RequestLogID: requestLogID, ProfileID: row.ProfileID}, nil
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
		`SELECT id, payload FROM runtime_telemetry_outbox WHERE schema_version = 1 AND COALESCE(payload->>'handoff_phase', '') <> $1 ORDER BY id ASC LIMIT 1 FOR UPDATE SKIP LOCKED`,
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
