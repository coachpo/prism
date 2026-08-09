package requestcontext

import "context"

// RuntimeIngressRequestID is the server-generated, opaque ingress identifier
// for runtime-branch traffic. It is generated from server entropy, never
// derived from a caller-supplied X-Request-ID or X-Prism-* header, and is the
// durable correlation key for telemetry, request logs and usage events.
type RuntimeIngressRequestID string

type runtimeIngressRequestIDContextKey struct{}

// WithRuntimeIngressRequestID stores the internal ingress correlation ID.
func WithRuntimeIngressRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, runtimeIngressRequestIDContextKey{}, id)
}

// RuntimeIngressRequestIDFromContext returns the internal ingress correlation
// ID and whether one was stored. It is empty when the request never reached
// the runtime branch (management traffic, OPTIONS preflight, etc.).
func RuntimeIngressRequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(runtimeIngressRequestIDContextKey{}).(string)
	return id, ok
}
