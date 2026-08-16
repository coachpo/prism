package requestcontext

import (
	"context"
	"time"
)

// RuntimeProxyKeySnapshot is the request-time immutable identity of an
// identified proxy key. It is frozen at ingress; later key renames or
// deletions never rewrite it.
type RuntimeProxyKeySnapshot struct {
	ID         int
	Name       string
	LastUsedAt time.Time
	LastUsedIP string
}

// RuntimeProxyKeyAttributionState distinguishes why a request does or does not
// carry a proxy key identity. It is persisted with every retained runtime row.
type RuntimeProxyKeyAttributionState string

const (
	// RuntimeProxyKeyIdentified means the runtime verified an active,
	// unexpired key and froze its ID/name snapshot.
	RuntimeProxyKeyIdentified RuntimeProxyKeyAttributionState = "identified"
	// RuntimeProxyKeyNone means no accepted credential was present: the
	// caller omitted a key or supplied an unrecognized one. It never
	// discloses whether a credential was supplied.
	RuntimeProxyKeyNone RuntimeProxyKeyAttributionState = "none"
	// RuntimeProxyKeyUnknown means telemetry/legacy evidence cannot reliably
	// determine identity (e.g. an optional permissive lookup failed, or the
	// row predates attribution state).
	RuntimeProxyKeyUnknown RuntimeProxyKeyAttributionState = "unknown"
)

// RuntimeProxyKeyAttribution is the typed request context for proxy key
// enforcement and attribution. enforcement and attribution are separate axes:
// AuthEnforced is the auth setting used for this request (frozen at ingress),
// while State describes the identity evidence.
type RuntimeProxyKeyAttribution struct {
	State        RuntimeProxyKeyAttributionState
	Snapshot     *RuntimeProxyKeySnapshot
	AuthEnforced bool
}

type runtimeProxyKeyAttributionContextKey struct{}

// WithRuntimeProxyKeyAttribution stores the typed attribution. It replaces the
// nullable-snapshot-only contract: consumers must not infer state from
// snapshot pointer nullability.
func WithRuntimeProxyKeyAttribution(ctx context.Context, attribution RuntimeProxyKeyAttribution) context.Context {
	return context.WithValue(ctx, runtimeProxyKeyAttributionContextKey{}, attribution)
}

// RuntimeProxyKeyAttributionFromContext returns the typed attribution. The
// bool is false when no attribution was stored.
func RuntimeProxyKeyAttributionFromContext(ctx context.Context) (RuntimeProxyKeyAttribution, bool) {
	attribution, ok := ctx.Value(runtimeProxyKeyAttributionContextKey{}).(RuntimeProxyKeyAttribution)
	return attribution, ok
}

// ---------------------------------------------------------------------------
// Legacy snapshot accessors (kept for callers that only need the identity).
// They read the typed attribution and return the snapshot only when the state
// is identified.
// ---------------------------------------------------------------------------

type runtimeProxyKeyContextKey struct{}

// WithRuntimeProxyKey stores an identified snapshot as the legacy context
// value. It is equivalent to WithRuntimeProxyKeyAttribution with
// RuntimeProxyKeyIdentified; prefer the typed form for new call sites.
func WithRuntimeProxyKey(ctx context.Context, snapshot RuntimeProxyKeySnapshot) context.Context {
	return WithRuntimeProxyKeyAttribution(ctx, RuntimeProxyKeyAttribution{
		State:        RuntimeProxyKeyIdentified,
		Snapshot:     &snapshot,
		AuthEnforced: true,
	})
}

// RuntimeProxyKeyFromContext returns the identified snapshot, or nil when the
// attribution is not identified (none/unknown/missing). Legacy consumers that
// need state or enforcement must migrate to
// RuntimeProxyKeyAttributionFromContext.
func RuntimeProxyKeyFromContext(ctx context.Context) (*RuntimeProxyKeySnapshot, bool) {
	attribution, ok := RuntimeProxyKeyAttributionFromContext(ctx)
	if !ok || attribution.State != RuntimeProxyKeyIdentified || attribution.Snapshot == nil {
		return nil, false
	}
	snapshot := *attribution.Snapshot
	return &snapshot, true
}
