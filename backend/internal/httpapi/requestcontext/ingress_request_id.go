package requestcontext

import "context"

// RuntimeIngressRequestID is the server-generated, opaque ingress identifier
// for runtime-branch traffic before operation acceptance. It is generated from
// server entropy and never derived from caller headers. Accepted operations use
// the runtime owner's UUID for telemetry and the response header instead; this
// branch token must not be used to look up durable request records.
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
