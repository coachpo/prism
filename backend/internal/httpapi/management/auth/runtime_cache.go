package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var errRuntimeCacheLoadInvalidated = errors.New("runtime cache load invalidated")

func isRuntimeCacheLoadInvalidated(err error) bool {
	return errors.Is(err, errRuntimeCacheLoadInvalidated)
}

type RuntimeCache struct {
	ttl time.Duration

	mu sync.RWMutex

	authSettings      *cachedRuntimeAuthSettings
	authSettingsLoad  *runtimeAuthSettingsLoadCall
	authDecisions     map[string]cachedRuntimeProxyKeyDecision
	authDecisionLoads map[string]*runtimeProxyKeyDecisionLoadCall
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

type runtimeAuthSettingsLoadCall struct {
	done        chan struct{}
	value       RuntimeAuthSettingsSnapshot
	err         error
	invalidated bool
}

type runtimeProxyKeyDecisionLoadCall struct {
	done        chan struct{}
	value       RuntimeProxyKeyDecision
	err         error
	invalidated bool
}

func NewRuntimeCache(ttl time.Duration) *RuntimeCache {
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	return &RuntimeCache{
		ttl:               ttl,
		authDecisions:     map[string]cachedRuntimeProxyKeyDecision{},
		authDecisionLoads: map[string]*runtimeProxyKeyDecisionLoadCall{},
	}
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
	if c.authSettingsLoad != nil {
		c.authSettingsLoad.invalidated = true
	}
	c.authSettingsLoad = nil
	c.authDecisions = map[string]cachedRuntimeProxyKeyDecision{}
	for _, call := range c.authDecisionLoads {
		call.invalidated = true
	}
	c.authDecisionLoads = map[string]*runtimeProxyKeyDecisionLoadCall{}
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
	if call := c.authSettingsLoad; call != nil {
		c.mu.RUnlock()
		return waitRuntimeAuthSettingsLoad(call)
	}
	c.mu.RUnlock()

	c.mu.Lock()
	entry = c.authSettings
	if entry != nil && resolvedNow.Before(entry.expiresAt) {
		value := entry.value
		c.mu.Unlock()
		return value, nil
	}
	if call := c.authSettingsLoad; call != nil {
		c.mu.Unlock()
		return waitRuntimeAuthSettingsLoad(call)
	}
	call := &runtimeAuthSettingsLoadCall{done: make(chan struct{})}
	c.authSettingsLoad = call
	c.mu.Unlock()

	value, err := loader()

	c.mu.Lock()
	call.value = value
	call.err = err
	if c.authSettingsLoad == call {
		c.authSettingsLoad = nil
		if err == nil && !call.invalidated {
			c.authSettings = &cachedRuntimeAuthSettings{value: value, expiresAt: c.expirationAt(resolvedNow)}
		}
	}
	close(call.done)
	invalidated := call.invalidated
	c.mu.Unlock()
	if err != nil {
		return zero, err
	}
	if invalidated {
		return zero, errRuntimeCacheLoadInvalidated
	}
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
	if call, ok := c.authDecisionLoads[cacheKey]; ok {
		c.mu.RUnlock()
		return waitRuntimeProxyKeyDecisionLoad(call)
	}
	c.mu.RUnlock()

	c.mu.Lock()
	entry, ok = c.authDecisions[cacheKey]
	if ok && resolvedNow.Before(entry.expiresAt) {
		value := cloneRuntimeProxyKeyDecision(entry.value)
		c.mu.Unlock()
		return value, nil
	}
	if call, ok := c.authDecisionLoads[cacheKey]; ok {
		c.mu.Unlock()
		return waitRuntimeProxyKeyDecisionLoad(call)
	}
	call := &runtimeProxyKeyDecisionLoadCall{done: make(chan struct{})}
	c.authDecisionLoads[cacheKey] = call
	c.mu.Unlock()

	value, err := loader()
	sharedValue := cloneRuntimeProxyKeyDecision(value)
	expiresAt := c.expirationAtWithCeiling(resolvedNow, sharedValue.ExpiresAt)

	c.mu.Lock()
	call.value = sharedValue
	call.err = err
	if current, ok := c.authDecisionLoads[cacheKey]; ok && current == call {
		delete(c.authDecisionLoads, cacheKey)
		if err == nil && expiresAt.After(resolvedNow) && !call.invalidated {
			c.authDecisions[cacheKey] = cachedRuntimeProxyKeyDecision{value: sharedValue, expiresAt: expiresAt}
		}
	}
	close(call.done)
	invalidated := call.invalidated
	c.mu.Unlock()
	if err != nil {
		return zero, err
	}
	if invalidated {
		return zero, errRuntimeCacheLoadInvalidated
	}
	if !expiresAt.After(resolvedNow) {
		return sharedValue, nil
	}
	return cloneRuntimeProxyKeyDecision(sharedValue), nil
}

func waitRuntimeAuthSettingsLoad(call *runtimeAuthSettingsLoadCall) (RuntimeAuthSettingsSnapshot, error) {
	var zero RuntimeAuthSettingsSnapshot
	<-call.done
	if call.err != nil {
		return zero, call.err
	}
	if call.invalidated {
		return zero, errRuntimeCacheLoadInvalidated
	}
	return call.value, nil
}

func waitRuntimeProxyKeyDecisionLoad(call *runtimeProxyKeyDecisionLoadCall) (RuntimeProxyKeyDecision, error) {
	var zero RuntimeProxyKeyDecision
	<-call.done
	if call.err != nil {
		return zero, call.err
	}
	if call.invalidated {
		return zero, errRuntimeCacheLoadInvalidated
	}
	return cloneRuntimeProxyKeyDecision(call.value), nil
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
