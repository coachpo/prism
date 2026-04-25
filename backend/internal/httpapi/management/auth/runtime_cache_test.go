package auth

import (
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

const runtimeAuthCacheTestConcurrency = 8

type runtimeAuthSettingsResult struct {
	value RuntimeAuthSettingsSnapshot
	err   error
}

type runtimeProxyKeyDecisionResult struct {
	value RuntimeProxyKeyDecision
	err   error
}

func TestRuntimeCacheCoalescesAuthSettingsReload(t *testing.T) {
	t.Parallel()

	cache := NewRuntimeCache(2 * time.Second)
	var loaderCalls atomic.Int32
	start := make(chan struct{})
	entered := make(chan struct{}, runtimeAuthCacheTestConcurrency)
	loaderStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	results := make(chan runtimeAuthSettingsResult, runtimeAuthCacheTestConcurrency)

	loader := func() (RuntimeAuthSettingsSnapshot, error) {
		loaderCalls.Add(1)
		signalRuntimeCacheOnce(loaderStarted)
		<-release
		return RuntimeAuthSettingsSnapshot{AuthEnabled: true}, nil
	}

	for range runtimeAuthCacheTestConcurrency {
		go func() {
			<-start
			entered <- struct{}{}
			value, err := cache.LoadRuntimeAuthSettings(time.Unix(10, 0), loader)
			results <- runtimeAuthSettingsResult{value: value, err: err}
		}()
	}

	close(start)
	waitForRuntimeCacheSignals(t, entered, runtimeAuthCacheTestConcurrency, "auth settings callers to start")
	waitForRuntimeCacheSignal(t, loaderStarted, "auth settings loader to start")
	close(release)

	for range runtimeAuthCacheTestConcurrency {
		result := <-results
		if result.err != nil {
			t.Fatalf("expected auth settings reload to succeed, got %v", result.err)
		}
		if !result.value.AuthEnabled {
			t.Fatalf("expected shared auth settings result, got %+v", result.value)
		}
	}
	if got := loaderCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one auth settings loader call, got %d", got)
	}
}

func TestRuntimeCacheCoalescesProxyKeyDecisionReload(t *testing.T) {
	t.Parallel()

	cache := NewRuntimeCache(2 * time.Second)
	var loaderCalls atomic.Int32
	start := make(chan struct{})
	entered := make(chan struct{}, runtimeAuthCacheTestConcurrency)
	loaderStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	results := make(chan runtimeProxyKeyDecisionResult, runtimeAuthCacheTestConcurrency)
	expiresAt := time.Unix(10, 0).Add(10 * time.Minute)

	loader := func() (RuntimeProxyKeyDecision, error) {
		loaderCalls.Add(1)
		signalRuntimeCacheOnce(loaderStarted)
		<-release
		return newRuntimeProxyKeyDecision(17, "coalesced-key", &expiresAt), nil
	}

	cacheKey := ProxyKeyDecisionCacheKey("prism_sk_runtime_coalesced")
	for range runtimeAuthCacheTestConcurrency {
		go func() {
			<-start
			entered <- struct{}{}
			value, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(10, 0), cacheKey, loader)
			results <- runtimeProxyKeyDecisionResult{value: value, err: err}
		}()
	}

	close(start)
	waitForRuntimeCacheSignals(t, entered, runtimeAuthCacheTestConcurrency, "proxy decision callers to start")
	waitForRuntimeCacheSignal(t, loaderStarted, "proxy decision loader to start")
	close(release)

	for range runtimeAuthCacheTestConcurrency {
		result := <-results
		if result.err != nil {
			t.Fatalf("expected proxy key decision reload to succeed, got %v", result.err)
		}
		if !result.value.Allowed || result.value.KeyID != 17 || result.value.KeyName != "coalesced-key" {
			t.Fatalf("expected shared proxy key decision result, got %+v", result.value)
		}
	}
	if got := loaderCalls.Load(); got != 1 {
		t.Fatalf("expected exactly one proxy key decision loader call, got %d", got)
	}
}

func TestRuntimeCacheFailureIsSharedNotCached(t *testing.T) {
	t.Parallel()

	t.Run("auth-settings", func(t *testing.T) {
		cache := NewRuntimeCache(2 * time.Second)
		var loaderCalls atomic.Int32
		wantErr := errors.New("auth settings reload failed")
		loaderStarted := make(chan struct{}, 1)
		release := make(chan struct{})
		results := make(chan runtimeAuthSettingsResult, 2)
		start := make(chan struct{})
		entered := make(chan struct{}, 2)

		loader := func() (RuntimeAuthSettingsSnapshot, error) {
			call := loaderCalls.Add(1)
			signalRuntimeCacheOnce(loaderStarted)
			<-release
			if call == 1 {
				return RuntimeAuthSettingsSnapshot{}, wantErr
			}
			return RuntimeAuthSettingsSnapshot{AuthEnabled: true}, nil
		}

		for range 2 {
			go func() {
				<-start
				entered <- struct{}{}
				value, err := cache.LoadRuntimeAuthSettings(time.Unix(30, 0), loader)
				results <- runtimeAuthSettingsResult{value: value, err: err}
			}()
		}

		close(start)
		waitForRuntimeCacheSignals(t, entered, 2, "auth settings failure callers to start")
		waitForRuntimeCacheSignal(t, loaderStarted, "auth settings failure loader to start")
		close(release)

		for range 2 {
			result := <-results
			if !errors.Is(result.err, wantErr) {
				t.Fatalf("expected shared auth settings error %v, got %v", wantErr, result.err)
			}
		}
		if got := loaderCalls.Load(); got != 1 {
			t.Fatalf("expected exactly one failing auth settings loader call, got %d", got)
		}

		value, err := cache.LoadRuntimeAuthSettings(time.Unix(31, 0), loader)
		if err != nil {
			t.Fatalf("expected auth settings reload after failure to succeed, got %v", err)
		}
		if !value.AuthEnabled {
			t.Fatalf("expected auth settings reload after failure, got %+v", value)
		}
		cachedValue, err := cache.LoadRuntimeAuthSettings(time.Unix(31, 1), loader)
		if err != nil {
			t.Fatalf("expected cached auth settings after recovery, got %v", err)
		}
		if !cachedValue.AuthEnabled {
			t.Fatalf("expected recovered auth settings to be cached, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected failure not to be cached as success, loader called %d times", got)
		}
	})

	t.Run("proxy-key-decision", func(t *testing.T) {
		cache := NewRuntimeCache(2 * time.Second)
		var loaderCalls atomic.Int32
		wantErr := errors.New("proxy key decision reload failed")
		loaderStarted := make(chan struct{}, 1)
		release := make(chan struct{})
		results := make(chan runtimeProxyKeyDecisionResult, 2)
		start := make(chan struct{})
		entered := make(chan struct{}, 2)
		expiresAt := time.Unix(30, 0).Add(10 * time.Minute)
		cacheKey := ProxyKeyDecisionCacheKey("prism_sk_runtime_failure")

		loader := func() (RuntimeProxyKeyDecision, error) {
			call := loaderCalls.Add(1)
			signalRuntimeCacheOnce(loaderStarted)
			<-release
			if call == 1 {
				return RuntimeProxyKeyDecision{}, wantErr
			}
			return newRuntimeProxyKeyDecision(27, "proxy-after-failure", &expiresAt), nil
		}

		for range 2 {
			go func() {
				<-start
				entered <- struct{}{}
				value, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(30, 0), cacheKey, loader)
				results <- runtimeProxyKeyDecisionResult{value: value, err: err}
			}()
		}

		close(start)
		waitForRuntimeCacheSignals(t, entered, 2, "proxy decision failure callers to start")
		waitForRuntimeCacheSignal(t, loaderStarted, "proxy decision failure loader to start")
		close(release)

		for range 2 {
			result := <-results
			if !errors.Is(result.err, wantErr) {
				t.Fatalf("expected shared proxy decision error %v, got %v", wantErr, result.err)
			}
		}
		if got := loaderCalls.Load(); got != 1 {
			t.Fatalf("expected exactly one failing proxy decision loader call, got %d", got)
		}

		value, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(31, 0), cacheKey, loader)
		if err != nil {
			t.Fatalf("expected proxy decision reload after failure to succeed, got %v", err)
		}
		if !value.Allowed || value.KeyID != 27 {
			t.Fatalf("expected proxy decision reload after failure, got %+v", value)
		}
		cachedValue, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(31, 1), cacheKey, loader)
		if err != nil {
			t.Fatalf("expected cached proxy decision after recovery, got %v", err)
		}
		if !cachedValue.Allowed || cachedValue.KeyID != 27 {
			t.Fatalf("expected recovered proxy decision to be cached, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected failure not to be cached as success, loader called %d times", got)
		}
	})
}

func TestRuntimeCacheInvalidationAfterCoalescedLoad(t *testing.T) {
	t.Parallel()

	t.Run("auth-settings", func(t *testing.T) {
		cache := NewRuntimeCache(2 * time.Second)
		var loaderCalls atomic.Int32
		firstStarted := make(chan struct{}, 1)
		secondStarted := make(chan struct{}, 1)
		waiterStarted := make(chan struct{}, 1)
		firstRelease := make(chan struct{})
		secondRelease := make(chan struct{})
		firstResult := make(chan runtimeAuthSettingsResult, 1)
		waiterResult := make(chan runtimeAuthSettingsResult, 1)
		secondResult := make(chan runtimeAuthSettingsResult, 1)

		loader := func() (RuntimeAuthSettingsSnapshot, error) {
			switch loaderCalls.Add(1) {
			case 1:
				signalRuntimeCacheOnce(firstStarted)
				<-firstRelease
				return RuntimeAuthSettingsSnapshot{AuthEnabled: false}, nil
			case 2:
				signalRuntimeCacheOnce(secondStarted)
				<-secondRelease
				return RuntimeAuthSettingsSnapshot{AuthEnabled: true}, nil
			default:
				return RuntimeAuthSettingsSnapshot{}, fmt.Errorf("unexpected auth settings loader call")
			}
		}

		go func() {
			value, err := cache.LoadRuntimeAuthSettings(time.Unix(50, 0), loader)
			firstResult <- runtimeAuthSettingsResult{value: value, err: err}
		}()

		waitForRuntimeCacheSignal(t, firstStarted, "first auth settings loader to start")
		cache.mu.RLock()
		firstCall := cache.authSettingsLoad
		cache.mu.RUnlock()
		if firstCall == nil {
			t.Fatal("expected first auth settings load to register an in-flight call")
		}

		go func() {
			signalRuntimeCacheOnce(waiterStarted)
			value, err := waitRuntimeAuthSettingsLoad(firstCall)
			waiterResult <- runtimeAuthSettingsResult{value: value, err: err}
		}()

		waitForRuntimeCacheSignal(t, waiterStarted, "auth settings waiter to start")
		cache.Invalidate()

		go func() {
			value, err := cache.LoadRuntimeAuthSettings(time.Unix(50, 0), loader)
			secondResult <- runtimeAuthSettingsResult{value: value, err: err}
		}()

		waitForRuntimeCacheSignal(t, secondStarted, "second auth settings loader to start")
		close(firstRelease)
		first := <-firstResult
		if !errors.Is(first.err, errRuntimeCacheLoadInvalidated) {
			t.Fatalf("expected invalidated auth settings load to fail with %v, got value %+v and err %v", errRuntimeCacheLoadInvalidated, first.value, first.err)
		}
		waiter := <-waiterResult
		if !errors.Is(waiter.err, errRuntimeCacheLoadInvalidated) {
			t.Fatalf("expected invalidated auth settings waiter to fail with %v, got value %+v and err %v", errRuntimeCacheLoadInvalidated, waiter.value, waiter.err)
		}

		close(secondRelease)
		second := <-secondResult
		if second.err != nil {
			t.Fatalf("expected second auth settings load to succeed, got %v", second.err)
		}
		if !second.value.AuthEnabled {
			t.Fatalf("expected invalidation to force fresh auth settings result, got %+v", second.value)
		}

		cachedValue, err := cache.LoadRuntimeAuthSettings(time.Unix(50, 1), func() (RuntimeAuthSettingsSnapshot, error) {
			return RuntimeAuthSettingsSnapshot{}, fmt.Errorf("stale auth settings wrote back into cache")
		})
		if err != nil {
			t.Fatalf("expected fresh auth settings to remain cached, got %v", err)
		}
		if !cachedValue.AuthEnabled {
			t.Fatalf("expected cached auth settings to use fresh value after invalidation, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected invalidation to trigger exactly one replacement auth settings load, got %d calls", got)
		}
	})

	t.Run("proxy-key-decision", func(t *testing.T) {
		cache := NewRuntimeCache(2 * time.Second)
		var loaderCalls atomic.Int32
		firstStarted := make(chan struct{}, 1)
		secondStarted := make(chan struct{}, 1)
		waiterStarted := make(chan struct{}, 1)
		firstRelease := make(chan struct{})
		secondRelease := make(chan struct{})
		firstResult := make(chan runtimeProxyKeyDecisionResult, 1)
		waiterResult := make(chan runtimeProxyKeyDecisionResult, 1)
		secondResult := make(chan runtimeProxyKeyDecisionResult, 1)
		firstExpiry := time.Unix(60, 0).Add(10 * time.Minute)
		secondExpiry := time.Unix(60, 0).Add(20 * time.Minute)
		cacheKey := ProxyKeyDecisionCacheKey("prism_sk_runtime_invalidation")

		loader := func() (RuntimeProxyKeyDecision, error) {
			switch loaderCalls.Add(1) {
			case 1:
				signalRuntimeCacheOnce(firstStarted)
				<-firstRelease
				return newRuntimeProxyKeyDecision(41, "stale-proxy", &firstExpiry), nil
			case 2:
				signalRuntimeCacheOnce(secondStarted)
				<-secondRelease
				return newRuntimeProxyKeyDecision(42, "fresh-proxy", &secondExpiry), nil
			default:
				return RuntimeProxyKeyDecision{}, fmt.Errorf("unexpected proxy decision loader call")
			}
		}

		go func() {
			value, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(60, 0), cacheKey, loader)
			firstResult <- runtimeProxyKeyDecisionResult{value: value, err: err}
		}()

		waitForRuntimeCacheSignal(t, firstStarted, "first proxy decision loader to start")
		cache.mu.RLock()
		firstCall, ok := cache.authDecisionLoads[cacheKey]
		cache.mu.RUnlock()
		if !ok || firstCall == nil {
			t.Fatal("expected first proxy decision load to register an in-flight call")
		}

		go func() {
			signalRuntimeCacheOnce(waiterStarted)
			value, err := waitRuntimeProxyKeyDecisionLoad(firstCall)
			waiterResult <- runtimeProxyKeyDecisionResult{value: value, err: err}
		}()

		waitForRuntimeCacheSignal(t, waiterStarted, "proxy decision waiter to start")
		cache.Invalidate()

		go func() {
			value, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(60, 0), cacheKey, loader)
			secondResult <- runtimeProxyKeyDecisionResult{value: value, err: err}
		}()

		waitForRuntimeCacheSignal(t, secondStarted, "second proxy decision loader to start")
		close(firstRelease)
		first := <-firstResult
		if !errors.Is(first.err, errRuntimeCacheLoadInvalidated) {
			t.Fatalf("expected invalidated proxy decision load to fail with %v, got value %+v and err %v", errRuntimeCacheLoadInvalidated, first.value, first.err)
		}
		waiter := <-waiterResult
		if !errors.Is(waiter.err, errRuntimeCacheLoadInvalidated) {
			t.Fatalf("expected invalidated proxy decision waiter to fail with %v, got value %+v and err %v", errRuntimeCacheLoadInvalidated, waiter.value, waiter.err)
		}

		close(secondRelease)
		second := <-secondResult
		if second.err != nil {
			t.Fatalf("expected second proxy decision load to succeed, got %v", second.err)
		}
		if !second.value.Allowed || second.value.KeyID != 42 {
			t.Fatalf("expected invalidation to force fresh proxy decision result, got %+v", second.value)
		}

		cachedValue, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(60, 1), cacheKey, func() (RuntimeProxyKeyDecision, error) {
			return RuntimeProxyKeyDecision{}, fmt.Errorf("stale proxy decision wrote back into cache")
		})
		if err != nil {
			t.Fatalf("expected fresh proxy decision to remain cached, got %v", err)
		}
		if !cachedValue.Allowed || cachedValue.KeyID != 42 {
			t.Fatalf("expected cached proxy decision to use fresh value after invalidation, got %+v", cachedValue)
		}
		if got := loaderCalls.Load(); got != 2 {
			t.Fatalf("expected invalidation to trigger exactly one replacement proxy decision load, got %d calls", got)
		}
	})
}

func newRuntimeProxyKeyDecision(keyID int, keyName string, expiresAt *time.Time) RuntimeProxyKeyDecision {
	decision := RuntimeProxyKeyDecision{Allowed: true, KeyID: keyID, KeyName: keyName}
	if expiresAt != nil {
		resolved := expiresAt.UTC()
		decision.ExpiresAt = &resolved
	}
	return decision
}

func signalRuntimeCacheOnce(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func waitForRuntimeCacheSignal(t *testing.T, ch <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForRuntimeCacheSignals(t *testing.T, ch <-chan struct{}, count int, description string) {
	t.Helper()
	for i := 0; i < count; i++ {
		waitForRuntimeCacheSignal(t, ch, fmt.Sprintf("%s (%d/%d)", description, i+1, count))
	}
}
