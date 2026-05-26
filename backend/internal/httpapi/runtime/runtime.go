package runtime

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

var apiFamilyAuthConfigs = map[string]apiFamilyAuthConfig{
	"openai": {
		AuthHeader:   "Authorization",
		AuthPrefix:   "Bearer ",
		ExtraHeaders: map[string]string{},
	},
	"anthropic": {
		AuthHeader: "x-api-key",
		AuthPrefix: "",
		ExtraHeaders: map[string]string{
			"anthropic-version": "2023-06-01",
		},
	},
	"gemini": {
		AuthHeader:   "Authorization",
		AuthPrefix:   "Bearer ",
		ExtraHeaders: map[string]string{},
	},
}

type runtimeFeedbackStore struct {
	pool *pgxpool.Pool
}

func newRuntimeFeedbackStore(pool *pgxpool.Pool) *runtimeFeedbackStore {
	return &runtimeFeedbackStore{pool: pool}
}

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailers":            {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"host":                {},
}

var clientAuthHeaders = map[string]struct{}{
	"authorization":  {},
	"x-api-key":      {},
	"x-goog-api-key": {},
}

type apiFamilyAuthConfig struct {
	AuthHeader   string
	AuthPrefix   string
	ExtraHeaders map[string]string
}

type runtimeModelRecord struct {
	ID                     int
	ProfileID              int
	APIFamily              string
	ModelID                string
	ModelType              string
	ProxySelectionStrategy string
	VendorID               *int
	VendorKey              *string
	VendorName             *string
	AuditEnabled           bool
	AuditCaptureBodies     bool
	LoadbalanceStrategyID  *int
}

type runtimeEndpoint struct {
	ID      int
	Name    *string
	BaseURL string
}

type runtimePricingTemplateSnapshot struct {
	ID                  int
	PricingUnit         string
	PricingCurrencyCode string
	InputPrice          string
	OutputPrice         string
	CachedInputPrice    string
	CacheCreationPrice  string
	ReasoningPrice      string
	Version             int
}

type runtimeEndpointFXSnapshot struct {
	ModelID    string
	EndpointID int
	FXRate     string
}

type runtimeReportCurrencySnapshot struct {
	Code   string
	Symbol string
}

type runtimeConnectionUpstreamAuthSnapshot struct {
	AuthHeader            string
	AuthValue             string
	ExtraHeaders          map[string]string
	ControlledHeaderNames map[string]struct{}
}

type runtimeConnection struct {
	ID                      int
	ProfileID               int
	ModelConfigID           int
	EndpointID              int
	Priority                int
	QPSLimit                *int
	MaxInFlightNonStream    *int
	MaxInFlightStream       *int
	Name                    *string
	AuthType                *string
	EncryptedEndpointAPIKey string
	CustomHeaders           map[string]any
	PricingTemplateID       *int
	PricingTemplateSnapshot *runtimePricingTemplateSnapshot
	EndpointFXSnapshot      *runtimeEndpointFXSnapshot
	UpstreamAuth            *runtimeConnectionUpstreamAuthSnapshot
	Endpoint                runtimeEndpoint
}

type headerBlocklistRule struct {
	MatchType string
	Pattern   string
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
	return len(plan.Connections) > 1
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
}

type resolvedRequestOperation struct {
	Match            RuntimeOperationMatch
	ContentType      string
	RequestedModelID string
}

type resolvedExecutionTarget struct {
	RequestedModel runtimeModelRecord
	TargetModel    runtimeModelRecord
	Connections    []runtimeConnection
	RuntimeStates  map[int]loadbalance.RuntimeConnectionState
	Strategy       loadbalance.RuntimeStrategy
}

type plannedUpstreamRequest struct {
	EffectiveRequestPath    string
	RawRequestBody          []byte
	UpstreamBody            []byte
	IsStreamingRequest      bool
	ClientHeaders           map[string]string
	RequestGenerationParams requestGenerationParamsSnapshot
}

type runtimeRequestBodySource struct {
	bufferedBody         []byte
	streamingBody        io.ReadCloser
	streamingContentSize int64
	useStreamingBody     bool
	generationObserver   *geminiGenerationParamsStreamingObserver

	mu       sync.Mutex
	consumed bool
}

func newBufferedRuntimeRequestBodySource(body []byte) *runtimeRequestBodySource {
	return &runtimeRequestBodySource{bufferedBody: body}
}

func newStreamingRuntimeRequestBodySource(body io.ReadCloser, contentLength int64) *runtimeRequestBodySource {
	return &runtimeRequestBodySource{
		streamingBody:        body,
		streamingContentSize: contentLength,
		useStreamingBody:     true,
	}
}

func (source *runtimeRequestBodySource) withGenerationParamsObserver(observer *geminiGenerationParamsStreamingObserver) *runtimeRequestBodySource {
	if source != nil {
		source.generationObserver = observer
	}
	return source
}

func (source *runtimeRequestBodySource) Open() (io.ReadCloser, int64, error) {
	if source == nil {
		return http.NoBody, 0, nil
	}
	if !source.useStreamingBody {
		return io.NopCloser(bytes.NewReader(source.bufferedBody)), int64(len(source.bufferedBody)), nil
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.consumed {
		return nil, 0, fmt.Errorf("runtime request body already consumed")
	}
	source.consumed = true
	if source.streamingBody == nil {
		return http.NoBody, 0, nil
	}
	if source.generationObserver != nil {
		return &requestGenerationParamsObservingReadCloser{source: source.streamingBody, observer: source.generationObserver}, source.streamingContentSize, nil
	}
	return source.streamingBody, source.streamingContentSize, nil
}

type executionAttempt struct {
	Connection      runtimeConnection
	RequestURL      string
	RequestHeaders  map[string]string
	ResponseHeaders http.Header
	StatusCode      int
	ResponseTimeMS  int
	CompletedAt     time.Time
}

type executionResult struct {
	Response       *http.Response
	Connection     runtimeConnection
	RequestHeaders map[string]string
	AttemptCount   int
	Attempts       []executionAttempt
}

type executionOutcome struct {
	Connection                runtimeConnection
	RequestHeaders            map[string]string
	Response                  *http.Response
	Attempt                   executionAttempt
	Launched                  bool
	Skipped                   bool
	Err                       error
	AdmissionReason           string
	ProbeEligibleRecord       *loadbalance.RuntimeConnectionState
	FailoverEligible          bool
	Definitive                bool
	SuppressTransportFeedback bool
	FatalError                error
}

type hedgedExecutionResult struct {
	Winner              *executionOutcome
	Attempts            []executionAttempt
	LaunchedAttempts    int
	AdmissionRejections int
	LastAdmissionReason string
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
	launchedAttempts    int
	attempts            []executionAttempt
	lastError           string
	lastAdmissionReason string
	admissionRejections int
	hedgeUsed           bool
}

var errHedgeLoserCanceled = errors.New("hedge loser canceled")

const hedgeCanceledAttemptStatusCode = 499

func (s *Service) buildRequestPlan(ctx context.Context, request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch) (requestPlan, error) {
	operationMatch, err := validateResolvedRuntimeOperation(operationMatch, request.Method, request.URL.Path)
	if err != nil {
		return requestPlan{}, err
	}
	if s.cache == nil {
		return requestPlan{}, runtimeSnapshotDomainError(ErrPublishedRuntimeSnapshotUnavailable)
	}
	activeProfile, snapshot, err := s.cache.LoadFreshActiveRuntimePlan(ctx)
	if err != nil {
		return requestPlan{}, runtimeSnapshotDomainError(err)
	}
	return s.buildRequestPlanFromSnapshot(request, rawBody, runtimeConfig, operationMatch, activeProfile.ID, snapshot)
}

func (s *Service) buildRequestPlanFromSnapshot(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	input := requestPlanningInput{
		Request:         request,
		RawBody:         rawBody,
		RuntimeConfig:   runtimeConfig,
		OperationMatch:  operationMatch,
		ActiveProfileID: activeProfileID,
		Snapshot:        snapshot,
	}
	operation, err := resolveRequestOperation(input)
	if err != nil {
		return requestPlan{}, err
	}
	target, err := s.resolveRequestPlanTarget(input, operation)
	if err != nil {
		return requestPlan{}, err
	}
	upstreamRequest, err := buildPlannedUpstreamRequest(input, operation, target.TargetModel)
	if err != nil {
		return requestPlan{}, err
	}
	return assembleRequestPlan(input, operation, target, upstreamRequest), nil
}

func resolveRequestOperation(input requestPlanningInput) (resolvedRequestOperation, error) {
	operationMatch, err := validateResolvedRuntimeOperation(input.OperationMatch, input.Request.Method, input.Request.URL.Path)
	if err != nil {
		return resolvedRequestOperation{}, err
	}
	requestContentType := input.Request.Header.Get("Content-Type")
	requestedModelID, err := resolveModelIDForOperation(input.RawBody, requestContentType, operationMatch)
	if err != nil {
		return resolvedRequestOperation{}, err
	}
	return resolvedRequestOperation{Match: operationMatch, ContentType: requestContentType, RequestedModelID: requestedModelID}, nil
}

func (s *Service) resolveRequestPlanTarget(input requestPlanningInput, operation resolvedRequestOperation) (resolvedExecutionTarget, error) {
	requestedModel, found := input.Snapshot.ModelsByID[operation.RequestedModelID]
	if !found {
		return resolvedExecutionTarget{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", operation.RequestedModelID)}
	}

	targetModel, connections, runtimeStates, strategy, err := s.resolveExecutionTargetFromSnapshot(input.ActiveProfileID, input.Snapshot, requestedModel, s.nowUTC())
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, targetModel); err != nil {
		return resolvedExecutionTarget{}, err
	}

	orderedConnectionIDs, err := loadbalance.OrderConnectionIDs(input.ActiveProfileID, targetModel.ID, strategy, toConnectionOrderCandidates(connections), runtimeStates, s.runtimeState, s.nowUTC())
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	orderedConnections := orderConnectionsByID(connections, orderedConnectionIDs)
	if len(orderedConnections) == 0 {
		return resolvedExecutionTarget{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", operation.RequestedModelID)}
	}

	return resolvedExecutionTarget{
		RequestedModel: requestedModel,
		TargetModel:    targetModel,
		Connections:    orderedConnections,
		RuntimeStates:  runtimeStates,
		Strategy:       strategy,
	}, nil
}

func buildPlannedUpstreamRequest(input requestPlanningInput, operation resolvedRequestOperation, targetModel runtimeModelRecord) (plannedUpstreamRequest, error) {
	effectiveRequestPath := input.Request.URL.Path
	upstreamBody := input.RawBody
	switch operation.Match.Operation.ModelBindingSource {
	case RuntimeOperationModelBindingPath:
		pathModelID := strings.TrimSpace(operation.Match.PathParams["model"])
		if pathModelID != "" && pathModelID != targetModel.ModelID {
			effectiveRequestPath = rewriteModelInPath(input.Request.URL.Path, pathModelID, targetModel.ModelID)
		}
	case RuntimeOperationModelBindingBody:
		if bodyModelID := extractModelFromBodyForOperation(input.RawBody, operation.ContentType, operation.Match.Operation); bodyModelID != "" && bodyModelID != targetModel.ModelID {
			upstreamBody = rewriteModelInBodyForOperation(input.RawBody, operation.ContentType, operation.Match.Operation, targetModel.ModelID)
		}
	default:
		return plannedUpstreamRequest{}, unsupportedOperationModelBindingError(operation.Match.Operation)
	}

	return plannedUpstreamRequest{
		EffectiveRequestPath:    effectiveRequestPath,
		RawRequestBody:          input.RawBody,
		UpstreamBody:            upstreamBody,
		IsStreamingRequest:      requestWantsStreamForOperation(operation.Match.Operation, input.RawBody, effectiveRequestPath),
		ClientHeaders:           flattenHeaders(input.Request.Header),
		RequestGenerationParams: extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody),
	}, nil
}

func assembleRequestPlan(input requestPlanningInput, operation resolvedRequestOperation, target resolvedExecutionTarget, upstreamRequest plannedUpstreamRequest) requestPlan {
	return requestPlan{
		RequestedModelID:            operation.RequestedModelID,
		ResolvedTargetModelID:       stringPointerIfNotEmpty(target.TargetModel.ModelID),
		ResolvedPricingModelID:      strings.TrimSpace(target.TargetModel.ModelID),
		RequestedVendorID:           target.RequestedModel.VendorID,
		RequestedVendorKey:          target.RequestedModel.VendorKey,
		RequestedVendorName:         target.RequestedModel.VendorName,
		ProfileID:                   input.ActiveProfileID,
		APIFamily:                   target.TargetModel.APIFamily,
		RuntimeOperation:            operation.Match.Operation,
		RuntimeOperationPathParams:  cloneStringMap(operation.Match.PathParams),
		AuditEnabledAtRequest:       target.TargetModel.AuditEnabled,
		AuditCaptureBodiesAtRequest: target.TargetModel.AuditEnabled && target.TargetModel.AuditCaptureBodies,
		ReportCurrencySnapshot:      input.Snapshot.ReportCurrency,
		EffectiveRequestPath:        upstreamRequest.EffectiveRequestPath,
		RawRequestBody:              upstreamRequest.RawRequestBody,
		UpstreamBody:                upstreamRequest.UpstreamBody,
		IsStreamingRequest:          upstreamRequest.IsStreamingRequest,
		Connections:                 target.Connections,
		RuntimeStates:               target.RuntimeStates,
		BlocklistRules:              input.Snapshot.BlocklistRules,
		ClientHeaders:               upstreamRequest.ClientHeaders,
		FailoverStatusCodes:         target.Strategy.FailoverStatusCodes(),
		Strategy:                    target.Strategy,
		RequestGenerationParams:     upstreamRequest.RequestGenerationParams,
		HTTPClient:                  input.RuntimeConfig.HTTPClient,
	}
}

func runtimeSnapshotDomainError(err error) error {
	if errors.Is(err, ErrPublishedRuntimeSnapshotUnavailable) {
		return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "Runtime snapshot is unavailable. Retry later."}
	}
	if errors.Is(err, ErrRuntimeSnapshotRefreshRequired) {
		return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: "Runtime snapshot refresh is required. Retry later."}
	}
	return err
}

func loadRuntimeReportCurrencySnapshot(ctx context.Context, tx pgx.Tx, profileID int) (runtimeReportCurrencySnapshot, error) {
	var code string
	var symbol string
	err := tx.QueryRow(ctx, `SELECT report_currency_code, report_currency_symbol FROM user_settings WHERE profile_id = $1 ORDER BY id ASC LIMIT 1`, profileID).Scan(&code, &symbol)
	if err == nil {
		return runtimeReportCurrencySnapshot{Code: strings.TrimSpace(code), Symbol: strings.TrimSpace(symbol)}, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, nil
	}
	return runtimeReportCurrencySnapshot{}, fmt.Errorf("load runtime report currency for profile %d: %w", profileID, err)
}

func listEnabledHeaderBlocklistRules(ctx context.Context, tx pgx.Tx, profileID int) ([]headerBlocklistRule, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT match_type, pattern
		FROM header_blocklist_rules
		WHERE enabled = TRUE AND (is_system = TRUE OR profile_id = $1)
		ORDER BY is_system DESC, id ASC`,
		profileID,
	)
	if err != nil {
		return nil, fmt.Errorf("query header blocklist rules for profile %d: %w", profileID, err)
	}
	defer rows.Close()

	items := make([]headerBlocklistRule, 0)
	for rows.Next() {
		var item headerBlocklistRule
		if err := rows.Scan(&item.MatchType, &item.Pattern); err != nil {
			return nil, fmt.Errorf("scan header blocklist rule: %w", err)
		}
		item.MatchType = strings.ToLower(strings.TrimSpace(item.MatchType))
		item.Pattern = strings.ToLower(strings.TrimSpace(item.Pattern))
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate header blocklist rules for profile %d: %w", profileID, err)
	}
	return items, nil
}

func toConnectionOrderCandidates(connections []runtimeConnection) []loadbalance.ConnectionOrderCandidate {
	candidates := make([]loadbalance.ConnectionOrderCandidate, 0, len(connections))
	for _, connection := range connections {
		candidates = append(candidates, loadbalance.ConnectionOrderCandidate{ID: connection.ID, Priority: connection.Priority})
	}
	return candidates
}

func runtimeConnectionRefs(connections []runtimeConnection) []loadbalance.RuntimeConnectionRef {
	refs := make([]loadbalance.RuntimeConnectionRef, 0, len(connections))
	for _, connection := range connections {
		refs = append(refs, loadbalance.RuntimeConnectionRef{ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID})
	}
	return refs
}

func orderConnectionsByID(connections []runtimeConnection, orderedIDs []int) []runtimeConnection {
	if len(orderedIDs) == 0 {
		return nil
	}
	connectionsByID := make(map[int]runtimeConnection, len(connections))
	for _, connection := range connections {
		connectionsByID[connection.ID] = connection
	}
	ordered := make([]runtimeConnection, 0, len(orderedIDs))
	for _, connectionID := range orderedIDs {
		connection, ok := connectionsByID[connectionID]
		if !ok {
			continue
		}
		ordered = append(ordered, connection)
	}
	return ordered
}

func (s *Service) executeRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, bodySource *runtimeRequestBodySource) (executionResult, error) {
	state := newRequestExecutionState(plan)
	limits := requestExecutionLimitsForPlan(plan)

	for index := 0; index < len(plan.Connections); index++ {
		if limits.remainingLaunchCapacity(state) <= 0 {
			break
		}
		if limits.shouldHedge(plan, state, index) {
			hedged, err := s.executeHedgedRequest(ctx, method, plan, requestQuery, index, limits.HedgePolicy, bodySource)
			if err != nil {
				return executionResult{}, err
			}
			state.recordHedgedResult(hedged)
			if hedged.Winner != nil {
				return s.executionResultForHedgedWinner(plan, state, hedged.Winner), nil
			}
			index += hedged.ConsumedConnections - 1
			continue
		}

		outcome := s.executeSingleAttempt(ctx, method, plan, requestQuery, plan.Connections[index], bodySource)
		result, done, err := s.handleSingleExecutionOutcome(plan, &state, outcome, index, limits.MaxAttempts)
		if err != nil {
			return executionResult{}, err
		}
		if done {
			return result, nil
		}
	}
	return state.failureResult(plan)
}

func newRequestExecutionState(plan requestPlan) requestExecutionState {
	return requestExecutionState{attempts: make([]executionAttempt, 0, len(plan.Connections))}
}

func requestExecutionLimitsForPlan(plan requestPlan) requestExecutionLimits {
	hedgePolicy := plan.Strategy.HedgePolicy()
	maxAttempts := len(plan.Connections)
	if strings.EqualFold(strings.TrimSpace(plan.Strategy.StrategyType), "adaptive") {
		maxAttempts = max(minInt(maxAttempts, hedgePolicy.MaxAdditionalAttempts+1), 1)
	}
	return requestExecutionLimits{HedgePolicy: hedgePolicy, MaxAttempts: maxAttempts}
}

func (limits requestExecutionLimits) remainingLaunchCapacity(state requestExecutionState) int {
	return limits.MaxAttempts - state.launchedAttempts
}

func (limits requestExecutionLimits) shouldHedge(plan requestPlan, state requestExecutionState, index int) bool {
	return !state.hedgeUsed && limits.HedgePolicy.Enabled && limits.remainingLaunchCapacity(state) >= 2 && len(plan.Connections)-index >= 2
}

func (state *requestExecutionState) recordHedgedResult(hedged hedgedExecutionResult) {
	state.hedgeUsed = true
	state.launchedAttempts += hedged.LaunchedAttempts
	state.attempts = append(state.attempts, hedged.Attempts...)
	state.admissionRejections += hedged.AdmissionRejections
	if strings.TrimSpace(hedged.LastAdmissionReason) != "" {
		state.lastAdmissionReason = hedged.LastAdmissionReason
	}
	if strings.TrimSpace(hedged.LastError) != "" {
		state.lastError = hedged.LastError
	}
}

func (state *requestExecutionState) recordAdmissionRejection(reason string) {
	state.admissionRejections++
	state.lastAdmissionReason = reason
}

func (state *requestExecutionState) recordLaunchedAttempt(outcome executionOutcome) {
	state.launchedAttempts++
	state.attempts = append(state.attempts, outcome.Attempt)
}

func (state *requestExecutionState) result(response *http.Response, connection runtimeConnection, headers map[string]string) executionResult {
	return executionResult{Response: response, Connection: connection, RequestHeaders: headers, AttemptCount: state.launchedAttempts, Attempts: state.attempts}
}

func (state *requestExecutionState) failureResult(plan requestPlan) (executionResult, error) {
	if len(plan.Connections) == 0 {
		return executionResult{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", plan.RequestedModelID)}
	}
	if state.launchedAttempts == 0 && state.admissionRejections > 0 {
		detail := fmt.Sprintf("All connections rejected for model '%s' because admission limits are exhausted.", plan.RequestedModelID)
		if strings.TrimSpace(state.lastAdmissionReason) != "" {
			detail = fmt.Sprintf("All connections rejected for model '%s' because admission limit '%s' is exhausted.", plan.RequestedModelID, state.lastAdmissionReason)
		}
		return executionResult{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: detail}
	}
	lastError := state.lastError
	if strings.TrimSpace(lastError) == "" {
		lastError = "Unknown upstream failure"
	}
	return executionResult{}, &domainError{StatusCode: http.StatusBadGateway, Detail: fmt.Sprintf("All connections failed for model '%s'. Last error: %s", plan.RequestedModelID, lastError)}
}

func (s *Service) executionResultForHedgedWinner(plan requestPlan, state requestExecutionState, winner *executionOutcome) executionResult {
	if winner.Response.StatusCode >= 200 && winner.Response.StatusCode <= 299 && winner.Launched {
		s.recordRuntimeSuccess(plan.ProfileID, winner.Connection, plan.Strategy, winner.Attempt.ResponseTimeMS, winner.Attempt.CompletedAt)
	}
	return state.result(winner.Response, winner.Connection, winner.RequestHeaders)
}

func (s *Service) handleSingleExecutionOutcome(plan requestPlan, state *requestExecutionState, outcome executionOutcome, index int, maxAttempts int) (executionResult, bool, error) {
	if outcome.FatalError != nil {
		return executionResult{}, false, outcome.FatalError
	}
	if outcome.ProbeEligibleRecord != nil {
		s.recordRuntimeProbeEligible(plan.ProfileID, outcome.Connection, plan.Strategy, *outcome.ProbeEligibleRecord, s.nowUTC())
	}
	if outcome.Skipped {
		return executionResult{}, false, nil
	}
	if outcome.AdmissionReason != "" {
		state.recordAdmissionRejection(outcome.AdmissionReason)
		return executionResult{}, false, nil
	}
	if outcome.Launched {
		state.recordLaunchedAttempt(outcome)
	}
	if outcome.Err != nil {
		state.lastError = outcome.Err.Error()
		if outcome.Launched && !outcome.SuppressTransportFeedback {
			s.recordRuntimeTransportFailure(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.CompletedAt)
		}
		return executionResult{}, false, nil
	}
	if outcome.FailoverEligible && outcome.Launched {
		s.recordRuntimeFailoverHTTPFailure(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.CompletedAt)
	}
	if outcome.FailoverEligible && index < len(plan.Connections)-1 && state.launchedAttempts < maxAttempts {
		state.lastError = fmt.Sprintf("Upstream returned %d", outcome.Response.StatusCode)
		_ = outcome.Response.Body.Close()
		return executionResult{}, false, nil
	}
	if outcome.Response.StatusCode >= 200 && outcome.Response.StatusCode <= 299 && outcome.Launched {
		s.recordRuntimeSuccess(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.ResponseTimeMS, outcome.Attempt.CompletedAt)
	}
	return state.result(outcome.Response, outcome.Connection, outcome.RequestHeaders), true, nil
}

func (s *Service) executeHedgedRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, startIndex int, hedgePolicy loadbalance.RuntimeHedgePolicy, bodySource *runtimeRequestBodySource) (hedgedExecutionResult, error) {
	totalCandidates := hedgePolicy.MaxAdditionalAttempts + 1
	remainingConnections := len(plan.Connections) - startIndex
	if totalCandidates > remainingConnections {
		totalCandidates = remainingConnections
	}
	if totalCandidates <= 0 {
		return hedgedExecutionResult{}, nil
	}

	results := make(chan hedgedAttemptResult, totalCandidates)
	cancelFuncs := make([]context.CancelCauseFunc, 0, totalCandidates)
	inFlight := 0
	launchedCandidates := 0
	nextOrder := 0
	launchAttempt := func(order int) {
		attemptCtx, cancel := context.WithCancelCause(ctx)
		cancelFuncs = append(cancelFuncs, cancel)
		connection := plan.Connections[startIndex+order]
		inFlight++
		launchedCandidates++
		go func() {
			results <- hedgedAttemptResult{Order: order, Outcome: s.executeSingleAttempt(attemptCtx, method, plan, requestQuery, connection, bodySource)}
		}()
	}
	launchAttempt(0)
	nextOrder = 1

	timer := time.NewTimer(hedgePolicy.Delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	nonWinningAttempts := make([]executionAttempt, 0, totalCandidates)
	result := hedgedExecutionResult{ConsumedConnections: launchedCandidates}
	var winner *executionOutcome

	for inFlight > 0 {
		var timerCh <-chan time.Time
		if winner == nil && nextOrder < totalCandidates {
			timerCh = timer.C
		}
		select {
		case <-timerCh:
			launchAttempt(nextOrder)
			nextOrder++
			result.ConsumedConnections = launchedCandidates
			if winner == nil && nextOrder < totalCandidates {
				timer.Reset(hedgePolicy.Delay)
			}
		case attemptResult := <-results:
			inFlight--
			outcome := attemptResult.Outcome
			if outcome.FatalError != nil {
				for _, cancel := range cancelFuncs {
					cancel(nil)
				}
				return hedgedExecutionResult{}, outcome.FatalError
			}
			if outcome.ProbeEligibleRecord != nil {
				s.recordRuntimeProbeEligible(plan.ProfileID, outcome.Connection, plan.Strategy, *outcome.ProbeEligibleRecord, s.nowUTC())
			}
			if outcome.Skipped {
				continue
			}
			if outcome.AdmissionReason != "" {
				result.AdmissionRejections++
				result.LastAdmissionReason = outcome.AdmissionReason
				continue
			}
			if outcome.Launched {
				result.LaunchedAttempts++
			}
			if winner != nil {
				if outcome.Response != nil {
					_ = outcome.Response.Body.Close()
				}
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				continue
			}
			if outcome.Err != nil {
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				if !outcome.SuppressTransportFeedback {
					result.LastError = outcome.Err.Error()
					s.recordRuntimeTransportFailure(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.CompletedAt)
				}
				continue
			}
			if outcome.FailoverEligible {
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				result.LastError = fmt.Sprintf("Upstream returned %d", outcome.Response.StatusCode)
				s.recordRuntimeFailoverHTTPFailure(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.CompletedAt)
				_ = outcome.Response.Body.Close()
				continue
			}
			winner = &outcome
			for order, cancel := range cancelFuncs {
				if order == attemptResult.Order {
					continue
				}
				cancel(errHedgeLoserCanceled)
			}
		}
	}

	result.ConsumedConnections = launchedCandidates
	result.Attempts = nonWinningAttempts
	if winner != nil {
		result.Winner = winner
		result.Attempts = append(result.Attempts, winner.Attempt)
	}
	return result, nil
}

func (s *Service) executeSingleAttempt(ctx context.Context, method string, plan requestPlan, requestQuery string, connection runtimeConnection, bodySource *runtimeRequestBodySource) executionOutcome {
	headers, err := s.buildUpstreamHeaders(connection, plan.APIFamily, plan.ClientHeaders, plan.BlocklistRules)
	if err != nil {
		return executionOutcome{FatalError: err}
	}
	upstreamURL, err := buildUpstreamURL(connection.Endpoint.BaseURL, plan.EffectiveRequestPath, requestQuery)
	if err != nil {
		return executionOutcome{FatalError: err}
	}

	decision := s.runtimeState.TryBeginConnectionAttempt(loadbalance.RuntimeConnectionAttemptInput{
		ProfileID:     plan.ProfileID,
		ModelConfigID: connection.ModelConfigID,
		ConnectionID:  connection.ID,
		Admission: loadbalance.RuntimeConnectionAdmission{
			QPSLimit:             connection.QPSLimit,
			MaxInFlightNonStream: connection.MaxInFlightNonStream,
			MaxInFlightStream:    connection.MaxInFlightStream,
		},
		Policy:      plan.Strategy.AdmissionPolicy(),
		IsStreaming: plan.IsStreamingRequest,
		ObservedAt:  s.nowUTC(),
	})
	if decision.Skipped {
		return executionOutcome{Connection: connection, Skipped: true, ProbeEligibleRecord: decision.ProbeEligibleRecord}
	}
	if decision.AdmissionReason != "" {
		return executionOutcome{Connection: connection, AdmissionReason: decision.AdmissionReason, ProbeEligibleRecord: decision.ProbeEligibleRecord}
	}
	defer func() {
		s.runtimeState.FinishConnectionAttempt(decision.Handle, s.nowUTC())
	}()

	attemptStartedAt := s.nowUTC()
	response, launched, requestErr := s.doUpstreamRequest(ctx, plan.HTTPClient, method, upstreamURL, headers, bodySource)
	outcome := executionOutcome{Connection: connection, RequestHeaders: cloneStringMap(headers), Response: response, Launched: launched, Err: requestErr, ProbeEligibleRecord: decision.ProbeEligibleRecord}
	if launched {
		attemptCompletedAt := s.nowUTC()
		outcome.Attempt = executionAttempt{
			Connection:     connection,
			RequestURL:     upstreamURL,
			RequestHeaders: cloneStringMap(headers),
			StatusCode:     http.StatusBadGateway,
			ResponseTimeMS: durationMilliseconds(attemptCompletedAt.Sub(attemptStartedAt)),
			CompletedAt:    attemptCompletedAt,
		}
		if response != nil {
			outcome.Attempt.StatusCode = response.StatusCode
			outcome.Attempt.ResponseHeaders = response.Header.Clone()
		}
		if s.isHedgeLoserCancellation(ctx, requestErr) {
			outcome.Attempt.StatusCode = hedgeCanceledAttemptStatusCode
			outcome.SuppressTransportFeedback = true
		}
	}
	if requestErr != nil {
		return outcome
	}
	outcome.FailoverEligible = shouldFailover(response.StatusCode, plan.FailoverStatusCodes)
	outcome.Definitive = !outcome.FailoverEligible
	return outcome
}

func (s *Service) isHedgeLoserCancellation(ctx context.Context, err error) bool {
	return err != nil && errors.Is(err, context.Canceled) && errors.Is(context.Cause(ctx), errHedgeLoserCanceled)
}

func (s *Service) recordRuntimeProbeEligible(profileID int, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, state loadbalance.RuntimeConnectionState, observedAt time.Time) {
	if s == nil || s.feedbackPipeline == nil {
		return
	}
	s.feedbackPipeline.TryEnqueue(runtimeFeedbackEvent{Kind: runtimeFeedbackProbeEligible, ProfileID: profileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, State: state, ObservedAt: observedAt})
}

func (s *Service) recordRuntimeSuccess(profileID int, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, responseTimeMS int, completedAt time.Time) {
	if s == nil || s.feedbackPipeline == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeSuccess(profileID, connection.ModelConfigID, connection.ID, strategy, responseTimeMS, completedAt)
	if !transition.RecoveryEventEligible {
		return
	}
	s.feedbackPipeline.TryEnqueue(runtimeFeedbackEvent{Kind: runtimeFeedbackSuccessRecovery, ProfileID: profileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, CompletedAt: completedAt, ResponseTimeMS: responseTimeMS})
}

func (s *Service) recordRuntimeFailoverHTTPFailure(profileID int, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s == nil || s.feedbackPipeline == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeFailoverHTTPFailure(profileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.feedbackPipeline.TryEnqueue(runtimeFeedbackEvent{Kind: runtimeFeedbackFailoverHTTP, ProfileID: profileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "transient_http", CompletedAt: completedAt})
}

func (s *Service) recordRuntimeTransportFailure(profileID int, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s == nil || s.feedbackPipeline == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeTransportFailure(profileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.feedbackPipeline.TryEnqueue(runtimeFeedbackEvent{Kind: runtimeFeedbackTransportFailure, ProfileID: profileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "connect_error", CompletedAt: completedAt})
}

func (s *Service) doUpstreamRequest(ctx context.Context, client *http.Client, method string, upstreamURL string, headers map[string]string, bodySource *runtimeRequestBodySource) (*http.Response, bool, error) {
	if client == nil {
		client = s.httpClient
	}
	if client == nil {
		return nil, false, fmt.Errorf("runtime HTTP client unavailable")
	}
	requestBody, contentLength, err := bodySource.Open()
	if err != nil {
		return nil, false, fmt.Errorf("open upstream request body: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, upstreamURL, requestBody)
	if err != nil {
		if requestBody != nil {
			_ = requestBody.Close()
		}
		return nil, false, fmt.Errorf("build upstream request: %w", err)
	}
	request.ContentLength = contentLength
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	if _, ok := headers["User-Agent"]; !ok {
		if _, ok := headers["user-agent"]; !ok {
			request.Header["User-Agent"] = []string{""}
		}
	}
	response, err := client.Do(request)
	return response, true, err
}

func (s *Service) buildUpstreamHeaders(connection runtimeConnection, apiFamily string, clientHeaders map[string]string, rules []headerBlocklistRule) (map[string]string, error) {
	_ = apiFamily
	compiledAuth := connection.UpstreamAuth
	if compiledAuth == nil {
		return nil, fmt.Errorf("runtime upstream auth snapshot unavailable for connection %d", connection.ID)
	}
	proxyControlledHeaders := compiledAuth.ControlledHeaderNames

	headers := map[string]string{}
	for key, value := range clientHeaders {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		if _, blocked := clientAuthHeaders[keyLower]; blocked {
			continue
		}
		if keyLower == "content-length" || keyLower == "accept-encoding" {
			continue
		}
		if _, blocked := proxyControlledHeaders[keyLower]; blocked {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(value)
		if !ok {
			continue
		}
		headers[key] = normalizedValue
	}
	headers = sanitizeHeaders(headers, rules)
	headers[compiledAuth.AuthHeader] = compiledAuth.AuthValue
	maps.Copy(headers, compiledAuth.ExtraHeaders)
	for key, rawValue := range connection.CustomHeaders {
		if _, protected := proxyControlledHeaders[strings.ToLower(strings.TrimSpace(key))]; protected {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(fmt.Sprint(rawValue))
		if !ok {
			continue
		}
		headers[key] = normalizedValue
	}

	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, protected := proxyControlledHeaders[keyLower]; protected || !headerIsBlocked(key, rules) {
			sanitized[key] = value
		}
	}
	return sanitized, nil
}

func resolveAuthConfig(authType *string, apiFamily string) (apiFamilyAuthConfig, error) {
	resolvedKey := strings.ToLower(strings.TrimSpace(apiFamily))
	if authType != nil && strings.TrimSpace(*authType) != "" {
		resolvedKey = strings.ToLower(strings.TrimSpace(*authType))
	}
	config, ok := apiFamilyAuthConfigs[resolvedKey]
	if !ok {
		return apiFamilyAuthConfig{}, fmt.Errorf("unsupported auth_type: %s", resolvedKey)
	}
	return config, nil
}

func buildUpstreamURL(baseURL string, requestPath string, requestQuery string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse upstream base URL: %w", err)
	}
	basePath := strings.TrimRight(parsedURL.Path, "/")
	finalPath := requestPath
	if !strings.HasPrefix(finalPath, "/") {
		finalPath = "/" + finalPath
	}
	parsedURL.Path = basePath + finalPath
	parsedURL.RawPath = parsedURL.EscapedPath()
	if requestQuery != "" {
		if parsedURL.RawQuery != "" {
			parsedURL.RawQuery = parsedURL.RawQuery + "&" + requestQuery
		} else {
			parsedURL.RawQuery = requestQuery
		}
	}
	return parsedURL.String(), nil
}

func flattenHeaders(header http.Header) map[string]string {
	flattened := make(map[string]string, len(header))
	for key, values := range header {
		flattened[key] = strings.Join(values, ", ")
	}
	return flattened
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	maps.Copy(cloned, source)
	return cloned
}

func normalizeHeaderValue(value string) (string, bool) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", false
	}
	for _, character := range normalized {
		if character < 0x20 || character == 0x7f {
			return "", false
		}
	}
	return normalized, true
}

func sanitizeHeaders(headers map[string]string, rules []headerBlocklistRule) map[string]string {
	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		if headerIsBlocked(key, rules) {
			continue
		}
		sanitized[key] = value
	}
	return sanitized
}

func headerIsBlocked(name string, rules []headerBlocklistRule) bool {
	normalizedName := strings.ToLower(strings.TrimSpace(name))
	for _, rule := range rules {
		switch rule.MatchType {
		case "exact":
			if normalizedName == rule.Pattern {
				return true
			}
		case "prefix":
			if strings.HasPrefix(normalizedName, rule.Pattern) {
				return true
			}
		}
	}
	return false
}

func parseCustomHeaders(value sql.NullString) map[string]any {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(value.String), &parsed); err != nil {
		return map[string]any{}
	}
	if parsed == nil {
		return map[string]any{}
	}
	return parsed
}

func validateResolvedRuntimeOperation(operationMatch RuntimeOperationMatch, requestMethod string, requestPath string) (RuntimeOperationMatch, error) {
	operation := operationMatch.Operation
	if strings.TrimSpace(operation.Name) == "" {
		return RuntimeOperationMatch{}, &domainError{StatusCode: http.StatusNotFound, Detail: runtimeOperationNotFoundDetail}
	}
	if operation.Method != requestMethod {
		return RuntimeOperationMatch{}, &domainError{StatusCode: http.StatusMethodNotAllowed, Detail: runtimeOperationMethodNotAllowedDetail}
	}
	pathParams, ok := operation.PathMatcher.Match(requestPath)
	if !ok {
		return RuntimeOperationMatch{}, &domainError{StatusCode: http.StatusNotFound, Detail: runtimeOperationNotFoundDetail}
	}
	return RuntimeOperationMatch{Operation: operation, PathParams: cloneStringMap(pathParams)}, nil
}

func resolveModelIDForOperation(rawBody []byte, contentType string, operationMatch RuntimeOperationMatch) (string, error) {
	switch operationMatch.Operation.ModelBindingSource {
	case RuntimeOperationModelBindingBody:
		if modelID := extractModelFromBodyForOperation(rawBody, contentType, operationMatch.Operation); modelID != "" {
			return modelID, nil
		}
	case RuntimeOperationModelBindingPath:
		if modelID := strings.TrimSpace(operationMatch.PathParams["model"]); modelID != "" {
			return modelID, nil
		}
	default:
		return "", unsupportedOperationModelBindingError(operationMatch.Operation)
	}
	return "", &domainError{
		StatusCode: http.StatusBadRequest,
		Detail:     fmt.Sprintf("Cannot determine model for routing. Operation '%s' binds models from the %s.", operationMatch.Operation.Name, operationMatch.Operation.ModelBindingSource),
	}
}

func validateOperationAPIFamily(operation RuntimeOperation, targetModel runtimeModelRecord) error {
	operationAPIFamily := strings.ToLower(strings.TrimSpace(operation.APIFamily))
	targetAPIFamily := strings.ToLower(strings.TrimSpace(targetModel.APIFamily))
	if operationAPIFamily == targetAPIFamily && operationAPIFamily != "" {
		return nil
	}
	return &domainError{
		StatusCode: http.StatusBadRequest,
		Detail:     fmt.Sprintf("Operation '%s' is incompatible with api_family '%s'. Use an operation that matches the resolved model api_family.", operation.Name, targetModel.APIFamily),
	}
}

func unsupportedOperationModelBindingError(operation RuntimeOperation) error {
	return &domainError{
		StatusCode: http.StatusBadRequest,
		Detail:     fmt.Sprintf("Operation '%s' has unsupported model binding source '%s'.", operation.Name, operation.ModelBindingSource),
	}
}

func extractModelFromBody(rawBody []byte) string {
	if len(rawBody) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return ""
	}
	modelID, _ := payload["model"].(string)
	return strings.TrimSpace(modelID)
}

func rewriteModelInBody(rawBody []byte, targetModelID string) []byte {
	if len(rawBody) == 0 {
		return rawBody
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return rawBody
	}
	payload["model"] = targetModelID
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return rawBody
	}
	return rewritten
}

func rewriteModelInPath(requestPath string, originalModel string, targetModel string) string {
	if originalModel == targetModel {
		return requestPath
	}
	return strings.Replace(requestPath, "/models/"+originalModel, "/models/"+targetModel, 1)
}

func shouldFailover(statusCode int, failoverStatusCodes []int) bool {
	return slices.Contains(failoverStatusCodes, statusCode)
}

func copyResponseHeaders(target http.Header, source http.Header) {
	for key, values := range filterResponseHeaders(source) {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func filterResponseHeaders(source http.Header) http.Header {
	filtered := make(http.Header)
	for key, values := range source {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	return filtered
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func stringPointerIfNotEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}
