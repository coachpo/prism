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
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

var (
	geminiModelRE           = regexp.MustCompile(`^/v1beta/models/([^/:]+)`)
	geminiNativePathRE      = regexp.MustCompile(`^/v1beta/models/[^/:]+(?:[:/].*)?/?$`)
	anthropicMessagesPathRE = regexp.MustCompile(`^/v1/messages(?:/count_tokens)?/?$`)
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
	ID                    int
	ProfileID             int
	APIFamily             string
	ModelID               string
	ModelType             string
	VendorID              *int
	VendorKey             *string
	VendorName            *string
	AuditEnabled          bool
	AuditCaptureBodies    bool
	LoadbalanceStrategyID *int
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
	CachedInputPrice    *string
	CacheCreationPrice  *string
	ReasoningPrice      *string
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

var errHedgeLoserCanceled = errors.New("hedge loser canceled")

const hedgeCanceledAttemptStatusCode = 499

func (s *Service) buildRequestPlan(ctx context.Context, request *http.Request, rawBody []byte) (requestPlan, error) {
	requestedModelID, err := resolveModelID(rawBody, request.URL.Path)
	if err != nil {
		return requestPlan{}, &domainError{
			StatusCode: http.StatusBadRequest,
			Detail:     "Cannot determine model for routing. Include 'model' in the request body or use a Gemini-style model path.",
		}
	}

	if s.cache == nil {
		return requestPlan{}, runtimeSnapshotDomainError(ErrPublishedRuntimeSnapshotUnavailable)
	}
	activeProfile, snapshot, err := s.cache.LoadFreshActiveRuntimePlan(ctx)
	if err != nil {
		return requestPlan{}, runtimeSnapshotDomainError(err)
	}

	requestedModel, found := snapshot.ModelsByID[requestedModelID]
	if !found {
		return requestPlan{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", requestedModelID)}
	}

	targetModel, connections, runtimeStates, strategy, err := s.resolveExecutionTargetFromSnapshot(activeProfile.ID, snapshot, requestedModel, s.nowUTC())
	if err != nil {
		return requestPlan{}, err
	}
	if err := validatePathCompatibility(targetModel.APIFamily, request.URL.Path); err != nil {
		return requestPlan{}, err
	}

	orderedConnectionIDs, err := loadbalance.OrderConnectionIDs(activeProfile.ID, targetModel.ID, strategy, toConnectionOrderCandidates(connections), runtimeStates, s.runtimeState, s.nowUTC())
	if err != nil {
		return requestPlan{}, err
	}
	orderedConnections := orderConnectionsByID(connections, orderedConnectionIDs)
	if len(orderedConnections) == 0 {
		return requestPlan{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", requestedModelID)}
	}

	effectiveRequestPath := request.URL.Path
	if pathModelID := extractModelFromPath(request.URL.Path); pathModelID != "" && pathModelID != targetModel.ModelID {
		effectiveRequestPath = rewriteModelInPath(request.URL.Path, pathModelID, targetModel.ModelID)
	}

	upstreamBody := rawBody
	requestGenerationParams := extractBufferedRequestGenerationParams(targetModel.APIFamily, request.URL.Path, rawBody)
	if bodyModelID := extractModelFromBody(rawBody); bodyModelID != "" && bodyModelID != targetModel.ModelID {
		upstreamBody = rewriteModelInBody(rawBody, targetModel.ModelID)
	}

	return requestPlan{
		RequestedModelID:            requestedModelID,
		ResolvedTargetModelID:       stringPointerIfNotEmpty(targetModel.ModelID),
		ResolvedPricingModelID:      strings.TrimSpace(targetModel.ModelID),
		RequestedVendorID:           requestedModel.VendorID,
		RequestedVendorKey:          requestedModel.VendorKey,
		RequestedVendorName:         requestedModel.VendorName,
		ProfileID:                   activeProfile.ID,
		APIFamily:                   targetModel.APIFamily,
		AuditEnabledAtRequest:       targetModel.AuditEnabled,
		AuditCaptureBodiesAtRequest: targetModel.AuditEnabled && targetModel.AuditCaptureBodies,
		ReportCurrencySnapshot:      snapshot.ReportCurrency,
		EffectiveRequestPath:        effectiveRequestPath,
		RawRequestBody:              rawBody,
		UpstreamBody:                upstreamBody,
		IsStreamingRequest:          requestWantsStream(rawBody, effectiveRequestPath),
		Connections:                 orderedConnections,
		RuntimeStates:               runtimeStates,
		BlocklistRules:              snapshot.BlocklistRules,
		ClientHeaders:               flattenHeaders(request.Header),
		FailoverStatusCodes:         strategy.FailoverStatusCodes(),
		Strategy:                    strategy,
		RequestGenerationParams:     requestGenerationParams,
	}, nil
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
	launchedAttempts := 0
	attempts := make([]executionAttempt, 0, len(plan.Connections))
	lastError := ""
	lastAdmissionReason := ""
	admissionRejections := 0
	hedgePolicy := plan.Strategy.HedgePolicy()
	hedgeUsed := false
	maxAttempts := len(plan.Connections)
	if strings.EqualFold(strings.TrimSpace(plan.Strategy.StrategyType), "adaptive") {
		maxAttempts = max(minInt(maxAttempts, hedgePolicy.MaxAdditionalAttempts+1), 1)
	}

	for index := 0; index < len(plan.Connections); index++ {
		remainingLaunchCapacity := maxAttempts - launchedAttempts
		if remainingLaunchCapacity <= 0 {
			break
		}
		if !hedgeUsed && hedgePolicy.Enabled && remainingLaunchCapacity >= 2 && len(plan.Connections)-index >= 2 {
			hedged, err := s.executeHedgedRequest(ctx, method, plan, requestQuery, index, hedgePolicy, bodySource)
			if err != nil {
				return executionResult{}, err
			}
			hedgeUsed = true
			launchedAttempts += hedged.LaunchedAttempts
			attempts = append(attempts, hedged.Attempts...)
			admissionRejections += hedged.AdmissionRejections
			if strings.TrimSpace(hedged.LastAdmissionReason) != "" {
				lastAdmissionReason = hedged.LastAdmissionReason
			}
			if strings.TrimSpace(hedged.LastError) != "" {
				lastError = hedged.LastError
			}
			if hedged.Winner != nil {
				winner := hedged.Winner
				if winner.Response.StatusCode >= 200 && winner.Response.StatusCode <= 299 && winner.Launched {
					s.recordRuntimeSuccess(plan.ProfileID, winner.Connection, plan.Strategy, winner.Attempt.ResponseTimeMS, winner.Attempt.CompletedAt)
				}
				return executionResult{Response: winner.Response, Connection: winner.Connection, RequestHeaders: winner.RequestHeaders, AttemptCount: launchedAttempts, Attempts: attempts}, nil
			}
			index += hedged.ConsumedConnections - 1
			continue
		}

		outcome := s.executeSingleAttempt(ctx, method, plan, requestQuery, plan.Connections[index], bodySource)
		if outcome.FatalError != nil {
			return executionResult{}, outcome.FatalError
		}
		if outcome.ProbeEligibleRecord != nil {
			s.recordRuntimeProbeEligible(plan.ProfileID, outcome.Connection, plan.Strategy, *outcome.ProbeEligibleRecord, s.nowUTC())
		}
		if outcome.Skipped {
			continue
		}
		if outcome.AdmissionReason != "" {
			admissionRejections++
			lastAdmissionReason = outcome.AdmissionReason
			continue
		}
		if outcome.Launched {
			launchedAttempts++
			attempts = append(attempts, outcome.Attempt)
		}
		if outcome.Err != nil {
			lastError = outcome.Err.Error()
			if outcome.Launched && !outcome.SuppressTransportFeedback {
				s.recordRuntimeTransportFailure(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.CompletedAt)
			}
			continue
		}
		if outcome.FailoverEligible && outcome.Launched {
			s.recordRuntimeFailoverHTTPFailure(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.CompletedAt)
		}
		if outcome.FailoverEligible && index < len(plan.Connections)-1 && launchedAttempts < maxAttempts {
			lastError = fmt.Sprintf("Upstream returned %d", outcome.Response.StatusCode)
			_ = outcome.Response.Body.Close()
			continue
		}
		if outcome.Response.StatusCode >= 200 && outcome.Response.StatusCode <= 299 && outcome.Launched {
			s.recordRuntimeSuccess(plan.ProfileID, outcome.Connection, plan.Strategy, outcome.Attempt.ResponseTimeMS, outcome.Attempt.CompletedAt)
		}
		return executionResult{Response: outcome.Response, Connection: outcome.Connection, RequestHeaders: outcome.RequestHeaders, AttemptCount: launchedAttempts, Attempts: attempts}, nil
	}

	if len(plan.Connections) == 0 {
		return executionResult{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", plan.RequestedModelID)}
	}
	if launchedAttempts == 0 && admissionRejections > 0 {
		detail := fmt.Sprintf("All connections rejected for model '%s' because admission limits are exhausted.", plan.RequestedModelID)
		if strings.TrimSpace(lastAdmissionReason) != "" {
			detail = fmt.Sprintf("All connections rejected for model '%s' because admission limit '%s' is exhausted.", plan.RequestedModelID, lastAdmissionReason)
		}
		return executionResult{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: detail}
	}
	if strings.TrimSpace(lastError) == "" {
		lastError = "Unknown upstream failure"
	}
	return executionResult{}, &domainError{StatusCode: http.StatusBadGateway, Detail: fmt.Sprintf("All connections failed for model '%s'. Last error: %s", plan.RequestedModelID, lastError)}
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
	response, launched, requestErr := s.doUpstreamRequest(ctx, method, upstreamURL, headers, bodySource)
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

func (s *Service) doUpstreamRequest(ctx context.Context, method string, upstreamURL string, headers map[string]string, bodySource *runtimeRequestBodySource) (*http.Response, bool, error) {
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
	response, err := s.httpClient.Do(request)
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

func resolveModelID(rawBody []byte, requestPath string) (string, error) {
	if modelID := extractModelFromBody(rawBody); modelID != "" {
		return modelID, nil
	}
	if modelID := extractModelFromPath(requestPath); modelID != "" {
		return modelID, nil
	}
	return "", fmt.Errorf("model is required")
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

func extractModelFromPath(requestPath string) string {
	matches := geminiModelRE.FindStringSubmatch(requestPath)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func rewriteModelInPath(requestPath string, originalModel string, targetModel string) string {
	if originalModel == targetModel {
		return requestPath
	}
	return strings.Replace(requestPath, "/models/"+originalModel, "/models/"+targetModel, 1)
}

func validatePathCompatibility(apiFamily string, requestPath string) error {
	pathFamily := "generic"
	switch {
	case geminiNativePathRE.MatchString(requestPath):
		pathFamily = "gemini_native"
	case anthropicMessagesPathRE.MatchString(requestPath):
		pathFamily = "anthropic_messages"
	}
	allowedFamilies := map[string]map[string]struct{}{
		"openai":    {"generic": {}},
		"anthropic": {"anthropic_messages": {}},
		"gemini":    {"gemini_native": {}},
	}
	allowed, ok := allowedFamilies[strings.ToLower(strings.TrimSpace(apiFamily))]
	if !ok {
		return nil
	}
	if _, ok := allowed[pathFamily]; ok {
		return nil
	}
	return &domainError{StatusCode: http.StatusBadRequest, Detail: fmt.Sprintf("Path '%s' is incompatible with api_family '%s'. Use an api-family-native path.", requestPath, apiFamily)}
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
