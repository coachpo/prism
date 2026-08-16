package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/background"
)

func TestRuntimeActivityIntentValidationRejectsIncompleteEvents(t *testing.T) {
	intent := RuntimeActivityIntent{}
	if err := intent.validate(); err == nil {
		t.Fatal("expected incomplete runtime activity intent to fail validation")
	}
}

func TestRuntimeSideEffectsAcceptedIntentIsAccountedForUntilTerminalFailure(t *testing.T) {
	scheduler := background.NewScheduler(background.Config{GlobalConcurrency: 1})
	terminalFailures := make(chan error, 1)
	manager := NewRuntimeSideEffectManager(nil, RuntimeSideEffectOptions{
		QueueCapacity:   1,
		WorkerCount:     1,
		AttemptTimeout:  20 * time.Millisecond,
		ShutdownTimeout: 200 * time.Millisecond,
		RetryDelay:      5 * time.Millisecond,
		MaxAttempts:     2,
		Hooks: &RuntimeSideEffectHooks{TerminalFailure: func(_ RuntimeActivityIntent, err error) {
			terminalFailures <- err
		}},
	})
	if err := manager.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register side-effect worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	defer func() { _ = scheduler.Stop(context.Background(), time.Now().Add(time.Second)) }()

	result := manager.SubmitRuntimeActivity(validRuntimeActivityIntent())
	if result.Status != RuntimeSideEffectAccepted {
		t.Fatalf("expected accepted runtime activity intent, got %+v", result)
	}
	if got := manager.pendingCount(); got != 1 {
		t.Fatalf("expected accepted intent to remain pending before worker terminal accounting, got %d", got)
	}
	select {
	case err := <-terminalFailures:
		if err == nil {
			t.Fatal("expected terminal failure error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal failure accounting")
	}
	waitForPendingRuntimeSideEffects(t, manager, 0, time.Second)
}

func TestRuntimeSideEffectsSubmitIgnoresCanceledRequestContext(t *testing.T) {
	scheduler := background.NewScheduler(background.Config{GlobalConcurrency: 1})
	manager := NewRuntimeSideEffectManager(nil, RuntimeSideEffectOptions{ShutdownTimeout: 20 * time.Millisecond})
	if err := manager.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register side-effect worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	defer func() { _ = scheduler.Stop(context.Background(), time.Now().Add(time.Second)) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := manager.SubmitRuntimeActivityContext(ctx, validRuntimeActivityIntent())
	if result.Status != RuntimeSideEffectAccepted {
		t.Fatalf("expected canceled request context not to reject side-effect submit, got %+v", result)
	}
}

func waitForPendingRuntimeSideEffects(t *testing.T, manager *RuntimeSideEffectManager, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if got := manager.pendingCount(); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for runtime side-effect pending count %d, got %d", want, manager.pendingCount())
}

func TestRuntimeSideEffectOptionsDefaultAttemptTimeout(t *testing.T) {
	if got := normalizeRuntimeSideEffectOptions(RuntimeSideEffectOptions{}).AttemptTimeout; got != 10*time.Second {
		t.Fatalf("expected default runtime side-effect attempt timeout 10s, got %v", got)
	}
}

func TestRuntimeTelemetrySideEffectsRejectAfterShutdown(t *testing.T) {
	scheduler := background.NewScheduler(background.Config{})
	manager := NewRuntimeSideEffectManager(nil, RuntimeSideEffectOptions{ShutdownTimeout: 20 * time.Millisecond})
	if err := manager.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register side-effect worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	manager.Close()
	result := manager.SubmitRuntimeActivity(validRuntimeActivityIntent())
	if result.Status != RuntimeSideEffectRejected {
		t.Fatalf("expected rejected submit after close, got %+v", result)
	}
	_ = scheduler.Stop(context.Background(), time.Now().Add(time.Second))
}

func TestRuntimeFeedbackTryEnqueueDropsInvalidFullAndClosedEvents(t *testing.T) {
	scheduler := background.NewScheduler(background.Config{GlobalConcurrency: 0})
	pipeline := newRuntimeFeedbackPipeline(nil, nil, nil, RuntimeFeedbackPipelineOptions{QueueCapacity: 1, WorkerCount: 1, WriteTimeout: time.Second})
	if err := pipeline.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register feedback worker: %v", err)
	}
	if got := pipeline.TryEnqueue(runtimeFeedbackEvent{}); got.Status != RuntimeFeedbackDroppedInvalid {
		t.Fatalf("expected invalid feedback drop, got %+v", got)
	}
	valid := runtimeFeedbackEvent{Kind: runtimeFeedbackAdmissionRejected, ProfileID: 1, ConnectionID: 2, ObservedAt: time.Now()}
	if got := pipeline.TryEnqueue(valid); got.Status != RuntimeFeedbackAccepted {
		t.Fatalf("expected first feedback event accepted, got %+v", got)
	}
	if got := pipeline.TryEnqueue(valid); got.Status != RuntimeFeedbackDroppedBackpressure {
		t.Fatalf("expected full feedback queue drop, got %+v", got)
	}
	pipeline.Close()
	if got := pipeline.TryEnqueue(valid); got.Status != RuntimeFeedbackDroppedUnavailable {
		t.Fatalf("expected closed feedback pipeline drop, got %+v", got)
	}
}

func TestRuntimeFeedbackEnqueueIgnoresCanceledRequestContext(t *testing.T) {
	scheduler := background.NewScheduler(background.Config{GlobalConcurrency: 1})
	pipeline := newRuntimeFeedbackPipeline(nil, nil, nil, RuntimeFeedbackPipelineOptions{QueueCapacity: 1, WorkerCount: 1, WriteTimeout: time.Second})
	if err := pipeline.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register feedback worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	defer func() { _ = scheduler.Stop(context.Background(), time.Now().Add(time.Second)) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	event := runtimeFeedbackEvent{Kind: runtimeFeedbackAdmissionRejected, ProfileID: 1, ConnectionID: 2, ObservedAt: time.Now()}
	result := pipeline.TryEnqueueContext(ctx, event)
	if result.Status != RuntimeFeedbackAccepted {
		t.Fatalf("expected canceled request context not to reject feedback enqueue, got %+v", result)
	}
}

func TestRuntimeFeedbackWorkerAccountsStoreFailure(t *testing.T) {
	writeResults := make(chan RuntimeFeedbackWriteResult, 1)
	scheduler := background.NewScheduler(background.Config{})
	pipeline := newRuntimeFeedbackPipeline(nil, nil, nil, RuntimeFeedbackPipelineOptions{QueueCapacity: 1, WorkerCount: 1, WriteTimeout: 20 * time.Millisecond, Hooks: &RuntimeFeedbackPipelineHooks{AfterWrite: func(result RuntimeFeedbackWriteResult) {
		writeResults <- result
	}}})
	if err := pipeline.RegisterBackgroundWorker(scheduler); err != nil {
		t.Fatalf("register feedback worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	defer func() { _ = scheduler.Stop(context.Background(), time.Now().Add(time.Second)) }()
	valid := runtimeFeedbackEvent{Kind: runtimeFeedbackAdmissionRejected, ProfileID: 1, ConnectionID: 2, ObservedAt: time.Now()}
	if got := pipeline.TryEnqueue(valid); got.Status != RuntimeFeedbackAccepted {
		t.Fatalf("expected feedback event accepted, got %+v", got)
	}
	select {
	case result := <-writeResults:
		if result.Success || result.Err == nil {
			t.Fatalf("expected store failure accounting, got %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for feedback store failure accounting")
	}
}

func validRuntimeActivityIntent() RuntimeActivityIntent {
	createdAt := time.Now().UTC()
	return RuntimeActivityIntent{Envelope: runtimeTelemetryEnvelope{
		RequestLogs: []requestLogInsert{{ProfileID: 1, ModelID: "model", APIFamily: "openai", EndpointID: intPtr(1), ConnectionID: intPtr(1), IngressRequestID: "request-1", AttemptNumber: 1, EndpointBaseURL: stringPtr("http://upstream"), StatusCode: 200, ResponseTimeMS: 1, SuccessFlag: true, CreatedAt: createdAt}},
		UsageEvent:  usageEventInsert{ProfileID: 1, IngressRequestID: "request-1", ModelID: "model", APIFamily: "openai", EndpointID: intPtr(1), ConnectionID: intPtr(1), StatusCode: 200, SuccessFlag: true, CreatedAt: createdAt},
	}}
}
