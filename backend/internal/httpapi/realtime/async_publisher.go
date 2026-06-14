package realtime

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/background"
)

type DashboardPublishTarget interface {
	PublishLatestDashboardSnapshot(context.Context, int) (bool, error)
	PublishDashboardActivity(context.Context, int, int) (bool, error)
	InvalidateDashboardSnapshot(int)
	HasDashboardSubscribers(int) bool
}

type AsyncDashboardPublisherOptions struct {
	QueueCapacity   int
	WorkerCount     int
	PublishTimeout  time.Duration
	ShutdownTimeout time.Duration
	Scheduler       *background.Scheduler
}

type AsyncDashboardPublisherSnapshot struct {
	QueueDepth        int
	InflightProfiles  int
	TrackedProfiles   int
	AcceptedCount     uint64
	CoalescedCount    uint64
	DroppedCount      uint64
	Drained           bool
	BusySince         time.Time
	LastDrainedAt     time.Time
	LastDrainDuration time.Duration
}

type AsyncDashboardPublisher struct {
	target             DashboardPublishTarget
	publishTimeout     time.Duration
	shutdownTimeout    time.Duration
	maxTrackedProfiles int
	scheduler          *background.Scheduler
	ownsScheduler      bool
	mu                 sync.Mutex
	states             map[int]*asyncDashboardProfileState
	closed             bool
	acceptedCount      uint64
	coalescedCount     uint64
	droppedCount       uint64
	busySince          time.Time
	lastDrainedAt      time.Time
	lastDrainDuration  time.Duration
}

type asyncDashboardProfileState struct {
	queued   bool
	inflight bool
}

func NewAsyncDashboardPublisher(target DashboardPublishTarget, options AsyncDashboardPublisherOptions) *AsyncDashboardPublisher {
	if target == nil {
		return nil
	}
	normalized := normalizeAsyncDashboardPublisherOptions(options)
	publisher := &AsyncDashboardPublisher{
		target:             target,
		publishTimeout:     normalized.PublishTimeout,
		shutdownTimeout:    normalized.ShutdownTimeout,
		maxTrackedProfiles: normalized.QueueCapacity + normalized.WorkerCount,
		scheduler:          normalized.Scheduler,
		states:             map[int]*asyncDashboardProfileState{},
	}
	if publisher.scheduler == nil {
		publisher.scheduler = background.NewScheduler(background.Config{})
		publisher.ownsScheduler = true
		_ = publisher.RegisterBackgroundWorker(publisher.scheduler)
		_ = publisher.scheduler.Start(context.Background())
	}
	return publisher
}

func (p *AsyncDashboardPublisher) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if p == nil || scheduler == nil {
		return nil
	}
	p.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             background.WorkerName("async_dashboard_publisher"),
		Priority:         background.PriorityHighBackground,
		MaxPriority:      background.PriorityHighBackground,
		QueueLimit:       p.maxTrackedProfiles,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainFinishRunning,
		CoalescePolicy:   background.CoalesceLatestWins,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 2, Delay: 100 * time.Millisecond},
		Timeout:          p.publishTimeout,
	}, p.handleScheduledPublish)
}

func (p *AsyncDashboardPublisher) PublishDashboardSnapshot(_ context.Context, profileID int) (bool, error) {
	if p == nil || p.target == nil || profileID <= 0 {
		return false, nil
	}
	p.target.InvalidateDashboardSnapshot(profileID)
	return p.enqueue(profileID), nil
}

func (p *AsyncDashboardPublisher) PublishDashboardActivity(ctx context.Context, requestLogID int, profileID int) (bool, error) {
	if p == nil || p.target == nil || profileID <= 0 || requestLogID <= 0 {
		return false, nil
	}
	return p.target.PublishDashboardActivity(ctx, requestLogID, profileID)
}

func (p *AsyncDashboardPublisher) PublishPendingDashboardSnapshot(ctx context.Context, profileID int) (bool, error) {
	if p == nil || p.target == nil || profileID <= 0 {
		return false, nil
	}
	if p.hasPending(profileID) {
		return false, nil
	}
	return p.target.PublishLatestDashboardSnapshot(ctx, profileID)
}

func (p *AsyncDashboardPublisher) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	if p.ownsScheduler && p.scheduler != nil {
		ctx, cancel := context.WithTimeout(context.Background(), p.shutdownTimeout)
		_ = p.scheduler.Stop(ctx, time.Now().Add(p.shutdownTimeout))
		cancel()
	}
}

func (p *AsyncDashboardPublisher) Snapshot() AsyncDashboardPublisherSnapshot {
	if p == nil {
		return AsyncDashboardPublisherSnapshot{Drained: true}
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	snapshot := AsyncDashboardPublisherSnapshot{
		TrackedProfiles:   len(p.states),
		AcceptedCount:     p.acceptedCount,
		CoalescedCount:    p.coalescedCount,
		DroppedCount:      p.droppedCount,
		BusySince:         p.busySince,
		LastDrainedAt:     p.lastDrainedAt,
		LastDrainDuration: p.lastDrainDuration,
	}
	for _, state := range p.states {
		if state.queued {
			snapshot.QueueDepth++
		}
		if state.inflight {
			snapshot.InflightProfiles++
		}
	}
	snapshot.Drained = snapshot.TrackedProfiles == 0
	return snapshot
}

func (p *AsyncDashboardPublisher) enqueue(profileID int) bool {
	now := time.Now()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	if state, ok := p.states[profileID]; ok {
		state.queued = true
		p.coalescedCount++
		p.mu.Unlock()
		return true
	}
	if len(p.states) >= p.maxTrackedProfiles {
		p.droppedCount++
		p.mu.Unlock()
		return false
	}
	state := &asyncDashboardProfileState{queued: true}
	p.states[profileID] = state
	p.acceptedCount++
	p.markBusyLocked(now)
	p.mu.Unlock()

	if p.scheduler == nil {
		p.dropTrackedState(profileID, state)
		return false
	}
	result := p.scheduler.Submit(context.Background(), background.JobRequest{Worker: background.WorkerName("async_dashboard_publisher"), Payload: profileID, CoalesceKey: dashboardCoalesceKey(profileID)})
	if result.Status == background.SubmitRejectedBackpressure {
		p.dropTrackedState(profileID, state)
		return false
	}
	return result.Status == background.SubmitAccepted || result.Status == background.SubmitCoalesced
}

func (p *AsyncDashboardPublisher) hasPending(profileID int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.states[profileID]
	return ok
}

func (p *AsyncDashboardPublisher) handleScheduledPublish(ctx context.Context, job background.Job) background.JobResult {
	profileID, _ := job.Payload.(int)
	if !p.beginPublish(profileID) {
		return background.JobResult{Status: background.JobSucceeded}
	}
	publishCtx, cancel := context.WithTimeout(ctx, p.publishTimeout)
	_, err := p.target.PublishLatestDashboardSnapshot(publishCtx, profileID)
	cancel()
	p.finishPublish(profileID)
	if err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (p *AsyncDashboardPublisher) beginPublish(profileID int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.states[profileID]
	if !ok || !state.queued {
		return false
	}
	state.queued = false
	state.inflight = true
	return true
}

func (p *AsyncDashboardPublisher) finishPublish(profileID int) {
	now := time.Now()
	p.mu.Lock()
	state, ok := p.states[profileID]
	if !ok {
		p.mu.Unlock()
		return
	}
	state.inflight = false
	if p.closed || !state.queued {
		delete(p.states, profileID)
		p.markDrainedLocked(now)
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	if p.scheduler == nil || p.scheduler.Submit(context.Background(), background.JobRequest{Worker: background.WorkerName("async_dashboard_publisher"), Payload: profileID, CoalesceKey: dashboardCoalesceKey(profileID)}).Status == background.SubmitRejectedBackpressure {
		p.mu.Lock()
		if current, ok := p.states[profileID]; ok && current == state {
			delete(p.states, profileID)
			p.droppedCount++
			p.markDrainedLocked(time.Now())
		}
		p.mu.Unlock()
	}
}

func (p *AsyncDashboardPublisher) dropTrackedState(profileID int, state *asyncDashboardProfileState) {
	p.mu.Lock()
	if current, ok := p.states[profileID]; ok && current == state {
		delete(p.states, profileID)
		p.droppedCount++
		p.markDrainedLocked(time.Now())
	}
	p.mu.Unlock()
}

func dashboardCoalesceKey(profileID int) string {
	return "dashboard-profile:" + strconv.Itoa(profileID)
}

func (p *AsyncDashboardPublisher) markBusyLocked(now time.Time) {
	if p.busySince.IsZero() {
		p.busySince = now
	}
}

func (p *AsyncDashboardPublisher) markDrainedLocked(now time.Time) {
	if len(p.states) != 0 || p.busySince.IsZero() {
		return
	}
	p.lastDrainDuration = now.Sub(p.busySince)
	p.lastDrainedAt = now
	p.busySince = time.Time{}
}
func normalizeAsyncDashboardPublisherOptions(options AsyncDashboardPublisherOptions) AsyncDashboardPublisherOptions {
	normalized := options
	if normalized.QueueCapacity <= 0 {
		normalized.QueueCapacity = defaultAsyncDashboardQueueSize
	}
	if normalized.WorkerCount <= 0 {
		normalized.WorkerCount = defaultAsyncDashboardWorkerCount
	}
	if normalized.PublishTimeout <= 0 {
		normalized.PublishTimeout = defaultAsyncDashboardTimeout
	}
	if normalized.ShutdownTimeout <= 0 {
		normalized.ShutdownTimeout = defaultAsyncDashboardDrainTimeout
	}
	return normalized
}
