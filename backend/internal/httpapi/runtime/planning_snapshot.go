package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/providercompat"
)

type planningSnapshot struct {
	ModelsByID                   map[string]runtimeModelRecord
	AccessTargetsBySourceModelID map[int][]runtimeAccessTargetRecord
	TerminalTargetsByID          map[int]runtimeConnection
	StrategiesByModelID          map[int]loadbalance.RuntimeStrategy
	BlocklistRules               []headerBlocklistRule
	ReportCurrency               runtimeReportCurrencySnapshot

	routingPlanOnce sync.Once
	routingPlan     *runtimeRoutingPlan
	routingPlanErr  error
}

func (snapshot *planningSnapshot) compiledRoutingPlan() (*runtimeRoutingPlan, error) {
	if snapshot == nil {
		return nil, invalidRuntimeRoutingPlanError("planning snapshot is nil")
	}
	snapshot.routingPlanOnce.Do(func() {
		snapshot.routingPlan, snapshot.routingPlanErr = compileRuntimeRoutingPlan(snapshot)
		if snapshot.routingPlanErr != nil {
			return
		}
		snapshot.routingPlanErr = validateRuntimeRoutingPlan(snapshot.routingPlan)
	})
	return snapshot.routingPlan, snapshot.routingPlanErr
}

type runtimeAccessTargetRecord struct {
	ID                        int
	ProfileID                 int
	SourceModelConfigID       int
	TargetType                string
	TargetModelConfigID       *int
	TargetModelID             string
	TargetModelProfileID      int
	TargetModelAPIFamily      string
	TargetModelEnabled        bool
	TargetConnectionID        *int
	TargetConnectionProfileID int
	TargetConnectionAPIFamily string
	Position                  int
	Weight                    int
	TargetPriority            int
	IsEnabled                 bool
	ConnectionEndpointFX      *runtimeEndpointFXSnapshot
}

func (record runtimeAccessTargetRecord) terminalTargetConnectionID() *int {
	return record.TargetConnectionID
}

func buildPlanningSnapshot(ctx context.Context, tx pgx.Tx, profileID int, secretEncryptionKey string) (*planningSnapshot, error) {
	modelsByID, err := listEnabledModelsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	accessTargetsBySourceModelID, err := listAccessTargetsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	strategiesByID, err := listRuntimeStrategiesForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	connectionsByID, err := listActiveConnectionsForProfile(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	blocklistRules, err := listEnabledHeaderBlocklistRules(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	reportCurrency, err := loadRuntimeReportCurrencySnapshot(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}

	for connectionID, connection := range connectionsByID {
		compiled, err := compileRuntimeConnection(connection, connection.APIFamily, secretEncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("compile runtime connection %d for profile %d: %w", connectionID, profileID, err)
		}
		connectionsByID[connectionID] = compiled
	}

	strategiesByModelID := make(map[int]loadbalance.RuntimeStrategy, len(modelsByID))
	for _, model := range modelsByID {
		if model.LoadbalanceStrategyID == nil {
			continue
		}
		if strategy, ok := strategiesByID[*model.LoadbalanceStrategyID]; ok {
			strategiesByModelID[model.ID] = strategy
		}
	}

	snapshot := &planningSnapshot{
		ModelsByID:                   modelsByID,
		AccessTargetsBySourceModelID: accessTargetsBySourceModelID,
		TerminalTargetsByID:          connectionsByID,
		StrategiesByModelID:          strategiesByModelID,
		BlocklistRules:               blocklistRules,
		ReportCurrency:               reportCurrency,
	}
	if err := validateRuntimePlanningSnapshotFacadePolicies(snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func compileRuntimeConnection(connection runtimeConnection, apiFamily string, secretEncryptionKey string) (runtimeConnection, error) {
	compiled := connection
	config, err := providercompat.ResolveAuthProfile(connection.AuthType, apiFamily)
	if err != nil {
		return runtimeConnection{}, err
	}
	if strings.TrimSpace(secretEncryptionKey) == "" {
		return compiled, nil
	}
	apiKey, err := endpointdomain.DecryptSecret(connection.EncryptedEndpointAPIKey, secretEncryptionKey)
	if err != nil {
		return runtimeConnection{}, fmt.Errorf("resolve endpoint api key for connection %d: %w", connection.ID, err)
	}
	controlledHeaderNames := config.ControlledHeaderNames()
	extraHeaders := make(map[string]string, len(config.ExtraHeaders))
	for key, value := range config.ExtraHeaders {
		extraHeaders[key] = value
	}
	compiled.UpstreamAuth = &runtimeConnectionUpstreamAuthSnapshot{
		AuthHeader:            config.AuthHeader,
		AuthValue:             config.AuthPrefix + apiKey,
		ExtraHeaders:          extraHeaders,
		ControlledHeaderNames: controlledHeaderNames,
	}
	compiled.EncryptedEndpointAPIKey = ""
	return compiled, nil
}

func listPublishedPlanningProfileIDs(ctx context.Context, tx pgx.Tx) ([]int, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id FROM profiles WHERE deleted_at IS NULL ORDER BY id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query published planning profile ids: %w", err)
	}
	defer rows.Close()

	profileIDs := make([]int, 0)
	for rows.Next() {
		var profileID int
		if err := rows.Scan(&profileID); err != nil {
			return nil, fmt.Errorf("scan published planning profile id: %w", err)
		}
		profileIDs = append(profileIDs, profileID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate published planning profile ids: %w", err)
	}
	return profileIDs, nil
}

const runtimeAccessResolverMaxDepth = 32

const (
	contextWindowExceededErrorCode = "context_window_exceeded"
	contextWindowExceededDetail    = "No configured target can fit the estimated request context."
)

type runtimeAccessResolutionContext struct {
	RequestedModelID              string
	RequestedAPIFamily            string
	RequestOperation              RuntimeOperation
	RawRequestBody                []byte
	RequestContextEstimation      *requestContextEstimation
	AllowMissingContextEstimation bool
	VisitedModelIDs               map[int]struct{}
	ConsideredModelPath           []string
	Depth                         int
	ReferenceNow                  time.Time
}

func (model runtimeModelRecord) allowsOpenAITextSiblingTranslation() bool {
	return providercompat.IsOpenAI(model.APIFamily)
}

type runtimeResolvedAccessPlan struct {
	TargetModel                      runtimeModelRecord
	SelectedTerminalTargetID         *int
	ContextRouting                   *runtimeContextRoutingDecision
	Connections                      []runtimeConnection
	TerminalAttempts                 []runtimeTerminalAttempt
	RuntimeStates                    map[int]loadbalance.RuntimeConnectionState
	Strategy                         loadbalance.RuntimeStrategy
	RouteReason                      gatewaycore.RouteReason
	CompatibilityError               error
	LargestUsableContextWindowTokens int
	ContextFitEvaluated              bool
}

func (ctx runtimeAccessResolutionContext) rejectsMissingContextEstimation() bool {
	return ctx.RequestContextEstimation == nil && !ctx.AllowMissingContextEstimation
}

type runtimeContextEligibilityBand int

const (
	runtimeContextEligibilityBandIneligible runtimeContextEligibilityBand = iota
	runtimeContextEligibilityBandDiscretionary
	runtimeContextEligibilityBandPreferred
)

type runtimeResolvedAccessCandidate struct {
	target      runtimeAccessTargetRecord
	resolved    runtimeResolvedAccessPlan
	contextBand runtimeContextEligibilityBand
	priced      bool
	costMicros  int64
}

type runtimeResolvedAccessCandidateEvaluation struct {
	eligibleCandidate                *runtimeResolvedAccessCandidate
	compatibilityError               error
	skippedTerminalTargets           []runtimeContextRoutingSkippedTerminalTarget
	largestUsableContextWindowTokens int
	contextFitEvaluated              bool
}

type runtimeModelPeerSelection struct {
	selectedCandidate                *runtimeResolvedAccessCandidate
	eligibleCandidates               []runtimeResolvedAccessCandidate
	skippedTerminalTargets           []runtimeContextRoutingSkippedTerminalTarget
	compatibilityError               error
	eligibleTotalWeight              int
	largestUsableContextWindowTokens int
	contextFitEvaluated              bool
}

type noEligibleTargetsError struct {
	requestedModelID string
	facadeSelection  *runtimeFacadeSelectionDecision
}

type noContextEligibleTargetsError struct {
	requestedModelID                 string
	estimatedTotalContextTokens      int
	largestUsableContextWindowTokens int
	consideredModelPath              []string
	skippedTerminalTargets           []runtimeContextRoutingSkippedTerminalTarget
	facadeSelection                  *runtimeFacadeSelectionDecision
}

func (err *noEligibleTargetsError) Error() string {
	return fmt.Sprintf("No eligible targets available for model '%s'.", err.requestedModelID)
}

func (err *noContextEligibleTargetsError) Error() string {
	return contextWindowExceededDetail
}

func (s *Service) resolveExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, requestOperation RuntimeOperation, contextEstimation *requestContextEstimation, referenceNow time.Time) (runtimeResolvedAccessPlan, error) {
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	return s.resolveExecutionTargetFromRoutingPlanWithOptions(profileID, routingPlan, requestedModel, requestOperation, nil, contextEstimation, false, referenceNow)
}

func (s *Service) resolveExecutionTargetFromRoutingPlanWithOptions(profileID int, routingPlan *runtimeRoutingPlan, requestedModel runtimeModelRecord, requestOperation RuntimeOperation, rawRequestBody []byte, contextEstimation *requestContextEstimation, allowMissingContextEstimation bool, referenceNow time.Time) (runtimeResolvedAccessPlan, error) {
	ctx := runtimeAccessResolutionContext{
		RequestedModelID:              requestedModel.ModelID,
		RequestedAPIFamily:            requestedModel.APIFamily,
		RequestOperation:              requestOperation,
		RawRequestBody:                rawRequestBody,
		RequestContextEstimation:      contextEstimation,
		AllowMissingContextEstimation: allowMissingContextEstimation,
		VisitedModelIDs:               map[int]struct{}{},
		ConsideredModelPath:           appendRuntimeModelPath(nil, requestedModel.ModelID),
		ReferenceNow:                  referenceNow,
	}
	resolved, err := s.resolveRequestedModelExecutionTargetFromRoutingPlan(profileID, routingPlan, requestedModel, ctx)
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
			contextRouting := buildRuntimeContextRoutingDecision(
				contextRoutingStrategy,
				contextEstimation,
				nil,
				runtimeContextRoutingCostRankingMethod,
				noContextEligible.largestUsableContextWindowTokens,
				noContextEligible.skippedTerminalTargets,
			)
			if noContextEligible.facadeSelection != nil {
				contextRouting = buildRuntimeFacadeContextRoutingDecision(
					contextEstimation,
					nil,
					noContextEligible.largestUsableContextWindowTokens,
					noContextEligible.skippedTerminalTargets,
					noContextEligible.facadeSelection,
				)
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

func (s *Service) resolveRequestedModelExecutionTargetFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, requestedModel runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	if isRuntimeExactOpenAIFacadeModel(requestedModel) {
		return s.resolveExactFacadeModelAccessFromRoutingPlan(profileID, routingPlan, requestedModel, ctx)
	}
	return s.resolveModelAccessFromRoutingPlan(profileID, routingPlan, requestedModel, ctx)
}

func (s *Service) resolveExactFacadeModelAccessFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	if ctx.Depth > runtimeAccessResolverMaxDepth {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access graph exceeded maximum depth of %d while resolving model '%s'.", runtimeAccessResolverMaxDepth, ctx.RequestedModelID)}
	}
	if _, seen := ctx.VisitedModelIDs[model.ID]; seen {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access cycle detected while resolving model '%s'.", ctx.RequestedModelID)}
	}
	if err := validateRuntimeModelFacadePolicies(model); err != nil {
		return runtimeResolvedAccessPlan{}, err
	}

	visited := cloneVisitedModelIDs(ctx.VisitedModelIDs)
	visited[model.ID] = struct{}{}
	childContext := ctx
	childContext.VisitedModelIDs = visited
	childContext.Depth++

	strategy := runtimeFacadeSelectionStrategy()
	peerSelection, err := s.selectModelPeerCandidateFromRoutingPlan(profileID, routingPlan, model, strategy, childContext)
	if err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	if peerSelection.selectedCandidate == nil {
		if peerSelection.compatibilityError != nil {
			return runtimeResolvedAccessPlan{}, peerSelection.compatibilityError
		}
		if ctx.rejectsMissingContextEstimation() {
			return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
		}
		facadeSelection := buildRuntimeFacadeSelectionDecision(model.ModelID, nil, peerSelection.eligibleTotalWeight, peerSelection.skippedTerminalTargets, 0)
		if peerSelection.contextFitEvaluated && ctx.RequestContextEstimation != nil {
			return runtimeResolvedAccessPlan{}, &noContextEligibleTargetsError{
				requestedModelID:                 ctx.RequestedModelID,
				estimatedTotalContextTokens:      ctx.RequestContextEstimation.EstimatedTotalContextTokens,
				largestUsableContextWindowTokens: peerSelection.largestUsableContextWindowTokens,
				consideredModelPath:              cloneRuntimeModelPath(ctx.ConsideredModelPath),
				skippedTerminalTargets:           peerSelection.skippedTerminalTargets,
				facadeSelection:                  facadeSelection,
			}
		}
		return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID, facadeSelection: facadeSelection}
	}
	if ctx.rejectsMissingContextEstimation() {
		return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
	}
	selectedCandidate := peerSelection.selectedCandidate
	resolved := selectedCandidate.resolved
	facadeSelection := buildRuntimeFacadeSelectionDecision(model.ModelID, selectedCandidate, peerSelection.eligibleTotalWeight, peerSelection.skippedTerminalTargets, 0)
	if resolved.ContextRouting == nil {
		resolved.ContextRouting = buildRuntimeFacadeContextRoutingDecision(ctx.RequestContextEstimation, selectedCandidate, peerSelection.largestUsableContextWindowTokens, peerSelection.skippedTerminalTargets, facadeSelection)
	} else {
		resolved.ContextRouting = attachRuntimeFacadeSelectionDecision(resolved.ContextRouting, facadeSelection)
	}
	if resolved.LargestUsableContextWindowTokens < peerSelection.largestUsableContextWindowTokens {
		resolved.LargestUsableContextWindowTokens = peerSelection.largestUsableContextWindowTokens
	}
	resolved.ContextFitEvaluated = resolved.ContextFitEvaluated || peerSelection.contextFitEvaluated
	return resolved, nil
}

func (s *Service) selectModelPeerCandidateFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, ctx runtimeAccessResolutionContext) (runtimeModelPeerSelection, error) {
	selection := runtimeModelPeerSelection{}
	if !runtimeStrategyUsesContextFiltering(strategy) {
		targets := routingPlan.orderedModelTargetsForStrategy(profileID, model, strategy, s.runtimeState)
		eligibleCandidates, err := s.evaluateModelPeerTargetsFromRoutingPlan(profileID, routingPlan, model, strategy, targets, ctx, &selection)
		if err != nil {
			return runtimeModelPeerSelection{}, err
		}
		if len(eligibleCandidates) == 0 {
			return selection, nil
		}
		selection.eligibleTotalWeight = runtimeAccessCandidateTotalWeight(eligibleCandidates)
		selection.eligibleCandidates = append(selection.eligibleCandidates, eligibleCandidates...)
		return selection, nil
	}

	peerTiers := routingPlan.orderedPeerTiersForModel(model)
	for _, tier := range peerTiers {
		eligibleCandidates, err := s.evaluateModelPeerTargetsFromRoutingPlan(profileID, routingPlan, model, strategy, tier.WeightedPeerSet.Targets, ctx, &selection)
		if err != nil {
			return runtimeModelPeerSelection{}, err
		}
		if len(eligibleCandidates) == 0 {
			continue
		}
		selection.eligibleTotalWeight = runtimeAccessCandidateTotalWeight(eligibleCandidates)
		selectedCandidate := selectWeightedRuntimeAccessCandidate(profileID, model.ID, eligibleCandidates, s.runtimeState)
		if selectedCandidate == nil {
			return selection, nil
		}
		selected := *selectedCandidate
		selection.selectedCandidate = &selected
		return selection, nil
	}
	return selection, nil
}

func (s *Service) evaluateModelPeerTargetsFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, targets []runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext, selection *runtimeModelPeerSelection) ([]runtimeResolvedAccessCandidate, error) {
	eligibleCandidates := make([]runtimeResolvedAccessCandidate, 0, len(targets))
	var firstCompatibilityError error
	for _, target := range targets {
		evaluation, err := s.evaluateAccessTargetCandidateFromRoutingPlan(profileID, routingPlan, model, strategy, target, ctx)
		if err != nil {
			return nil, err
		}
		if firstCompatibilityError == nil && evaluation.compatibilityError != nil {
			firstCompatibilityError = evaluation.compatibilityError
		}
		if evaluation.contextFitEvaluated {
			selection.contextFitEvaluated = true
			if evaluation.largestUsableContextWindowTokens > selection.largestUsableContextWindowTokens {
				selection.largestUsableContextWindowTokens = evaluation.largestUsableContextWindowTokens
			}
		}
		selection.skippedTerminalTargets = append(selection.skippedTerminalTargets, evaluation.skippedTerminalTargets...)
		if evaluation.eligibleCandidate != nil {
			eligibleCandidates = append(eligibleCandidates, *evaluation.eligibleCandidate)
		}
	}
	eligibleCandidates = preferNativeResolvedAccessCandidates(eligibleCandidates)
	if len(eligibleCandidates) == 0 && firstCompatibilityError != nil && selection.compatibilityError == nil {
		selection.compatibilityError = firstCompatibilityError
	}
	return eligibleCandidates, nil
}

func (s *Service) resolveModelAccessFromSnapshot(profileID int, snapshot *planningSnapshot, model runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	return s.resolveModelAccessFromRoutingPlan(profileID, routingPlan, model, ctx)
}

func (s *Service) resolveModelAccessFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	if ctx.Depth > runtimeAccessResolverMaxDepth {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access graph exceeded maximum depth of %d while resolving model '%s'.", runtimeAccessResolverMaxDepth, ctx.RequestedModelID)}
	}
	if _, seen := ctx.VisitedModelIDs[model.ID]; seen {
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access cycle detected while resolving model '%s'.", ctx.RequestedModelID)}
	}
	if err := validateRuntimeModelFacadePolicies(model); err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	strategy, ok := routingPlan.strategyForModel(model)
	if !ok {
		return runtimeResolvedAccessPlan{}, fmt.Errorf("model %q is missing loadbalance_strategy", model.ModelID)
	}
	visited := cloneVisitedModelIDs(ctx.VisitedModelIDs)
	visited[model.ID] = struct{}{}
	childContext := ctx
	childContext.VisitedModelIDs = visited
	childContext.Depth++

	peerSelection, err := s.selectModelPeerCandidateFromRoutingPlan(profileID, routingPlan, model, strategy, childContext)
	if err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	if peerSelection.selectedCandidate != nil {
		if strategy.IsCheapestEligibleContextStrategy() && ctx.rejectsMissingContextEstimation() {
			return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
		}
		resolved := peerSelection.selectedCandidate.resolved
		if strategy.IsCheapestEligibleContextStrategy() && resolved.ContextRouting == nil {
			resolved.ContextRouting = buildRuntimeContextRoutingDecision(strategy, ctx.RequestContextEstimation, peerSelection.selectedCandidate, runtimeContextRoutingCostRankingMethod, peerSelection.largestUsableContextWindowTokens, peerSelection.skippedTerminalTargets)
		}
		if resolved.LargestUsableContextWindowTokens < peerSelection.largestUsableContextWindowTokens {
			resolved.LargestUsableContextWindowTokens = peerSelection.largestUsableContextWindowTokens
		}
		resolved.ContextFitEvaluated = resolved.ContextFitEvaluated || peerSelection.contextFitEvaluated
		return resolved, nil
	}
	if len(peerSelection.eligibleCandidates) > 0 {
		resolved := runtimeResolvedAccessPlan{RuntimeStates: map[int]loadbalance.RuntimeConnectionState{}, Strategy: strategy}
		for _, candidate := range peerSelection.eligibleCandidates {
			appendRuntimeResolvedAccessPlan(&resolved, candidate.resolved)
		}
		if len(resolved.TerminalAttempts) > 0 && len(resolved.Connections) > 0 {
			if resolved.LargestUsableContextWindowTokens < peerSelection.largestUsableContextWindowTokens {
				resolved.LargestUsableContextWindowTokens = peerSelection.largestUsableContextWindowTokens
			}
			resolved.ContextFitEvaluated = resolved.ContextFitEvaluated || peerSelection.contextFitEvaluated
			return resolved, nil
		}
	}

	orderedTerminalTargets := routingPlan.orderedTerminalTargetsForStrategy(profileID, model, strategy, s.runtimeState)
	if len(orderedTerminalTargets) > 0 {
		if strategy.IsCheapestEligibleContextStrategy() {
			return s.resolveCheapestEligibleContextModelAccess(profileID, routingPlan, model, strategy, orderedTerminalTargets, childContext)
		}
		resolved := runtimeResolvedAccessPlan{RuntimeStates: map[int]loadbalance.RuntimeConnectionState{}, Strategy: strategy}
		var firstCompatibilityError error
		for _, target := range orderedTerminalTargets {
			candidate, eligible, err := s.resolveAccessTargetFromRoutingPlan(profileID, routingPlan, model, strategy, target, childContext)
			if err != nil {
				return runtimeResolvedAccessPlan{}, err
			}
			if firstCompatibilityError == nil && candidate.CompatibilityError != nil {
				firstCompatibilityError = candidate.CompatibilityError
			}
			if !eligible {
				continue
			}
			appendRuntimeResolvedAccessPlan(&resolved, candidate)
		}
		if len(resolved.TerminalAttempts) > 0 && len(resolved.Connections) > 0 {
			compatibleResolved, compatible, err := s.applyIngressOperationCompatibility(resolved, childContext)
			if err != nil {
				return runtimeResolvedAccessPlan{}, err
			}
			if compatible {
				return compatibleResolved, nil
			}
		}
		if firstCompatibilityError != nil {
			return runtimeResolvedAccessPlan{}, firstCompatibilityError
		}
	}
	if peerSelection.compatibilityError != nil {
		return runtimeResolvedAccessPlan{}, peerSelection.compatibilityError
	}
	if strategy.IsCheapestEligibleContextStrategy() && ctx.rejectsMissingContextEstimation() {
		return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
	}
	if peerSelection.contextFitEvaluated && ctx.RequestContextEstimation != nil {
		return runtimeResolvedAccessPlan{}, &noContextEligibleTargetsError{
			requestedModelID:                 ctx.RequestedModelID,
			estimatedTotalContextTokens:      ctx.RequestContextEstimation.EstimatedTotalContextTokens,
			largestUsableContextWindowTokens: peerSelection.largestUsableContextWindowTokens,
			consideredModelPath:              cloneRuntimeModelPath(ctx.ConsideredModelPath),
			skippedTerminalTargets:           peerSelection.skippedTerminalTargets,
		}
	}
	return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID}
}

func (s *Service) resolveCheapestEligibleContextModelAccess(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, orderedTargets []runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	eligibleCandidates := make([]runtimeResolvedAccessCandidate, 0, len(orderedTargets))
	skippedTerminalTargets := make([]runtimeContextRoutingSkippedTerminalTarget, 0, len(orderedTargets))
	largestUsableContextWindowTokens := 0
	contextFitEvaluated := false
	var firstCompatibilityError error
	for _, target := range orderedTargets {
		evaluation, err := s.evaluateAccessTargetCandidateFromRoutingPlan(profileID, routingPlan, model, strategy, target, ctx)
		if err != nil {
			return runtimeResolvedAccessPlan{}, err
		}
		if firstCompatibilityError == nil && evaluation.compatibilityError != nil {
			firstCompatibilityError = evaluation.compatibilityError
		}
		if evaluation.contextFitEvaluated {
			contextFitEvaluated = true
			if evaluation.largestUsableContextWindowTokens > largestUsableContextWindowTokens {
				largestUsableContextWindowTokens = evaluation.largestUsableContextWindowTokens
			}
		}
		skippedTerminalTargets = append(skippedTerminalTargets, evaluation.skippedTerminalTargets...)
		if evaluation.eligibleCandidate != nil {
			eligibleCandidates = append(eligibleCandidates, *evaluation.eligibleCandidate)
		}
	}
	eligibleCandidates = preferNativeResolvedAccessCandidates(eligibleCandidates)
	if len(eligibleCandidates) == 0 {
		if firstCompatibilityError != nil {
			return runtimeResolvedAccessPlan{}, firstCompatibilityError
		}
		if ctx.rejectsMissingContextEstimation() {
			return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
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
	if ctx.rejectsMissingContextEstimation() {
		return runtimeResolvedAccessPlan{}, contextEstimationUnavailableDomainError()
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
	resolved.ContextFitEvaluated = contextFitEvaluated
	return resolved, nil
}

func (s *Service) evaluateAccessTargetCandidateFromSnapshot(profileID int, snapshot *planningSnapshot, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessCandidateEvaluation, error) {
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return runtimeResolvedAccessCandidateEvaluation{}, err
	}
	return s.evaluateAccessTargetCandidateFromRoutingPlan(profileID, routingPlan, model, strategy, target, ctx)
}

func (s *Service) evaluateAccessTargetCandidateFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessCandidateEvaluation, error) {
	candidate, eligible, err := s.resolveAccessTargetFromRoutingPlan(profileID, routingPlan, model, strategy, target, ctx)
	if err != nil {
		return runtimeResolvedAccessCandidateEvaluation{}, err
	}

	evaluation := runtimeResolvedAccessCandidateEvaluation{
		largestUsableContextWindowTokens: candidate.LargestUsableContextWindowTokens,
		contextFitEvaluated:              candidate.ContextFitEvaluated,
	}
	if !eligible || len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		if candidate.CompatibilityError != nil {
			evaluation.compatibilityError = candidate.CompatibilityError
		}
		evaluation.skippedTerminalTargets = append(evaluation.skippedTerminalTargets, candidateSkippedTerminalTargets(candidate)...)
		return evaluation, nil
	}

	compatibleCandidate, compatible, err := s.applyIngressOperationCompatibility(candidate, ctx)
	if err != nil {
		if domainErr, ok := isRequestTranslationUnsupportedError(err); ok && domainErr != nil {
			evaluation.compatibilityError = domainErr
			return evaluation, nil
		}
		return runtimeResolvedAccessCandidateEvaluation{}, err
	}
	if !compatible || len(compatibleCandidate.TerminalAttempts) == 0 || len(compatibleCandidate.Connections) == 0 {
		return evaluation, nil
	}

	contextFilteredCandidate := compatibleCandidate
	if runtimeStrategyUsesContextFiltering(strategy) {
		filteredCandidate, skippedTerminalTargets, largestUsableContextWindowTokens, contextFitEvaluated := filterRuntimeResolvedAccessPlanByContext(compatibleCandidate, ctx.RequestContextEstimation)
		contextFilteredCandidate = filteredCandidate
		if contextFitEvaluated {
			evaluation.contextFitEvaluated = true
			if largestUsableContextWindowTokens > evaluation.largestUsableContextWindowTokens {
				evaluation.largestUsableContextWindowTokens = largestUsableContextWindowTokens
			}
			evaluation.skippedTerminalTargets = append(evaluation.skippedTerminalTargets, skippedTerminalTargets...)
		}
	}
	if len(contextFilteredCandidate.TerminalAttempts) == 0 || len(contextFilteredCandidate.Connections) == 0 {
		return evaluation, nil
	}

	terminalAttempt := contextFilteredCandidate.TerminalAttempts[0]
	contextBand := runtimeContextEligibilityBandPreferred
	if runtimeStrategyUsesContextFiltering(strategy) {
		contextBand = classifyRequestContextBand(ctx.RequestContextEstimation, terminalAttempt.Connection)
	}
	costMicros, priced := estimateRuntimeBlendedRequestCost(terminalAttempt.Connection, ctx.RequestContextEstimation)
	eligibleCandidate := runtimeResolvedAccessCandidate{target: target, resolved: contextFilteredCandidate, contextBand: contextBand, priced: priced, costMicros: costMicros}
	evaluation.eligibleCandidate = &eligibleCandidate
	return evaluation, nil
}

func appendRuntimeResolvedAccessPlan(resolved *runtimeResolvedAccessPlan, candidate runtimeResolvedAccessPlan) {
	if resolved == nil {
		return
	}
	if len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		if candidate.LargestUsableContextWindowTokens > resolved.LargestUsableContextWindowTokens {
			resolved.LargestUsableContextWindowTokens = candidate.LargestUsableContextWindowTokens
		}
		resolved.ContextFitEvaluated = resolved.ContextFitEvaluated || candidate.ContextFitEvaluated
		return
	}
	if len(resolved.TerminalAttempts) == 0 {
		resolved.TargetModel = candidate.TargetModel
		resolved.SelectedTerminalTargetID = cloneRuntimeIntPointer(candidate.SelectedTerminalTargetID)
		resolved.ContextRouting = cloneRuntimeContextRoutingDecision(candidate.ContextRouting)
		resolved.Strategy = candidate.Strategy
		resolved.RouteReason = candidate.RouteReason
	} else {
		resolved.RouteReason = mergeRuntimeRouteReason(resolved.RouteReason, candidate.RouteReason)
	}
	resolved.Connections = append(resolved.Connections, candidate.Connections...)
	resolved.TerminalAttempts = append(resolved.TerminalAttempts, candidate.TerminalAttempts...)
	for connectionID, state := range candidate.RuntimeStates {
		resolved.RuntimeStates[connectionID] = state
	}
	if candidate.LargestUsableContextWindowTokens > resolved.LargestUsableContextWindowTokens {
		resolved.LargestUsableContextWindowTokens = candidate.LargestUsableContextWindowTokens
	}
	resolved.ContextFitEvaluated = resolved.ContextFitEvaluated || candidate.ContextFitEvaluated
}

func candidateSkippedTerminalTargets(candidate runtimeResolvedAccessPlan) []runtimeContextRoutingSkippedTerminalTarget {
	if candidate.ContextRouting == nil {
		return nil
	}
	return cloneRuntimeContextRoutingSkippedTerminalTargets(candidate.ContextRouting.SkippedTerminalTargets)
}

func mergeRuntimeRouteReason(current gatewaycore.RouteReason, next gatewaycore.RouteReason) gatewaycore.RouteReason {
	if current != "" && current != gatewaycore.RouteReasonDirectMatch {
		return current
	}
	if next != "" {
		return next
	}
	return gatewaycore.RouteReasonDirectMatch
}

func runtimeStrategyUsesContextFiltering(strategy loadbalance.RuntimeStrategy) bool {
	strategyType := normalizedRuntimeLegacyStrategyType(strategy)
	return strategy.IsCheapestEligibleContextStrategy() || strategyType == runtimeFacadeSelectionPolicyWeightedEligibleContext
}

func filterRuntimeResolvedAccessPlanByContext(candidate runtimeResolvedAccessPlan, estimation *requestContextEstimation) (runtimeResolvedAccessPlan, []runtimeContextRoutingSkippedTerminalTarget, int, bool) {
	largestUsableContextWindowTokens := candidate.LargestUsableContextWindowTokens
	if estimation == nil || len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		return candidate, nil, largestUsableContextWindowTokens, false
	}

	filtered := candidate
	filteredAttempts := make([]runtimeTerminalAttempt, 0, len(candidate.TerminalAttempts))
	filteredConnections := make([]runtimeConnection, 0, len(candidate.Connections))
	filteredStates := make(map[int]loadbalance.RuntimeConnectionState, len(candidate.RuntimeStates))
	skippedTerminalTargets := make([]runtimeContextRoutingSkippedTerminalTarget, 0)
	for _, attempt := range candidate.TerminalAttempts {
		usableContextWindowTokens := usableContextWindowTokensForConnection(attempt.Connection)
		if usableContextWindowTokens > largestUsableContextWindowTokens {
			largestUsableContextWindowTokens = usableContextWindowTokens
		}
		if !estimation.fitsUsableContextWindowTokens(usableContextWindowTokens) {
			skippedTerminalTargets = append(skippedTerminalTargets, buildRuntimeContextRoutingSkippedTerminalTarget(attempt.Connection, estimation, usableContextWindowTokens))
			continue
		}
		filteredAttempts = append(filteredAttempts, attempt)
		filteredConnections = append(filteredConnections, attempt.Connection)
		if state, ok := candidate.RuntimeStates[attempt.Connection.ID]; ok {
			filteredStates[attempt.Connection.ID] = state
		}
	}

	filtered.TerminalAttempts = filteredAttempts
	filtered.Connections = filteredConnections
	filtered.RuntimeStates = filteredStates
	filtered.LargestUsableContextWindowTokens = largestUsableContextWindowTokens
	filtered.ContextFitEvaluated = true
	if len(filteredAttempts) == 0 {
		filtered.SelectedTerminalTargetID = nil
		return filtered, skippedTerminalTargets, largestUsableContextWindowTokens, true
	}
	filtered.SelectedTerminalTargetID = intPtr(filteredAttempts[0].Connection.ID)
	return filtered, skippedTerminalTargets, largestUsableContextWindowTokens, true
}

func (s *Service) applyIngressOperationCompatibility(candidate runtimeResolvedAccessPlan, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	if len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		return candidate, false, nil
	}
	if !openai.IsTextOperation(providerOperationFromRuntime(ctx.RequestOperation)) {
		return candidate, true, nil
	}
	nativeAttempts := make([]runtimeTerminalAttempt, 0, len(candidate.TerminalAttempts))
	translatedAttempts := make([]runtimeTerminalAttempt, 0, len(candidate.TerminalAttempts))
	var firstTranslationError error
	adapter := openai.New()
	for _, attempt := range candidate.TerminalAttempts {
		if !attempt.TargetModel.allowsOpenAITextSiblingTranslation() {
			continue
		}
		compatibility := planOpenAITextAttemptCompatibility(ctx.RequestOperation, ctx.RawRequestBody, attempt, adapter)
		if compatibility.Err != nil {
			if firstTranslationError == nil {
				firstTranslationError = compatibility.Err
			}
			continue
		}
		if !compatibility.Compatible {
			continue
		}
		plannedAttempt := attempt
		plannedAttempt.TranslationMode = compatibility.TranslationMode
		if plannedAttempt.TranslationMode == TranslationModeNone {
			nativeAttempts = append(nativeAttempts, plannedAttempt)
			continue
		}
		translatedAttempts = append(translatedAttempts, plannedAttempt)
	}
	if len(nativeAttempts) > 0 {
		return candidateWithCompatibleOpenAITextAttempts(candidate, nativeAttempts), true, nil
	}
	if len(translatedAttempts) > 0 {
		return candidateWithCompatibleOpenAITextAttempts(candidate, translatedAttempts), true, nil
	}
	if firstTranslationError != nil {
		return runtimeResolvedAccessPlan{}, false, firstTranslationError
	}
	return runtimeResolvedAccessPlan{}, false, nil
}

func candidateWithCompatibleOpenAITextAttempts(candidate runtimeResolvedAccessPlan, attempts []runtimeTerminalAttempt) runtimeResolvedAccessPlan {
	connections := make([]runtimeConnection, 0, len(attempts))
	states := make(map[int]loadbalance.RuntimeConnectionState, len(candidate.RuntimeStates))
	for _, attempt := range attempts {
		connections = append(connections, attempt.Connection)
		if state, ok := candidate.RuntimeStates[attempt.Connection.ID]; ok {
			states[attempt.Connection.ID] = state
		}
	}
	candidate.TargetModel = attempts[0].TargetModel
	candidate.Strategy = attempts[0].Strategy
	candidate.TerminalAttempts = attempts
	candidate.Connections = connections
	candidate.RuntimeStates = states
	candidate.SelectedTerminalTargetID = intPtr(attempts[0].Connection.ID)
	return candidate
}

func preferNativeResolvedAccessCandidates(candidates []runtimeResolvedAccessCandidate) []runtimeResolvedAccessCandidate {
	nativeCandidates := make([]runtimeResolvedAccessCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if len(candidate.resolved.TerminalAttempts) == 0 {
			continue
		}
		if candidate.resolved.TerminalAttempts[0].TranslationMode == TranslationModeNone {
			nativeCandidates = append(nativeCandidates, candidate)
		}
	}
	if len(nativeCandidates) > 0 {
		return nativeCandidates
	}
	return candidates
}

func buildRuntimeContextRoutingSkippedTerminalTarget(connection runtimeConnection, estimation *requestContextEstimation, usableContextWindowTokens int) runtimeContextRoutingSkippedTerminalTarget {
	skippedTarget := runtimeContextRoutingSkippedTerminalTarget{
		TerminalTargetID:          intPtr(connection.ID),
		EndpointID:                intPtr(connection.Endpoint.ID),
		ContextBand:               stringPtr(runtimeContextBandIneligible),
		UsableContextWindowTokens: runtimeContextWindowTokensPointer(usableContextWindowTokens),
	}
	if estimation != nil {
		skippedTarget.EstimatedTotalContextTokens = intPtr(estimation.EstimatedTotalContextTokens)
	}
	if usableContextWindowTokens > 0 {
		skippedTarget.Reason = runtimeContextRoutingSkipReasonEstimatedContextExceedsUsableWindow
	} else {
		skippedTarget.Reason = runtimeContextRoutingSkipReasonUsableContextWindowUnavailable
	}
	return skippedTarget
}

const runtimeFacadeExclusionReasonTranslationRejection = "translation_rejection"

func runtimeAccessCandidateTotalWeight(candidates []runtimeResolvedAccessCandidate) int {
	totalWeight := 0
	for _, candidate := range candidates {
		totalWeight += effectiveRuntimeAccessTargetWeight(candidate.target)
	}
	return totalWeight
}

func buildRuntimeFacadeSelectionDecision(facadeModelID string, selectedCandidate *runtimeResolvedAccessCandidate, eligibleTotalWeight int, skippedTerminalTargets []runtimeContextRoutingSkippedTerminalTarget, translatedRejectedCount int) *runtimeFacadeSelectionDecision {
	trimmedFacadeModelID := strings.TrimSpace(facadeModelID)
	if trimmedFacadeModelID == "" {
		return nil
	}
	decision := &runtimeFacadeSelectionDecision{
		FacadeModelID:       trimmedFacadeModelID,
		EligibleTotalWeight: intPtr(eligibleTotalWeight),
	}
	if selectedCandidate != nil {
		decision.SelectedTargetModelID = stringPointerIfNotEmpty(selectedCandidate.target.TargetModelID)
		decision.SelectedWeight = intPtr(effectiveRuntimeAccessTargetWeight(selectedCandidate.target))
	}
	decision.ExclusionReasons = buildRuntimeFacadeExclusionReasons(skippedTerminalTargets, translatedRejectedCount)
	decision.ExclusionSummary = buildRuntimeFacadeExclusionSummary(decision.ExclusionReasons)
	return decision
}

func buildRuntimeFacadeExclusionReasons(skippedTerminalTargets []runtimeContextRoutingSkippedTerminalTarget, translatedRejectedCount int) []runtimeFacadeExclusionReason {
	counts := map[string]int{}
	for _, skippedTarget := range skippedTerminalTargets {
		reason := strings.TrimSpace(skippedTarget.Reason)
		if reason == "" {
			continue
		}
		counts[reason]++
	}
	if translatedRejectedCount > 0 {
		counts[runtimeFacadeExclusionReasonTranslationRejection] += translatedRejectedCount
	}
	if len(counts) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	exclusions := make([]runtimeFacadeExclusionReason, 0, len(reasons))
	for _, reason := range reasons {
		exclusions = append(exclusions, runtimeFacadeExclusionReason{Reason: reason, Count: counts[reason]})
	}
	return exclusions
}

func buildRuntimeFacadeExclusionSummary(exclusions []runtimeFacadeExclusionReason) *string {
	if len(exclusions) == 0 {
		return nil
	}
	parts := make([]string, 0, len(exclusions))
	for _, exclusion := range exclusions {
		parts = append(parts, fmt.Sprintf("%s=%d", exclusion.Reason, exclusion.Count))
	}
	summary := strings.Join(parts, ",")
	return stringPtr(summary)
}

func attachRuntimeFacadeSelectionDecision(contextRouting *runtimeContextRoutingDecision, facadeSelection *runtimeFacadeSelectionDecision) *runtimeContextRoutingDecision {
	if facadeSelection == nil {
		return cloneRuntimeContextRoutingDecision(contextRouting)
	}
	if contextRouting == nil {
		return &runtimeContextRoutingDecision{
			FacadeSelection: cloneRuntimeFacadeSelectionDecision(facadeSelection),
			PlannerTrace:    buildRuntimePlannerTraceDecision("", nil, nil, facadeSelection),
		}
	}
	cloned := cloneRuntimeContextRoutingDecision(contextRouting)
	cloned.FacadeSelection = cloneRuntimeFacadeSelectionDecision(facadeSelection)
	if cloned.PlannerTrace == nil {
		cloned.PlannerTrace = buildRuntimePlannerTraceDecision(cloned.Policy, nil, cloned.SkippedTerminalTargets, facadeSelection)
	} else {
		cloned.PlannerTrace.FacadeExclusionSummary = cloneRuntimeStringPointer(facadeSelection.ExclusionSummary)
		if cloned.PlannerTrace.SelectedTargetModelID == nil {
			cloned.PlannerTrace.SelectedTargetModelID = cloneRuntimeStringPointer(facadeSelection.SelectedTargetModelID)
		}
	}
	return cloned
}

func buildRuntimeFacadeContextRoutingDecision(estimation *requestContextEstimation, selectedCandidate *runtimeResolvedAccessCandidate, usableContextWindowTokens int, skippedTerminalTargets []runtimeContextRoutingSkippedTerminalTarget, facadeSelection *runtimeFacadeSelectionDecision) *runtimeContextRoutingDecision {
	contextRouting := buildRuntimeContextRoutingDecision(runtimeFacadeSelectionStrategy(), estimation, selectedCandidate, "", usableContextWindowTokens, skippedTerminalTargets)
	if contextRouting == nil {
		contextRouting = &runtimeContextRoutingDecision{Policy: runtimeFacadeSelectionPolicyWeightedEligibleContext}
		if estimation != nil {
			contextRouting.EstimationMethod = stringPointerIfNotEmpty(estimation.Method)
			contextRouting.EstimatedInputTokens = intPtr(estimation.EstimatedInputTokens)
			contextRouting.ReservedOutputTokens = intPtr(estimation.ReservedOutputTokens)
			contextRouting.EstimatedTotalContextTokens = intPtr(estimation.EstimatedTotalContextTokens)
		}
		if usableContextWindowTokens > 0 {
			contextRouting.UsableContextWindowTokens = intPtr(usableContextWindowTokens)
		}
		contextRouting.SkippedTerminalTargets = cloneRuntimeContextRoutingSkippedTerminalTargets(skippedTerminalTargets)
		contextRouting.PlannerTrace = buildRuntimePlannerTraceDecision(contextRouting.Policy, selectedCandidate, skippedTerminalTargets, nil)
	}
	return attachRuntimeFacadeSelectionDecision(contextRouting, facadeSelection)
}

func buildRuntimeContextRoutingDecision(strategy loadbalance.RuntimeStrategy, estimation *requestContextEstimation, selectedCandidate *runtimeResolvedAccessCandidate, costRankingMethod string, usableContextWindowTokens int, skippedTerminalTargets []runtimeContextRoutingSkippedTerminalTarget) *runtimeContextRoutingDecision {
	if !strategy.IsCheapestEligibleContextStrategy() && len(skippedTerminalTargets) == 0 && selectedCandidate == nil {
		return nil
	}
	decision := &runtimeContextRoutingDecision{
		Policy:                    runtimeContextRoutingPolicyName(strategy),
		CostRankingMethod:         cloneRuntimeStringPointer(stringPointerIfNotEmpty(costRankingMethod)),
		UsableContextWindowTokens: runtimeContextWindowTokensPointer(usableContextWindowTokens),
		SkippedTerminalTargets:    cloneRuntimeContextRoutingSkippedTerminalTargets(skippedTerminalTargets),
	}
	if estimation != nil {
		decision.EstimationMethod = cloneRuntimeStringPointer(stringPointerIfNotEmpty(estimation.Method))
		decision.EstimatedInputTokens = intPtr(estimation.EstimatedInputTokens)
		decision.ReservedOutputTokens = intPtr(estimation.ReservedOutputTokens)
		decision.EstimatedTotalContextTokens = intPtr(estimation.EstimatedTotalContextTokens)
	}
	if selectedCandidate != nil && len(selectedCandidate.resolved.TerminalAttempts) > 0 {
		selectedConnection := selectedCandidate.resolved.TerminalAttempts[0].Connection
		decision.SelectedTerminalTargetID = intPtr(selectedConnection.ID)
		decision.SelectedEndpointID = intPtr(selectedConnection.Endpoint.ID)
		decision.SelectedContextBand = runtimeContextBandPointer(selectedCandidate.contextBand)
		decision.SelectedUsableContextWindowTokens = runtimeContextWindowTokensPointer(usableContextWindowTokensForConnection(selectedConnection))
		if selectedCandidate.priced {
			decision.SelectedEstimatedBlendedCostMicros = int64Ptr(selectedCandidate.costMicros)
		}
	}
	decision.PlannerTrace = buildRuntimePlannerTraceDecision(decision.Policy, selectedCandidate, skippedTerminalTargets, nil)
	return decision
}

func buildRuntimePlannerTraceDecision(policy string, selectedCandidate *runtimeResolvedAccessCandidate, skippedTerminalTargets []runtimeContextRoutingSkippedTerminalTarget, facadeSelection *runtimeFacadeSelectionDecision) *runtimePlannerTraceDecision {
	decision := &runtimePlannerTraceDecision{
		PlannerVersion:         runtimePlannerTraceVersion,
		Decision:               runtimePlannerTraceDecisionNoTarget,
		Policy:                 strings.TrimSpace(policy),
		SkippedTerminalTargets: len(skippedTerminalTargets),
	}
	if selectedCandidate != nil {
		decision.Decision = runtimePlannerTraceDecisionSelected
		decision.AccessTargetID = intPtr(selectedCandidate.target.ID)
		decision.AccessTargetType = stringPointerIfNotEmpty(selectedCandidate.target.TargetType)
		decision.SelectedTargetModelID = stringPointerIfNotEmpty(selectedCandidate.target.TargetModelID)
		decision.SelectedTierPriority = intPtr(selectedCandidate.target.TargetPriority)
		if len(selectedCandidate.resolved.TerminalAttempts) > 0 {
			attempt := selectedCandidate.resolved.TerminalAttempts[0]
			decision.SelectedTerminalTargetID = intPtr(attempt.Connection.ID)
			decision.TranslationMode = runtimeTranslationModePointer(attempt.TranslationMode)
		}
	} else if len(skippedTerminalTargets) > 0 {
		decision.Decision = runtimePlannerTraceDecisionNoFit
	}
	if facadeSelection != nil {
		decision.FacadeExclusionSummary = cloneRuntimeStringPointer(facadeSelection.ExclusionSummary)
		if decision.SelectedTargetModelID == nil {
			decision.SelectedTargetModelID = cloneRuntimeStringPointer(facadeSelection.SelectedTargetModelID)
		}
	}
	return decision
}

func runtimeContextRoutingPolicyName(strategy loadbalance.RuntimeStrategy) string {
	if strategy.LegacyStrategyType == nil {
		return ""
	}
	return strings.TrimSpace(*strategy.LegacyStrategyType)
}

func runtimeContextWindowTokensPointer(value int) *int {
	if value <= 0 {
		return nil
	}
	return intPtr(value)
}

func compareRuntimeResolvedAccessCandidates(left runtimeResolvedAccessCandidate, right runtimeResolvedAccessCandidate) int {
	if left.contextBand != right.contextBand {
		if left.contextBand > right.contextBand {
			return -1
		}
		return 1
	}
	if left.priced != right.priced {
		if left.priced {
			return -1
		}
		return 1
	}
	if left.priced && right.priced && left.costMicros != right.costMicros {
		if left.costMicros < right.costMicros {
			return -1
		}
		return 1
	}
	if left.target.Position != right.target.Position {
		if left.target.Position < right.target.Position {
			return -1
		}
		return 1
	}
	leftTerminalTargetID := left.resolved.TerminalAttempts[0].Connection.ID
	rightTerminalTargetID := right.resolved.TerminalAttempts[0].Connection.ID
	if leftTerminalTargetID < rightTerminalTargetID {
		return -1
	}
	if leftTerminalTargetID > rightTerminalTargetID {
		return 1
	}
	if left.target.ID < right.target.ID {
		return -1
	}
	if left.target.ID > right.target.ID {
		return 1
	}
	return 0
}

func preferredContextWindowTokensForConnection(connection runtimeConnection) int {
	if connection.PreferredContextUtilizationThreshold == nil {
		return usableContextWindowTokensForConnection(connection)
	}
	preferredContextWindowTokens := computeUsableContextWindowTokens(connection.ContextWindowTokens, connection.PreferredContextUtilizationThreshold)
	if preferredContextWindowTokens == nil || *preferredContextWindowTokens <= 0 {
		return 0
	}
	return *preferredContextWindowTokens
}

func classifyRequestContextBand(estimation *requestContextEstimation, connection runtimeConnection) runtimeContextEligibilityBand {
	if !requestContextFitsConnection(estimation, connection) {
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

func usableContextWindowTokensForConnection(connection runtimeConnection) int {
	maxContextUtilization := connection.MaxContextUtilization
	usableContextWindowTokens := computeUsableContextWindowTokens(connection.ContextWindowTokens, &maxContextUtilization)
	if usableContextWindowTokens == nil || *usableContextWindowTokens <= 0 {
		return 0
	}
	return *usableContextWindowTokens
}

func requestContextFitsConnection(estimation *requestContextEstimation, connection runtimeConnection) bool {
	usableContextWindowTokens := usableContextWindowTokensForConnection(connection)
	return estimation.fitsUsableContextWindowTokens(usableContextWindowTokens)
}

func estimateRuntimeBlendedRequestCost(connection runtimeConnection, estimation *requestContextEstimation) (int64, bool) {
	if estimation == nil || connection.PricingTemplateSnapshot == nil {
		return 0, false
	}
	pricingTemplateSnapshot := connection.PricingTemplateSnapshot
	if strings.TrimSpace(pricingTemplateSnapshot.PricingUnit) != runtimePricingUnitPerMillion {
		return 0, false
	}
	inputTokens := estimation.EstimatedInputTokens
	outputTokens := estimation.ReservedOutputTokens
	inputCostMicros, ok := runtimePriceConcreteComponentMicros(&inputTokens, pricingTemplateSnapshot.InputPrice)
	if !ok {
		return 0, false
	}
	outputCostMicros, ok := runtimePriceConcreteComponentMicros(&outputTokens, pricingTemplateSnapshot.OutputPrice)
	if !ok {
		return 0, false
	}
	return runtimeSumMicros(inputCostMicros, outputCostMicros), true
}

func (s *Service) resolveAccessTargetFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, sourceModel runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	if !target.IsEnabled || target.ProfileID != profileID || target.SourceModelConfigID != sourceModel.ID {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	switch target.TargetType {
	case runtimeAccessTargetTypeConnection:
		return s.resolveTerminalTargetFromRoutingPlan(profileID, routingPlan, sourceModel, strategy, target, ctx)
	case runtimeAccessTargetTypeModel:
		return s.resolveModelAccessTargetFromRoutingPlan(profileID, routingPlan, sourceModel, target, ctx)
	default:
		return runtimeResolvedAccessPlan{}, false, nil
	}
}

func (s *Service) resolveTerminalTargetFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, sourceModel runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	connection, ok := routingPlan.terminalConnectionForAccessTarget(sourceModel, target)
	if !ok {
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
	eligibleConnectionIDs := loadbalance.FilterEligibleConnectionIDs(toConnectionOrderCandidates([]runtimeConnection{resolvedConnection}), runtimeStates, ctx.ReferenceNow)
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
	routeReason := gatewaycore.RouteReasonDirectMatch
	if runtimeTerminalTargetIsUpstreamRedirect(routingPlan, sourceModel, target, ctx) {
		routeReason = gatewaycore.RouteReasonUpstreamRedirect
	}
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
		RouteReason:                      routeReason,
		LargestUsableContextWindowTokens: largestUsableContextWindowTokens,
	}, true, nil
}

func runtimeTerminalTargetIsUpstreamRedirect(routingPlan *runtimeRoutingPlan, sourceModel runtimeModelRecord, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) bool {
	if target.TargetType != runtimeAccessTargetTypeConnection || strings.TrimSpace(sourceModel.ModelID) != strings.TrimSpace(ctx.RequestedModelID) {
		return false
	}
	return len(routingPlan.orderedPeerTiersForModel(sourceModel)) > 0
}

func (s *Service) resolveModelAccessTargetFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, sourceModel runtimeModelRecord, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	childModel, ok, err := routingPlan.modelForAccessTarget(sourceModel, target)
	if err != nil {
		return runtimeResolvedAccessPlan{}, false, err
	}
	if !ok {
		return runtimeResolvedAccessPlan{}, false, nil
	}
	childContext := ctx
	childContext.ConsideredModelPath = appendRuntimeModelPath(ctx.ConsideredModelPath, childModel.ModelID)
	resolved, err := s.resolveModelAccessFromRoutingPlan(profileID, routingPlan, childModel, childContext)
	if err != nil {
		if domainErr, ok := isRequestTranslationUnsupportedError(err); ok && domainErr != nil {
			return runtimeResolvedAccessPlan{CompatibilityError: domainErr}, false, nil
		}
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			return runtimeResolvedAccessPlan{}, false, nil
		}
		var noContextEligible *noContextEligibleTargetsError
		if errors.As(err, &noContextEligible) {
			contextRouting := buildRuntimeContextRoutingDecision(
				loadbalance.RuntimeStrategy{LegacyStrategyType: stringPtr("cheapest_eligible_context")},
				ctx.RequestContextEstimation,
				nil,
				runtimeContextRoutingCostRankingMethod,
				noContextEligible.largestUsableContextWindowTokens,
				noContextEligible.skippedTerminalTargets,
			)
			return runtimeResolvedAccessPlan{ContextRouting: contextRouting, LargestUsableContextWindowTokens: noContextEligible.largestUsableContextWindowTokens, ContextFitEvaluated: true}, false, nil
		}
		return runtimeResolvedAccessPlan{}, false, err
	}
	resolved.RouteReason = mergeRuntimeRouteReason(gatewaycore.RouteReasonModelRedirect, resolved.RouteReason)
	if resolved.ContextRouting != nil {
		resolved.ContextRouting = runtimeContextRoutingWithRouteReason(resolved.ContextRouting, resolved.RouteReason, resolved.ContextRouting.Policy)
	}
	return resolved, true, nil
}

func cloneVisitedModelIDs(source map[int]struct{}) map[int]struct{} {
	cloned := make(map[int]struct{}, len(source)+1)
	for modelID := range source {
		cloned[modelID] = struct{}{}
	}
	return cloned
}

func appendRuntimeModelPath(source []string, modelID string) []string {
	cloned := cloneRuntimeModelPath(source)
	trimmedModelID := strings.TrimSpace(modelID)
	if trimmedModelID == "" {
		return cloned
	}
	return append(cloned, trimmedModelID)
}

func cloneRuntimeModelPath(source []string) []string {
	if len(source) == 0 {
		return nil
	}
	cloned := make([]string, 0, len(source))
	for _, modelID := range source {
		trimmedModelID := strings.TrimSpace(modelID)
		if trimmedModelID != "" {
			cloned = append(cloned, trimmedModelID)
		}
	}
	return cloned
}

func listEnabledModelsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[string]runtimeModelRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_configs.id, model_configs.profile_id, model_configs.api_family, model_configs.model_id,
			model_configs.loadbalance_strategy_id, model_configs.facade_enabled, model_configs.facade_selection_policy,
			model_configs.facade_fallback_policy, model_configs.context_window_tokens, model_configs.default_output_token_reserve,
			model_configs.max_context_utilization, model_configs.preferred_context_utilization_threshold,
			model_configs.context_overflow_promotion_target_id
		FROM model_configs
		WHERE model_configs.profile_id = $1 AND model_configs.is_enabled = TRUE
		ORDER BY model_configs.model_id ASC, model_configs.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query enabled models for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[string]runtimeModelRecord)
	for rows.Next() {
		var strategyID sql.NullInt32
		var facadeSelectionPolicy sql.NullString
		var facadeFallbackPolicy sql.NullString
		var contextWindowTokens sql.NullInt32
		var defaultOutputTokenReserve sql.NullInt32
		var maxContextUtilization sql.NullFloat64
		var preferredContextUtilizationThreshold sql.NullFloat64
		var contextOverflowPromotionTargetID sql.NullString
		item := runtimeModelRecord{}
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.APIFamily, &item.ModelID, &strategyID, &item.FacadeEnabled, &facadeSelectionPolicy, &facadeFallbackPolicy, &contextWindowTokens, &defaultOutputTokenReserve, &maxContextUtilization, &preferredContextUtilizationThreshold, &contextOverflowPromotionTargetID); err != nil {
			return nil, fmt.Errorf("scan enabled model for profile %d: %w", profileID, err)
		}
		if _, exists := items[item.ModelID]; exists {
			continue
		}
		if strategyID.Valid {
			resolved := int(strategyID.Int32)
			item.LoadbalanceStrategyID = &resolved
		}
		item.FacadeSelectionPolicy = nullableString(facadeSelectionPolicy)
		item.FacadeFallbackPolicy = nullableString(facadeFallbackPolicy)
		item.ContextWindowTokens = nullableInt32(contextWindowTokens)
		item.DefaultOutputTokenReserve = nullableInt32(defaultOutputTokenReserve)
		item.MaxContextUtilization = nullableFloat64(maxContextUtilization)
		item.PreferredContextUtilizationThreshold = nullableFloat64(preferredContextUtilizationThreshold)
		item.ContextOverflowPromotionTargetID = nullableString(contextOverflowPromotionTargetID)
		items[item.ModelID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled models for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listAccessTargetsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int][]runtimeAccessTargetRecord, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT model_access_targets.id, model_access_targets.profile_id, model_access_targets.source_model_config_id,
			model_access_targets.target_type, model_access_targets.target_model_config_id, target_models.model_id,
			target_models.profile_id, target_models.api_family, COALESCE(target_models.is_enabled, FALSE),
			model_access_targets.target_connection_id, connections.profile_id, connections.api_family,
			model_access_targets.position, model_access_targets.weight, model_access_targets.target_priority, model_access_targets.is_enabled,
			source_models.model_id, connections.endpoint_id, endpoint_fx_rate_settings.fx_rate::text
		FROM model_access_targets
		JOIN model_configs AS source_models ON source_models.id = model_access_targets.source_model_config_id
		LEFT JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id
		LEFT JOIN endpoint_fx_rate_settings ON endpoint_fx_rate_settings.profile_id = model_access_targets.profile_id
			AND endpoint_fx_rate_settings.model_id = source_models.model_id
			AND endpoint_fx_rate_settings.endpoint_id = connections.endpoint_id
		WHERE model_access_targets.profile_id = $1
		ORDER BY model_access_targets.source_model_config_id ASC, model_access_targets.position ASC, model_access_targets.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query access targets for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int][]runtimeAccessTargetRecord)
	for rows.Next() {
		var targetModelConfigID sql.NullInt32
		var targetModelID sql.NullString
		var targetModelProfileID sql.NullInt32
		var targetModelAPIFamily sql.NullString
		var targetModelEnabled sql.NullBool
		var targetConnectionID sql.NullInt32
		var targetConnectionProfileID sql.NullInt32
		var targetConnectionAPIFamily sql.NullString
		var weight sql.NullInt32
		var targetPriority sql.NullInt32
		var sourceModelID sql.NullString
		var connectionEndpointID sql.NullInt32
		var endpointFXRate sql.NullString
		item := runtimeAccessTargetRecord{}
		if err := rows.Scan(
			&item.ID,
			&item.ProfileID,
			&item.SourceModelConfigID,
			&item.TargetType,
			&targetModelConfigID,
			&targetModelID,
			&targetModelProfileID,
			&targetModelAPIFamily,
			&targetModelEnabled,
			&targetConnectionID,
			&targetConnectionProfileID,
			&targetConnectionAPIFamily,
			&item.Position,
			&weight,
			&targetPriority,
			&item.IsEnabled,
			&sourceModelID,
			&connectionEndpointID,
			&endpointFXRate,
		); err != nil {
			return nil, fmt.Errorf("scan access target for profile %d: %w", profileID, err)
		}
		item.TargetModelConfigID = nullableInt32(targetModelConfigID)
		item.TargetModelID = strings.TrimSpace(targetModelID.String)
		if targetModelProfileID.Valid {
			item.TargetModelProfileID = int(targetModelProfileID.Int32)
		}
		if targetModelAPIFamily.Valid {
			item.TargetModelAPIFamily = strings.TrimSpace(targetModelAPIFamily.String)
		}
		item.TargetModelEnabled = targetModelEnabled.Valid && targetModelEnabled.Bool
		item.TargetConnectionID = nullableInt32(targetConnectionID)
		if targetConnectionProfileID.Valid {
			item.TargetConnectionProfileID = int(targetConnectionProfileID.Int32)
		}
		if targetConnectionAPIFamily.Valid {
			item.TargetConnectionAPIFamily = strings.TrimSpace(targetConnectionAPIFamily.String)
		}
		item.Weight = runtimeActiveModelTargetDefaultWeight
		item.TargetPriority = item.Position
		if weight.Valid {
			item.Weight = int(weight.Int32)
		}
		if targetPriority.Valid {
			item.TargetPriority = int(targetPriority.Int32)
		}
		if endpointFXRate.Valid && connectionEndpointID.Valid && sourceModelID.Valid {
			item.ConnectionEndpointFX = &runtimeEndpointFXSnapshot{
				ModelID:    strings.TrimSpace(sourceModelID.String),
				EndpointID: int(connectionEndpointID.Int32),
				FXRate:     strings.TrimSpace(endpointFXRate.String),
			}
		}
		items[item.SourceModelConfigID] = append(items[item.SourceModelConfigID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access targets for profile %d: %w", profileID, err)
	}
	return items, nil
}

func listRuntimeStrategiesForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int]loadbalance.RuntimeStrategy, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT id, name, legacy_strategy_type, failure_status_codes, ban_mode,
			retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio,
			retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds
		FROM loadbalance_strategies
		WHERE profile_id = $1
		ORDER BY id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query runtime strategies for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int]loadbalance.RuntimeStrategy)
	for rows.Next() {
		var legacyStrategyType string
		var failureStatusCodes []int32
		item := loadbalance.RuntimeStrategy{}
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&legacyStrategyType,
			&failureStatusCodes,
			&item.BanMode,
			&item.RetryBaseDelayMS,
			&item.RetryBackoffMultiplier,
			&item.RetryJitterRatio,
			&item.RetryMaxDelayMS,
			&item.CycleRetryAttemptLimit,
			&item.BanCumulativeRetryAttemptThreshold,
			&item.BanDurationSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan runtime strategy for profile %d: %w", profileID, err)
		}
		item.LegacyStrategyType = &legacyStrategyType
		item.FailureStatusCodes = intSliceFromInt32(failureStatusCodes)
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runtime strategies for profile %d: %w", profileID, err)
	}
	return items, nil
}

func intSliceFromInt32(values []int32) []int {
	items := make([]int, 0, len(values))
	for _, value := range values {
		items = append(items, int(value))
	}
	return items
}

func listActiveConnectionsForProfile(ctx context.Context, tx pgx.Tx, profileID int) (map[int]runtimeConnection, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT connections.id, connections.profile_id, connections.api_family, connections.endpoint_id,
			connections.priority, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream,
			connections.name, connections.auth_type, connections.custom_headers, connections.pricing_template_id,
			connections.context_window_tokens, connections.default_output_token_reserve, connections.max_context_utilization,
			connections.preferred_context_utilization_threshold, connections.openai_probe_endpoint_variant, connections.openai_text_capability,
			pricing_templates.id, pricing_templates.pricing_unit, pricing_templates.pricing_currency_code,
			pricing_templates.input_price::text, pricing_templates.output_price::text,
			pricing_templates.cached_input_price::text, pricing_templates.cache_creation_price::text,
			pricing_templates.reasoning_price::text, pricing_templates.version,
			endpoints.id, endpoints.name, endpoints.base_url, endpoints.api_key
		FROM connections
		JOIN endpoints ON endpoints.id = connections.endpoint_id
		LEFT JOIN pricing_templates ON pricing_templates.id = connections.pricing_template_id
		WHERE connections.profile_id = $1 AND connections.is_active = TRUE
		ORDER BY connections.id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query active connections for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make(map[int]runtimeConnection)
	for rows.Next() {
		record, scanErr := scanRuntimeTerminalTargetRecord(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan runtime connection for profile %d: %w", profileID, scanErr)
		}
		item := runtimeConnectionFromTerminalTargetRecord(record)
		items[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active connections for profile %d: %w", profileID, err)
	}
	return items, nil
}

func scanRuntimeTerminalTargetRecord(scanner interface{ Scan(...any) error }) (terminaltarget.RuntimeRecord, error) {
	var qpsLimit sql.NullInt32
	var maxInFlightNonStream sql.NullInt32
	var maxInFlightStream sql.NullInt32
	var name sql.NullString
	var authType sql.NullString
	var customHeaders sql.NullString
	var pricingTemplateID sql.NullInt32
	var contextWindowTokens sql.NullInt32
	var defaultOutputTokenReserve sql.NullInt32
	var maxContextUtilization sql.NullFloat64
	var preferredContextUtilizationThreshold sql.NullFloat64
	var openAIProbeEndpointVariant sql.NullString
	var openAITextCapability sql.NullString
	var templateID sql.NullInt32
	var templatePricingUnit sql.NullString
	var templatePricingCurrencyCode sql.NullString
	var templateInputPrice sql.NullString
	var templateOutputPrice sql.NullString
	var templateCachedInputPrice sql.NullString
	var templateCacheCreationPrice sql.NullString
	var templateReasoningPrice sql.NullString
	var templateVersion sql.NullInt32
	var endpointName sql.NullString
	record := terminaltarget.RuntimeRecord{}
	if err := scanner.Scan(
		&record.ID,
		&record.ProfileID,
		&record.APIFamily,
		&record.EndpointID,
		&record.Priority,
		&qpsLimit,
		&maxInFlightNonStream,
		&maxInFlightStream,
		&name,
		&authType,
		&customHeaders,
		&pricingTemplateID,
		&contextWindowTokens,
		&defaultOutputTokenReserve,
		&maxContextUtilization,
		&preferredContextUtilizationThreshold,
		&openAIProbeEndpointVariant,
		&openAITextCapability,
		&templateID,
		&templatePricingUnit,
		&templatePricingCurrencyCode,
		&templateInputPrice,
		&templateOutputPrice,
		&templateCachedInputPrice,
		&templateCacheCreationPrice,
		&templateReasoningPrice,
		&templateVersion,
		&record.Endpoint.ID,
		&endpointName,
		&record.Endpoint.BaseURL,
		&record.Endpoint.EncryptedAPIKey,
	); err != nil {
		return terminaltarget.RuntimeRecord{}, err
	}
	record.QPSLimit = nullableInt32(qpsLimit)
	record.MaxInFlightNonStream = nullableInt32(maxInFlightNonStream)
	record.MaxInFlightStream = nullableInt32(maxInFlightStream)
	record.Name = nullableString(name)
	record.AuthType = nullableString(authType)
	record.CustomHeaders = parseCustomHeaders(customHeaders)
	record.PricingTemplateID = nullableInt32(pricingTemplateID)
	record.ContextWindowTokens = nullableInt32(contextWindowTokens)
	record.PreferredContextUtilizationThreshold = nullableFloat64(preferredContextUtilizationThreshold)
	record.OpenAIProbeEndpointVariant = nullableString(openAIProbeEndpointVariant)
	record.OpenAITextCapability = nullableString(openAITextCapability)
	record.Endpoint.Name = nullableString(endpointName)
	if defaultOutputTokenReserve.Valid {
		record.DefaultOutputTokenReserve = int(defaultOutputTokenReserve.Int32)
	}
	if maxContextUtilization.Valid {
		record.MaxContextUtilization = maxContextUtilization.Float64
	}
	if templateID.Valid {
		record.PricingTemplate = &terminaltarget.RuntimePricingTemplateSnapshot{
			ID:                  int(templateID.Int32),
			PricingUnit:         strings.TrimSpace(templatePricingUnit.String),
			PricingCurrencyCode: strings.TrimSpace(templatePricingCurrencyCode.String),
			InputPrice:          strings.TrimSpace(templateInputPrice.String),
			OutputPrice:         strings.TrimSpace(templateOutputPrice.String),
			CachedInputPrice:    strings.TrimSpace(templateCachedInputPrice.String),
			CacheCreationPrice:  strings.TrimSpace(templateCacheCreationPrice.String),
			ReasoningPrice:      strings.TrimSpace(templateReasoningPrice.String),
			Version:             int(templateVersion.Int32),
		}
	}
	return record, nil
}

func runtimeConnectionFromTerminalTargetRecord(record terminaltarget.RuntimeRecord) runtimeConnection {
	item := runtimeConnection{
		ID:                                   record.ID,
		ProfileID:                            record.ProfileID,
		APIFamily:                            record.APIFamily,
		EndpointID:                           record.EndpointID,
		Priority:                             record.Priority,
		QPSLimit:                             record.QPSLimit,
		MaxInFlightNonStream:                 record.MaxInFlightNonStream,
		MaxInFlightStream:                    record.MaxInFlightStream,
		Name:                                 record.Name,
		AuthType:                             record.AuthType,
		EncryptedEndpointAPIKey:              record.Endpoint.EncryptedAPIKey,
		CustomHeaders:                        record.CustomHeaders,
		PricingTemplateID:                    record.PricingTemplateID,
		ContextWindowTokens:                  record.ContextWindowTokens,
		DefaultOutputTokenReserve:            record.DefaultOutputTokenReserve,
		MaxContextUtilization:                record.MaxContextUtilization,
		PreferredContextUtilizationThreshold: record.PreferredContextUtilizationThreshold,
		OpenAIProbeEndpointVariant:           record.OpenAIProbeEndpointVariant,
		OpenAITextCapability:                 record.OpenAITextCapability,
		Endpoint: runtimeEndpoint{
			ID:      record.Endpoint.ID,
			Name:    record.Endpoint.Name,
			BaseURL: record.Endpoint.BaseURL,
		},
	}
	if record.PricingTemplate != nil {
		item.PricingTemplateSnapshot = &runtimePricingTemplateSnapshot{
			ID:                  record.PricingTemplate.ID,
			PricingUnit:         record.PricingTemplate.PricingUnit,
			PricingCurrencyCode: record.PricingTemplate.PricingCurrencyCode,
			InputPrice:          record.PricingTemplate.InputPrice,
			OutputPrice:         record.PricingTemplate.OutputPrice,
			CachedInputPrice:    record.PricingTemplate.CachedInputPrice,
			CacheCreationPrice:  record.PricingTemplate.CacheCreationPrice,
			ReasoningPrice:      record.PricingTemplate.ReasoningPrice,
			Version:             record.PricingTemplate.Version,
		}
	}
	return item
}
