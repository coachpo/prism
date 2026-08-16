package requestcontext

import "context"

// ManagementPrincipalSnapshot is the server-observed identity/session
// generation that a destructive management preview is bound to. It contains
// no credential material; changing the subject, token version, or effective
// auth generation invalidates the sealed preview.
type ManagementPrincipalSnapshot struct {
	SubjectID      string
	TokenVersion   string
	AuthGeneration string
}

type managementPrincipalSnapshotContextKey struct{}

// WithManagementPrincipalSnapshot stores the authenticated management
// principal snapshot for downstream domain handlers.
func WithManagementPrincipalSnapshot(ctx context.Context, snapshot ManagementPrincipalSnapshot) context.Context {
	return context.WithValue(ctx, managementPrincipalSnapshotContextKey{}, snapshot)
}

// ManagementPrincipalSnapshotFromContext returns the authenticated principal
// snapshot, if the management auth middleware established one.
func ManagementPrincipalSnapshotFromContext(ctx context.Context) (ManagementPrincipalSnapshot, bool) {
	snapshot, ok := ctx.Value(managementPrincipalSnapshotContextKey{}).(ManagementPrincipalSnapshot)
	return snapshot, ok
}
