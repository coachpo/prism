package runtime

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

func (s *Service) buildRequestPlanFromSnapshotCore(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return requestPlan{}, err
	}
	input := requestPlanningInput{
		Request:         request,
		RawBody:         rawBody,
		RuntimeConfig:   runtimeConfig,
		OperationMatch:  operationMatch,
		ActiveProfileID: activeProfileID,
		Snapshot:        snapshot,
		RoutingPlan:     routingPlan,
	}
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
	input.ContextEstimationUnavailableReason = contextEstimationUnavailableReasonFromError(contextEstimationErr)
	target, err := s.resolveRequestPlanTargetWithRecursiveContextOverflow(input, operation, requestedModel, contextEstimation, contextEstimationErr)
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

func (s *Service) resolveRequestPlanTargetWithRecursiveContextOverflow(input requestPlanningInput, operation resolvedRequestOperation, requestedModel runtimeModelRecord, contextEstimation *requestContextEstimation, contextEstimationErr error) (resolvedExecutionTarget, error) {
	normalTarget, normalErr := s.resolveRequestPlanTarget(input, operation, requestedModel, contextEstimation)
	if contextEstimationErr != nil {
		if normalErr != nil {
			return normalTarget, normalErr
		}
		if input.AllowMissingContextEstimation {
			normalTarget.RecursivePlanner = newRuntimeRecursiveContextOverflowStoppedResult([]string{requestedModel.ModelID}, 0, runtimeRecursiveContextOverflowStopReasonEstimationUnavailable)
		}
		return normalTarget, nil
	}
	if normalErr != nil {
		if !isContextWindowExceededPlanningError(normalErr) || contextEstimation == nil {
			return normalTarget, normalErr
		}
		return s.resolveRecursiveContextOverflowPromotionTarget(input, operation, requestedModel, nil, normalErr, contextEstimation, []string{requestedModel.ModelID}, map[string]struct{}{requestedModel.ModelID: {}}, 0)
	}
	if contextEstimation == nil {
		return normalTarget, nil
	}
	fittingNormalTarget, fitStatus := fittingResolvedTargetForRecursiveContextOverflow(normalTarget, contextEstimation)
	switch fitStatus {
	case runtimeRecursiveContextFitStatusFits:
		return fittingNormalTarget, nil
	case runtimeRecursiveContextFitStatusMissingWindow:
		normalTarget.RecursivePlanner = newRuntimeRecursiveContextOverflowStoppedResult([]string{requestedModel.ModelID}, 0, runtimeRecursiveContextOverflowStopReasonMissingContextWindow)
		return normalTarget, nil
	}
	return s.resolveRecursiveContextOverflowPromotionTarget(input, operation, requestedModel, &normalTarget, nil, contextEstimation, []string{requestedModel.ModelID}, map[string]struct{}{requestedModel.ModelID: {}}, 0)
}

func (s *Service) resolveRecursiveContextOverflowPromotionTarget(input requestPlanningInput, operation resolvedRequestOperation, currentModel runtimeModelRecord, fallbackTarget *resolvedExecutionTarget, fallbackErr error, contextEstimation *requestContextEstimation, promotionChain []string, visitedModelIDs map[string]struct{}, depth int) (resolvedExecutionTarget, error) {
	promotionTargetID := ""
	if currentModel.ContextOverflowPromotionTargetID != nil {
		promotionTargetID = strings.TrimSpace(*currentModel.ContextOverflowPromotionTargetID)
	}
	if promotionTargetID == "" {
		if fallbackTarget != nil {
			return *fallbackTarget, nil
		}
		return resolvedExecutionTarget{}, fallbackErr
	}
	if depth >= runtimeRecursiveContextOverflowMaxTransitions {
		return resolvedExecutionTarget{}, recursiveContextOverflowPlanningError(runtimeRecursiveContextOverflowStopReasonMaxDepth, promotionChain, depth)
	}
	if _, seen := visitedModelIDs[promotionTargetID]; seen {
		return resolvedExecutionTarget{}, recursiveContextOverflowPlanningError(runtimeRecursiveContextOverflowStopReasonCycle, appendRuntimeModelPath(promotionChain, promotionTargetID), depth)
	}

	promotedModel, err := resolveRequestedModelByID(input, operation, promotionTargetID)
	if err != nil {
		if fallbackTarget != nil {
			return *fallbackTarget, nil
		}
		return resolvedExecutionTarget{}, fallbackErr
	}
	promotedDepth := depth + 1
	promotedChain := appendRuntimeModelPath(promotionChain, promotedModel.ModelID)
	promotedVisited := cloneVisitedRuntimeModelIDsByString(visitedModelIDs)
	promotedVisited[promotedModel.ModelID] = struct{}{}
	if err := validateRecursivePromotionFacadeTarget(input, promotedModel); err != nil {
		return resolvedExecutionTarget{}, err
	}

	promotedTarget, promotedErr := s.resolveRequestPlanTarget(input, operation, promotedModel, contextEstimation)
	if promotedErr != nil {
		if isRuntimeFacadeGuardrailPlanningError(promotedErr) {
			return resolvedExecutionTarget{}, promotedErr
		}
		if !isContextWindowExceededPlanningError(promotedErr) {
			if fallbackTarget != nil {
				return *fallbackTarget, nil
			}
			return resolvedExecutionTarget{}, fallbackErr
		}
		return s.resolveRecursiveContextOverflowPromotionTarget(input, operation, promotedModel, fallbackTarget, fallbackErr, contextEstimation, promotedChain, promotedVisited, promotedDepth)
	}

	fittingPromotedTarget, fitStatus := fittingResolvedTargetForRecursiveContextOverflow(promotedTarget, contextEstimation)
	switch fitStatus {
	case runtimeRecursiveContextFitStatusFits:
		fittingPromotedTarget.RequestedModel = requestedModelForRecursivePromotion(input, operation, promotedChain)
		fittingPromotedTarget.ContextRouting = attachRecursivePreDispatchPromotionRouting(fallbackTarget, fittingPromotedTarget, contextEstimation)
		fittingPromotedTarget.RecursivePlanner = &runtimeRecursiveContextOverflowPlannerResult{PromotionChain: cloneRuntimeModelPath(promotedChain), Depth: promotedDepth, Promoted: true}
		return fittingPromotedTarget, nil
	case runtimeRecursiveContextFitStatusMissingWindow:
		if fallbackTarget != nil {
			stopped := *fallbackTarget
			stopped.RecursivePlanner = newRuntimeRecursiveContextOverflowStoppedResult(promotedChain, promotedDepth, runtimeRecursiveContextOverflowStopReasonMissingContextWindow)
			return stopped, nil
		}
		return resolvedExecutionTarget{}, fallbackErr
	}
	return s.resolveRecursiveContextOverflowPromotionTarget(input, operation, promotedModel, fallbackTarget, fallbackErr, contextEstimation, promotedChain, promotedVisited, promotedDepth)
}

func validateRecursivePromotionFacadeTarget(input requestPlanningInput, promotedModel runtimeModelRecord) error {
	if !isRuntimeExactOpenAIFacadeModel(promotedModel) {
		return nil
	}
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return err
	}
	return validateRuntimeExactFacadeModelTargets(routingPlan, promotedModel)
}

func requestedModelForRecursivePromotion(input requestPlanningInput, operation resolvedRequestOperation, promotionChain []string) runtimeModelRecord {
	if len(promotionChain) == 0 {
		return runtimeModelRecord{}
	}
	requestedModel, err := resolveRequestedModelByID(input, operation, promotionChain[0])
	if err != nil {
		return runtimeModelRecord{ModelID: promotionChain[0]}
	}
	return requestedModel
}

func attachRecursivePreDispatchPromotionRouting(sourceTarget *resolvedExecutionTarget, promotedTarget resolvedExecutionTarget, estimation *requestContextEstimation) *runtimeContextRoutingDecision {
	contextRouting := promotedTarget.ContextRouting
	policy := runtimeContextRoutingPolicyName(promotedTarget.Strategy)
	if sourceTarget != nil {
		contextRouting = sourceTarget.ContextRouting
		policy = runtimeContextRoutingPolicyName(sourceTarget.Strategy)
	}
	if contextRouting == nil {
		contextRouting = &runtimeContextRoutingDecision{Policy: policy}
	}
	promotion := &runtimeContextOverflowPromotionDecision{
		TriggerPhase:      runtimeContextOverflowPromotionTriggerPhasePreDispatchEstimate,
		TriggerErrorCode:  stringPtr(contextWindowExceededErrorCode),
		TriggerClassifier: runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow,
		EstimationMode:    runtimeContextOverflowPromotionEstimationModeEstimated,
		EstimationStatus:  runtimeContextEstimationStatusPresent,
		FinalAttemptCount: 1,
		Result:            runtimeContextOverflowPromotionResultPromotedSuccess,
	}
	if estimation != nil {
		promotion.EstimationMethod = stringPointerIfNotEmpty(estimation.Method)
		promotion.EstimatedInputTokens = intPtr(estimation.EstimatedInputTokens)
		promotion.ReservedOutputTokens = intPtr(estimation.ReservedOutputTokens)
		promotion.EstimatedTotalContextTokens = intPtr(estimation.EstimatedTotalContextTokens)
	}
	if sourceTarget != nil {
		promotion.FromResolvedTargetModelID = stringPointerIfNotEmpty(sourceTarget.TargetModel.ModelID)
		promotion.FromSelectedTerminalTargetID = cloneRuntimeIntPointer(sourceTarget.SelectedTerminalTargetID)
		if sourceUsable := selectedUsableContextWindowTokensForResolvedTarget(*sourceTarget); sourceUsable > 0 {
			promotion.FromUsableContextWindowTokens = intPtr(sourceUsable)
		}
	}
	promotion.ToResolvedTargetModelID = stringPointerIfNotEmpty(promotedTarget.TargetModel.ModelID)
	promotion.ToSelectedTerminalTargetID = cloneRuntimeIntPointer(promotedTarget.SelectedTerminalTargetID)
	if promotedUsable := selectedUsableContextWindowTokensForResolvedTarget(promotedTarget); promotedUsable > 0 {
		promotion.ToUsableContextWindowTokens = intPtr(promotedUsable)
	}
	merged := attachRuntimeContextOverflowPromotionDecision(contextRouting, promotion)
	merged = runtimeContextRoutingWithRouteReason(merged, gatewaycore.RouteReasonContextOverflowPreflight, policy)
	return merged
}

type runtimeRecursiveContextFitStatus int

const (
	runtimeRecursiveContextFitStatusFits runtimeRecursiveContextFitStatus = iota
	runtimeRecursiveContextFitStatusNoFit
	runtimeRecursiveContextFitStatusMissingWindow
)

func fittingResolvedTargetForRecursiveContextOverflow(target resolvedExecutionTarget, estimation *requestContextEstimation) (resolvedExecutionTarget, runtimeRecursiveContextFitStatus) {
	if estimation == nil || len(target.TerminalAttempts) == 0 {
		return target, runtimeRecursiveContextFitStatusFits
	}
	fittingAttempts := make([]runtimeTerminalAttempt, 0, len(target.TerminalAttempts))
	fittingConnections := make([]runtimeConnection, 0, len(target.Connections))
	fittingRuntimeStates := make(map[int]loadbalance.RuntimeConnectionState, len(target.RuntimeStates))
	sawUsableWindow := false
	for _, attempt := range target.TerminalAttempts {
		usableContextWindowTokens := usableContextWindowTokensForConnection(attempt.Connection)
		if usableContextWindowTokens <= 0 {
			continue
		}
		sawUsableWindow = true
		if !estimation.fitsUsableContextWindowTokens(usableContextWindowTokens) {
			continue
		}
		fittingAttempts = append(fittingAttempts, attempt)
		fittingConnections = append(fittingConnections, attempt.Connection)
		if state, ok := target.RuntimeStates[attempt.Connection.ID]; ok {
			fittingRuntimeStates[attempt.Connection.ID] = state
		}
	}
	if len(fittingAttempts) == 0 {
		if !sawUsableWindow {
			return target, runtimeRecursiveContextFitStatusMissingWindow
		}
		return target, runtimeRecursiveContextFitStatusNoFit
	}
	if len(fittingAttempts) == len(target.TerminalAttempts) && target.SelectedTerminalTargetID != nil && *target.SelectedTerminalTargetID == fittingAttempts[0].Connection.ID {
		return target, runtimeRecursiveContextFitStatusFits
	}
	fittingTarget := target
	fittingTarget.TerminalAttempts = fittingAttempts
	fittingTarget.Connections = fittingConnections
	fittingTarget.RuntimeStates = fittingRuntimeStates
	fittingTarget.SelectedTerminalTargetID = intPtr(fittingAttempts[0].Connection.ID)
	return fittingTarget, runtimeRecursiveContextFitStatusFits
}

func selectedUsableContextWindowTokensForResolvedTarget(target resolvedExecutionTarget) int {
	return selectedUsableContextWindowTokensForResolvedAccessPlan(runtimeResolvedAccessPlan{
		ContextRouting:   target.ContextRouting,
		TerminalAttempts: target.TerminalAttempts,
	})
}

func newRuntimeRecursiveContextOverflowStoppedResult(promotionChain []string, depth int, reason runtimeRecursiveContextOverflowStopReason) *runtimeRecursiveContextOverflowPlannerResult {
	return &runtimeRecursiveContextOverflowPlannerResult{PromotionChain: cloneRuntimeModelPath(promotionChain), Depth: depth, StopReason: string(reason)}
}

func recursiveContextOverflowPlanningError(reason runtimeRecursiveContextOverflowStopReason, promotionChain []string, depth int) error {
	return &domainError{
		StatusCode: http.StatusServiceUnavailable,
		Detail:     fmt.Sprintf("Recursive context-overflow planning stopped with reason '%s'.", reason),
		Fields: map[string]any{
			"recursive_context_overflow_stop_reason": string(reason),
			"promotion_chain":                        cloneRuntimeModelPath(promotionChain),
			"promotion_depth":                        depth,
		},
	}
}

func isContextWindowExceededPlanningError(err error) bool {
	var domainErr *domainError
	return errors.As(err, &domainErr) && domainErr != nil && domainErr.ErrorCode == contextWindowExceededErrorCode
}

func isRuntimeFacadeGuardrailPlanningError(err error) bool {
	var domainErr *domainError
	return errors.As(err, &domainErr) && domainErr != nil && (domainErr.Detail == runtimeNestedFacadesNotSupportedDetail || domainErr.Detail == runtimeFacadeTerminalTargetsNotSupportedDetail)
}

func cloneVisitedRuntimeModelIDsByString(source map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(source)+1)
	for modelID := range source {
		trimmedModelID := strings.TrimSpace(modelID)
		if trimmedModelID != "" {
			cloned[trimmedModelID] = struct{}{}
		}
	}
	return cloned
}
