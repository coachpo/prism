package admission

import "context"

type releaseContextKey struct{}

func WithRelease(ctx context.Context, release func()) context.Context {
	if release == nil {
		return ctx
	}
	return context.WithValue(ctx, releaseContextKey{}, release)
}

func ReleaseFromContext(ctx context.Context) func() {
	if ctx == nil {
		return func() {}
	}
	release, ok := ctx.Value(releaseContextKey{}).(func())
	if !ok || release == nil {
		return func() {}
	}
	return release
}
