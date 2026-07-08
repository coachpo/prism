package realtime

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/background"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type AnalyticsPublishTarget interface {
	PublishLatestAnalyticsSnapshot(context.Context, int, string) (bool, error)
	ActiveAnalyticsScopes(int) []string
}

type AsyncAnalyticsPublisherOptions struct {
	QueueCapacity   int
	WorkerCount     int
	PublishTimeout  time.Duration
	ShutdownTimeout time.Duration
	Scheduler       *background.Scheduler
}

type AsyncAnalyticsPublisherSnapshot struct {
	QueueDepth        int
	InflightScopes    int
	TrackedScopes     int
	AcceptedCount     uint64
	CoalescedCount    uint64
	DroppedCount      uint64
	Drained           bool
	BusySince         time.Time
	LastDrainedAt     time.Time
	LastDrainDuration time.Duration
}

type AsyncAnalyticsPublisher struct {
	target            AnalyticsPublishTarget
	publishTimeout    time.Duration
	shutdownTimeout   time.Duration
	maxTrackedScopes  int
	scheduler         *background.Scheduler
	ownsScheduler     bool
	mu                sync.Mutex
	states            map[analyticsPublishKey]*asyncAnalyticsScopeState
	closed            bool
	acceptedCount     uint64
	coalescedCount    uint64
	droppedCount      uint64
	busySince         time.Time
	lastDrainedAt     time.Time
	lastDrainDuration time.Duration
}

type analyticsPublishKey struct {
	ProfileID int
	Preset    string
}

type asyncAnalyticsScopeState struct {
	queued   bool
	inflight bool
	dirty    bool
}

func NewAsyncAnalyticsPublisher(target AnalyticsPublishTarget, options AsyncAnalyticsPublisherOptions) *AsyncAnalyticsPublisher {
	if target == nil {
		return nil
	}
	normalized := normalizeAsyncAnalyticsPublisherOptions(options)
	publisher := &AsyncAnalyticsPublisher{
		target:           target,
		publishTimeout:   normalized.PublishTimeout,
		shutdownTimeout:  normalized.ShutdownTimeout,
		maxTrackedScopes: normalized.QueueCapacity + normalized.WorkerCount,
		scheduler:        normalized.Scheduler,
		states:           map[analyticsPublishKey]*asyncAnalyticsScopeState{},
	}
	if publisher.scheduler == nil {
		publisher.scheduler = background.NewScheduler(background.Config{})
		publisher.ownsScheduler = true
		_ = publisher.RegisterBackgroundWorker(publisher.scheduler)
		_ = publisher.scheduler.Start(context.Background())
	}
	return publisher
}

func (p *AsyncAnalyticsPublisher) RegisterBackgroundWorker(scheduler *background.Scheduler) error {
	if p == nil || scheduler == nil {
		return nil
	}
	p.scheduler = scheduler
	return scheduler.Register(background.WorkerSpec{
		Name:             background.WorkerName("async_analytics_publisher"),
		Priority:         background.PriorityHighBackground,
		MaxPriority:      background.PriorityHighBackground,
		QueueLimit:       p.maxTrackedScopes,
		ConcurrencyLimit: 1,
		DrainPolicy:      background.DrainFinishRunning,
		CoalescePolicy:   background.CoalesceLatestWins,
		RetryPolicy:      &background.RetryPolicy{MaxAttempts: 2, Delay: 100 * time.Millisecond},
		Timeout:          p.publishTimeout,
	}, p.handleScheduledPublish)
}

func (p *AsyncAnalyticsPublisher) PublishAnalyticsUpdates(_ context.Context, profileID int) (bool, error) {
	if p == nil || p.target == nil || profileID <= 0 {
		return false, nil
	}
	profileID = profiledomain.DefaultProfileID
	delivered := false
	for _, preset := range p.target.ActiveAnalyticsScopes(profileID) {
		if p.enqueue(analyticsPublishKey{ProfileID: profileID, Preset: normalizeAnalyticsPreset(preset)}) {
			delivered = true
		}
	}
	return delivered, nil
}

func (p *AsyncAnalyticsPublisher) Close() {
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

func (p *AsyncAnalyticsPublisher) Snapshot() AsyncAnalyticsPublisherSnapshot {
	if p == nil {
		return AsyncAnalyticsPublisherSnapshot{Drained: true}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	snapshot := AsyncAnalyticsPublisherSnapshot{
		TrackedScopes:     len(p.states),
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
			snapshot.InflightScopes++
		}
	}
	snapshot.Drained = snapshot.TrackedScopes == 0
	return snapshot
}

func (p *AsyncAnalyticsPublisher) enqueue(key analyticsPublishKey) bool {
	if key.ProfileID <= 0 || key.Preset == "" {
		return false
	}
	now := time.Now()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	if state, ok := p.states[key]; ok {
		if state.inflight {
			state.dirty = true
		}
		p.coalescedCount++
		p.mu.Unlock()
		return true
	}
	if len(p.states) >= p.maxTrackedScopes {
		p.droppedCount++
		p.mu.Unlock()
		return false
	}
	state := &asyncAnalyticsScopeState{queued: true}
	p.states[key] = state
	p.acceptedCount++
	p.markBusyLocked(now)
	p.mu.Unlock()

	if !p.submitPublish(key) {
		p.dropState(key, state)
		return false
	}
	return true
}

func (p *AsyncAnalyticsPublisher) handleScheduledPublish(ctx context.Context, job background.Job) background.JobResult {
	key, ok := job.Payload.(analyticsPublishKey)
	if !ok {
		return background.JobResult{Status: background.JobSucceeded}
	}
	if !p.beginPublish(key) {
		return background.JobResult{Status: background.JobSucceeded}
	}
	publishCtx, cancel := context.WithTimeout(ctx, p.publishTimeout)
	_, err := p.target.PublishLatestAnalyticsSnapshot(publishCtx, key.ProfileID, key.Preset)
	cancel()
	p.finishPublish(key)
	if err != nil {
		return background.JobResult{Status: background.JobFailed, Err: err, Retry: true}
	}
	return background.JobResult{Status: background.JobSucceeded}
}

func (p *AsyncAnalyticsPublisher) beginPublish(key analyticsPublishKey) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.states[key]
	if !ok || !state.queued {
		return false
	}
	state.queued = false
	state.inflight = true
	return true
}

func (p *AsyncAnalyticsPublisher) finishPublish(key analyticsPublishKey) {
	now := time.Now()
	p.mu.Lock()
	state, ok := p.states[key]
	if !ok {
		p.mu.Unlock()
		return
	}
	state.inflight = false
	if state.dirty && !p.closed {
		state.dirty = false
		state.queued = true
		p.mu.Unlock()
		if !p.submitPublish(key) {
			p.dropState(key, state)
		}
		return
	}
	delete(p.states, key)
	p.markDrainedLocked(now)
	p.mu.Unlock()
}

func (p *AsyncAnalyticsPublisher) submitPublish(key analyticsPublishKey) bool {
	if p.scheduler == nil {
		return false
	}
	result := p.scheduler.Submit(context.Background(), background.JobRequest{Worker: background.WorkerName("async_analytics_publisher"), Payload: key, CoalesceKey: analyticsCoalesceKey(key)})
	return result.Status == background.SubmitAccepted || result.Status == background.SubmitCoalesced
}

func (p *AsyncAnalyticsPublisher) dropState(key analyticsPublishKey, state *asyncAnalyticsScopeState) {
	p.mu.Lock()
	if current, ok := p.states[key]; ok && current == state {
		delete(p.states, key)
		p.droppedCount++
		p.markDrainedLocked(time.Now())
	}
	p.mu.Unlock()
}

func analyticsCoalesceKey(key analyticsPublishKey) string {
	return "analytics-profile:" + strconv.Itoa(key.ProfileID) + ":preset:" + key.Preset
}

func (p *AsyncAnalyticsPublisher) markBusyLocked(now time.Time) {
	if p.busySince.IsZero() {
		p.busySince = now
	}
}

func (p *AsyncAnalyticsPublisher) markDrainedLocked(now time.Time) {
	if len(p.states) != 0 || p.busySince.IsZero() {
		return
	}
	p.lastDrainDuration = now.Sub(p.busySince)
	p.lastDrainedAt = now
	p.busySince = time.Time{}
}

func normalizeAsyncAnalyticsPublisherOptions(options AsyncAnalyticsPublisherOptions) AsyncAnalyticsPublisherOptions {
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
