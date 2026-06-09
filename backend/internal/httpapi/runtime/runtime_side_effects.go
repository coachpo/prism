package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	gatewayaccounting "github.com/coachpo/prism/backend/internal/gateway/accounting"
	"github.com/coachpo/prism/backend/internal/platform/asyncmetrics"
	"github.com/coachpo/prism/backend/internal/platform/background"
)

const (
	runtimeSideEffectsWorkerName            = background.WorkerName("runtime_side_effects_activity")
	defaultRuntimeSideEffectQueueCapacity   = 1024
	defaultRuntimeSideEffectWorkerCount     = 1
	defaultRuntimeSideEffectAttemptTimeout  = 10 * time.Second
	defaultRuntimeSideEffectShutdownTimeout = 3 * time.Second
	defaultRuntimeSideEffectRetryDelay      = 25 * time.Millisecond
	defaultRuntimeSideEffectMaxAttempts     = 5
)

type RuntimeActivityIntent struct {
	Envelope     runtimeTelemetryEnvelope
	TraceContext runtimeTraceContext
}

type RuntimeSideEffectOptions struct {
	QueueCapacity   int
	WorkerCount     int
	AttemptTimeout  time.Duration
	ShutdownTimeout time.Duration
	RetryDelay      time.Duration
	MaxAttempts     int
	Hooks           *RuntimeSideEffectHooks
}

type RuntimeSideEffectHooks struct {
	AfterSubmit     func(RuntimeSideEffectSubmitResult)
	AfterCommit     func(RuntimeActivityIntent)
	TerminalFailure func(RuntimeActivityIntent, error)
}

type RuntimeSideEffectSubmitStatus string

const (
	RuntimeSideEffectAccepted RuntimeSideEffectSubmitStatus = "accepted"
	RuntimeSideEffectRejected RuntimeSideEffectSubmitStatus = "rejected"
)

type RuntimeSideEffectSubmitResult struct {
	Status RuntimeSideEffectSubmitStatus
	Reason string
}

type RuntimeSideEffectCloseResult struct {
	Drained         bool
	TimedOut        bool
	Pending         int
	ForcedAbandoned int
	Elapsed         time.Duration
}

type RuntimeSideEffectManager struct {
	outbox    *runtimeTelemetryOutbox
	options   RuntimeSideEffectOptions
	scheduler *background.Scheduler
	mu        sync.Mutex
	closed    bool
	pending   int
}

func NewRuntimeSideEffectManager(outbox *runtimeTelemetryOutbox, options RuntimeSideEffectOptions) *RuntimeSideEffectManager {
	return &RuntimeSideEffectManager{outbox: outbox, options: normalizeRuntimeSideEffectOptions(options)}
}

func (m *RuntimeSideEffectManager) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if m == nil || scheduler == nil {
		return nil
	}
	m.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             runtimeSideEffectsWorkerName,
		Priority:         background.PriorityNormalBackground,
		MaxPriority:      background.PriorityNormalBackground,
		QueueLimit:       m.options.QueueCapacity,
		ConcurrencyLimit: m.options.WorkerCount,
		DrainPolicy:      background.DrainFlush,
		CoalescePolicy:   background.CoalesceNone,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: m.options.MaxAttempts, Delay: m.options.RetryDelay},
		Timeout:          m.options.AttemptTimeout,
	}, m.handleRuntimeActivity)
}

func (m *RuntimeSideEffectManager) SubmitRuntimeActivity(intent RuntimeActivityIntent) RuntimeSideEffectSubmitResult {
	return m.SubmitRuntimeActivityContext(context.Background(), intent)
}

func (m *RuntimeSideEffectManager) SubmitRuntimeActivityContext(ctx context.Context, intent RuntimeActivityIntent) RuntimeSideEffectSubmitResult {
	ctx = runtimeTraceDetachedContext(ctx)
	ctx, span := startRuntimeSpan(ctx, "side_effect.submit", runtimeTraceEnvelopeAttributes(intent.Envelope)...)
	defer span.End()
	if intent.TraceContext.empty() {
		intent.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if intent.Envelope.TraceContext.empty() {
		intent.Envelope.TraceContext = intent.TraceContext
	}
	if m == nil || m.scheduler == nil {
		runtimeTraceMarkError(span, "side_effect_submit_rejected")
		asyncmetrics.RecordOutcome(ctx, "runtime_side_effect_manager", "submit", asyncmetrics.OutcomeUnavailable)
		return m.observeSubmit(RuntimeSideEffectSubmitResult{Status: RuntimeSideEffectRejected, Reason: "side_effect_manager_unavailable"})
	}
	if err := intent.validate(); err != nil {
		runtimeTraceMarkError(span, "side_effect_submit_rejected")
		asyncmetrics.RecordOutcome(ctx, "runtime_side_effect_manager", "submit", asyncmetrics.OutcomeInvalid)
		return m.observeSubmit(RuntimeSideEffectSubmitResult{Status: RuntimeSideEffectRejected, Reason: err.Error()})
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		runtimeTraceMarkError(span, "side_effect_submit_rejected")
		asyncmetrics.RecordOutcome(ctx, "runtime_side_effect_manager", "submit", asyncmetrics.OutcomeUnavailable)
		return m.observeSubmit(RuntimeSideEffectSubmitResult{Status: RuntimeSideEffectRejected, Reason: "side_effect_manager_closed"})
	}
	m.pending++
	asyncmetrics.RecordQueueDepth(ctx, "runtime_side_effect_manager", "pending", int64(m.pending))
	m.mu.Unlock()
	result := m.scheduler.Submit(ctx, background.JobRequest{Worker: runtimeSideEffectsWorkerName, Payload: intent.clone()})
	if result.Status != background.SubmitAccepted {
		m.finishIntent()
		runtimeTraceMarkError(span, "side_effect_submit_rejected")
		asyncmetrics.RecordOutcome(ctx, "runtime_side_effect_manager", "submit", asyncmetrics.OutcomeBackpressure)
		return m.observeSubmit(RuntimeSideEffectSubmitResult{Status: RuntimeSideEffectRejected, Reason: string(result.Status)})
	}
	asyncmetrics.RecordOutcome(ctx, "runtime_side_effect_manager", "submit", asyncmetrics.OutcomeAccepted)
	return m.observeSubmit(RuntimeSideEffectSubmitResult{Status: RuntimeSideEffectAccepted, Reason: "accepted"})
}

func (m *RuntimeSideEffectManager) CommitRuntimeActivityBeforeResponse(ctx context.Context, intent RuntimeActivityIntent) error {
	if m == nil || m.outbox == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	if intent.TraceContext.empty() {
		intent.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if intent.Envelope.TraceContext.empty() {
		intent.Envelope.TraceContext = intent.TraceContext
	}
	if err := intent.validate(); err != nil {
		return err
	}
	return m.outbox.Enqueue(ctx, intent.Envelope)
}

func (m *RuntimeSideEffectManager) CommitStreamingRuntimeActivityAcceptedBeforeResponse(ctx context.Context, intent RuntimeActivityIntent) (int64, error) {
	if m == nil || m.outbox == nil {
		return 0, fmt.Errorf("runtime telemetry outbox unavailable")
	}
	if intent.TraceContext.empty() {
		intent.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if intent.Envelope.TraceContext.empty() {
		intent.Envelope.TraceContext = intent.TraceContext
	}
	if err := intent.validate(); err != nil {
		return 0, err
	}
	return m.outbox.EnqueueStreamingAccepted(ctx, intent.Envelope)
}

func (m *RuntimeSideEffectManager) FinalizeStreamingRuntimeActivityBeforeCompletion(ctx context.Context, acceptedRowID int64, intent RuntimeActivityIntent) error {
	if m == nil || m.outbox == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	if intent.TraceContext.empty() {
		intent.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if intent.Envelope.TraceContext.empty() {
		intent.Envelope.TraceContext = intent.TraceContext
	}
	if err := intent.validate(); err != nil {
		return err
	}
	return m.outbox.FinalizeStreamingAccepted(ctx, acceptedRowID, intent.Envelope)
}

func (m *RuntimeSideEffectManager) Close() RuntimeSideEffectCloseResult {
	if m == nil {
		return RuntimeSideEffectCloseResult{Drained: true}
	}
	startedAt := time.Now()
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	deadline := startedAt.Add(m.options.ShutdownTimeout)
	for time.Now().Before(deadline) {
		pending := m.pendingCount()
		if pending == 0 {
			elapsed := time.Since(startedAt)
			asyncmetrics.RecordDuration(context.Background(), "runtime_side_effect_manager", "close", asyncmetrics.OutcomeSuccess, elapsed)
			return RuntimeSideEffectCloseResult{Drained: true, Elapsed: elapsed}
		}
		time.Sleep(10 * time.Millisecond)
	}
	pending := m.pendingCount()
	elapsed := time.Since(startedAt)
	outcome := asyncmetrics.OutcomeSuccess
	if pending > 0 {
		outcome = asyncmetrics.OutcomeTimeout
	}
	asyncmetrics.RecordQueueDepth(context.Background(), "runtime_side_effect_manager", "pending", int64(pending))
	asyncmetrics.RecordDuration(context.Background(), "runtime_side_effect_manager", "close", outcome, elapsed)
	return RuntimeSideEffectCloseResult{TimedOut: pending > 0, Pending: pending, ForcedAbandoned: pending, Elapsed: elapsed}
}

func (m *RuntimeSideEffectManager) handleRuntimeActivity(ctx context.Context, job background.Job) background.JobResult {
	startedAt := time.Now()
	asyncmetrics.AddInflight(ctx, "runtime_side_effect_manager", "commit_runtime_activity", 1)
	defer asyncmetrics.AddInflight(ctx, "runtime_side_effect_manager", "commit_runtime_activity", -1)
	intent, ok := job.Payload.(RuntimeActivityIntent)
	if !ok {
		m.finishIntent()
		asyncmetrics.RecordDuration(ctx, "runtime_side_effect_manager", "commit_runtime_activity", asyncmetrics.OutcomeInvalid, time.Since(startedAt))
		return background.JobResult{Status: background.JobSucceeded}
	}
	ctx = intent.TraceContext.context(ctx)
	ctx, span := startRuntimeSpan(ctx, "side_effect.commit", runtimeTraceEnvelopeAttributes(intent.Envelope)...)
	defer span.End()
	if err := intent.validate(); err != nil {
		runtimeTraceMarkError(span, "invalid_payload")
		m.terminalFailure(intent, err)
		m.finishIntent()
		asyncmetrics.RecordDuration(ctx, "runtime_side_effect_manager", "commit_runtime_activity", asyncmetrics.OutcomeInvalid, time.Since(startedAt))
		return background.JobResult{Status: background.JobSucceeded}
	}
	attemptCtx, cancel := context.WithTimeout(ctx, m.options.AttemptTimeout)
	defer cancel()
	if err := m.outbox.Enqueue(attemptCtx, intent.Envelope); err != nil {
		runtimeTraceMarkError(span, "side_effect_commit_failed")
		asyncmetrics.RecordOutbound(attemptCtx, "runtime_side_effect_manager", "runtime_telemetry_outbox", asyncmetrics.OutcomeFromError(err), time.Since(startedAt))
		if job.Attempt < m.options.MaxAttempts {
			asyncmetrics.RecordRetry(ctx, "runtime_side_effect_manager", "commit_runtime_activity", asyncmetrics.OutcomeRetryScheduled)
			asyncmetrics.RecordDuration(ctx, "runtime_side_effect_manager", "commit_runtime_activity", asyncmetrics.OutcomeRetryScheduled, time.Since(startedAt))
			return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
		}
		asyncmetrics.RecordRetry(ctx, "runtime_side_effect_manager", "commit_runtime_activity", asyncmetrics.OutcomeRetryExhausted)
		m.terminalFailure(intent, err)
		m.finishIntent()
		asyncmetrics.RecordDuration(ctx, "runtime_side_effect_manager", "commit_runtime_activity", asyncmetrics.OutcomeFailure, time.Since(startedAt))
		return background.JobResult{Status: background.JobFailed, Err: err}
	}
	asyncmetrics.RecordOutbound(attemptCtx, "runtime_side_effect_manager", "runtime_telemetry_outbox", asyncmetrics.OutcomeSuccess, time.Since(startedAt))
	if m.options.Hooks != nil && m.options.Hooks.AfterCommit != nil {
		m.options.Hooks.AfterCommit(intent)
	}
	m.finishIntent()
	asyncmetrics.RecordDuration(ctx, "runtime_side_effect_manager", "commit_runtime_activity", asyncmetrics.OutcomeSuccess, time.Since(startedAt))
	return background.JobResult{Status: background.JobSucceeded}
}

func (m *RuntimeSideEffectManager) finishIntent() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.pending > 0 {
		m.pending--
	}
	pending := m.pending
	m.mu.Unlock()
	asyncmetrics.RecordQueueDepth(context.Background(), "runtime_side_effect_manager", "pending", int64(pending))
}

func (m *RuntimeSideEffectManager) pendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending
}

func (m *RuntimeSideEffectManager) terminalFailure(intent RuntimeActivityIntent, err error) {
	if m != nil && m.options.Hooks != nil && m.options.Hooks.TerminalFailure != nil {
		m.options.Hooks.TerminalFailure(intent, err)
	}
	slog.Error("runtime activity telemetry intent failed", "error", err, "profile_id", intent.Envelope.UsageEvent.ProfileID, "ingress_request_id", intent.Envelope.UsageEvent.IngressRequestID)
}

func (m *RuntimeSideEffectManager) observeSubmit(result RuntimeSideEffectSubmitResult) RuntimeSideEffectSubmitResult {
	if m != nil && m.options.Hooks != nil && m.options.Hooks.AfterSubmit != nil {
		m.options.Hooks.AfterSubmit(result)
	}
	return result
}

func (intent RuntimeActivityIntent) validate() error {
	if intent.Envelope.UsageEvent.ProfileID <= 0 {
		return fmt.Errorf("profile_id_required")
	}
	if intent.Envelope.UsageEvent.IngressRequestID == "" {
		return fmt.Errorf("ingress_request_id_required")
	}
	if len(intent.Envelope.RequestLogs) == 0 {
		return fmt.Errorf("request_logs_required")
	}
	return nil
}

func (intent RuntimeActivityIntent) clone() RuntimeActivityIntent {
	cloned := intent
	cloned.Envelope.RequestLogs = append([]requestLogInsert(nil), intent.Envelope.RequestLogs...)
	cloned.Envelope.AuditLogs = append([]auditLogInsert(nil), intent.Envelope.AuditLogs...)
	cloned.Envelope.AccountingAttempts = append([]gatewayaccounting.Event(nil), intent.Envelope.AccountingAttempts...)
	return cloned
}

func normalizeRuntimeSideEffectOptions(options RuntimeSideEffectOptions) RuntimeSideEffectOptions {
	if options.QueueCapacity <= 0 {
		options.QueueCapacity = defaultRuntimeSideEffectQueueCapacity
	}
	if options.WorkerCount <= 0 {
		options.WorkerCount = defaultRuntimeSideEffectWorkerCount
	}
	if options.AttemptTimeout <= 0 {
		options.AttemptTimeout = defaultRuntimeSideEffectAttemptTimeout
	}
	if options.ShutdownTimeout <= 0 {
		options.ShutdownTimeout = defaultRuntimeSideEffectShutdownTimeout
	}
	if options.RetryDelay <= 0 {
		options.RetryDelay = defaultRuntimeSideEffectRetryDelay
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = defaultRuntimeSideEffectMaxAttempts
	}
	return options
}
