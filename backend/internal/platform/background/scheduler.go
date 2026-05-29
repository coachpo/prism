package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/asyncmetrics"
	"github.com/coachpo/prism/backend/internal/platform/priority"
)

type WorkerName string
type JobID string
type PriorityClass string
type DrainPolicy string
type CoalescePolicy string
type JobStatus string
type SubmitStatus string

const (
	PriorityHighBackground   PriorityClass = PriorityClass(priority.BackgroundSubclassHigh)
	PriorityNormalBackground PriorityClass = PriorityClass(priority.BackgroundSubclassNormal)
	PriorityLowBackground    PriorityClass = PriorityClass(priority.BackgroundSubclassLow)

	DrainFlush         DrainPolicy = "flush"
	DrainFinishRunning DrainPolicy = "finish_running"
	DrainCancel        DrainPolicy = "cancel"
	DrainBestEffort    DrainPolicy = "best_effort"

	CoalesceNone       CoalescePolicy = "none"
	CoalesceLatestWins CoalescePolicy = "latest_wins"
	CoalesceMerge      CoalescePolicy = "merge"
	CoalesceDropNew    CoalescePolicy = "drop_new"
	CoalesceDropOldest CoalescePolicy = "drop_oldest"

	SubmitAccepted                SubmitStatus = "accepted"
	SubmitCoalesced               SubmitStatus = "coalesced"
	SubmitRejectedBackpressure    SubmitStatus = "rejected_backpressure"
	SubmitRejectedStopping        SubmitStatus = "rejected_stopping"
	SubmitRejectedUnknownWorker   SubmitStatus = "rejected_unknown_worker"
	SubmitRejectedInvalidPriority SubmitStatus = "rejected_invalid_priority"

	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

type Config struct {
	GlobalConcurrency   int
	GlobalQueueLimit    int
	PriorityConcurrency map[PriorityClass]int
	PriorityQueueLimit  map[PriorityClass]int
}

type WorkerSpec struct {
	Name             WorkerName
	Priority         PriorityClass
	MaxPriority      PriorityClass
	QueueLimit       int
	ConcurrencyLimit int
	DrainPolicy      DrainPolicy
	CoalescePolicy   CoalescePolicy
	RetryPolicy      *RetryPolicy
	PeriodicTrigger  *PeriodicTrigger
	Timeout          time.Duration
	Merge            func(existing any, incoming any) any
}

type RetryPolicy struct {
	MaxAttempts int
	Delay       time.Duration
}

type PeriodicTrigger struct {
	Interval     time.Duration
	InitialDelay time.Duration
}

type JobRequest struct {
	Worker           WorkerName
	Payload          any
	IdempotencyKey   string
	CoalesceKey      string
	PriorityOverride PriorityClass
	Deadline         time.Time
}

type SubmitResult struct {
	Status SubmitStatus
	JobID  JobID
	Reason string
}

type Job struct {
	ID             JobID
	Worker         WorkerName
	Payload        any
	Priority       PriorityClass
	Attempt        int
	EnqueuedAt     time.Time
	StartedAt      time.Time
	Deadline       time.Time
	IdempotencyKey string
	CoalesceKey    string
}

type JobResult struct {
	Status JobStatus
	Err    error
	Retry  bool
}

type Handler func(context.Context, Job) JobResult

type DrainResult struct {
	Completed    int
	Failed       int
	Cancelled    int
	Dropped      int
	TimedOut     bool
	StillRunning int
}

type Scheduler struct {
	mu       sync.Mutex
	workers  map[WorkerName]*workerState
	state    string
	nextID   uint64
	config   Config
	periodic context.CancelFunc
	retryCtx context.Context
	changed  chan struct{}
	running  int
	prioRun  map[PriorityClass]int
	prioQ    map[PriorityClass]int
	queued   int
	result   DrainResult
}

type workerState struct {
	spec     WorkerSpec
	handler  Handler
	queue    []*queuedJob
	coalesce map[string]*queuedJob
	running  int
}

type queuedJob struct {
	job     Job
	readyAt time.Time
}

func NewScheduler(config Config) *Scheduler {
	config = normalizeConfig(config)
	return &Scheduler{workers: map[WorkerName]*workerState{}, state: "initialized", config: config, changed: make(chan struct{}, 1), prioRun: map[PriorityClass]int{}, prioQ: map[PriorityClass]int{}}
}

func (s *Scheduler) Register(spec WorkerSpec, handler Handler) error {
	if s == nil {
		return errors.New("background scheduler is nil")
	}
	if handler == nil {
		return fmt.Errorf("background worker %q handler is required", spec.Name)
	}
	if err := validateSpec(spec); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != "initialized" {
		return fmt.Errorf("background worker %q registration after start", spec.Name)
	}
	if _, exists := s.workers[spec.Name]; exists {
		return fmt.Errorf("background worker %q already registered", spec.Name)
	}
	s.workers[spec.Name] = &workerState{spec: normalizeWorkerSpec(spec), handler: handler, coalesce: map[string]*queuedJob{}}
	return nil
}

func (s *Scheduler) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.state == "running" {
		s.mu.Unlock()
		return nil
	}
	if s.state != "initialized" {
		s.mu.Unlock()
		return fmt.Errorf("background scheduler cannot start from %s", s.state)
	}
	periodicCtx, cancel := context.WithCancel(ctx)
	s.periodic = cancel
	s.retryCtx = periodicCtx
	s.state = "running"
	periodicSpecs := make([]WorkerSpec, 0, len(s.workers))
	for _, worker := range s.workers {
		if worker.spec.PeriodicTrigger != nil {
			periodicSpecs = append(periodicSpecs, worker.spec)
		}
	}
	s.mu.Unlock()
	for _, spec := range periodicSpecs {
		s.startPeriodic(periodicCtx, spec)
	}
	s.notify()
	return nil
}

func (s *Scheduler) Submit(ctx context.Context, req JobRequest) (result SubmitResult) {
	if ctx == nil {
		ctx = context.Background()
	}
	workerName := string(req.Worker)
	defer func() {
		asyncmetrics.RecordOutcome(ctx, "background_scheduler", workerName, schedulerSubmitOutcome(result.Status))
	}()
	if s == nil {
		return SubmitResult{Status: SubmitRejectedUnknownWorker, Reason: "scheduler unavailable"}
	}
	select {
	case <-ctx.Done():
		return SubmitResult{Status: SubmitRejectedBackpressure, Reason: ctx.Err().Error()}
	default:
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == "stopping" || s.state == "draining" || s.state == "stopped" {
		return SubmitResult{Status: SubmitRejectedStopping, Reason: "scheduler stopping"}
	}
	worker, ok := s.workers[req.Worker]
	if !ok {
		return SubmitResult{Status: SubmitRejectedUnknownWorker, Reason: "unknown worker"}
	}
	jobPriority := worker.spec.Priority
	if req.PriorityOverride != "" {
		if comparePriority(req.PriorityOverride, worker.spec.MaxPriority) > 0 {
			return SubmitResult{Status: SubmitRejectedInvalidPriority, Reason: "priority override exceeds worker maximum"}
		}
		jobPriority = req.PriorityOverride
	}
	if req.CoalesceKey != "" && worker.spec.CoalescePolicy != CoalesceNone {
		if existing := worker.coalesce[req.CoalesceKey]; existing != nil {
			s.applyCoalescing(worker, existing, req)
			asyncmetrics.RecordQueueDepth(ctx, "background_scheduler", workerName, int64(len(worker.queue)))
			return SubmitResult{Status: SubmitCoalesced, JobID: existing.job.ID}
		}
	}
	if worker.spec.QueueLimit > 0 && len(worker.queue) >= worker.spec.QueueLimit {
		if worker.spec.CoalescePolicy != CoalesceDropOldest {
			return SubmitResult{Status: SubmitRejectedBackpressure, Reason: "worker queue limit reached"}
		}
		s.dropOldestLocked(worker)
	}
	if s.config.GlobalQueueLimit > 0 && s.queued >= s.config.GlobalQueueLimit {
		return SubmitResult{Status: SubmitRejectedBackpressure, Reason: "global queue limit reached"}
	}
	if limit := s.config.PriorityQueueLimit[jobPriority]; limit > 0 && s.prioQ[jobPriority] >= limit {
		return SubmitResult{Status: SubmitRejectedBackpressure, Reason: "priority queue limit reached"}
	}
	s.nextID++
	job := Job{ID: JobID(fmt.Sprintf("background-%d", s.nextID)), Worker: req.Worker, Payload: req.Payload, Priority: jobPriority, Attempt: 1, EnqueuedAt: now, Deadline: req.Deadline, IdempotencyKey: req.IdempotencyKey, CoalesceKey: req.CoalesceKey}
	queued := &queuedJob{job: job, readyAt: now}
	worker.queue = append(worker.queue, queued)
	if req.CoalesceKey != "" && worker.spec.CoalescePolicy != CoalesceNone {
		worker.coalesce[req.CoalesceKey] = queued
	}
	s.queued++
	s.prioQ[jobPriority]++
	asyncmetrics.RecordQueueDepth(ctx, "background_scheduler", workerName, int64(len(worker.queue)))
	s.dispatchLocked()
	return SubmitResult{Status: SubmitAccepted, JobID: job.ID}
}

func (s *Scheduler) Drain(ctx context.Context, deadline time.Time) DrainResult {
	if s == nil {
		return DrainResult{}
	}
	s.mu.Lock()
	if s.state != "stopped" {
		s.state = "draining"
	}
	if s.periodic != nil {
		s.periodic()
		s.periodic = nil
	}
	for _, worker := range s.workers {
		s.applyDrainPolicyLocked(worker)
	}
	s.dispatchLocked()
	s.mu.Unlock()
	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			s.mu.Lock()
			s.result.TimedOut = true
			s.result.StillRunning = s.running
			result := s.result
			s.mu.Unlock()
			return result
		}
		s.mu.Lock()
		s.dispatchLocked()
		done := s.running == 0 && s.queued == 0
		result := s.result
		s.mu.Unlock()
		if done {
			return result
		}
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.result.TimedOut = true
			s.result.StillRunning = s.running
			result = s.result
			s.mu.Unlock()
			return result
		case <-s.changed:
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (s *Scheduler) Stop(ctx context.Context, deadline time.Time) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.state == "stopped" {
		s.mu.Unlock()
		return nil
	}
	s.state = "stopping"
	if s.periodic != nil {
		s.periodic()
		s.periodic = nil
	}
	s.mu.Unlock()
	result := s.Drain(ctx, deadline)
	s.mu.Lock()
	s.state = "stopped"
	s.mu.Unlock()
	if result.TimedOut {
		return fmt.Errorf("background scheduler drain timed out with %d running", result.StillRunning)
	}
	return nil
}

func (s *Scheduler) RegisteredWorkers() []WorkerName {
	s.mu.Lock()
	defer s.mu.Unlock()
	workers := make([]WorkerName, 0, len(s.workers))
	for name := range s.workers {
		workers = append(workers, name)
	}
	return workers
}

func (s *Scheduler) dispatchLocked() {
	if s.state != "running" && s.state != "draining" {
		return
	}
	for s.config.GlobalConcurrency <= 0 || s.running < s.config.GlobalConcurrency {
		started := false
		for _, priorityClass := range []PriorityClass{PriorityHighBackground, PriorityNormalBackground, PriorityLowBackground} {
			if limit := s.config.PriorityConcurrency[priorityClass]; limit > 0 && s.prioRun[priorityClass] >= limit {
				continue
			}
			for _, worker := range s.workers {
				if worker.spec.Priority != priorityClass || len(worker.queue) == 0 {
					continue
				}
				if worker.spec.ConcurrencyLimit > 0 && worker.running >= worker.spec.ConcurrencyLimit {
					continue
				}
				jobIndex := worker.readyJobIndex(time.Now().UTC())
				if jobIndex < 0 {
					continue
				}
				s.startJobLocked(worker, jobIndex)
				started = true
				break
			}
			if started {
				break
			}
		}
		if !started {
			return
		}
	}
}

func (s *Scheduler) startJobLocked(worker *workerState, jobIndex int) {
	queued := worker.queue[jobIndex]
	worker.queue = append(worker.queue[:jobIndex], worker.queue[jobIndex+1:]...)
	delete(worker.coalesce, queued.job.CoalesceKey)
	s.queued--
	s.prioQ[queued.job.Priority]--
	s.running++
	s.prioRun[queued.job.Priority]++
	worker.running++
	queued.job.StartedAt = time.Now().UTC()
	asyncmetrics.RecordQueueDepth(context.Background(), "background_scheduler", string(queued.job.Worker), int64(len(worker.queue)))
	asyncmetrics.AddInflight(context.Background(), "background_scheduler", string(queued.job.Worker), 1)
	go s.runJob(worker, queued.job)
}

func (s *Scheduler) runJob(worker *workerState, job Job) {
	ctx := priority.WithMetadata(context.Background(), priority.Metadata{Priority: priority.PriorityBackground, BackgroundSubclass: priority.BackgroundSubclass(job.Priority)})
	if !job.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, job.Deadline)
		defer cancel()
	} else if worker.spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, worker.spec.Timeout)
		defer cancel()
	}
	startedAt := job.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	result := worker.handler(ctx, job)
	s.mu.Lock()
	outcome := asyncmetrics.OutcomeSuccess
	if s.shouldRetryLocked(worker, job, result) {
		outcome = asyncmetrics.OutcomeRetryScheduled
		asyncmetrics.RecordRetry(ctx, "background_scheduler", string(job.Worker), outcome)
		s.scheduleRetryLocked(worker, job)
	} else if result.Status == JobFailed || result.Err != nil {
		outcome = asyncmetrics.OutcomeFailure
		if result.Retry {
			asyncmetrics.RecordRetry(ctx, "background_scheduler", string(job.Worker), asyncmetrics.OutcomeRetryExhausted)
		}
		s.result.Failed++
	} else {
		s.result.Completed++
	}
	asyncmetrics.RecordDuration(ctx, "background_scheduler", string(job.Worker), outcome, time.Since(startedAt))
	asyncmetrics.AddInflight(ctx, "background_scheduler", string(job.Worker), -1)
	s.running--
	s.prioRun[job.Priority]--
	worker.running--
	s.dispatchLocked()
	s.mu.Unlock()
	s.notify()
}

func (s *Scheduler) shouldRetryLocked(worker *workerState, job Job, result JobResult) bool {
	if !result.Retry || worker.spec.RetryPolicy == nil || worker.spec.RetryPolicy.MaxAttempts <= 1 {
		return false
	}
	if result.Status != JobFailed && result.Err == nil {
		return false
	}
	if job.Attempt >= worker.spec.RetryPolicy.MaxAttempts {
		return false
	}
	return s.state == "running"
}

func (s *Scheduler) scheduleRetryLocked(worker *workerState, job Job) {
	policy := *worker.spec.RetryPolicy
	job.Attempt++
	job.StartedAt = time.Time{}
	readyAt := time.Now().UTC().Add(policy.Delay)
	s.enqueueRetryLocked(worker, job, readyAt)
	if policy.Delay > 0 {
		s.wakeAfter(policy.Delay)
	}
}

func (s *Scheduler) enqueueRetryLocked(worker *workerState, job Job, readyAt time.Time) {
	if job.CoalesceKey != "" && worker.spec.CoalescePolicy != CoalesceNone {
		if worker.coalesce[job.CoalesceKey] != nil {
			return
		}
	}
	queued := &queuedJob{job: job, readyAt: readyAt}
	worker.queue = append(worker.queue, queued)
	if job.CoalesceKey != "" && worker.spec.CoalescePolicy != CoalesceNone {
		worker.coalesce[job.CoalesceKey] = queued
	}
	s.queued++
	s.prioQ[job.Priority]++
	asyncmetrics.RecordQueueDepth(context.Background(), "background_scheduler", string(job.Worker), int64(len(worker.queue)))
}

func (s *Scheduler) wakeAfter(delay time.Duration) {
	retryCtx := s.retryCtx
	if retryCtx == nil {
		retryCtx = context.Background()
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			s.mu.Lock()
			s.dispatchLocked()
			s.mu.Unlock()
			s.notify()
		case <-retryCtx.Done():
		}
	}()
}

func (w *workerState) readyJobIndex(now time.Time) int {
	for index, queued := range w.queue {
		if queued.readyAt.IsZero() || !queued.readyAt.After(now) {
			return index
		}
	}
	return -1
}

func (s *Scheduler) startPeriodic(ctx context.Context, spec WorkerSpec) {
	go func() {
		initialDelay := spec.PeriodicTrigger.InitialDelay
		if initialDelay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(initialDelay):
			}
		}
		_ = s.Submit(ctx, JobRequest{Worker: spec.Name, CoalesceKey: string(spec.Name) + ":periodic"})
		ticker := time.NewTicker(spec.PeriodicTrigger.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.Submit(ctx, JobRequest{Worker: spec.Name, CoalesceKey: string(spec.Name) + ":periodic"})
			}
		}
	}()
}

func (s *Scheduler) applyCoalescing(worker *workerState, existing *queuedJob, req JobRequest) {
	switch worker.spec.CoalescePolicy {
	case CoalesceLatestWins:
		existing.job.Payload = req.Payload
	case CoalesceMerge:
		if worker.spec.Merge != nil {
			existing.job.Payload = worker.spec.Merge(existing.job.Payload, req.Payload)
		}
	case CoalesceDropNew:
	case CoalesceDropOldest:
		existing.job.Payload = req.Payload
	}
}

func (s *Scheduler) applyDrainPolicyLocked(worker *workerState) {
	switch worker.spec.DrainPolicy {
	case DrainCancel, DrainFinishRunning:
		s.dropQueuedLocked(worker)
	case DrainBestEffort:
		if len(worker.queue) > worker.spec.ConcurrencyLimit && worker.spec.ConcurrencyLimit > 0 {
			for len(worker.queue) > worker.spec.ConcurrencyLimit {
				s.dropOldestLocked(worker)
			}
		}
	}
}

func (s *Scheduler) dropQueuedLocked(worker *workerState) {
	for len(worker.queue) > 0 {
		s.dropOldestLocked(worker)
	}
}

func (s *Scheduler) dropOldestLocked(worker *workerState) {
	if len(worker.queue) == 0 {
		return
	}
	oldest := worker.queue[0]
	worker.queue = worker.queue[1:]
	delete(worker.coalesce, oldest.job.CoalesceKey)
	s.queued--
	s.prioQ[oldest.job.Priority]--
	s.result.Dropped++
	asyncmetrics.RecordQueueDepth(context.Background(), "background_scheduler", string(oldest.job.Worker), int64(len(worker.queue)))
	asyncmetrics.RecordOutcome(context.Background(), "background_scheduler", string(oldest.job.Worker), asyncmetrics.OutcomeBackpressure)
}

func (s *Scheduler) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func normalizeConfig(config Config) Config {
	if config.GlobalConcurrency <= 0 {
		config.GlobalConcurrency = 4
	}
	if config.GlobalQueueLimit <= 0 {
		config.GlobalQueueLimit = 512
	}
	if config.PriorityConcurrency == nil {
		config.PriorityConcurrency = map[PriorityClass]int{PriorityHighBackground: 2, PriorityNormalBackground: 2, PriorityLowBackground: 1}
	}
	if config.PriorityQueueLimit == nil {
		config.PriorityQueueLimit = map[PriorityClass]int{PriorityHighBackground: 128, PriorityNormalBackground: 256, PriorityLowBackground: 128}
	}
	return config
}

func validateSpec(spec WorkerSpec) error {
	if spec.Name == "" {
		return errors.New("background worker name is required")
	}
	if !validPriority(spec.Priority) {
		return fmt.Errorf("background worker %q invalid priority %q", spec.Name, spec.Priority)
	}
	if spec.MaxPriority != "" && !validPriority(spec.MaxPriority) {
		return fmt.Errorf("background worker %q invalid max priority %q", spec.Name, spec.MaxPriority)
	}
	if spec.DrainPolicy == "" {
		return fmt.Errorf("background worker %q drain policy is required", spec.Name)
	}
	if spec.CoalescePolicy == "" {
		return fmt.Errorf("background worker %q coalesce policy is required", spec.Name)
	}
	if spec.PeriodicTrigger != nil && spec.PeriodicTrigger.Interval <= 0 {
		return fmt.Errorf("background worker %q periodic interval must be positive", spec.Name)
	}
	if spec.RetryPolicy != nil && spec.RetryPolicy.MaxAttempts <= 1 {
		return fmt.Errorf("background worker %q retry max attempts must be greater than one", spec.Name)
	}
	return nil
}

func normalizeWorkerSpec(spec WorkerSpec) WorkerSpec {
	if spec.MaxPriority == "" {
		spec.MaxPriority = spec.Priority
	}
	if spec.QueueLimit <= 0 {
		spec.QueueLimit = 64
	}
	if spec.ConcurrencyLimit <= 0 {
		spec.ConcurrencyLimit = 1
	}
	return spec
}

func validPriority(priorityClass PriorityClass) bool {
	switch priorityClass {
	case PriorityHighBackground, PriorityNormalBackground, PriorityLowBackground:
		return true
	default:
		return false
	}
}

func comparePriority(left PriorityClass, right PriorityClass) int {
	rank := func(priorityClass PriorityClass) int {
		switch priorityClass {
		case PriorityHighBackground:
			return 3
		case PriorityNormalBackground:
			return 2
		case PriorityLowBackground:
			return 1
		default:
			return 0
		}
	}
	return rank(left) - rank(right)
}

func schedulerSubmitOutcome(status SubmitStatus) string {
	switch status {
	case SubmitAccepted:
		return asyncmetrics.OutcomeAccepted
	case SubmitCoalesced:
		return asyncmetrics.OutcomeCoalesced
	case SubmitRejectedBackpressure:
		return asyncmetrics.OutcomeBackpressure
	case SubmitRejectedStopping, SubmitRejectedUnknownWorker, SubmitRejectedInvalidPriority:
		return asyncmetrics.OutcomeRejected
	default:
		return asyncmetrics.OutcomeOther
	}
}
