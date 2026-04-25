package runtime

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/endpointdomain"
	"github.com/coachpo/prism/backend/internal/pgxutil"
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
	LoadbalanceStrategyID *int
}

type runtimeEndpoint struct {
	ID      int
	Name    *string
	BaseURL string
	APIKey  string
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
	CustomHeaders           map[string]any
	PricingTemplateID       *int
	PricingTemplateSnapshot *runtimePricingTemplateSnapshot
	EndpointFXSnapshot      *runtimeEndpointFXSnapshot
	Endpoint                runtimeEndpoint
}

type headerBlocklistRule struct {
	MatchType string
	Pattern   string
}

type requestPlan struct {
	RequestedModelID       string
	ResolvedTargetModelID  *string
	ResolvedPricingModelID string
	RequestedVendorID      *int
	RequestedVendorKey     *string
	RequestedVendorName    *string
	ProfileID              int
	APIFamily              string
	AuditEnabledAtRequest  bool
	ReportCurrencySnapshot runtimeReportCurrencySnapshot
	EffectiveRequestPath   string
	RawRequestBody         []byte
	UpstreamBody           []byte
	IsStreamingRequest     bool
	Connections            []runtimeConnection
	RuntimeStates          map[int]loadbalance.RuntimeConnectionState
	BlocklistRules         []headerBlocklistRule
	ClientHeaders          map[string]string
	FailoverStatusCodes    []int
	Strategy               loadbalance.RuntimeStrategy
}

type runtimeRequestBodySource struct {
	bufferedBody         []byte
	streamingBody        io.ReadCloser
	streamingContentSize int64
	useStreamingBody     bool

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
	return source.streamingBody, source.streamingContentSize, nil
}

type executionAttempt struct {
	Connection      runtimeConnection
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

func (s *Service) buildRequestPlan(ctx context.Context, tx pgx.Tx, request *http.Request, rawBody []byte) (requestPlan, error) {
	requestedModelID, err := resolveModelID(rawBody, request.URL.Path)
	if err != nil {
		return requestPlan{}, &domainError{
			StatusCode: http.StatusBadRequest,
			Detail:     "Cannot determine model for routing. Include 'model' in the request body or use a Gemini-style model path.",
		}
	}

	activeProfile, err := s.loadActiveProfileWithCache(ctx, tx)
	if err != nil {
		return requestPlan{}, err
	}
	snapshot, err := s.loadPlanningSnapshotWithCache(ctx, tx, activeProfile.ID)
	if err != nil {
		return requestPlan{}, err
	}

	requestedModel, found := snapshot.ModelsByID[requestedModelID]
	if !found {
		return requestPlan{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", requestedModelID)}
	}
	requestedModel = cloneRuntimeModelRecord(requestedModel)

	targetModel, connections, runtimeStates, strategy, err := s.resolveExecutionTargetFromSnapshot(ctx, tx, activeProfile.ID, snapshot, requestedModel, s.nowUTC())
	if err != nil {
		return requestPlan{}, err
	}
	if err := validatePathCompatibility(targetModel.APIFamily, request.URL.Path); err != nil {
		return requestPlan{}, err
	}

	orderedConnectionIDs, err := loadbalance.OrderConnectionIDs(ctx, tx, activeProfile.ID, targetModel.ID, strategy, toConnectionOrderCandidates(connections), runtimeStates, s.nowUTC())
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
	if bodyModelID := extractModelFromBody(rawBody); bodyModelID != "" && bodyModelID != targetModel.ModelID {
		upstreamBody = rewriteModelInBody(rawBody, targetModel.ModelID)
	}

	blocklistRules := cloneHeaderBlocklistRules(snapshot.BlocklistRules)
	reportCurrencySnapshot := cloneReportCurrencySnapshot(snapshot.ReportCurrency)

	return requestPlan{
		RequestedModelID:       requestedModelID,
		ResolvedTargetModelID:  stringPointerIfNotEmpty(targetModel.ModelID),
		ResolvedPricingModelID: strings.TrimSpace(targetModel.ModelID),
		RequestedVendorID:      requestedModel.VendorID,
		RequestedVendorKey:     requestedModel.VendorKey,
		RequestedVendorName:    requestedModel.VendorName,
		ProfileID:              activeProfile.ID,
		APIFamily:              targetModel.APIFamily,
		AuditEnabledAtRequest:  targetModel.AuditEnabled,
		ReportCurrencySnapshot: reportCurrencySnapshot,
		EffectiveRequestPath:   effectiveRequestPath,
		RawRequestBody:         rawBody,
		UpstreamBody:           upstreamBody,
		IsStreamingRequest:     requestWantsStream(rawBody),
		Connections:            orderedConnections,
		RuntimeStates:          runtimeStates,
		BlocklistRules:         blocklistRules,
		ClientHeaders:          flattenHeaders(request.Header),
		FailoverStatusCodes:    strategy.FailoverStatusCodes(),
		Strategy:               strategy,
	}, nil
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

func runtimeConnectionIDs(connections []runtimeConnection) []int {
	ids := make([]int, 0, len(connections))
	for _, connection := range connections {
		ids = append(ids, connection.ID)
	}
	return ids
}

type runtimeLeaseResult struct {
	Token    string
	Acquired bool
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

	for index := 0; index < len(plan.Connections); index++ {
		if !hedgeUsed && hedgePolicy.Enabled && hedgePolicy.MaxAdditionalAttempts > 0 && len(plan.Connections)-index >= 2 {
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
					if feedbackErr := s.recordRuntimeSuccess(ctx, plan.ProfileID, winner.Connection.ID, plan.Strategy, winner.Attempt.ResponseTimeMS, winner.Attempt.CompletedAt); feedbackErr != nil {
						_ = winner.Response.Body.Close()
						return executionResult{}, feedbackErr
					}
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
				if feedbackErr := s.recordRuntimeTransportFailure(ctx, plan.ProfileID, outcome.Connection.ID, plan.Strategy, outcome.Attempt.CompletedAt); feedbackErr != nil {
					return executionResult{}, feedbackErr
				}
			}
			continue
		}
		if outcome.FailoverEligible && outcome.Launched {
			if feedbackErr := s.recordRuntimeFailoverHTTPFailure(ctx, plan.ProfileID, outcome.Connection.ID, plan.Strategy, outcome.Attempt.CompletedAt); feedbackErr != nil {
				_ = outcome.Response.Body.Close()
				return executionResult{}, feedbackErr
			}
		}
		if outcome.FailoverEligible && index < len(plan.Connections)-1 {
			lastError = fmt.Sprintf("Upstream returned %d", outcome.Response.StatusCode)
			_ = outcome.Response.Body.Close()
			continue
		}
		if outcome.Response.StatusCode >= 200 && outcome.Response.StatusCode <= 299 && outcome.Launched {
			if feedbackErr := s.recordRuntimeSuccess(ctx, plan.ProfileID, outcome.Connection.ID, plan.Strategy, outcome.Attempt.ResponseTimeMS, outcome.Attempt.CompletedAt); feedbackErr != nil {
				_ = outcome.Response.Body.Close()
				return executionResult{}, feedbackErr
			}
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
	primary := plan.Connections[startIndex]
	secondary := plan.Connections[startIndex+1]
	firstCtx, firstCancel := context.WithCancelCause(ctx)
	defer firstCancel(nil)
	results := make(chan hedgedAttemptResult, 2)
	go func() {
		results <- hedgedAttemptResult{Order: 0, Outcome: s.executeSingleAttempt(firstCtx, method, plan, requestQuery, primary, bodySource)}
	}()
	inFlight := 1
	secondStarted := false
	consumedConnections := 1
	var secondCancel context.CancelCauseFunc
	timer := time.NewTimer(hedgePolicy.Delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()
	launchSecond := func() {
		if secondStarted {
			return
		}
		secondStarted = true
		consumedConnections = 2
		secondCtx, cancel := context.WithCancelCause(ctx)
		secondCancel = cancel
		inFlight++
		go func() {
			results <- hedgedAttemptResult{Order: 1, Outcome: s.executeSingleAttempt(secondCtx, method, plan, requestQuery, secondary, bodySource)}
		}()
	}

	nonWinningAttempts := make([]executionAttempt, 0, 2)
	result := hedgedExecutionResult{ConsumedConnections: consumedConnections}
	var winner *executionOutcome

	for inFlight > 0 {
		var timerCh <-chan time.Time
		if !secondStarted && winner == nil {
			timerCh = timer.C
		}
		select {
		case <-timerCh:
			launchSecond()
			result.ConsumedConnections = consumedConnections
		case attemptResult := <-results:
			inFlight--
			outcome := attemptResult.Outcome
			if outcome.FatalError != nil {
				if secondCancel != nil {
					secondCancel(nil)
				}
				firstCancel(nil)
				return hedgedExecutionResult{}, outcome.FatalError
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
					if feedbackErr := s.recordRuntimeTransportFailure(ctx, plan.ProfileID, outcome.Connection.ID, plan.Strategy, outcome.Attempt.CompletedAt); feedbackErr != nil {
						return hedgedExecutionResult{}, feedbackErr
					}
				}
				continue
			}
			if outcome.FailoverEligible {
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				result.LastError = fmt.Sprintf("Upstream returned %d", outcome.Response.StatusCode)
				if feedbackErr := s.recordRuntimeFailoverHTTPFailure(ctx, plan.ProfileID, outcome.Connection.ID, plan.Strategy, outcome.Attempt.CompletedAt); feedbackErr != nil {
					_ = outcome.Response.Body.Close()
					return hedgedExecutionResult{}, feedbackErr
				}
				_ = outcome.Response.Body.Close()
				continue
			}
			winner = &outcome
			if attemptResult.Order == 0 {
				if secondCancel != nil {
					secondCancel(errHedgeLoserCanceled)
				}
			} else {
				firstCancel(errHedgeLoserCanceled)
			}
		}
	}

	result.ConsumedConnections = consumedConnections
	if winner != nil {
		result.Winner = winner
		result.Attempts = append(nonWinningAttempts, winner.Attempt)
	}
	return result, nil
}

func (s *Service) executeSingleAttempt(ctx context.Context, method string, plan requestPlan, requestQuery string, connection runtimeConnection, bodySource *runtimeRequestBodySource) executionOutcome {
	state := plan.RuntimeStates[connection.ID]
	admissionReason := loadbalance.AdmissionRejectionReason(
		state,
		loadbalance.RuntimeConnectionAdmission{
			QPSLimit:             connection.QPSLimit,
			MaxInFlightNonStream: connection.MaxInFlightNonStream,
			MaxInFlightStream:    connection.MaxInFlightStream,
		},
		plan.Strategy.AdmissionPolicy(),
		plan.IsStreamingRequest,
		s.nowUTC(),
	)
	if admissionReason != "" {
		if recordErr := s.recordRuntimeAdmissionRejection(ctx, plan.ProfileID, connection.ID, s.nowUTC()); recordErr != nil {
			return executionOutcome{FatalError: recordErr}
		}
		return executionOutcome{Connection: connection, AdmissionReason: admissionReason}
	}
	headers, err := s.buildUpstreamHeaders(connection, plan.APIFamily, plan.ClientHeaders, plan.BlocklistRules)
	if err != nil {
		return executionOutcome{FatalError: err}
	}
	upstreamURL, err := buildUpstreamURL(connection.Endpoint.BaseURL, plan.EffectiveRequestPath, requestQuery)
	if err != nil {
		return executionOutcome{FatalError: err}
	}
	probeLease, err := s.acquireRuntimeProbeLease(ctx, plan.ProfileID, connection.ID, state, s.nowUTC())
	if err != nil {
		return executionOutcome{FatalError: err}
	}
	if !probeLease.Acquired {
		return executionOutcome{Connection: connection, Skipped: true}
	}
	nonStreamLease, err := s.acquireRuntimeNonStreamLease(ctx, plan.ProfileID, connection, plan.Strategy, plan.IsStreamingRequest, s.nowUTC())
	if err != nil {
		if probeLease.Token != "" {
			if releaseErr := s.releaseRuntimeLeaseDetached(ctx, probeLease.Token); releaseErr != nil {
				return executionOutcome{FatalError: releaseErr}
			}
		}
		return executionOutcome{FatalError: err}
	}
	if !nonStreamLease.Acquired {
		if probeLease.Token != "" {
			if releaseErr := s.releaseRuntimeLeaseDetached(ctx, probeLease.Token); releaseErr != nil {
				return executionOutcome{FatalError: releaseErr}
			}
		}
		if recordErr := s.recordRuntimeAdmissionRejection(ctx, plan.ProfileID, connection.ID, s.nowUTC()); recordErr != nil {
			return executionOutcome{FatalError: recordErr}
		}
		return executionOutcome{Connection: connection, AdmissionReason: "max_in_flight_non_stream"}
	}
	attemptStartedAt := s.nowUTC()
	response, launched, requestErr := s.doUpstreamRequest(ctx, method, upstreamURL, headers, bodySource)
	if nonStreamLease.Token != "" {
		if releaseErr := s.releaseRuntimeLeaseDetached(ctx, nonStreamLease.Token); releaseErr != nil {
			if response != nil {
				_ = response.Body.Close()
			}
			if probeLease.Token != "" {
				_ = s.releaseRuntimeLeaseDetached(ctx, probeLease.Token)
			}
			return executionOutcome{FatalError: releaseErr}
		}
	}
	if probeLease.Token != "" {
		if releaseErr := s.releaseRuntimeLeaseDetached(ctx, probeLease.Token); releaseErr != nil {
			if response != nil {
				_ = response.Body.Close()
			}
			return executionOutcome{FatalError: releaseErr}
		}
	}
	outcome := executionOutcome{Connection: connection, RequestHeaders: cloneStringMap(headers), Response: response, Launched: launched, Err: requestErr}
	if launched {
		attemptCompletedAt := s.nowUTC()
		outcome.Attempt = executionAttempt{
			Connection:     connection,
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

func (s *Service) releaseRuntimeLeaseDetached(ctx context.Context, token string) error {
	releaseCtx, cancel := runtimeFeedbackContext(ctx)
	defer cancel()
	return s.releaseRuntimeLease(releaseCtx, token)
}

func (s *Service) recordRuntimeSuccess(ctx context.Context, profileID int, connectionID int, strategy loadbalance.RuntimeStrategy, responseTimeMS int, completedAt time.Time) error {
	feedbackCtx, cancel := runtimeFeedbackContext(ctx)
	defer cancel()
	_, err := pgxutil.InTxValue(feedbackCtx, s.pool, "runtime", func(tx pgx.Tx) (bool, error) {
		return true, loadbalance.RecordRuntimeSuccess(feedbackCtx, tx, profileID, connectionID, strategy, responseTimeMS, completedAt)
	})
	if err != nil {
		return fmt.Errorf("persist runtime success feedback: %w", err)
	}
	return nil
}

func (s *Service) recordRuntimeAdmissionRejection(ctx context.Context, profileID int, connectionID int, observedAt time.Time) error {
	feedbackCtx, cancel := runtimeFeedbackContext(ctx)
	defer cancel()
	_, err := pgxutil.InTxValue(feedbackCtx, s.pool, "runtime", func(tx pgx.Tx) (bool, error) {
		return true, loadbalance.RecordRuntimeAdmissionRejection(feedbackCtx, tx, profileID, connectionID, observedAt)
	})
	if err != nil {
		return fmt.Errorf("persist runtime admission rejection feedback: %w", err)
	}
	return nil
}

func (s *Service) recordRuntimeFailoverHTTPFailure(ctx context.Context, profileID int, connectionID int, strategy loadbalance.RuntimeStrategy, completedAt time.Time) error {
	feedbackCtx, cancel := runtimeFeedbackContext(ctx)
	defer cancel()
	_, err := pgxutil.InTxValue(feedbackCtx, s.pool, "runtime", func(tx pgx.Tx) (bool, error) {
		return true, loadbalance.RecordRuntimeFailoverHTTPFailure(feedbackCtx, tx, profileID, connectionID, strategy, completedAt)
	})
	if err != nil {
		return fmt.Errorf("persist runtime failure feedback: %w", err)
	}
	return nil
}

func (s *Service) recordRuntimeTransportFailure(ctx context.Context, profileID int, connectionID int, strategy loadbalance.RuntimeStrategy, completedAt time.Time) error {
	feedbackCtx, cancel := runtimeFeedbackContext(ctx)
	defer cancel()
	_, err := pgxutil.InTxValue(feedbackCtx, s.pool, "runtime", func(tx pgx.Tx) (bool, error) {
		return true, loadbalance.RecordRuntimeTransportFailure(feedbackCtx, tx, profileID, connectionID, strategy, completedAt)
	})
	if err != nil {
		return fmt.Errorf("persist runtime transport failure feedback: %w", err)
	}
	return nil
}

func (s *Service) acquireRuntimeProbeLease(ctx context.Context, profileID int, connectionID int, state loadbalance.RuntimeConnectionState, observedAt time.Time) (runtimeLeaseResult, error) {
	if !loadbalance.RequiresHalfOpenProbeLease(state, observedAt) {
		return runtimeLeaseResult{Acquired: true}, nil
	}
	result, err := pgxutil.InTxValue(ctx, s.pool, "runtime", func(tx pgx.Tx) (runtimeLeaseResult, error) {
		leaseToken, acquired, leaseErr := loadbalance.TryAcquireRuntimeHalfOpenProbeLease(ctx, tx, profileID, connectionID, observedAt)
		if leaseErr != nil {
			return runtimeLeaseResult{}, leaseErr
		}
		return runtimeLeaseResult{Token: leaseToken, Acquired: acquired}, nil
	})
	if err != nil {
		return runtimeLeaseResult{}, fmt.Errorf("acquire runtime probe lease: %w", err)
	}
	return result, nil
}

func (s *Service) acquireRuntimeNonStreamLease(ctx context.Context, profileID int, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, isStreamingRequest bool, observedAt time.Time) (runtimeLeaseResult, error) {
	if isStreamingRequest || connection.MaxInFlightNonStream == nil || *connection.MaxInFlightNonStream <= 0 {
		return runtimeLeaseResult{Acquired: true}, nil
	}
	if !strategy.AdmissionPolicy().RespectInFlightLimits {
		return runtimeLeaseResult{Acquired: true}, nil
	}
	result, err := pgxutil.InTxValue(ctx, s.pool, "runtime", func(tx pgx.Tx) (runtimeLeaseResult, error) {
		leaseToken, acquired, leaseErr := loadbalance.TryAcquireRuntimeNonStreamLease(ctx, tx, profileID, connection.ID, *connection.MaxInFlightNonStream, observedAt)
		if leaseErr != nil {
			return runtimeLeaseResult{}, leaseErr
		}
		return runtimeLeaseResult{Token: leaseToken, Acquired: acquired}, nil
	})
	if err != nil {
		return runtimeLeaseResult{}, fmt.Errorf("acquire runtime non-stream lease: %w", err)
	}
	return result, nil
}

func (s *Service) releaseRuntimeLease(ctx context.Context, leaseToken string) error {
	if strings.TrimSpace(leaseToken) == "" {
		return nil
	}
	releaseCtx, cancel := runtimeFeedbackContext(ctx)
	defer cancel()
	_, err := pgxutil.InTxValue(releaseCtx, s.pool, "runtime", func(tx pgx.Tx) (bool, error) {
		return true, loadbalance.ReleaseRuntimeLease(releaseCtx, tx, leaseToken)
	})
	if err != nil {
		return fmt.Errorf("release runtime lease: %w", err)
	}
	return nil
}

func runtimeFeedbackContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
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
	config, err := resolveAuthConfig(connection.AuthType, apiFamily)
	if err != nil {
		return nil, err
	}
	apiKey, err := endpointdomain.DecryptSecret(connection.Endpoint.APIKey, s.secretEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("resolve endpoint api key: %w", err)
	}
	proxyControlledHeaders := map[string]struct{}{strings.ToLower(config.AuthHeader): {}}
	for key := range config.ExtraHeaders {
		proxyControlledHeaders[strings.ToLower(key)] = struct{}{}
	}

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
	headers[config.AuthHeader] = config.AuthPrefix + apiKey
	for key, value := range config.ExtraHeaders {
		headers[key] = value
	}
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
	for key, value := range source {
		cloned[key] = value
	}
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
	for _, candidate := range failoverStatusCodes {
		if statusCode == candidate {
			return true
		}
	}
	return false
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
