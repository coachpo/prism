package background

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/priority"
)

func TestSchedulerBudgetsAndPriorityDispatch(t *testing.T) {
	scheduler := NewScheduler(Config{GlobalConcurrency: 1, PriorityConcurrency: map[PriorityClass]int{PriorityHighBackground: 1, PriorityNormalBackground: 1, PriorityLowBackground: 1}})
	started := make(chan string, 3)
	release := make(chan struct{})
	handler := func(_ context.Context, job Job) JobResult {
		started <- string(job.Worker)
		<-release
		return JobResult{Status: JobSucceeded}
	}
	for _, spec := range []WorkerSpec{
		{Name: "low", Priority: PriorityLowBackground, MaxPriority: PriorityLowBackground, QueueLimit: 8, ConcurrencyLimit: 1, DrainPolicy: DrainFlush, CoalescePolicy: CoalesceNone},
		{Name: "high", Priority: PriorityHighBackground, MaxPriority: PriorityHighBackground, QueueLimit: 8, ConcurrencyLimit: 1, DrainPolicy: DrainFlush, CoalescePolicy: CoalesceNone},
	} {
		if err := scheduler.Register(spec, handler); err != nil {
			t.Fatalf("register %s: %v", spec.Name, err)
		}
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "low"}); got.Status != SubmitAccepted {
		t.Fatalf("submit low status = %s", got.Status)
	}
	if first := <-started; first != "low" {
		t.Fatalf("first job = %s", first)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "low"}); got.Status != SubmitAccepted {
		t.Fatalf("queue second low status = %s", got.Status)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "high"}); got.Status != SubmitAccepted {
		t.Fatalf("queue high status = %s", got.Status)
	}
	release <- struct{}{}
	if second := <-started; second != "high" {
		t.Fatalf("second job = %s, want high priority dispatch before queued low", second)
	}
	close(release)
	_ = scheduler.Stop(context.Background(), time.Now().Add(time.Second))
}

func TestSchedulerCoalescesRejectsAndDrains(t *testing.T) {
	scheduler := NewScheduler(Config{GlobalConcurrency: 1, GlobalQueueLimit: 2})
	seen := make(chan int, 2)
	release := make(chan struct{})
	if err := scheduler.Register(WorkerSpec{Name: "coalesce", Priority: PriorityNormalBackground, MaxPriority: PriorityNormalBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: DrainFlush, CoalescePolicy: CoalesceLatestWins}, func(_ context.Context, job Job) JobResult {
		seen <- job.Payload.(int)
		<-release
		return JobResult{Status: JobSucceeded}
	}); err != nil {
		t.Fatalf("register coalesce: %v", err)
	}
	if err := scheduler.Register(WorkerSpec{Name: "reject", Priority: PriorityNormalBackground, MaxPriority: PriorityNormalBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: DrainCancel, CoalescePolicy: CoalesceNone}, func(context.Context, Job) JobResult {
		return JobResult{Status: JobSucceeded}
	}); err != nil {
		t.Fatalf("register reject: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "coalesce", Payload: 1, CoalesceKey: "key"}); got.Status != SubmitAccepted {
		t.Fatalf("first submit = %s", got.Status)
	}
	if first := <-seen; first != 1 {
		t.Fatalf("running payload = %d", first)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "coalesce", Payload: 2, CoalesceKey: "key"}); got.Status != SubmitAccepted {
		t.Fatalf("queued submit = %s", got.Status)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "coalesce", Payload: 3, CoalesceKey: "key"}); got.Status != SubmitCoalesced {
		t.Fatalf("coalesced submit = %s", got.Status)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "coalesce", Payload: 4, CoalesceKey: "other"}); got.Status != SubmitRejectedBackpressure {
		t.Fatalf("expected worker queue backpressure while coalesced queue is full, got %s", got.Status)
	}
	release <- struct{}{}
	if second := <-seen; second != 3 {
		t.Fatalf("coalesced payload = %d, want latest payload", second)
	}
	close(release)
	result := scheduler.Drain(context.Background(), time.Now().Add(time.Second))
	if result.Completed < 2 || result.TimedOut {
		t.Fatalf("unexpected drain result: %+v", result)
	}
	_ = scheduler.Stop(context.Background(), time.Now().Add(time.Second))
}

func TestSchedulerAttachesBackgroundMetadata(t *testing.T) {
	scheduler := NewScheduler(Config{})
	var mu sync.Mutex
	var observed string
	if err := scheduler.Register(WorkerSpec{Name: "metadata", Priority: PriorityLowBackground, MaxPriority: PriorityLowBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: DrainFlush, CoalescePolicy: CoalesceNone}, func(ctx context.Context, _ Job) JobResult {
		metadata, ok := priority.MetadataFromContext(ctx)
		if ok {
			mu.Lock()
			observed = string(metadata.Priority) + ":" + string(metadata.BackgroundSubclass)
			mu.Unlock()
		}
		return JobResult{Status: JobSucceeded}
	}); err != nil {
		t.Fatalf("register metadata worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "metadata"}); got.Status != SubmitAccepted {
		t.Fatalf("submit metadata status = %s", got.Status)
	}
	if result := scheduler.Drain(context.Background(), time.Now().Add(time.Second)); result.TimedOut {
		t.Fatalf("drain timed out: %+v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if observed != "background:low_background" {
		t.Fatalf("metadata = %q", observed)
	}
}

func TestSchedulerRetriesRetryableFailureUntilSuccess(t *testing.T) {
	scheduler := NewScheduler(Config{GlobalConcurrency: 1})
	attempts := make(chan int, 2)
	if err := scheduler.Register(WorkerSpec{Name: "retry-success", Priority: PriorityNormalBackground, MaxPriority: PriorityNormalBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: DrainFlush, CoalescePolicy: CoalesceNone, RetryPolicy: &RetryPolicy{MaxAttempts: 2}}, func(_ context.Context, job Job) JobResult {
		attempts <- job.Attempt
		if job.Attempt == 1 {
			return JobResult{Status: JobFailed, Err: context.Canceled, Retry: true}
		}
		if job.Worker != "retry-success" || job.Payload.(string) != "payload" || job.Priority != PriorityNormalBackground || job.IdempotencyKey != "idempotent" || job.CoalesceKey != "retry-key" {
			t.Fatalf("retry did not preserve job fields: %+v", job)
		}
		return JobResult{Status: JobSucceeded}
	}); err != nil {
		t.Fatalf("register retry worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "retry-success", Payload: "payload", IdempotencyKey: "idempotent", CoalesceKey: "retry-key"}); got.Status != SubmitAccepted {
		t.Fatalf("submit retry worker status = %s", got.Status)
	}
	if first := <-attempts; first != 1 {
		t.Fatalf("first attempt = %d", first)
	}
	if result := scheduler.Drain(context.Background(), time.Now().Add(time.Second)); result.Completed != 1 || result.Failed != 0 || result.TimedOut {
		t.Fatalf("unexpected drain result: %+v", result)
	}
	got := []int{1, <-attempts}
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("attempts = %+v", got)
	}
}

func TestSchedulerStopsRetryingAtMaxAttempts(t *testing.T) {
	scheduler := NewScheduler(Config{GlobalConcurrency: 1})
	attempts := make(chan int, 4)
	if err := scheduler.Register(WorkerSpec{Name: "retry-exhaust", Priority: PriorityLowBackground, MaxPriority: PriorityLowBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: DrainFlush, CoalescePolicy: CoalesceNone, RetryPolicy: &RetryPolicy{MaxAttempts: 3}}, func(_ context.Context, job Job) JobResult {
		attempts <- job.Attempt
		return JobResult{Status: JobFailed, Err: context.Canceled, Retry: true}
	}); err != nil {
		t.Fatalf("register retry worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "retry-exhaust"}); got.Status != SubmitAccepted {
		t.Fatalf("submit retry worker status = %s", got.Status)
	}
	if first := <-attempts; first != 1 {
		t.Fatalf("first attempt = %d", first)
	}
	if result := scheduler.Drain(context.Background(), time.Now().Add(time.Second)); result.Completed != 0 || result.Failed != 1 || result.TimedOut {
		t.Fatalf("unexpected drain result: %+v", result)
	}
	got := []int{1, <-attempts, <-attempts}
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("attempts = %+v", got)
	}
	select {
	case extra := <-attempts:
		t.Fatalf("unexpected extra retry attempt %d", extra)
	default:
	}
}

func TestSchedulerHonorsRetryDelay(t *testing.T) {
	scheduler := NewScheduler(Config{GlobalConcurrency: 1})
	attempts := make(chan int, 2)
	if err := scheduler.Register(WorkerSpec{Name: "retry-delay", Priority: PriorityNormalBackground, MaxPriority: PriorityNormalBackground, QueueLimit: 1, ConcurrencyLimit: 1, DrainPolicy: DrainFlush, CoalescePolicy: CoalesceNone, RetryPolicy: &RetryPolicy{MaxAttempts: 2, Delay: 40 * time.Millisecond}}, func(_ context.Context, job Job) JobResult {
		attempts <- job.Attempt
		if job.Attempt == 1 {
			return JobResult{Status: JobFailed, Err: context.Canceled, Retry: true}
		}
		return JobResult{Status: JobSucceeded}
	}); err != nil {
		t.Fatalf("register retry worker: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatalf("start scheduler: %v", err)
	}
	if got := scheduler.Submit(context.Background(), JobRequest{Worker: "retry-delay"}); got.Status != SubmitAccepted {
		t.Fatalf("submit retry worker status = %s", got.Status)
	}
	if first := <-attempts; first != 1 {
		t.Fatalf("first attempt = %d", first)
	}
	select {
	case attempt := <-attempts:
		t.Fatalf("retry attempt %d ran before configured delay", attempt)
	case <-time.After(10 * time.Millisecond):
	}
	if result := scheduler.Drain(context.Background(), time.Now().Add(time.Second)); result.Completed != 1 || result.Failed != 0 || result.TimedOut {
		t.Fatalf("drain should honor delayed retry before completion, got %+v", result)
	}
	if second := <-attempts; second != 2 {
		t.Fatalf("second attempt = %d", second)
	}
}
