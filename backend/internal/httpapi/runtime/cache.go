package runtime

import (
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/profiledomain"
)

const defaultSharedCacheTTL = 2 * time.Second

type SharedCache struct {
	ttl time.Duration

	mu sync.RWMutex

	activeProfile     *cachedActiveProfile
	activeProfileLoad *activeProfileLoadCall
	planning          map[int]cachedPlanningSnapshot
	planningLoads     map[int]*planningSnapshotLoadCall
}

type cachedActiveProfile struct {
	value     profiledomain.Profile
	expiresAt time.Time
}

type cachedPlanningSnapshot struct {
	value     planningSnapshot
	expiresAt time.Time
}

type activeProfileLoadCall struct {
	done  chan struct{}
	value profiledomain.Profile
	err   error
}

type planningSnapshotLoadCall struct {
	done  chan struct{}
	value *planningSnapshot
	err   error
}

func NewSharedCache(ttl time.Duration) *SharedCache {
	if ttl <= 0 {
		ttl = defaultSharedCacheTTL
	}
	return &SharedCache{
		ttl:           ttl,
		planning:      map[int]cachedPlanningSnapshot{},
		planningLoads: map[int]*planningSnapshotLoadCall{},
	}
}

func (c *SharedCache) InvalidateActiveProfile() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeProfile = nil
	c.activeProfileLoad = nil
}

func (c *SharedCache) InvalidatePlanningProfile(profileID int) {
	if c == nil || profileID <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.planning, profileID)
	delete(c.planningLoads, profileID)
}

func (c *SharedCache) InvalidateAllPlanning() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.planning = map[int]cachedPlanningSnapshot{}
	c.planningLoads = map[int]*planningSnapshotLoadCall{}
}

func (c *SharedCache) loadActiveProfile(now time.Time, loader func() (profiledomain.Profile, error)) (profiledomain.Profile, error) {
	var zero profiledomain.Profile
	if c == nil {
		return loader()
	}
	resolvedNow := now.UTC()
	c.mu.RLock()
	entry := c.activeProfile
	if entry != nil && resolvedNow.Before(entry.expiresAt) {
		value := cloneProfile(entry.value)
		c.mu.RUnlock()
		return value, nil
	}
	if call := c.activeProfileLoad; call != nil {
		c.mu.RUnlock()
		return waitActiveProfileLoad(call)
	}
	c.mu.RUnlock()

	c.mu.Lock()
	entry = c.activeProfile
	if entry != nil && resolvedNow.Before(entry.expiresAt) {
		value := cloneProfile(entry.value)
		c.mu.Unlock()
		return value, nil
	}
	if call := c.activeProfileLoad; call != nil {
		c.mu.Unlock()
		return waitActiveProfileLoad(call)
	}
	call := &activeProfileLoadCall{done: make(chan struct{})}
	c.activeProfileLoad = call
	c.mu.Unlock()

	value, err := loader()
	var cachedValue profiledomain.Profile
	if err == nil {
		cachedValue = cloneProfile(value)
	}

	c.mu.Lock()
	call.value = cachedValue
	call.err = err
	if c.activeProfileLoad == call {
		c.activeProfileLoad = nil
		if err == nil {
			c.activeProfile = &cachedActiveProfile{value: cachedValue, expiresAt: c.expirationAt(resolvedNow)}
		}
	}
	c.mu.Unlock()
	close(call.done)
	if err != nil {
		return zero, err
	}
	return cloneProfile(cachedValue), nil
}

func (c *SharedCache) loadPlanningSnapshot(now time.Time, profileID int, loader func() (*planningSnapshot, error)) (*planningSnapshot, error) {
	if c == nil {
		return loader()
	}
	resolvedNow := now.UTC()
	c.mu.RLock()
	entry, ok := c.planning[profileID]
	if ok && resolvedNow.Before(entry.expiresAt) {
		value := clonePlanningSnapshot(entry.value)
		c.mu.RUnlock()
		return &value, nil
	}
	if call, ok := c.planningLoads[profileID]; ok {
		c.mu.RUnlock()
		return waitPlanningSnapshotLoad(call)
	}
	c.mu.RUnlock()

	c.mu.Lock()
	entry, ok = c.planning[profileID]
	if ok && resolvedNow.Before(entry.expiresAt) {
		value := clonePlanningSnapshot(entry.value)
		c.mu.Unlock()
		return &value, nil
	}
	if call, ok := c.planningLoads[profileID]; ok {
		c.mu.Unlock()
		return waitPlanningSnapshotLoad(call)
	}
	call := &planningSnapshotLoadCall{done: make(chan struct{})}
	c.planningLoads[profileID] = call
	c.mu.Unlock()

	value, err := loader()
	var cachedValue *planningSnapshot
	if err == nil && value != nil {
		cloned := clonePlanningSnapshot(*value)
		cachedValue = &cloned
	}

	c.mu.Lock()
	call.value = cachedValue
	call.err = err
	if current, ok := c.planningLoads[profileID]; ok && current == call {
		delete(c.planningLoads, profileID)
		if cachedValue != nil {
			c.planning[profileID] = cachedPlanningSnapshot{value: *cachedValue, expiresAt: c.expirationAt(resolvedNow)}
		}
	}
	c.mu.Unlock()
	close(call.done)
	if err != nil || cachedValue == nil {
		return cachedValue, err
	}
	copyValue := clonePlanningSnapshot(*cachedValue)
	return &copyValue, nil
}

func waitActiveProfileLoad(call *activeProfileLoadCall) (profiledomain.Profile, error) {
	var zero profiledomain.Profile
	<-call.done
	if call.err != nil {
		return zero, call.err
	}
	return cloneProfile(call.value), nil
}

func waitPlanningSnapshotLoad(call *planningSnapshotLoadCall) (*planningSnapshot, error) {
	<-call.done
	if call.err != nil || call.value == nil {
		return call.value, call.err
	}
	cloned := clonePlanningSnapshot(*call.value)
	return &cloned, nil
}

func (c *SharedCache) expirationAt(now time.Time) time.Time {
	return now.UTC().Add(c.ttl)
}

func cloneProfile(profile profiledomain.Profile) profiledomain.Profile {
	cloned := profile
	if profile.Description != nil {
		description := *profile.Description
		cloned.Description = &description
	}
	if profile.DeletedAt != nil {
		deletedAt := profile.DeletedAt.UTC()
		cloned.DeletedAt = &deletedAt
	}
	return cloned
}
