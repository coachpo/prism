package runtime

import (
	"time"

	gatewayaccounting "github.com/coachpo/prism/backend/internal/gateway/accounting"
)

type requestLogInsert struct {
	ProfileID                         int
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	OperationName                     string  `json:"operation_name"`
	UpstreamOperationName             *string `json:"upstream_operation_name,omitempty"`
	OperationTranslationMode          *string `json:"operation_translation_mode,omitempty"`
	EndpointID                        *int
	ConnectionID                      *int
	SelectedTerminalTargetID          *int
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
	ProxyAPIKeyAuthEnforcedAtRequest  *bool
	IngressRequestID                  string
	AttemptNumber                     int
	ProviderCorrelationID             *string
	EndpointBaseURL                   *string
	EndpointDescription               *string
	StatusCode                        int
	ResponseTimeMS                    int
	IsStream                          bool
	InputTokens                       *int
	OutputTokens                      *int
	TotalTokens                       *int
	SuccessFlag                       bool
	UnpricedReason                    *string
	CacheReadInputTokens              *int
	CacheCreationInputTokens          *int
	ReasoningTokens                   *int
	InputCostMicros                   *int64
	OutputCostMicros                  *int64
	CacheReadInputCostMicros          *int64
	CacheCreationInputCostMicros      *int64
	ReasoningCostMicros               *int64
	TotalCostOriginalMicros           *int64
	TotalCostUserCurrencyMicros       *int64
	CurrencyCodeOriginal              *string
	ReportCurrencyCode                *string
	ReportCurrencySymbol              *string
	FXRateUsed                        *string
	FXRateSource                      *string
	PricingSnapshotUnit               *string
	PricingSnapshotInput              *string
	PricingSnapshotOutput             *string
	PricingSnapshotCacheReadInput     *string
	PricingSnapshotCacheCreationInput *string
	PricingSnapshotReasoning          *string
	PricingConfigVersionUsed          *int
	RequestPath                       string
	UpstreamRequestPath               *string `json:"upstream_request_path,omitempty"`
	ErrorDetail                       *string
	CreatedAt                         time.Time
	CallerUserAgent                   *string
	UpstreamUserAgent                 *string
	CompletionDurationMS              *int
	TTFTMS                            *int
	StreamOutcome                     string
	StreamErrorKind                   *string
	StreamErrorDetail                 *string
	AuditEnabledAtRequest             bool
	AuditCaptureBodiesAtRequest       bool
	RequestGenerationParams           *requestGenerationParams
	RequestGenerationParamsStatus     *string

	// Pricing cost-trust v2 (Pricing SPEC §6.4).
	PricingStatus                 string
	PricingResolutionKind         *string
	MissingPriceComponents        []string
	PricingEvidenceTrust          string
	PricingTemplateIDUsed         *int
	PricingTemplateNameSnapshot   *string
	PricingTemplateRevisionIDUsed *int64
	PricingVersionEffectiveAt     *time.Time
	ReportingCurrencyEpoch        *int

	// Requests/Audit v2 fields (Requests SPEC §3.2-§3.4/§4.4).
	RowKind                    string
	CallerRequestID            *string
	URLScrubProvenance         string
	MetadataRedactedFields     []string
	MetadataTruncatedFields    []string
	AttemptTrigger             *string
	AttemptResult              *string
	IsWinner                   *bool
	AttemptDurationMS          *int
	LegacyDurationMS           *int
	UpstreamStatusCode         *int
	GatewayStatusCode          *int
	LegacyStatusCode           *int
	ErrorSource                *string
	ErrorCode                  *string
	FailureStage               *string
	ErrorDetailRedacted        bool
	ErrorDetailTruncated       bool
	StreamErrorDetailRedacted  bool
	StreamErrorDetailTruncated bool
	UpstreamRequestStarted     *bool
	ResponseHeadersReceived    *bool
	FirstBodyOrStreamEventSeen *bool
}

type usageEventInsert struct {
	ProfileID                         int
	IngressRequestID                  string
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	OperationName                     string  `json:"operation_name"`
	UpstreamOperationName             *string `json:"upstream_operation_name,omitempty"`
	OperationTranslationMode          *string `json:"operation_translation_mode,omitempty"`
	EndpointID                        *int
	EndpointLabelSnapshot             string
	ConnectionID                      *int
	SelectedTerminalTargetID          *int
	ProxyAPIKeyNameSnapshot           *string
	ProxyAPIKeyAuthEnforcedAtRequest  *bool
	StatusCode                        int
	SuccessFlag                       bool
	BillableFlag                      *bool
	PricedFlag                        *bool
	UnpricedReason                    *string
	InputTokens                       *int
	OutputTokens                      *int
	TotalTokens                       *int
	CacheReadInputTokens              *int
	CacheCreationInputTokens          *int
	ReasoningTokens                   *int
	InputCostMicros                   *int64
	OutputCostMicros                  *int64
	CacheReadInputCostMicros          *int64
	CacheCreationInputCostMicros      *int64
	ReasoningCostMicros               *int64
	TotalCostOriginalMicros           *int64
	TotalCostUserCurrencyMicros       *int64
	CurrencyCodeOriginal              *string
	ReportCurrencyCode                *string
	ReportCurrencySymbol              *string
	FXRateUsed                        *string
	FXRateSource                      *string
	PricingSnapshotUnit               *string
	PricingSnapshotInput              *string
	PricingSnapshotOutput             *string
	PricingSnapshotCacheReadInput     *string
	PricingSnapshotCacheCreationInput *string
	PricingSnapshotReasoning          *string
	PricingConfigVersionUsed          *int
	AttemptCount                      int
	RequestPath                       string
	UpstreamRequestPath               *string `json:"upstream_request_path,omitempty"`
	CreatedAt                         time.Time
	ResponseTimeMS                    *int
	CompletionDurationMS              *int
	TTFTMS                            *int
	StreamOutcome                     string
	StreamErrorKind                   *string

	// Pricing cost-trust v2 (Pricing SPEC §6.4).
	PricingStatus                 string
	PricingResolutionKind         *string
	MissingPriceComponents        []string
	PricingEvidenceTrust          string
	PricingTemplateIDUsed         *int
	PricingTemplateNameSnapshot   *string
	PricingTemplateRevisionIDUsed *int64
	PricingVersionEffectiveAt     *time.Time
	ReportingCurrencyEpoch        *int
	CurrencyAttribution           string

	// Observe finalized-ingress fields (Observe SPEC §3.5, Requests SPEC
	// §3.6): expected request-log row count, routing evidence, final attempt
	// identity, and the terminal error code for failed/client-disconnected
	// final results.
	ExpectedRequestLogRowCount *int
	FinalAttemptNumber         *int
	FinalAttemptTrigger        *string
	FinalTargetEntryTrigger    *string
	SameTargetRetryOccurred    bool
	HedgeOccurred              bool
	FailoverOccurred           bool
	RoutingEvidenceComplete    *bool
	FinalErrorCode             *string
	IngressStartedAt           *time.Time
	IngressCompletedAt         *time.Time
	ProxyAPIKeyIDSnapshot      *int
}

func (requestLog *requestLogInsert) applyRuntimePricingResult(pricingResult runtimePricingResult) {
	requestLog.UnpricedReason = pricingResult.UnpricedReason
	requestLog.InputCostMicros = pricingResult.InputCostMicros
	requestLog.OutputCostMicros = pricingResult.OutputCostMicros
	requestLog.CacheReadInputCostMicros = pricingResult.CacheReadInputCostMicros
	requestLog.CacheCreationInputCostMicros = pricingResult.CacheCreationInputCostMicros
	requestLog.ReasoningCostMicros = pricingResult.ReasoningCostMicros
	requestLog.TotalCostOriginalMicros = pricingResult.TotalCostOriginalMicros
	requestLog.TotalCostUserCurrencyMicros = pricingResult.TotalCostUserCurrencyMicros
	requestLog.CurrencyCodeOriginal = pricingResult.CurrencyCodeOriginal
	requestLog.ReportCurrencyCode = pricingResult.ReportCurrencyCode
	requestLog.ReportCurrencySymbol = pricingResult.ReportCurrencySymbol
	requestLog.FXRateUsed = pricingResult.FXRateUsed
	requestLog.FXRateSource = pricingResult.FXRateSource
	requestLog.PricingSnapshotUnit = pricingResult.PricingSnapshotUnit
	requestLog.PricingSnapshotInput = pricingResult.PricingSnapshotInput
	requestLog.PricingSnapshotOutput = pricingResult.PricingSnapshotOutput
	requestLog.PricingSnapshotCacheReadInput = pricingResult.PricingSnapshotCacheReadInput
	requestLog.PricingSnapshotCacheCreationInput = pricingResult.PricingSnapshotCacheCreationInput
	requestLog.PricingSnapshotReasoning = pricingResult.PricingSnapshotReasoning
	requestLog.PricingConfigVersionUsed = pricingResult.PricingConfigVersionUsed
	requestLog.PricingStatus = pricingResult.PricingStatus
	requestLog.PricingResolutionKind = pricingResult.PricingResolutionKind
	requestLog.MissingPriceComponents = pricingResult.MissingPriceComponents
	requestLog.PricingEvidenceTrust = pricingResult.PricingEvidenceTrust
	requestLog.PricingTemplateIDUsed = pricingResult.PricingTemplateIDUsed
	requestLog.PricingTemplateNameSnapshot = pricingResult.PricingTemplateNameSnapshot
	requestLog.PricingTemplateRevisionIDUsed = pricingResult.PricingTemplateRevisionIDUsed
	requestLog.PricingVersionEffectiveAt = pricingResult.PricingVersionEffectiveAt
	requestLog.ReportingCurrencyEpoch = pricingResult.ReportingCurrencyEpoch
}

func (usageEvent *usageEventInsert) applyRuntimePricingResult(pricingResult runtimePricingResult) {
	usageEvent.UnpricedReason = pricingResult.UnpricedReason
	usageEvent.InputCostMicros = pricingResult.InputCostMicros
	usageEvent.OutputCostMicros = pricingResult.OutputCostMicros
	usageEvent.CacheReadInputCostMicros = pricingResult.CacheReadInputCostMicros
	usageEvent.CacheCreationInputCostMicros = pricingResult.CacheCreationInputCostMicros
	usageEvent.ReasoningCostMicros = pricingResult.ReasoningCostMicros
	usageEvent.TotalCostOriginalMicros = pricingResult.TotalCostOriginalMicros
	usageEvent.TotalCostUserCurrencyMicros = pricingResult.TotalCostUserCurrencyMicros
	usageEvent.CurrencyCodeOriginal = pricingResult.CurrencyCodeOriginal
	usageEvent.ReportCurrencyCode = pricingResult.ReportCurrencyCode
	usageEvent.ReportCurrencySymbol = pricingResult.ReportCurrencySymbol
	usageEvent.FXRateUsed = pricingResult.FXRateUsed
	usageEvent.FXRateSource = pricingResult.FXRateSource
	usageEvent.PricingSnapshotUnit = pricingResult.PricingSnapshotUnit
	usageEvent.PricingSnapshotInput = pricingResult.PricingSnapshotInput
	usageEvent.PricingSnapshotOutput = pricingResult.PricingSnapshotOutput
	usageEvent.PricingSnapshotCacheReadInput = pricingResult.PricingSnapshotCacheReadInput
	usageEvent.PricingSnapshotCacheCreationInput = pricingResult.PricingSnapshotCacheCreationInput
	usageEvent.PricingSnapshotReasoning = pricingResult.PricingSnapshotReasoning
	usageEvent.PricingConfigVersionUsed = pricingResult.PricingConfigVersionUsed
	usageEvent.PricingStatus = pricingResult.PricingStatus
	usageEvent.PricingResolutionKind = pricingResult.PricingResolutionKind
	usageEvent.MissingPriceComponents = pricingResult.MissingPriceComponents
	usageEvent.PricingEvidenceTrust = pricingResult.PricingEvidenceTrust
	usageEvent.PricingTemplateIDUsed = pricingResult.PricingTemplateIDUsed
	usageEvent.PricingTemplateNameSnapshot = pricingResult.PricingTemplateNameSnapshot
	usageEvent.PricingTemplateRevisionIDUsed = pricingResult.PricingTemplateRevisionIDUsed
	usageEvent.PricingVersionEffectiveAt = pricingResult.PricingVersionEffectiveAt
	usageEvent.ReportingCurrencyEpoch = pricingResult.ReportingCurrencyEpoch
}

func withRuntimePricingSnapshotForPersistence(pricingResult runtimePricingResult, pricingTemplateSnapshot *runtimePricingTemplateSnapshot) runtimePricingResult {
	if pricingTemplateSnapshot == nil {
		return pricingResult
	}
	pricingResult.PricingSnapshotUnit = runtimeOptionalTrimmedString(pricingTemplateSnapshot.PricingUnit)
	pricingResult.PricingSnapshotInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.InputPrice)
	pricingResult.PricingSnapshotOutput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.OutputPrice)
	pricingResult.PricingSnapshotCacheReadInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.CachedInputPrice)
	pricingResult.PricingSnapshotCacheCreationInput = runtimeOptionalTrimmedString(pricingTemplateSnapshot.CacheCreationPrice)
	pricingResult.PricingSnapshotReasoning = runtimeOptionalTrimmedString(pricingTemplateSnapshot.ReasoningPrice)
	pricingResult.PricingConfigVersionUsed = intPtr(pricingTemplateSnapshot.Version)
	return pricingResult
}

type auditLogInsert struct {
	RequestLogAttemptNumber     int       `json:"request_log_attempt_number"`
	ProfileID                   int       `json:"profile_id"`
	ModelID                     string    `json:"model_id"`
	EndpointID                  int       `json:"endpoint_id"`
	ConnectionID                int       `json:"connection_id"`
	EndpointBaseURL             string    `json:"endpoint_base_url"`
	EndpointDescription         *string   `json:"endpoint_description,omitempty"`
	RequestMethod               string    `json:"request_method"`
	RequestURL                  string    `json:"request_url"`
	RequestURLTruncated         bool      `json:"request_url_truncated"`
	EndpointBaseURLTruncated    bool      `json:"endpoint_base_url_truncated"`
	RequestHeaders              string    `json:"request_headers"`
	RequestBody                 *string   `json:"request_body,omitempty"`
	RequestBodyStored           bool      `json:"request_body_stored"`
	ResponseStatus              int       `json:"response_status"`
	ResponseHeaders             *string   `json:"response_headers,omitempty"`
	ResponseBody                *string   `json:"response_body,omitempty"`
	ResponseBodyStored          bool      `json:"response_body_stored"`
	IsStream                    bool      `json:"is_stream"`
	DurationMS                  int       `json:"duration_ms"`
	CreatedAt                   time.Time `json:"created_at"`
	AuditEnabledAtRequest       bool      `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest bool      `json:"audit_capture_bodies_at_request"`

	// Audit v2 scoped fields (Requests SPEC §5.2): row kind, scoped statuses,
	// attempt/legacy durations, URL scrub provenance, and body/header capture
	// provenance for legacy-consistent writes.
	RowKind                           string
	AttemptNumber                     *int
	AttemptDurationMS                 *int
	LegacyDurationMS                  *int
	UpstreamStatusCode                *int
	GatewayStatusCode                 *int
	LegacyStatusCode                  *int
	URLScrubProvenance                string
	RequestHeadersScrubProvenance     string
	ResponseHeadersScrubProvenance    string
	RequestHeadersCaptureStatus       string
	ResponseHeadersCaptureStatus      string
	RequestHeadersCaptureLimitReason  string
	ResponseHeadersCaptureLimitReason string
	RequestBodyCaptureProvenance      string
	ResponseBodyCaptureProvenance     string
	RequestBodyCaptureStatus          string
	ResponseBodyCaptureStatus         string
	RequestBodyCaptureLimitReason     string
	ResponseBodyCaptureLimitReason    string
	RequestBodyCaptureEndState        *string
	ResponseBodyCaptureEndState       *string
	RequestBodyEncoding               *string
	ResponseBodyEncoding              *string
	RequestBodyBytesObserved          *int64
	RequestBodyBytesStored            *int64
	ResponseBodyBytesObserved         *int64
	ResponseBodyBytesStored           *int64
	RequestBodyTruncated              bool
	ResponseBodyTruncated             bool
}

type runtimeProxyKeyUsageSignal struct {
	KeyID      int       `json:"key_id"`
	LastUsedAt time.Time `json:"last_used_at"`
	LastUsedIP string    `json:"last_used_ip,omitempty"`
}

type runtimeTelemetryEnvelope struct {
	RequestLogs          []requestLogInsert          `json:"request_logs"`
	AuditLogs            []auditLogInsert            `json:"audit_logs,omitempty"`
	UsageEvent           usageEventInsert            `json:"usage_event"`
	AccountingEvent      gatewayaccounting.Event     `json:"accounting_event"`
	AccountingAttempts   []gatewayaccounting.Event   `json:"accounting_attempts,omitempty"`
	ProxyKeyUsage        *runtimeProxyKeyUsageSignal `json:"proxy_key_usage,omitempty"`
	ProxyKeyAuthEnforced *bool                       `json:"proxy_key_auth_enforced_at_request,omitempty"`
	TraceContext         runtimeTraceContext         `json:"trace_context,omitempty"`
	HandoffPhase         string                      `json:"handoff_phase,omitempty"`
}

const (
	runtimeTelemetryHandoffPhaseStreamAccepted   = "stream_accepted"
	runtimeUsageCurrencyAttributionIdentified    = "identified"
	runtimeUsageCurrencyAttributionLegacyUnknown = "legacy_unknown"

	// request_logs row kinds (Observe SPEC §3.5): new writers never produce
	// legacy_unknown.
	requestLogRowKindPlanning  = "planning"
	requestLogRowKindAdmission = "admission"
	requestLogRowKindUpstream  = "upstream"

	// URL scrub provenance (Requests SPEC §4.4).
	runtimeURLScrubProvenanceRuntime = "runtime_scrubbed"
)
