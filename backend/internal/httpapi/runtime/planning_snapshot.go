package runtime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/coachpo/prism/backend/internal/providerauth"
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
	ID                             int
	ProfileID                      int
	SourceModelConfigID            int
	TargetType                     string
	TargetModelConfigID            *int
	TargetModelID                  string
	TargetModelProfileID           int
	TargetModelAPIFamily           string
	TargetModelEnabled             bool
	TargetConnectionID             *int
	TargetConnectionProfileID      int
	TargetConnectionAPIFamily      string
	ConnectionOpenAITextCapability *string
	Position                       int
	IsEnabled                      bool
	ConnectionEndpointFX           *runtimeEndpointFXSnapshot
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
	return snapshot, nil
}

func compileRuntimeConnection(connection runtimeConnection, apiFamily string, secretEncryptionKey string) (runtimeConnection, error) {
	compiled := connection
	config, err := providerauth.ResolveAuthProfile(connection.AuthType, apiFamily)
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
	profile, found, err := profiledomain.LoadNonDeletedProfile(ctx, tx, profiledomain.DefaultProfileID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: default profile %d not found", ErrPublishedRuntimeSnapshotUnavailable, profiledomain.DefaultProfileID)
	}
	return []int{profile.ID}, nil
}

const runtimeAccessResolverMaxDepth = 32

// runtimePlanningObservation accumulates facts discovered while resolving one
// request so the final static rejection can be classified.
type runtimePlanningObservation struct {
	// CompatibleStaticRouteSeen is set when an enabled access-target row with an
	// active, capability-compatible connection was considered by the strategy
	// but could not form a runtime candidate (dynamic unavailability).
	CompatibleStaticRouteSeen bool
}

type runtimeAccessResolutionContext struct {
	RequestedModelID              string
	RequestedAPIFamily            string
	RequestedOpenAIAcceptedFormat *string
	RequestOperation              RuntimeOperation
	VisitedModelIDs               map[int]struct{}
	ConsideredModelPath           []string
	Depth                         int
	ReferenceNow                  time.Time
	Observation                   *runtimePlanningObservation
}

type runtimeResolvedAccessPlan struct {
	TargetModel              runtimeModelRecord
	SelectedTerminalTargetID *int
	Connections              []runtimeConnection
	TerminalAttempts         []runtimeTerminalAttempt
	RuntimeStates            map[int]loadbalance.RuntimeConnectionState
	Strategy                 loadbalance.RuntimeStrategy
	RouteReason              gatewaycore.RouteReason
	CompatibilityError       error
}

type runtimeResolvedAccessCandidate struct {
	target   runtimeAccessTargetRecord
	resolved runtimeResolvedAccessPlan
}

type runtimeResolvedAccessCandidateEvaluation struct {
	eligibleCandidate  *runtimeResolvedAccessCandidate
	compatibilityError error
}

type runtimeModelPeerSelection struct {
	eligibleCandidates []runtimeResolvedAccessCandidate
	compatibilityError error
}

type noEligibleTargetsError struct {
	requestedModelID string
}

func (err *noEligibleTargetsError) Error() string {
	return noEligibleTargetsErrorDetail(err.requestedModelID)
}

func (s *Service) resolveExecutionTargetFromSnapshot(profileID int, snapshot *planningSnapshot, requestedModel runtimeModelRecord, requestOperation RuntimeOperation, referenceNow time.Time) (runtimeResolvedAccessPlan, error) {
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	return s.resolveExecutionTargetFromRoutingPlanWithOptions(profileID, routingPlan, requestedModel, requestOperation, referenceNow)
}

func (s *Service) resolveExecutionTargetFromRoutingPlanWithOptions(profileID int, routingPlan *runtimeRoutingPlan, requestedModel runtimeModelRecord, requestOperation RuntimeOperation, referenceNow time.Time) (runtimeResolvedAccessPlan, error) {
	if err := validateOpenAIModelAcceptedFormat(requestOperation, requestedModel); err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	ctx := runtimeAccessResolutionContext{
		RequestedModelID:              requestedModel.ModelID,
		RequestedAPIFamily:            requestedModel.APIFamily,
		RequestedOpenAIAcceptedFormat: requestedModel.OpenAIAcceptedFormat,
		RequestOperation:              requestOperation,
		VisitedModelIDs:               map[int]struct{}{},
		ConsideredModelPath:           appendRuntimeModelPath(nil, requestedModel.ModelID),
		ReferenceNow:                  referenceNow,
		Observation:                   &runtimePlanningObservation{},
	}
	resolved, err := s.resolveRequestedModelExecutionTargetFromRoutingPlan(profileID, routingPlan, requestedModel, ctx)
	if err != nil {
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			if !openai.IsTextOperation(providerOperationFromRuntime(requestOperation)) {
				return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: noEligible.Error()}
			}
			return runtimeResolvedAccessPlan{}, s.classifyOpenAIPlanningRejection(profileID, routingPlan, requestedModel, requestOperation, ctx.Observation)
		}
		var planningRejection *domainError
		if errors.As(err, &planningRejection) && planningRejection != nil {
			if _, ok := isOpenAIPlanningRejectionError(planningRejection); ok {
				if !openai.IsTextOperation(providerOperationFromRuntime(requestOperation)) {
					return runtimeResolvedAccessPlan{}, err
				}
				return runtimeResolvedAccessPlan{}, s.classifyOpenAIPlanningRejection(profileID, routingPlan, requestedModel, requestOperation, ctx.Observation)
			}
		}
		return runtimeResolvedAccessPlan{}, err
	}
	return resolved, nil
}

func (s *Service) resolveRequestedModelExecutionTargetFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, requestedModel runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	return s.resolveModelAccessFromRoutingPlan(profileID, routingPlan, requestedModel, ctx)
}

func (s *Service) selectModelPeerCandidateFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, ctx runtimeAccessResolutionContext) (runtimeModelPeerSelection, error) {
	selection := runtimeModelPeerSelection{}
	// Model and Terminal Targets are type-neutral peers. Keep the authored
	// position order intact and apply the strategy once across the mixed list.
	targets := routingPlan.orderedMixedTargetsForStrategy(profileID, model, strategy, s.runtimeState)
	eligibleCandidates, err := s.evaluateModelPeerTargetsFromRoutingPlan(profileID, routingPlan, model, strategy, targets, ctx, &selection)
	if err != nil {
		return runtimeModelPeerSelection{}, err
	}
	selection.eligibleCandidates = append(selection.eligibleCandidates, eligibleCandidates...)
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
		if evaluation.eligibleCandidate != nil {
			eligibleCandidates = append(eligibleCandidates, *evaluation.eligibleCandidate)
		}
	}
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
	if len(peerSelection.eligibleCandidates) > 0 {
		resolved := runtimeResolvedAccessPlan{RuntimeStates: map[int]loadbalance.RuntimeConnectionState{}, Strategy: strategy}
		for _, candidate := range peerSelection.eligibleCandidates {
			appendRuntimeResolvedAccessPlan(&resolved, candidate.resolved)
		}
		if len(resolved.TerminalAttempts) > 0 && len(resolved.Connections) > 0 {
			return resolved, nil
		}
	}

	if peerSelection.compatibilityError != nil {
		return runtimeResolvedAccessPlan{}, peerSelection.compatibilityError
	}
	return runtimeResolvedAccessPlan{}, &noEligibleTargetsError{requestedModelID: ctx.RequestedModelID}
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

	evaluation := runtimeResolvedAccessCandidateEvaluation{}
	if !eligible || len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		if candidate.CompatibilityError != nil {
			evaluation.compatibilityError = candidate.CompatibilityError
		}
		return evaluation, nil
	}

	compatibleCandidate, compatible, err := s.applyIngressOperationCompatibility(candidate, ctx)
	if err != nil {
		if domainErr, ok := isOpenAIPlanningRejectionError(err); ok {
			evaluation.compatibilityError = domainErr
			return evaluation, nil
		}
		return runtimeResolvedAccessCandidateEvaluation{}, err
	}
	if !compatible || len(compatibleCandidate.TerminalAttempts) == 0 || len(compatibleCandidate.Connections) == 0 {
		return evaluation, nil
	}

	if len(compatibleCandidate.TerminalAttempts) == 0 || len(compatibleCandidate.Connections) == 0 {
		return evaluation, nil
	}

	eligibleCandidate := runtimeResolvedAccessCandidate{target: target, resolved: compatibleCandidate}
	evaluation.eligibleCandidate = &eligibleCandidate
	return evaluation, nil
}

func appendRuntimeResolvedAccessPlan(resolved *runtimeResolvedAccessPlan, candidate runtimeResolvedAccessPlan) {
	if resolved == nil {
		return
	}
	if len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		return
	}
	if len(resolved.TerminalAttempts) == 0 {
		resolved.TargetModel = candidate.TargetModel
		resolved.SelectedTerminalTargetID = cloneRuntimeIntPointer(candidate.SelectedTerminalTargetID)
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

func (s *Service) applyIngressOperationCompatibility(candidate runtimeResolvedAccessPlan, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	if len(candidate.TerminalAttempts) == 0 || len(candidate.Connections) == 0 {
		return candidate, false, nil
	}
	if !openai.IsTextOperation(providerOperationFromRuntime(ctx.RequestOperation)) {
		return candidate, true, nil
	}
	compatibleAttempts := make([]runtimeTerminalAttempt, 0, len(candidate.TerminalAttempts))
	for _, attempt := range candidate.TerminalAttempts {
		if !providerauth.IsOpenAI(attempt.TargetModel.APIFamily) {
			continue
		}
		mode, supported := resolveTranslationMode(ctx.RequestOperation, ctx.RequestedOpenAIAcceptedFormat, attempt.Connection.OpenAITextCapability)
		if !supported {
			continue
		}
		plannedAttempt := attempt
		plannedAttempt.TranslationMode = mode
		compatibleAttempts = append(compatibleAttempts, plannedAttempt)
	}
	if len(compatibleAttempts) > 0 {
		return candidateWithCompatibleOpenAITextAttempts(candidate, compatibleAttempts), true, nil
	}
	return runtimeResolvedAccessPlan{}, false, openAINoCompatibleTerminalTargetDomainError()
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
	if ctx.Observation != nil && connection.OpenAITextCapability != nil && openai.IsTextOperation(providerOperationFromRuntime(ctx.RequestOperation)) && providerauth.OpenAITextModesMatch(ctx.RequestedOpenAIAcceptedFormat, connection.OpenAITextCapability) && providerauth.OpenAITextCapabilitySupportsNativeOperation(*connection.OpenAITextCapability, ctx.RequestOperation.Name) {
		ctx.Observation.CompatibleStaticRouteSeen = true
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
		RuntimeStates: eligibleRuntimeStates,
		Strategy:      strategy,
		RouteReason:   routeReason,
	}, true, nil
}

func runtimeTerminalTargetIsUpstreamRedirect(routingPlan *runtimeRoutingPlan, sourceModel runtimeModelRecord, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) bool {
	if target.TargetType != runtimeAccessTargetTypeConnection || strings.TrimSpace(sourceModel.ModelID) != strings.TrimSpace(ctx.RequestedModelID) {
		return false
	}
	compiled, ok := routingPlan.ModelsByConfigID[sourceModel.ID]
	if !ok {
		return false
	}
	for _, accessTarget := range compiled.OrderedEnabledTargets {
		if accessTarget.TargetType == runtimeAccessTargetTypeModel {
			return true
		}
	}
	return false
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
		if domainErr, ok := isOpenAIPlanningRejectionError(err); ok {
			return runtimeResolvedAccessPlan{CompatibilityError: domainErr}, false, nil
		}
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			return runtimeResolvedAccessPlan{}, false, nil
		}
		return runtimeResolvedAccessPlan{}, false, err
	}
	resolved.RouteReason = mergeRuntimeRouteReason(gatewaycore.RouteReasonModelRedirect, resolved.RouteReason)
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
			model_configs.loadbalance_strategy_id, model_configs.openai_accepted_format,
			model_configs.created_at,
			COALESCE(audit_settings.audit_enabled, FALSE),
			COALESCE(audit_settings.audit_enabled, FALSE) AND COALESCE(audit_settings.audit_capture_bodies, FALSE)
		FROM model_configs
		LEFT JOIN profile_api_family_audit_settings AS audit_settings ON audit_settings.profile_id = model_configs.profile_id
			AND audit_settings.api_family = model_configs.api_family
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
		var openAIAcceptedFormat sql.NullString
		item := runtimeModelRecord{}
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.APIFamily, &item.ModelID, &strategyID, &openAIAcceptedFormat, &item.CreatedAt, &item.AuditEnabled, &item.AuditCaptureBodies); err != nil {
			return nil, fmt.Errorf("scan enabled model for profile %d: %w", profileID, err)
		}
		if _, exists := items[item.ModelID]; exists {
			continue
		}
		if strategyID.Valid {
			resolved := int(strategyID.Int32)
			item.LoadbalanceStrategyID = &resolved
		}
		item.OpenAIAcceptedFormat = nullableString(openAIAcceptedFormat)
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
			connections.openai_text_capability,
			model_access_targets.position, model_access_targets.is_enabled,
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
		var connectionOpenAITextCapability sql.NullString
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
			&connectionOpenAITextCapability,
			&item.Position,
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
		item.ConnectionOpenAITextCapability = nullableString(connectionOpenAITextCapability)
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
			connections.name, connections.auth_type, connections.custom_headers, connections.custom_request_parameters, connections.pricing_template_id,
			connections.openai_text_capability,
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
	var customRequestParameters sql.NullString
	var pricingTemplateID sql.NullInt32
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
		&customRequestParameters,
		&pricingTemplateID,
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
	parsedCustomRequestParameters, parseErr := parseRuntimeCustomRequestParameters(customRequestParameters)
	if parseErr != nil {
		return terminaltarget.RuntimeRecord{}, fmt.Errorf("invalid custom request parameters for connection %d: %w", record.ID, parseErr)
	}
	record.CustomRequestParameters = parsedCustomRequestParameters
	record.PricingTemplateID = nullableInt32(pricingTemplateID)
	record.OpenAITextCapability = nullableString(openAITextCapability)
	record.Endpoint.Name = nullableString(endpointName)
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

// parseRuntimeCustomRequestParameters validates persisted JSONB before it
// enters the immutable runtime snapshot. Invalid data fails closed instead of
// silently dropping an operator-configured overlay.
func parseRuntimeCustomRequestParameters(value sql.NullString) (*terminaltarget.CustomRequestParameters, error) {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil, nil
	}
	parsed, validationErr := terminaltarget.ParseCustomRequestParametersJSON([]byte(value.String))
	if validationErr != nil {
		return nil, validationErr
	}
	if parsed.IsEmpty() {
		return nil, nil
	}
	return parsed, nil
}

func runtimeConnectionFromTerminalTargetRecord(record terminaltarget.RuntimeRecord) runtimeConnection {
	item := runtimeConnection{
		ID:                      record.ID,
		ProfileID:               record.ProfileID,
		APIFamily:               record.APIFamily,
		EndpointID:              record.EndpointID,
		Priority:                record.Priority,
		QPSLimit:                record.QPSLimit,
		MaxInFlightNonStream:    record.MaxInFlightNonStream,
		MaxInFlightStream:       record.MaxInFlightStream,
		Name:                    record.Name,
		AuthType:                record.AuthType,
		EncryptedEndpointAPIKey: record.Endpoint.EncryptedAPIKey,
		CustomHeaders:           record.CustomHeaders,
		CustomRequestParameters: record.CustomRequestParameters,
		PricingTemplateID:       record.PricingTemplateID,
		OpenAITextCapability:    record.OpenAITextCapability,
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
