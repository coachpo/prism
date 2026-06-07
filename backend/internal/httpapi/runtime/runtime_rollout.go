package runtime

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

const (
	runtimeShadowComparisonResultMismatch = "mismatch"
)

type runtimeShadowComparableOutcome struct {
	resolvedTargetModelID string
	selectedTerminalID    int
	translationMode       string
	contextRejected       bool
	noEligible            bool
	plannerValidation     bool
	hasPlan               bool
	hasError              bool
}

func (s *Service) resolvedPlannerMode() config.RuntimeRoutingPlannerMode {
	if strings.TrimSpace(string(s.plannerMode)) == "" {
		return config.RuntimeRoutingPlannerModeLegacy
	}
	return s.plannerMode
}

func (s *Service) resolvedOpenAITerminalTranslationMode() config.OpenAITerminalTranslationMode {
	if strings.TrimSpace(string(s.openAITerminalTranslationMode)) == "" {
		return config.OpenAITerminalTranslationModeSafeOnly
	}
	return s.openAITerminalTranslationMode
}

func (s *Service) codingAgentFormatBridge() CodingAgentFormatBridge {
	return NewCodingAgentFormatBridge(s.resolvedOpenAITerminalTranslationMode())
}

func resolveRequestedModelLegacy(input requestPlanningInput, operation resolvedRequestOperation) (runtimeModelRecord, error) {
	requestedModel, found := input.Snapshot.ModelsByID[operation.RequestedModelID]
	if !found {
		return runtimeModelRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model '" + operation.RequestedModelID + "' not configured or disabled"}
	}
	if err := validateRuntimeModelFacadePolicies(requestedModel); err != nil {
		return runtimeModelRecord{}, err
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, requestedModel); err != nil {
		return runtimeModelRecord{}, err
	}
	return requestedModel, nil
}

func (s *Service) resolveRequestPlanTargetLegacy(input requestPlanningInput, operation resolvedRequestOperation, requestedModel runtimeModelRecord, contextEstimation *requestContextEstimation) (resolvedExecutionTarget, error) {
	resolved, err := s.resolveLegacyExecutionTargetFromSnapshotWithOptions(input.ActiveProfileID, input.Snapshot, requestedModel, operation.Match.Operation, contextEstimation, input.AllowMissingContextEstimation, s.nowUTC())
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, resolved.TargetModel); err != nil {
		return resolvedExecutionTarget{}, err
	}
	if len(resolved.TerminalAttempts) == 0 {
		return resolvedExecutionTarget{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "No eligible targets available for model '" + operation.RequestedModelID + "'."}
	}
	selectedTerminalTargetID := &resolved.TerminalAttempts[0].Connection.ID
	if resolved.ContextRouting != nil && resolved.ContextRouting.SelectedTerminalTargetID != nil {
		selectedTerminalTargetID = cloneRuntimeIntPointer(resolved.ContextRouting.SelectedTerminalTargetID)
	}
	return resolvedExecutionTarget{
		RequestedModel:           requestedModel,
		TargetModel:              resolved.TargetModel,
		SelectedTerminalTargetID: selectedTerminalTargetID,
		ContextRouting:           cloneRuntimeContextRoutingDecision(resolved.ContextRouting),
		Connections:              resolved.Connections,
		TerminalAttempts:         resolved.TerminalAttempts,
		RuntimeStates:            resolved.RuntimeStates,
		Strategy:                 resolved.Strategy,
	}, nil
}

func (s *Service) buildRequestPlanFromSnapshotLegacyCore(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	input := requestPlanningInput{Request: request, RawBody: rawBody, RuntimeConfig: runtimeConfig, OperationMatch: operationMatch, ActiveProfileID: activeProfileID, Snapshot: snapshot}
	operation, err := resolveRequestOperation(input)
	if err != nil {
		return requestPlan{}, err
	}
	requestedModel, err := resolveRequestedModelLegacy(input, operation)
	if err != nil {
		return requestPlan{}, err
	}
	contextEstimation, contextEstimationErr := estimatePreflightRequestContext(operation.Match.Operation, input.RawBody, requestedModel)
	input.AllowMissingContextEstimation = allowContextEstimationUnavailablePassThrough(operation.Match.Operation, contextEstimationErr)
	target, err := s.resolveRequestPlanTargetLegacy(input, operation, requestedModel, contextEstimation)
	if err != nil {
		var runtimeErr *domainError
		if contextEstimationErr != nil && !input.AllowMissingContextEstimation && (!errors.As(err, &runtimeErr) || runtimeErr == nil || runtimeErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode) {
			return requestPlan{}, contextEstimationErr
		}
		return requestPlan{}, attachRuntimePlanningFailureTelemetry(err, input, operation, requestedModel)
	}
	if contextEstimationErr != nil && !input.AllowMissingContextEstimation {
		return requestPlan{}, contextEstimationErr
	}
	return assembleRequestPlan(input, operation, target, contextEstimation)
}

func (s *Service) buildRequestPlanFromSnapshotEnforcedCore(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return requestPlan{}, err
	}
	input := requestPlanningInput{Request: request, RawBody: rawBody, RuntimeConfig: runtimeConfig, OperationMatch: operationMatch, ActiveProfileID: activeProfileID, Snapshot: snapshot, RoutingPlan: routingPlan}
	operation, err := resolveRequestOperation(input)
	if err != nil {
		return requestPlan{}, err
	}
	requestedModel, err := resolveRequestedModel(input, operation)
	if err != nil {
		return requestPlan{}, err
	}
	contextEstimation, contextEstimationErr := estimatePreflightRequestContext(operation.Match.Operation, input.RawBody, requestedModel)
	input.AllowMissingContextEstimation = allowContextEstimationUnavailablePassThrough(operation.Match.Operation, contextEstimationErr)
	target, err := s.resolveRequestPlanTarget(input, operation, requestedModel, contextEstimation)
	if err != nil {
		var runtimeErr *domainError
		if contextEstimationErr != nil && !input.AllowMissingContextEstimation && (!errors.As(err, &runtimeErr) || runtimeErr == nil || runtimeErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode) {
			return requestPlan{}, contextEstimationErr
		}
		return requestPlan{}, attachRuntimePlanningFailureTelemetry(err, input, operation, requestedModel)
	}
	if contextEstimationErr != nil && !input.AllowMissingContextEstimation {
		return requestPlan{}, contextEstimationErr
	}
	return assembleRequestPlan(input, operation, target, contextEstimation)
}

func shadowComparableOutcomeFromPlan(plan requestPlan) runtimeShadowComparableOutcome {
	translationMode := "none"
	attempts := plan.orderedTerminalAttempts()
	if len(attempts) > 0 {
		translationMode = string(normalizedRuntimeTranslationMode(attempts[0].TranslationMode))
	}
	selectedTerminalID := 0
	if selected := plan.selectedTerminalTargetID(); selected != nil {
		selectedTerminalID = *selected
	}
	return runtimeShadowComparableOutcome{
		resolvedTargetModelID: strings.TrimSpace(dereferenceString(plan.ResolvedTargetModelID)),
		selectedTerminalID:    selectedTerminalID,
		translationMode:       translationMode,
		hasPlan:               true,
	}
}

func shadowComparableOutcomeFromError(err error) runtimeShadowComparableOutcome {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil {
		return runtimeShadowComparableOutcome{hasError: err != nil}
	}
	translationMode := stringValue(domainErr.Fields["translation_mode"])
	plannerValidation := strings.HasPrefix(strings.TrimSpace(domainErr.Detail), "Invalid runtime routing plan:")
	noEligible := strings.Contains(strings.TrimSpace(domainErr.Detail), "No eligible targets available for model '")
	return runtimeShadowComparableOutcome{
		resolvedTargetModelID: strings.TrimSpace(dereferenceString(domainErr.ResolvedTargetModelID)),
		translationMode:       strings.TrimSpace(translationMode),
		contextRejected:       domainErr.ErrorCode == contextWindowExceededErrorCode || domainErr.StatusCode == http.StatusRequestEntityTooLarge,
		noEligible:            noEligible,
		plannerValidation:     plannerValidation,
		hasError:              true,
	}
}

func compareShadowOutcomes(servedPlan *requestPlan, servedErr error, shadowPlan *requestPlan, shadowErr error) *runtimeShadowComparisonResult {
	served := runtimeShadowComparableOutcome{}
	shadow := runtimeShadowComparableOutcome{}
	if servedErr != nil {
		served = shadowComparableOutcomeFromError(servedErr)
	} else if servedPlan != nil {
		served = shadowComparableOutcomeFromPlan(*servedPlan)
	}
	if shadowErr != nil {
		shadow = shadowComparableOutcomeFromError(shadowErr)
	} else if shadowPlan != nil {
		shadow = shadowComparableOutcomeFromPlan(*shadowPlan)
	}
	reasons := make([]string, 0, 5)
	if served.hasPlan != shadow.hasPlan || served.hasError != shadow.hasError {
		if served.contextRejected || shadow.contextRejected {
			reasons = append(reasons, "context_rejection")
		}
		if served.plannerValidation || shadow.plannerValidation {
			reasons = append(reasons, "planner_validation")
		}
		if served.noEligible || shadow.noEligible {
			reasons = append(reasons, "eligibility")
		}
	}
	if served.resolvedTargetModelID != shadow.resolvedTargetModelID {
		reasons = append(reasons, "resolved_model")
	}
	if served.selectedTerminalID != shadow.selectedTerminalID {
		reasons = append(reasons, "selected_connection")
	}
	if served.translationMode != shadow.translationMode {
		reasons = append(reasons, "translation_mode")
	}
	if len(reasons) == 0 {
		return nil
	}
	return &runtimeShadowComparisonResult{Result: runtimeShadowComparisonResultMismatch, MismatchReasons: dedupeShadowMismatchReasons(reasons)}
}

func dedupeShadowMismatchReasons(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	ordered := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		trimmed := strings.TrimSpace(reason)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		ordered = append(ordered, trimmed)
	}
	return ordered
}

func annotatePlannerTraceRollout(plan requestPlan, plannerMode config.RuntimeRoutingPlannerMode, comparison *runtimeShadowComparisonResult) requestPlan {
	policy := normalizedRuntimeLegacyStrategyType(plan.Strategy)
	selectedTerminalTargetID := plan.selectedTerminalTargetID()
	contextRouting := cloneRuntimeContextRoutingDecision(plan.ContextRouting)
	if contextRouting == nil {
		contextRouting = &runtimeContextRoutingDecision{Policy: policy, SelectedTerminalTargetID: cloneRuntimeIntPointer(selectedTerminalTargetID)}
	}
	if contextRouting.PlannerTrace == nil {
		contextRouting.PlannerTrace = &runtimePlannerTraceDecision{PlannerVersion: runtimePlannerTraceVersion, Decision: runtimePlannerTraceDecisionSelected, Policy: policy, SelectedTerminalTargetID: cloneRuntimeIntPointer(selectedTerminalTargetID)}
	}
	contextRouting.PlannerTrace.PlannerMode = string(plannerMode)
	if comparison != nil {
		contextRouting.PlannerTrace.ShadowComparisonResult = &runtimeShadowComparisonResult{Result: comparison.Result, MismatchReasons: append([]string(nil), comparison.MismatchReasons...)}
	}
	plan.ContextRouting = contextRouting
	return plan
}

func annotatePlannerErrorRollout(err error, plannerMode config.RuntimeRoutingPlannerMode, comparison *runtimeShadowComparisonResult) error {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil {
		return err
	}
	contextRouting := cloneRuntimeContextRoutingDecision(domainErr.ContextRouting)
	if contextRouting == nil {
		contextRouting = &runtimeContextRoutingDecision{SelectedTerminalTargetID: cloneRuntimeIntPointer(domainErr.SelectedTerminalTargetID)}
	}
	if contextRouting.PlannerTrace == nil {
		contextRouting.PlannerTrace = &runtimePlannerTraceDecision{PlannerVersion: runtimePlannerTraceVersion, SelectedTerminalTargetID: cloneRuntimeIntPointer(contextRouting.SelectedTerminalTargetID)}
	}
	contextRouting.PlannerTrace.PlannerMode = string(plannerMode)
	if comparison != nil {
		contextRouting.PlannerTrace.ShadowComparisonResult = &runtimeShadowComparisonResult{Result: comparison.Result, MismatchReasons: append([]string(nil), comparison.MismatchReasons...)}
	}
	domainErr.ContextRouting = contextRouting
	return err
}

func logShadowComparisonMismatch(requestedModelID string, comparison *runtimeShadowComparisonResult) {
	if comparison == nil {
		return
	}
	slog.Debug("runtime planner shadow comparison mismatch", "requested_model_id", strings.TrimSpace(requestedModelID), "result", comparison.Result, "mismatch_reasons", comparison.MismatchReasons)
}
