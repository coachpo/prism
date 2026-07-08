package profiledomain

import (
	"context"
	"time"
)

func ResolveEffectiveProfile(ctx context.Context, exec QueryExecutor, rawHeader string) (Profile, error) {
	_ = rawHeader
	// ponytail: pinned to Default profile id=1; unfreeze by restoring header parsing.
	profile, found, err := LoadNonDeletedProfile(ctx, exec, 1)
	if err != nil {
		return Profile{}, err
	}
	if !found {
		return Profile{}, &HTTPError{StatusCode: 404, Code: ScopeErrorCodeProfileNotFound, Detail: "Profile 1 not found"}
	}
	return profile, nil
}

func ResolveActiveProfile(ctx context.Context, exec QueryExecutor, now func() time.Time) (Profile, error) {
	return EnsureInvariants(ctx, exec, now)
}
