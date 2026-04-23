package realtime

import (
	"context"
	"sync"
	"time"
)

type DashboardPublishTarget interface {
	PublishLatestDashboardUpdate(context.Context, int) (int, bool, error)
	RecordLatestDashboardRequestLog(int, int)
	HasDashboardSubscribers(int) bool
}

type AsyncDashboardPublisherOptions struct {
	QueueCapacity   int
	WorkerCount     int
	PublishTimeout  time.Duration
	ShutdownTimeout time.Duration
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
	queue              chan int
	publishTimeout     time.Duration
	shutdownTimeout    time.Duration
	maxTrackedProfiles int
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan struct{}
	wg                 sync.WaitGroup
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
	requestLogID int
	queued       bool
	inflight     bool
}

func NewAsyncDashboardPublisher(target DashboardPublishTarget, options AsyncDashboardPublisherOptions) *AsyncDashboardPublisher {
	if target == nil {
		return nil
	}
	normalized := normalizeAsyncDashboardPublisherOptions(options)
	ctx, cancel := context.WithCancel(context.Background())
	publisher := &AsyncDashboardPublisher{
		target:             target,
		queue:              make(chan int, normalized.QueueCapacity),
		publishTimeout:     normalized.PublishTimeout,
		shutdownTimeout:    normalized.ShutdownTimeout,
		maxTrackedProfiles: normalized.QueueCapacity + normalized.WorkerCount,
		ctx:                ctx,
		cancel:             cancel,
		done:               make(chan struct{}),
		states:             map[int]*asyncDashboardProfileState{},
	}
	publisher.wg.Add(normalized.WorkerCount)
	for worker := 0; worker < normalized.WorkerCount; worker++ {
		go publisher.worker()
	}
	go func() {
		publisher.wg.Wait()
		close(publisher.done)
	}()
	return publisher
}

func (p *AsyncDashboardPublisher) PublishDashboardUpdate(_ context.Context, requestLogID int, profileID int) (bool, error) {
	if p == nil || p.target == nil || profileID <= 0 || requestLogID <= 0 {
		return false, nil
	}
	p.target.RecordLatestDashboardRequestLog(profileID, requestLogID)
	if !p.target.HasDashboardSubscribers(profileID) {
		return false, nil
	}
	return p.enqueue(profileID, requestLogID), nil
}

func (p *AsyncDashboardPublisher) PublishPendingDashboardUpdate(ctx context.Context, profileID int) (bool, error) {
	if p == nil || p.target == nil || profileID <= 0 {
		return false, nil
	}
	if p.hasPending(profileID) {
		return false, nil
	}
	_, delivered, err := p.target.PublishLatestDashboardUpdate(ctx, profileID)
	return delivered, err
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
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(p.shutdownTimeout):
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

func (p *AsyncDashboardPublisher) enqueue(profileID int, requestLogID int) bool {
	now := time.Now()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return false
	}
	if state, ok := p.states[profileID]; ok {
		state.requestLogID = requestLogID
		p.coalescedCount++
		p.mu.Unlock()
		return true
	}
	if len(p.states) >= p.maxTrackedProfiles {
		p.droppedCount++
		p.mu.Unlock()
		return false
	}
	state := &asyncDashboardProfileState{requestLogID: requestLogID, queued: true}
	p.states[profileID] = state
	p.acceptedCount++
	p.markBusyLocked(now)
	p.mu.Unlock()

	select {
	case p.queue <- profileID:
		return true
	default:
		p.mu.Lock()
		if current, ok := p.states[profileID]; ok && current == state {
			delete(p.states, profileID)
			p.droppedCount++
			p.markDrainedLocked(time.Now())
		}
		p.mu.Unlock()
		return false
	}
}

func (p *AsyncDashboardPublisher) hasPending(profileID int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.states[profileID]
	return ok
}

func (p *AsyncDashboardPublisher) worker() {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			return
		case profileID := <-p.queue:
			requestedRequestLogID, ok := p.beginPublish(profileID)
			if !ok {
				continue
			}
			publishCtx, cancel := context.WithTimeout(p.ctx, p.publishTimeout)
			publishedRequestLogID, _, _ := p.target.PublishLatestDashboardUpdate(publishCtx, profileID)
			cancel()
			if publishedRequestLogID <= 0 {
				publishedRequestLogID = requestedRequestLogID
			}
			p.finishPublish(profileID, publishedRequestLogID)
		}
	}
}

func (p *AsyncDashboardPublisher) beginPublish(profileID int) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.states[profileID]
	if !ok || !state.queued {
		return 0, false
	}
	state.queued = false
	state.inflight = true
	return state.requestLogID, true
}

func (p *AsyncDashboardPublisher) finishPublish(profileID int, publishedRequestLogID int) {
	now := time.Now()
	p.mu.Lock()
	state, ok := p.states[profileID]
	if !ok {
		p.mu.Unlock()
		return
	}
	state.inflight = false
	if p.closed || state.requestLogID == publishedRequestLogID {
		delete(p.states, profileID)
		p.markDrainedLocked(now)
		p.mu.Unlock()
		return
	}
	state.queued = true
	p.mu.Unlock()

	select {
	case p.queue <- profileID:
	default:
		p.mu.Lock()
		if current, ok := p.states[profileID]; ok && current == state {
			delete(p.states, profileID)
			p.droppedCount++
			p.markDrainedLocked(time.Now())
		}
		p.mu.Unlock()
	}
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
