package runtime

import (
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func normalizedRuntimeTranslationMode(mode TranslationMode) TranslationMode {
	if strings.TrimSpace(string(mode)) == "" {
		return TranslationModeNone
	}
	return mode
}

func runtimeTranslationModePointer(mode TranslationMode) *string {
	normalized := normalizedRuntimeTranslationMode(mode)
	if strings.TrimSpace(string(normalized)) == "" {
		return nil
	}
	return stringPtr(string(normalized))
}

func runtimeUpstreamOperationName(operation RuntimeOperation, _ TranslationMode) string {
	return strings.TrimSpace(operation.Name)
}

func runtimeUpstreamRequestPathTemplate(operation RuntimeOperation, _ TranslationMode) string {
	return strings.TrimSpace(operation.PathTemplate)
}

func runtimeUpstreamRequestPath(operation RuntimeOperation, mode TranslationMode, effectivePath string) *string {
	trimmed := strings.TrimSpace(effectivePath)
	if trimmed != "" {
		return stringPtr(trimmed)
	}
	trimmed = runtimeUpstreamRequestPathTemplate(operation, mode)
	if trimmed == "" {
		return nil
	}
	return stringPtr(trimmed)
}

type requestPlan struct {
	RequestedModelID            string
	ResolvedTargetModelID       *string
	ResolvedPricingModelID      string
	RequestedVendorID           *int
	RequestedVendorKey          *string
	RequestedVendorName         *string
	ProfileID                   int
	APIFamily                   string
	RuntimeOperation            RuntimeOperation
	RuntimeOperationPathParams  map[string]string
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	ReportCurrencySnapshot      runtimeReportCurrencySnapshot
	EffectiveRequestPath        string
	RawRequestBody              []byte
	UpstreamBody                []byte
	IsStreamingRequest          bool
	SelectedTerminalTargetID    *int
	TerminalAttempts            []runtimeTerminalAttempt
	Connections                 []runtimeConnection
	RuntimeStates               map[int]loadbalance.RuntimeConnectionState
	BlocklistRules              []headerBlocklistRule
	ClientHeaders               map[string]string
	FailoverStatusCodes         []int
	Strategy                    loadbalance.RuntimeStrategy
	RequestGenerationParams     requestGenerationParamsSnapshot
	RequestGenerationSnapshot   func() requestGenerationParamsSnapshot
	HTTPClient                  *http.Client
}

func (plan requestPlan) requiresReplayableRequestBody() bool {
	attempts := plan.orderedTerminalAttempts()
	if len(attempts) > 1 {
		return true
	}
	for _, attempt := range attempts {
		if attempt.Connection.CustomRequestParameters != nil && !attempt.Connection.CustomRequestParameters.IsEmpty() {
			return true
		}
	}
	return false
}

// requiresCustomRequestParametersOverlay reports whether any planned terminal
// target candidate carries a non-empty custom request parameters
// configuration. When true, the incoming body must be buffered, verified as a
// JSON object, and re-materialized per attempt before provider transport.
func (plan requestPlan) requiresCustomRequestParametersOverlay() bool {
	for _, attempt := range plan.orderedTerminalAttempts() {
		if attempt.Connection.CustomRequestParameters != nil && !attempt.Connection.CustomRequestParameters.IsEmpty() {
			return true
		}
	}
	return false
}

func (plan requestPlan) selectedTerminalTargetID() *int {
	if plan.SelectedTerminalTargetID != nil {
		return cloneRuntimeIntPointer(plan.SelectedTerminalTargetID)
	}
	attempts := plan.orderedTerminalAttempts()
	if len(attempts) == 0 {
		return nil
	}
	return intPtr(attempts[0].Connection.ID)
}

func (plan requestPlan) orderedTerminalAttempts() []runtimeTerminalAttempt {
	if len(plan.TerminalAttempts) > 0 {
		return plan.TerminalAttempts
	}
	attempts := make([]runtimeTerminalAttempt, 0, len(plan.Connections))
	for _, connection := range plan.Connections {
		attempts = append(attempts, runtimeTerminalAttempt{
			TargetModel:               runtimeModelRecord{ModelID: dereferenceString(plan.ResolvedTargetModelID), APIFamily: plan.APIFamily, AuditEnabled: plan.AuditEnabledAtRequest, AuditCaptureBodies: plan.AuditCaptureBodiesAtRequest},
			Connection:                connection,
			Strategy:                  plan.Strategy,
			EffectiveRequestPath:      plan.EffectiveRequestPath,
			UpstreamBody:              plan.UpstreamBody,
			AuditEnabledAtRequest:     plan.AuditEnabledAtRequest,
			AuditCaptureBodiesRequest: plan.AuditCaptureBodiesAtRequest,
		})
	}
	return attempts
}

func (plan requestPlan) RequestGenerationParamsSnapshot() requestGenerationParamsSnapshot {
	if plan.RequestGenerationSnapshot != nil {
		return plan.RequestGenerationSnapshot().clone()
	}
	return plan.RequestGenerationParams.clone()
}

type requestPlanningInput struct {
	Request         *http.Request
	RawBody         []byte
	RuntimeConfig   RuntimeProxyConfigSnapshot
	OperationMatch  RuntimeOperationMatch
	ActiveProfileID int
	Snapshot        *planningSnapshot
	RoutingPlan     *runtimeRoutingPlan
	// ReferenceNow is the single planning clock of this ingress, captured
	// once at the runtime-operation boundary and shared by the probe plan
	// and the final plan (Gemini path-bound requests plan twice with an
	// upstream body read in between). Routing eligibility must never read
	// the live clock; execution-phase admission and Ban re-checks
	// deliberately keep reading it.
	ReferenceNow time.Time
	// ProbePlanning marks the rawBody == nil Gemini path-bound probe phase:
	// the plan only decides whether the incoming body must be buffered, so
	// custom-request-parameter overlay and object validation must be skipped.
	ProbePlanning bool
}

func (input requestPlanningInput) compiledRoutingPlan() (*runtimeRoutingPlan, error) {
	if input.RoutingPlan != nil {
		return input.RoutingPlan, nil
	}
	return input.Snapshot.compiledRoutingPlan()
}

type runtimePlanningFailureTelemetry struct {
	ProfileID                   int
	RequestedModelID            string
	RequestedVendorID           *int
	RequestedVendorKey          *string
	RequestedVendorName         *string
	APIFamily                   string
	RuntimeOperation            RuntimeOperation
	UpstreamOperationName       *string
	RequestPath                 string
	UpstreamRequestPath         *string
	OperationTranslationMode    *string
	IsStreamingRequest          bool
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	ReportCurrencySnapshot      runtimeReportCurrencySnapshot
	RequestGenerationParams     requestGenerationParamsSnapshot
	SelectedTerminalTargetID    *int
}

type resolvedRequestOperation struct {
	Match            RuntimeOperationMatch
	ContentType      string
	RequestedModelID string
}

type resolvedExecutionTarget struct {
	RequestedModel           runtimeModelRecord
	TargetModel              runtimeModelRecord
	SelectedTerminalTargetID *int
	Connections              []runtimeConnection
	TerminalAttempts         []runtimeTerminalAttempt
	RuntimeStates            map[int]loadbalance.RuntimeConnectionState
	Strategy                 loadbalance.RuntimeStrategy
}

type plannedUpstreamRequest struct {
	EffectiveRequestPath    string
	RawRequestBody          []byte
	UpstreamBody            []byte
	IsStreamingRequest      bool
	ClientHeaders           map[string]string
	RequestGenerationParams requestGenerationParamsSnapshot
}

type runtimeTerminalAttempt struct {
	TargetModel               runtimeModelRecord
	Connection                runtimeConnection
	Strategy                  loadbalance.RuntimeStrategy
	TranslationMode           TranslationMode
	EffectiveRequestPath      string
	UpstreamBody              []byte
	RequestGenerationParams   requestGenerationParamsSnapshot
	AuditEnabledAtRequest     bool
	AuditCaptureBodiesRequest bool
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	maps.Copy(cloned, source)
	return cloned
}

func stringPointerIfNotEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
