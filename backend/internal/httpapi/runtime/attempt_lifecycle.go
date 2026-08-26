package runtime

import (
	"fmt"
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

// Attempt lifecycle constants (Requests SPEC §3.4/§4.5, Observe SPEC §3.5).
const (
	attemptTriggerInitial         = "initial"
	attemptTriggerRetrySameTarget = "retry_same_target"
	attemptTriggerHedge           = "hedge"
	attemptTriggerFailover        = "failover"

	attemptResultCompleted          = "completed"
	attemptResultHTTPError          = "http_error"
	attemptResultStreamError        = "stream_error"
	attemptResultTransportError     = "transport_error"
	attemptResultCancelled          = "cancelled"
	attemptResultClientDisconnected = "client_disconnected"
	attemptResultUnknown            = "unknown"

	// MaxLaunchedUpstreamAttempts is the executor hard safety bound (64 per
	// ingress). Reaching it terminates further launches with a gateway 503 and
	// typed attempt_budget_exhausted; no 65th upstream row is ever constructed.
	MaxLaunchedUpstreamAttempts = 64
)

// runtimeAttemptLifecycle is frozen at the launch site before provider
// transport begins.
type runtimeAttemptLifecycle struct {
	LaunchOrdinal  int
	AttemptTrigger string
}

// validateAttemptTrigger ensures only the four fixed trigger values are
// persisted for new upstream rows.
func validateAttemptTrigger(trigger string) bool {
	switch trigger {
	case attemptTriggerInitial, attemptTriggerRetrySameTarget, attemptTriggerHedge, attemptTriggerFailover:
		return true
	default:
		return false
	}
}

// formatAttemptBudgetError builds the typed launch-safety error.
func formatAttemptBudgetError(modelID string) error {
	return &domainError{
		StatusCode: http.StatusServiceUnavailable,
		ErrorCode:  safediag.CodeAttemptBudgetExhausted,
		Detail:     fmt.Sprintf("Launch safety bound reached for model '%s': maximum of %d upstream attempts per ingress.", modelID, MaxLaunchedUpstreamAttempts),
		Fields:     map[string]any{"max_launched_upstream_attempts": MaxLaunchedUpstreamAttempts},
	}
}
