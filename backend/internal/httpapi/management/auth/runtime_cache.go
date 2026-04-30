package auth

import (
	"context"
	"errors"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type RuntimeCache struct {
	shared *runtimeapi.SharedCache
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

func NewRuntimeCache(ttl time.Duration) *RuntimeCache {
	return NewRuntimeCacheFromShared(runtimeapi.NewSharedCache(ttl))
}

func NewRuntimeCacheFromShared(shared *runtimeapi.SharedCache) *RuntimeCache {
	if shared == nil {
		return nil
	}
	return &RuntimeCache{shared: shared}
}

func (c *RuntimeCache) SharedCache() *runtimeapi.SharedCache {
	if c == nil {
		return nil
	}
	return c.shared
}

func (c *RuntimeCache) Bootstrap(ctx context.Context) error {
	if c == nil || c.shared == nil {
		return runtimeapi.ErrPublishedRuntimeSnapshotUnavailable
	}
	return c.shared.Bootstrap(ctx)
}

func (c *RuntimeCache) PublishedGeneration() uint64 {
	if c == nil || c.shared == nil {
		return 0
	}
	return c.shared.PublishedGeneration()
}

func (c *RuntimeCache) Invalidate() {
	c.ScheduleRefresh(runtimeapi.RefreshRequest{Auth: true})
}

func (c *RuntimeCache) RefreshNow(ctx context.Context, request runtimeapi.RefreshRequest) error {
	if c == nil || c.shared == nil {
		return runtimeapi.ErrPublishedRuntimeSnapshotUnavailable
	}
	return c.shared.RefreshNow(ctx, request)
}

func (c *RuntimeCache) ScheduleRefresh(request runtimeapi.RefreshRequest) {
	if c == nil || c.shared == nil {
		return
	}
	c.shared.ScheduleRefresh(request)
}

func (c *RuntimeCache) LoadRuntimeAuthSettings() (RuntimeAuthSettingsSnapshot, error) {
	return c.LoadFreshRuntimeAuthSettings(context.Background())
}

func (c *RuntimeCache) LoadFreshRuntimeAuthSettings(ctx context.Context) (RuntimeAuthSettingsSnapshot, error) {
	if c == nil || c.shared == nil {
		return RuntimeAuthSettingsSnapshot{}, runtimeapi.ErrPublishedRuntimeSnapshotUnavailable
	}
	snapshot, err := c.shared.LoadFreshRuntimeAuthSettings(ctx)
	if err != nil {
		return RuntimeAuthSettingsSnapshot{}, err
	}
	return RuntimeAuthSettingsSnapshot{AuthEnabled: snapshot.AuthEnabled}, nil
}

func (c *RuntimeCache) LoadRuntimeProxyKeyDecision(now time.Time, rawKey string) (RuntimeProxyKeyDecision, error) {
	return c.LoadFreshRuntimeProxyKeyDecision(context.Background(), now, rawKey)
}

func (c *RuntimeCache) LoadFreshRuntimeProxyKeyDecision(ctx context.Context, now time.Time, rawKey string) (RuntimeProxyKeyDecision, error) {
	if c == nil || c.shared == nil {
		return RuntimeProxyKeyDecision{}, runtimeapi.ErrPublishedRuntimeSnapshotUnavailable
	}
	normalizedKey, keyPrefix, err := parseProxyAPIKey(rawKey)
	if err != nil {
		return RuntimeProxyKeyDecision{}, nil
	}
	record, ok, err := c.shared.LoadFreshRuntimeProxyKeyRecord(ctx, keyPrefix)
	if err != nil {
		return RuntimeProxyKeyDecision{}, err
	}
	if !ok {
		return RuntimeProxyKeyDecision{}, nil
	}
	if record.ExpiresAt != nil && !now.UTC().Before(record.ExpiresAt.UTC()) {
		return RuntimeProxyKeyDecision{}, nil
	}
	if !verifyOpaqueToken(normalizedKey, record.KeyHash) {
		return RuntimeProxyKeyDecision{}, nil
	}
	decision := RuntimeProxyKeyDecision{Allowed: true, KeyID: record.KeyID, KeyName: record.KeyName}
	if record.ExpiresAt != nil {
		expiresAt := record.ExpiresAt.UTC()
		decision.ExpiresAt = &expiresAt
	}
	return decision, nil
}

func runtimeSnapshotUnavailableError() error {
	return runtimeapi.ErrPublishedRuntimeSnapshotUnavailable
}

func isPublishedSnapshotUnavailable(err error) bool {
	return errors.Is(err, runtimeapi.ErrPublishedRuntimeSnapshotUnavailable) || errors.Is(err, runtimeapi.ErrRuntimeSnapshotRefreshRequired)
}
