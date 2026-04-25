package requestcontext

import (
	"context"
	"time"
)

type RuntimeProxyKeySnapshot struct {
	ID         int
	Name       string
	LastUsedAt time.Time
	LastUsedIP string
}

type runtimeProxyKeyContextKey struct{}

func WithRuntimeProxyKey(ctx context.Context, snapshot RuntimeProxyKeySnapshot) context.Context {
	return context.WithValue(ctx, runtimeProxyKeyContextKey{}, snapshot)
}

func RuntimeProxyKeyFromContext(ctx context.Context) (*RuntimeProxyKeySnapshot, bool) {
	proxyKeyValue := ctx.Value(runtimeProxyKeyContextKey{})
	proxyKey, ok := proxyKeyValue.(RuntimeProxyKeySnapshot)
	if !ok {
		return nil, false
	}
	return &RuntimeProxyKeySnapshot{
		ID:         proxyKey.ID,
		Name:       proxyKey.Name,
		LastUsedAt: proxyKey.LastUsedAt,
		LastUsedIP: proxyKey.LastUsedIP,
	}, true
}
