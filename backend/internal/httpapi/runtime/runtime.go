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
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/safediag"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	gatewayrouting "github.com/coachpo/prism/backend/internal/gateway/routing"
	"github.com/coachpo/prism/backend/internal/platform/bodylimits"
)

const runtimeAdmissionExhaustedErrorCode = "admission_exhausted"

// maxUpstreamAttemptsPerIngress is the fixed platform safety cap for launched
// upstream attempts per ingress (SPEC §4.3/§5.1). The next launch beyond this
// cap terminates with gateway 503 + typed attempt_budget_exhausted; the cap
// never reorders or changes decisions within the limit.
const maxUpstreamAttemptsPerIngress = 64

const runtimeAttemptBudgetExhaustedErrorCode = "attempt_budget_exhausted"

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

// forwardableClientHeaderNames is the only set of client headers allowed to
// pass through to the upstream verbatim. Credentials (authorization /
// x-api-key / x-goog-api-key), session state (cookie), tracing and arbitrary
// custom headers are never forwarded; operators add extra headers via
// connection.custom_headers instead.
//
// user-agent is deliberately absent. It is the strongest client fingerprint
// there is, and forwarding it also leaks transitively when the upstream is
// itself a proxy. An upstream that demands a particular User-Agent is stating a
// fact about that endpoint, not about whoever happened to call, so it belongs on
// connection.custom_headers: declared once, identical on every request, and
// visible afterwards through request_logs.user_agent_overridden. Forwarding the
// caller's value instead made acceptance depend on which client made the call —
// the same model working from one IDE and failing from a script. With nothing
// configured, doUpstreamRequest sends an empty User-Agent rather than Go's
// default, so no client identity reaches the upstream.
var forwardableClientHeaderNames = map[string]struct{}{
	"accept":              {},
	"accept-language":     {},
	"content-type":        {},
	"anthropic-version":   {},
	"anthropic-beta":      {},
	"openai-beta":         {},
	"openai-organization": {},
	"openai-project":      {},
}

// forwardableClientHeaders applies the outbound whitelist. content-length and
// accept-encoding are decided by the transport and response decoding and are
// never forwarded.
func forwardableClientHeaders(clientHeaders map[string]string, proxyControlledHeaders map[string]struct{}) map[string]string {
	forwarded := make(map[string]string, len(clientHeaders))
	for key, value := range clientHeaders {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, allowed := forwardableClientHeaderNames[keyLower]; !allowed {
			continue
		}
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		if _, blocked := proxyControlledHeaders[keyLower]; blocked {
			continue
		}
		normalizedValue, ok := normalizeHeaderValue(value)
		if !ok {
			continue
		}
		forwarded[key] = normalizedValue
	}
	return forwarded
}

// upstreamResponseHeaderNames is the set of upstream response headers allowed
// back to the caller. Upstream session state (set-cookie), identity and
// version exposure (server, x-powered-by, vendor-private x-* headers) and
// upstream CORS decisions (access-control-*) are not relayed: Prism owns this
// response.
var upstreamResponseHeaderNames = map[string]struct{}{
	"content-type":        {},
	"content-length":      {},
	"content-encoding":    {},
	"content-disposition": {},
	"cache-control":       {},
	"date":                {},
	"etag":                {},
	"last-modified":       {},
	"vary":                {},
	"retry-after":         {},
	"request-id":          {},
}

// upstreamResponseHeaderPrefixes keeps the rate-limit headers callers need
// for backpressure.
var upstreamResponseHeaderPrefixes = []string{"x-ratelimit-", "anthropic-ratelimit-", "openai-"}

func copyUpstreamResponseHeaders(target http.Header, source http.Header) {
	for key, values := range filterUpstreamResponseHeaders(source) {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func filterUpstreamResponseHeaders(source http.Header) http.Header {
	filtered := make(http.Header)
	for key, values := range source {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		if !upstreamResponseHeaderAllowed(keyLower) {
			continue
		}
		for _, value := range values {
			filtered.Add(key, value)
		}
	}
	return filtered
}

func upstreamResponseHeaderAllowed(keyLower string) bool {
	if _, allowed := upstreamResponseHeaderNames[keyLower]; allowed {
		return true
	}
	for _, prefix := range upstreamResponseHeaderPrefixes {
		if strings.HasPrefix(keyLower, prefix) {
			return true
		}
	}
	return false
}

type runtimeModelRecord struct {
	ID                    int
	ProfileID             int
	APIFamily             string
	ModelID               string
	VendorID              *int
	VendorKey             *string
	VendorName            *string
	AuditEnabled          bool
	AuditCaptureBodies    bool
	LoadbalanceStrategyID *int
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
	CreatedAt             time.Time
}

type runtimeEndpoint struct {
	ID      int
	Name    *string
	BaseURL string
}

type runtimePricingTemplateSnapshot struct {
	ID                     int
	Name                   string
	RevisionID             int64
	PricingUnit            string
	PricingCurrencyCode    string
	ReportingCurrencyEpoch *int
	InputPrice             string
	OutputPrice            string
	CachedInputPrice       string
	CacheCreationPrice     string
	ReasoningPrice         string
	Version                int
	VersionEffectiveAt     *time.Time
}

type runtimeEndpointFXSnapshot struct {
	ModelID    string
	EndpointID int
	FXRate     string
}

type runtimeReportCurrencySnapshot struct {
	Code   string
	Symbol string
	// Epoch is the profile-local reporting currency epoch ordinal (0 when
	// unavailable, e.g. legacy settings without an epoch row).
	Epoch int
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
	APIFamily               string
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
	CustomRequestParameters *terminaltarget.CustomRequestParameters
	PricingTemplateID       *int
	PricingTemplateSnapshot *runtimePricingTemplateSnapshot
	OpenAITextCapability    *string
	OpenAIImageCapability   *string
	RoutingSchedule         terminaltarget.CompiledRoutingSchedule
	EndpointFXSnapshot      *runtimeEndpointFXSnapshot
	UpstreamAuth            *runtimeConnectionUpstreamAuthSnapshot
	Endpoint                runtimeEndpoint
}

func cloneRuntimeIntPointer(source *int) *int {
	if source == nil {
		return nil
	}
	return intPtr(*source)
}

func cloneRuntimeInt64Pointer(source *int64) *int64 {
	if source == nil {
		return nil
	}
	return int64Ptr(*source)
}

func cloneRuntimeStringPointer(source *string) *string {
	if source == nil {
		return nil
	}
	return stringPtr(*source)
}

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
	Connection                  runtimeConnection
	ResolvedTargetModelID       string
	RequestURL                  string
	RequestHeaders              map[string]string
	RequestBody                 []byte
	ResponseHeaders             http.Header
	StatusCode                  int
	ResponseTimeMS              int
	ResponseHeadersLatencyMS    int
	CompletedAt                 time.Time
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	UpstreamOperationName       string
	UpstreamRequestPath         string
	OperationTranslationMode    TranslationMode
	RequestGenerationParams     *requestGenerationParamsSnapshot

	// Attempt lifecycle (Observe SPEC §3.5): frozen at the launch site.
	LaunchOrdinal              int
	AttemptTrigger             string
	AttemptResult              string
	IsWinner                   bool
	AttemptDurationMS          int
	UpstreamRequestStarted     bool
	ResponseHeadersReceived    bool
	FirstBodyOrStreamEventSeen bool
	StreamOutcome              string
	StreamErrorKind            *string
	StreamErrorDetail          *string

	// Failure diagnostics: safe bounded projection. For intermediate
	// failover-eligible non-2xx responses, the sampler fills this asynchronously;
	// the telemetry sealer uses a generic fallback when the sampler has not
	// completed by sealing time.
	Diagnostics *attemptFailureDiagnostics
	Sampler     *failedResponseSampler
}

func (attempt executionAttempt) diagnosticsOrFallback(statusCode int) attemptFailureDiagnostics {
	if attempt.Diagnostics != nil {
		return *attempt.Diagnostics
	}
	if attempt.Sampler != nil && attempt.Sampler.result != nil {
		if diagnostic, ok := attempt.Sampler.result.result(); ok {
			return diagnostic
		}
	}
	return attemptFailureDiagnostics{
		Source: errorSourceUpstream,
		Stage:  failureStageUpstreamResponse,
		Code:   stableHTTPErrorCode(statusCode, ""),
		Detail: fmt.Sprintf("upstream returned HTTP %d", statusCode),
	}
}

type executionResult struct {
	Response                    *http.Response
	Connection                  runtimeConnection
	RequestHeaders              map[string]string
	ResolvedTargetModelID       *string
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	AttemptCount                int
	Attempts                    []executionAttempt
	RouteReason                 gatewaycore.RouteReason
	// WinnerOrdinal is the launch ordinal of the selected response, or 0 when
	// no attempt produced a selectable response (all failed / zero launched).
	WinnerOrdinal int
}

type executionOutcome struct {
	TerminalAttempt           runtimeTerminalAttempt
	Connection                runtimeConnection
	RequestHeaders            map[string]string
	Response                  *http.Response
	Attempt                   executionAttempt
	Launched                  bool
	Skipped                   bool
	Err                       error
	AdmissionReason           string
	AdmissionState            *loadbalance.RuntimeConnectionState
	UnbannedRecord            *loadbalance.RuntimeConnectionState
	RetryDecision             gatewayrouting.RetryDecision
	FailoverEligible          bool
	Definitive                bool
	SuppressTransportFeedback bool
	FatalError                error
}

// runtimeAttemptLeaseBody keeps a connection's in-flight lease until the
// upstream response body reaches a terminal read or is explicitly closed.
// http.Client.Do returns after response headers, which is too early to release
// a concurrency slot for either streaming or non-streaming responses.
type runtimeAttemptLeaseBody struct {
	io.ReadCloser
	release     func()
	releaseOnce sync.Once
}

func (body *runtimeAttemptLeaseBody) Read(payload []byte) (int, error) {
	written, err := body.ReadCloser.Read(payload)
	if err != nil {
		body.releaseLease()
	}
	return written, err
}

func (body *runtimeAttemptLeaseBody) Close() error {
	err := body.ReadCloser.Close()
	body.releaseLease()
	return err
}

func (body *runtimeAttemptLeaseBody) releaseLease() {
	body.releaseOnce.Do(body.release)
}

type hedgedExecutionResult struct {
	Winner              *executionOutcome
	Attempts            []executionAttempt
	LaunchedAttempts    int
	AdmissionRejections int
	LastAdmissionReason string
	RouteReason         gatewaycore.RouteReason
	// LastError is the normalized failure classification label; it never
	// contains an upstream address.
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
	launchedAttempts int
	attempts         []executionAttempt
	// lastError is the normalized failure classification label; it never
	// contains an upstream address.
	lastError           string
	lastAdmissionReason string
	routeReason         gatewaycore.RouteReason
	admissionRejections int
	hedgeUsed           bool
	// nextLaunchOrdinal is the immutable 1-based launch ordinal to assign to
	// the next real upstream launch within this ingress.
	nextLaunchOrdinal int
	// lastLaunchedConnectionID and lastLaunchedTrigger track the previous
	// launch so retry_same_target vs failover can be classified from persisted
	// executor evidence, not inferred later from display fields.
	lastLaunchedConnectionID int
	lastLaunchedTrigger      string
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
	defaultProfile, snapshot, err := s.cache.LoadFreshDefaultRuntimePlan(ctx)
	if err != nil {
		return requestPlan{}, runtimeSnapshotDomainError(err)
	}
	plan, err := s.buildRequestPlanFromSnapshot(request.WithContext(ctx), rawBody, runtimeConfig, operationMatch, defaultProfile.ID, snapshot)
	if err != nil {
		return requestPlan{}, err
	}
	return plan, nil
}

func (s *Service) buildRequestPlanFromSnapshot(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	plan, err := s.buildRequestPlanFromSnapshotCore(request, rawBody, runtimeConfig, operationMatch, activeProfileID, snapshot)
	if err != nil {
		return requestPlan{}, err
	}
	return plan, nil
}

func (s *Service) buildProbeRequestPlanFromSnapshot(request *http.Request, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	return s.buildRequestPlanFromSnapshotCoreWithProbe(request, nil, runtimeConfig, operationMatch, activeProfileID, snapshot, true)
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

func resolveRequestedModel(input requestPlanningInput, operation resolvedRequestOperation) (runtimeModelRecord, error) {
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return runtimeModelRecord{}, err
	}
	requestedModel, found := routingPlan.requestedModelByID(operation.RequestedModelID)
	if !found {
		return runtimeModelRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", operation.RequestedModelID)}
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, requestedModel); err != nil {
		return runtimeModelRecord{}, err
	}
	return requestedModel, nil
}

func resolveRequestedModelByID(input requestPlanningInput, operation resolvedRequestOperation, requestedModelID string) (runtimeModelRecord, error) {
	trimmedRequestedModelID := strings.TrimSpace(requestedModelID)
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return runtimeModelRecord{}, err
	}
	requestedModel, found := routingPlan.requestedModelByID(trimmedRequestedModelID)
	if !found {
		return runtimeModelRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", trimmedRequestedModelID)}
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, requestedModel); err != nil {
		return runtimeModelRecord{}, err
	}
	return requestedModel, nil
}

func attachRuntimePlanningFailureTelemetry(err error, input requestPlanningInput, operation resolvedRequestOperation, requestedModel runtimeModelRecord) error {
	var runtimeErr *domainError
	if !errors.As(err, &runtimeErr) || runtimeErr == nil {
		return err
	}
	_, unsupportedWire := isRequestTranslationUnsupportedError(runtimeErr)
	if !unsupportedWire && runtimeErr.StatusCode != http.StatusServiceUnavailable {
		return err
	}
	generationParams := extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody)
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	var upstreamOperationName *string
	var upstreamRequestPath *string
	var operationTranslationMode *string
	if unsupportedWire {
		upstreamOperationName = stringPtr(runtimeUpstreamOperationName(operation.Match.Operation, TranslationModeNone))
		upstreamRequestPath = runtimeUpstreamRequestPath(operation.Match.Operation, TranslationModeNone, "")
		operationTranslationMode = runtimeTranslationModePointer(TranslationModeNone)
	}
	var resolvedTargetModelID *string
	if runtimeErr.ResolvedTargetModelID != nil && strings.TrimSpace(*runtimeErr.ResolvedTargetModelID) != "" {
		resolvedTargetModelID = cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	}
	runtimeErr.PlanningFailure = &runtimePlanningFailureTelemetry{
		ProfileID:                   input.ActiveProfileID,
		RequestedModelID:            requestedModel.ModelID,
		RequestedVendorID:           requestedModel.VendorID,
		RequestedVendorKey:          requestedModel.VendorKey,
		RequestedVendorName:         requestedModel.VendorName,
		APIFamily:                   requestedModel.APIFamily,
		RuntimeOperation:            operation.Match.Operation,
		UpstreamOperationName:       upstreamOperationName,
		RequestPath:                 input.Request.URL.Path,
		UpstreamRequestPath:         upstreamRequestPath,
		OperationTranslationMode:    operationTranslationMode,
		IsStreamingRequest:          requestWantsStreamForOperation(operation.Match.Operation, input.RawBody, input.Request.URL.Path),
		AuditEnabledAtRequest:       requestedModel.AuditEnabled,
		AuditCaptureBodiesAtRequest: requestedModel.AuditEnabled && requestedModel.AuditCaptureBodies,
		ReportCurrencySnapshot:      input.Snapshot.ReportCurrency,
		RequestGenerationParams:     generationParams,
		SelectedTerminalTargetID:    selectedTerminalTargetID,
	}
	runtimeErr.ResolvedTargetModelID = resolvedTargetModelID
	return err
}

func (s *Service) resolveRequestPlanTarget(input requestPlanningInput, operation resolvedRequestOperation, requestedModel runtimeModelRecord) (resolvedExecutionTarget, error) {
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	resolved, err := s.resolveExecutionTargetFromRoutingPlanWithOptions(input.ActiveProfileID, routingPlan, requestedModel, operation.Match.Operation, input.ReferenceNow)
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, resolved.TargetModel); err != nil {
		return resolvedExecutionTarget{}, err
	}
	if len(resolved.TerminalAttempts) == 0 {
		return resolvedExecutionTarget{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No eligible targets available for model '%s'.", operation.RequestedModelID)}
	}

	selectedTerminalTargetID := intPtr(resolved.TerminalAttempts[0].Connection.ID)
	return resolvedExecutionTarget{
		RequestedModel:           requestedModel,
		TargetModel:              resolved.TargetModel,
		SelectedTerminalTargetID: selectedTerminalTargetID,
		Connections:              resolved.Connections,
		TerminalAttempts:         resolved.TerminalAttempts,
		RuntimeStates:            resolved.RuntimeStates,
		Strategy:                 resolved.Strategy,
	}, nil
}

func buildPlannedUpstreamRequest(input requestPlanningInput, operation resolvedRequestOperation, attempt runtimeTerminalAttempt) (plannedUpstreamRequest, error) {
	if upstreamRequest, ok, err := buildOpenAITextPlannedUpstreamRequest(input, operation, attempt); ok || err != nil {
		return upstreamRequest, err
	}
	if upstreamRequest, ok, err := buildAnthropicPlannedUpstreamRequest(input, operation, attempt); ok || err != nil {
		return upstreamRequest, err
	}
	if upstreamRequest, ok, err := buildGeminiPlannedUpstreamRequest(input, operation, attempt); ok || err != nil {
		return upstreamRequest, err
	}
	effectiveRequestPath := input.Request.URL.Path
	upstreamBody := input.RawBody
	switch operation.Match.Operation.ModelBindingSource {
	case RuntimeOperationModelBindingPath:
		pathModelID := strings.TrimSpace(operation.Match.PathParams["model"])
		if pathModelID != "" && pathModelID != attempt.TargetModel.ModelID {
			effectiveRequestPath = rewriteModelInPath(input.Request.URL.Path, pathModelID, attempt.TargetModel.ModelID)
		}
	case RuntimeOperationModelBindingBody:
		if bodyModelID := extractModelFromBody(input.RawBody); bodyModelID != "" && bodyModelID != attempt.TargetModel.ModelID {
			upstreamBody = rewriteModelInBody(input.RawBody, attempt.TargetModel.ModelID)
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

func assembleRequestPlan(input requestPlanningInput, operation resolvedRequestOperation, target resolvedExecutionTarget) (requestPlan, error) {
	terminalAttempts, upstreamRequest, err := buildPlannedTerminalAttempts(input, operation, target.TerminalAttempts)
	if err != nil {
		return requestPlan{}, err
	}
	firstAttempt := terminalAttempts[0]
	connections := connectionsFromTerminalAttempts(terminalAttempts)
	return requestPlan{
		RequestedModelID:            operation.RequestedModelID,
		ResolvedTargetModelID:       stringPointerIfNotEmpty(firstAttempt.TargetModel.ModelID),
		ResolvedPricingModelID:      strings.TrimSpace(firstAttempt.TargetModel.ModelID),
		RequestedVendorID:           target.RequestedModel.VendorID,
		RequestedVendorKey:          target.RequestedModel.VendorKey,
		RequestedVendorName:         target.RequestedModel.VendorName,
		ProfileID:                   input.ActiveProfileID,
		APIFamily:                   firstAttempt.TargetModel.APIFamily,
		RuntimeOperation:            operation.Match.Operation,
		RuntimeOperationPathParams:  cloneStringMap(operation.Match.PathParams),
		AuditEnabledAtRequest:       firstAttempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: firstAttempt.AuditCaptureBodiesRequest,
		ReportCurrencySnapshot:      input.Snapshot.ReportCurrency,
		EffectiveRequestPath:        upstreamRequest.EffectiveRequestPath,
		RawRequestBody:              upstreamRequest.RawRequestBody,
		UpstreamBody:                upstreamRequest.UpstreamBody,
		IsStreamingRequest:          upstreamRequest.IsStreamingRequest,
		SelectedTerminalTargetID:    cloneRuntimeIntPointer(target.SelectedTerminalTargetID),
		TerminalAttempts:            terminalAttempts,
		Connections:                 connections,
		RuntimeStates:               target.RuntimeStates,
		BlocklistRules:              input.Snapshot.BlocklistRules,
		ClientHeaders:               upstreamRequest.ClientHeaders,
		FailoverStatusCodes:         firstAttempt.Strategy.FailoverStatusCodes(),
		Strategy:                    firstAttempt.Strategy,
		RequestGenerationParams:     upstreamRequest.RequestGenerationParams,
		HTTPClient:                  input.RuntimeConfig.HTTPClient,
	}, nil
}

func buildPlannedTerminalAttempts(input requestPlanningInput, operation resolvedRequestOperation, attempts []runtimeTerminalAttempt) ([]runtimeTerminalAttempt, plannedUpstreamRequest, error) {
	if len(attempts) == 0 {
		return nil, plannedUpstreamRequest{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No eligible targets available for model '%s'.", operation.RequestedModelID)}
	}
	plannedAttempts := make([]runtimeTerminalAttempt, 0, len(attempts))
	var firstUpstream plannedUpstreamRequest
	for index, attempt := range attempts {
		upstreamRequest, err := buildPlannedUpstreamRequest(input, operation, attempt)
		if err != nil {
			return nil, plannedUpstreamRequest{}, err
		}
		upstreamRequest, err = applyCustomRequestParametersOverlay(input, operation, upstreamRequest, attempt)
		if err != nil {
			return nil, plannedUpstreamRequest{}, err
		}
		planned := attempt
		planned.EffectiveRequestPath = upstreamRequest.EffectiveRequestPath
		planned.UpstreamBody = upstreamRequest.UpstreamBody
		planned.RequestGenerationParams = upstreamRequest.RequestGenerationParams
		planned.AuditEnabledAtRequest = attempt.TargetModel.AuditEnabled
		planned.AuditCaptureBodiesRequest = attempt.TargetModel.AuditEnabled && attempt.TargetModel.AuditCaptureBodies
		plannedAttempts = append(plannedAttempts, planned)
		if index == 0 {
			firstUpstream = upstreamRequest
		}
	}
	return plannedAttempts, firstUpstream, nil
}

// applyCustomRequestParametersOverlay applies the attempt Connection's custom
// request parameters as a top-level shallow overlay on the provider-native
// upstream body (after model/path rewrite). It is a no-op for unconfigured
// Connections and for the rawBody == nil Gemini probe phase. When a
// configuration exists, the ingress body must be a valid JSON object, the
// merged body must stay within the runtime JSON body limit, and the
// generation-parameter snapshot is re-extracted from the final effective
// body. All failures happen before admission, Ban Policy attempt counting,
// and provider transport.
func applyCustomRequestParametersOverlay(input requestPlanningInput, operation resolvedRequestOperation, upstreamRequest plannedUpstreamRequest, attempt runtimeTerminalAttempt) (plannedUpstreamRequest, error) {
	config := attempt.Connection.CustomRequestParameters
	if config == nil || config.IsEmpty() {
		return upstreamRequest, nil
	}
	if input.ProbePlanning {
		return upstreamRequest, nil
	}
	if !isJSONObjectBody(input.RawBody) {
		return plannedUpstreamRequest{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Request body must be a JSON object when custom request parameters are configured"}
	}
	merged, err := config.OverlayRequestBody(upstreamRequest.UpstreamBody)
	if err != nil {
		return plannedUpstreamRequest{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Request body must be a JSON object when custom request parameters are configured"}
	}
	if int64(len(merged)) > bodylimits.RuntimeJSONRequestBodyLimitBytes {
		return plannedUpstreamRequest{}, &domainError{
			StatusCode: http.StatusRequestEntityTooLarge,
			ErrorCode:  "request_body_too_large",
			Detail:     "Request body is too large after applying custom request parameters",
			Fields:     map[string]any{"limit_bytes": bodylimits.RuntimeJSONRequestBodyLimitBytes},
		}
	}
	upstreamRequest.UpstreamBody = merged
	upstreamRequest.RequestGenerationParams = extractBufferedRequestGenerationParams(operation.Match.Operation, merged)
	return upstreamRequest, nil
}

func isJSONObjectBody(raw []byte) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	return ok && delim == '{'
}

func connectionsFromTerminalAttempts(attempts []runtimeTerminalAttempt) []runtimeConnection {
	connections := make([]runtimeConnection, 0, len(attempts))
	for _, attempt := range attempts {
		connections = append(connections, attempt.Connection)
	}
	return connections
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
	var epoch sql.NullInt64
	err := tx.QueryRow(ctx, `SELECT user_settings.report_currency_code, user_settings.report_currency_symbol, reporting_currency_epochs.epoch
		FROM user_settings
		LEFT JOIN reporting_currency_epochs ON reporting_currency_epochs.id = user_settings.current_reporting_currency_epoch_id
		WHERE user_settings.profile_id = $1 ORDER BY user_settings.id ASC LIMIT 1`, profileID).Scan(&code, &symbol, &epoch)
	if err == nil {
		snapshot := runtimeReportCurrencySnapshot{Code: strings.TrimSpace(code), Symbol: strings.TrimSpace(symbol)}
		if epoch.Valid {
			snapshot.Epoch = int(epoch.Int64)
		}
		return snapshot, nil
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

func (s *Service) executeRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, bodySource *runtimeRequestBodySource) (executionResult, error) {
	state := newRequestExecutionState(plan)
	limits := requestExecutionLimitsForPlan(plan)
	terminalAttempts := plan.orderedTerminalAttempts()

	for index := 0; index < len(terminalAttempts); index++ {
		// Launch safety bound (Requests SPEC §4.6): at most 64 launched
		// upstream attempts per ingress. The executor checks before the next
		// launch and terminates with gateway 503 + typed
		// attempt_budget_exhausted; it never constructs a 65th upstream row.
		if state.launchedAttempts >= MaxLaunchedUpstreamAttempts {
			result, err := state.budgetExhaustedResult(plan)
			return result, err
		}
		if limits.remainingLaunchCapacity(state) <= 0 {
			break
		}
		// Fixed safety cap: never launch the 65th upstream attempt.
		if state.launchedAttempts >= maxUpstreamAttemptsPerIngress {
			return state.attemptBudgetExhaustedResult(plan)
		}
		if limits.shouldHedge(plan, state, index) {
			hedged, err := s.executeHedgedRequest(ctx, method, plan, requestQuery, index, limits.HedgePolicy, bodySource, &state)
			if err != nil {
				return executionResult{}, err
			}
			state.recordHedgedResult(hedged)
			if hedged.Winner != nil {
				return s.executionResultForHedgedWinner(ctx, plan, state, hedged.Winner), nil
			}
			index += hedged.ConsumedConnections - 1
			continue
		}

		lifecycle := runtimeAttemptLifecycle{LaunchOrdinal: state.nextLaunchOrdinal, AttemptTrigger: state.nextLaunchTrigger(plan, index, terminalAttempts[index])}
		outcome := s.executeSingleAttempt(ctx, method, plan, requestQuery, terminalAttempts[index], bodySource, lifecycle)
		result, done, err := s.handleSingleExecutionOutcome(ctx, plan, &state, outcome, index, limits.MaxAttempts)
		if err != nil {
			return executionResult{}, err
		}
		if done {
			return result, nil
		}
	}
	result, err := state.failureResult(plan)
	return result, err
}

func newRequestExecutionState(plan requestPlan) requestExecutionState {
	return requestExecutionState{
		attempts:          make([]executionAttempt, 0, len(plan.orderedTerminalAttempts())),
		routeReason:       runtimePlanRouteReason(plan),
		nextLaunchOrdinal: 1,
	}
}

// nextLaunchTrigger classifies the trigger for the next launch from persisted
// executor evidence: the first launch is `initial`; a later launch to the same
// connection is `retry_same_target`; a hedge launch is `hedge`; any other
// later launch is `failover`. The winner's entry lineage is classified at the
// launch site, never inferred from completion order. The immutable launch
// ordinal is stamped only when the attempt actually launches.
func (state *requestExecutionState) nextLaunchTrigger(plan requestPlan, index int, terminalAttempt runtimeTerminalAttempt) string {
	trigger := attemptTriggerInitial
	switch {
	case state.hedgeUsed:
		trigger = attemptTriggerHedge
	case state.launchedAttempts > 0:
		if state.lastLaunchedConnectionID == terminalAttempt.Connection.ID {
			trigger = attemptTriggerRetrySameTarget
		} else {
			trigger = attemptTriggerFailover
		}
	}
	return trigger
}

func runtimePlanRouteReason(plan requestPlan) gatewaycore.RouteReason {
	return gatewaycore.RouteReasonDirectMatch
}

func requestExecutionLimitsForPlan(plan requestPlan) requestExecutionLimits {
	hedgePolicy := plan.Strategy.HedgePolicy()
	maxAttempts := len(plan.orderedTerminalAttempts())
	return requestExecutionLimits{HedgePolicy: hedgePolicy, MaxAttempts: maxAttempts}
}

func (limits requestExecutionLimits) remainingLaunchCapacity(state requestExecutionState) int {
	return limits.MaxAttempts - state.launchedAttempts
}

func (limits requestExecutionLimits) shouldHedge(plan requestPlan, state requestExecutionState, index int) bool {
	return !state.hedgeUsed && limits.HedgePolicy.Enabled && limits.remainingLaunchCapacity(state) >= 2 && len(plan.orderedTerminalAttempts())-index >= 2
}

func runtimeAdmissionRouteReason(reason string) gatewaycore.RouteReason {
	switch strings.TrimSpace(reason) {
	case "qps_limit":
		return gatewaycore.RouteReasonQPSOverflow
	case "max_in_flight_stream", "max_in_flight_non_stream":
		return gatewaycore.RouteReasonConcurrencyOverflow
	default:
		return gatewaycore.RouteReasonPolicyReject
	}
}

func runtimeExecutionRouteReason(reason gatewaycore.RouteReason) gatewaycore.RouteReason {
	switch reason {
	case gatewaycore.RouteReasonModelRedirect,
		gatewaycore.RouteReasonUpstreamRedirect,
		gatewaycore.RouteReasonQPSOverflow,
		gatewaycore.RouteReasonRPMOverflow,
		gatewaycore.RouteReasonTPMOverflow,
		gatewaycore.RouteReasonIPMOverflow,
		gatewaycore.RouteReasonConcurrencyOverflow,
		gatewaycore.RouteReasonRetry429,
		gatewaycore.RouteReasonRetry5xx,
		gatewaycore.RouteReasonRetryHTTP,
		gatewaycore.RouteReasonRetryConnectTimeout,
		gatewaycore.RouteReasonRetryTransport,
		gatewaycore.RouteReasonCircuitOpenSkip,
		gatewaycore.RouteReasonNoHealthyUpstream,
		gatewaycore.RouteReasonPolicyReject:
		return reason
	default:
		return gatewaycore.RouteReasonDirectMatch
	}
}

func (state *requestExecutionState) recordHedgedResult(hedged hedgedExecutionResult) {
	state.hedgeUsed = true
	state.launchedAttempts += hedged.LaunchedAttempts
	state.attempts = append(state.attempts, hedged.Attempts...)
	for _, attempt := range hedged.Attempts {
		if attempt.LaunchOrdinal >= state.nextLaunchOrdinal {
			state.nextLaunchOrdinal = attempt.LaunchOrdinal + 1
		}
		if validateAttemptTrigger(attempt.AttemptTrigger) {
			state.lastLaunchedTrigger = attempt.AttemptTrigger
			state.lastLaunchedConnectionID = attempt.Connection.ID
		}
	}
	state.admissionRejections += hedged.AdmissionRejections
	if strings.TrimSpace(hedged.LastAdmissionReason) != "" {
		state.lastAdmissionReason = hedged.LastAdmissionReason
	}
	if hedged.RouteReason != "" {
		state.routeReason = runtimeExecutionRouteReason(hedged.RouteReason)
	}
	if strings.TrimSpace(hedged.LastError) != "" {
		state.lastError = hedged.LastError
	}
}

func (state *requestExecutionState) recordAdmissionRejection(reason string) {
	state.admissionRejections++
	state.lastAdmissionReason = reason
	state.routeReason = runtimeAdmissionRouteReason(reason)
}

func (state *requestExecutionState) recordRetry(reason gatewaycore.RouteReason) {
	state.routeReason = runtimeExecutionRouteReason(reason)
}

func (state *requestExecutionState) recordLaunchedAttempt(outcome executionOutcome) {
	state.launchedAttempts++
	if outcome.Attempt.LaunchOrdinal >= state.nextLaunchOrdinal {
		state.nextLaunchOrdinal = outcome.Attempt.LaunchOrdinal + 1
	}
	if validateAttemptTrigger(outcome.Attempt.AttemptTrigger) {
		state.lastLaunchedTrigger = outcome.Attempt.AttemptTrigger
		state.lastLaunchedConnectionID = outcome.Attempt.Connection.ID
	}
	state.attempts = append(state.attempts, outcome.Attempt)
}

func (state *requestExecutionState) result(plan requestPlan, outcome executionOutcome) executionResult {
	result := executionResult{
		Response:                    outcome.Response,
		Connection:                  outcome.Connection,
		RequestHeaders:              outcome.RequestHeaders,
		ResolvedTargetModelID:       stringPointerIfNotEmpty(outcome.TerminalAttempt.TargetModel.ModelID),
		AuditEnabledAtRequest:       outcome.TerminalAttempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: outcome.TerminalAttempt.AuditCaptureBodiesRequest,
		AttemptCount:                state.launchedAttempts,
		Attempts:                    state.attempts,
		RouteReason:                 runtimeExecutionRouteReason(state.routeReason),
	}
	if outcome.Launched {
		result.WinnerOrdinal = outcome.Attempt.LaunchOrdinal
	}
	return result
}

func (state *requestExecutionState) failureResult(plan requestPlan) (executionResult, error) {
	if len(plan.orderedTerminalAttempts()) == 0 {
		return executionResult{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No active connections available for model '%s'.", plan.RequestedModelID)}
	}
	if state.launchedAttempts == 0 && state.admissionRejections > 0 {
		routeReason := runtimeExecutionRouteReason(state.routeReason)
		detail := fmt.Sprintf("All connections rejected for model '%s' because admission limits are exhausted.", plan.RequestedModelID)
		if strings.TrimSpace(state.lastAdmissionReason) != "" {
			detail = fmt.Sprintf("All connections rejected for model '%s' because admission limit '%s' is exhausted.", plan.RequestedModelID, state.lastAdmissionReason)
		}
		result := executionResult{AttemptCount: state.launchedAttempts, Attempts: state.attempts, RouteReason: routeReason}
		return result, &domainError{
			StatusCode:               http.StatusServiceUnavailable,
			ErrorCode:                runtimeAdmissionExhaustedErrorCode,
			Detail:                   detail,
			Fields:                   map[string]any{"route_reason": string(routeReason)},
			ResolvedTargetModelID:    cloneRuntimeStringPointer(plan.ResolvedTargetModelID),
			SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
		}
	}
	lastFailure := strings.TrimSpace(state.lastError)
	if lastFailure == "" {
		lastFailure = "unknown_upstream_failure"
	}
	// All launched transport attempts failed: the executor preserves every
	// launched upstream row (trigger, target identity, duration, safe
	// transport detail) and materializes a finalized usage/event summary with
	// gateway 502 (Requests SPEC §4.6). Skipped candidates are never turned
	// into attempts; no synthetic "final 502 attempt" row is constructed.
	result := executionResult{
		AttemptCount: state.launchedAttempts,
		Attempts:     state.attempts,
		RouteReason:  runtimeExecutionRouteReason(state.routeReason),
	}
	return result, &domainError{
		StatusCode:               http.StatusBadGateway,
		ErrorCode:                "transport_error",
		Detail:                   fmt.Sprintf("All connections failed for model '%s'. Last failure: %s.", plan.RequestedModelID, lastFailure),
		Fields:                   map[string]any{"route_reason": string(result.RouteReason), "last_failure": lastFailure},
		ResolvedTargetModelID:    cloneRuntimeStringPointer(plan.ResolvedTargetModelID),
		SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
	}
}

// budgetExhaustedResult terminates the ingress when the 64-attempt launch
// safety bound is reached: gateway 503 + typed attempt_budget_exhausted with
// every already-launched attempt preserved. The finalized usage summary
// carries the terminal code; no 65th upstream row is ever constructed.
func (state *requestExecutionState) budgetExhaustedResult(plan requestPlan) (executionResult, error) {
	result := executionResult{
		AttemptCount: state.launchedAttempts,
		Attempts:     state.attempts,
		RouteReason:  runtimeExecutionRouteReason(state.routeReason),
	}
	return result, formatAttemptBudgetError(plan.RequestedModelID)
}

// attemptBudgetExhaustedResult terminates the ingress when the fixed 64-attempt
// launch cap is reached: gateway 503 with the typed attempt_budget_exhausted
// code, preserving the launched attempts already recorded.
func (state *requestExecutionState) attemptBudgetExhaustedResult(plan requestPlan) (executionResult, error) {
	routeReason := runtimeExecutionRouteReason(state.routeReason)
	result := executionResult{AttemptCount: state.launchedAttempts, Attempts: state.attempts, RouteReason: routeReason}
	return result, &domainError{
		StatusCode:               http.StatusServiceUnavailable,
		ErrorCode:                runtimeAttemptBudgetExhaustedErrorCode,
		Detail:                   fmt.Sprintf("Model '%s' exceeded the maximum of %d launched upstream attempts for a single ingress request.", plan.RequestedModelID, maxUpstreamAttemptsPerIngress),
		Fields:                   map[string]any{"route_reason": string(routeReason), "attempt_limit": maxUpstreamAttemptsPerIngress},
		ResolvedTargetModelID:    cloneRuntimeStringPointer(plan.ResolvedTargetModelID),
		SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
	}
}

func (s *Service) executionResultForHedgedWinner(ctx context.Context, plan requestPlan, state requestExecutionState, winner *executionOutcome) executionResult {
	if winner.Response.StatusCode >= 200 && winner.Response.StatusCode <= 299 && winner.Launched {
		s.recordRuntimeSuccess(ctx, plan, winner.Connection, winner.TerminalAttempt.Strategy, winner.Attempt.ResponseHeadersLatencyMS, winner.Attempt.CompletedAt)
	}
	return state.result(plan, *winner)
}

func (s *Service) handleSingleExecutionOutcome(ctx context.Context, plan requestPlan, state *requestExecutionState, outcome executionOutcome, index int, maxAttempts int) (executionResult, bool, error) {
	if outcome.FatalError != nil {
		return executionResult{}, false, outcome.FatalError
	}
	if outcome.UnbannedRecord != nil {
		s.recordRuntimeUnbanned(ctx, plan, outcome.Connection, *outcome.UnbannedRecord, s.nowUTC())
	}
	if outcome.Skipped {
		return executionResult{}, false, nil
	}
	if outcome.AdmissionReason != "" {
		state.recordAdmissionRejection(outcome.AdmissionReason)
		if outcome.AdmissionState != nil {
			s.recordRuntimeAdmissionRejected(ctx, plan, outcome.Connection, *outcome.AdmissionState, s.nowUTC())
		}
		return executionResult{}, false, nil
	}
	if outcome.Launched {
		state.recordLaunchedAttempt(outcome)
	}
	if outcome.Err != nil {
		state.lastError = upstreamFailureClass(outcome.Err)
		if outcome.Launched && !outcome.SuppressTransportFeedback {
			s.recordRuntimeTransportFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
		}
		if outcome.FailoverEligible && index < len(plan.orderedTerminalAttempts())-1 && state.launchedAttempts < maxAttempts {
			state.recordRetry(outcome.RetryDecision.Reason)
			return executionResult{}, false, nil
		}
		result, err := state.failureResult(plan)
		return result, true, err
	}
	if outcome.FailoverEligible && outcome.Launched {
		s.recordRuntimeFailoverHTTPFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
	}
	if outcome.FailoverEligible && index < len(plan.orderedTerminalAttempts())-1 && state.launchedAttempts < maxAttempts {
		state.lastError = safediag.HTTPFallbackCode(outcome.Response.StatusCode)
		state.recordRetry(outcome.RetryDecision.Reason)
		// Intermediate retry/failover: the bounded sampler owns the failed
		// response body; the next launch never waits for it.
		s.startFailedResponseSampler(ctx, plan, &outcome)
		if len(state.attempts) > 0 {
			state.attempts[len(state.attempts)-1].Sampler = outcome.Attempt.Sampler
		}
		if outcome.Attempt.Sampler == nil && outcome.Response != nil {
			// No sampler owns this body (sampler-cap hit or non-sampled path);
			// close it here so the next launch is not blocked by an open body.
			_ = outcome.Response.Body.Close()
		}
		return executionResult{}, false, nil
	}
	if outcome.Response.StatusCode >= 200 && outcome.Response.StatusCode <= 299 && outcome.Launched {
		s.recordRuntimeSuccess(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.ResponseHeadersLatencyMS, outcome.Attempt.CompletedAt)
	}
	return state.result(plan, outcome), true, nil
}

func (s *Service) executeHedgedRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, startIndex int, hedgePolicy loadbalance.RuntimeHedgePolicy, bodySource *runtimeRequestBodySource, state *requestExecutionState) (hedgedExecutionResult, error) {
	terminalAttempts := plan.orderedTerminalAttempts()
	totalCandidates := hedgePolicy.MaxAdditionalAttempts + 1
	remainingConnections := len(terminalAttempts) - startIndex
	if totalCandidates > remainingConnections {
		totalCandidates = remainingConnections
	}
	if totalCandidates > maxUpstreamAttemptsPerIngress {
		totalCandidates = maxUpstreamAttemptsPerIngress
	}
	if totalCandidates <= 0 {
		return hedgedExecutionResult{}, nil
	}

	results := make(chan hedgedAttemptResult, totalCandidates)
	cancelFuncs := make([]context.CancelCauseFunc, 0, totalCandidates)
	inFlight := 0
	launchedCandidates := 0
	nextOrder := 0
	// Immutable launch ordinals and hedge triggers are frozen at the launch
	// site (Observe SPEC §3.5); the first hedged launch carries the trigger
	// classified by the caller, later launches are hedge-triggered.
	firstTrigger := attemptTriggerHedge
	if state != nil && state.launchedAttempts == 0 {
		firstTrigger = attemptTriggerInitial
	}
	nextOrdinal := 0
	if state != nil {
		nextOrdinal = state.nextLaunchOrdinal
	}
	launchAttempt := func(order int) {
		attemptCtx, cancel := context.WithCancelCause(ctx)
		cancelFuncs = append(cancelFuncs, cancel)
		terminalAttempt := terminalAttempts[startIndex+order]
		trigger := attemptTriggerHedge
		if order == 0 {
			trigger = firstTrigger
		}
		lifecycle := runtimeAttemptLifecycle{LaunchOrdinal: nextOrdinal + order, AttemptTrigger: trigger}
		inFlight++
		launchedCandidates++
		go func() {
			results <- hedgedAttemptResult{Order: order, Outcome: s.executeSingleAttempt(attemptCtx, method, plan, requestQuery, terminalAttempt, bodySource, lifecycle)}
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
			if outcome.UnbannedRecord != nil {
				s.recordRuntimeUnbanned(ctx, plan, outcome.Connection, *outcome.UnbannedRecord, s.nowUTC())
			}
			if outcome.Skipped {
				continue
			}
			if outcome.AdmissionReason != "" {
				result.AdmissionRejections++
				result.LastAdmissionReason = outcome.AdmissionReason
				result.RouteReason = runtimeAdmissionRouteReason(outcome.AdmissionReason)
				if outcome.AdmissionState != nil {
					s.recordRuntimeAdmissionRejected(ctx, plan, outcome.Connection, *outcome.AdmissionState, s.nowUTC())
				}
				continue
			}
			if outcome.Launched {
				result.LaunchedAttempts++
			}
			if winner != nil {
				if outcome.Response != nil && outcome.Attempt.Sampler == nil {
					// Non-winner responses that are failover-eligible get the
					// bounded sampler (which exclusively owns the failed-response
					// body); other bodies are closed here.
					if outcome.FailoverEligible && outcome.Launched {
						s.startFailedResponseSampler(ctx, plan, &outcome)
					}
					if outcome.Attempt.Sampler == nil {
						_ = outcome.Response.Body.Close()
					}
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
					result.LastError = upstreamFailureClass(outcome.Err)
					s.recordRuntimeTransportFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
				}
				continue
			}
			if outcome.FailoverEligible {
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				result.LastError = safediag.HTTPFallbackCode(outcome.Response.StatusCode)
				s.recordRuntimeFailoverHTTPFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
				// Failover-eligible loser responses are sampled (sampler owns
				// close); non-sampled bodies are closed here.
				s.startFailedResponseSampler(ctx, plan, &outcome)
				if len(nonWinningAttempts) > 0 && outcome.Attempt.Sampler != nil {
					nonWinningAttempts[len(nonWinningAttempts)-1].Sampler = outcome.Attempt.Sampler
				}
				if outcome.Attempt.Sampler == nil && outcome.Response != nil {
					_ = outcome.Response.Body.Close()
				}
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

func (s *Service) executeSingleAttempt(ctx context.Context, method string, plan requestPlan, requestQuery string, terminalAttempt runtimeTerminalAttempt, bodySource *runtimeRequestBodySource, lifecycle runtimeAttemptLifecycle) executionOutcome {
	connection := terminalAttempt.Connection
	stripBodyDependentHeaders := connection.CustomRequestParameters != nil && !connection.CustomRequestParameters.IsEmpty()
	headers, err := s.buildUpstreamHeaders(connection, plan.APIFamily, plan.ClientHeaders, plan.BlocklistRules, stripBodyDependentHeaders)
	if err != nil {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, FatalError: err}
	}
	upstreamURL, err := buildUpstreamURL(connection.Endpoint.BaseURL, terminalAttempt.EffectiveRequestPath, requestQuery)
	if err != nil {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, FatalError: err}
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
		Policy:      terminalAttempt.Strategy.AdmissionPolicy(),
		IsStreaming: plan.IsStreamingRequest,
		ObservedAt:  s.nowUTC(),
	})
	if decision.Skipped {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, Skipped: true, UnbannedRecord: decision.UnbannedRecord}
	}
	if decision.AdmissionReason != "" {
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, AdmissionReason: decision.AdmissionReason, AdmissionState: decision.AdmissionState, UnbannedRecord: decision.UnbannedRecord}
	}
	var releaseOnce sync.Once
	releaseAttempt := func() {
		releaseOnce.Do(func() {
			s.runtimeState.FinishConnectionAttempt(decision.Handle, s.nowUTC())
		})
	}

	attemptStartedAt := s.nowUTC()
	attemptBodySource := bodySourceForTerminalAttempt(bodySource, terminalAttempt)
	response, headersLatencyMS, launched, requestErr := s.doUpstreamRequest(ctx, plan.HTTPClient, method, upstreamURL, headers, attemptBodySource)
	if response != nil && response.Body != nil {
		response.Body = &runtimeAttemptLeaseBody{ReadCloser: response.Body, release: releaseAttempt}
	} else {
		releaseAttempt()
	}
	if requestErr != nil && response != nil && response.Body != nil {
		// An errored transport response is never passed through or sampled.
		// Close it here so its lease and transport resources are released.
		_ = response.Body.Close()
	}
	auditHeaders := auditableAttemptHeaders(plan.ClientHeaders, headers)
	outcome := executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, RequestHeaders: auditHeaders, Response: response, Launched: launched, Err: requestErr, UnbannedRecord: decision.UnbannedRecord}
	if launched {
		attemptCompletedAt := s.nowUTC()
		outcome.Attempt = executionAttempt{
			Connection:                  connection,
			ResolvedTargetModelID:       strings.TrimSpace(terminalAttempt.TargetModel.ModelID),
			RequestURL:                  upstreamURL,
			RequestHeaders:              cloneStringMap(auditHeaders),
			RequestBody:                 append([]byte(nil), terminalAttempt.UpstreamBody...),
			StatusCode:                  http.StatusBadGateway,
			ResponseTimeMS:              durationMilliseconds(attemptCompletedAt.Sub(attemptStartedAt)),
			ResponseHeadersLatencyMS:    headersLatencyMS,
			CompletedAt:                 attemptCompletedAt,
			AuditEnabledAtRequest:       terminalAttempt.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: terminalAttempt.AuditCaptureBodiesRequest,
			UpstreamOperationName:       runtimeUpstreamOperationName(plan.RuntimeOperation, terminalAttempt.TranslationMode),
			UpstreamRequestPath:         dereferenceString(runtimeUpstreamRequestPath(plan.RuntimeOperation, terminalAttempt.TranslationMode, terminalAttempt.EffectiveRequestPath)),
			OperationTranslationMode:    normalizedRuntimeTranslationMode(terminalAttempt.TranslationMode),
			RequestGenerationParams:     terminalAttempt.RequestGenerationParams.clonePointer(),
			LaunchOrdinal:               lifecycle.LaunchOrdinal,
			AttemptTrigger:              lifecycle.AttemptTrigger,
			AttemptDurationMS:           durationMilliseconds(attemptCompletedAt.Sub(attemptStartedAt)),
			UpstreamRequestStarted:      true,
		}
		if response != nil {
			outcome.Attempt.StatusCode = response.StatusCode
			outcome.Attempt.ResponseHeaders = response.Header.Clone()
			outcome.Attempt.ResponseHeadersReceived = true
		}
		if s.isHedgeLoserCancellation(ctx, requestErr) {
			outcome.Attempt.StatusCode = hedgeCanceledAttemptStatusCode
			outcome.Attempt.AttemptResult = attemptResultCancelled
			outcome.SuppressTransportFeedback = true
		}
	}
	if requestErr != nil {
		requestContextErr := ctx.Err()
		outcome.RetryDecision = gatewayrouting.RetryPolicy{FailoverStatusCodes: terminalAttempt.Strategy.FailoverStatusCodes()}.ClassifyTransportError(requestContextErr, requestErr)
		outcome.FailoverEligible = outcome.RetryDecision.Retryable
		outcome.Definitive = !outcome.FailoverEligible
		if requestContextErr != nil {
			outcome.SuppressTransportFeedback = true
			if launched && outcome.Attempt.AttemptResult == "" {
				outcome.Attempt.StatusCode = hedgeCanceledAttemptStatusCode
				outcome.Attempt.AttemptResult = attemptResultCancelled
			}
		}
		if launched && outcome.Attempt.AttemptResult == "" {
			// Bounded, safe transport diagnostic formed at the failure site.
			diagnostic := safeTransportDiagnostic(requestErr)
			outcome.Attempt.Diagnostics = &diagnostic
			outcome.Attempt.AttemptResult = attemptResultTransportError
		}
		return outcome
	}
	outcome.RetryDecision = gatewayrouting.RetryPolicy{FailoverStatusCodes: terminalAttempt.Strategy.FailoverStatusCodes()}.ClassifyHTTPStatus(response.StatusCode)
	outcome.FailoverEligible = outcome.RetryDecision.Retryable
	outcome.Definitive = !outcome.FailoverEligible
	if launched && outcome.FailoverEligible {
		outcome.Attempt.AttemptResult = attemptResultHTTPError
	}
	return outcome
}

// startFailedResponseSampler begins the bounded failed-response sampler for an
// intermediate retry/failover non-2xx response. The sampler reads at most
// 32 KiB with a 50 ms deadline and exclusively owns the failed response body
// close. The next launch never waits for it; telemetry sealing uses whatever
// completed or falls back to generic status. It MUST only be called for
// responses that will NOT be passed through to the client (the final selected
// response keeps its body for passthrough).
func (s *Service) startFailedResponseSampler(ctx context.Context, plan requestPlan, outcome *executionOutcome) {
	if outcome == nil || outcome.Response == nil || outcome.Attempt.Sampler != nil {
		return
	}
	ingressID := runtimeIngressRequestIDFromContext(ctx)
	s.failedResponseSamplerOnce.Do(func() {
		s.failedResponseSamplers = &failedResponseSamplerLimiter{}
	})
	if !s.failedResponseSamplers.acquire(ingressID) {
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(outcome.Response.Header.Get("Content-Type")))
	sampler := newFailedResponseSampler(
		ingressID,
		outcome.Response,
		contentType,
		planBlocklistSensitiveRules(plan),
	)
	sampler.release = func() { s.failedResponseSamplers.release(ingressID) }
	outcome.Attempt.Sampler = sampler
	go sampler.run()
}

// planBlocklistSensitiveRules converts the request-time effective Header
// Blocklist into scrubber extra rules so diagnostics are at least as strict
// as the outbound forwarding policy.
func planBlocklistSensitiveRules(plan requestPlan) []safediag.SensitiveNameRule {
	rules := make([]safediag.SensitiveNameRule, 0, len(plan.BlocklistRules))
	for _, rule := range plan.BlocklistRules {
		rules = append(rules, safediag.SensitiveNameRule{MatchType: rule.MatchType, Pattern: rule.Pattern})
	}
	return rules
}

func bodySourceForTerminalAttempt(bodySource *runtimeRequestBodySource, terminalAttempt runtimeTerminalAttempt) *runtimeRequestBodySource {
	if bodySource != nil && bodySource.useStreamingBody {
		return bodySource
	}
	return newBufferedRuntimeRequestBodySource(terminalAttempt.UpstreamBody)
}

func (s *Service) isHedgeLoserCancellation(ctx context.Context, err error) bool {
	return err != nil && errors.Is(err, context.Canceled) && errors.Is(context.Cause(ctx), errHedgeLoserCanceled)
}

func (s *Service) recordRuntimeUnbanned(ctx context.Context, plan requestPlan, connection runtimeConnection, state loadbalance.RuntimeConnectionState, observedAt time.Time) {
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackUnbanned, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, State: state, ObservedAt: observedAt})
}

func (s *Service) recordRuntimeAdmissionRejected(ctx context.Context, plan requestPlan, connection runtimeConnection, state loadbalance.RuntimeConnectionState, observedAt time.Time) {
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackAdmissionRejected, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, State: state, ObservedAt: observedAt})
}

func (s *Service) recordRuntimeSuccess(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, responseHeadersLatencyMS int, completedAt time.Time) {
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeSuccess(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, responseHeadersLatencyMS, completedAt)
	if !transition.RecoveryEventEligible {
		return
	}
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackSuccessRecovery, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, CompletedAt: completedAt, ResponseTimeMS: responseHeadersLatencyMS})
}

func (s *Service) recordRuntimeFailoverHTTPFailure(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeFailoverHTTPFailure(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackFailoverHTTP, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "transient_http", CompletedAt: completedAt})
}

func (s *Service) recordRuntimeTransportFailure(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeTransportFailure(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackTransportFailure, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "connect_error", CompletedAt: completedAt})
}

func (s *Service) enqueueRuntimeFeedback(ctx context.Context, operationName string, event runtimeFeedbackEvent) {
	if s == nil {
		return
	}
	event.APIFamily = eventAPIFamily(event.APIFamily, operationName)
	if event.TraceContext.empty() {
		event.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	if s.feedbackPipeline != nil {
		s.feedbackPipeline.TryEnqueueContext(contextFromContext(ctx), event)
	}
}

func contextFromContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func (s *Service) doUpstreamRequest(ctx context.Context, client *http.Client, method string, upstreamURL string, headers map[string]string, bodySource *runtimeRequestBodySource) (*http.Response, int, bool, error) {
	if client == nil {
		client = s.httpClient
	}
	if client == nil {
		return nil, 0, false, fmt.Errorf("runtime HTTP client unavailable")
	}
	requestBody, contentLength, err := bodySource.Open()
	if err != nil {
		return nil, 0, false, fmt.Errorf("open upstream request body: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, upstreamURL, requestBody)
	if err != nil {
		if requestBody != nil {
			_ = requestBody.Close()
		}
		return nil, 0, false, fmt.Errorf("build upstream request: %w", err)
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
	headersReceivedAt := s.nowUTC()
	response, err := client.Do(request)
	headersLatencyMS := durationMilliseconds(s.nowUTC().Sub(headersReceivedAt))
	return response, headersLatencyMS, true, err
}

// auditableAttemptHeaders is what the audit trail records for one attempt: the
// headers the client actually sent, unioned with the headers Prism actually
// forwarded.
//
// Recording only the forwarded set made the outbound allowlist erase evidence:
// a client that leaked a Cookie left no trace at all, so the operator could not
// answer "did anything leak, and did Prism stop it?" — the one question this
// filter exists to make answerable. Client-only entries are the ones that were
// seen and dropped; forwarded-only entries are what Prism added (provider auth,
// connection custom headers). Sensitive values are replaced downstream by the
// audit scrubber, which already derives its rules from the same blocklist.
func auditableAttemptHeaders(clientHeaders map[string]string, forwardedHeaders map[string]string) map[string]string {
	audited := make(map[string]string, len(clientHeaders)+len(forwardedHeaders))
	maps.Copy(audited, clientHeaders)
	maps.Copy(audited, forwardedHeaders)
	return audited
}

func (s *Service) buildUpstreamHeaders(connection runtimeConnection, apiFamily string, clientHeaders map[string]string, rules []headerBlocklistRule, stripBodyDependentHeaders bool) (map[string]string, error) {
	_ = apiFamily
	compiledAuth := connection.UpstreamAuth
	if compiledAuth == nil {
		return nil, fmt.Errorf("runtime upstream auth snapshot unavailable for connection %d", connection.ID)
	}
	proxyControlledHeaders := compiledAuth.ControlledHeaderNames

	headers := forwardableClientHeaders(clientHeaders, proxyControlledHeaders)
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
	if stripBodyDependentHeaders {
		// The merged body is re-encoded uncompressed JSON with a freshly
		// computed Content-Length; stale body-dependent headers from the
		// client, provider auth extras, or Connection custom_headers must not
		// reach the captured upstream.
		for key := range sanitized {
			keyLower := strings.ToLower(strings.TrimSpace(key))
			if _, bodyDependent := bodyDependentHeaders[keyLower]; bodyDependent {
				delete(sanitized, key)
			}
		}
	}
	return sanitized, nil
}

// bodyDependentHeaders are invalidated whenever Prism re-encodes an upstream
// request body for custom request parameters: Content-Length is recomputed
// from the new body and the rest describe digests or encodings of the old
// bytes.
var bodyDependentHeaders = map[string]struct{}{
	"content-length":   {},
	"content-encoding": {},
	"content-md5":      {},
	"digest":           {},
	"content-digest":   {},
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
		if modelID := extractModelFromBody(rawBody); modelID != "" {
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

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableFloat64(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	resolved := value.Float64
	return &resolved
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}
