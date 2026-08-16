package auth

import (
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimeCacheFromSharedKeepsSharedPointer(t *testing.T) {
	t.Parallel()

	shared := runtimeapi.NewSharedCache()
	cache := NewRuntimeCacheFromShared(shared)
	if cache == nil {
		t.Fatal("expected runtime cache wrapper to be created")
	}
	if cache.SharedCache() != shared {
		t.Fatalf("expected runtime cache wrapper to keep shared pointer %p, got %p", shared, cache.SharedCache())
	}
}

func TestRuntimeCachePublishedSnapshotRequired(t *testing.T) {
	t.Parallel()

	cache := NewRuntimeCache()

	settings, err := cache.LoadRuntimeAuthSettings()
	if !isPublishedSnapshotUnavailable(err) {
		t.Fatalf("expected unpublished auth settings load to report snapshot unavailable, got value %+v and err %v", settings, err)
	}

	decision, err := cache.LoadRuntimeProxyKeyDecision(time.Unix(10, 0).UTC(), "pm-12345678deadbeef")
	if !isPublishedSnapshotUnavailable(err) {
		t.Fatalf("expected unpublished proxy decision load to report snapshot unavailable, got value %+v and err %v", decision, err)
	}
}

func TestRuntimeCacheInvalidateWithoutPublishedSnapshotIsSafe(t *testing.T) {
	t.Parallel()

	cache := NewRuntimeCache()
	cache.Invalidate()
	if got := cache.PublishedGeneration(); got != 0 {
		t.Fatalf("expected invalidate on unpublished cache to leave generation at 0, got %d", got)
	}
}
