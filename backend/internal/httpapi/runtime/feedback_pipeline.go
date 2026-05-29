package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/asyncmetrics"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	runtimeFeedbackWorkerName           = background.WorkerName("runtime_feedback_pipeline")
	defaultRuntimeFeedbackQueueCapacity = 1024
	defaultRuntimeFeedbackWorkerCount   = 1
	defaultRuntimeFeedbackWriteTimeout  = 2 * time.Second
)

type RuntimeFeedbackPipelineOptions struct {
	QueueCapacity int
	WorkerCount   int
	WriteTimeout  time.Duration
	Hooks         *RuntimeFeedbackPipelineHooks
}

type RuntimeFeedbackPipelineHooks struct {
	AfterEnqueue func(RuntimeFeedbackEnqueueResult)
	AfterWrite   func(RuntimeFeedbackWriteResult)
	StoreError   func() error
}

type RuntimeFeedbackEnqueueStatus string

const (
	RuntimeFeedbackAccepted            RuntimeFeedbackEnqueueStatus = "accepted"
	RuntimeFeedbackDroppedInvalid      RuntimeFeedbackEnqueueStatus = "dropped_invalid"
	RuntimeFeedbackDroppedUnavailable  RuntimeFeedbackEnqueueStatus = "dropped_unavailable"
	RuntimeFeedbackDroppedBackpressure RuntimeFeedbackEnqueueStatus = "dropped_backpressure"
)

type RuntimeFeedbackEnqueueResult struct {
	Status RuntimeFeedbackEnqueueStatus
	Reason string
}

type RuntimeFeedbackWriteResult struct {
	Success bool
	Kind    runtimeFeedbackKind
	Err     error
}

type runtimeFeedbackKind string

const (
	runtimeFeedbackAdmissionRejected runtimeFeedbackKind = "admission_rejected"
	runtimeFeedbackUnbanned          runtimeFeedbackKind = "unbanned"
	runtimeFeedbackSuccessRecovery   runtimeFeedbackKind = "success_recovery"
	runtimeFeedbackFailoverHTTP      runtimeFeedbackKind = "failover_http"
	runtimeFeedbackTransportFailure  runtimeFeedbackKind = "transport_failure"
)

type runtimeFeedbackEvent struct {
	Kind           runtimeFeedbackKind
	ProfileID      int
	ConnectionID   int
	ModelConfigID  int
	APIFamily      string
	TraceContext   runtimeTraceContext
	Strategy       loadbalance.RuntimeStrategy
	State          loadbalance.RuntimeConnectionState
	Transition     loadbalance.RuntimeStateTransition
	FailureKind    string
	ObservedAt     time.Time
	CompletedAt    time.Time
	ResponseTimeMS int
}

type runtimeFeedbackPipeline struct {
	store         *runtimeFeedbackStore
	runtimeState  *loadbalance.LocalRuntimeStateStore
	logPartitions *runtimeLogPartitionCache
	options       RuntimeFeedbackPipelineOptions
	scheduler     *background.Scheduler
	mu            sync.Mutex
	closed        bool
}

func newRuntimeFeedbackPipeline(store *runtimeFeedbackStore, runtimeState *loadbalance.LocalRuntimeStateStore, logPartitions *runtimeLogPartitionCache, options RuntimeFeedbackPipelineOptions) *runtimeFeedbackPipeline {
	return &runtimeFeedbackPipeline{store: store, runtimeState: runtimeState, logPartitions: logPartitions, options: normalizeRuntimeFeedbackPipelineOptions(options)}
}

func (p *runtimeFeedbackPipeline) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if p == nil || scheduler == nil {
		return nil
	}
	p.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             runtimeFeedbackWorkerName,
		Priority:         background.PriorityNormalBackground,
		MaxPriority:      background.PriorityNormalBackground,
		QueueLimit:       p.options.QueueCapacity,
		ConcurrencyLimit: p.options.WorkerCount,
		DrainPolicy:      background.DrainBestEffort,
		CoalescePolicy:   background.CoalesceNone,
		Timeout:          p.options.WriteTimeout,
	}, p.handleScheduledFeedback)
}

func (p *runtimeFeedbackPipeline) TryEnqueue(event runtimeFeedbackEvent) RuntimeFeedbackEnqueueResult {
	return p.TryEnqueueContext(context.Background(), event)
}

func (p *runtimeFeedbackPipeline) TryEnqueueContext(ctx context.Context, event runtimeFeedbackEvent) RuntimeFeedbackEnqueueResult {
	ctx = runtimeTraceDetachedContext(ctx)
	ctx, span := startRuntimeSpan(ctx, "runtime.feedback.enqueue", runtimeTraceFeedbackAttributes(event)...)
	defer span.End()
	if event.TraceContext.empty() {
		event.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if p == nil || p.scheduler == nil {
		runtimeTraceSetFeedbackStatus(span, RuntimeFeedbackDroppedUnavailable)
		runtimeTraceMarkError(span, "feedback_enqueue_failed")
		asyncmetrics.RecordOutcome(ctx, "runtime_feedback_pipeline", "enqueue", asyncmetrics.OutcomeUnavailable)
		return p.observeEnqueue(RuntimeFeedbackEnqueueResult{Status: RuntimeFeedbackDroppedUnavailable, Reason: "pipeline_unavailable"})
	}
	if err := event.validate(); err != nil {
		runtimeTraceSetFeedbackStatus(span, RuntimeFeedbackDroppedInvalid)
		runtimeTraceMarkError(span, "feedback_enqueue_failed")
		asyncmetrics.RecordOutcome(ctx, "runtime_feedback_pipeline", "enqueue", asyncmetrics.OutcomeInvalid)
		return p.observeEnqueue(RuntimeFeedbackEnqueueResult{Status: RuntimeFeedbackDroppedInvalid, Reason: err.Error()})
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		runtimeTraceSetFeedbackStatus(span, RuntimeFeedbackDroppedUnavailable)
		runtimeTraceMarkError(span, "feedback_enqueue_failed")
		asyncmetrics.RecordOutcome(ctx, "runtime_feedback_pipeline", "enqueue", asyncmetrics.OutcomeUnavailable)
		return p.observeEnqueue(RuntimeFeedbackEnqueueResult{Status: RuntimeFeedbackDroppedUnavailable, Reason: "pipeline_closed"})
	}
	result := p.scheduler.Submit(ctx, background.JobRequest{Worker: runtimeFeedbackWorkerName, Payload: event})
	if result.Status != background.SubmitAccepted {
		runtimeTraceSetFeedbackStatus(span, RuntimeFeedbackDroppedBackpressure)
		runtimeTraceMarkError(span, "feedback_enqueue_failed")
		asyncmetrics.RecordOutcome(ctx, "runtime_feedback_pipeline", "enqueue", asyncmetrics.OutcomeBackpressure)
		return p.observeEnqueue(RuntimeFeedbackEnqueueResult{Status: RuntimeFeedbackDroppedBackpressure, Reason: string(result.Status)})
	}
	runtimeTraceSetFeedbackStatus(span, RuntimeFeedbackAccepted)
	asyncmetrics.RecordOutcome(ctx, "runtime_feedback_pipeline", "enqueue", asyncmetrics.OutcomeAccepted)
	return p.observeEnqueue(RuntimeFeedbackEnqueueResult{Status: RuntimeFeedbackAccepted, Reason: "accepted"})
}

func (p *runtimeFeedbackPipeline) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
}

func (p *runtimeFeedbackPipeline) handleScheduledFeedback(ctx context.Context, job background.Job) background.JobResult {
	startedAt := time.Now()
	asyncmetrics.AddInflight(ctx, "runtime_feedback_pipeline", "write", 1)
	defer asyncmetrics.AddInflight(ctx, "runtime_feedback_pipeline", "write", -1)
	event, ok := job.Payload.(runtimeFeedbackEvent)
	if !ok {
		p.observeWrite(RuntimeFeedbackWriteResult{Success: false, Err: fmt.Errorf("invalid runtime feedback payload")})
		asyncmetrics.RecordDuration(ctx, "runtime_feedback_pipeline", "write", asyncmetrics.OutcomeInvalid, time.Since(startedAt))
		return background.JobResult{Status: background.JobSucceeded}
	}
	ctx = event.TraceContext.context(ctx)
	ctx, span := startRuntimeSpan(ctx, "runtime.feedback.write", runtimeTraceFeedbackAttributes(event)...)
	defer span.End()
	writeCtx, cancel := context.WithTimeout(ctx, p.options.WriteTimeout)
	defer cancel()
	if err := p.persist(writeCtx, event); err != nil {
		runtimeTraceMarkError(span, "feedback_write_failed")
		p.observeWrite(RuntimeFeedbackWriteResult{Success: false, Kind: event.Kind, Err: err})
		asyncmetrics.RecordDuration(ctx, "runtime_feedback_pipeline", runtimeFeedbackMetricOperation(event.Kind), asyncmetrics.OutcomeFailure, time.Since(startedAt))
		slog.Warn("runtime feedback write failed", "kind", event.Kind, "profile_id", event.ProfileID, "connection_id", event.ConnectionID, "error", err)
		return background.JobResult{Status: background.JobSucceeded}
	}
	p.observeWrite(RuntimeFeedbackWriteResult{Success: true, Kind: event.Kind})
	asyncmetrics.RecordDuration(ctx, "runtime_feedback_pipeline", runtimeFeedbackMetricOperation(event.Kind), asyncmetrics.OutcomeSuccess, time.Since(startedAt))
	return background.JobResult{Status: background.JobSucceeded}
}

func (p *runtimeFeedbackPipeline) persist(ctx context.Context, event runtimeFeedbackEvent) error {
	if p == nil || p.store == nil || p.store.pool == nil {
		return fmt.Errorf("runtime feedback store unavailable")
	}
	if p.options.Hooks != nil && p.options.Hooks.StoreError != nil {
		if err := p.options.Hooks.StoreError(); err != nil {
			return err
		}
	}
	_, err := pgxutil.InTxValue(ctx, p.store.pool, "runtime_feedback", func(tx pgx.Tx) (bool, error) {
		switch event.Kind {
		case runtimeFeedbackAdmissionRejected:
			return true, loadbalance.InsertRuntimeAdmissionRejectedEvent(ctx, tx, p.logPartitions, event.ProfileID, event.ModelConfigID, event.ConnectionID, event.State, event.ObservedAt)
		case runtimeFeedbackUnbanned:
			return true, loadbalance.InsertRuntimeUnbannedEvent(ctx, tx, p.logPartitions, event.ProfileID, event.ModelConfigID, event.ConnectionID, event.State, event.ObservedAt)
		case runtimeFeedbackSuccessRecovery:
			return true, loadbalance.InsertRuntimeRecoveryEvent(ctx, tx, p.logPartitions, event.ProfileID, event.ModelConfigID, event.ConnectionID, event.Transition, event.Strategy, event.CompletedAt)
		case runtimeFeedbackFailoverHTTP, runtimeFeedbackTransportFailure:
			return true, loadbalance.InsertRuntimeFailureEvent(ctx, tx, p.logPartitions, event.ProfileID, event.ModelConfigID, event.ConnectionID, event.Transition, event.Strategy, event.FailureKind, event.CompletedAt)
		default:
			return false, fmt.Errorf("unsupported runtime feedback kind %q", event.Kind)
		}
	})
	if err != nil {
		return fmt.Errorf("persist runtime feedback: %w", err)
	}
	return nil
}

func (event runtimeFeedbackEvent) validate() error {
	if event.ProfileID <= 0 {
		return fmt.Errorf("profile_id_required")
	}
	if event.ConnectionID <= 0 {
		return fmt.Errorf("connection_id_required")
	}
	switch event.Kind {
	case runtimeFeedbackAdmissionRejected, runtimeFeedbackUnbanned:
		if event.ObservedAt.IsZero() {
			return fmt.Errorf("observed_at_required")
		}
	case runtimeFeedbackSuccessRecovery:
		if event.CompletedAt.IsZero() {
			return fmt.Errorf("completed_at_required")
		}
	case runtimeFeedbackFailoverHTTP, runtimeFeedbackTransportFailure:
		if event.CompletedAt.IsZero() {
			return fmt.Errorf("completed_at_required")
		}
		if event.FailureKind == "" {
			return fmt.Errorf("failure_kind_required")
		}
	default:
		return fmt.Errorf("kind_required")
	}
	return nil
}

func (p *runtimeFeedbackPipeline) observeEnqueue(result RuntimeFeedbackEnqueueResult) RuntimeFeedbackEnqueueResult {
	if p != nil && p.options.Hooks != nil && p.options.Hooks.AfterEnqueue != nil {
		p.options.Hooks.AfterEnqueue(result)
	}
	return result
}

func (p *runtimeFeedbackPipeline) observeWrite(result RuntimeFeedbackWriteResult) {
	if p != nil && p.options.Hooks != nil && p.options.Hooks.AfterWrite != nil {
		p.options.Hooks.AfterWrite(result)
	}
}

func normalizeRuntimeFeedbackPipelineOptions(options RuntimeFeedbackPipelineOptions) RuntimeFeedbackPipelineOptions {
	if options.QueueCapacity <= 0 {
		options.QueueCapacity = defaultRuntimeFeedbackQueueCapacity
	}
	if options.WorkerCount <= 0 {
		options.WorkerCount = defaultRuntimeFeedbackWorkerCount
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = defaultRuntimeFeedbackWriteTimeout
	}
	return options
}

func runtimeFeedbackMetricOperation(kind runtimeFeedbackKind) string {
	switch kind {
	case runtimeFeedbackAdmissionRejected, runtimeFeedbackUnbanned, runtimeFeedbackSuccessRecovery, runtimeFeedbackFailoverHTTP, runtimeFeedbackTransportFailure:
		return string(kind)
	default:
		return "unknown_feedback"
	}
}
