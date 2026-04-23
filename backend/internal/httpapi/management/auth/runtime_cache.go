package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type RuntimeCache struct {
	ttl time.Duration

	mu sync.RWMutex

	authSettings  *cachedRuntimeAuthSettings
	authDecisions map[string]cachedRuntimeProxyKeyDecision
}

type RuntimeAuthSettingsSnapshot struct {
	AuthEnabled bool
}

type RuntimeProxyKeyDecision struct {
	Allowed   bool
	KeyID     int
	KeyName   string
	ExpiresAt *time.Time
}

type cachedRuntimeAuthSettings struct {
	value     RuntimeAuthSettingsSnapshot
	expiresAt time.Time
}

type cachedRuntimeProxyKeyDecision struct {
	value     RuntimeProxyKeyDecision
	expiresAt time.Time
}

func NewRuntimeCache(ttl time.Duration) *RuntimeCache {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	return &RuntimeCache{ttl: ttl, authDecisions: map[string]cachedRuntimeProxyKeyDecision{}}
}

func ProxyKeyDecisionCacheKey(normalizedKey string) string {
	sum := sha256.Sum256([]byte(normalizedKey))
	return hex.EncodeToString(sum[:])
}

func (c *RuntimeCache) Invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.authSettings = nil
	c.authDecisions = map[string]cachedRuntimeProxyKeyDecision{}
}

func (c *RuntimeCache) LoadRuntimeAuthSettings(now time.Time, loader func() (RuntimeAuthSettingsSnapshot, error)) (RuntimeAuthSettingsSnapshot, error) {
	var zero RuntimeAuthSettingsSnapshot
	if c == nil {
		return loader()
	}
	resolvedNow := now.UTC()
	c.mu.RLock()
	entry := c.authSettings
	if entry != nil && resolvedNow.Before(entry.expiresAt) {
		value := entry.value
		c.mu.RUnlock()
		return value, nil
	}
	c.mu.RUnlock()

	value, err := loader()
	if err != nil {
		return zero, err
	}

	c.mu.Lock()
	c.authSettings = &cachedRuntimeAuthSettings{value: value, expiresAt: c.expirationAt(resolvedNow)}
	c.mu.Unlock()
	return value, nil
}

func (c *RuntimeCache) LoadRuntimeProxyKeyDecision(now time.Time, cacheKey string, loader func() (RuntimeProxyKeyDecision, error)) (RuntimeProxyKeyDecision, error) {
	var zero RuntimeProxyKeyDecision
	if c == nil {
		return loader()
	}
	resolvedNow := now.UTC()
	c.mu.RLock()
	entry, ok := c.authDecisions[cacheKey]
	if ok && resolvedNow.Before(entry.expiresAt) {
		value := cloneRuntimeProxyKeyDecision(entry.value)
		c.mu.RUnlock()
		return value, nil
	}
	c.mu.RUnlock()

	value, err := loader()
	if err != nil {
		return zero, err
	}
	expiresAt := c.expirationAtWithCeiling(resolvedNow, value.ExpiresAt)
	if !expiresAt.After(resolvedNow) {
		return value, nil
	}

	c.mu.Lock()
	c.authDecisions[cacheKey] = cachedRuntimeProxyKeyDecision{value: cloneRuntimeProxyKeyDecision(value), expiresAt: expiresAt}
	c.mu.Unlock()
	return value, nil
}

func (c *RuntimeCache) expirationAt(now time.Time) time.Time {
	return now.UTC().Add(c.ttl)
}

func (c *RuntimeCache) expirationAtWithCeiling(now time.Time, ceiling *time.Time) time.Time {
	expiresAt := c.expirationAt(now)
	if ceiling == nil {
		return expiresAt
	}
	resolvedCeiling := ceiling.UTC()
	if resolvedCeiling.Before(expiresAt) {
		return resolvedCeiling
	}
	return expiresAt
}

func cloneRuntimeProxyKeyDecision(decision RuntimeProxyKeyDecision) RuntimeProxyKeyDecision {
	cloned := RuntimeProxyKeyDecision{
		Allowed: decision.Allowed,
		KeyID:   decision.KeyID,
		KeyName: decision.KeyName,
	}
	if decision.ExpiresAt != nil {
		expiresAt := decision.ExpiresAt.UTC()
		cloned.ExpiresAt = &expiresAt
	}
	return cloned
}
