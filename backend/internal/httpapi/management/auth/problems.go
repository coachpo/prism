package auth

import (
	"fmt"
	"net/http"
	"time"
)

// Auth problem codes owned by this package (umbrella flat envelope).
const (
	ProblemCodeAuthNotAuthenticated         = "auth_not_authenticated"
	ProblemCodeAuthNotEnabled               = "auth_not_enabled"
	ProblemCodeAuthInvalidCredentials       = "auth_invalid_credentials"
	ProblemCodeAuthLoginLocked              = "auth_login_locked"
	ProblemCodeAuthTransitionInProgress     = "auth_transition_in_progress"
	ProblemCodeAuthTransitionRecoveryNeeded = "auth_transition_recovery_required"
)

// ProblemRouteMatcher is the exact route class bound to a registered problem
// entry. It mirrors the frontend coordinator's matcher so a wrong matcher is
// a typed registry failure instead of a silent drift.
type ProblemRouteMatcher string

const (
	// ProblemRouteProtectedPreHandler matches mounted management routes that
	// pass through session middleware and are not in the permanent auth
	// exempt set; the 401 is produced before any domain handler side effect.
	ProblemRouteProtectedPreHandler ProblemRouteMatcher = "protected_management_pre_handler"
	// ProblemRouteOrdinaryOrAuthTransitionClient matches the pre-handler
	// matcher plus the exact auth transition client routes
	// POST /api/auth/login, POST /api/auth/refresh, POST /api/auth/logout.
	ProblemRouteOrdinaryOrAuthTransitionClient ProblemRouteMatcher = "ordinary_management_or_auth_transition_client"
	// ProblemRouteLoginLogout matches only the exact login/logout routes.
	ProblemRouteLoginLogout ProblemRouteMatcher = "POST /api/auth/login | POST /api/auth/logout"
	// ProblemRouteLogin matches only the exact login route.
	ProblemRouteLogin ProblemRouteMatcher = "POST /api/auth/login"
)

type problemRetryPolicy string

const (
	problemRetryCoordinatorRefreshThenReplayOnce problemRetryPolicy = "coordinator_refresh_then_replay_once"
	problemRetryNever                            problemRetryPolicy = "never"
	problemRetryCorrectCredentials               problemRetryPolicy = "correct_credentials"
	problemRetryOperatorAfterRetryAt             problemRetryPolicy = "operator_after_retry_at"
	problemRetryCoordinatorPublicStatusOnly      problemRetryPolicy = "coordinator_public_status_only"
)

type problemRecoveryKind string

const (
	problemRecoverySessionRefresh       problemRecoveryKind = "session_refresh"
	problemRecoveryPublicAuthBootstrap  problemRecoveryKind = "public_auth_bootstrap"
	problemRecoveryCorrectCredentials   problemRecoveryKind = "correct_credentials"
	problemRecoveryWaitThenResubmit     problemRecoveryKind = "wait_then_resubmit"
	problemRecoveryConfirmPublicStatus  problemRecoveryKind = "confirm_public_status"
	problemRetryAfterForbidden                              = "forbidden"
	problemRetryAfterRequiredSameSource                     = "required_same_source"
	problemRetryAfterOptionalSameSource                     = "optional_same_source_no_request_replay"
)

// AuthLoginLockedDetails is the registered details payload for
// auth_login_locked: authoritative UTC retry instant plus the same-source
// delta seconds. No failure counts or subject existence are exposed.
type AuthLoginLockedDetails struct {
	RetryAt           time.Time `json:"retry_at"`
	RetryAfterSeconds int64     `json:"retry_after_seconds"`
}

// AuthTransitionProblemDetails is the registered details payload for the two
// typed transition 503 codes.
type AuthTransitionProblemDetails struct {
	TransitionState     string `json:"transition_state"` // enabling_fail_closed | rollback_required
	EffectiveGeneration string `json:"effective_generation"`
	Recovery            string `json:"recovery"` // "confirm_public_status"
	RetryAfterSeconds   *int64 `json:"retry_after_seconds"`
}

// authProblemEntry is one row of the auth problem registry. The Go registry,
// the TypeScript known-code decoder, the coordinator classifier and the
// zh-CN catalog are kept exhaustive against this manifest.
type authProblemEntry struct {
	Code           string
	Status         int
	RouteMatcher   ProblemRouteMatcher
	Params         map[string]any
	DetailsSchema  string
	Retry          problemRetryPolicy
	Recovery       problemRecoveryKind
	RetryAfter     string
	SensitiveField []string
}

// authProblemRegistry is the single machine-readable auth problem manifest.
// Wire params are exact empty objects for every entry; sensitive_fields is
// empty for all auth entries (no password, cookie, Authorization, subject
// existence, failure count, throttle key, username or session identity).
var authProblemRegistry = []authProblemEntry{
	{
		Code: ProblemCodeAuthNotAuthenticated, Status: http.StatusUnauthorized,
		RouteMatcher:  ProblemRouteProtectedPreHandler,
		Params:        map[string]any{},
		DetailsSchema: "empty_object",
		Retry:         problemRetryCoordinatorRefreshThenReplayOnce,
		Recovery:      problemRecoverySessionRefresh,
		RetryAfter:    problemRetryAfterForbidden,
	},
	{
		Code: ProblemCodeAuthNotEnabled, Status: http.StatusBadRequest,
		RouteMatcher:  ProblemRouteLoginLogout,
		Params:        map[string]any{},
		DetailsSchema: "empty_object",
		Retry:         problemRetryNever,
		Recovery:      problemRecoveryPublicAuthBootstrap,
		RetryAfter:    problemRetryAfterForbidden,
	},
	{
		Code: ProblemCodeAuthInvalidCredentials, Status: http.StatusUnauthorized,
		RouteMatcher:  ProblemRouteLogin,
		Params:        map[string]any{},
		DetailsSchema: "empty_object",
		Retry:         problemRetryCorrectCredentials,
		Recovery:      problemRecoveryCorrectCredentials,
		RetryAfter:    problemRetryAfterForbidden,
	},
	{
		Code: ProblemCodeAuthLoginLocked, Status: http.StatusTooManyRequests,
		RouteMatcher:  ProblemRouteLogin,
		Params:        map[string]any{},
		DetailsSchema: "AuthLoginLockedDetails",
		Retry:         problemRetryOperatorAfterRetryAt,
		Recovery:      problemRecoveryWaitThenResubmit,
		RetryAfter:    problemRetryAfterRequiredSameSource,
	},
	{
		Code: ProblemCodeAuthTransitionInProgress, Status: http.StatusServiceUnavailable,
		RouteMatcher:  ProblemRouteOrdinaryOrAuthTransitionClient,
		Params:        map[string]any{},
		DetailsSchema: "AuthTransitionProblemDetails(enabling_fail_closed)",
		Retry:         problemRetryCoordinatorPublicStatusOnly,
		Recovery:      problemRecoveryConfirmPublicStatus,
		RetryAfter:    problemRetryAfterOptionalSameSource,
	},
	{
		Code: ProblemCodeAuthTransitionRecoveryNeeded, Status: http.StatusServiceUnavailable,
		RouteMatcher:  ProblemRouteOrdinaryOrAuthTransitionClient,
		Params:        map[string]any{},
		DetailsSchema: "AuthTransitionProblemDetails(rollback_required)",
		Retry:         problemRetryCoordinatorPublicStatusOnly,
		Recovery:      problemRecoveryConfirmPublicStatus,
		RetryAfter:    problemRetryAfterOptionalSameSource,
	},
}

func lookupAuthProblemEntry(code string) (authProblemEntry, bool) {
	for _, entry := range authProblemRegistry {
		if entry.Code == code {
			return entry, true
		}
	}
	return authProblemEntry{}, false
}

// authProblemParams ensures the wire params object is exactly the registered
// empty object for the given code.
func authProblemParams(code string) map[string]any {
	entry, ok := lookupAuthProblemEntry(code)
	if !ok {
		panic(fmt.Sprintf("auth problem code %q is not registered", code))
	}
	params := make(map[string]any, len(entry.Params))
	for key, value := range entry.Params {
		params[key] = value
	}
	return params
}

// transitionProblemDetailsFor builds the registered details payload for a
// transition state. retryAfterSeconds must be nil or a non-negative delta.
func transitionProblemDetailsFor(state string, effectiveGeneration int64, retryAfterSeconds *int64) AuthTransitionProblemDetails {
	return AuthTransitionProblemDetails{
		TransitionState:     state,
		EffectiveGeneration: fmt.Sprintf("%d", effectiveGeneration),
		Recovery:            "confirm_public_status",
		RetryAfterSeconds:   retryAfterSeconds,
	}
}
