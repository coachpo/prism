package runtime

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

const runtimeAdmissionExhaustedErrorCode = "admission_exhausted"

const runtimeAttemptBudgetExhaustedErrorCode = "attempt_budget_exhausted"

type executionAttempt struct {
	Connection                  runtimeConnection
	ResolvedTargetModelID       string
	RequestURL                  string
	RequestHeaders              map[string]string
	RequestBody                 []byte
	ResponseHeaders             http.Header
	StatusCode                  int
	ResponseTimeMS              int
	ResponseHeadersLatencyMS    int
	CompletedAt                 time.Time
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	UpstreamOperationName       string
	UpstreamRequestPath         string
	OperationTranslationMode    TranslationMode
	RequestGenerationParams     *requestGenerationParamsSnapshot

	// Attempt lifecycle (Observe SPEC §3.5): frozen at the launch site.
	LaunchOrdinal              int
	AttemptTrigger             string
	AttemptResult              string
	IsWinner                   bool
	AttemptDurationMS          int
	UpstreamRequestStarted     bool
	ResponseHeadersReceived    bool
	FirstBodyOrStreamEventSeen bool
	StreamOutcome              string
	StreamErrorKind            *string
	StreamErrorDetail          *string

	// Failure diagnostics: safe bounded projection. For intermediate
	// failover-eligible non-2xx responses, the sampler fills this asynchronously;
	// the telemetry sealer uses a generic fallback when the sampler has not
	// completed by sealing time.
	Diagnostics *attemptFailureDiagnostics
	Sampler     *failedResponseSampler
}

func (attempt executionAttempt) diagnosticsOrFallback(statusCode int) attemptFailureDiagnostics {
	if attempt.Diagnostics != nil {
		return *attempt.Diagnostics
	}
	if attempt.Sampler != nil && attempt.Sampler.result != nil {
		if diagnostic, ok := attempt.Sampler.result.result(); ok {
			return diagnostic
		}
	}
	return attemptFailureDiagnostics{
		Source: errorSourceUpstream,
		Stage:  failureStageUpstreamResponse,
		Code:   stableHTTPErrorCode(statusCode, ""),
		Detail: fmt.Sprintf("upstream returned HTTP %d", statusCode),
	}
}

type executionResult struct {
	Response                    *http.Response
	Connection                  runtimeConnection
	RequestHeaders              map[string]string
	ResolvedTargetModelID       *string
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	AttemptCount                int
	Attempts                    []executionAttempt
	RouteReason                 gatewaycore.RouteReason
	// WinnerOrdinal is the launch ordinal of the selected response, or 0 when
	// no attempt produced a selectable response (all failed / zero launched).
	WinnerOrdinal int
}

type hedgedExecutionResult struct {
	Winner              *executionOutcome
	Attempts            []executionAttempt
	LaunchedAttempts    int
	AdmissionRejections int
	LastAdmissionReason string
	RouteReason         gatewaycore.RouteReason
	// LastError is the normalized failure classification label; it never
	// contains an upstream address.
	LastError           string
	ConsumedConnections int
}

type hedgedAttemptResult struct {
	Order   int
	Outcome executionOutcome
}

type requestExecutionLimits struct {
	HedgePolicy loadbalance.RuntimeHedgePolicy
	MaxAttempts int
}

type requestExecutionState struct {
	launchedAttempts int
	attempts         []executionAttempt
	// lastError is the normalized failure classification label; it never
	// contains an upstream address.
	lastError           string
	lastAdmissionReason string
	routeReason         gatewaycore.RouteReason
	admissionRejections int
	hedgeUsed           bool
	// nextLaunchOrdinal is the immutable 1-based launch ordinal to assign to
	// the next real upstream launch within this ingress.
	nextLaunchOrdinal int
	// lastLaunchedConnectionID and lastLaunchedTrigger track the previous
	// launch so retry_same_target vs failover can be classified from persisted
	// executor evidence, not inferred later from display fields.
	lastLaunchedConnectionID int
	lastLaunchedTrigger      string
}

var errHedgeLoserCanceled = errors.New("hedge loser canceled")

const hedgeCanceledAttemptStatusCode = 499

func newRequestExecutionState(plan requestPlan) requestExecutionState {
	return requestExecutionState{
		attempts:          make([]executionAttempt, 0, len(plan.orderedTerminalAttempts())),
		routeReason:       gatewaycore.RouteReasonDirectMatch,
		nextLaunchOrdinal: 1,
	}
}

// nextLaunchTrigger classifies the trigger for the next launch from persisted
// executor evidence: the first launch is `initial`; a later launch to the same
// connection is `retry_same_target`; a hedge launch is `hedge`; any other
// later launch is `failover`. The winner's entry lineage is classified at the
// launch site, never inferred from completion order. The immutable launch
// ordinal is stamped only when the attempt actually launches.
func (state *requestExecutionState) nextLaunchTrigger(plan requestPlan, index int, terminalAttempt runtimeTerminalAttempt) string {
	trigger := attemptTriggerInitial
	switch {
	case state.hedgeUsed:
		trigger = attemptTriggerHedge
	case state.launchedAttempts > 0:
		if state.lastLaunchedConnectionID == terminalAttempt.Connection.ID {
			trigger = attemptTriggerRetrySameTarget
		} else {
			trigger = attemptTriggerFailover
		}
	}
	return trigger
}

func requestExecutionLimitsForPlan(plan requestPlan) requestExecutionLimits {
	hedgePolicy := plan.Strategy.HedgePolicy()
	maxAttempts := len(plan.orderedTerminalAttempts())
	return requestExecutionLimits{HedgePolicy: hedgePolicy, MaxAttempts: maxAttempts}
}

func (limits requestExecutionLimits) remainingLaunchCapacity(state requestExecutionState) int {
	return limits.MaxAttempts - state.launchedAttempts
}

func (limits requestExecutionLimits) shouldHedge(plan requestPlan, state requestExecutionState, index int) bool {
	return !state.hedgeUsed && limits.HedgePolicy.Enabled && limits.remainingLaunchCapacity(state) >= 2 && len(plan.orderedTerminalAttempts())-index >= 2
}

func runtimeAdmissionRouteReason(reason string) gatewaycore.RouteReason {
	switch strings.TrimSpace(reason) {
	case "qps_limit":
		return gatewaycore.RouteReasonQPSOverflow
	case "max_in_flight_stream", "max_in_flight_non_stream":
		return gatewaycore.RouteReasonConcurrencyOverflow
	default:
		return gatewaycore.RouteReasonPolicyReject
	}
}

func runtimeExecutionRouteReason(reason gatewaycore.RouteReason) gatewaycore.RouteReason {
	switch reason {
	case gatewaycore.RouteReasonModelRedirect,
		gatewaycore.RouteReasonUpstreamRedirect,
		gatewaycore.RouteReasonQPSOverflow,
		gatewaycore.RouteReasonRPMOverflow,
		gatewaycore.RouteReasonTPMOverflow,
		gatewaycore.RouteReasonIPMOverflow,
		gatewaycore.RouteReasonConcurrencyOverflow,
		gatewaycore.RouteReasonRetry429,
		gatewaycore.RouteReasonRetry5xx,
		gatewaycore.RouteReasonRetryHTTP,
		gatewaycore.RouteReasonRetryConnectTimeout,
		gatewaycore.RouteReasonRetryTransport,
		gatewaycore.RouteReasonCircuitOpenSkip,
		gatewaycore.RouteReasonNoHealthyUpstream,
		gatewaycore.RouteReasonPolicyReject:
		return reason
	default:
		return gatewaycore.RouteReasonDirectMatch
	}
}

func (state *requestExecutionState) recordHedgedResult(hedged hedgedExecutionResult) {
	state.hedgeUsed = true
	state.launchedAttempts += hedged.LaunchedAttempts
	state.attempts = append(state.attempts, hedged.Attempts...)
	for _, attempt := range hedged.Attempts {
		if attempt.LaunchOrdinal >= state.nextLaunchOrdinal {
			state.nextLaunchOrdinal = attempt.LaunchOrdinal + 1
		}
		if validateAttemptTrigger(attempt.AttemptTrigger) {
			state.lastLaunchedTrigger = attempt.AttemptTrigger
			state.lastLaunchedConnectionID = attempt.Connection.ID
		}
	}
	state.admissionRejections += hedged.AdmissionRejections
	if strings.TrimSpace(hedged.LastAdmissionReason) != "" {
		state.lastAdmissionReason = hedged.LastAdmissionReason
	}
	if hedged.RouteReason != "" {
		state.routeReason = runtimeExecutionRouteReason(hedged.RouteReason)
	}
	if strings.TrimSpace(hedged.LastError) != "" {
		state.lastError = hedged.LastError
	}
}

func (state *requestExecutionState) recordAdmissionRejection(reason string) {
	state.admissionRejections++
	state.lastAdmissionReason = reason
	state.routeReason = runtimeAdmissionRouteReason(reason)
}

func (state *requestExecutionState) recordRetry(reason gatewaycore.RouteReason) {
	state.routeReason = runtimeExecutionRouteReason(reason)
}

func (state *requestExecutionState) recordLaunchedAttempt(outcome executionOutcome) {
	state.launchedAttempts++
	if outcome.Attempt.LaunchOrdinal >= state.nextLaunchOrdinal {
		state.nextLaunchOrdinal = outcome.Attempt.LaunchOrdinal + 1
	}
	if validateAttemptTrigger(outcome.Attempt.AttemptTrigger) {
		state.lastLaunchedTrigger = outcome.Attempt.AttemptTrigger
		state.lastLaunchedConnectionID = outcome.Attempt.Connection.ID
	}
	state.attempts = append(state.attempts, outcome.Attempt)
}

func (state *requestExecutionState) result(plan requestPlan, outcome executionOutcome) executionResult {
	result := executionResult{
		Response:                    outcome.Response,
		Connection:                  outcome.Connection,
		RequestHeaders:              outcome.RequestHeaders,
		ResolvedTargetModelID:       stringPointerIfNotEmpty(outcome.TerminalAttempt.TargetModel.ModelID),
		AuditEnabledAtRequest:       outcome.TerminalAttempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: outcome.TerminalAttempt.AuditCaptureBodiesRequest,
		AttemptCount:                state.launchedAttempts,
		Attempts:                    state.attempts,
		RouteReason:                 runtimeExecutionRouteReason(state.routeReason),
	}
	if outcome.Launched {
		result.WinnerOrdinal = outcome.Attempt.LaunchOrdinal
	}
	return result
}

func (state *requestExecutionState) failureResult(plan requestPlan) (executionResult, error) {
	if len(plan.orderedTerminalAttempts()) == 0 {
		return executionResult{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", plan.RequestedModelID)}
	}
	if state.launchedAttempts == 0 && state.admissionRejections > 0 {
		routeReason := runtimeExecutionRouteReason(state.routeReason)
		detail := fmt.Sprintf("All connections rejected for model '%s' because admission limits are exhausted.", plan.RequestedModelID)
		if strings.TrimSpace(state.lastAdmissionReason) != "" {
			detail = fmt.Sprintf("All connections rejected for model '%s' because admission limit '%s' is exhausted.", plan.RequestedModelID, state.lastAdmissionReason)
		}
		result := executionResult{AttemptCount: state.launchedAttempts, Attempts: state.attempts, RouteReason: routeReason}
		return result, &domainError{
			StatusCode:               http.StatusServiceUnavailable,
			ErrorCode:                runtimeAdmissionExhaustedErrorCode,
			Detail:                   detail,
			Fields:                   map[string]any{"route_reason": string(routeReason)},
			ResolvedTargetModelID:    cloneRuntimeStringPointer(plan.ResolvedTargetModelID),
			SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
		}
	}
	lastFailure := strings.TrimSpace(state.lastError)
	if lastFailure == "" {
		lastFailure = "unknown_upstream_failure"
	}
	// All launched transport attempts failed: the executor preserves every
	// launched upstream row (trigger, target identity, duration, safe
	// transport detail) and materializes a finalized usage/event summary with
	// gateway 502 (Requests SPEC §4.6). Skipped candidates are never turned
	// into attempts; no synthetic "final 502 attempt" row is constructed.
	result := executionResult{
		AttemptCount: state.launchedAttempts,
		Attempts:     state.attempts,
		RouteReason:  runtimeExecutionRouteReason(state.routeReason),
	}
	return result, &domainError{
		StatusCode:               http.StatusBadGateway,
		ErrorCode:                "transport_error",
		Detail:                   fmt.Sprintf("All connections failed for model '%s'. Last failure: %s.", plan.RequestedModelID, lastFailure),
		Fields:                   map[string]any{"route_reason": string(result.RouteReason), "last_failure": lastFailure},
		ResolvedTargetModelID:    cloneRuntimeStringPointer(plan.ResolvedTargetModelID),
		SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
	}
}

// budgetExhaustedResult terminates the ingress when the 64-attempt launch
// safety bound is reached: gateway 503 + typed attempt_budget_exhausted with
// every already-launched attempt preserved. The finalized usage summary
// carries the terminal code; no 65th upstream row is ever constructed.
func (state *requestExecutionState) budgetExhaustedResult(plan requestPlan) (executionResult, error) {
	result := executionResult{
		AttemptCount: state.launchedAttempts,
		Attempts:     state.attempts,
		RouteReason:  runtimeExecutionRouteReason(state.routeReason),
	}
	return result, formatAttemptBudgetError(plan.RequestedModelID)
}

// attemptBudgetExhaustedResult terminates the ingress when the fixed 64-attempt
// launch cap is reached: gateway 503 with the typed attempt_budget_exhausted
// code, preserving the launched attempts already recorded.
func (state *requestExecutionState) attemptBudgetExhaustedResult(plan requestPlan) (executionResult, error) {
	routeReason := runtimeExecutionRouteReason(state.routeReason)
	result := executionResult{AttemptCount: state.launchedAttempts, Attempts: state.attempts, RouteReason: routeReason}
	return result, &domainError{
		StatusCode:               http.StatusServiceUnavailable,
		ErrorCode:                runtimeAttemptBudgetExhaustedErrorCode,
		Detail:                   fmt.Sprintf("Model '%s' exceeded the maximum of %d launched upstream attempts for a single ingress request.", plan.RequestedModelID, MaxLaunchedUpstreamAttempts),
		Fields:                   map[string]any{"route_reason": string(routeReason), "attempt_limit": MaxLaunchedUpstreamAttempts},
		ResolvedTargetModelID:    cloneRuntimeStringPointer(plan.ResolvedTargetModelID),
		SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
	}
}
