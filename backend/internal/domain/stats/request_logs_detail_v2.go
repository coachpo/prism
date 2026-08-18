package stats

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// v2 exact request detail (Requests SPEC §6.4). Old un-scoped `status_code`
// and mixed `response_time_ms` are gone from the public DTO; the row carries
// scoped statuses and attempt/legacy durations plus the unified failure
// projection, canonical refs, and pricing projections.

// FailureProjectionDetail is the unified failure projection for one row.
type FailureProjectionDetail struct {
	Category                   string  `json:"category"`
	Source                     *string `json:"source"`
	Stage                      *string `json:"stage"`
	Code                       *string `json:"code"`
	Detail                     *string `json:"detail"`
	DetailRedacted             bool    `json:"detail_redacted"`
	DetailTruncated            bool    `json:"detail_truncated"`
	DetailSource               string  `json:"detail_source"`
	EvidenceState              string  `json:"evidence_state"`
	UpstreamRequestStarted     *bool   `json:"upstream_request_started"`
	ResponseHeadersReceived    *bool   `json:"response_headers_received"`
	FirstBodyOrStreamEventSeen *bool   `json:"first_body_or_stream_event_seen"`
	StreamOutcome              *string `json:"stream_outcome"`
	StreamErrorKind            *string `json:"stream_error_kind"`
	StreamErrorDetail          *string `json:"stream_error_detail"`
}

// TerminalTargetProjectionDetail is the canonical terminal-target ref.
type TerminalTargetProjectionDetail struct {
	Kind               string  `json:"kind"`
	TerminalTargetID   string  `json:"terminal_target_id"`
	OwnerModelConfigID *string `json:"owner_model_config_id"`
	Name               *string `json:"name"`
	NameSource         string  `json:"name_source"`
	Deleted            bool    `json:"deleted"`
	Configured         bool    `json:"configured"`
}

// EndpointProjectionDetail is the canonical endpoint ref.
type EndpointProjectionDetail struct {
	Kind       string  `json:"kind"`
	ID         string  `json:"id"`
	Name       *string `json:"name"`
	NameSource string  `json:"name_source"`
	Deleted    bool    `json:"deleted"`
	Configured bool    `json:"configured"`
}

// RoutingProvenanceDetail carries the initial vs actual terminal target split.
type RoutingProvenanceDetail struct {
	InitialTerminalTarget *TerminalTargetProjectionDetail `json:"initial_terminal_target"`
	DiffersFromActual     bool                            `json:"differs_from_actual"`
}

// PricingProjectionDetail is the exact pricing projection (no billable/priced
// aliases; cost segment key and currency attribution derived canonically).
type PricingProjectionDetail struct {
	PricingStatus                     string     `json:"pricing_status"`
	UnpricedReason                    *string    `json:"unpriced_reason"`
	PricingResolutionKind             *string    `json:"pricing_resolution_kind"`
	MissingPriceComponents            []string   `json:"missing_price_components"`
	PricingEvidenceTrust              string     `json:"pricing_evidence_trust"`
	TotalCostUserCurrencyMicros       *int64     `json:"total_cost_user_currency_micros"`
	TotalCostOriginalMicros           *int64     `json:"total_cost_original_micros"`
	CurrencyCodeOriginal              *string    `json:"currency_code_original"`
	FXRateUsed                        *string    `json:"fx_rate_used"`
	FXRateSource                      *string    `json:"fx_rate_source"`
	ReportCurrencyCode                *string    `json:"report_currency_code"`
	ReportCurrencySymbol              *string    `json:"report_currency_symbol"`
	ReportingCurrencyEpoch            *int       `json:"reporting_currency_epoch"`
	CurrencyAttribution               string     `json:"currency_attribution"`
	CostSegmentKey                    *string    `json:"cost_segment_key"`
	PricingTemplateIDUsed             *int       `json:"pricing_template_id_used"`
	PricingTemplateNameSnapshot       *string    `json:"pricing_template_name_snapshot"`
	PricingTemplateRevisionIDUsed     *int64     `json:"pricing_template_revision_id_used"`
	PricingConfigVersionUsed          *int       `json:"pricing_config_version_used"`
	PricingVersionEffectiveAt         *time.Time `json:"pricing_version_effective_at"`
	PricingSnapshotUnit               *string    `json:"pricing_snapshot_unit"`
	PricingSnapshotInput              *string    `json:"pricing_snapshot_input"`
	PricingSnapshotOutput             *string    `json:"pricing_snapshot_output"`
	PricingSnapshotCacheReadInput     *string    `json:"pricing_snapshot_cache_read_input"`
	PricingSnapshotCacheCreationInput *string    `json:"pricing_snapshot_cache_creation_input"`
	PricingSnapshotReasoning          *string    `json:"pricing_snapshot_reasoning"`
	PricingTierApplied                *string    `json:"pricing_tier_applied"`
	PricingTierThresholdTokens        *int       `json:"pricing_tier_threshold_tokens"`
	PricingTierBasisTokens            *int64     `json:"pricing_tier_basis_tokens"`
	EvidenceState                     string     `json:"evidence_state"`
}

// LegacyPricingEvidenceDetail is only non-null when evidence trust is
// legacy_untrusted; raw values never backfill canonical pricing fields.
type LegacyPricingEvidenceDetail struct {
	RawTotalCostOriginalMicros *int64            `json:"raw_total_cost_original_micros"`
	RawTotalCostReportMicros   *int64            `json:"raw_total_cost_report_micros"`
	RawComponentCostMicros     map[string]string `json:"raw_component_cost_micros"`
	RawPriceSnapshots          map[string]string `json:"raw_price_snapshots"`
	OriginalCurrencyCode       *string           `json:"original_currency_code"`
	ReportCurrencyCode         *string           `json:"report_currency_code"`
	WarningCode                string            `json:"warning_code"`
}

// CurrentPricingTemplateDetail is an optional read-only comparison.
type CurrentPricingTemplateDetail struct {
	TemplateID             int        `json:"template_id"`
	Deleted                bool       `json:"deleted"`
	CurrentRevisionID      string     `json:"current_revision_id"`
	CurrentVersion         int        `json:"current_version"`
	CurrentEffectiveAt     *time.Time `json:"current_effective_at"`
	MatchesRequestRevision bool       `json:"matches_request_revision"`
}

// RequestLogDetailSummaryV2 is the scoped summary block.
type RequestLogDetailSummaryV2 struct {
	RequestLogID             string    `json:"request_log_id"`
	CreatedAt                time.Time `json:"created_at"`
	ModelID                  string    `json:"model_id"`
	ModelLabel               string    `json:"model_label"`
	ResolvedTargetModelID    *string   `json:"resolved_target_model_id"`
	ResolvedTargetModelLabel *string   `json:"resolved_target_model_label"`
	APIFamily                string    `json:"api_family"`
	RowKind                  string    `json:"row_kind"`
	UpstreamStatusCode       *int      `json:"upstream_status_code"`
	GatewayStatusCode        *int      `json:"gateway_status_code"`
	LegacyStatusCode         *int      `json:"legacy_status_code"`
	AttemptDurationMS        *int      `json:"attempt_duration_ms"`
	LegacyDurationMS         *int      `json:"legacy_duration_ms"`
	TTFTMS                   *int      `json:"ttft_ms"`
	CompletionDurationMS     *int      `json:"completion_duration_ms"`
	IsStream                 bool      `json:"is_stream"`
	StreamOutcome            string    `json:"stream_outcome"`
	StreamErrorKind          *string   `json:"stream_error_kind"`
	AttemptNumber            *int      `json:"attempt_number"`
	AttemptTrigger           *string   `json:"attempt_trigger"`
	AttemptResult            *string   `json:"attempt_result"`
	IsWinner                 *bool     `json:"is_winner"`
}

// RequestLogDetailRequestV2 is the request-context block.
type RequestLogDetailRequestV2 struct {
	OperationName                    *string          `json:"operation_name"`
	UpstreamOperationName            *string          `json:"upstream_operation_name"`
	OperationTranslationMode         *string          `json:"operation_translation_mode"`
	RequestPath                      string           `json:"request_path"`
	UpstreamRequestPath              *string          `json:"upstream_request_path"`
	IngressRequestID                 *string          `json:"ingress_request_id"`
	ProviderCorrelationID            *string          `json:"provider_correlation_id"`
	ProxyAPIKeyID                    *int             `json:"proxy_api_key_id"`
	ProxyAPIKeyNameSnapshot          *string          `json:"proxy_api_key_name_snapshot"`
	ProxyAPIKeyAttributionState      *string          `json:"proxy_api_key_attribution_state"`
	ProxyAPIKeyAuthEnforcedAtRequest *bool            `json:"proxy_api_key_auth_enforced_at_request"`
	CallerUserAgent                  *string          `json:"caller_user_agent"`
	UpstreamUserAgent                *string          `json:"upstream_user_agent"`
	CallerClientDisplay              *string          `json:"caller_client_display"`
	UpstreamClientDisplay            *string          `json:"upstream_client_display"`
	UserAgentOverridden              bool             `json:"user_agent_overridden"`
	RequestGenerationParams          *json.RawMessage `json:"request_generation_params"`
	RequestGenerationParamsStatus    *string          `json:"request_generation_params_status"`
	MetadataRedactedFields           []string         `json:"metadata_redacted_fields"`
	MetadataTruncatedFields          []string         `json:"metadata_truncated_fields"`
	URLScrubProvenance               string           `json:"url_scrub_provenance"`
}

// RequestLogDetailRoutingV2 is the routing-context block.
type RequestLogDetailRoutingV2 struct {
	ProfileID                   int     `json:"profile_id"`
	EndpointLabel               string  `json:"endpoint_label"`
	EndpointID                  *int    `json:"endpoint_id"`
	TerminalTargetID            *int    `json:"terminal_target_id"`
	SelectedTerminalTargetID    *int    `json:"selected_terminal_target_id"`
	EndpointBaseURL             *string `json:"endpoint_base_url"`
	EndpointDescription         *string `json:"endpoint_description"`
	AuditEnabledAtRequest       bool    `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest bool    `json:"audit_capture_bodies_at_request"`
}

// RequestLogDetailUsageV2 is the usage block.
type RequestLogDetailUsageV2 struct {
	InputTokens              *int  `json:"input_tokens"`
	OutputTokens             *int  `json:"output_tokens"`
	TotalTokens              *int  `json:"total_tokens"`
	SuccessFlag              *bool `json:"success_flag"`
	CacheReadInputTokens     *int  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int  `json:"cache_creation_input_tokens"`
	ReasoningTokens          *int  `json:"reasoning_tokens"`
}

// RequestLogDetailResponseV2 is the exact v2 detail envelope.
type RequestLogDetailResponseV2 struct {
	Summary                RequestLogDetailSummaryV2       `json:"summary"`
	Request                RequestLogDetailRequestV2       `json:"request"`
	Routing                RequestLogDetailRoutingV2       `json:"routing"`
	Usage                  RequestLogDetailUsageV2         `json:"usage"`
	Failure                *FailureProjectionDetail        `json:"failure"`
	TerminalTarget         *TerminalTargetProjectionDetail `json:"terminal_target"`
	Endpoint               *EndpointProjectionDetail       `json:"endpoint"`
	RoutingProvenance      RoutingProvenanceDetail         `json:"routing_provenance"`
	Pricing                PricingProjectionDetail         `json:"pricing"`
	LegacyPricingEvidence  *LegacyPricingEvidenceDetail    `json:"legacy_pricing_evidence"`
	CurrentPricingTemplate *CurrentPricingTemplateDetail   `json:"current_pricing_template"`
}

// requestLogDetailRowV2 is the full v2 row projection.
type requestLogDetailRowV2 struct {
	ProfileID                         int
	ID                                int64
	CreatedAt                         time.Time
	ModelID                           string
	ResolvedTargetModelID             *string
	APIFamily                         string
	RowKind                           string
	UpstreamStatusCode                *int
	GatewayStatusCode                 *int
	LegacyStatusCode                  *int
	AttemptDurationMS                 *int
	LegacyDurationMS                  *int
	TTFTMS                            *int
	CompletionDurationMS              *int
	IsStream                          bool
	StreamOutcome                     string
	StreamErrorKind                   *string
	AttemptNumber                     *int
	AttemptTrigger                    *string
	AttemptResult                     *string
	IsWinner                          *bool
	OperationName                     *string
	UpstreamOperationName             *string
	OperationTranslationMode          *string
	RequestPath                       string
	UpstreamRequestPath               *string
	IngressRequestID                  *string
	ProviderCorrelationID             *string
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
	ProxyAPIKeyAttributionState       *string
	ProxyAPIKeyAuthEnforcedAtRequest  *bool
	CallerUserAgent                   *string
	UpstreamUserAgent                 *string
	ErrorDetail                       *string
	ErrorDetailRedacted               bool
	ErrorDetailTruncated              bool
	ErrorSource                       *string
	ErrorCode                         *string
	FailureStage                      *string
	UpstreamRequestStarted            *bool
	ResponseHeadersReceived           *bool
	FirstBodyOrStreamEventSeen        *bool
	StreamErrorDetail                 *string
	StreamErrorDetailRedacted         bool
	StreamErrorDetailTruncated        bool
	RequestGenerationParams           *json.RawMessage
	RequestGenerationParamsStatus     *string
	MetadataRedactedFields            []string
	MetadataTruncatedFields           []string
	URLScrubProvenance                string
	EndpointID                        *int
	ConnectionID                      *int
	SelectedTerminalTargetID          *int
	EndpointBaseURL                   *string
	EndpointDescription               *string
	AuditEnabledAtRequest             bool
	AuditCaptureBodiesAtRequest       bool
	InputTokens                       *int
	OutputTokens                      *int
	TotalTokens                       *int
	SuccessFlag                       *bool
	PricingStatus                     string
	PricingEvidenceTrust              string
	UnpricedReason                    *string
	PricingResolutionKind             *string
	MissingPriceComponents            []string
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
	PricingTemplateIDUsed             *int
	PricingTemplateNameSnapshot       *string
	PricingTemplateRevisionIDUsed     *int64
	PricingConfigVersionUsed          *int
	PricingVersionEffectiveAt         *time.Time
	ReportingCurrencyEpoch            *int
	PricingSnapshotUnit               *string
	PricingSnapshotInput              *string
	PricingSnapshotOutput             *string
	PricingSnapshotCacheReadInput     *string
	PricingSnapshotCacheCreationInput *string
	PricingSnapshotReasoning          *string
	PricingTierApplied                *string
	PricingTierThresholdTokens        *int
	PricingTierBasisTokens            *int64
}

// GetRequestLogDetailV2 loads the exact v2 detail for one request-log row.
func GetRequestLogDetailV2(ctx context.Context, exec queryExecutor, profileID int, requestLogID int64) (*RequestLogDetailResponseV2, bool, error) {
	row, found, err := loadRequestLogDetailRowV2(ctx, exec, profileID, requestLogID)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	_, currentEndpointsByID, err := loadCurrentEndpoints(ctx, exec, profileID)
	if err != nil {
		return nil, false, err
	}
	_, currentModelsByID, err := loadRequestLogModels(ctx, exec, profileID)
	if err != nil {
		return nil, false, err
	}
	rules, err := loadCompiledUserAgentRules(ctx, exec, profileID)
	if err != nil {
		return nil, false, err
	}
	connectionCatalog, err := loadCurrentConnections(ctx, exec, profileID)
	if err != nil {
		return nil, false, err
	}
	currentEndpoint, _ := endpointFromMap(currentEndpointsByID, row.EndpointID)
	callerClientDisplay := classifyUserAgentDisplay(row.CallerUserAgent, rules)
	upstreamClientDisplay := classifyUserAgentDisplay(row.UpstreamUserAgent, rules)

	// Canonical refs: terminal target + endpoint from current tables.
	terminalTarget := buildDetailTerminalTarget(connectionCatalog, row)
	endpoint := buildDetailEndpoint(ctx, exec, profileID, currentEndpoint, row)
	initialTarget := buildDetailInitialTarget(connectionCatalog, row)

	pricing := buildDetailPricing(row)
	legacyEvidence := buildDetailLegacyPricingEvidence(row)
	currentTemplate := buildDetailCurrentPricingTemplate(ctx, exec, profileID, row)

	response := &RequestLogDetailResponseV2{
		Summary: RequestLogDetailSummaryV2{
			RequestLogID:             fmt.Sprintf("%d", row.ID),
			CreatedAt:                row.CreatedAt.UTC(),
			ModelID:                  row.ModelID,
			ModelLabel:               resolveRequestLogModelLabel(currentModelsByID, row.ModelID),
			ResolvedTargetModelID:    row.ResolvedTargetModelID,
			ResolvedTargetModelLabel: resolveRequestLogResolvedTargetModelLabel(currentModelsByID, row.ResolvedTargetModelID),
			APIFamily:                row.APIFamily,
			RowKind:                  row.RowKind,
			UpstreamStatusCode:       row.UpstreamStatusCode,
			GatewayStatusCode:        row.GatewayStatusCode,
			LegacyStatusCode:         row.LegacyStatusCode,
			AttemptDurationMS:        row.AttemptDurationMS,
			LegacyDurationMS:         row.LegacyDurationMS,
			TTFTMS:                   row.TTFTMS,
			CompletionDurationMS:     row.CompletionDurationMS,
			IsStream:                 row.IsStream,
			StreamOutcome:            row.StreamOutcome,
			StreamErrorKind:          row.StreamErrorKind,
			AttemptNumber:            row.AttemptNumber,
			AttemptTrigger:           row.AttemptTrigger,
			AttemptResult:            row.AttemptResult,
			IsWinner:                 row.IsWinner,
		},
		Request: RequestLogDetailRequestV2{
			OperationName:                    row.OperationName,
			UpstreamOperationName:            row.UpstreamOperationName,
			OperationTranslationMode:         row.OperationTranslationMode,
			RequestPath:                      row.RequestPath,
			UpstreamRequestPath:              row.UpstreamRequestPath,
			IngressRequestID:                 row.IngressRequestID,
			ProviderCorrelationID:            row.ProviderCorrelationID,
			ProxyAPIKeyID:                    row.ProxyAPIKeyID,
			ProxyAPIKeyNameSnapshot:          row.ProxyAPIKeyNameSnapshot,
			ProxyAPIKeyAttributionState:      row.ProxyAPIKeyAttributionState,
			ProxyAPIKeyAuthEnforcedAtRequest: row.ProxyAPIKeyAuthEnforcedAtRequest,
			CallerUserAgent:                  row.CallerUserAgent,
			UpstreamUserAgent:                row.UpstreamUserAgent,
			CallerClientDisplay:              callerClientDisplay,
			UpstreamClientDisplay:            upstreamClientDisplay,
			UserAgentOverridden:              userAgentOverridden(row.CallerUserAgent, row.UpstreamUserAgent),
			RequestGenerationParams:          row.RequestGenerationParams,
			RequestGenerationParamsStatus:    row.RequestGenerationParamsStatus,
			MetadataRedactedFields:           row.MetadataRedactedFields,
			MetadataTruncatedFields:          row.MetadataTruncatedFields,
			URLScrubProvenance:               row.URLScrubProvenance,
		},
		Routing: RequestLogDetailRoutingV2{
			ProfileID:                   row.ProfileID,
			EndpointLabel:               resolveEndpointLabel(currentEndpoint.Name, currentEndpoint.BaseURL, row.EndpointBaseURL, row.EndpointID, "Unknown Endpoint"),
			EndpointID:                  row.EndpointID,
			TerminalTargetID:            row.ConnectionID,
			SelectedTerminalTargetID:    row.SelectedTerminalTargetID,
			EndpointBaseURL:             row.EndpointBaseURL,
			EndpointDescription:         row.EndpointDescription,
			AuditEnabledAtRequest:       row.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: row.AuditCaptureBodiesAtRequest,
		},
		Usage: RequestLogDetailUsageV2{
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			TotalTokens:              row.TotalTokens,
			SuccessFlag:              row.SuccessFlag,
			CacheReadInputTokens:     row.CacheReadInputTokens,
			CacheCreationInputTokens: row.CacheCreationInputTokens,
			ReasoningTokens:          row.ReasoningTokens,
		},
		Failure:                buildDetailFailureProjection(row),
		TerminalTarget:         terminalTarget,
		Endpoint:               endpoint,
		RoutingProvenance:      RoutingProvenanceDetail{InitialTerminalTarget: initialTarget, DiffersFromActual: initialTarget != nil && terminalTarget != nil && initialTarget.TerminalTargetID != terminalTarget.TerminalTargetID},
		Pricing:                pricing,
		LegacyPricingEvidence:  legacyEvidence,
		CurrentPricingTemplate: currentTemplate,
	}
	return response, true, nil
}

// failureCategoryFor derives the canonical failure category from typed fields.
func failureCategoryFor(row requestLogDetailRowV2) string {
	switch row.RowKind {
	case "planning":
		return "planning"
	case "admission":
		return "admission"
	}
	result := ""
	if row.AttemptResult != nil {
		result = *row.AttemptResult
	}
	switch result {
	case "http_error":
		return "upstream_http"
	case "transport_error":
		return "transport"
	case "stream_error":
		return "provider_stream"
	case "client_disconnected":
		return "client_disconnect"
	case "cancelled":
		return ""
	default:
		if row.ErrorCode != nil || row.ErrorDetail != nil || row.StreamErrorDetail != nil {
			return "unknown"
		}
		return ""
	}
}

func buildDetailFailureProjection(row requestLogDetailRowV2) *FailureProjectionDetail {
	category := failureCategoryFor(row)
	if category == "" {
		return nil
	}
	detail := row.ErrorDetail
	source := "error_detail"
	redacted := row.ErrorDetailRedacted
	truncated := row.ErrorDetailTruncated
	if detail == nil && row.StreamErrorDetail != nil {
		detail = row.StreamErrorDetail
		source = "stream_error_detail"
		redacted = row.StreamErrorDetailRedacted
		truncated = row.StreamErrorDetailTruncated
	}
	streamOutcome := row.StreamOutcome
	evidenceState := "authoritative"
	if detail == nil && row.ErrorCode == nil && row.FailureStage == nil {
		evidenceState = "unavailable"
	}
	projection := &FailureProjectionDetail{
		Category:                   category,
		Source:                     row.ErrorSource,
		Stage:                      row.FailureStage,
		Code:                       row.ErrorCode,
		Detail:                     detail,
		DetailRedacted:             redacted,
		DetailTruncated:            truncated,
		DetailSource:               source,
		EvidenceState:              evidenceState,
		UpstreamRequestStarted:     row.UpstreamRequestStarted,
		ResponseHeadersReceived:    row.ResponseHeadersReceived,
		FirstBodyOrStreamEventSeen: row.FirstBodyOrStreamEventSeen,
		StreamOutcome:              &streamOutcome,
		StreamErrorKind:            row.StreamErrorKind,
		StreamErrorDetail:          row.StreamErrorDetail,
	}
	return projection
}

func buildDetailPricing(row requestLogDetailRowV2) PricingProjectionDetail {
	attribution := "identified"
	if row.ReportingCurrencyEpoch == nil && row.ReportCurrencyCode == nil {
		attribution = "legacy_unknown"
	}
	var costSegmentKey *string
	if row.ReportingCurrencyEpoch != nil {
		key := fmt.Sprintf("e.%d", *row.ReportingCurrencyEpoch)
		costSegmentKey = &key
	} else if row.ReportCurrencyCode != nil {
		key := "l." + strings.ToUpper(*row.ReportCurrencyCode)
		costSegmentKey = &key
	} else {
		key := "l.__unknown__"
		costSegmentKey = &key
	}
	projection := PricingProjectionDetail{
		PricingStatus:                     row.PricingStatus,
		UnpricedReason:                    row.UnpricedReason,
		PricingResolutionKind:             row.PricingResolutionKind,
		MissingPriceComponents:            row.MissingPriceComponents,
		PricingEvidenceTrust:              row.PricingEvidenceTrust,
		TotalCostUserCurrencyMicros:       row.TotalCostUserCurrencyMicros,
		TotalCostOriginalMicros:           row.TotalCostOriginalMicros,
		CurrencyCodeOriginal:              row.CurrencyCodeOriginal,
		FXRateUsed:                        row.FXRateUsed,
		FXRateSource:                      row.FXRateSource,
		ReportCurrencyCode:                row.ReportCurrencyCode,
		ReportCurrencySymbol:              row.ReportCurrencySymbol,
		ReportingCurrencyEpoch:            row.ReportingCurrencyEpoch,
		CurrencyAttribution:               attribution,
		CostSegmentKey:                    costSegmentKey,
		PricingTemplateIDUsed:             row.PricingTemplateIDUsed,
		PricingTemplateNameSnapshot:       row.PricingTemplateNameSnapshot,
		PricingTemplateRevisionIDUsed:     row.PricingTemplateRevisionIDUsed,
		PricingConfigVersionUsed:          row.PricingConfigVersionUsed,
		PricingVersionEffectiveAt:         row.PricingVersionEffectiveAt,
		PricingSnapshotUnit:               row.PricingSnapshotUnit,
		PricingSnapshotInput:              row.PricingSnapshotInput,
		PricingSnapshotOutput:             row.PricingSnapshotOutput,
		PricingSnapshotCacheReadInput:     row.PricingSnapshotCacheReadInput,
		PricingSnapshotCacheCreationInput: row.PricingSnapshotCacheCreationInput,
		PricingSnapshotReasoning:          row.PricingSnapshotReasoning,
		PricingTierApplied:                row.PricingTierApplied,
		PricingTierThresholdTokens:        row.PricingTierThresholdTokens,
		PricingTierBasisTokens:            row.PricingTierBasisTokens,
		EvidenceState:                     "authoritative",
	}
	if projection.PricingStatus == "" {
		projection.EvidenceState = "unavailable"
	}
	return projection
}

func buildDetailLegacyPricingEvidence(row requestLogDetailRowV2) *LegacyPricingEvidenceDetail {
	if row.PricingEvidenceTrust != "legacy_untrusted" {
		return nil
	}
	components := map[string]string{}
	appendComponent := func(name string, value *int64) {
		if value != nil {
			components[name] = fmt.Sprintf("%d", *value)
		}
	}
	appendComponent("input", row.InputCostMicros)
	appendComponent("output", row.OutputCostMicros)
	appendComponent("cache_read_input", row.CacheReadInputCostMicros)
	appendComponent("cache_creation_input", row.CacheCreationInputCostMicros)
	appendComponent("reasoning", row.ReasoningCostMicros)
	snapshots := map[string]string{}
	appendSnapshot := func(name string, value *string) {
		if value != nil {
			snapshots[name] = *value
		}
	}
	appendSnapshot("input", row.PricingSnapshotInput)
	appendSnapshot("output", row.PricingSnapshotOutput)
	appendSnapshot("cache_read_input", row.PricingSnapshotCacheReadInput)
	appendSnapshot("cache_creation_input", row.PricingSnapshotCacheCreationInput)
	appendSnapshot("reasoning", row.PricingSnapshotReasoning)
	return &LegacyPricingEvidenceDetail{
		RawTotalCostOriginalMicros: row.TotalCostOriginalMicros,
		RawTotalCostReportMicros:   row.TotalCostUserCurrencyMicros,
		RawComponentCostMicros:     components,
		RawPriceSnapshots:          snapshots,
		OriginalCurrencyCode:       row.CurrencyCodeOriginal,
		ReportCurrencyCode:         row.ReportCurrencyCode,
		WarningCode:                "historical_unverified",
	}
}

func buildDetailTerminalTarget(catalog map[int]connectionRecord, row requestLogDetailRowV2) *TerminalTargetProjectionDetail {
	if row.ConnectionID == nil {
		return nil
	}
	target := resolveTerminalTargetProjection(catalog, row.ConnectionID)
	projection := &TerminalTargetProjectionDetail{
		Kind:               "terminal_target",
		TerminalTargetID:   fmt.Sprintf("%d", *row.ConnectionID),
		OwnerModelConfigID: target.OwnerModelID,
		Name:               target.Label,
		Configured:         target.Configured,
		Deleted:            target.Deleted,
		NameSource:         "current",
	}
	if target.Deleted {
		projection.NameSource = "unavailable"
	}
	return projection
}

func buildDetailInitialTarget(catalog map[int]connectionRecord, row requestLogDetailRowV2) *TerminalTargetProjectionDetail {
	if row.SelectedTerminalTargetID == nil {
		return nil
	}
	target := resolveTerminalTargetProjection(catalog, row.SelectedTerminalTargetID)
	projection := &TerminalTargetProjectionDetail{
		Kind:               "terminal_target",
		TerminalTargetID:   fmt.Sprintf("%d", *row.SelectedTerminalTargetID),
		OwnerModelConfigID: target.OwnerModelID,
		Name:               target.Label,
		Configured:         target.Configured,
		Deleted:            target.Deleted,
		NameSource:         "current",
	}
	if target.Deleted {
		projection.NameSource = "unavailable"
	}
	return projection
}

func buildDetailEndpoint(ctx context.Context, exec queryExecutor, profileID int, current endpointRecord, row requestLogDetailRowV2) *EndpointProjectionDetail {
	if row.EndpointID == nil {
		return nil
	}
	name := ""
	deleted := true
	configured := false
	if current.ID == *row.EndpointID {
		deleted = false
		configured = true
		if current.Name != nil {
			name = *current.Name
		}
	}
	projection := &EndpointProjectionDetail{
		Kind:       "endpoint",
		ID:         fmt.Sprintf("%d", *row.EndpointID),
		NameSource: "current",
		Deleted:    deleted,
		Configured: configured,
	}
	if name != "" {
		projection.Name = &name
	}
	return projection
}

func buildDetailCurrentPricingTemplate(ctx context.Context, exec queryExecutor, profileID int, row requestLogDetailRowV2) *CurrentPricingTemplateDetail {
	if row.PricingTemplateIDUsed == nil {
		return nil
	}
	var deletedAt *time.Time
	var revisionID *int64
	var version *int
	var effectiveAt *time.Time
	err := exec.QueryRow(ctx, `SELECT templates.deleted_at, revisions.id, revisions.version, revisions.effective_at
		FROM pricing_templates AS templates
		LEFT JOIN pricing_template_revisions AS revisions ON revisions.id = templates.current_revision_id
		WHERE templates.id = $1 AND templates.profile_id = $2`, *row.PricingTemplateIDUsed, profileID).Scan(&deletedAt, &revisionID, &version, &effectiveAt)
	if err == pgx.ErrNoRows {
		return &CurrentPricingTemplateDetail{
			TemplateID: *row.PricingTemplateIDUsed,
			Deleted:    true,
		}
	}
	if err != nil {
		return nil
	}
	detail := &CurrentPricingTemplateDetail{
		TemplateID: *row.PricingTemplateIDUsed,
		Deleted:    deletedAt != nil,
	}
	if revisionID != nil {
		detail.CurrentRevisionID = fmt.Sprintf("%d", *revisionID)
	}
	if version != nil {
		detail.CurrentVersion = *version
	}
	if effectiveAt != nil {
		currentEffectiveAt := effectiveAt.UTC()
		detail.CurrentEffectiveAt = &currentEffectiveAt
	}
	if row.PricingTemplateRevisionIDUsed != nil {
		detail.MatchesRequestRevision = revisionID != nil && *row.PricingTemplateRevisionIDUsed == *revisionID
	}
	return detail
}

func loadRequestLogDetailRowV2(ctx context.Context, exec queryExecutor, profileID int, requestLogID int64) (requestLogDetailRowV2, bool, error) {
	row := exec.QueryRow(
		ctx,
		`SELECT profile_id, id, created_at, model_id, resolved_target_model_id, api_family, row_kind,
		 upstream_status_code, gateway_status_code, legacy_status_code, attempt_duration_ms, legacy_duration_ms,
		 ttft_ms, completion_duration_ms, is_stream, stream_outcome, stream_error_kind,
		 attempt_number, attempt_trigger, attempt_result, is_winner,
		 operation_name, upstream_operation_name, operation_translation_mode, request_path, upstream_request_path,
			 ingress_request_id, provider_correlation_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot,
		 proxy_api_key_attribution_state, proxy_api_key_auth_enforced_at_request,
		 caller_user_agent, upstream_user_agent,
		 error_detail, error_detail_redacted, error_detail_truncated, error_source, error_code, failure_stage,
		 upstream_request_started, response_headers_received, first_body_or_stream_event_seen,
		 stream_error_detail, stream_error_detail_redacted, stream_error_detail_truncated,
		 request_generation_params, request_generation_params_status,
		 metadata_redacted_fields, metadata_truncated_fields, url_scrub_provenance,
		 endpoint_id, connection_id, selected_terminal_target_id, endpoint_base_url, endpoint_description,
		 audit_enabled_at_request, audit_capture_bodies_at_request,
		 input_tokens, output_tokens, total_tokens, success_flag,
		 pricing_status, pricing_evidence_trust, unpriced_reason, pricing_resolution_kind, missing_price_components,
		 cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
		 input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros,
		 total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol,
		 fx_rate_used, fx_rate_source,
		 pricing_template_id_used, pricing_template_name_snapshot, pricing_template_revision_id_used, pricing_config_version_used,
		 pricing_version_effective_at, reporting_currency_epoch,
		 pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input,
		 pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning,
		 pricing_tier_applied, pricing_tier_threshold_tokens, pricing_tier_basis_tokens
		 FROM request_logs
		 WHERE profile_id = $1 AND id = $2
		 ORDER BY created_at DESC
		 LIMIT 1`,
		profileID,
		requestLogID,
	)
	record, err := scanRequestLogDetailRowV2(row)
	if err == pgx.ErrNoRows {
		return requestLogDetailRowV2{}, false, nil
	}
	if err != nil {
		return requestLogDetailRowV2{}, false, fmt.Errorf("load request log %d for profile %d: %w", requestLogID, profileID, err)
	}
	return record, true, nil
}

func scanRequestLogDetailRowV2(scanner interface{ Scan(...any) error }) (requestLogDetailRowV2, error) {
	var resolvedTargetModelID sql.NullString
	var upstreamStatusCode, gatewayStatusCode, legacyStatusCode sql.NullInt64
	var attemptDurationMS, legacyDurationMS, ttftMS, completionDurationMS, attemptNumber sql.NullInt32
	var streamOutcome, streamErrorKind, attemptTrigger, attemptResult sql.NullString
	var isWinner sql.NullBool
	var operationName, upstreamOperationName, operationTranslationMode, upstreamRequestPath, ingressRequestID sql.NullString
	var providerCorrelationID, proxyAPIKeyNameSnapshot, proxyAPIKeyAttributionState, callerUserAgent, upstreamUserAgent sql.NullString
	var proxyAPIKeyAuthEnforced sql.NullBool
	var proxyAPIKeyID, endpointID, connectionID, selectedTerminalTargetID sql.NullInt32
	var errorDetail, errorSource, errorCode, failureStage, streamErrorDetail sql.NullString
	var errorDetailRedacted, errorDetailTruncated, streamErrorDetailRedacted, streamErrorDetailTruncated bool
	var upstreamRequestStarted, responseHeadersReceived, firstBodyOrStreamEventSeen sql.NullBool
	var requestGenerationParams []byte
	var requestGenerationParamsStatus sql.NullString
	var metadataRedactedFields, metadataTruncatedFields []string
	var urlScrubProvenance sql.NullString
	var endpointBaseURL, endpointDescription sql.NullString
	var inputTokens, outputTokens, totalTokens, cacheReadInputTokens, cacheCreationInputTokens, reasoningTokens sql.NullInt32
	var successFlag sql.NullBool
	var pricingStatus, pricingEvidenceTrust, unpricedReason, pricingResolutionKind sql.NullString
	var missingPriceComponents []string
	var inputCostMicros, outputCostMicros, cacheReadInputCostMicros, cacheCreationInputCostMicros, reasoningCostMicros sql.NullInt64
	var totalCostOriginalMicros, totalCostUserCurrencyMicros sql.NullInt64
	var currencyCodeOriginal, reportCurrencyCode, reportCurrencySymbol, fxRateUsed, fxRateSource sql.NullString
	var pricingTemplateIDUsed, pricingConfigVersionUsed, reportingCurrencyEpoch sql.NullInt32
	var pricingTemplateNameSnapshot sql.NullString
	var pricingTemplateRevisionIDUsed sql.NullInt64
	var pricingVersionEffectiveAt *time.Time
	var pricingSnapshotUnit, pricingSnapshotInput, pricingSnapshotOutput, pricingSnapshotCacheReadInput, pricingSnapshotCacheCreationInput, pricingSnapshotReasoning sql.NullString
	var pricingTierApplied sql.NullString
	var pricingTierThreshold sql.NullInt32
	var pricingTierBasis sql.NullInt64

	item := requestLogDetailRowV2{}
	if err := scanner.Scan(
		&item.ProfileID, &item.ID, &item.CreatedAt, &item.ModelID, &resolvedTargetModelID, &item.APIFamily, &item.RowKind,
		&upstreamStatusCode, &gatewayStatusCode, &legacyStatusCode, &attemptDurationMS, &legacyDurationMS,
		&ttftMS, &completionDurationMS, &item.IsStream, &streamOutcome, &streamErrorKind,
		&attemptNumber, &attemptTrigger, &attemptResult, &isWinner,
		&operationName, &upstreamOperationName, &operationTranslationMode, &item.RequestPath, &upstreamRequestPath,
		&ingressRequestID, &providerCorrelationID, &proxyAPIKeyID, &proxyAPIKeyNameSnapshot, &proxyAPIKeyAttributionState, &proxyAPIKeyAuthEnforced,
		&callerUserAgent, &upstreamUserAgent,
		&errorDetail, &errorDetailRedacted, &errorDetailTruncated, &errorSource, &errorCode, &failureStage,
		&upstreamRequestStarted, &responseHeadersReceived, &firstBodyOrStreamEventSeen,
		&streamErrorDetail, &streamErrorDetailRedacted, &streamErrorDetailTruncated,
		&requestGenerationParams, &requestGenerationParamsStatus,
		&metadataRedactedFields, &metadataTruncatedFields, &urlScrubProvenance,
		&endpointID, &connectionID, &selectedTerminalTargetID, &endpointBaseURL, &endpointDescription,
		&item.AuditEnabledAtRequest, &item.AuditCaptureBodiesAtRequest,
		&inputTokens, &outputTokens, &totalTokens, &successFlag,
		&pricingStatus, &pricingEvidenceTrust, &unpricedReason, &pricingResolutionKind, &missingPriceComponents,
		&cacheReadInputTokens, &cacheCreationInputTokens, &reasoningTokens,
		&inputCostMicros, &outputCostMicros, &cacheReadInputCostMicros, &cacheCreationInputCostMicros, &reasoningCostMicros,
		&totalCostOriginalMicros, &totalCostUserCurrencyMicros, &currencyCodeOriginal, &reportCurrencyCode, &reportCurrencySymbol,
		&fxRateUsed, &fxRateSource,
		&pricingTemplateIDUsed, &pricingTemplateNameSnapshot, &pricingTemplateRevisionIDUsed, &pricingConfigVersionUsed,
		&pricingVersionEffectiveAt, &reportingCurrencyEpoch,
		&pricingSnapshotUnit, &pricingSnapshotInput, &pricingSnapshotOutput, &pricingSnapshotCacheReadInput,
		&pricingSnapshotCacheCreationInput, &pricingSnapshotReasoning,
		&pricingTierApplied, &pricingTierThreshold, &pricingTierBasis,
	); err != nil {
		return requestLogDetailRowV2{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.ResolvedTargetModelID = nullableString(resolvedTargetModelID)
	item.RowKind = stringValue(normalizeOptionalString(nonNullString(item.RowKind)))
	item.UpstreamStatusCode = nullableInt64AsInt(upstreamStatusCode)
	item.GatewayStatusCode = nullableInt64AsInt(gatewayStatusCode)
	item.LegacyStatusCode = nullableInt64AsInt(legacyStatusCode)
	item.AttemptDurationMS = nullableInt32(attemptDurationMS)
	item.LegacyDurationMS = nullableInt32(legacyDurationMS)
	item.TTFTMS = nullableInt32(ttftMS)
	item.CompletionDurationMS = nullableInt32(completionDurationMS)
	item.StreamOutcome = normalizeRequestLogStreamOutcome(nullableString(streamOutcome), item.IsStream, item.CompletionDurationMS)
	item.StreamErrorKind = normalizeOptionalString(nullableString(streamErrorKind))
	item.AttemptNumber = nullableInt32(attemptNumber)
	item.AttemptTrigger = normalizeOptionalString(nullableString(attemptTrigger))
	item.AttemptResult = normalizeOptionalString(nullableString(attemptResult))
	item.IsWinner = nullableBool(isWinner)
	item.OperationName = normalizeOptionalString(nullableString(operationName))
	item.UpstreamOperationName = normalizeOptionalString(nullableString(upstreamOperationName))
	item.OperationTranslationMode = normalizeOptionalString(nullableString(operationTranslationMode))
	item.UpstreamRequestPath = normalizeOptionalString(nullableString(upstreamRequestPath))
	item.IngressRequestID = nullableString(ingressRequestID)
	item.ProviderCorrelationID = nullableString(providerCorrelationID)
	item.ProxyAPIKeyID = nullableInt32(proxyAPIKeyID)
	item.ProxyAPIKeyNameSnapshot = nullableString(proxyAPIKeyNameSnapshot)
	item.ProxyAPIKeyAttributionState = normalizeOptionalString(nullableString(proxyAPIKeyAttributionState))
	item.ProxyAPIKeyAuthEnforcedAtRequest = nullableBool(proxyAPIKeyAuthEnforced)
	item.CallerUserAgent = nullableString(callerUserAgent)
	item.UpstreamUserAgent = nullableString(upstreamUserAgent)
	item.ErrorDetail = nullableString(errorDetail)
	item.ErrorDetailRedacted = errorDetailRedacted
	item.ErrorDetailTruncated = errorDetailTruncated
	item.ErrorSource = normalizeOptionalString(nullableString(errorSource))
	item.ErrorCode = normalizeOptionalString(nullableString(errorCode))
	item.FailureStage = normalizeOptionalString(nullableString(failureStage))
	item.UpstreamRequestStarted = nullableBool(upstreamRequestStarted)
	item.ResponseHeadersReceived = nullableBool(responseHeadersReceived)
	item.FirstBodyOrStreamEventSeen = nullableBool(firstBodyOrStreamEventSeen)
	item.StreamErrorDetail = nullableString(streamErrorDetail)
	item.StreamErrorDetailRedacted = streamErrorDetailRedacted
	item.StreamErrorDetailTruncated = streamErrorDetailTruncated
	item.RequestGenerationParams = nullableJSONRawMessage(requestGenerationParams)
	item.RequestGenerationParamsStatus = nullableString(requestGenerationParamsStatus)
	item.MetadataRedactedFields = metadataRedactedFields
	item.MetadataTruncatedFields = metadataTruncatedFields
	item.URLScrubProvenance = stringValue(normalizeOptionalString(nullableString(urlScrubProvenance)))
	item.EndpointID = nullableInt32(endpointID)
	item.ConnectionID = nullableInt32(connectionID)
	item.SelectedTerminalTargetID = nullableInt32(selectedTerminalTargetID)
	item.EndpointBaseURL = nullableString(endpointBaseURL)
	item.EndpointDescription = nullableString(endpointDescription)
	item.InputTokens = nullableInt32(inputTokens)
	item.OutputTokens = nullableInt32(outputTokens)
	item.TotalTokens = nullableInt32(totalTokens)
	item.SuccessFlag = nullableBool(successFlag)
	item.PricingStatus = stringValue(nullableString(pricingStatus))
	item.PricingEvidenceTrust = stringValue(nullableString(pricingEvidenceTrust))
	item.UnpricedReason = nullableString(unpricedReason)
	item.PricingResolutionKind = nullableString(pricingResolutionKind)
	item.MissingPriceComponents = missingPriceComponents
	item.CacheReadInputTokens = nullableInt32(cacheReadInputTokens)
	item.CacheCreationInputTokens = nullableInt32(cacheCreationInputTokens)
	item.ReasoningTokens = nullableInt32(reasoningTokens)
	item.InputCostMicros = nullableInt64(inputCostMicros)
	item.OutputCostMicros = nullableInt64(outputCostMicros)
	item.CacheReadInputCostMicros = nullableInt64(cacheReadInputCostMicros)
	item.CacheCreationInputCostMicros = nullableInt64(cacheCreationInputCostMicros)
	item.ReasoningCostMicros = nullableInt64(reasoningCostMicros)
	item.TotalCostOriginalMicros = nullableInt64(totalCostOriginalMicros)
	item.TotalCostUserCurrencyMicros = nullableInt64(totalCostUserCurrencyMicros)
	item.CurrencyCodeOriginal = nullableString(currencyCodeOriginal)
	item.ReportCurrencyCode = nullableString(reportCurrencyCode)
	item.ReportCurrencySymbol = nullableString(reportCurrencySymbol)
	item.FXRateUsed = nullableString(fxRateUsed)
	item.FXRateSource = nullableString(fxRateSource)
	item.PricingTemplateIDUsed = nullableInt32(pricingTemplateIDUsed)
	item.PricingTemplateNameSnapshot = nullableString(pricingTemplateNameSnapshot)
	item.PricingTemplateRevisionIDUsed = nullableInt64(pricingTemplateRevisionIDUsed)
	item.PricingConfigVersionUsed = nullableInt32(pricingConfigVersionUsed)
	item.PricingVersionEffectiveAt = pricingVersionEffectiveAt
	item.ReportingCurrencyEpoch = nullableInt32(reportingCurrencyEpoch)
	item.PricingSnapshotUnit = nullableString(pricingSnapshotUnit)
	item.PricingSnapshotInput = nullableString(pricingSnapshotInput)
	item.PricingSnapshotOutput = nullableString(pricingSnapshotOutput)
	item.PricingSnapshotCacheReadInput = nullableString(pricingSnapshotCacheReadInput)
	item.PricingSnapshotCacheCreationInput = nullableString(pricingSnapshotCacheCreationInput)
	item.PricingSnapshotReasoning = nullableString(pricingSnapshotReasoning)
	item.PricingTierApplied = nullableString(pricingTierApplied)
	item.PricingTierThresholdTokens = nullableInt32(pricingTierThreshold)
	item.PricingTierBasisTokens = nullableInt64(pricingTierBasis)
	return item, nil
}

func nonNullString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nullableInt64AsInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	n := int(value.Int64)
	return &n
}
