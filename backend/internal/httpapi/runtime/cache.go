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

	activeProfile *cachedActiveProfile
	planning      map[int]cachedPlanningSnapshot
}

type cachedActiveProfile struct {
	value     profiledomain.Profile
	expiresAt time.Time
}

type cachedPlanningSnapshot struct {
	value     planningSnapshot
	expiresAt time.Time
}

func NewSharedCache(ttl time.Duration) *SharedCache {
	if ttl <= 0 {
		ttl = defaultSharedCacheTTL
	}
	return &SharedCache{ttl: ttl, planning: map[int]cachedPlanningSnapshot{}}
}

func (c *SharedCache) InvalidateActiveProfile() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeProfile = nil
}

func (c *SharedCache) InvalidatePlanningProfile(profileID int) {
	if c == nil || profileID <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.planning, profileID)
}

func (c *SharedCache) InvalidateAllPlanning() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.planning = map[int]cachedPlanningSnapshot{}
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
	c.mu.RUnlock()

	value, err := loader()
	if err != nil {
		return zero, err
	}
	cachedValue := cloneProfile(value)

	c.mu.Lock()
	c.activeProfile = &cachedActiveProfile{value: cachedValue, expiresAt: c.expirationAt(resolvedNow)}
	c.mu.Unlock()
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
	c.mu.RUnlock()

	value, err := loader()
	if err != nil || value == nil {
		return value, err
	}
	cachedValue := clonePlanningSnapshot(*value)

	c.mu.Lock()
	c.planning[profileID] = cachedPlanningSnapshot{value: cachedValue, expiresAt: c.expirationAt(resolvedNow)}
	c.mu.Unlock()
	copyValue := clonePlanningSnapshot(cachedValue)
	return &copyValue, nil
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
