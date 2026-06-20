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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	"github.com/coachpo/prism/backend/internal/gateway/provider"
	gatewayrouting "github.com/coachpo/prism/backend/internal/gateway/routing"
	"github.com/coachpo/prism/backend/internal/providercompat"
)

const (
	openAIUpstreamOperationResponses                   = providercompat.OpenAIUpstreamOperationResponses
	openAIUpstreamOperationChatCompletions             = providercompat.OpenAIUpstreamOperationChatCompletions
	runtimeFacadeSelectionPolicyOrderedEligibleContext = "ordered_eligible_context"
	runtimeFacadeFallbackPolicySkipIneligibleTargets   = "skip_ineligible_targets"
	runtimeNestedFacadesNotSupportedDetail             = "nested facades are not supported"
	runtimeFacadeTerminalTargetsNotSupportedDetail     = "facade models must use model targets only"
	runtimeAdmissionExhaustedErrorCode                 = "admission_exhausted"
)

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

var translatedResponseUnsafeEntityHeaders = map[string]struct{}{
	"content-encoding": {},
	"content-length":   {},
	"content-md5":      {},
	"content-range":    {},
	"digest":           {},
	"etag":             {},
	"last-modified":    {},
}

var clientAuthHeaders = map[string]struct{}{
	"authorization":  {},
	"x-api-key":      {},
	"x-goog-api-key": {},
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
	FacadeEnabled         bool
	FacadeSelectionPolicy *string
	FacadeFallbackPolicy  *string
	OpenAIAcceptedFormat  *string
}

func validateRuntimePlanningSnapshotFacadePolicies(snapshot *planningSnapshot) error {
	if snapshot == nil || len(snapshot.ModelsByID) == 0 {
		return nil
	}
	modelIDs := make([]string, 0, len(snapshot.ModelsByID))
	for modelID := range snapshot.ModelsByID {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)
	for _, modelID := range modelIDs {
		if err := validateRuntimeModelFacadePolicies(snapshot.ModelsByID[modelID]); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeModelFacadePolicies(model runtimeModelRecord) error {
	if err := validateRuntimeFacadePolicyValues(model.FacadeSelectionPolicy, model.FacadeFallbackPolicy); err != nil {
		return invalidRuntimeFacadePolicyError(model.ModelID, err.Error())
	}
	if !model.FacadeEnabled {
		return nil
	}
	if model.FacadeSelectionPolicy == nil {
		return invalidRuntimeFacadePolicyError(model.ModelID, "facade_selection_policy is required when facade_enabled is true")
	}
	if model.FacadeFallbackPolicy == nil {
		return invalidRuntimeFacadePolicyError(model.ModelID, "facade_fallback_policy is required when facade_enabled is true")
	}
	return nil
}

func validateRuntimeFacadePolicyValues(selectionPolicy *string, fallbackPolicy *string) error {
	if selectionPolicy != nil && *selectionPolicy != runtimeFacadeSelectionPolicyOrderedEligibleContext {
		return fmt.Errorf("facade_selection_policy must be '%s'", runtimeFacadeSelectionPolicyOrderedEligibleContext)
	}
	if fallbackPolicy != nil && *fallbackPolicy != runtimeFacadeFallbackPolicySkipIneligibleTargets {
		return fmt.Errorf("facade_fallback_policy must be '%s'", runtimeFacadeFallbackPolicySkipIneligibleTargets)
	}
	return nil
}

func invalidRuntimeFacadePolicyError(modelID string, detail string) error {
	return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("Model '%s' has invalid persisted facade policy data: %s", modelID, detail)}
}

func isRuntimeExactOpenAIFacadeModel(model runtimeModelRecord) bool {
	return model.FacadeEnabled && modelrouting.SameAPIFamily(model.APIFamily, "openai") && model.FacadeSelectionPolicy != nil && *model.FacadeSelectionPolicy == runtimeFacadeSelectionPolicyOrderedEligibleContext
}

func runtimeFacadeSelectionStrategy() loadbalance.RuntimeStrategy {
	return loadbalance.RuntimeStrategy{LegacyStrategyType: stringPtr(runtimeFacadeSelectionPolicyOrderedEligibleContext)}
}

func nestedRuntimeFacadeTargetError() error {
	return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: runtimeNestedFacadesNotSupportedDetail}
}

func runtimeFacadeTerminalTargetError() error {
	return &domainError{StatusCode: http.StatusServiceUnavailable, Detail: runtimeFacadeTerminalTargetsNotSupportedDetail}
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
	ID                                   int
	ProfileID                            int
	APIFamily                            string
	ModelConfigID                        int
	EndpointID                           int
	Priority                             int
	QPSLimit                             *int
	MaxInFlightNonStream                 *int
	MaxInFlightStream                    *int
	Name                                 *string
	AuthType                             *string
	EncryptedEndpointAPIKey              string
	CustomHeaders                        map[string]any
	PricingTemplateID                    *int
	PricingTemplateSnapshot              *runtimePricingTemplateSnapshot
	ContextWindowTokens                  *int
	DefaultOutputTokenReserve            int
	MaxContextUtilization                float64
	PreferredContextUtilizationThreshold *float64
	OpenAIProbeEndpointVariant           *string
	OpenAITextCapability                 *string
	EndpointFXSnapshot                   *runtimeEndpointFXSnapshot
	UpstreamAuth                         *runtimeConnectionUpstreamAuthSnapshot
	Endpoint                             runtimeEndpoint
}

const (
	runtimeRoutingSkipReasonEstimatedContextExceedsUsableWindow = "estimated_context_exceeds_usable_window"
	runtimeRoutingSkipReasonUsableContextWindowUnavailable      = "usable_context_window_unavailable"
	runtimeContextBandPreferred                                 = "preferred"
	runtimeContextBandDiscretionary                             = "discretionary"
	runtimeContextBandIneligible                                = "ineligible"
)

type runtimeSkippedTerminalTarget struct {
	TerminalTargetID            *int
	EndpointID                  *int
	ContextBand                 *string
	Reason                      string
	UsableContextWindowTokens   *int
	EstimatedTotalContextTokens *int
}

type runtimeFacadeExclusionReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type runtimeFacadeSelectionDecision struct {
	FacadeModelID         string                         `json:"facade_model_id"`
	SelectedTargetModelID *string                        `json:"selected_target_model_id,omitempty"`
	ExclusionReasons      []runtimeFacadeExclusionReason `json:"exclusion_reasons,omitempty"`
	ExclusionSummary      *string                        `json:"exclusion_summary,omitempty"`
}

type runtimeTranslationLossDecision struct {
	Lossy         bool     `json:"lossy"`
	Direction     string   `json:"direction"`
	DroppedFields []string `json:"dropped_fields,omitempty"`
	MappedFields  []string `json:"mapped_fields,omitempty"`
}

func cloneRuntimeTranslationLossDecision(source *runtimeTranslationLossDecision) *runtimeTranslationLossDecision {
	if source == nil || !source.Lossy {
		return nil
	}
	return &runtimeTranslationLossDecision{
		Lossy:         source.Lossy,
		Direction:     strings.TrimSpace(source.Direction),
		DroppedFields: cloneRuntimeStringList(source.DroppedFields),
		MappedFields:  cloneRuntimeStringList(source.MappedFields),
	}
}

func cloneRuntimeStringList(source []string) []string {
	if len(source) == 0 {
		return nil
	}
	cloned := make([]string, 0, len(source))
	for _, value := range source {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cloned = append(cloned, trimmed)
		}
	}
	return cloned
}

func runtimeTranslationLossDecisionFromProvider(loss *provider.TranslationLoss, mode TranslationMode) *runtimeTranslationLossDecision {
	if loss == nil {
		return nil
	}
	dropped := cloneRuntimeStringList(loss.DroppedFields)
	mapped := cloneRuntimeStringList(loss.MappedFields)
	if len(dropped) == 0 && len(mapped) == 0 {
		return nil
	}
	direction := runtimeTranslationLossDirection(mode)
	if direction == "" {
		return nil
	}
	return &runtimeTranslationLossDecision{Lossy: true, Direction: direction, DroppedFields: dropped, MappedFields: mapped}
}

func runtimeTranslationLossDirection(mode TranslationMode) string {
	switch normalizedRuntimeTranslationMode(mode) {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return "responses_to_chat"
	case TranslationModeOpenAIChatCompletionsToResponses:
		return "chat_to_responses"
	default:
		return ""
	}
}

func cloneRuntimeFacadeSelectionDecision(source *runtimeFacadeSelectionDecision) *runtimeFacadeSelectionDecision {
	if source == nil {
		return nil
	}
	cloned := &runtimeFacadeSelectionDecision{
		FacadeModelID:         source.FacadeModelID,
		SelectedTargetModelID: cloneRuntimeStringPointer(source.SelectedTargetModelID),
		ExclusionReasons:      cloneRuntimeFacadeExclusionReasons(source.ExclusionReasons),
		ExclusionSummary:      cloneRuntimeStringPointer(source.ExclusionSummary),
	}
	if cloned.FacadeModelID == "" {
		cloned.FacadeModelID = source.FacadeModelID
	}
	return cloned
}

func cloneRuntimeFacadeExclusionReasons(source []runtimeFacadeExclusionReason) []runtimeFacadeExclusionReason {
	if len(source) == 0 {
		return nil
	}
	cloned := make([]runtimeFacadeExclusionReason, 0, len(source))
	for _, item := range source {
		cloned = append(cloned, runtimeFacadeExclusionReason{Reason: item.Reason, Count: item.Count})
	}
	return cloned
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

func runtimeContextBandPointer(band runtimeContextEligibilityBand) *string {
	switch band {
	case runtimeContextEligibilityBandPreferred:
		return stringPtr(runtimeContextBandPreferred)
	case runtimeContextEligibilityBandDiscretionary:
		return stringPtr(runtimeContextBandDiscretionary)
	case runtimeContextEligibilityBandIneligible:
		return stringPtr(runtimeContextBandIneligible)
	default:
		return nil
	}
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

type runtimeFinalResponseTranslationDirection string

const (
	runtimeFinalResponseTranslationDirectionNone                          runtimeFinalResponseTranslationDirection = "none"
	runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient runtimeFinalResponseTranslationDirection = "responses_upstream_to_chat_client"
	runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient runtimeFinalResponseTranslationDirection = "chat_upstream_to_responses_client"
)

func normalizedRuntimeFinalResponseTranslationDirection(direction runtimeFinalResponseTranslationDirection) runtimeFinalResponseTranslationDirection {
	if strings.TrimSpace(string(direction)) == "" {
		return runtimeFinalResponseTranslationDirectionNone
	}
	return direction
}

func (direction runtimeFinalResponseTranslationDirection) requiresTranslation() bool {
	return normalizedRuntimeFinalResponseTranslationDirection(direction) != runtimeFinalResponseTranslationDirectionNone
}

func runtimeFinalResponseTranslationDirectionFromMode(mode TranslationMode) runtimeFinalResponseTranslationDirection {
	switch normalizedRuntimeTranslationMode(mode) {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient
	case TranslationModeOpenAIChatCompletionsToResponses:
		return runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient
	default:
		return runtimeFinalResponseTranslationDirectionNone
	}
}

func runtimeTranslationModeForFinalResponseDirection(direction runtimeFinalResponseTranslationDirection) (TranslationMode, error) {
	switch normalizedRuntimeFinalResponseTranslationDirection(direction) {
	case runtimeFinalResponseTranslationDirectionNone:
		return TranslationModeNone, nil
	case runtimeFinalResponseTranslationDirectionResponsesUpstreamToChatClient:
		return TranslationModeOpenAIChatCompletionsToResponses, nil
	case runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient:
		return TranslationModeOpenAIResponsesToChatCompletions, nil
	default:
		return TranslationModeNone, fmt.Errorf("unsupported final response translation direction %q", direction)
	}
}

func runtimeUpstreamOperationName(operation RuntimeOperation, mode TranslationMode) string {
	switch normalizedRuntimeTranslationMode(mode) {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return openAIUpstreamOperationChatCompletions
	case TranslationModeOpenAIChatCompletionsToResponses:
		return openAIUpstreamOperationResponses
	default:
		return strings.TrimSpace(operation.Name)
	}
}

func runtimeUpstreamRequestPathTemplate(operation RuntimeOperation, mode TranslationMode) string {
	switch normalizedRuntimeTranslationMode(mode) {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return "/v1/chat/completions"
	case TranslationModeOpenAIChatCompletionsToResponses:
		return "/v1/responses"
	default:
		return strings.TrimSpace(operation.PathTemplate)
	}
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

func finalResponseTranslationMetadataFromAttempt(operation RuntimeOperation, attempt runtimeTerminalAttempt, requestedModelID string, selectedTerminalTargetID int) *runtimeFinalResponseTranslationMetadata {
	mode := normalizedRuntimeTranslationMode(attempt.TranslationMode)
	return &runtimeFinalResponseTranslationMetadata{
		TranslationMode:              mode,
		RequestedModelID:             strings.TrimSpace(requestedModelID),
		ClientOperationName:          strings.TrimSpace(operation.Name),
		SelectedTerminalTargetID:     intPtr(selectedTerminalTargetID),
		UpstreamOperationName:        runtimeUpstreamOperationName(operation, mode),
		UpstreamRequestPath:          dereferenceString(runtimeUpstreamRequestPath(operation, mode, attempt.EffectiveRequestPath)),
		ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionFromMode(mode),
	}
}

func cloneRuntimeFinalResponseTranslationMetadata(source *runtimeFinalResponseTranslationMetadata) *runtimeFinalResponseTranslationMetadata {
	if source == nil {
		return nil
	}
	return &runtimeFinalResponseTranslationMetadata{
		TranslationMode:              normalizedRuntimeTranslationMode(source.TranslationMode),
		RequestedModelID:             strings.TrimSpace(source.RequestedModelID),
		ClientOperationName:          strings.TrimSpace(source.ClientOperationName),
		SelectedTerminalTargetID:     cloneRuntimeIntPointer(source.SelectedTerminalTargetID),
		UpstreamOperationName:        strings.TrimSpace(source.UpstreamOperationName),
		UpstreamRequestPath:          strings.TrimSpace(source.UpstreamRequestPath),
		ResponseTranslationDirection: normalizedRuntimeFinalResponseTranslationDirection(source.ResponseTranslationDirection),
	}
}

type headerBlocklistRule struct {
	MatchType string
	Pattern   string
}

type requestPlan struct {
	RequestedModelID                          string
	ResolvedTargetModelID                     *string
	ResolvedPricingModelID                    string
	RequestedVendorID                         *int
	RequestedVendorKey                        *string
	RequestedVendorName                       *string
	ProfileID                                 int
	APIFamily                                 string
	RuntimeOperation                          RuntimeOperation
	RuntimeOperationPathParams                map[string]string
	AuditEnabledAtRequest                     bool
	AuditCaptureBodiesAtRequest               bool
	ReportCurrencySnapshot                    runtimeReportCurrencySnapshot
	EffectiveRequestPath                      string
	RawRequestBody                            []byte
	UpstreamBody                              []byte
	IsStreamingRequest                        bool
	SelectedTerminalTargetID                  *int
	TerminalAttempts                          []runtimeTerminalAttempt
	Connections                               []runtimeConnection
	RuntimeStates                             map[int]loadbalance.RuntimeConnectionState
	BlocklistRules                            []headerBlocklistRule
	ClientHeaders                             map[string]string
	FailoverStatusCodes                       []int
	Strategy                                  loadbalance.RuntimeStrategy
	RequestGenerationParams                   requestGenerationParamsSnapshot
	RequestContextEstimation                  *requestContextEstimation
	RequestContextEstimationUnavailableReason *string
	RequestGenerationSnapshot                 func() requestGenerationParamsSnapshot
	HTTPClient                                *http.Client
}

func (plan requestPlan) requiresReplayableRequestBody() bool {
	return len(plan.orderedTerminalAttempts()) > 1
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
	Request                            *http.Request
	RawBody                            []byte
	RuntimeConfig                      RuntimeProxyConfigSnapshot
	OperationMatch                     RuntimeOperationMatch
	ActiveProfileID                    int
	Snapshot                           *planningSnapshot
	RoutingPlan                        *runtimeRoutingPlan
	AllowMissingContextEstimation      bool
	ContextEstimationUnavailableReason *string
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
	TranslationLoss         *runtimeTranslationLossDecision
}

type runtimeTerminalAttempt struct {
	TargetModel               runtimeModelRecord
	Connection                runtimeConnection
	Strategy                  loadbalance.RuntimeStrategy
	TranslationMode           TranslationMode
	EffectiveRequestPath      string
	UpstreamBody              []byte
	AuditEnabledAtRequest     bool
	AuditCaptureBodiesRequest bool
	TranslationLoss           *runtimeTranslationLossDecision
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
	CompletedAt                 time.Time
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	UpstreamOperationName       string
	UpstreamRequestPath         string
	OperationTranslationMode    TranslationMode
}

type runtimeFinalResponseTranslationMetadata struct {
	TranslationMode              TranslationMode
	RequestedModelID             string
	ClientOperationName          string
	SelectedTerminalTargetID     *int
	UpstreamOperationName        string
	UpstreamRequestPath          string
	ResponseTranslationDirection runtimeFinalResponseTranslationDirection
}

type executionResult struct {
	Response                    *http.Response
	Connection                  runtimeConnection
	RequestHeaders              map[string]string
	ResolvedTargetModelID       *string
	AuditEnabledAtRequest       bool
	AuditCaptureBodiesAtRequest bool
	FinalResponseTranslation    *runtimeFinalResponseTranslationMetadata
	AttemptCount                int
	Attempts                    []executionAttempt
	RouteReason                 gatewaycore.RouteReason
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

type hedgedExecutionResult struct {
	Winner              *executionOutcome
	Attempts            []executionAttempt
	LaunchedAttempts    int
	AdmissionRejections int
	LastAdmissionReason string
	RouteReason         gatewaycore.RouteReason
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
	routeReason         gatewaycore.RouteReason
	admissionRejections int
	hedgeUsed           bool
}

var errHedgeLoserCanceled = errors.New("hedge loser canceled")

const hedgeCanceledAttemptStatusCode = 499

func (s *Service) buildRequestPlan(ctx context.Context, request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch) (requestPlan, error) {
	ctx, span := startRuntimeSpan(ctx, "request.plan", runtimeTraceOperationAttributes(operationMatch.Operation)...)
	defer span.End()
	operationMatch, err := validateResolvedRuntimeOperation(operationMatch, request.Method, request.URL.Path)
	if err != nil {
		runtimeTraceMarkError(span, "request_plan_failed")
		return requestPlan{}, err
	}
	span.SetAttributes(runtimeTraceOperationAttributes(operationMatch.Operation)...)
	if s.cache == nil {
		runtimeTraceMarkError(span, "request_plan_failed")
		return requestPlan{}, runtimeSnapshotDomainError(ErrPublishedRuntimeSnapshotUnavailable)
	}
	activeProfile, snapshot, err := s.cache.LoadFreshActiveRuntimePlan(ctx)
	if err != nil {
		runtimeTraceMarkError(span, "request_plan_failed")
		return requestPlan{}, runtimeSnapshotDomainError(err)
	}
	plan, err := s.buildRequestPlanFromSnapshot(request.WithContext(ctx), rawBody, runtimeConfig, operationMatch, activeProfile.ID, snapshot)
	if err != nil {
		runtimeTraceMarkError(span, "request_plan_failed")
		return requestPlan{}, err
	}
	span.SetAttributes(runtimeTracePlanAttributes(plan)...)
	return plan, nil
}

func (s *Service) buildRequestPlanFromSnapshot(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	ctx, span := startRuntimeSpan(request.Context(), "request.plan", runtimeTraceOperationAttributes(operationMatch.Operation)...)
	defer span.End()
	request = request.WithContext(ctx)

	plan, err := s.buildRequestPlanFromSnapshotCore(request, rawBody, runtimeConfig, operationMatch, activeProfileID, snapshot)
	if err != nil {
		runtimeTraceMarkError(span, "request_plan_failed")
		return requestPlan{}, err
	}
	span.SetAttributes(runtimeTracePlanAttributes(plan)...)
	return plan, nil
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
	if err := validateRuntimeModelFacadePolicies(requestedModel); err != nil {
		return runtimeModelRecord{}, err
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
	if err := validateRuntimeModelFacadePolicies(requestedModel); err != nil {
		return runtimeModelRecord{}, err
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
	if runtimeErr.ErrorCode != contextWindowExceededErrorCode && runtimeErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode {
		if runtimeErr.StatusCode != http.StatusServiceUnavailable {
			return err
		}
	}
	generationParams := extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody)
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	translationMode := TranslationModeNone
	if fieldValue, ok := runtimeErr.Fields["translation_mode"].(string); ok && strings.TrimSpace(fieldValue) != "" {
		translationMode = normalizedRuntimeTranslationMode(TranslationMode(strings.TrimSpace(fieldValue)))
	}
	var upstreamOperationName *string
	var upstreamRequestPath *string
	var operationTranslationMode *string
	if runtimeErr.ErrorCode == openAIRequestTranslationUnsupportedErrorCode {
		upstreamOperationName = stringPtr(runtimeUpstreamOperationName(operation.Match.Operation, translationMode))
		upstreamRequestPath = runtimeUpstreamRequestPath(operation.Match.Operation, translationMode, "")
		operationTranslationMode = runtimeTranslationModePointer(translationMode)
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

func (s *Service) resolveRequestPlanTarget(input requestPlanningInput, operation resolvedRequestOperation, requestedModel runtimeModelRecord, contextEstimation *requestContextEstimation) (resolvedExecutionTarget, error) {
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	resolved, err := s.resolveExecutionTargetFromRoutingPlanWithOptions(input.ActiveProfileID, routingPlan, requestedModel, operation.Match.Operation, input.RawBody, contextEstimation, input.AllowMissingContextEstimation, s.nowUTC())
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
	if upstreamRequest, ok, err := buildOpenAIImagePlannedUpstreamRequest(input, operation, attempt); ok || err != nil {
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
	var translationLoss *runtimeTranslationLossDecision
	switch attempt.TranslationMode {
	case "", TranslationModeNone:
		switch operation.Match.Operation.ModelBindingSource {
		case RuntimeOperationModelBindingPath:
			pathModelID := strings.TrimSpace(operation.Match.PathParams["model"])
			if pathModelID != "" && pathModelID != attempt.TargetModel.ModelID {
				effectiveRequestPath = rewriteModelInPath(input.Request.URL.Path, pathModelID, attempt.TargetModel.ModelID)
			}
		case RuntimeOperationModelBindingBody:
			if bodyModelID := extractModelFromBodyForOperation(input.RawBody, operation.ContentType, operation.Match.Operation); bodyModelID != "" && bodyModelID != attempt.TargetModel.ModelID {
				upstreamBody = rewriteModelInBodyForOperation(input.RawBody, operation.ContentType, operation.Match.Operation, attempt.TargetModel.ModelID)
			}
		default:
			return plannedUpstreamRequest{}, unsupportedOperationModelBindingError(operation.Match.Operation)
		}
	default:
		translatedPath, translatedBody, loss, err := defaultCodingAgentFormatBridge().TranslateRequestWithLoss(input.RawBody, attempt.TranslationMode, attempt.TargetModel.ModelID)
		if err != nil {
			return plannedUpstreamRequest{}, err
		}
		effectiveRequestPath = translatedPath
		upstreamBody = translatedBody
		translationLoss = loss
	}

	return plannedUpstreamRequest{
		EffectiveRequestPath:    effectiveRequestPath,
		RawRequestBody:          input.RawBody,
		UpstreamBody:            upstreamBody,
		IsStreamingRequest:      requestWantsStreamForOperation(operation.Match.Operation, input.RawBody, effectiveRequestPath),
		ClientHeaders:           flattenHeaders(input.Request.Header),
		RequestGenerationParams: extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody),
		TranslationLoss:         translationLoss,
	}, nil
}

func assembleRequestPlan(input requestPlanningInput, operation resolvedRequestOperation, target resolvedExecutionTarget, contextEstimation *requestContextEstimation) (requestPlan, error) {
	terminalAttempts, upstreamRequest, err := buildPlannedTerminalAttempts(input, operation, target.TerminalAttempts)
	if err != nil {
		return requestPlan{}, err
	}
	firstAttempt := terminalAttempts[0]
	connections := connectionsFromTerminalAttempts(terminalAttempts)
	return requestPlan{
		RequestedModelID:                          operation.RequestedModelID,
		ResolvedTargetModelID:                     stringPointerIfNotEmpty(firstAttempt.TargetModel.ModelID),
		ResolvedPricingModelID:                    strings.TrimSpace(firstAttempt.TargetModel.ModelID),
		RequestedVendorID:                         target.RequestedModel.VendorID,
		RequestedVendorKey:                        target.RequestedModel.VendorKey,
		RequestedVendorName:                       target.RequestedModel.VendorName,
		ProfileID:                                 input.ActiveProfileID,
		APIFamily:                                 firstAttempt.TargetModel.APIFamily,
		RuntimeOperation:                          operation.Match.Operation,
		RuntimeOperationPathParams:                cloneStringMap(operation.Match.PathParams),
		AuditEnabledAtRequest:                     firstAttempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:               firstAttempt.AuditCaptureBodiesRequest,
		ReportCurrencySnapshot:                    input.Snapshot.ReportCurrency,
		EffectiveRequestPath:                      upstreamRequest.EffectiveRequestPath,
		RawRequestBody:                            upstreamRequest.RawRequestBody,
		UpstreamBody:                              upstreamRequest.UpstreamBody,
		IsStreamingRequest:                        upstreamRequest.IsStreamingRequest,
		SelectedTerminalTargetID:                  cloneRuntimeIntPointer(target.SelectedTerminalTargetID),
		TerminalAttempts:                          terminalAttempts,
		Connections:                               connections,
		RuntimeStates:                             target.RuntimeStates,
		BlocklistRules:                            input.Snapshot.BlocklistRules,
		ClientHeaders:                             upstreamRequest.ClientHeaders,
		FailoverStatusCodes:                       firstAttempt.Strategy.FailoverStatusCodes(),
		Strategy:                                  firstAttempt.Strategy,
		RequestGenerationParams:                   upstreamRequest.RequestGenerationParams,
		RequestContextEstimation:                  contextEstimation,
		RequestContextEstimationUnavailableReason: cloneRuntimeStringPointer(input.ContextEstimationUnavailableReason),
		HTTPClient:                                input.RuntimeConfig.HTTPClient,
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
		planned := attempt
		planned.EffectiveRequestPath = upstreamRequest.EffectiveRequestPath
		planned.UpstreamBody = upstreamRequest.UpstreamBody
		planned.AuditEnabledAtRequest = attempt.TargetModel.AuditEnabled
		planned.AuditCaptureBodiesRequest = attempt.TargetModel.AuditEnabled && attempt.TargetModel.AuditCaptureBodies
		planned.TranslationLoss = cloneRuntimeTranslationLossDecision(upstreamRequest.TranslationLoss)
		plannedAttempts = append(plannedAttempts, planned)
		if index == 0 {
			firstUpstream = upstreamRequest
		}
	}
	return plannedAttempts, firstUpstream, nil
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

func (s *Service) executeRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, bodySource *runtimeRequestBodySource) (executionResult, error) {
	ctx, span := startRuntimeSpan(ctx, "request.execute", runtimeTracePlanAttributes(plan)...)
	defer span.End()
	state := newRequestExecutionState(plan)
	limits := requestExecutionLimitsForPlan(plan)
	terminalAttempts := plan.orderedTerminalAttempts()

	for index := 0; index < len(terminalAttempts); index++ {
		if limits.remainingLaunchCapacity(state) <= 0 {
			break
		}
		if limits.shouldHedge(plan, state, index) {
			hedged, err := s.executeHedgedRequest(ctx, method, plan, requestQuery, index, limits.HedgePolicy, bodySource)
			if err != nil {
				runtimeTraceMarkError(span, "runtime_execute_failed")
				return executionResult{}, err
			}
			state.recordHedgedResult(hedged)
			if hedged.Winner != nil {
				runtimeTraceSetAttemptCount(span, state.launchedAttempts)
				return s.executionResultForHedgedWinner(ctx, plan, state, hedged.Winner), nil
			}
			index += hedged.ConsumedConnections - 1
			continue
		}

		outcome := s.executeSingleAttempt(ctx, method, plan, requestQuery, terminalAttempts[index], bodySource)
		result, done, err := s.handleSingleExecutionOutcome(ctx, plan, &state, outcome, index, limits.MaxAttempts)
		if err != nil {
			runtimeTraceMarkError(span, "runtime_execute_failed")
			return executionResult{}, err
		}
		if done {
			runtimeTraceSetAttemptCount(span, state.launchedAttempts)
			return result, nil
		}
	}
	result, err := state.failureResult(plan)
	if err != nil {
		runtimeTraceMarkError(span, "runtime_execute_failed")
	}
	runtimeTraceSetAttemptCount(span, state.launchedAttempts)
	return result, err
}

func newRequestExecutionState(plan requestPlan) requestExecutionState {
	return requestExecutionState{attempts: make([]executionAttempt, 0, len(plan.orderedTerminalAttempts())), routeReason: runtimePlanRouteReason(plan)}
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
		gatewaycore.RouteReasonRetryConnectTimeout,
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
	state.attempts = append(state.attempts, outcome.Attempt)
}

func (state *requestExecutionState) result(plan requestPlan, outcome executionOutcome) executionResult {
	return executionResult{
		Response:                    outcome.Response,
		Connection:                  outcome.Connection,
		RequestHeaders:              outcome.RequestHeaders,
		ResolvedTargetModelID:       stringPointerIfNotEmpty(outcome.TerminalAttempt.TargetModel.ModelID),
		AuditEnabledAtRequest:       outcome.TerminalAttempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: outcome.TerminalAttempt.AuditCaptureBodiesRequest,
		FinalResponseTranslation:    finalResponseTranslationMetadataFromAttempt(plan.RuntimeOperation, outcome.TerminalAttempt, plan.RequestedModelID, outcome.TerminalAttempt.Connection.ID),
		AttemptCount:                state.launchedAttempts,
		Attempts:                    state.attempts,
		RouteReason:                 runtimeExecutionRouteReason(state.routeReason),
	}
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
	lastError := state.lastError
	if strings.TrimSpace(lastError) == "" {
		lastError = "Unknown upstream failure"
	}
	return executionResult{}, &domainError{StatusCode: http.StatusBadGateway, Detail: fmt.Sprintf("All connections failed for model '%s'. Last error: %s", plan.RequestedModelID, lastError)}
}

func (s *Service) executionResultForHedgedWinner(ctx context.Context, plan requestPlan, state requestExecutionState, winner *executionOutcome) executionResult {
	if winner.Response.StatusCode >= 200 && winner.Response.StatusCode <= 299 && winner.Launched {
		s.recordRuntimeSuccess(ctx, plan, winner.Connection, winner.TerminalAttempt.Strategy, winner.Attempt.ResponseTimeMS, winner.Attempt.CompletedAt)
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
		state.lastError = outcome.Err.Error()
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
		state.lastError = fmt.Sprintf("Upstream returned %d", outcome.Response.StatusCode)
		state.recordRetry(outcome.RetryDecision.Reason)
		_ = outcome.Response.Body.Close()
		return executionResult{}, false, nil
	}
	if outcome.Response.StatusCode >= 200 && outcome.Response.StatusCode <= 299 && outcome.Launched {
		s.recordRuntimeSuccess(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.ResponseTimeMS, outcome.Attempt.CompletedAt)
	}
	return state.result(plan, outcome), true, nil
}

func (s *Service) executeHedgedRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, startIndex int, hedgePolicy loadbalance.RuntimeHedgePolicy, bodySource *runtimeRequestBodySource) (hedgedExecutionResult, error) {
	terminalAttempts := plan.orderedTerminalAttempts()
	totalCandidates := hedgePolicy.MaxAdditionalAttempts + 1
	remainingConnections := len(terminalAttempts) - startIndex
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
		terminalAttempt := terminalAttempts[startIndex+order]
		inFlight++
		launchedCandidates++
		go func() {
			results <- hedgedAttemptResult{Order: order, Outcome: s.executeSingleAttempt(attemptCtx, method, plan, requestQuery, terminalAttempt, bodySource)}
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
					s.recordRuntimeTransportFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
				}
				continue
			}
			if outcome.FailoverEligible {
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				result.LastError = fmt.Sprintf("Upstream returned %d", outcome.Response.StatusCode)
				s.recordRuntimeFailoverHTTPFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
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
	s.recordRuntimeHedge(ctx, plan.RuntimeOperation.Name, int64(launchedCandidates-1))
	return result, nil
}

func (s *Service) executeSingleAttempt(ctx context.Context, method string, plan requestPlan, requestQuery string, terminalAttempt runtimeTerminalAttempt, bodySource *runtimeRequestBodySource) executionOutcome {
	attemptTraceAttrs := runtimeTraceAttemptAttributes(plan, terminalAttempt)
	ctx, span := startRuntimeSpan(ctx, "connection.attempt", attemptTraceAttrs...)
	defer span.End()
	connection := terminalAttempt.Connection
	headers, err := s.buildUpstreamHeaders(connection, plan.APIFamily, plan.ClientHeaders, plan.BlocklistRules)
	if err != nil {
		runtimeTraceMarkError(span, "connection_attempt_failed")
		runtimeTraceSetAttemptResult(span, "fatal_error")
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, FatalError: err}
	}
	upstreamURL, err := buildUpstreamURL(connection.Endpoint.BaseURL, terminalAttempt.EffectiveRequestPath, requestQuery)
	if err != nil {
		runtimeTraceMarkError(span, "connection_attempt_failed")
		runtimeTraceSetAttemptResult(span, "fatal_error")
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
		runtimeTraceSetAttemptResult(span, "skipped")
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, Skipped: true, UnbannedRecord: decision.UnbannedRecord}
	}
	if decision.AdmissionReason != "" {
		runtimeTraceSetAttemptResult(span, "admission_rejected")
		return executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, AdmissionReason: decision.AdmissionReason, AdmissionState: decision.AdmissionState, UnbannedRecord: decision.UnbannedRecord}
	}
	defer func() {
		s.runtimeState.FinishConnectionAttempt(decision.Handle, s.nowUTC())
	}()

	attemptStartedAt := s.nowUTC()
	attemptBodySource := bodySourceForTerminalAttempt(bodySource, terminalAttempt)
	response, launched, requestErr := s.doUpstreamRequest(ctx, plan.HTTPClient, method, upstreamURL, headers, attemptBodySource, plan.RuntimeOperation, plan.IsStreamingRequest, runtimeTraceAttemptAttributionAttributes(plan.RuntimeOperation, terminalAttempt.TranslationMode)...)
	outcome := executionOutcome{TerminalAttempt: terminalAttempt, Connection: connection, RequestHeaders: cloneStringMap(headers), Response: response, Launched: launched, Err: requestErr, UnbannedRecord: decision.UnbannedRecord}
	if launched {
		attemptCompletedAt := s.nowUTC()
		outcome.Attempt = executionAttempt{
			Connection:                  connection,
			ResolvedTargetModelID:       strings.TrimSpace(terminalAttempt.TargetModel.ModelID),
			RequestURL:                  upstreamURL,
			RequestHeaders:              cloneStringMap(headers),
			RequestBody:                 append([]byte(nil), terminalAttempt.UpstreamBody...),
			StatusCode:                  http.StatusBadGateway,
			ResponseTimeMS:              durationMilliseconds(attemptCompletedAt.Sub(attemptStartedAt)),
			CompletedAt:                 attemptCompletedAt,
			AuditEnabledAtRequest:       terminalAttempt.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: terminalAttempt.AuditCaptureBodiesRequest,
			UpstreamOperationName:       runtimeUpstreamOperationName(plan.RuntimeOperation, terminalAttempt.TranslationMode),
			UpstreamRequestPath:         dereferenceString(runtimeUpstreamRequestPath(plan.RuntimeOperation, terminalAttempt.TranslationMode, terminalAttempt.EffectiveRequestPath)),
			OperationTranslationMode:    normalizedRuntimeTranslationMode(terminalAttempt.TranslationMode),
		}
		if response != nil {
			outcome.Attempt.StatusCode = response.StatusCode
			outcome.Attempt.ResponseHeaders = response.Header.Clone()
			runtimeTraceSetStatusCode(span, response.StatusCode)
		}
		if s.isHedgeLoserCancellation(ctx, requestErr) {
			outcome.Attempt.StatusCode = hedgeCanceledAttemptStatusCode
			outcome.SuppressTransportFeedback = true
			runtimeTraceSetAttemptResult(span, "hedge_canceled")
		}
	}
	if requestErr != nil {
		if !outcome.SuppressTransportFeedback {
			runtimeTraceMarkError(span, "provider_http_failed")
			runtimeTraceSetAttemptResult(span, "transport_error")
		}
		outcome.RetryDecision = gatewayrouting.RetryPolicy{FailoverStatusCodes: terminalAttempt.Strategy.FailoverStatusCodes()}.ClassifyTransportError(requestErr)
		outcome.FailoverEligible = outcome.RetryDecision.Retryable
		outcome.Definitive = !outcome.FailoverEligible
		return outcome
	}
	outcome.RetryDecision = gatewayrouting.RetryPolicy{FailoverStatusCodes: terminalAttempt.Strategy.FailoverStatusCodes()}.ClassifyHTTPStatus(response.StatusCode)
	outcome.FailoverEligible = outcome.RetryDecision.Retryable
	outcome.Definitive = !outcome.FailoverEligible
	if outcome.FailoverEligible {
		runtimeTraceSetAttemptResult(span, "failover_http")
	} else {
		runtimeTraceSetAttemptResult(span, "success")
	}
	return outcome
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

func (s *Service) recordRuntimeSuccess(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, responseTimeMS int, completedAt time.Time) {
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeSuccess(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, responseTimeMS, completedAt)
	if !transition.RecoveryEventEligible {
		return
	}
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackSuccessRecovery, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, CompletedAt: completedAt, ResponseTimeMS: responseTimeMS})
}

func (s *Service) recordRuntimeFailoverHTTPFailure(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s != nil && s.runtimeMetrics != nil {
		s.runtimeMetrics.recordFailover(runtimeMetricContextFromContext(ctx), plan.RuntimeOperation.Name, "http")
	}
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeFailoverHTTPFailure(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackFailoverHTTP, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "transient_http", CompletedAt: completedAt})
}

func (s *Service) recordRuntimeTransportFailure(ctx context.Context, plan requestPlan, connection runtimeConnection, strategy loadbalance.RuntimeStrategy, completedAt time.Time) {
	if s != nil && s.runtimeMetrics != nil {
		s.runtimeMetrics.recordFailover(runtimeMetricContextFromContext(ctx), plan.RuntimeOperation.Name, "transport")
	}
	if s == nil || s.runtimeState == nil {
		return
	}
	transition := s.runtimeState.RecordRuntimeTransportFailure(plan.ProfileID, connection.ModelConfigID, connection.ID, strategy, completedAt)
	s.enqueueRuntimeFeedback(ctx, plan.RuntimeOperation.Name, runtimeFeedbackEvent{Kind: runtimeFeedbackTransportFailure, ProfileID: plan.ProfileID, ConnectionID: connection.ID, ModelConfigID: connection.ModelConfigID, Strategy: strategy, Transition: transition, FailureKind: "connect_error", CompletedAt: completedAt})
}

func (s *Service) recordRuntimeHedge(ctx context.Context, operationName string, count int64) {
	if s == nil || s.runtimeMetrics == nil {
		return
	}
	s.runtimeMetrics.recordHedge(runtimeMetricContextFromContext(ctx), operationName, count)
}

func (s *Service) enqueueRuntimeFeedback(ctx context.Context, operationName string, event runtimeFeedbackEvent) {
	if s == nil {
		return
	}
	event.APIFamily = eventAPIFamily(event.APIFamily, operationName)
	if event.TraceContext.empty() {
		event.TraceContext = runtimeTraceContextFromContext(ctx)
	}
	result := RuntimeFeedbackEnqueueResult{Status: RuntimeFeedbackDroppedUnavailable, Reason: "pipeline_unavailable"}
	if s.feedbackPipeline != nil {
		result = s.feedbackPipeline.TryEnqueueContext(runtimeMetricContextFromContext(ctx), event)
	}
	s.recordRuntimeFeedbackEnqueue(runtimeMetricContextFromContext(ctx), operationName, event.Kind, result.Status)
}

func runtimeMetricContextFromContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func (s *Service) doUpstreamRequest(ctx context.Context, client *http.Client, method string, upstreamURL string, headers map[string]string, bodySource *runtimeRequestBodySource, operation RuntimeOperation, isStreaming bool, extraAttrs ...attribute.KeyValue) (*http.Response, bool, error) {
	attrs := runtimeTraceHTTPAttributes(method, operation, isStreaming, runtimeTraceBodyMode(bodySource))
	attrs = append(attrs, extraAttrs...)
	ctx, span := startRuntimeClientSpan(ctx, "provider.http", attrs...)
	defer span.End()
	if client == nil {
		client = s.httpClient
	}
	if client == nil {
		runtimeTraceMarkError(span, "provider_http_failed")
		return nil, false, fmt.Errorf("runtime HTTP client unavailable")
	}
	requestBody, contentLength, err := bodySource.Open()
	if err != nil {
		runtimeTraceMarkError(span, "provider_http_failed")
		return nil, false, fmt.Errorf("open upstream request body: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, method, upstreamURL, requestBody)
	if err != nil {
		runtimeTraceMarkError(span, "provider_http_failed")
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
	if response != nil {
		runtimeTraceSetStatusCode(span, response.StatusCode)
	}
	if err != nil {
		runtimeTraceMarkError(span, "provider_http_failed")
	}
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

func copyResponseHeaders(target http.Header, source http.Header) {
	for key, values := range filterResponseHeaders(source) {
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func copyTranslatedResponseHeaders(target http.Header, source http.Header) {
	copyTranslatedResponseHeadersWithContentType(target, source, "application/json")
}

func copyTranslatedResponseHeadersWithContentType(target http.Header, source http.Header, contentType string) {
	for key, values := range filterTranslatedResponseHeaders(source) {
		for _, value := range values {
			target.Add(key, value)
		}
	}
	target.Set("Content-Type", contentType)
}

func filterResponseHeaders(source http.Header) http.Header {
	return filterResponseHeadersWithEntitySafety(source, false)
}

func filterTranslatedResponseHeaders(source http.Header) http.Header {
	return filterResponseHeadersWithEntitySafety(source, true)
}

func filterResponseHeadersWithEntitySafety(source http.Header, translated bool) http.Header {
	filtered := make(http.Header)
	for key, values := range source {
		keyLower := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := hopByHopHeaders[keyLower]; blocked {
			continue
		}
		if translated {
			if _, unsafeEntity := translatedResponseUnsafeEntityHeaders[keyLower]; unsafeEntity {
				continue
			}
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
