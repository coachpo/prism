package profiledomain

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func ResolveEffectiveProfile(ctx context.Context, exec QueryExecutor, rawHeader string) (Profile, error) {
	trimmed := strings.TrimSpace(rawHeader)
	if trimmed == "" {
		return Profile{}, &HTTPError{StatusCode: 400, Code: ScopeErrorCodeHeaderMissing, Detail: fmt.Sprintf("%s header is required", ProfileIDHeader)}
	}

	profileID, err := strconv.Atoi(trimmed)
	if err != nil {
		return Profile{}, &HTTPError{StatusCode: 400, Code: ScopeErrorCodeHeaderInvalid, Detail: fmt.Sprintf("%s must be an integer", ProfileIDHeader)}
	}
	if profileID <= 0 {
		return Profile{}, &HTTPError{StatusCode: 400, Code: ScopeErrorCodeHeaderNonPositive, Detail: fmt.Sprintf("%s must be a positive integer", ProfileIDHeader)}
	}

	profile, found, err := LoadNonDeletedProfile(ctx, exec, profileID)
	if err != nil {
		return Profile{}, err
	}
	if !found {
		return Profile{}, &HTTPError{StatusCode: 404, Code: ScopeErrorCodeProfileNotFound, Detail: fmt.Sprintf("Profile %d not found", profileID)}
	}
	return profile, nil
}

func ResolveActiveProfile(ctx context.Context, exec QueryExecutor, now func() time.Time) (Profile, error) {
	return EnsureInvariants(ctx, exec, now)
}
