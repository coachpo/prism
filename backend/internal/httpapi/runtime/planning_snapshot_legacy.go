package runtime

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func (snapshot *planningSnapshot) terminalTargetsByID() map[int]runtimeConnection {
	return snapshot.ConnectionsByID
}

func (s *Service) resolveLegacyExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, requestOperation RuntimeOperation, translationEligibility requestTranslationEligibilitySummary, contextEstimation *requestContextEstimation, referenceNow time.Time) (runtimeResolvedAccessPlan, error) {
	ctx := runtimeAccessResolutionContext{
		RequestedModelID:         requestedModel.ModelID,
		RequestedAPIFamily:       requestedModel.APIFamily,
		RequestOperation:         requestOperation,
		TranslationEligibility:   translationEligibility,
		RequestContextEstimation: contextEstimation,
		VisitedModelIDs:          map[int]struct{}{},
		ConsideredModelPath:      appendRuntimeModelPath(nil, requestedModel.ModelID),
		ReferenceNow:             referenceNow,
	}
	resolved, err := s.resolveLegacyRequestedModelExecutionTargetFromSnapshot(profileID, snapshot, requestedModel, ctx)
	if err != nil {
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			domainErr := &domainError{StatusCode: http.StatusServiceUnavailable, Detail: noEligible.Error()}
			if noEligible.facadeSelection != nil {
				domainErr.ContextRouting = buildRuntimeFacadeContextRoutingDecision(contextEstimation, nil, 0, nil, noEligible.facadeSelection)
			}
			return runtimeResolvedAccessPlan{}, domainErr
		}
		var noContextEligible *noContextEligibleTargetsError
		if errors.As(err, &noContextEligible) {
			contextRoutingStrategy := loadbalance.RuntimeStrategy{LegacyStrategyType: stringPtr("cheapest_eligible_context")}
			if isRuntimeExactOpenAIFacadeModel(requestedModel) {
				contextRoutingStrategy = runtimeFacadeSelectionStrategy()
			}
			contextRouting := buildRuntimeContextRoutingDecision(contextRoutingStrategy, contextEstimation, nil, runtimeContextRoutingCostRankingMethod, noContextEligible.largestUsableContextWindowTokens, noContextEligible.skippedTerminalTargets)
			if noContextEligible.facadeSelection != nil {
				contextRouting = buildRuntimeFacadeContextRoutingDecision(contextEstimation, nil, noContextEligible.largestUsableContextWindowTokens, noContextEligible.skippedTerminalTargets, noContextEligible.facadeSelection)
			}
			fields := map[string]any{
				"estimated_total_context_tokens":       noContextEligible.estimatedTotalContextTokens,
				"largest_usable_context_window_tokens": noContextEligible.largestUsableContextWindowTokens,
				"requested_model_id":                   noContextEligible.requestedModelID,
			}
			if len(noContextEligible.consideredModelPath) > 0 {
				fields["considered_model_path"] = cloneRuntimeModelPath(noContextEligible.consideredModelPath)
			}
			return runtimeResolvedAccessPlan{}, &domainError{
				StatusCode:     http.StatusRequestEntityTooLarge,
				ErrorCode:      contextWindowExceededErrorCode,
				Detail:         contextWindowExceededDetail,
				Fields:         fields,
				ContextRouting: contextRouting,
			}
		}
		return runtimeResolvedAccessPlan{}, err
	}
	return resolved, nil
}

func (s *Service) resolveLegacyRequestedModelExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	if isRuntimeExactOpenAIFacadeModel(requestedModel) {
		return s.resolveLegacyExactOpenAIFacadeModelAccessFromSnapshot(profileID, snapshot, requestedModel, ctx)
	}
	return s.resolveLegacyModelAccessFromSnapshot(profileID, snapshot, requestedModel, ctx)
}

func (s *Service) resolveLegacyExactOpenAIFacadeModelAccessFromSnapshot(profileID int, snapshot *planningSnapshot, model runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	if ctx.Depth > runtimeAccessResolverMaxDepth {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access graph exceeded maximum depth of %d while resolving model '%s'.", runtimeAccessResolverMaxDepth, ctx.RequestedModelID)}
	}
	if _, seen := ctx.VisitedModelIDs[model.ID]; seen {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access cycle detected while resolving model '%s'.", ctx.RequestedModelID)}
	}
	if err := validateRuntimeModelFacadePolicies(model); err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	if ctx.RequestContextEstimation == nil {
		return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
	}

	visited := cloneVisitedModelIDs(ctx.VisitedModelIDs)
	visited[model.ID] = struct{}{}
	childContext := ctx
	childContext.VisitedModelIDs = visited
	childContext.Depth++

	orderedTargets := sortedEnabledRuntimeAccessTargets(snapshot.AccessTargetsBySourceModelID[model.ID])
	strategy := runtimeFacadeSelectionStrategy()
	eligibleCandidates := make([]runtimeResolvedAccessCandidate, 0, len(orderedTargets))
	translatedRejectedCandidates := make([]runtimeResolvedAccessCandidate, 0, len(orderedTargets))
	skippedTerminalTargets := make([]runtimeContextRoutingSkippedTerminalTarget, 0, len(orderedTargets))
	largestUsableContextWindowTokens := 0
	contextFitEvaluated := false
	translatedRejectedCount := 0
	var translationRejection *domainError
	var selectedTranslatedRejectedTarget *runtimeAccessTargetRecord
	for _, target := range orderedTargets {
		if target.TargetType != runtimeAccessTargetTypeModel {
			continue
		}
		evaluation, err := s.evaluateLegacyAccessTargetCandidateFromSnapshot(profileID, snapshot, model, strategy, target, childContext)
		if err != nil {
			return runtimeResolvedAccessPlan{}, err
		}
		if evaluation.contextFitEvaluated {
			contextFitEvaluated = true
			if evaluation.largestUsableContextWindowTokens > largestUsableContextWindowTokens {
				largestUsableContextWindowTokens = evaluation.largestUsableContextWindowTokens
			}
		}
		if evaluation.translationRejection != nil {
			translatedRejectedCount++
			if translationRejection == nil {
				translationRejection = cloneDomainError(evaluation.translationRejection)
			}
			if selectedTranslatedRejectedTarget == nil {
				selectedTarget := target
				selectedTranslatedRejectedTarget = &selectedTarget
			}
		}
		skippedTerminalTargets = append(skippedTerminalTargets, evaluation.skippedTerminalTargets...)
		if evaluation.translatedRejectedCandidate != nil {
			translatedRejectedCandidates = append(translatedRejectedCandidates, *evaluation.translatedRejectedCandidate)
		}
		if evaluation.eligibleCandidate != nil {
			eligibleCandidates = append(eligibleCandidates, *evaluation.eligibleCandidate)
		}
	}
	eligibleTotalWeight := legacyRuntimeFacadeEligibleTotalWeight(eligibleCandidates)
	if len(eligibleCandidates) == 0 {
		if translationRejection != nil {
			facadeSelection := buildRuntimeFacadeSelectionDecision(model.ModelID, nil, eligibleTotalWeight, skippedTerminalTargets, translatedRejectedCount)
			if selectedTranslatedRejectedTarget != nil {
				facadeSelection.SelectedTargetModelID = stringPointerIfNotEmpty(selectedTranslatedRejectedTarget.TargetModelID)
				facadeSelection.SelectedWeight = intPtr(selectedTranslatedRejectedTarget.Weight)
			}
			selectedTerminalTargetID := cloneRuntimeIntPointer(translationRejection.SelectedTerminalTargetID)
			if selectedTerminalTargetID == nil && translationRejection.ContextRouting != nil {
				selectedTerminalTargetID = cloneRuntimeIntPointer(translationRejection.ContextRouting.SelectedTerminalTargetID)
			}
			contextRouting := attachRuntimeFacadeSelectionDecision(translationRejection.ContextRouting, facadeSelection)
			if len(translatedRejectedCandidates) > 0 {
				selectedRejectedCandidate := translatedRejectedCandidates[0]
				if selectedTerminalTargetID == nil {
					selectedTerminalTargetID = cloneRuntimeIntPointer(selectedRejectedCandidate.resolved.SelectedTerminalTargetID)
					if selectedTerminalTargetID == nil && len(selectedRejectedCandidate.resolved.TerminalAttempts) > 0 {
						selectedTerminalTargetID = intPtr(selectedRejectedCandidate.resolved.TerminalAttempts[0].Connection.ID)
					}
				}
				contextRouting = buildRuntimeFacadeContextRoutingDecision(ctx.RequestContextEstimation, &selectedRejectedCandidate, largestUsableContextWindowTokens, skippedTerminalTargets, facadeSelection)
			}
			translationRejection = decorateRequestTranslationRejection(translationRejection, selectedTerminalTargetID, contextRouting)
			if selectedTranslatedRejectedTarget != nil && selectedTranslatedRejectedTarget.TargetModelID != "" {
				resolvedTargetModelID := selectedTranslatedRejectedTarget.TargetModelID
				if resolvedTargetModelID != "" {
					translationRejection.ResolvedTargetModelID = &resolvedTargetModelID
				}
			}
			return runtimeResolvedAccessPlan{}, translationRejection
		}
		facadeSelection := buildRuntimeFacadeSelectionDecision(model.ModelID, nil, eligibleTotalWeight, skippedTerminalTargets, translatedRejectedCount)
		if contextFitEvaluated {
			return runtimeResolvedAccessPlan{}, &noContextEligibleTargetsError{
				requestedModelID:                 ctx.RequestedModelID,
				estimatedTotalContextTokens:      ctx.RequestContextEstimation.EstimatedTotalContextTokens,
				largestUsableContextWindowTokens: largestUsableContextWindowTokens,
				consideredModelPath:              cloneRuntimeModelPath(ctx.ConsideredModelPath),
				skippedTerminalTargets:           skippedTerminalTargets,
				facadeSelection:                  facadeSelection,
			}
		}
		return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID, facadeSelection: facadeSelection}
	}
	selectedCandidate := selectWeightedRuntimeAccessCandidate(profileID, model.ID, eligibleCandidates, s.runtimeState)
	if selectedCandidate == nil {
		facadeSelection := buildRuntimeFacadeSelectionDecision(model.ModelID, nil, eligibleTotalWeight, skippedTerminalTargets, translatedRejectedCount)
		return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID, facadeSelection: facadeSelection}
	}
	resolved := selectedCandidate.resolved
	facadeSelection := buildRuntimeFacadeSelectionDecision(model.ModelID, selectedCandidate, eligibleTotalWeight, skippedTerminalTargets, translatedRejectedCount)
	if resolved.ContextRouting == nil {
		resolved.ContextRouting = buildRuntimeFacadeContextRoutingDecision(ctx.RequestContextEstimation, selectedCandidate, largestUsableContextWindowTokens, skippedTerminalTargets, facadeSelection)
	} else {
		resolved.ContextRouting = attachRuntimeFacadeSelectionDecision(resolved.ContextRouting, facadeSelection)
	}
	if resolved.LargestUsableContextWindowTokens < largestUsableContextWindowTokens {
		resolved.LargestUsableContextWindowTokens = largestUsableContextWindowTokens
	}
	resolved.ContextFitEvaluated = resolved.ContextFitEvaluated || contextFitEvaluated
	return resolved, nil
}

func (s *Service) resolveLegacyModelAccessFromSnapshot(profileID int, snapshot *planningSnapshot, model runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	if ctx.Depth > runtimeAccessResolverMaxDepth {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access graph exceeded maximum depth of %d while resolving model '%s'.", runtimeAccessResolverMaxDepth, ctx.RequestedModelID)}
	}
	if _, seen := ctx.VisitedModelIDs[model.ID]; seen {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access cycle detected while resolving model '%s'.", ctx.RequestedModelID)}
	}
	if err := validateRuntimeModelFacadePolicies(model); err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	strategy, ok := snapshot.StrategiesByModelID[model.ID]
	if !ok {
		return runtimeResolvedAccessPlan{}, fmt.Errorf("model %q is missing loadbalance_strategy", model.ModelID)
	}
	if strategy.IsCheapestEligibleContextStrategy() && ctx.RequestContextEstimation == nil {
		return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
	}

	visited := cloneVisitedModelIDs(ctx.VisitedModelIDs)
	visited[model.ID] = struct{}{}
	childContext := ctx
	childContext.VisitedModelIDs = visited
	childContext.Depth++

	orderedTargets := orderRuntimeAccessTargets(profileID, model.ID, strategy, snapshot.AccessTargetsBySourceModelID[model.ID], s.runtimeState)
	if strategy.IsCheapestEligibleContextStrategy() {
		return s.resolveLegacyCheapestEligibleContextModelAccess(profileID, snapshot, model, strategy, orderedTargets, childContext)
	}
	resolved := runtimeResolvedAccessPlan{RuntimeStates: map[int]loadbalance.RuntimeConnectionState{}, Strategy: strategy}
	var translationRejection *domainError
	for _, target := range orderedTargets {
		candidate, eligible, err := s.resolveLegacyAccessTargetFromSnapshot(profileID, snapshot, model, strategy, target, childContext)
		if err != nil {
			return runtimeResolvedAccessPlan{}, err
		}
		if candidate.TranslationRejection != nil && translationRejection == nil {
			translationRejection = candidate.TranslationRejection
		}
		if !eligible {
			continue
		}
		candidate, compatible, rejection := applyRequestTranslationCompatibility(candidate, childContext)
		if rejection != nil {
			if translationRejection == nil {
				translationRejection = rejection
			}
			continue
		}
		if !compatible {
			continue
		}
		appendRuntimeResolvedAccessPlan(&resolved, candidate)
	}
	if len(resolved.TerminalAttempts) == 0 || len(resolved.Connections) == 0 {
		if translationRejection != nil {
			return runtimeResolvedAccessPlan{}, translationRejection
		}
		return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID}
	}
	return resolved, nil
}

func (s *Service) resolveLegacyCheapestEligibleContextModelAccess(profileID int, snapshot *planningSnapshot, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, orderedTargets []runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	eligibleCandidates := make([]runtimeResolvedAccessCandidate, 0, len(orderedTargets))
	translatedRejectedCandidates := make([]runtimeResolvedAccessCandidate, 0, len(orderedTargets))
	skippedTerminalTargets := make([]runtimeContextRoutingSkippedTerminalTarget, 0, len(orderedTargets))
	largestUsableContextWindowTokens := 0
	contextFitEvaluated := false
	var translationRejection *domainError
	for _, target := range orderedTargets {
		evaluation, err := s.evaluateLegacyAccessTargetCandidateFromSnapshot(profileID, snapshot, model, strategy, target, ctx)
		if err != nil {
			return runtimeResolvedAccessPlan{}, err
		}
		if evaluation.contextFitEvaluated {
			contextFitEvaluated = true
			if evaluation.largestUsableContextWindowTokens > largestUsableContextWindowTokens {
				largestUsableContextWindowTokens = evaluation.largestUsableContextWindowTokens
			}
		}
		if evaluation.translationRejection != nil && translationRejection == nil {
			translationRejection = cloneDomainError(evaluation.translationRejection)
		}
		skippedTerminalTargets = append(skippedTerminalTargets, evaluation.skippedTerminalTargets...)
		if evaluation.translatedRejectedCandidate != nil {
			translatedRejectedCandidates = append(translatedRejectedCandidates, *evaluation.translatedRejectedCandidate)
		}
		if evaluation.eligibleCandidate != nil {
			eligibleCandidates = append(eligibleCandidates, *evaluation.eligibleCandidate)
		}
	}
	if len(eligibleCandidates) == 0 {
		if translationRejection != nil {
			if len(translatedRejectedCandidates) > 0 {
				sort.SliceStable(translatedRejectedCandidates, func(left int, right int) bool {
					return compareRuntimeResolvedAccessCandidates(translatedRejectedCandidates[left], translatedRejectedCandidates[right]) < 0
				})
				selectedRejectedCandidate := translatedRejectedCandidates[0]
				selectedTerminalTargetID := cloneRuntimeIntPointer(selectedRejectedCandidate.resolved.SelectedTerminalTargetID)
				if selectedTerminalTargetID == nil && len(selectedRejectedCandidate.resolved.TerminalAttempts) > 0 {
					selectedTerminalTargetID = intPtr(selectedRejectedCandidate.resolved.TerminalAttempts[0].Connection.ID)
				}
				translationRejection = decorateRequestTranslationRejection(
					translationRejection,
					selectedTerminalTargetID,
					buildRuntimeContextRoutingDecision(strategy, ctx.RequestContextEstimation, &selectedRejectedCandidate, runtimeContextRoutingCostRankingMethod, largestUsableContextWindowTokens, skippedTerminalTargets),
				)
				if len(selectedRejectedCandidate.resolved.TerminalAttempts) > 0 {
					resolvedTargetModelID := selectedRejectedCandidate.resolved.TerminalAttempts[0].TargetModel.ModelID
					if resolvedTargetModelID != "" {
						translationRejection.ResolvedTargetModelID = &resolvedTargetModelID
					}
				}
			}
			return runtimeResolvedAccessPlan{}, translationRejection
		}
		if contextFitEvaluated && ctx.RequestContextEstimation != nil {
			return runtimeResolvedAccessPlan{}, &noContextEligibleTargetsError{
				requestedModelID:                 ctx.RequestedModelID,
				estimatedTotalContextTokens:      ctx.RequestContextEstimation.EstimatedTotalContextTokens,
				largestUsableContextWindowTokens: largestUsableContextWindowTokens,
				consideredModelPath:              cloneRuntimeModelPath(ctx.ConsideredModelPath),
				skippedTerminalTargets:           skippedTerminalTargets,
			}
		}
		return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID}
	}
	sort.SliceStable(eligibleCandidates, func(left int, right int) bool {
		return compareRuntimeResolvedAccessCandidates(eligibleCandidates[left], eligibleCandidates[right]) < 0
	})
	resolved := runtimeResolvedAccessPlan{RuntimeStates: map[int]loadbalance.RuntimeConnectionState{}, Strategy: strategy}
	for _, candidate := range eligibleCandidates {
		appendRuntimeResolvedAccessPlan(&resolved, candidate.resolved)
	}
	selectedCandidate := eligibleCandidates[0]
	resolved.SelectedTerminalTargetID = intPtr(selectedCandidate.resolved.TerminalAttempts[0].Connection.ID)
	resolved.ContextRouting = buildRuntimeContextRoutingDecision(strategy, ctx.RequestContextEstimation, &selectedCandidate, runtimeContextRoutingCostRankingMethod, largestUsableContextWindowTokens, skippedTerminalTargets)
	resolved.LargestUsableContextWindowTokens = largestUsableContextWindowTokens
	resolved.ContextFitEvaluated = true
	return resolved, nil
}

func (s *Service) evaluateLegacyAccessTargetCandidateFromSnapshot(profileID int, snapshot *planningSnapshot, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessCandidateEvaluation, error) {
	candidate, eligible, err := s.resolveLegacyAccessTargetFromSnapshot(profileID, snapshot, model, strategy, target, ctx)
	if err != nil {
		return runtimeResolvedAccessCandidateEvaluation{}, err
	}

	evaluation := runtimeResolvedAccessCandidateEvaluation{
		largestUsableContextWindowTokens: candidate.LargestUsableContextWindowTokens,
		contextFitEvaluated:              candidate.ContextFitEvaluated,
	}
	if candidate.TranslationRejection != nil {
		evaluation.translationRejection = cloneDomainError(candidate.TranslationRejection)
	}
	if !eligible || len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		evaluation.skippedTerminalTargets = append(evaluation.skippedTerminalTargets, candidateSkippedTerminalTargets(candidate)...)
		return evaluation, nil
	}

	evaluation.contextFitEvaluated = true
	terminalAttempt := candidate.TerminalAttempts[0]
	usableContextWindowTokens := usableContextWindowTokensForConnection(terminalAttempt.Connection)
	if usableContextWindowTokens > evaluation.largestUsableContextWindowTokens {
		evaluation.largestUsableContextWindowTokens = usableContextWindowTokens
	}
	contextBand := classifyRequestContextBandLegacy(ctx.RequestContextEstimation, terminalAttempt.Connection)
	if contextBand == runtimeContextEligibilityBandIneligible {
		evaluation.skippedTerminalTargets = append(evaluation.skippedTerminalTargets, buildRuntimeContextRoutingSkippedTerminalTarget(terminalAttempt.Connection, ctx.RequestContextEstimation, usableContextWindowTokens))
		return evaluation, nil
	}

	costMicros, priced := estimateRuntimeBlendedRequestCost(terminalAttempt.Connection, ctx.RequestContextEstimation)
	compatibleCandidate, compatible, rejection := applyRequestTranslationCompatibility(candidate, ctx)
	if rejection != nil {
		rejectedCandidate := runtimeResolvedAccessCandidate{target: target, resolved: candidate, contextBand: contextBand, priced: priced, costMicros: costMicros}
		evaluation.translatedRejectedCandidate = &rejectedCandidate
		evaluation.translationRejection = rejection
		return evaluation, nil
	}
	if !compatible {
		return evaluation, nil
	}

	eligibleCandidate := runtimeResolvedAccessCandidate{target: target, resolved: compatibleCandidate, contextBand: contextBand, priced: priced, costMicros: costMicros}
	evaluation.eligibleCandidate = &eligibleCandidate
	return evaluation, nil
}

func classifyRequestContextBandLegacy(estimation *requestContextEstimation, connection runtimeConnection) runtimeContextEligibilityBand {
	if !requestContextFitsConnectionLegacy(estimation, connection) {
		return runtimeContextEligibilityBandIneligible
	}
	if estimation == nil {
		return runtimeContextEligibilityBandIneligible
	}
	if connection.PreferredContextUtilizationThreshold == nil {
		return runtimeContextEligibilityBandPreferred
	}
	preferredContextWindowTokens := preferredContextWindowTokensForConnection(connection)
	if estimation.EstimatedTotalContextTokens <= preferredContextWindowTokens {
		return runtimeContextEligibilityBandPreferred
	}
	return runtimeContextEligibilityBandDiscretionary
}

func requestContextFitsConnectionLegacy(estimation *requestContextEstimation, connection runtimeConnection) bool {
	if estimation == nil {
		return false
	}
	usableContextWindowTokens := usableContextWindowTokensForConnection(connection)
	if usableContextWindowTokens <= 0 {
		return false
	}
	return estimation.EstimatedTotalContextTokens <= usableContextWindowTokens
}

func (s *Service) resolveLegacyAccessTargetFromSnapshot(profileID int, snapshot *planningSnapshot, sourceModel runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	if !target.IsEnabled || target.ProfileID != profileID || target.SourceModelConfigID != sourceModel.ID {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	switch target.TargetType {
	case runtimeAccessTargetTypeConnection:
		return s.resolveLegacyTerminalTargetFromSnapshot(profileID, snapshot, sourceModel, strategy, target, ctx.ReferenceNow)
	case runtimeAccessTargetTypeModel:
		return s.resolveLegacyModelAccessTargetFromSnapshot(profileID, snapshot, sourceModel, target, ctx)
	default:
		return runtimeResolvedAccessPlan{}, false, nil
	}
}

func (s *Service) resolveLegacyTerminalTargetFromSnapshot(profileID int, snapshot *planningSnapshot, sourceModel runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, referenceNow time.Time) (runtimeResolvedAccessPlan, bool, error) {
	terminalTargetConnectionID := target.terminalTargetConnectionID()
	if terminalTargetConnectionID == nil {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	connection, ok := snapshot.terminalTargetsByID()[*terminalTargetConnectionID]
	if !ok {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if connection.ProfileID != sourceModel.ProfileID || !sameRuntimeAPIFamily(connection.APIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if target.TargetConnectionProfileID != 0 && target.TargetConnectionProfileID != sourceModel.ProfileID {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if strings.TrimSpace(target.TargetConnectionAPIFamily) != "" && !sameRuntimeAPIFamily(target.TargetConnectionAPIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}

	resolvedConnection := connection
	resolvedConnection.ModelConfigID = sourceModel.ID
	resolvedConnection.Priority = target.Position
	if target.ConnectionEndpointFX != nil {
		fx := *target.ConnectionEndpointFX
		resolvedConnection.EndpointFXSnapshot = &fx
	}

	runtimeStates := s.runtimeState.SnapshotConnectionStates(profileID, runtimeConnectionRefs([]runtimeConnection{resolvedConnection}))
	eligibleConnectionIDs := loadbalance.FilterEligibleConnectionIDs(toConnectionOrderCandidates([]runtimeConnection{resolvedConnection}), runtimeStates, referenceNow)
	if len(eligibleConnectionIDs) == 0 {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	eligibleRuntimeStates := make(map[int]loadbalance.RuntimeConnectionState, len(eligibleConnectionIDs))
	for _, connectionID := range eligibleConnectionIDs {
		if state, ok := runtimeStates[connectionID]; ok {
			eligibleRuntimeStates[connectionID] = state
		}
	}
	largestUsableContextWindowTokens := usableContextWindowTokensForConnection(resolvedConnection)
	return runtimeResolvedAccessPlan{
		TargetModel:              sourceModel,
		SelectedTerminalTargetID: intPtr(resolvedConnection.ID),
		Connections:              []runtimeConnection{resolvedConnection},
		TerminalAttempts: []runtimeTerminalAttempt{{
			TargetModel:               sourceModel,
			Connection:                resolvedConnection,
			Strategy:                  strategy,
			AuditEnabledAtRequest:     sourceModel.AuditEnabled,
			AuditCaptureBodiesRequest: sourceModel.AuditEnabled && sourceModel.AuditCaptureBodies,
		}},
		RuntimeStates:                    eligibleRuntimeStates,
		Strategy:                         strategy,
		LargestUsableContextWindowTokens: largestUsableContextWindowTokens,
	}, true, nil
}

func (s *Service) resolveLegacyModelAccessTargetFromSnapshot(profileID int, snapshot *planningSnapshot, sourceModel runtimeModelRecord, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	if target.TargetModelConfigID == nil || !target.TargetModelEnabled {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if target.TargetModelProfileID != sourceModel.ProfileID || !sameRuntimeAPIFamily(target.TargetModelAPIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	childModel, ok := snapshot.ModelsByID[target.TargetModelID]
	if !ok || childModel.ID != *target.TargetModelConfigID {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if childModel.ProfileID != sourceModel.ProfileID || !sameRuntimeAPIFamily(childModel.APIFamily, sourceModel.APIFamily) {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	if childModel.FacadeEnabled {
		return runtimeResolvedAccessPlan{}, false, nestedRuntimeFacadeTargetError()
	}
	childContext := ctx
	childContext.ConsideredModelPath = appendRuntimeModelPath(ctx.ConsideredModelPath, childModel.ModelID)
	resolved, err := s.resolveLegacyModelAccessFromSnapshot(profileID, snapshot, childModel, childContext)
	if err != nil {
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			return runtimeResolvedAccessPlan{}, false, nil
		}
		var noContextEligible *noContextEligibleTargetsError
		if errors.As(err, &noContextEligible) {
			contextRouting := buildRuntimeContextRoutingDecision(loadbalance.RuntimeStrategy{LegacyStrategyType: stringPtr("cheapest_eligible_context")}, ctx.RequestContextEstimation, nil, runtimeContextRoutingCostRankingMethod, noContextEligible.largestUsableContextWindowTokens, noContextEligible.skippedTerminalTargets)
			return runtimeResolvedAccessPlan{ContextRouting: contextRouting, LargestUsableContextWindowTokens: noContextEligible.largestUsableContextWindowTokens, ContextFitEvaluated: true}, false, nil
		}
		if translationRejection, ok := isRequestTranslationUnsupportedError(err); ok {
			return runtimeResolvedAccessPlan{TranslationRejection: cloneDomainError(translationRejection)}, false, nil
		}
		return runtimeResolvedAccessPlan{}, false, err
	}
	return resolved, true, nil
}

func legacyRuntimeFacadeEligibleTotalWeight(candidates []runtimeResolvedAccessCandidate) int {
	totalWeight := 0
	for _, candidate := range candidates {
		totalWeight += candidate.target.Weight
	}
	return totalWeight
}
