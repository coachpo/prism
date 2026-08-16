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
	ID                              int
	ProfileID                       int
	SourceModelConfigID             int
	TargetType                      string
	TargetModelConfigID             *int
	TargetModelID                   string
	TargetModelProfileID            int
	TargetModelAPIFamily            string
	TargetModelEnabled              bool
	TargetConnectionID              *int
	TargetConnectionProfileID       int
	TargetConnectionAPIFamily       string
	ConnectionOpenAITextCapability  *string
	ConnectionOpenAIImageCapability *string
	Position                        int
	IsEnabled                       bool
	ConnectionEndpointFX            *runtimeEndpointFXSnapshot
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
// request so a failed static route can be distinguished from a dynamic
// unavailability (for example a ban or admission limit) and a routing-window
// exclusion can be attributed to the schedule.
type runtimePlanningObservation struct {
	CompatibleStaticRouteSeen         bool
	EvaluatedTerminalConnectionIDs    map[int]struct{}
	ScheduleExcludedConnectionIDs     map[int]struct{}
	ScheduleUnresolvableConnectionIDs map[int]struct{}
	OtherExclusionSeen                bool
}

type runtimeAccessResolutionContext struct {
	RequestedModelID               string
	RequestedAPIFamily             string
	RequestedOpenAIAcceptedFormat  *string
	RequestedOpenAIImageOperations *string
	RequestOperation               RuntimeOperation
	VisitedModelIDs                map[int]struct{}
	ConsideredModelPath            []string
	Depth                          int
	ReferenceNow                   time.Time
	Observation                    *runtimePlanningObservation
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

type runtimeMixedPeerSelection struct {
	eligibleCandidates []runtimeResolvedAccessCandidate
	compatibilityError error
}

type noEligibleTargetsError struct {
	requestedModelID string
}

func (err *noEligibleTargetsError) Error() string {
	return fmt.Sprintf("No eligible targets available for model '%s'.", err.requestedModelID)
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
		RequestedModelID:               requestedModel.ModelID,
		RequestedAPIFamily:             requestedModel.APIFamily,
		RequestedOpenAIAcceptedFormat:  requestedModel.OpenAIAcceptedFormat,
		RequestedOpenAIImageOperations: requestedModel.OpenAIImageOperations,
		RequestOperation:               requestOperation,
		VisitedModelIDs:                map[int]struct{}{},
		ConsideredModelPath:            appendRuntimeModelPath(nil, requestedModel.ModelID),
		ReferenceNow:                   referenceNow,
		Observation:                    &runtimePlanningObservation{},
	}
	resolved, err := s.resolveRequestedModelExecutionTargetFromRoutingPlan(profileID, routingPlan, requestedModel, ctx)
	if err != nil {
		// Routing-window attribution runs before classification: it consumes
		// the observation accumulated across every exit (including the E3
		// fallthrough for depth/cycle/missing strategy/compiled model), and it
		// must not be hooked onto the noEligible / planning-rejection branches
		// below, where the E3 fallthrough would be missed and the
		// compatibilityError return would bypass it. E0
		// (validateOpenAIModelAcceptedFormat) returns before the observation
		// exists and is deliberately not attributed.
		if scheduleErr := scheduleRejectionError(routingPlan, requestedModel.ModelID, ctx.Observation, referenceNow); scheduleErr != nil {
			return runtimeResolvedAccessPlan{}, scheduleErr
		}
		classified := s.classifyPlanningRejection(profileID, routingPlan, requestedModel, requestOperation, ctx, err)
		if domainErr, ok := classified.(*domainError); ok {
			return runtimeResolvedAccessPlan{}, annotateSchedulePartialExclusion(domainErr, ctx.Observation)
		}
		return runtimeResolvedAccessPlan{}, classified
	}
	return resolved, nil
}

// classifyPlanningRejection holds the classification body of the entry error
// handling: noEligible and OpenAI-planning-rejection errors map to the stable
// OpenAI codes, everything else is a hard error. All exits return either a
// bare *domainError or a non-domainError, so the caller's direct type
// assertion (never errors.As, which would unwrap and drop outer wrappers)
// is safe.
func (s *Service) classifyPlanningRejection(profileID int, routingPlan *runtimeRoutingPlan, requestedModel runtimeModelRecord, requestOperation RuntimeOperation, ctx runtimeAccessResolutionContext, err error) error {
	var noEligible *noEligibleTargetsError
	if errors.As(err, &noEligible) {
		if !openai.IsTextOperation(providerOperationFromRuntime(requestOperation)) {
			return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: noEligible.Error()}
		}
		return s.classifyOpenAIPlanningRejection(profileID, routingPlan, requestedModel, requestOperation, ctx.Observation)
	}
	if _, ok := isOpenAIPlanningRejectionError(err); ok {
		if !openai.IsTextOperation(providerOperationFromRuntime(requestOperation)) {
			return err
		}
		return s.classifyOpenAIPlanningRejection(profileID, routingPlan, requestedModel, requestOperation, ctx.Observation)
	}
	return err
}

func (s *Service) resolveRequestedModelExecutionTargetFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, requestedModel runtimeModelRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, error) {
	return s.resolveModelAccessFromRoutingPlan(profileID, routingPlan, requestedModel, ctx)
}

func (s *Service) evaluateMixedPeerTargetsFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, targets []runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext, selection *runtimeMixedPeerSelection) ([]runtimeResolvedAccessCandidate, error) {
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
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access graph exceeded maximum depth of %d while resolving model '%s'.", runtimeAccessResolverMaxDepth, ctx.RequestedModelID)}
	}
	if _, seen := ctx.VisitedModelIDs[model.ID]; seen {
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model access cycle detected while resolving model '%s'.", ctx.RequestedModelID)}
	}
	strategy, ok := routingPlan.strategyForModel(model)
	if !ok {
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, fmt.Errorf("model %q is missing loadbalance_strategy", model.ModelID)
	}
	visited := cloneVisitedModelIDs(ctx.VisitedModelIDs)
	visited[model.ID] = struct{}{}
	childContext := ctx
	childContext.VisitedModelIDs = visited
	childContext.Depth++

	compiled, ok := routingPlan.ModelsByConfigID[model.ID]
	if !ok {
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, fmt.Errorf("model %q is missing from the compiled routing plan", model.ModelID)
	}
	effectivePeers := orderRuntimeAccessTargets(profileID, model.ID, strategy, compiled.OrderedEnabledTargets, s.runtimeState)

	selection := runtimeMixedPeerSelection{}
	eligibleCandidates, err := s.evaluateMixedPeerTargetsFromRoutingPlan(profileID, routingPlan, model, strategy, effectivePeers, childContext, &selection)
	if err != nil {
		return runtimeResolvedAccessPlan{}, err
	}
	if len(eligibleCandidates) > 0 {
		resolved := runtimeResolvedAccessPlan{RuntimeStates: map[int]loadbalance.RuntimeConnectionState{}, Strategy: strategy}
		for _, candidate := range eligibleCandidates {
			appendRuntimeResolvedAccessPlan(&resolved, candidate.resolved)
		}
		if len(resolved.TerminalAttempts) > 0 && len(resolved.Connections) > 0 {
			return resolved, nil
		}
	}
	if selection.compatibilityError != nil {
		return runtimeResolvedAccessPlan{}, selection.compatibilityError
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
		if domainErr, ok := isRequestTranslationUnsupportedError(err); ok {
			ctx.Observation.recordOtherExclusion()
			evaluation.compatibilityError = domainErr
			return evaluation, nil
		}
		return runtimeResolvedAccessCandidateEvaluation{}, err
	}
	if !compatible || len(compatibleCandidate.TerminalAttempts) == 0 || len(compatibleCandidate.Connections) == 0 {
		ctx.Observation.recordOtherExclusion()
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
	if !runtimeOperationIsOpenAICapabilityGated(ctx.RequestOperation) {
		return candidate, true, nil
	}
	compatibleAttempts := make([]runtimeTerminalAttempt, 0, len(candidate.TerminalAttempts))
	for _, attempt := range candidate.TerminalAttempts {
		if !providerauth.IsOpenAI(attempt.TargetModel.APIFamily) {
			continue
		}
		mode, supported := resolveTranslationMode(ctx.RequestOperation, runtimeOpenAICapabilityDimensions{
			TextMode:        ctx.RequestedOpenAIAcceptedFormat,
			ImageOperations: ctx.RequestedOpenAIImageOperations,
		}, runtimeConnectionCapabilityDimensions(attempt.Connection))
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
	return runtimeResolvedAccessPlan{}, false, openAIRequestTranslationUnsupportedDomainError()
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
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, false, nil
	}
	switch target.TargetType {
	case runtimeAccessTargetTypeConnection:
		return s.resolveTerminalTargetFromRoutingPlan(profileID, routingPlan, sourceModel, strategy, target, ctx)
	case runtimeAccessTargetTypeModel:
		return s.resolveModelAccessTargetFromRoutingPlan(profileID, routingPlan, sourceModel, target, ctx)
	default:
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, false, nil
	}
}

func (s *Service) resolveTerminalTargetFromRoutingPlan(profileID int, routingPlan *runtimeRoutingPlan, sourceModel runtimeModelRecord, strategy loadbalance.RuntimeStrategy, target runtimeAccessTargetRecord, ctx runtimeAccessResolutionContext) (runtimeResolvedAccessPlan, bool, error) {
	connection, ok := routingPlan.terminalConnectionForAccessTarget(sourceModel, target)
	if !ok {
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, false, nil
	}
	ctx.Observation.recordEvaluatedTerminalConnection(connection.ID)
	if ctx.Observation != nil && runtimeOperationIsOpenAICapabilityGated(ctx.RequestOperation) &&
		runtimeOpenAICapabilitySatisfied(ctx.RequestOperation, runtimeModelCapabilityDimensions(sourceModel), runtimeConnectionCapabilityDimensions(connection)) {
		ctx.Observation.recordCompatibleStaticRoute()
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
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, false, nil
	}
	// Routing-window gate, deliberately after the Ban early exit above: placed
	// before it, next_open_at would promise an instant that is still dark
	// under an until_reset ban. DecideAt is the only eligibility entry point
	// for CompiledRoutingSchedule — never IsOpenAt directly, which is false
	// for every unconfigured connection and would fail all existing rows
	// closed. Unrestricted / Open fall through to the existing code unchanged.
	switch resolvedConnection.RoutingSchedule.DecideAt(ctx.ReferenceNow) {
	case terminaltarget.RoutingScheduleUnresolved:
		ctx.Observation.recordScheduleUnresolvable(resolvedConnection.ID)
		return runtimeResolvedAccessPlan{}, false, nil
	case terminaltarget.RoutingScheduleClosed:
		ctx.Observation.recordScheduleExclusion(resolvedConnection.ID,
			terminalTargetScheduleAttributable(sourceModel, connection, ctx))
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
		ctx.Observation.recordOtherExclusion()
		return runtimeResolvedAccessPlan{}, false, nil
	}
	childObservation := &runtimePlanningObservation{}
	childContext := ctx
	childContext.ConsideredModelPath = appendRuntimeModelPath(ctx.ConsideredModelPath, childModel.ModelID)
	childContext.Observation = childObservation
	resolved, err := s.resolveModelAccessFromRoutingPlan(profileID, routingPlan, childModel, childContext)
	// The child writes its own observation; CompatibleStaticRouteSeen must
	// float up unconditionally, before every error branch: losing it would
	// reclassify nested bare-503 failures as openai_no_eligible_terminal_target
	// (a behavior regression, not a missing feature).
	ctx.Observation.absorbChildStaticRoute(childObservation)
	if err != nil {
		if domainErr, ok := isRequestTranslationUnsupportedError(err); ok {
			ctx.Observation.recordOtherExclusion()
			return runtimeResolvedAccessPlan{CompatibilityError: domainErr}, false, nil
		}
		var noEligible *noEligibleTargetsError
		if errors.As(err, &noEligible) {
			ctx.Observation.mergeSwallowedChildObservation(childObservation)
			return runtimeResolvedAccessPlan{}, false, nil
		}
		ctx.Observation.recordOtherExclusion()
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
			model_configs.openai_image_operations,
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
		var openAIImageOperations sql.NullString
		item := runtimeModelRecord{}
		if err := rows.Scan(&item.ID, &item.ProfileID, &item.APIFamily, &item.ModelID, &strategyID, &openAIAcceptedFormat, &openAIImageOperations, &item.CreatedAt, &item.AuditEnabled, &item.AuditCaptureBodies); err != nil {
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
		item.OpenAIImageOperations = nullableString(openAIImageOperations)
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
			connections.openai_text_capability, connections.openai_image_capability,
			model_access_targets.position, model_access_targets.is_enabled
		FROM model_access_targets
		JOIN model_configs AS source_models ON source_models.id = model_access_targets.source_model_config_id
		LEFT JOIN model_configs AS target_models ON target_models.id = model_access_targets.target_model_config_id
		LEFT JOIN connections ON connections.id = model_access_targets.target_connection_id
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
		var connectionOpenAIImageCapability sql.NullString
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
			&connectionOpenAIImageCapability,
			&item.Position,
			&item.IsEnabled,
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
		item.ConnectionOpenAIImageCapability = nullableString(connectionOpenAIImageCapability)
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
