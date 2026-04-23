package auth

import (
	"testing"
	"time"
)

func TestRuntimeCacheProxyKeyDecisionInvalidation(t *testing.T) {
	t.Parallel()

	cache := NewRuntimeCache(2 * time.Second)
	loaderCalls := 0
	loader := func() (RuntimeProxyKeyDecision, error) {
		loaderCalls++
		return RuntimeProxyKeyDecision{Allowed: true, KeyID: loaderCalls, KeyName: "primary"}, nil
	}

	cacheKey := ProxyKeyDecisionCacheKey("prism_sk_runtime")
	first, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(10, 0), cacheKey, loader)
	if err != nil {
		t.Fatalf("first load returned error: %v", err)
	}
	second, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(11, 0), cacheKey, loader)
	if err != nil {
		t.Fatalf("second load returned error: %v", err)
	}
	if loaderCalls != 1 {
		t.Fatalf("expected cached auth decision after first load, loader called %d times", loaderCalls)
	}
	if first.KeyID != second.KeyID || second.KeyID != 1 {
		t.Fatalf("expected cached decision to be reused, got first=%+v second=%+v", first, second)
	}

	cache.Invalidate()
	third, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(11, 0), cacheKey, loader)
	if err != nil {
		t.Fatalf("third load returned error: %v", err)
	}
	if loaderCalls != 2 {
		t.Fatalf("expected invalidation to force a reload, loader called %d times", loaderCalls)
	}
	if third.KeyID != 2 {
		t.Fatalf("expected reloaded decision after invalidation, got %+v", third)
	}
}
