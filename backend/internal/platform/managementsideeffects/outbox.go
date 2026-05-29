package managementsideeffects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/asyncmetrics"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	WorkerName = background.WorkerName("management_side_effect_outbox")

	EventDashboardSnapshotInvalidate = "management.dashboard_snapshot.invalidate"

	defaultBatchSize       = 50
	defaultConcurrency     = 4
	defaultPollInterval    = 5 * time.Second
	defaultClaimTimeout    = 2 * time.Second
	defaultHandlerTimeout  = 30 * time.Second
	defaultRetryCap        = 8
	defaultBackoffBase     = 5 * time.Second
	defaultBackoffCap      = 15 * time.Minute
	defaultStaleLockAfter  = 5 * time.Minute
	defaultShutdownTimeout = 30 * time.Second
)

type Intent struct {
	OperationID      string
	EventType        string
	AggregateType    string
	AggregateID      string
	AggregateVersion *int64
	DedupeKey        string
	Payload          any
	ActorID          *string
	TraceID          *string
}

type Event struct {
	ID               int64
	OperationID      string
	EventType        string
	AggregateType    string
	AggregateID      string
	AggregateVersion *int64
	DedupeKey        string
	Payload          json.RawMessage
	AttemptCount     int
	CreatedAt        time.Time
}

type Handler func(context.Context, Event) error

type PermanentError struct {
	Err error
}

func (e PermanentError) Error() string {
	if e.Err == nil {
		return "permanent management side-effect failure"
	}
	return e.Err.Error()
}

func (e PermanentError) Unwrap() error { return e.Err }

type Dispatcher struct {
	pool        *pgxpool.Pool
	scheduler   *background.Scheduler
	lockedBy    string
	now         func() time.Time
	handlersMu  sync.RWMutex
	handlers    map[string]Handler
	batchSize   int
	maxAttempts int
}

type Options struct {
	Pool      *pgxpool.Pool
	Scheduler *background.Scheduler
	Now       func() time.Time
	LockedBy  string
}

type DashboardSnapshotInvalidatePayload struct {
	ProfileID int `json:"profile_id"`
}

func NewDispatcher(options Options) *Dispatcher {
	if options.Pool == nil {
		return nil
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	lockedBy := options.LockedBy
	if lockedBy == "" {
		lockedBy = defaultLockedBy()
	}
	return &Dispatcher{pool: options.Pool, scheduler: options.Scheduler, lockedBy: lockedBy, now: now, handlers: map[string]Handler{}, batchSize: defaultBatchSize, maxAttempts: defaultRetryCap}
}

func (d *Dispatcher) RegisterHandler(eventType string, handler Handler) {
	if d == nil || eventType == "" || handler == nil {
		return
	}
	d.handlersMu.Lock()
	d.handlers[eventType] = handler
	d.handlersMu.Unlock()
}

func (d *Dispatcher) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if d == nil || scheduler == nil {
		return nil
	}
	d.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             WorkerName,
		Priority:         background.PriorityNormalBackground,
		MaxPriority:      background.PriorityNormalBackground,
		QueueLimit:       defaultBatchSize,
		ConcurrencyLimit: defaultConcurrency,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceDropNew,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 3, Delay: defaultPollInterval},
		PeriodicTrigger:  &background.PeriodicTrigger{Interval: defaultPollInterval},
		Timeout:          defaultShutdownTimeout,
	}, d.handleScheduledDispatch)
}

func (d *Dispatcher) Wake(ctx context.Context) error {
	if d == nil || d.scheduler == nil {
		return nil
	}
	result := d.scheduler.Submit(ctx, background.JobRequest{Worker: WorkerName, CoalesceKey: string(WorkerName)})
	asyncmetrics.RecordOutcome(ctx, "management_side_effect_outbox", "wake", managementSideEffectSubmitOutcome(result.Status))
	switch result.Status {
	case background.SubmitAccepted, background.SubmitCoalesced:
		return nil
	default:
		return fmt.Errorf("wake management side-effect dispatcher: %s", result.Status)
	}
}

func InsertTx(ctx context.Context, tx pgx.Tx, intent Intent) (int64, error) {
	if intent.EventType == "" || intent.AggregateType == "" || intent.AggregateID == "" || intent.DedupeKey == "" {
		return 0, fmt.Errorf("management side-effect intent missing required identity")
	}
	rawPayload, err := json.Marshal(intent.Payload)
	if err != nil {
		return 0, fmt.Errorf("marshal management side-effect payload: %w", err)
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO management_outbox (
			operation_id, event_type, aggregate_type, aggregate_id, aggregate_version,
			dedupe_key, payload, status, actor_id, trace_id, created_at, next_attempt_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9, now(), now())
		ON CONFLICT (dedupe_key) DO UPDATE SET dedupe_key = EXCLUDED.dedupe_key
		RETURNING id`,
		intent.OperationID,
		intent.EventType,
		intent.AggregateType,
		intent.AggregateID,
		intent.AggregateVersion,
		intent.DedupeKey,
		rawPayload,
		intent.ActorID,
		intent.TraceID,
	).Scan(&id)
	if err != nil {
		asyncmetrics.RecordOutcome(ctx, "management_side_effect_outbox", "enqueue", asyncmetrics.OutcomeFailure)
		return 0, fmt.Errorf("insert management side-effect outbox row: %w", err)
	}
	asyncmetrics.RecordOutcome(ctx, "management_side_effect_outbox", "enqueue", asyncmetrics.OutcomeSuccess)
	return id, nil
}

func AfterCommit(ctx context.Context, wake func(context.Context) error, hooks ...func(context.Context) error) {
	for _, hook := range hooks {
		if hook == nil {
			continue
		}
		if err := hook(ctx); err != nil {
			asyncmetrics.RecordOutcome(ctx, "management_side_effect_outbox", "after_commit_hook", asyncmetrics.OutcomeFailure)
			slog.Error("management after-commit hook failed", "error", err)
		} else {
			asyncmetrics.RecordOutcome(ctx, "management_side_effect_outbox", "after_commit_hook", asyncmetrics.OutcomeSuccess)
		}
	}
	if wake != nil {
		if err := wake(ctx); err != nil {
			asyncmetrics.RecordOutcome(ctx, "management_side_effect_outbox", "after_commit_wake", asyncmetrics.OutcomeFailure)
			slog.Error("management side-effect dispatcher wake failed", "error", err)
		} else {
			asyncmetrics.RecordOutcome(ctx, "management_side_effect_outbox", "after_commit_wake", asyncmetrics.OutcomeSuccess)
		}
	}
}

func InTxValue[T any](ctx context.Context, beginner pgxutil.Beginner, label string, dispatcher *Dispatcher, fn func(pgx.Tx) (T, []func(context.Context) error, error)) (T, error) {
	result, err := pgxutil.InTxValue(ctx, beginner, label, func(tx pgx.Tx) (struct {
		Value T
		Hooks []func(context.Context) error
	}, error) {
		value, hooks, err := fn(tx)
		return struct {
			Value T
			Hooks []func(context.Context) error
		}{Value: value, Hooks: hooks}, err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	AfterCommit(context.Background(), dispatcher.Wake, result.Hooks...)
	return result.Value, nil
}

func (d *Dispatcher) handleScheduledDispatch(ctx context.Context, _ background.Job) background.JobResult {
	if d == nil || d.pool == nil {
		return background.JobResult{Status: background.JobSucceeded}
	}
	startedAt := time.Now()
	if err := d.recoverStaleLocks(ctx); err != nil {
		asyncmetrics.RecordDuration(ctx, "management_side_effect_outbox", "scheduled_dispatch", asyncmetrics.OutcomeFailure, time.Since(startedAt))
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	rows, err := d.claimBatch(ctx)
	if err != nil {
		asyncmetrics.RecordDuration(ctx, "management_side_effect_outbox", "scheduled_dispatch", asyncmetrics.OutcomeFailure, time.Since(startedAt))
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	asyncmetrics.RecordBatchSize(ctx, "management_side_effect_outbox", "claim", int64(len(rows)))
	for _, row := range rows {
		d.processRow(ctx, row)
	}
	asyncmetrics.RecordDuration(ctx, "management_side_effect_outbox", "scheduled_dispatch", asyncmetrics.OutcomeSuccess, time.Since(startedAt))
	return background.JobResult{Status: background.JobSucceeded}
}

func (d *Dispatcher) recoverStaleLocks(ctx context.Context) error {
	claimCtx, cancel := context.WithTimeout(ctx, defaultClaimTimeout)
	defer cancel()
	_, err := d.pool.Exec(claimCtx, `
		UPDATE management_outbox
		SET status = 'retry', locked_by = NULL, locked_at = NULL, next_attempt_at = now()
		WHERE status = 'processing' AND locked_at < now() - $1::interval`, intervalLiteral(defaultStaleLockAfter))
	if err != nil {
		return fmt.Errorf("recover stale management side-effect locks: %w", err)
	}
	return nil
}

func (d *Dispatcher) claimBatch(ctx context.Context) ([]Event, error) {
	claimCtx, cancel := context.WithTimeout(ctx, defaultClaimTimeout)
	defer cancel()
	return pgxutil.InTxValue(claimCtx, d.pool, "management_side_effect_claim", func(tx pgx.Tx) ([]Event, error) {
		rows, err := tx.Query(claimCtx, `
			WITH claimable AS (
				SELECT id FROM management_outbox
				WHERE status IN ('pending', 'retry') AND next_attempt_at <= now()
				ORDER BY next_attempt_at ASC, created_at ASC, id ASC
				LIMIT $1
				FOR UPDATE SKIP LOCKED
			)
			UPDATE management_outbox outbox
			SET status = 'processing', attempt_count = outbox.attempt_count + 1, locked_by = $2, locked_at = now(), last_error = NULL
			FROM claimable
			WHERE outbox.id = claimable.id
			RETURNING outbox.id, outbox.operation_id, outbox.event_type, outbox.aggregate_type, outbox.aggregate_id,
				outbox.aggregate_version, outbox.dedupe_key, outbox.payload, outbox.attempt_count, outbox.created_at`, d.batchSize, d.lockedBy)
		if err != nil {
			return nil, fmt.Errorf("claim management side-effect rows: %w", err)
		}
		defer rows.Close()
		claimed := []Event{}
		for rows.Next() {
			var event Event
			if err := rows.Scan(&event.ID, &event.OperationID, &event.EventType, &event.AggregateType, &event.AggregateID, &event.AggregateVersion, &event.DedupeKey, &event.Payload, &event.AttemptCount, &event.CreatedAt); err != nil {
				return nil, fmt.Errorf("scan management side-effect row: %w", err)
			}
			claimed = append(claimed, event)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate management side-effect rows: %w", err)
		}
		return claimed, nil
	})
}

func (d *Dispatcher) processRow(ctx context.Context, event Event) {
	startedAt := time.Now()
	asyncmetrics.AddInflight(ctx, "management_side_effect_outbox", "process_row", 1)
	defer asyncmetrics.AddInflight(ctx, "management_side_effect_outbox", "process_row", -1)
	handler := d.handler(event.EventType)
	if handler == nil {
		d.finalizeFailure(ctx, event, PermanentError{Err: fmt.Errorf("no handler registered for %s", event.EventType)})
		asyncmetrics.RecordDuration(ctx, "management_side_effect_outbox", "process_row", asyncmetrics.OutcomePermanentFailure, time.Since(startedAt))
		return
	}
	handlerCtx, cancel := context.WithTimeout(ctx, defaultHandlerTimeout)
	err := handler(handlerCtx, event)
	cancel()
	if err == nil {
		d.finalizeSuccess(context.Background(), event.ID)
		asyncmetrics.RecordDuration(ctx, "management_side_effect_outbox", "process_row", asyncmetrics.OutcomeSuccess, time.Since(startedAt))
		return
	}
	d.finalizeFailure(context.Background(), event, err)
	asyncmetrics.RecordDuration(ctx, "management_side_effect_outbox", "process_row", managementSideEffectFailureOutcome(event, err, d.maxAttempts), time.Since(startedAt))
}

func (d *Dispatcher) handler(eventType string) Handler {
	d.handlersMu.RLock()
	defer d.handlersMu.RUnlock()
	return d.handlers[eventType]
}

func (d *Dispatcher) finalizeSuccess(ctx context.Context, id int64) {
	_, err := d.pool.Exec(ctx, `UPDATE management_outbox SET status = 'succeeded', processed_at = now(), locked_by = NULL, locked_at = NULL, last_error = NULL WHERE id = $1`, id)
	if err != nil {
		slog.Error("failed to finalize management side-effect success", "error", err, "outbox_id", id)
	}
}

func (d *Dispatcher) finalizeFailure(ctx context.Context, event Event, err error) {
	lastError := err.Error()
	status := "retry"
	nextAttemptAt := d.now().UTC().Add(backoffForAttempt(event.AttemptCount))
	var permanent PermanentError
	if errors.As(err, &permanent) || event.AttemptCount >= d.maxAttempts {
		status = "failed_permanent"
		nextAttemptAt = d.now().UTC()
		asyncmetrics.RecordRetry(ctx, "management_side_effect_outbox", "process_row", asyncmetrics.OutcomeRetryExhausted)
	} else {
		asyncmetrics.RecordRetry(ctx, "management_side_effect_outbox", "process_row", asyncmetrics.OutcomeRetryScheduled)
	}
	_, execErr := d.pool.Exec(ctx, `
		UPDATE management_outbox
		SET status = $2, next_attempt_at = $3, locked_by = NULL, locked_at = NULL, last_error = $4
		WHERE id = $1`, event.ID, status, nextAttemptAt, lastError)
	if execErr != nil {
		slog.Error("failed to finalize management side-effect failure", "error", execErr, "outbox_id", event.ID, "handler_error", lastError)
		return
	}
	slog.Error("management side-effect handler failed", "error", err, "outbox_id", event.ID, "event_type", event.EventType, "status", status, "attempt", event.AttemptCount)
}

func backoffForAttempt(attempt int) time.Duration {
	if attempt <= 1 {
		return defaultBackoffBase
	}
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(defaultBackoffBase) * multiplier)
	if delay > defaultBackoffCap || delay <= 0 {
		return defaultBackoffCap
	}
	return delay
}

func intervalLiteral(duration time.Duration) string {
	seconds := int64(duration.Seconds())
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf("%d seconds", seconds)
}

func defaultLockedBy() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", hostname, os.Getpid())
}

func managementSideEffectSubmitOutcome(status background.SubmitStatus) string {
	switch status {
	case background.SubmitAccepted:
		return asyncmetrics.OutcomeAccepted
	case background.SubmitCoalesced:
		return asyncmetrics.OutcomeCoalesced
	case background.SubmitRejectedBackpressure:
		return asyncmetrics.OutcomeBackpressure
	case background.SubmitRejectedStopping, background.SubmitRejectedUnknownWorker, background.SubmitRejectedInvalidPriority:
		return asyncmetrics.OutcomeRejected
	default:
		return asyncmetrics.OutcomeOther
	}
}

func managementSideEffectFailureOutcome(event Event, err error, maxAttempts int) string {
	var permanent PermanentError
	if errors.As(err, &permanent) || event.AttemptCount >= maxAttempts {
		return asyncmetrics.OutcomePermanentFailure
	}
	return asyncmetrics.OutcomeTransientFailure
}
