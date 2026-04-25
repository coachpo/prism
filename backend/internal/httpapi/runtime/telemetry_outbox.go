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
	pool             *pgxpool.Pool
	now              func() time.Time
	dashboardUpdates DashboardUpdatePublisher
	pollInterval     time.Duration
	shutdownTimeout  time.Duration
	hooks            TelemetryOutboxHooks
	wake             chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	done             chan struct{}
	wg               sync.WaitGroup
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

func newRuntimeTelemetryOutbox(pool *pgxpool.Pool, now func() time.Time, dashboardUpdates DashboardUpdatePublisher, options TelemetryOutboxOptions) *runtimeTelemetryOutbox {
	normalized := normalizeTelemetryOutboxOptions(options)
	ctx, cancel := context.WithCancel(context.Background())
	outbox := &runtimeTelemetryOutbox{
		pool:             pool,
		now:              now,
		dashboardUpdates: dashboardUpdates,
		pollInterval:     normalized.PollInterval,
		shutdownTimeout:  normalized.ShutdownTimeout,
		hooks:            normalized.hooks(),
		wake:             make(chan struct{}, normalized.WakeupBuffer),
		ctx:              ctx,
		cancel:           cancel,
		done:             make(chan struct{}),
	}
	outbox.wg.Add(normalized.WorkerCount)
	for worker := 0; worker < normalized.WorkerCount; worker++ {
		go outbox.worker()
	}
	go func() {
		outbox.wg.Wait()
		close(outbox.done)
	}()
	outbox.signal()
	return outbox
}

func (o *runtimeTelemetryOutbox) Enqueue(ctx context.Context, envelope runtimeTelemetryEnvelope) error {
	if o == nil || o.pool == nil {
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
	if _, err := o.pool.Exec(
		ctx,
		`INSERT INTO runtime_telemetry_outbox (profile_id, ingress_request_id, payload, created_at) VALUES ($1, $2, $3, $4)`,
		envelope.UsageEvent.ProfileID,
		envelope.UsageEvent.IngressRequestID,
		rawEnvelope,
		envelope.UsageEvent.CreatedAt,
	); err != nil {
		return fmt.Errorf("enqueue runtime telemetry envelope: %w", err)
	}
	o.signal()
	return nil
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
		o.cancel()
		remaining := time.Until(deadline)
		if remaining < 0 {
			remaining = 0
		}
		select {
		case <-o.done:
		case <-time.After(remaining):
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

func (o *runtimeTelemetryOutbox) worker() {
	defer o.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-o.ctx.Done():
			return
		case <-o.wake:
		case <-timer.C:
		}
		for {
			processed, err := o.processNext(o.ctx)
			if err != nil {
				break
			}
			if !processed {
				break
			}
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(o.pollInterval)
	}
}

func (o *runtimeTelemetryOutbox) processNext(ctx context.Context) (bool, error) {
	o.beginInflight()
	result, err := pgxutil.InTxValue(ctx, o.pool, "runtime", func(tx pgx.Tx) (runtimeTelemetryMaterializationResult, error) {
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
		if o.hooks.BeforeMaterialize != nil {
			if err := o.hooks.BeforeMaterialize(ctx); err != nil {
				return runtimeTelemetryMaterializationResult{}, err
			}
		}
		requestLogID, err := materializeRuntimeTelemetryEnvelopeTx(ctx, tx, envelope)
		if err != nil {
			return runtimeTelemetryMaterializationResult{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM runtime_telemetry_outbox WHERE id = $1`, row.ID); err != nil {
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
		_, _ = o.dashboardUpdates.PublishDashboardUpdate(ctx, result.RequestLogID, result.ProfileID)
	}
	return true, nil
}

func (o *runtimeTelemetryOutbox) drainState() (runtimeTelemetryDrainState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	var pendingRows int
	if err := o.pool.QueryRow(ctx, `SELECT COUNT(*) FROM runtime_telemetry_outbox`).Scan(&pendingRows); err != nil {
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
		`SELECT id, payload FROM runtime_telemetry_outbox ORDER BY id ASC LIMIT 1 FOR UPDATE SKIP LOCKED`,
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
