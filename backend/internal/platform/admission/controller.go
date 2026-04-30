package admission

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/priority"
)

type Limits struct {
	Proxy               int64
	ManagementM1        int64
	ManagementM2        int64
	ManagementM3        int64
	ManagementM2Reserve int64
	Background          int64
}

type Controller struct {
	proxy               *counterLimiter
	managementM1        *counterLimiter
	managementM2        *counterLimiter
	managementM3        *counterLimiter
	managementM2Reserve int64
	background          *counterLimiter
	retryAfter          time.Duration
	now                 func() time.Time
}

type OverloadError struct {
	WorkloadName string
	Metadata     priority.Metadata
	Resource     string
	RetryAfter   time.Duration
}

type counterLimiter struct {
	mu    sync.Mutex
	max   int64
	inUse int64
}

func NewController(limits Limits) *Controller {
	reserve := limits.ManagementM2Reserve
	if reserve == 0 && limits.ManagementM2 > 1 {
		reserve = 1
	}
	if reserve < 0 {
		reserve = 0
	}
	if limits.ManagementM2 > 0 && reserve >= limits.ManagementM2 {
		reserve = limits.ManagementM2 - 1
	}
	return &Controller{
		proxy:               newCounterLimiter(limits.Proxy),
		managementM1:        newCounterLimiter(limits.ManagementM1),
		managementM2:        newCounterLimiter(limits.ManagementM2),
		managementM3:        newCounterLimiter(limits.ManagementM3),
		managementM2Reserve: reserve,
		background:          newCounterLimiter(limits.Background),
		retryAfter:          time.Second,
		now:                 time.Now,
	}
}

func (c *Controller) Admit(ctx context.Context, spec Spec) (context.Context, func(), error) {
	if err := spec.Validate(); err != nil {
		return nil, nil, err
	}
	release, err := c.acquire(spec)
	if err != nil {
		return nil, nil, err
	}
	admittedAt := time.Now()
	if c != nil && c.now != nil {
		admittedAt = c.now()
	}
	workloadContext, cancel := attachWorkload(ctx, spec, admittedAt)
	return workloadContext, combineRelease(cancel, release), nil
}

func (c *Controller) acquire(spec Spec) (func(), error) {
	if c == nil {
		return func() {}, nil
	}
	switch spec.Metadata.Priority {
	case priority.PriorityProxy:
		return c.acquireOne(c.proxy, spec, "proxy admission", 0)
	case priority.PriorityManagement:
		switch spec.Metadata.ManagementTier {
		case priority.ManagementTierM1:
			return c.acquireOne(c.managementM1, spec, "management M1 admission", 0)
		case priority.ManagementTierM2:
			return c.acquireOne(c.managementM2, spec, "management M2 admission", 0)
		case priority.ManagementTierM3:
			return c.acquireM3(spec)
		default:
			return nil, spec.Metadata.Validate()
		}
	case priority.PriorityBackground:
		return c.acquireOne(c.background, spec, "background admission", 0)
	default:
		return nil, spec.Metadata.Validate()
	}
}

func (c *Controller) acquireM3(spec Spec) (func(), error) {
	releaseM3, err := c.acquireOne(c.managementM3, spec, "management M3 admission", 0)
	if err != nil {
		return nil, err
	}
	releaseM2, err := c.acquireOne(c.managementM2, spec, "management M2 shared admission", c.managementM2Reserve)
	if err != nil {
		releaseM3()
		return nil, err
	}
	return combineRelease(releaseM2, releaseM3), nil
}

func (c *Controller) acquireOne(limiter *counterLimiter, spec Spec, resource string, reserve int64) (func(), error) {
	if limiter == nil {
		return func() {}, nil
	}
	if !limiter.tryAcquire(reserve) {
		return nil, &OverloadError{WorkloadName: spec.Name, Metadata: spec.Metadata, Resource: resource, RetryAfter: c.retryAfter}
	}
	return limiter.release, nil
}

func newCounterLimiter(max int64) *counterLimiter {
	if max <= 0 {
		return nil
	}
	return &counterLimiter{max: max}
}

func (l *counterLimiter) tryAcquire(reserve int64) bool {
	if reserve < 0 {
		reserve = 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inUse >= l.max-reserve {
		return false
	}
	l.inUse++
	return true
}

func (l *counterLimiter) release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inUse > 0 {
		l.inUse--
	}
}

func (e *OverloadError) Error() string {
	if e == nil {
		return "admission overloaded"
	}
	if e.Resource != "" {
		return fmt.Sprintf("%s overloaded for %s", e.Resource, e.WorkloadName)
	}
	return fmt.Sprintf("admission overloaded for %s", e.WorkloadName)
}

func combineRelease(releases ...func()) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			for idx := len(releases) - 1; idx >= 0; idx-- {
				if releases[idx] != nil {
					releases[idx]()
				}
			}
		})
	}
}
