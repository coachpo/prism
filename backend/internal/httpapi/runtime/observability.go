package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	gatewayaccounting "github.com/coachpo/prism/backend/internal/gateway/accounting"
	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	anthropicprovider "github.com/coachpo/prism/backend/internal/gateway/provider/anthropic"
	geminiprovider "github.com/coachpo/prism/backend/internal/gateway/provider/gemini"
	"github.com/coachpo/prism/backend/internal/httpapi/proxykeyusage"
	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

// responseUsage is Prism's canonical runtime usage model. InputTokens and
// OutputTokens are base-token dimensions, cache and reasoning tokens are
// separate optional dimensions, provider TotalTokens is preserved when present,
// and cached token counts stay derived on aggregate or presentation surfaces.
type responseUsage struct {
	InputTokens              *int
	OutputTokens             *int
	TotalTokens              *int
	CacheReadInputTokens     *int
	CacheCreationInputTokens *int
	ReasoningTokens          *int
	discarded                bool
}

type runtimeUsagePayloadShape string

const (
	runtimeUsagePayloadShapeStandard              runtimeUsagePayloadShape = "standard"
	runtimeUsagePayloadShapeAnthropicMessages     runtimeUsagePayloadShape = "anthropic_messages"
	runtimeUsagePayloadShapeOpenAIChatCompletions runtimeUsagePayloadShape = "openai_chat_completions"
	runtimeUsagePayloadShapeOpenAIResponses       runtimeUsagePayloadShape = "openai_responses"
	runtimeUsagePayloadShapeOpenAIImages          runtimeUsagePayloadShape = "openai_images"
	runtimeUsagePayloadShapeGemini                runtimeUsagePayloadShape = "gemini"
)

type runtimeUsageCarrier uint8

const (
	runtimeUsageCarrierNone runtimeUsageCarrier = iota
	runtimeUsageCarrierRootUsage
	runtimeUsageCarrierResponseUsage
	runtimeUsageCarrierMessageUsage
	runtimeUsageCarrierRootUsageMetadata
)

type runtimeUsageCarrierMask uint8

const (
	runtimeUsageCarrierMaskRootUsage runtimeUsageCarrierMask = 1 << iota
	runtimeUsageCarrierMaskResponseUsage
	runtimeUsageCarrierMaskMessageUsage
	runtimeUsageCarrierMaskRootUsageMetadata
)

type runtimeUsageNormalizationRule struct {
	Provider                       string
	OperationName                  string
	PayloadShape                   runtimeUsagePayloadShape
	AllowedCarriers                runtimeUsageCarrierMask
	AllowDerivedTotal              bool
	PreserveProviderTotal          bool
	ValidateParentSplitBounds      bool
	ValidateDisjointComponentTotal bool
}

var (
	runtimeUsageRuleOpenAIChatCompletions = runtimeUsageNormalizationRule{
		Provider:                       "openai",
		OperationName:                  "openai.chat_completions",
		PayloadShape:                   runtimeUsagePayloadShapeOpenAIChatCompletions,
		AllowedCarriers:                runtimeUsageCarrierMaskRootUsage,
		AllowDerivedTotal:              true,
		PreserveProviderTotal:          true,
		ValidateParentSplitBounds:      false,
		ValidateDisjointComponentTotal: true,
	}
	runtimeUsageRuleOpenAIResponses = runtimeUsageNormalizationRule{
		Provider:                       "openai",
		OperationName:                  "openai.responses",
		PayloadShape:                   runtimeUsagePayloadShapeOpenAIResponses,
		AllowedCarriers:                runtimeUsageCarrierMaskRootUsage | runtimeUsageCarrierMaskResponseUsage,
		AllowDerivedTotal:              true,
		PreserveProviderTotal:          true,
		ValidateParentSplitBounds:      false,
		ValidateDisjointComponentTotal: true,
	}
	// Both image operations share one rule. ImagesUsage carries no cache or
	// reasoning component, so the canonical disjoint split is input plus output
	// and the provider total is preserved as authoritative.
	runtimeUsageRuleOpenAIImages = runtimeUsageNormalizationRule{
		Provider:                       "openai",
		OperationName:                  "openai.images",
		PayloadShape:                   runtimeUsagePayloadShapeOpenAIImages,
		AllowedCarriers:                runtimeUsageCarrierMaskRootUsage,
		AllowDerivedTotal:              true,
		PreserveProviderTotal:          true,
		ValidateParentSplitBounds:      false,
		ValidateDisjointComponentTotal: true,
	}
	runtimeUsageRuleAnthropicMessages = runtimeUsageNormalizationRule{
		Provider:                       "anthropic",
		OperationName:                  "anthropic.messages",
		PayloadShape:                   runtimeUsagePayloadShapeAnthropicMessages,
		AllowedCarriers:                runtimeUsageCarrierMaskRootUsage | runtimeUsageCarrierMaskMessageUsage,
		AllowDerivedTotal:              true,
		PreserveProviderTotal:          true,
		ValidateDisjointComponentTotal: true,
	}
	runtimeUsageRuleGeminiGenerateContent = runtimeUsageNormalizationRule{
		Provider:                       "gemini",
		OperationName:                  "gemini.generate_content",
		PayloadShape:                   runtimeUsagePayloadShapeGemini,
		AllowedCarriers:                runtimeUsageCarrierMaskRootUsageMetadata,
		AllowDerivedTotal:              true,
		PreserveProviderTotal:          true,
		ValidateDisjointComponentTotal: true,
	}
	runtimeUsageRuleGeminiStreamGenerateContent = runtimeUsageNormalizationRule{
		Provider:                       "gemini",
		OperationName:                  "gemini.stream_generate_content",
		PayloadShape:                   runtimeUsagePayloadShapeGemini,
		AllowedCarriers:                runtimeUsageCarrierMaskRootUsageMetadata,
		AllowDerivedTotal:              true,
		PreserveProviderTotal:          true,
		ValidateDisjointComponentTotal: true,
	}
)

func (usage responseUsage) hasValues() bool {
	if usage.discarded {
		return false
	}
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil || usage.ReasoningTokens != nil
}

func (usage responseUsage) normalized() responseUsage {
	if usage.discarded {
		return responseUsage{}
	}
	if usage.TotalTokens == nil && usage.hasTokenComponents() {
		total := usage.componentTotal()
		usage.TotalTokens = &total
	}
	return usage
}

func (usage responseUsage) hasTokenComponents() bool {
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil || usage.ReasoningTokens != nil
}

func (usage responseUsage) componentTotal() int {
	return intValue(usage.InputTokens) + intValue(usage.OutputTokens) + intValue(usage.CacheReadInputTokens) + intValue(usage.CacheCreationInputTokens) + intValue(usage.ReasoningTokens)
}

func (rule runtimeUsageNormalizationRule) configured() bool {
	return strings.TrimSpace(rule.Provider) != "" && strings.TrimSpace(rule.OperationName) != "" && rule.PayloadShape != ""
}

func (rule runtimeUsageNormalizationRule) allowsCarrier(carrier runtimeUsageCarrier) bool {
	return rule.configured() && rule.AllowedCarriers&carrier.mask() != 0
}

func (carrier runtimeUsageCarrier) mask() runtimeUsageCarrierMask {
	switch carrier {
	case runtimeUsageCarrierRootUsage:
		return runtimeUsageCarrierMaskRootUsage
	case runtimeUsageCarrierResponseUsage:
		return runtimeUsageCarrierMaskResponseUsage
	case runtimeUsageCarrierMessageUsage:
		return runtimeUsageCarrierMaskMessageUsage
	case runtimeUsageCarrierRootUsageMetadata:
		return runtimeUsageCarrierMaskRootUsageMetadata
	default:
		return 0
	}
}

func (usage *responseUsage) mergeRuntimeUsagePayload(rule runtimeUsageNormalizationRule, carrier runtimeUsageCarrier, usagePayload map[string]any) {
	if usage == nil || usage.discarded || !rule.allowsCarrier(carrier) || usagePayload == nil {
		return
	}
	parsed, ok := parseRuntimeUsagePayload(rule.PayloadShape, usagePayload)
	if !ok {
		return
	}
	merged := *usage
	merged.merge(parsed)
	if !merged.validForRuntimeUsage(rule) {
		*usage = responseUsage{discarded: true}
		return
	}
	*usage = merged
}

func (usage responseUsage) canonicalizedForRuntimeUsage(rule runtimeUsageNormalizationRule) responseUsage {
	if usage.discarded || !usage.validForRuntimeUsage(rule) {
		return responseUsage{}
	}
	if rule.AllowDerivedTotal && usage.TotalTokens == nil && usage.hasTokenComponents() {
		total := usage.componentTotal()
		usage.TotalTokens = &total
	}
	if !usage.validForRuntimeUsage(rule) {
		return responseUsage{}
	}
	return usage
}

func parseRuntimeUsagePayload(shape runtimeUsagePayloadShape, usagePayload map[string]any) (responseUsage, bool) {
	switch shape {
	case runtimeUsagePayloadShapeStandard:
		return parseStandardRuntimeUsagePayload(usagePayload)
	case runtimeUsagePayloadShapeAnthropicMessages:
		return parseAnthropicMessagesUsagePayload(usagePayload)
	case runtimeUsagePayloadShapeOpenAIChatCompletions:
		return parseOpenAIChatCompletionsUsagePayload(usagePayload)
	case runtimeUsagePayloadShapeOpenAIResponses:
		return parseOpenAIResponsesUsagePayload(usagePayload)
	case runtimeUsagePayloadShapeOpenAIImages:
		return parseOpenAIImagesUsagePayload(usagePayload)
	case runtimeUsagePayloadShapeGemini:
		return parseGeminiRuntimeUsagePayload(usagePayload)
	default:
		return responseUsage{}, false
	}
}

func parseStandardRuntimeUsagePayload(usagePayload map[string]any) (responseUsage, bool) {
	usage := responseUsage{}
	if inputTokens := intPointerFromAny(firstValue(usagePayload, "prompt_tokens", "input_tokens")); inputTokens != nil {
		usage.InputTokens = inputTokens
	}
	if outputTokens := intPointerFromAny(firstValue(usagePayload, "completion_tokens", "output_tokens")); outputTokens != nil {
		usage.OutputTokens = outputTokens
	}
	if totalTokens := intPointerFromAny(usagePayload["total_tokens"]); totalTokens != nil {
		usage.TotalTokens = totalTokens
	}
	if cacheReadTokens := intPointerFromAny(usagePayload["cache_read_input_tokens"]); cacheReadTokens != nil {
		usage.CacheReadInputTokens = cacheReadTokens
	}
	if cacheCreationTokens := intPointerFromAny(usagePayload["cache_creation_input_tokens"]); cacheCreationTokens != nil {
		usage.CacheCreationInputTokens = cacheCreationTokens
	}
	reasoningTokens := intPointerFromAny(nestedValue(usagePayload, "completion_tokens_details", "reasoning_tokens"))
	if reasoningTokens == nil {
		reasoningTokens = intPointerFromAny(nestedValue(usagePayload, "output_tokens_details", "reasoning_tokens"))
	}
	if reasoningTokens != nil {
		usage.ReasoningTokens = reasoningTokens
	}
	return usage, usage.hasValues()
}

func parseAnthropicMessagesUsagePayload(usagePayload map[string]any) (responseUsage, bool) {
	usage := responseUsageFromProviderUsageEnvelope(anthropicprovider.ParseMessagesUsagePayload(usagePayload))
	return usage, usage.hasValues()
}

func parseOpenAIChatCompletionsUsagePayload(usagePayload map[string]any) (responseUsage, bool) {
	inputTokens := intPointerFromAny(usagePayload["prompt_tokens"])
	cacheReadTokens := intPointerFromAny(nestedValue(usagePayload, "prompt_tokens_details", "cached_tokens"))
	outputTokens := intPointerFromAny(usagePayload["completion_tokens"])
	reasoningTokens := intPointerFromAny(nestedValue(usagePayload, "completion_tokens_details", "reasoning_tokens"))
	totalTokens := intPointerFromAny(usagePayload["total_tokens"])
	usage := usageFromParentTotals(inputTokens, cacheReadTokens, outputTokens, reasoningTokens, totalTokens)
	return usage, usage.hasValues()
}

func parseOpenAIResponsesUsagePayload(usagePayload map[string]any) (responseUsage, bool) {
	inputTokens := intPointerFromAny(usagePayload["input_tokens"])
	cacheReadTokens := intPointerFromAny(nestedValue(usagePayload, "input_tokens_details", "cached_tokens"))
	outputTokens := intPointerFromAny(usagePayload["output_tokens"])
	reasoningTokens := intPointerFromAny(nestedValue(usagePayload, "output_tokens_details", "reasoning_tokens"))
	totalTokens := intPointerFromAny(usagePayload["total_tokens"])
	usage := usageFromParentTotals(inputTokens, cacheReadTokens, outputTokens, reasoningTokens, totalTokens)
	return usage, usage.hasValues()
}

// parseOpenAIImagesUsagePayload reads the ImagesUsage object returned by the
// image generation and edit operations, both in the non-stream response body
// and in the terminal stream event.
//
// `input_tokens_details` splits the input into text and image tokens, but that
// split is a breakdown of `input_tokens` rather than a separate billing
// component the way `cached_tokens` or `reasoning_tokens` are. It is therefore
// not subtracted from the parent: the canonical disjoint components stay input
// and output, and the two detail counts are deliberately not persisted, because
// the pricing template has no per-component slot that could price them apart.
func parseOpenAIImagesUsagePayload(usagePayload map[string]any) (responseUsage, bool) {
	usage := responseUsage{
		InputTokens:  intPointerFromAny(usagePayload["input_tokens"]),
		OutputTokens: intPointerFromAny(usagePayload["output_tokens"]),
		TotalTokens:  intPointerFromAny(usagePayload["total_tokens"]),
	}
	return usage, usage.hasValues()
}

func usageFromParentTotals(inputTokens *int, cacheReadTokens *int, outputTokens *int, reasoningTokens *int, totalTokens *int) responseUsage {
	usage := responseUsage{
		TotalTokens:          totalTokens,
		CacheReadInputTokens: cacheReadTokens,
		ReasoningTokens:      reasoningTokens,
	}
	if inputTokens != nil {
		baseInputTokens := *inputTokens - intValue(cacheReadTokens)
		usage.InputTokens = &baseInputTokens
	}
	if outputTokens != nil {
		baseOutputTokens := *outputTokens - intValue(reasoningTokens)
		usage.OutputTokens = &baseOutputTokens
	}
	return usage
}

func parseGeminiRuntimeUsagePayload(usagePayload map[string]any) (responseUsage, bool) {
	usage := responseUsageFromProviderUsageEnvelope(geminiprovider.ParseUsageMetadata(usagePayload, geminiprovider.OperationGenerateContent))
	return usage, usage.hasValues()
}

func (usage *responseUsage) merge(parsed responseUsage) {
	if parsed.InputTokens != nil {
		usage.InputTokens = parsed.InputTokens
	}
	if parsed.OutputTokens != nil {
		usage.OutputTokens = parsed.OutputTokens
	}
	if parsed.TotalTokens != nil {
		usage.TotalTokens = parsed.TotalTokens
	}
	if parsed.CacheReadInputTokens != nil {
		usage.CacheReadInputTokens = parsed.CacheReadInputTokens
	}
	if parsed.CacheCreationInputTokens != nil {
		usage.CacheCreationInputTokens = parsed.CacheCreationInputTokens
	}
	if parsed.ReasoningTokens != nil {
		usage.ReasoningTokens = parsed.ReasoningTokens
	}
}

func (usage responseUsage) validForRuntimeUsage(rule runtimeUsageNormalizationRule) bool {
	if usage.discarded {
		return false
	}
	if hasNegativeRuntimeUsageValue(usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.CacheReadInputTokens, usage.CacheCreationInputTokens, usage.ReasoningTokens) {
		return false
	}
	if usage.TotalTokens != nil {
		minimumTotal := intValue(usage.InputTokens) + intValue(usage.OutputTokens)
		if rule.ValidateDisjointComponentTotal {
			minimumTotal = usage.componentTotal()
		}
		if minimumTotal > 0 && *usage.TotalTokens < minimumTotal {
			return false
		}
	}
	if !rule.ValidateParentSplitBounds {
		return true
	}
	cacheInputTokens := intValue(usage.CacheReadInputTokens) + intValue(usage.CacheCreationInputTokens)
	if usage.InputTokens != nil && (usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil) && cacheInputTokens > *usage.InputTokens {
		return false
	}
	if usage.OutputTokens != nil && usage.ReasoningTokens != nil && *usage.ReasoningTokens > *usage.OutputTokens {
		return false
	}
	return true
}

func hasNegativeRuntimeUsageValue(values ...*int) bool {
	for _, value := range values {
		if value != nil && *value < 0 {
			return true
		}
	}
	return false
}

func buildUsageBodyFromResponseUsage(usage responseUsage) []byte {
	usage = usage.normalized()
	payload := map[string]any{}
	if usage.InputTokens != nil {
		payload["input_tokens"] = *usage.InputTokens
	}
	if usage.OutputTokens != nil {
		payload["output_tokens"] = *usage.OutputTokens
	}
	totalTokens := usage.TotalTokens
	if totalTokens != nil {
		payload["total_tokens"] = *totalTokens
	}
	if usage.CacheReadInputTokens != nil {
		payload["cache_read_input_tokens"] = *usage.CacheReadInputTokens
	}
	if usage.CacheCreationInputTokens != nil {
		payload["cache_creation_input_tokens"] = *usage.CacheCreationInputTokens
	}
	if usage.ReasoningTokens != nil {
		payload["output_tokens_details"] = map[string]any{"reasoning_tokens": *usage.ReasoningTokens}
	}
	if len(payload) == 0 {
		return nil
	}
	body, err := json.Marshal(map[string]any{"usage": payload})
	if err != nil {
		return nil
	}
	return body
}

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
	ProxyAPIKeyID                     *int
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
	ReportingCurrencyEpoch        *int

	// Observe finalized-ingress fields (Observe SPEC §3.5, Requests SPEC
	// §3.6): expected request-log row count, routing evidence, final attempt
	// identity, and the terminal error code for failed/client-disconnected
	// final results.
	ExpectedRequestLogRowCount  *int
	FinalAttemptNumber          *int
	FinalAttemptTrigger         *string
	FinalTargetEntryTrigger     *string
	SameTargetRetryOccurred     bool
	HedgeOccurred               bool
	FailoverOccurred            bool
	RoutingEvidenceComplete     *bool
	FinalErrorCode              *string
	IngressStartedAt            *time.Time
	IngressCompletedAt          *time.Time
	ProxyAPIKeyIDSnapshot       *int
	ProxyAPIKeyAttributionState string
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
	runtimeTelemetryHandoffPhaseStreamAccepted = "stream_accepted"

	// request_logs row kinds (Observe SPEC §3.5): new writers never produce
	// legacy_unknown.
	requestLogRowKindPlanning      = "planning"
	requestLogRowKindAdmission     = "admission"
	requestLogRowKindUpstream      = "upstream"
	requestLogRowKindLegacyUnknown = "legacy_unknown"

	// URL scrub provenance (Requests SPEC §4.4).
	runtimeURLScrubProvenanceRuntime       = "runtime_scrubbed"
	runtimeURLScrubProvenanceNotApplicable = "not_applicable"
)

func (s *Service) recordRuntimeActivity(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	ctx := runtimeRequestContext(request)
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, responseCapture)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		slog.Error("failed to accept runtime activity telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func (s *Service) enqueueRuntimeActivityBeforeResponse(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) error {
	if s == nil || s.runtimeSideEffects == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	ctx := runtimeRequestContext(request)
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, responseCapture)
	if err := s.validateRuntimeActivityHandoff(envelope); err != nil {
		return err
	}
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if err := s.runtimeSideEffects.CommitRuntimeActivityBeforeResponse(ctx, intent); err != nil {
		return err
	}
	return nil
}

func (s *Service) enqueueStreamingRuntimeActivityAcceptedBeforeResponse(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time) (int64, error) {
	if s == nil || s.runtimeSideEffects == nil {
		return 0, fmt.Errorf("runtime telemetry outbox unavailable")
	}
	ctx := runtimeRequestContext(request)
	acceptedAt := s.nowUTC()
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, runtimeResponseCapture{CompletedAt: &acceptedAt, StreamOutcome: runtimeStreamOutcomeUnknown})
	envelope.HandoffPhase = runtimeTelemetryHandoffPhaseStreamAccepted
	if err := s.validateRuntimeActivityHandoff(envelope); err != nil {
		return 0, err
	}
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	rowID, err := s.runtimeSideEffects.CommitStreamingRuntimeActivityAcceptedBeforeResponse(ctx, intent)
	if err != nil {
		return 0, err
	}
	return rowID, nil
}

func (s *Service) finalizeStreamingRuntimeActivityBeforeCompletion(acceptedRowID int64, plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) error {
	if s == nil || s.runtimeSideEffects == nil {
		return fmt.Errorf("runtime telemetry outbox unavailable")
	}
	ctx := runtimeDetachedContext(runtimeRequestContext(request))
	envelope := s.buildRuntimeActivityEnvelope(ctx, plan, result, request, startedAt, responseCapture)
	if err := s.validateRuntimeActivityHandoff(envelope); err != nil {
		return err
	}
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if err := s.runtimeSideEffects.FinalizeStreamingRuntimeActivityBeforeCompletion(ctx, acceptedRowID, intent); err != nil {
		slog.Error("runtime streaming terminal telemetry handoff failed", "error", err, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
		return err
	}
	return nil
}

func (s *Service) validateRuntimeActivityHandoff(envelope runtimeTelemetryEnvelope) error {
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if err := intent.validate(); err != nil {
		return err
	}
	return nil
}

func (s *Service) buildRuntimeActivityEnvelope(ctx context.Context, plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelope {
	envelope := s.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, responseCapture)
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	return envelope
}

func (s *Service) recordRuntimePlanningFailure(request *http.Request, startedAt time.Time, err error) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	var runtimeErr *domainError
	if !errors.As(err, &runtimeErr) || runtimeErr == nil || runtimeErr.PlanningFailure == nil {
		return
	}
	ctx := runtimeRequestContext(request)
	envelope := s.buildRuntimePlanningFailureTelemetryEnvelope(*runtimeErr.PlanningFailure, request, startedAt, runtimeErr)
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		slog.Error("failed to accept runtime planning-failure telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func (s *Service) recordRuntimeExecutionFailure(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, err error) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	var runtimeErr *domainError
	if !errors.As(err, &runtimeErr) || runtimeErr == nil {
		return
	}
	// Execution-failure telemetry covers both gateway terminal codes:
	// admission_exhausted and the 64-launch safety bound (attempt budget
	// exhaustion preserves all launched attempt rows with typed facts).
	switch runtimeErr.ErrorCode {
	case runtimeAdmissionExhaustedErrorCode, safediag.CodeAttemptBudgetExhausted:
	default:
		return
	}
	ctx := runtimeRequestContext(request)
	var envelope runtimeTelemetryEnvelope
	if runtimeErr.ErrorCode == safediag.CodeAttemptBudgetExhausted && len(result.Attempts) > 0 {
		// The 64-launch safety bound preserves every already-launched
		// upstream row (trigger, target identity, attempt duration, safe
		// transport detail) plus a finalized usage summary carrying the
		// gateway terminal code (Requests SPEC §4.6).
		envelope = s.buildRuntimeBudgetExhaustionTelemetryEnvelope(plan, result, request, startedAt, runtimeErr)
	} else {
		envelope = s.buildRuntimeExecutionFailureTelemetryEnvelope(plan, result, request, startedAt, runtimeErr)
	}
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		slog.Error("failed to accept runtime execution-failure telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

// buildRuntimeBudgetExhaustionTelemetryEnvelope materializes the 64-launch
// safety bound: one retained upstream row per launched attempt with typed
// attempt facts and safe diagnostics, plus a finalized usage summary with the
// gateway terminal code `attempt_budget_exhausted` and the true attempt count.
func (s *Service) buildRuntimeBudgetExhaustionTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	resolvedTargetModelID := cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	if resolvedTargetModelID == nil {
		resolvedTargetModelID = cloneRuntimeStringPointer(plan.ResolvedTargetModelID)
	}
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	if selectedTerminalTargetID == nil {
		selectedTerminalTargetID = plan.selectedTerminalTargetID()
	}
	routeReason := runtimeExecutionRouteReason(result.RouteReason)
	if routeReason == gatewaycore.RouteReasonDirectMatch {
		routeReason = gatewaycore.RouteReasonPolicyReject
	}

	requestLogs := make([]requestLogInsert, 0, len(result.Attempts))
	for _, attempt := range result.Attempts {
		attemptDurationMS := attempt.AttemptDurationMS
		if attemptDurationMS <= 0 {
			attemptDurationMS = attempt.ResponseTimeMS
		}
		requestLog := requestLogInsert{
			ProfileID:                   plan.ProfileID,
			ModelID:                     plan.RequestedModelID,
			ResolvedTargetModelID:       resolvedTargetModelID,
			APIFamily:                   plan.APIFamily,
			OperationName:               strings.TrimSpace(plan.RuntimeOperation.Name),
			EndpointID:                  intPtr(attempt.Connection.Endpoint.ID),
			ConnectionID:                intPtr(attempt.Connection.ID),
			SelectedTerminalTargetID:    selectedTerminalTargetID,
			ProxyAPIKeyID:               proxyKeyIDPointer(proxyKey),
			ProxyAPIKeyNameSnapshot:     proxyKeyNamePointer(proxyKey),
			IngressRequestID:            ingressRequestID,
			AttemptNumber:               attempt.LaunchOrdinal,
			AttemptTrigger:              optionalTrimmedStringPointer(attempt.AttemptTrigger),
			AttemptResult:               optionalTrimmedStringPointer(attempt.AttemptResult),
			IsWinner:                    boolPtr(attempt.IsWinner),
			LegacyStatusCode:            nil,
			AttemptDurationMS:           intPtr(attemptDurationMS),
			ProviderCorrelationID:       headerValuePointer(attempt.ResponseHeaders, "x-request-id", "request-id"),
			EndpointBaseURL:             optionalTrimmedStringPointer(attempt.Connection.Endpoint.BaseURL),
			UpstreamStatusCode:          intPtr(attempt.StatusCode),
			IsStream:                    plan.IsStreamingRequest,
			SuccessFlag:                 attempt.StatusCode >= 200 && attempt.StatusCode <= 299,
			ReportCurrencyCode:          reportCurrencyCode,
			ReportCurrencySymbol:        reportCurrencySymbol,
			RequestPath:                 request.URL.Path,
			CreatedAt:                   requestCompletedAt,
			CallerUserAgent:             trimmedStringPointer(request.UserAgent()),
			RowKind:                     requestLogRowKindUpstream,
			URLScrubProvenance:          runtimeURLScrubProvenanceRuntime,
			PricingStatus:               runtimePricingStatusIneligible,
			PricingEvidenceTrust:        runtimePricingEvidenceTrust,
			StreamOutcome:               runtimeStreamOutcomeNotStreaming,
			AuditEnabledAtRequest:       plan.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: plan.AuditCaptureBodiesAtRequest,
		}
		if attempt.StatusCode > 0 {
			requestLog.ErrorSource = stringPtr(errorSourceUpstream)
			requestLog.FailureStage = stringPtr(failureStageUpstreamResponse)
			requestLog.ErrorCode = stringPtr(stableHTTPErrorCode(attempt.StatusCode, ""))
		} else if attempt.Diagnostics != nil {
			requestLog.ErrorSource = stringPtr(attempt.Diagnostics.Source)
			requestLog.FailureStage = stringPtr(attempt.Diagnostics.Stage)
			requestLog.ErrorCode = stringPtr(attempt.Diagnostics.Code)
			requestLog.ErrorDetail = stringPtr(attempt.Diagnostics.Detail)
			requestLog.ErrorDetailRedacted = attempt.Diagnostics.Redacted
			requestLog.ErrorDetailTruncated = attempt.Diagnostics.Truncated
		}
		requestLogs = append(requestLogs, requestLog)
	}
	if len(requestLogs) == 0 {
		requestLog := requestLogInsert{
			ProfileID:                   plan.ProfileID,
			ModelID:                     plan.RequestedModelID,
			ResolvedTargetModelID:       resolvedTargetModelID,
			APIFamily:                   plan.APIFamily,
			OperationName:               strings.TrimSpace(plan.RuntimeOperation.Name),
			IngressRequestID:            ingressRequestID,
			AttemptNumber:               1,
			AttemptTrigger:              stringPtr(attemptTriggerInitial),
			AttemptResult:               stringPtr(attemptResultTransportError),
			IsWinner:                    boolPtr(false),
			UpstreamStatusCode:          nil,
			GatewayStatusCode:           intPtr(runtimeErr.StatusCode),
			IsStream:                    plan.IsStreamingRequest,
			SuccessFlag:                 false,
			RequestPath:                 request.URL.Path,
			CreatedAt:                   requestCompletedAt,
			CallerUserAgent:             trimmedStringPointer(request.UserAgent()),
			RowKind:                     requestLogRowKindAdmission,
			URLScrubProvenance:          runtimeURLScrubProvenanceRuntime,
			PricingStatus:               runtimePricingStatusIneligible,
			PricingEvidenceTrust:        runtimePricingEvidenceTrust,
			StreamOutcome:               runtimeStreamOutcomeNotStreaming,
			AuditEnabledAtRequest:       plan.AuditEnabledAtRequest,
			AuditCaptureBodiesAtRequest: plan.AuditCaptureBodiesAtRequest,
		}
		applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)
		requestLogs = append(requestLogs, requestLog)
	}

	usageEvent := usageEventInsert{
		ProfileID:                   plan.ProfileID,
		IngressRequestID:            ingressRequestID,
		ModelID:                     plan.RequestedModelID,
		ResolvedTargetModelID:       resolvedTargetModelID,
		APIFamily:                   plan.APIFamily,
		OperationName:               strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:                  intPtr(result.Connection.Endpoint.ID),
		EndpointLabelSnapshot:       runtimeEndpointLabelSnapshot(result.Connection.Endpoint),
		ConnectionID:                intPtr(result.Connection.ID),
		SelectedTerminalTargetID:    selectedTerminalTargetID,
		ProxyAPIKeyID:               proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:     proxyKeyNamePointer(proxyKey),
		ProxyAPIKeyAttributionState: runtimeProxyKeyAttributionState(proxyKey),
		StatusCode:                  runtimeErr.StatusCode,
		SuccessFlag:                 false,
		UnpricedReason:              nil,
		ReportCurrencyCode:          reportCurrencyCode,
		ReportCurrencySymbol:        reportCurrencySymbol,
		AttemptCount:                len(requestLogs),
		RequestPath:                 request.URL.Path,
		CreatedAt:                   requestCompletedAt,
		ResponseTimeMS:              intPtr(responseTimeMS),
		CompletionDurationMS:        intPtr(responseTimeMS),
		StreamOutcome:               runtimeStreamOutcomeNotStreaming,
		PricingStatus:               runtimePricingStatusIneligible,
		PricingEvidenceTrust:        runtimePricingEvidenceTrust,
		FinalErrorCode:              stringPtr(safediag.CodeAttemptBudgetExhausted),
		FinalAttemptNumber:          nil,
		FinalAttemptTrigger:         nil,
		IngressStartedAt:            &startedAt,
		IngressCompletedAt:          &requestCompletedAt,
	}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

func runtimeRequestContext(request *http.Request) context.Context {
	if request != nil && request.Context() != nil {
		return request.Context()
	}
	return context.Background()
}

type runtimeTelemetryPricingTimingContext struct {
	requestCompletedAt   time.Time
	responseTimeMS       int
	usage                responseUsage
	streamOutcome        string
	isStream             bool
	ttftMS               *int
	completionDurationMS *int
	successFlag          bool
	reportCurrencyCode   *string
	reportCurrencySymbol *string
	operationName        string
	pricingResult        runtimePricingResult
	usageSource          gatewaycore.UsageSource
	streamErrorKind      *string
	streamErrorDetail    *string
}

type runtimeTelemetryEnvelopeContext struct {
	runtimeTelemetryPricingTimingContext
	ingressRequestID          string
	ingressStartedAt          time.Time
	callerRequestID           *string
	proxyKey                  *requestcontext.RuntimeProxyKeySnapshot
	callerUserAgent           *string
	requestGenerationSnapshot requestGenerationParamsSnapshot
	attempts                  []executionAttempt
	winnerOrdinal             int
	routeReason               gatewaycore.RouteReason
	capturedRequestBody       *string
	capturedResponseBody      *string
	capturedResponseObserved  int64
	capturedResponseStored    int64
	capturedResponseTruncated bool
}

type runtimeTelemetryAttemptContext struct {
	attempt        executionAttempt
	attemptNumber  int
	isFinal        bool
	isWinner       bool
	success        bool
	unpricedReason *string
	createdAt      time.Time
	responseTimeMS int
}

// applyRuntimeDiagnosticFailureFields writes the safe failure projection for a
// planning/admission diagnostic row from a Prism domain error (Requests SPEC
// §4.2/§4.4). The domain code wins when it matches the stable grammar;
// otherwise a stage fallback is used. The detail is value-scrubbed and capped
// at 4 KiB before persistence.
func applyRuntimeDiagnosticFailureFields(requestLog *requestLogInsert, runtimeErr *domainError) {
	if runtimeErr == nil {
		return
	}
	stage := failureStageRouting
	if requestLog.RowKind == requestLogRowKindAdmission {
		stage = failureStageAdmission
	}
	code := runtimeDiagnosticFailureCode(runtimeErr, stage)
	requestLog.ErrorSource = stringPtr(errorSourcePrism)
	requestLog.FailureStage = stringPtr(stage)
	requestLog.ErrorCode = stringPtr(code)
	scrubbed := scrubRuntimeDiagnosticDetail(runtimeErr, stage, code)
	requestLog.ErrorDetail = stringPtr(scrubbed.Value)
	requestLog.ErrorDetailRedacted = scrubbed.Redacted
	requestLog.ErrorDetailTruncated = scrubbed.Truncated
}

func runtimeDiagnosticFailureCode(runtimeErr *domainError, stage string) string {
	code := strings.TrimSpace(runtimeErr.ErrorCode)
	if !safediag.ValidErrorCode(code) {
		return safediag.PrismStageFallbackCode(stage)
	}
	return code
}

func scrubRuntimeDiagnosticDetail(runtimeErr *domainError, stage string, code string) safediag.ScrubResult {
	detail := strings.TrimSpace(runtimeErr.Detail)
	if detail == "" {
		detail = fmt.Sprintf("Prism %s failure (%s).", stage, code)
	}
	return safediag.ScrubValue(detail, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
}

// applyRuntimePlanningFailureMetadataScrub captures and scrubs caller/request
// metadata for planning/admission diagnostic rows. These synthetic rows must
// preserve the same caller correlation contract as upstream attempt rows.
func applyRuntimePlanningFailureMetadataScrub(requestLog *requestLogInsert, request *http.Request) {
	var provenance safediag.MetadataProvenance
	if callerRequestID := strings.TrimSpace(runtimeCallerRequestIDFromContext(request.Context())); callerRequestID != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerRequestID, callerRequestID, 255)
		requestLog.CallerRequestID = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerRequestID, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.CallerUserAgent != nil {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerUserAgent, *requestLog.CallerUserAgent, 0)
		requestLog.CallerUserAgent = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerUserAgent, scrubbed.Redacted, scrubbed.Truncated)
	}
	requestPath, pathTruncated := safediag.ScrubRequestPath(request.URL.Path)
	if pathTruncated {
		provenance.Record(safediag.MetadataFieldRequestPath, false, true)
	}
	requestLog.RequestPath = requestPath
	operationScrub := safediag.ScrubMetadataValue(safediag.MetadataFieldOperationName, requestLog.OperationName, 120)
	if operationScrub.Truncated {
		provenance.Record(safediag.MetadataFieldOperationName, operationScrub.Redacted, true)
	}
	requestLog.OperationName = operationScrub.Value
	requestLog.MetadataRedactedFields = safediag.CanonicalFieldNames(provenance.Redacted)
	requestLog.MetadataTruncatedFields = safediag.CanonicalFieldNames(provenance.Truncated)
}

func (s *Service) buildRuntimeTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelope {
	telemetry := s.buildRuntimeTelemetryEnvelopeContext(plan, result, request, startedAt, responseCapture)
	requestLogs := buildRuntimeRequestLogRows(plan, request, telemetry)
	auditLogs := buildRuntimeAuditLogRows(plan, request, telemetry)
	usageEvent := buildRuntimeUsageEvent(plan, result, request, telemetry, len(requestLogs))
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		AuditLogs:            auditLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, telemetry.routeReason, telemetry.usageSource),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, telemetry.routeReason, telemetry.usageSource),
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(telemetry.proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

func (s *Service) buildRuntimePlanningFailureTelemetryEnvelope(failure runtimePlanningFailureTelemetry, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	unpricedReason := billingStateUnpricedOnly(false)
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	reportCurrencyCode := runtimeOptionalTrimmedString(failure.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(failure.ReportCurrencySnapshot.Symbol)
	requestGenerationSnapshot := failure.RequestGenerationParams.clone()
	selectedTerminalTargetID := cloneRuntimeIntPointer(failure.SelectedTerminalTargetID)
	resolvedTargetModelID := cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	completionDurationMS := intPtr(responseTimeMS)
	requestLog := requestLogInsert{
		ProfileID:                     failure.ProfileID,
		ModelID:                       failure.RequestedModelID,
		ResolvedTargetModelID:         resolvedTargetModelID,
		APIFamily:                     failure.APIFamily,
		OperationName:                 strings.TrimSpace(failure.RuntimeOperation.Name),
		UpstreamOperationName:         cloneRuntimeStringPointer(failure.UpstreamOperationName),
		OperationTranslationMode:      cloneRuntimeStringPointer(failure.OperationTranslationMode),
		EndpointID:                    nil,
		ConnectionID:                  nil,
		SelectedTerminalTargetID:      selectedTerminalTargetID,
		ProxyAPIKeyID:                 proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(proxyKey),
		IngressRequestID:              ingressRequestID,
		AttemptNumber:                 1,
		ProviderCorrelationID:         nil,
		EndpointBaseURL:               nil,
		EndpointDescription:           nil,
		StatusCode:                    runtimeErr.StatusCode,
		ResponseTimeMS:                responseTimeMS,
		IsStream:                      failure.IsStreamingRequest,
		SuccessFlag:                   false,
		UnpricedReason:                unpricedReason,
		ReportCurrencyCode:            reportCurrencyCode,
		ReportCurrencySymbol:          reportCurrencySymbol,
		RequestPath:                   failure.RequestPath,
		UpstreamRequestPath:           cloneRuntimeStringPointer(failure.UpstreamRequestPath),
		ErrorDetail:                   stringPtr(scrubRuntimeDiagnosticDetail(runtimeErr, failureStageRouting, runtimeDiagnosticFailureCode(runtimeErr, failureStageRouting)).Value),
		CreatedAt:                     requestCompletedAt,
		CallerUserAgent:               trimmedStringPointer(request.UserAgent()),
		UpstreamUserAgent:             nil,
		CompletionDurationMS:          completionDurationMS,
		TTFTMS:                        nil,
		StreamOutcome:                 runtimeStreamOutcomeNotStreaming,
		AuditEnabledAtRequest:         failure.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:   failure.AuditCaptureBodiesAtRequest,
		RequestGenerationParams:       requestGenerationSnapshot.Params,
		RequestGenerationParamsStatus: trimmedStringPointer(requestGenerationSnapshot.Status),
		RowKind:                       requestLogRowKindPlanning,
		URLScrubProvenance:            runtimeURLScrubProvenanceRuntime,
		GatewayStatusCode:             intPtr(runtimeErr.StatusCode),
		PricingStatus:                 runtimePricingStatusIneligible,
		PricingEvidenceTrust:          runtimePricingEvidenceTrust,
	}
	applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)
	applyRuntimePlanningFailureMetadataScrub(&requestLog, request)
	usageEvent := usageEventInsert{
		ProfileID:                   failure.ProfileID,
		IngressRequestID:            ingressRequestID,
		ModelID:                     failure.RequestedModelID,
		ResolvedTargetModelID:       resolvedTargetModelID,
		APIFamily:                   failure.APIFamily,
		OperationName:               strings.TrimSpace(failure.RuntimeOperation.Name),
		UpstreamOperationName:       cloneRuntimeStringPointer(failure.UpstreamOperationName),
		OperationTranslationMode:    cloneRuntimeStringPointer(failure.OperationTranslationMode),
		EndpointID:                  nil,
		EndpointLabelSnapshot:       "Unknown Endpoint",
		ConnectionID:                nil,
		SelectedTerminalTargetID:    selectedTerminalTargetID,
		ProxyAPIKeyID:               proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:     proxyKeyNamePointer(proxyKey),
		ProxyAPIKeyAttributionState: runtimeProxyKeyAttributionState(proxyKey),
		StatusCode:                  runtimeErr.StatusCode,
		SuccessFlag:                 false,
		UnpricedReason:              unpricedReason,
		ReportCurrencyCode:          reportCurrencyCode,
		ReportCurrencySymbol:        reportCurrencySymbol,
		AttemptCount:                1,
		RequestPath:                 failure.RequestPath,
		UpstreamRequestPath:         cloneRuntimeStringPointer(failure.UpstreamRequestPath),
		CreatedAt:                   requestCompletedAt,
		ResponseTimeMS:              intPtr(responseTimeMS),
		CompletionDurationMS:        completionDurationMS,
		TTFTMS:                      nil,
		StreamOutcome:               runtimeStreamOutcomeNotStreaming,
		StreamErrorKind:             nil,
		PricingStatus:               runtimePricingStatusIneligible,
		PricingEvidenceTrust:        runtimePricingEvidenceTrust,
	}
	routeReason := runtimeExecutionRouteReason(gatewaycore.RouteReasonPolicyReject)
	requestLogs := []requestLogInsert{requestLog}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

func (s *Service) buildRuntimeExecutionFailureTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	unpricedReason := billingStateUnpricedOnly(false)
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	requestGenerationSnapshot := plan.RequestGenerationParamsSnapshot()
	resolvedTargetModelID := cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	if resolvedTargetModelID == nil {
		resolvedTargetModelID = cloneRuntimeStringPointer(plan.ResolvedTargetModelID)
	}
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	if selectedTerminalTargetID == nil {
		selectedTerminalTargetID = plan.selectedTerminalTargetID()
	}
	routeReason := runtimeExecutionRouteReason(result.RouteReason)
	if routeReason == gatewaycore.RouteReasonDirectMatch {
		routeReason = gatewaycore.RouteReasonPolicyReject
	}
	completionDurationMS := intPtr(responseTimeMS)
	requestLog := requestLogInsert{
		ProfileID:                     plan.ProfileID,
		ModelID:                       plan.RequestedModelID,
		ResolvedTargetModelID:         resolvedTargetModelID,
		APIFamily:                     plan.APIFamily,
		OperationName:                 strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:                    nil,
		ConnectionID:                  nil,
		SelectedTerminalTargetID:      selectedTerminalTargetID,
		ProxyAPIKeyID:                 proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(proxyKey),
		IngressRequestID:              ingressRequestID,
		AttemptNumber:                 1,
		StatusCode:                    runtimeErr.StatusCode,
		ResponseTimeMS:                responseTimeMS,
		IsStream:                      plan.IsStreamingRequest,
		SuccessFlag:                   false,
		UnpricedReason:                unpricedReason,
		ReportCurrencyCode:            reportCurrencyCode,
		ReportCurrencySymbol:          reportCurrencySymbol,
		RequestPath:                   request.URL.Path,
		CreatedAt:                     requestCompletedAt,
		CallerUserAgent:               trimmedStringPointer(request.UserAgent()),
		CompletionDurationMS:          completionDurationMS,
		StreamOutcome:                 runtimeStreamOutcomeNotStreaming,
		AuditEnabledAtRequest:         plan.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:   plan.AuditCaptureBodiesAtRequest,
		RequestGenerationParams:       requestGenerationSnapshot.Params,
		RequestGenerationParamsStatus: trimmedStringPointer(requestGenerationSnapshot.Status),
		RowKind:                       requestLogRowKindAdmission,
		URLScrubProvenance:            runtimeURLScrubProvenanceRuntime,
		GatewayStatusCode:             intPtr(runtimeErr.StatusCode),
		PricingStatus:                 runtimePricingStatusIneligible,
		PricingEvidenceTrust:          runtimePricingEvidenceTrust,
	}
	applyRuntimeDiagnosticFailureFields(&requestLog, runtimeErr)
	applyRuntimePlanningFailureMetadataScrub(&requestLog, request)
	usageEvent := usageEventInsert{
		ProfileID:                   plan.ProfileID,
		IngressRequestID:            ingressRequestID,
		ModelID:                     plan.RequestedModelID,
		ResolvedTargetModelID:       resolvedTargetModelID,
		APIFamily:                   plan.APIFamily,
		OperationName:               strings.TrimSpace(plan.RuntimeOperation.Name),
		EndpointID:                  nil,
		EndpointLabelSnapshot:       "Unknown Endpoint",
		ConnectionID:                nil,
		SelectedTerminalTargetID:    selectedTerminalTargetID,
		ProxyAPIKeyID:               proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:     proxyKeyNamePointer(proxyKey),
		ProxyAPIKeyAttributionState: runtimeProxyKeyAttributionState(proxyKey),
		StatusCode:                  runtimeErr.StatusCode,
		SuccessFlag:                 false,
		UnpricedReason:              unpricedReason,
		ReportCurrencyCode:          reportCurrencyCode,
		ReportCurrencySymbol:        reportCurrencySymbol,
		AttemptCount:                1,
		RequestPath:                 request.URL.Path,
		CreatedAt:                   requestCompletedAt,
		ResponseTimeMS:              intPtr(responseTimeMS),
		CompletionDurationMS:        completionDurationMS,
		StreamOutcome:               runtimeStreamOutcomeNotStreaming,
		PricingStatus:               runtimePricingStatusIneligible,
		PricingEvidenceTrust:        runtimePricingEvidenceTrust,
	}
	requestLogs := []requestLogInsert{requestLog}
	return runtimeTelemetryEnvelope{
		RequestLogs:          requestLogs,
		UsageEvent:           usageEvent,
		AccountingEvent:      buildRuntimeAccountingFinalEvent(usageEvent, requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		AccountingAttempts:   buildRuntimeAccountingAttemptEvents(requestLogs, routeReason, gatewaycore.UsageSourceMissing),
		ProxyKeyUsage:        runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
		ProxyKeyAuthEnforced: runtimeProxyKeyAuthEnforcedFromContext(request.Context()),
	}
}

func (s *Service) buildRuntimeTelemetryEnvelopeContext(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelopeContext {
	pricingTiming := s.buildRuntimeTelemetryPricingTimingContext(plan, result, startedAt, responseCapture)
	ingressRequestID := runtimeIngressRequestIDFromContext(request.Context())
	if ingressRequestID == "" {
		ingressRequestID = newRuntimeUUIDv4()
	}
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	callerRequestID := strings.TrimSpace(runtimeCallerRequestIDFromContext(request.Context()))
	return runtimeTelemetryEnvelopeContext{
		runtimeTelemetryPricingTimingContext: pricingTiming,
		ingressRequestID:                     ingressRequestID,
		ingressStartedAt:                     startedAt.UTC(),
		callerRequestID:                      optionalTrimmedStringPointer(callerRequestID),
		proxyKey:                             proxyKey,
		callerUserAgent:                      trimmedStringPointer(request.UserAgent()),
		requestGenerationSnapshot:            plan.RequestGenerationParamsSnapshot(),
		attempts:                             runtimeTelemetryAttempts(plan, result, request, pricingTiming),
		winnerOrdinal:                        result.WinnerOrdinal,
		routeReason:                          runtimeExecutionRouteReason(result.RouteReason),
		capturedResponseBody:                 runtimeCapturedAuditResponseBodyForOperation(plan.RuntimeOperation, result.AuditEnabledAtRequest && result.AuditCaptureBodiesAtRequest, responseCapture.AuditBody, responseCapture.StreamOutcome),
		capturedResponseObserved:             responseCapture.AuditBodyObserved,
		capturedResponseStored:               responseCapture.AuditBodyStored,
		capturedResponseTruncated:            responseCapture.AuditBodyTruncated,
	}
}

func (s *Service) buildRuntimeTelemetryPricingTimingContext(plan requestPlan, result executionResult, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryPricingTimingContext {
	requestCompletedAt := s.nowUTC()
	if responseCapture.CompletedAt != nil && !responseCapture.CompletedAt.IsZero() {
		requestCompletedAt = responseCapture.CompletedAt.UTC()
	}
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	usage := responseCapture.extractedUsage()
	streamOutcome := runtimeStreamOutcomeForTelemetry(responseCapture.StreamOutcome)
	isStream := runtimeStreamOutcomeIsStreaming(streamOutcome)
	ttftMS, completionDurationMS := runtimeResponseTiming(startedAt, requestCompletedAt, isStream, responseCapture)
	successFlag := result.Response.StatusCode >= 200 && result.Response.StatusCode <= 299
	reportCurrencyCode := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(plan.ReportCurrencySnapshot.Symbol)
	pricingResult := runtimePricingResult{ReportCurrencyCode: reportCurrencyCode, ReportCurrencySymbol: reportCurrencySymbol}
	if successFlag {
		pricingResult = buildRuntimePricingResult(plan.ReportCurrencySnapshot, result.Connection.PricingTemplateSnapshot, result.Connection.EndpointFXSnapshot, usage, streamOutcome)
		pricingResult = withRuntimePricingSnapshotForPersistence(pricingResult, result.Connection.PricingTemplateSnapshot)
		pricingResult = enforceRuntimeSpendCoherence(successFlag, pricingResult)
	}
	return runtimeTelemetryPricingTimingContext{
		requestCompletedAt:   requestCompletedAt,
		responseTimeMS:       responseTimeMS,
		usage:                usage,
		streamOutcome:        streamOutcome,
		isStream:             isStream,
		ttftMS:               ttftMS,
		completionDurationMS: completionDurationMS,
		successFlag:          successFlag,
		reportCurrencyCode:   reportCurrencyCode,
		reportCurrencySymbol: reportCurrencySymbol,
		operationName:        strings.TrimSpace(plan.RuntimeOperation.Name),
		pricingResult:        pricingResult,
		usageSource:          runtimeUsageSourceFromCapture(responseCapture, usage, streamOutcome),
		streamErrorKind:      responseCapture.StreamErrorKind,
		streamErrorDetail:    responseCapture.StreamErrorDetail,
	}
}

func runtimeTelemetryAttempts(plan requestPlan, result executionResult, request *http.Request, pricingTiming runtimeTelemetryPricingTimingContext) []executionAttempt {
	if len(result.Attempts) > 0 {
		return result.Attempts
	}
	selectedAttempt := firstTerminalAttempt(plan)
	attempt := executionAttempt{
		Connection:                  result.Connection,
		ResolvedTargetModelID:       dereferenceString(result.ResolvedTargetModelID),
		RequestURL:                  request.URL.String(),
		RequestHeaders:              result.RequestHeaders,
		ResponseHeaders:             result.Response.Header.Clone(),
		StatusCode:                  result.Response.StatusCode,
		ResponseTimeMS:              pricingTiming.responseTimeMS,
		CompletedAt:                 pricingTiming.requestCompletedAt,
		AuditEnabledAtRequest:       result.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: result.AuditCaptureBodiesAtRequest,
		UpstreamOperationName:       runtimeUpstreamOperationName(plan.RuntimeOperation, selectedAttempt.TranslationMode),
		UpstreamRequestPath:         dereferenceString(runtimeUpstreamRequestPath(plan.RuntimeOperation, selectedAttempt.TranslationMode, plan.EffectiveRequestPath)),
		OperationTranslationMode:    normalizedRuntimeTranslationMode(selectedAttempt.TranslationMode),
		RequestGenerationParams:     selectedAttempt.RequestGenerationParams.clonePointer(),
		LaunchOrdinal:               1,
		AttemptTrigger:              attemptTriggerInitial,
		UpstreamRequestStarted:      true,
		ResponseHeadersReceived:     result.Response != nil,
	}
	return []executionAttempt{attempt}
}

func firstTerminalAttempt(plan requestPlan) runtimeTerminalAttempt {
	attempts := plan.orderedTerminalAttempts()
	if len(attempts) == 0 {
		return runtimeTerminalAttempt{}
	}
	return attempts[0]
}

func (telemetry runtimeTelemetryEnvelopeContext) attemptContext(index int) runtimeTelemetryAttemptContext {
	attempt := telemetry.attempts[index]
	isFinal := index == len(telemetry.attempts)-1
	isWinner := telemetry.winnerOrdinal > 0 && attempt.LaunchOrdinal == telemetry.winnerOrdinal
	if attempt.LaunchOrdinal <= 0 {
		isWinner = isFinal
	}
	attemptSuccess := attempt.StatusCode >= 200 && attempt.StatusCode <= 299
	attemptUnpricedReason := billingStateUnpricedOnly(attemptSuccess)
	attemptCreatedAt := attempt.CompletedAt
	if attemptCreatedAt.IsZero() || isFinal {
		attemptCreatedAt = telemetry.requestCompletedAt
	}
	attemptResponseTimeMS := attempt.ResponseTimeMS
	if attemptResponseTimeMS < 1 || isFinal {
		attemptResponseTimeMS = telemetry.responseTimeMS
	}
	return runtimeTelemetryAttemptContext{
		attempt:        attempt,
		attemptNumber:  attempt.LaunchOrdinal,
		isFinal:        isFinal,
		isWinner:       isWinner,
		success:        attemptSuccess,
		unpricedReason: attemptUnpricedReason,
		createdAt:      attemptCreatedAt.UTC(),
		responseTimeMS: attemptResponseTimeMS,
	}
}

func selectedTerminalTargetIDForAttempt(plan requestPlan, attempt runtimeTelemetryAttemptContext) *int {
	return plan.selectedTerminalTargetID()
}

func selectedTerminalTargetIDForUsageEvent(plan requestPlan) *int {
	return plan.selectedTerminalTargetID()
}

func buildRuntimeRequestLogRows(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext) []requestLogInsert {
	requestLogs := make([]requestLogInsert, 0, len(telemetry.attempts))
	for index := range telemetry.attempts {
		requestLogs = append(requestLogs, buildRuntimeRequestLogRow(plan, request, telemetry, telemetry.attemptContext(index)))
	}
	return requestLogs
}

func buildRuntimeRequestLogRow(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext) requestLogInsert {
	generationSnapshot := telemetry.requestGenerationSnapshot.clone()
	// A materialized attempt carries its own snapshot extracted from its final
	// effective upstream body. The only exception is the no-configuration
	// streaming request-body observer path, where the plan-level snapshot is
	// the source of truth because the probe attempt never materialized a body.
	if attempt.attempt.RequestGenerationParams != nil && plan.RequestGenerationSnapshot == nil {
		generationSnapshot = attempt.attempt.RequestGenerationParams.clone()
	}
	requestLog := requestLogInsert{
		ProfileID:                     plan.ProfileID,
		ModelID:                       plan.RequestedModelID,
		ResolvedTargetModelID:         resolvedTargetModelIDForAttempt(plan, attempt.attempt),
		APIFamily:                     plan.APIFamily,
		OperationName:                 telemetry.operationName,
		UpstreamOperationName:         trimmedStringPointer(attempt.attempt.UpstreamOperationName),
		OperationTranslationMode:      runtimeTranslationModePointer(attempt.attempt.OperationTranslationMode),
		EndpointID:                    intPtr(attempt.attempt.Connection.Endpoint.ID),
		ConnectionID:                  intPtr(attempt.attempt.Connection.ID),
		SelectedTerminalTargetID:      selectedTerminalTargetIDForAttempt(plan, attempt),
		ProxyAPIKeyID:                 proxyKeyIDPointer(telemetry.proxyKey),
		ProxyAPIKeyNameSnapshot:       proxyKeyNamePointer(telemetry.proxyKey),
		IngressRequestID:              telemetry.ingressRequestID,
		AttemptNumber:                 attempt.attemptNumber,
		ProviderCorrelationID:         headerValuePointer(attempt.attempt.ResponseHeaders, "x-request-id", "request-id"),
		EndpointBaseURL:               stringPointerIfNotEmpty(attempt.attempt.Connection.Endpoint.BaseURL),
		EndpointDescription:           attempt.attempt.Connection.Endpoint.Name,
		StatusCode:                    attempt.attempt.StatusCode,
		ResponseTimeMS:                attempt.responseTimeMS,
		IsStream:                      telemetry.isStream,
		SuccessFlag:                   attempt.success,
		UnpricedReason:                attempt.unpricedReason,
		ReportCurrencyCode:            telemetry.reportCurrencyCode,
		ReportCurrencySymbol:          telemetry.reportCurrencySymbol,
		RequestPath:                   request.URL.Path,
		UpstreamRequestPath:           trimmedStringPointer(attempt.attempt.UpstreamRequestPath),
		CreatedAt:                     attempt.createdAt,
		CallerUserAgent:               telemetry.callerUserAgent,
		UpstreamUserAgent:             headerMapValuePointer(attempt.attempt.RequestHeaders, "User-Agent"),
		CompletionDurationMS:          nil,
		TTFTMS:                        nil,
		StreamOutcome:                 runtimeStreamOutcomeNotStreaming,
		AuditEnabledAtRequest:         attempt.attempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:   attempt.attempt.AuditCaptureBodiesAtRequest,
		RequestGenerationParams:       generationSnapshot.Params,
		RequestGenerationParamsStatus: trimmedStringPointer(generationSnapshot.Status),
		RowKind:                       requestLogRowKindUpstream,
		URLScrubProvenance:            runtimeURLScrubProvenanceRuntime,
		AttemptTrigger:                optionalTrimmedStringPointer(attempt.attempt.AttemptTrigger),
		AttemptResult:                 optionalTrimmedStringPointer(attempt.attempt.AttemptResult),
		IsWinner:                      boolPtr(attempt.isWinner),
		AttemptDurationMS:             nonNegativeIntPointer(attempt.attempt.AttemptDurationMS),
		UpstreamRequestStarted:        boolPtr(attempt.attempt.UpstreamRequestStarted),
		ResponseHeadersReceived:       boolPtr(attempt.attempt.ResponseHeadersReceived),
	}
	if attempt.attempt.ResponseHeadersReceived {
		requestLog.UpstreamStatusCode = intPtr(attempt.attempt.StatusCode)
	} else {
		requestLog.UpstreamStatusCode = nil
	}
	applyRuntimeRequestRowFailureFields(&requestLog, telemetry, attempt)
	applyRuntimeRequestRowMetadataScrub(&requestLog, telemetry, request)
	if attempt.isFinal {
		applyRuntimeFinalAttemptTelemetry(&requestLog, telemetry, attempt)
	}
	return requestLog
}

// applyRuntimeRequestRowFailureFields derives the scoped failure projection
// for one upstream row (Requests SPEC §3.2-§3.4/§4.2/§4.5). Success rows keep
// failure fields empty; transport rows keep upstream status null; cancelled
// Hedge losers carry a typed neutral cancellation without failure detail.
// Every new failed row is ineligible for pricing (Pricing SPEC §3.4: non-2xx
// is never priced/unpriced).
func applyRuntimeRequestRowFailureFields(requestLog *requestLogInsert, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext) {
	attemptFacts := attempt.attempt
	requestLog.PricingStatus = runtimePricingStatusIneligible
	requestLog.PricingEvidenceTrust = runtimePricingEvidenceTrust
	switch {
	case attemptFacts.AttemptResult == attemptResultCancelled:
		// Expected Hedge-loser arbitration: typed neutral cancellation, no
		// failure detail, no HTTP status.
		requestLog.AttemptResult = stringPtr(attemptResultCancelled)
		requestLog.UpstreamStatusCode = nil
		return
	case attemptFacts.AttemptResult == attemptResultTransportError || (attemptFacts.Diagnostics != nil && attemptFacts.Diagnostics.Source == errorSourceTransport):
		diagnostic := attemptFacts.diagnosticsOrFallback(http.StatusBadGateway)
		if attemptFacts.Diagnostics != nil {
			diagnostic = *attemptFacts.Diagnostics
		}
		requestLog.AttemptResult = stringPtr(attemptResultTransportError)
		requestLog.UpstreamStatusCode = nil
		requestLog.ErrorSource = stringPtr(diagnostic.Source)
		requestLog.FailureStage = stringPtr(diagnostic.Stage)
		requestLog.ErrorCode = stringPtr(diagnostic.Code)
		if diagnostic.Detail != "" {
			requestLog.ErrorDetail = stringPtr(diagnostic.Detail)
		}
		requestLog.ErrorDetailRedacted = diagnostic.Redacted
		requestLog.ErrorDetailTruncated = diagnostic.Truncated
		return
	}

	// Non-transport rows: complete the attempt result from response evidence
	// and stream outcome.
	isWinner := attempt.isWinner
	streamOutcome := telemetry.streamOutcome
	streamKind := telemetry.streamErrorKind
	streamDetail := telemetry.streamErrorDetail
	if !isWinner {
		// Intermediate attempts never carry stream outcome evidence; the
		// response status alone classifies them.
		streamOutcome = runtimeStreamOutcomeNotStreaming
		streamKind = nil
		streamDetail = nil
	}
	if attemptFacts.StatusCode >= 200 && attemptFacts.StatusCode <= 299 {
		switch streamOutcome {
		case runtimeStreamOutcomeCompleted, runtimeStreamOutcomeNotStreaming:
			if attemptFacts.AttemptResult == "" {
				requestLog.AttemptResult = stringPtr(attemptResultCompleted)
			}
			return
		case runtimeStreamOutcomeClientDisconnected:
			diagnostic := safeStreamDiagnostic(errorSourceClient, failureStageStream, dereferenceString(streamKind), streamOutcome, dereferenceString(streamDetail))
			requestLog.AttemptResult = stringPtr(attemptResultClientDisconnected)
			requestLog.ErrorSource = stringPtr(diagnostic.Source)
			requestLog.FailureStage = stringPtr(diagnostic.Stage)
			requestLog.ErrorCode = stringPtr(diagnostic.Code)
			requestLog.StreamErrorDetail = optionalTrimmedStringPointer(diagnostic.Detail)
			requestLog.StreamErrorDetailRedacted = diagnostic.Redacted
			requestLog.StreamErrorDetailTruncated = diagnostic.Truncated
			return
		default:
			// provider_incomplete / upstream_read_error /
			// upstream_ended_without_terminal / unknown -> stream_error.
			diagnostic := safeStreamDiagnostic(errorSourceUpstream, failureStageStream, dereferenceString(streamKind), streamOutcome, dereferenceString(streamDetail))
			requestLog.AttemptResult = stringPtr(attemptResultStreamError)
			requestLog.ErrorSource = stringPtr(diagnostic.Source)
			requestLog.FailureStage = stringPtr(diagnostic.Stage)
			requestLog.ErrorCode = stringPtr(diagnostic.Code)
			requestLog.StreamErrorDetail = optionalTrimmedStringPointer(diagnostic.Detail)
			requestLog.StreamErrorDetailRedacted = diagnostic.Redacted
			requestLog.StreamErrorDetailTruncated = diagnostic.Truncated
			return
		}
	}

	// Upstream HTTP failure (non-2xx). The sampler diagnostic wins when
	// completed; otherwise a generic status fallback is used (the sample never
	// blocks sealing).
	diagnostic := attemptFacts.diagnosticsOrFallback(attemptFacts.StatusCode)
	if attemptFacts.Diagnostics != nil {
		diagnostic = *attemptFacts.Diagnostics
	}
	requestLog.AttemptResult = stringPtr(attemptResultHTTPError)
	requestLog.ErrorSource = stringPtr(errorSourceUpstream)
	requestLog.FailureStage = stringPtr(failureStageUpstreamResponse)
	requestLog.ErrorCode = stringPtr(stableHTTPErrorCode(attemptFacts.StatusCode, diagnostic.Code))
	if strings.TrimSpace(diagnostic.Detail) == "" {
		diagnostic.Detail = fmt.Sprintf("upstream request returned HTTP %d", attemptFacts.StatusCode)
	}
	scrubbedDetail := safediag.ScrubValue(diagnostic.Detail, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
	requestLog.ErrorDetail = stringPtr(scrubbedDetail.Value)
	diagnostic.Redacted = diagnostic.Redacted || scrubbedDetail.Redacted
	diagnostic.Truncated = diagnostic.Truncated || scrubbedDetail.Truncated
	requestLog.ErrorDetailRedacted = diagnostic.Redacted
	requestLog.ErrorDetailTruncated = diagnostic.Truncated
}

// applyRuntimeRequestRowMetadataScrub applies the fixed-bottom-line value
// scrubber and per-field caps to externally controlled metadata strings and
// records redacted/truncated provenance (Requests SPEC §4.3).
func applyRuntimeRequestRowMetadataScrub(requestLog *requestLogInsert, telemetry runtimeTelemetryEnvelopeContext, request *http.Request) {
	var provenance safediag.MetadataProvenance

	if telemetry.callerRequestID != nil && strings.TrimSpace(*telemetry.callerRequestID) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerRequestID, *telemetry.callerRequestID, 255)
		requestLog.CallerRequestID = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerRequestID, scrubbed.Redacted, scrubbed.Truncated)
	}
	if telemetry.callerUserAgent != nil && strings.TrimSpace(*telemetry.callerUserAgent) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldCallerUserAgent, *telemetry.callerUserAgent, 0)
		requestLog.CallerUserAgent = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldCallerUserAgent, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.UpstreamUserAgent != nil && strings.TrimSpace(*requestLog.UpstreamUserAgent) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldUpstreamUserAgent, *requestLog.UpstreamUserAgent, 0)
		requestLog.UpstreamUserAgent = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldUpstreamUserAgent, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.ProviderCorrelationID != nil && strings.TrimSpace(*requestLog.ProviderCorrelationID) != "" {
		scrubbed := safediag.ScrubMetadataValue(safediag.MetadataFieldProviderCorrelationID, *requestLog.ProviderCorrelationID, 255)
		requestLog.ProviderCorrelationID = optionalTrimmedStringPointer(scrubbed.Value)
		provenance.Record(safediag.MetadataFieldProviderCorrelationID, scrubbed.Redacted, scrubbed.Truncated)
	}
	if requestLog.EndpointBaseURL != nil {
		scrubbed, truncated := safediag.ScrubEndpointBaseURL(*requestLog.EndpointBaseURL)
		requestLog.EndpointBaseURL = optionalTrimmedStringPointer(scrubbed)
		if truncated {
			provenance.Record(safediag.MetadataFieldEndpointBaseURL, false, true)
		}
	}
	requestPath, pathTruncated := safediag.ScrubRequestPath(request.URL.Path)
	if pathTruncated {
		provenance.Record(safediag.MetadataFieldRequestPath, false, true)
	}
	requestLog.RequestPath = requestPath
	operationScrub := safediag.ScrubMetadataValue(safediag.MetadataFieldOperationName, requestLog.OperationName, 120)
	if operationScrub.Truncated {
		provenance.Record(safediag.MetadataFieldOperationName, operationScrub.Redacted, true)
	}
	requestLog.OperationName = operationScrub.Value

	requestLog.MetadataRedactedFields = safediag.CanonicalFieldNames(provenance.Redacted)
	requestLog.MetadataTruncatedFields = safediag.CanonicalFieldNames(provenance.Truncated)
}

func resolvedTargetModelIDForAttempt(plan requestPlan, attempt executionAttempt) *string {
	if trimmed := strings.TrimSpace(attempt.ResolvedTargetModelID); trimmed != "" {
		return &trimmed
	}
	return plan.ResolvedTargetModelID
}

func applyRuntimeFinalAttemptTelemetry(requestLog *requestLogInsert, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext) {
	requestLog.InputTokens = telemetry.usage.InputTokens
	requestLog.OutputTokens = telemetry.usage.OutputTokens
	requestLog.TotalTokens = telemetry.usage.TotalTokens
	requestLog.CacheReadInputTokens = telemetry.usage.CacheReadInputTokens
	requestLog.CacheCreationInputTokens = telemetry.usage.CacheCreationInputTokens
	requestLog.ReasoningTokens = telemetry.usage.ReasoningTokens
	requestLog.CompletionDurationMS = telemetry.completionDurationMS
	requestLog.TTFTMS = telemetry.ttftMS
	requestLog.StreamOutcome = telemetry.streamOutcome
	requestLog.StreamErrorKind = telemetry.streamErrorKind
	requestLog.StreamErrorDetail = telemetry.streamErrorDetail
	if attempt.success {
		requestLog.applyRuntimePricingResult(telemetry.pricingResult)
	}
}

// ingressAuditRequestBudgetBytes is the aggregate request-body audit budget
// per ingress (12 MiB) from Requests SPEC §3.1; the final response body has a
// separate fixed 4 MiB reservation.
const ingressAuditRequestBudgetBytes = int64(12 * 1024 * 1024)

func buildRuntimeAuditLogRows(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext) []auditLogInsert {
	auditLogs := make([]auditLogInsert, 0, len(telemetry.attempts))
	requestBudgetRemaining := ingressAuditRequestBudgetBytes
	for index := range telemetry.attempts {
		attempt := telemetry.attemptContext(index)
		if !attempt.attempt.AuditEnabledAtRequest {
			continue
		}
		auditLogs = append(auditLogs, buildRuntimeAuditLogRow(plan, request, telemetry, attempt, &requestBudgetRemaining))
	}
	return auditLogs
}

func buildRuntimeAuditLogRow(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext, requestBudgetRemaining *int64) auditLogInsert {
	requestBody := runtimeCapturedAuditRequestBodyForOperation(plan.RuntimeOperation, attempt.attempt.AuditCaptureBodiesAtRequest, attempt.attempt.RequestBody)
	scrubbedRequestURL, requestURLTruncated := safediag.ScrubRequestURL(runtimeAuditRequestURL(attempt.attempt.RequestURL, request))
	scrubbedBaseURL, baseURLTruncated := safediag.ScrubEndpointBaseURL(attempt.attempt.Connection.Endpoint.BaseURL)
	requestHeaders := marshalAuditHeaders(attempt.attempt.RequestHeaders)
	responseHeaders := marshalAuditHTTPHeaders(attempt.attempt.ResponseHeaders)
	requestBodyBytes := []byte(nil)
	requestBodyStoredBytes := int64(0)
	requestBodyObserved := int64(0)
	requestBodyTruncated := false
	requestBodyStatus := "not_requested"
	requestBodyLimitReason := "none"
	requestBodyEndState := ""
	requestBodyEncoding := ""
	if requestBody != nil {
		requestBodyBytes = []byte(*requestBody)
		requestBodyObserved = int64(len(requestBodyBytes))
		// Per-body 4 MiB cap then shared 12 MiB ingress budget in immutable
		// launch order (Requests SPEC §5.2 allocation formula).
		perBodyCap := int64(auditBodyCapBytes)
		stored := requestBodyObserved
		if stored > perBodyCap {
			stored = perBodyCap
		}
		if requestBudgetRemaining != nil && stored > *requestBudgetRemaining {
			stored = *requestBudgetRemaining
		}
		if requestBudgetRemaining != nil {
			*requestBudgetRemaining -= stored
		}
		if stored > 0 {
			requestBodyBytes = requestBodyBytes[:stored]
			requestBodyStoredBytes = stored
			requestBodyStatus = "captured"
			requestBodyEndState = "complete"
			requestBodyEncoding = "utf8"
			if stored < requestBodyObserved {
				requestBodyStatus = "truncated"
				requestBodyTruncated = true
				switch {
				case requestBodyObserved > perBodyCap && stored >= perBodyCap:
					requestBodyLimitReason = "body_cap"
				case requestBodyObserved > perBodyCap && stored < perBodyCap:
					requestBodyLimitReason = "both"
				default:
					requestBodyLimitReason = "ingress_budget"
				}
			}
		} else if requestBodyObserved > 0 {
			requestBodyBytes = nil
			requestBodyStatus = "omitted_ingress_budget"
			requestBodyLimitReason = "ingress_budget"
		}
	}
	responseBodyBytes := []byte(nil)
	responseBodyObserved := int64(0)
	responseBodyStoredBytes := int64(0)
	responseBodyTruncated := false
	responseBodyStatus := "not_requested"
	responseBodyLimitReason := "none"
	responseBodyEndState := ""
	responseBodyEncoding := ""
	if attempt.isFinal && attempt.attempt.AuditCaptureBodiesAtRequest {
		if telemetry.capturedResponseBody != nil {
			responseBodyBytes = []byte(*telemetry.capturedResponseBody)
		}
		if telemetry.capturedResponseObserved > 0 || len(responseBodyBytes) > 0 {
			responseBodyObserved = telemetry.capturedResponseObserved
			if responseBodyObserved == 0 {
				responseBodyObserved = int64(len(responseBodyBytes))
			}
			responseBodyStoredBytes = int64(len(responseBodyBytes))
			responseBodyStatus = "captured"
			responseBodyEndState = "complete"
			responseBodyEncoding = "utf8"
			if telemetry.capturedResponseTruncated || (responseBodyStoredBytes > 0 && responseBodyStoredBytes < responseBodyObserved) {
				responseBodyStatus = "truncated"
				responseBodyTruncated = true
				responseBodyLimitReason = "body_cap"
			}
		}
	}
	auditLog := auditLogInsert{
		RequestLogAttemptNumber:           attempt.attemptNumber,
		ProfileID:                         plan.ProfileID,
		ModelID:                           plan.RequestedModelID,
		EndpointID:                        attempt.attempt.Connection.Endpoint.ID,
		ConnectionID:                      attempt.attempt.Connection.ID,
		EndpointBaseURL:                   scrubbedBaseURL,
		EndpointBaseURLTruncated:          baseURLTruncated,
		EndpointDescription:               attempt.attempt.Connection.Endpoint.Name,
		RequestMethod:                     request.Method,
		RequestURL:                        scrubbedRequestURL,
		RequestURLTruncated:               requestURLTruncated,
		RequestHeaders:                    requestHeaders,
		RequestBody:                       requestBody,
		RequestBodyStored:                 requestBodyStoredBytes > 0,
		ResponseStatus:                    attempt.attempt.StatusCode,
		ResponseHeaders:                   responseHeaders,
		ResponseBody:                      auditBodyString(responseBodyBytes),
		ResponseBodyStored:                responseBodyStoredBytes > 0,
		IsStream:                          telemetry.isStream,
		DurationMS:                        attempt.responseTimeMS,
		CreatedAt:                         attempt.createdAt,
		AuditEnabledAtRequest:             attempt.attempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest:       attempt.attempt.AuditCaptureBodiesAtRequest,
		RowKind:                           requestLogRowKindUpstream,
		AttemptNumber:                     intPtr(attempt.attemptNumber),
		AttemptDurationMS:                 nonNegativeIntPointer(attempt.attempt.AttemptDurationMS),
		UpstreamStatusCode:                upstreamStatusPointer(attempt.attempt),
		URLScrubProvenance:                runtimeURLScrubProvenanceRuntime,
		RequestHeadersScrubProvenance:     runtimeURLScrubProvenanceRuntime,
		ResponseHeadersScrubProvenance:    runtimeURLScrubProvenanceRuntime,
		RequestHeadersCaptureStatus:       runtimeAuditHeadersCaptureStatus(requestHeaders),
		ResponseHeadersCaptureStatus:      runtimeAuditHeadersCaptureStatusOptional(responseHeaders),
		RequestHeadersCaptureLimitReason:  "none",
		ResponseHeadersCaptureLimitReason: "none",
		RequestBodyCaptureProvenance:      "runtime_bytes",
		ResponseBodyCaptureProvenance:     "runtime_bytes",
		RequestBodyCaptureStatus:          requestBodyStatus,
		ResponseBodyCaptureStatus:         responseBodyStatus,
		RequestBodyCaptureLimitReason:     requestBodyLimitReason,
		ResponseBodyCaptureLimitReason:    responseBodyLimitReason,
		RequestBodyCaptureEndState:        optionalTrimmedStringPointer(requestBodyEndState),
		ResponseBodyCaptureEndState:       optionalTrimmedStringPointer(responseBodyEndState),
		RequestBodyEncoding:               optionalTrimmedStringPointer(requestBodyEncoding),
		ResponseBodyEncoding:              optionalTrimmedStringPointer(responseBodyEncoding),
		RequestBodyBytesObserved:          int64Ptr(requestBodyObserved),
		RequestBodyBytesStored:            int64Ptr(requestBodyStoredBytes),
		ResponseBodyBytesObserved:         int64Ptr(responseBodyObserved),
		ResponseBodyBytesStored:           int64Ptr(responseBodyStoredBytes),
		RequestBodyTruncated:              requestBodyTruncated,
		ResponseBodyTruncated:             responseBodyTruncated,
	}
	if attempt.isFinal && attempt.attempt.AuditCaptureBodiesAtRequest && responseBodyBytes != nil {
		auditLog.ResponseBody = auditBodyString(responseBodyBytes)
		auditLog.ResponseBodyStored = true
	}
	return auditLog
}

func auditBodyString(bytes []byte) *string {
	if bytes == nil {
		return nil
	}
	resolved := string(bytes)
	return &resolved
}

// upstreamStatusPointer returns the real upstream HTTP status when response
// headers were received; transport/no-response attempts stay null (Requests
// SPEC §5.2: never copy gateway 502 into an attempt HTTP status).
func upstreamStatusPointer(attempt executionAttempt) *int {
	if !attempt.ResponseHeadersReceived {
		return nil
	}
	return intPtr(attempt.StatusCode)
}

func resolvedTargetModelIDForResult(plan requestPlan, result executionResult) *string {
	if result.ResolvedTargetModelID != nil && strings.TrimSpace(*result.ResolvedTargetModelID) != "" {
		return result.ResolvedTargetModelID
	}
	return plan.ResolvedTargetModelID
}

func finalExecutionAttempt(result executionResult) *executionAttempt {
	if len(result.Attempts) == 0 {
		return nil
	}
	attempt := result.Attempts[len(result.Attempts)-1]
	return &attempt
}

func executionAttemptUpstreamOperationName(attempt *executionAttempt) *string {
	if attempt == nil {
		return nil
	}
	return trimmedStringPointer(attempt.UpstreamOperationName)
}

func executionAttemptUpstreamRequestPath(attempt *executionAttempt) *string {
	if attempt == nil {
		return nil
	}
	return trimmedStringPointer(attempt.UpstreamRequestPath)
}

func executionAttemptTranslationMode(attempt *executionAttempt) *string {
	if attempt == nil {
		return nil
	}
	return runtimeTranslationModePointer(attempt.OperationTranslationMode)
}

func runtimeEndpointLabelSnapshot(endpoint runtimeEndpoint) string {
	if endpoint.Name != nil {
		if label := strings.TrimSpace(*endpoint.Name); label != "" {
			return label
		}
	}
	if label := strings.TrimSpace(endpoint.BaseURL); label != "" {
		return label
	}
	if endpoint.ID > 0 {
		return fmt.Sprintf("Endpoint %d", endpoint.ID)
	}
	return "Unknown Endpoint"
}

func usageEventEndpointLabelSnapshotForInsert(usageEvent usageEventInsert) string {
	if label := strings.TrimSpace(usageEvent.EndpointLabelSnapshot); label != "" {
		return label
	}
	if usageEvent.EndpointID != nil {
		return fmt.Sprintf("Endpoint %d", *usageEvent.EndpointID)
	}
	return "Unknown Endpoint"
}

func buildRuntimeUsageEvent(plan requestPlan, result executionResult, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, requestLogCount int) usageEventInsert {
	attemptCount := upstreamAttemptCount(telemetry.attempts)
	finalAttempt := finalExecutionAttempt(result)
	usageEvent := usageEventInsert{
		ProfileID:                plan.ProfileID,
		IngressRequestID:         telemetry.ingressRequestID,
		ModelID:                  plan.RequestedModelID,
		ResolvedTargetModelID:    resolvedTargetModelIDForResult(plan, result),
		APIFamily:                plan.APIFamily,
		OperationName:            telemetry.operationName,
		UpstreamOperationName:    executionAttemptUpstreamOperationName(finalAttempt),
		OperationTranslationMode: executionAttemptTranslationMode(finalAttempt),
		EndpointID:               intPtr(result.Connection.Endpoint.ID),
		EndpointLabelSnapshot:    runtimeEndpointLabelSnapshot(result.Connection.Endpoint),
		ConnectionID:             intPtr(result.Connection.ID),
		SelectedTerminalTargetID: selectedTerminalTargetIDForUsageEvent(plan),
		ProxyAPIKeyID:            proxyKeyIDPointer(telemetry.proxyKey),
		ProxyAPIKeyNameSnapshot:  proxyKeyNamePointer(telemetry.proxyKey),
		StatusCode:               result.Response.StatusCode,
		SuccessFlag:              telemetry.successFlag,
		InputTokens:              telemetry.usage.InputTokens,
		OutputTokens:             telemetry.usage.OutputTokens,
		TotalTokens:              telemetry.usage.TotalTokens,
		CacheReadInputTokens:     telemetry.usage.CacheReadInputTokens,
		CacheCreationInputTokens: telemetry.usage.CacheCreationInputTokens,
		ReasoningTokens:          telemetry.usage.ReasoningTokens,
		AttemptCount:             attemptCount,
		RequestPath:              request.URL.Path,
		UpstreamRequestPath:      executionAttemptUpstreamRequestPath(finalAttempt),
		CreatedAt:                telemetry.requestCompletedAt,
		ResponseTimeMS:           intPtr(telemetry.responseTimeMS),
		CompletionDurationMS:     telemetry.completionDurationMS,
		TTFTMS:                   telemetry.ttftMS,
		StreamOutcome:            telemetry.streamOutcome,
		StreamErrorKind:          telemetry.streamErrorKind,
	}
	usageEvent.applyRuntimePricingResult(telemetry.pricingResult)
	applyRuntimeUsageEventFinalizedFields(&usageEvent, plan, result, telemetry, finalAttempt, requestLogCount)
	applyRuntimeUsageEventPricingScope(&usageEvent, usageEvent.StatusCode)
	return usageEvent
}

// applyRuntimeUsageEventPricingScope forces the finalized-ingress pricing
// classifier for the usage event: non-2xx finalized results are always
// ineligible (Pricing SPEC §3.4) with no reason/resolution/components, while
// 2xx abnormal streams stay in the pricing cohort.
func applyRuntimeUsageEventPricingScope(usageEvent *usageEventInsert, statusCode int) {
	if statusCode >= 200 && statusCode <= 299 {
		return
	}
	usageEvent.PricingStatus = runtimePricingStatusIneligible
	usageEvent.PricingEvidenceTrust = runtimePricingEvidenceTrust
	usageEvent.UnpricedReason = nil
	usageEvent.PricingResolutionKind = nil
	usageEvent.MissingPriceComponents = nil
}

// upstreamAttemptCount counts only real launched upstream rows; diagnostic
// rows never count as attempts.
func upstreamAttemptCount(attempts []executionAttempt) int {
	count := 0
	for _, attempt := range attempts {
		if attempt.LaunchOrdinal > 0 {
			count++
		}
	}
	if count == 0 && len(attempts) > 0 {
		count = 1
	}
	return count
}

// applyRuntimeUsageEventFinalizedFields fills the Observe finalized-ingress
// fields (Observe SPEC §3.5, Requests SPEC §3.6). The winner's identity comes
// from the executor's persisted arbitration; final_target_entry_trigger is the
// trigger of the winning actual target's first entry into the chain, never
// inferred from completion order.
func applyRuntimeUsageEventFinalizedFields(usageEvent *usageEventInsert, plan requestPlan, result executionResult, telemetry runtimeTelemetryEnvelopeContext, finalAttempt *executionAttempt, requestLogCount int) {
	expectedRows := requestLogCount
	if expectedRows < 1 {
		expectedRows = 1
	}
	usageEvent.ExpectedRequestLogRowCount = intPtr(expectedRows)
	usageEvent.ProxyAPIKeyIDSnapshot = proxyKeyIDPointer(telemetry.proxyKey)
	usageEvent.ProxyAPIKeyAttributionState = runtimeProxyKeyAttributionState(telemetry.proxyKey)

	// Routing evidence from persisted triggers.
	var winnerAttempt *executionAttempt
	for index := range telemetry.attempts {
		attempt := telemetry.attempts[index]
		switch attempt.AttemptTrigger {
		case attemptTriggerRetrySameTarget:
			usageEvent.SameTargetRetryOccurred = true
		case attemptTriggerHedge:
			usageEvent.HedgeOccurred = true
		case attemptTriggerFailover:
			usageEvent.FailoverOccurred = true
		}
		if telemetry.winnerOrdinal > 0 && attempt.LaunchOrdinal == telemetry.winnerOrdinal {
			winnerAttempt = &telemetry.attempts[index]
		}
	}
	if winnerAttempt == nil && finalAttempt != nil {
		winnerAttempt = finalAttempt
	}

	// Zero-launched requests: expected counts still reconcile (Observe SPEC
	// §3.5), but final attempt/target-entry identity stays null.
	if winnerAttempt != nil && winnerAttempt.LaunchOrdinal > 0 {
		usageEvent.FinalAttemptNumber = intPtr(winnerAttempt.LaunchOrdinal)
		usageEvent.FinalAttemptTrigger = optionalTrimmedStringPointer(winnerAttempt.AttemptTrigger)
		// The winning actual target's first entry trigger: the first launched
		// attempt targeting the same connection.
		for index := range telemetry.attempts {
			attempt := telemetry.attempts[index]
			if attempt.Connection.ID == winnerAttempt.Connection.ID && validateAttemptTrigger(attempt.AttemptTrigger) {
				usageEvent.FinalTargetEntryTrigger = optionalTrimmedStringPointer(attempt.AttemptTrigger)
				break
			}
		}
		if usageEvent.FinalTargetEntryTrigger == nil {
			usageEvent.FinalTargetEntryTrigger = optionalTrimmedStringPointer(winnerAttempt.AttemptTrigger)
		}
	}
	if !usageEvent.SameTargetRetryOccurred && !usageEvent.HedgeOccurred && !usageEvent.FailoverOccurred {
		// New writer rows always carry authoritative triggers for launched
		// attempts; a single-attempt ingress is complete evidence.
		usageEvent.RoutingEvidenceComplete = boolPtr(telemetry.winnerOrdinal > 0 || len(telemetry.attempts) > 0)
	} else {
		usageEvent.RoutingEvidenceComplete = boolPtr(true)
	}

	// Ingress wall-clock only from authoritative finalized evidence.
	if !telemetry.ingressStartedAt.IsZero() && !telemetry.requestCompletedAt.IsZero() {
		startedAt := telemetry.ingressStartedAt
		completedAt := telemetry.requestCompletedAt.UTC()
		usageEvent.IngressStartedAt = &startedAt
		usageEvent.IngressCompletedAt = &completedAt
	}

	// Terminal error code for failed/client-disconnected final results
	// (Requests SPEC §3.6): never copied into an upstream row, never invented
	// for completed results.
	if !usageEvent.SuccessFlag {
		switch telemetry.streamOutcome {
		case runtimeStreamOutcomeClientDisconnected:
			usageEvent.FinalErrorCode = stringPtr(safediag.CodeClientDisconnected)
		default:
			if winnerAttempt != nil && winnerAttempt.Diagnostics != nil && winnerAttempt.Diagnostics.Code != "" {
				usageEvent.FinalErrorCode = stringPtr(winnerAttempt.Diagnostics.Code)
			} else if winnerAttempt != nil && winnerAttempt.StatusCode >= 200 && winnerAttempt.StatusCode < 300 {
				// Abnormal 2xx stream without a captured code.
				usageEvent.FinalErrorCode = optionalTrimmedStringPointer(safediag.StreamOutcomeFallbackCode(telemetry.streamOutcome))
			} else if winnerAttempt != nil {
				usageEvent.FinalErrorCode = stringPtr(safediag.HTTPFallbackCode(winnerAttempt.StatusCode))
			}
		}
	}
}

func buildRuntimeAccountingFinalEvent(event usageEventInsert, requestLogs []requestLogInsert, routeReason gatewaycore.RouteReason, usageSource gatewaycore.UsageSource) gatewayaccounting.Event {
	finalAuditEnabled, finalAuditCaptureBodies := runtimeAccountingFinalAuditState(requestLogs)
	accountingEvent, err := gatewayaccounting.NewEvent(gatewayaccounting.Event{
		Phase:                    gatewayaccounting.EventPhaseFinal,
		RequestID:                event.IngressRequestID,
		ProfileID:                event.ProfileID,
		OperationName:            event.OperationName,
		APIFamily:                event.APIFamily,
		RequestedModelID:         event.ModelID,
		EffectiveModelID:         cloneRuntimeStringPointer(event.ResolvedTargetModelID),
		EndpointID:               cloneRuntimeIntPointer(event.EndpointID),
		ConnectionID:             cloneRuntimeIntPointer(event.ConnectionID),
		SelectedTerminalTargetID: cloneRuntimeIntPointer(event.SelectedTerminalTargetID),
		AttemptNumber:            event.AttemptCount,
		Final:                    true,
		StatusCode:               event.StatusCode,
		Success:                  event.SuccessFlag,
		RouteReason:              routeReason,
		UsageSource:              usageSource,
		PricingConfigVersionUsed: cloneRuntimeIntPointer(event.PricingConfigVersionUsed),
		StreamOutcome:            event.StreamOutcome,
		AuditEnabled:             finalAuditEnabled,
		AuditCaptureBodies:       finalAuditCaptureBodies,
		ObservedAt:               event.CreatedAt,
	})
	if err != nil {
		return gatewayaccounting.Event{}
	}
	return accountingEvent
}

func runtimeAccountingFinalAuditState(requestLogs []requestLogInsert) (bool, bool) {
	if len(requestLogs) == 0 {
		return false, false
	}
	finalLog := requestLogs[len(requestLogs)-1]
	return finalLog.AuditEnabledAtRequest, finalLog.AuditCaptureBodiesAtRequest
}

func buildRuntimeAccountingAttemptEvents(requestLogs []requestLogInsert, routeReason gatewaycore.RouteReason, usageSource gatewaycore.UsageSource) []gatewayaccounting.Event {
	events := make([]gatewayaccounting.Event, 0, len(requestLogs))
	for index, requestLog := range requestLogs {
		attemptUsageSource := gatewaycore.UsageSourceMissing
		if index == len(requestLogs)-1 {
			attemptUsageSource = usageSource
		}
		accountingEvent, err := gatewayaccounting.NewEvent(gatewayaccounting.Event{
			Phase:                    gatewayaccounting.EventPhaseAttempt,
			RequestID:                requestLog.IngressRequestID,
			ProfileID:                requestLog.ProfileID,
			OperationName:            requestLog.OperationName,
			APIFamily:                requestLog.APIFamily,
			RequestedModelID:         requestLog.ModelID,
			EffectiveModelID:         cloneRuntimeStringPointer(requestLog.ResolvedTargetModelID),
			EndpointID:               cloneRuntimeIntPointer(requestLog.EndpointID),
			ConnectionID:             cloneRuntimeIntPointer(requestLog.ConnectionID),
			SelectedTerminalTargetID: cloneRuntimeIntPointer(requestLog.SelectedTerminalTargetID),
			AttemptNumber:            requestLog.AttemptNumber,
			Final:                    index == len(requestLogs)-1,
			StatusCode:               requestLog.StatusCode,
			Success:                  requestLog.SuccessFlag,
			RouteReason:              routeReason,
			UsageSource:              attemptUsageSource,
			PricingConfigVersionUsed: cloneRuntimeIntPointer(requestLog.PricingConfigVersionUsed),
			StreamOutcome:            requestLog.StreamOutcome,
			AuditEnabled:             requestLog.AuditEnabledAtRequest,
			AuditCaptureBodies:       requestLog.AuditCaptureBodiesAtRequest,
			ObservedAt:               requestLog.CreatedAt,
		})
		if err != nil {
			continue
		}
		events = append(events, accountingEvent)
	}
	return events
}

func runtimeUsageSourceFromCapture(capture runtimeResponseCapture, usage responseUsage, streamOutcome string) gatewaycore.UsageSource {
	if capture.UsageSource != "" {
		return gatewayaccounting.NormalizeUsageSource(capture.UsageSource)
	}
	return runtimeUsageSourceFromUsage(usage, streamOutcome)
}

func runtimeUsageSourceFromUsage(usage responseUsage, streamOutcome string) gatewaycore.UsageSource {
	if !usage.hasValues() {
		return gatewaycore.UsageSourceMissing
	}
	if runtimeStreamOutcomeIsStreaming(streamOutcome) {
		return gatewaycore.UsageSourceProviderStreamTerminal
	}
	return gatewaycore.UsageSourceProvider
}

func runtimeResponseTiming(startedAt time.Time, completedAt time.Time, isStream bool, capture runtimeResponseCapture) (*int, *int) {
	var ttftMS *int
	if capture.FirstMeaningfulPayloadAt != nil {
		ttft := durationMilliseconds(capture.FirstMeaningfulPayloadAt.Sub(startedAt))
		ttftMS = &ttft
	}
	finishedAt := completedAt
	if capture.CompletedAt != nil && !capture.CompletedAt.IsZero() {
		finishedAt = capture.CompletedAt.UTC()
	}
	if !isStream {
		completionDuration := durationMilliseconds(finishedAt.Sub(startedAt))
		return ttftMS, &completionDuration
	}
	if capture.CompletedAt == nil {
		return ttftMS, nil
	}
	completionDuration := durationMilliseconds(finishedAt.Sub(startedAt))
	return ttftMS, &completionDuration
}

func runtimeStreamOutcomeForTelemetry(outcome string) string {
	switch strings.TrimSpace(outcome) {
	case runtimeStreamOutcomeNotStreaming:
		return runtimeStreamOutcomeNotStreaming
	case runtimeStreamOutcomeCompleted:
		return runtimeStreamOutcomeCompleted
	case runtimeStreamOutcomeProviderIncomplete:
		return runtimeStreamOutcomeProviderIncomplete
	case runtimeStreamOutcomeClientDisconnected:
		return runtimeStreamOutcomeClientDisconnected
	case runtimeStreamOutcomeUpstreamReadError:
		return runtimeStreamOutcomeUpstreamReadError
	case runtimeStreamOutcomeUpstreamEndedWithoutTerminal:
		return runtimeStreamOutcomeUpstreamEndedWithoutTerminal
	case runtimeStreamOutcomeUnknown:
		return runtimeStreamOutcomeUnknown
	default:
		return runtimeStreamOutcomeUnknown
	}
}

func runtimeStreamOutcomeIsStreaming(outcome string) bool {
	return runtimeStreamOutcomeForTelemetry(outcome) != runtimeStreamOutcomeNotStreaming
}

func runtimeCapturedAuditBody(enabled bool, body []byte) *string {
	if !enabled || len(body) == 0 {
		return nil
	}
	resolved := string(body)
	return &resolved
}

func runtimeAuditRequestURL(requestURL string, request *http.Request) string {
	trimmed := strings.TrimSpace(requestURL)
	if trimmed != "" {
		return trimmed
	}
	if request == nil || request.URL == nil {
		return ""
	}
	return request.URL.String()
}

// marshalAuditHeaders serializes scrubbed canonical request header entries.
// Every value is irreversibly scrubbed with the fixed-bottom-line matcher
// (Requests SPEC §5.5) before it may enter telemetry; pre-scrub values never
// reach the outbox, staging, DB, logs, or traces.
func marshalAuditHeaders(headers map[string]string) string {
	entries := scrubAuditHeaderMap(headers)
	encoded, err := json.Marshal(entries)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func marshalAuditHTTPHeaders(headers http.Header) *string {
	entries := scrubAuditHTTPHeaderEntries(headers)
	if entries == nil {
		return nil
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil
	}
	resolved := string(encoded)
	return &resolved
}

// scrubAuditHeaderMap returns canonical lowercased-name entries with every
// value redacted when its name is sensitive; non-sensitive values keep their
// original bytes (they were already sanitized by buildUpstreamHeaders).
func scrubAuditHeaderMap(headers map[string]string) []auditHeaderEntry {
	if len(headers) == 0 {
		return []auditHeaderEntry{}
	}
	matcher := safediag.NewSensitiveNameMatcher()
	entries := make([]auditHeaderEntry, 0, len(headers))
	for key, value := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
		entry := auditHeaderEntry{Name: normalizedKey, Value: scrubbed.Value}
		if matcher.IsSensitiveName(normalizedKey) {
			entry.Value = safediag.RedactedMarker
		} else {
			// Non-sensitive names still carry caller-controlled values: the
			// fixed-bottom-line value scrubber removes embedded credentials
			// (Bearer/sk-/token= fragments) while preserving safe text.
			scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxAuditHeaderValueBytes})
			entry.Value = scrubbed.Value
		}
		entries = append(entries, entry)
	}
	sortAuditHeaderEntries(entries)
	return entries
}

func scrubAuditHTTPHeaderEntries(headers http.Header) []auditHeaderEntry {
	if len(headers) == 0 {
		return nil
	}
	matcher := safediag.NewSensitiveNameMatcher()
	entries := make([]auditHeaderEntry, 0, len(headers))
	for key, values := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		for _, value := range values {
			scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
			entry := auditHeaderEntry{Name: normalizedKey, Value: scrubbed.Value}
			if matcher.IsSensitiveName(normalizedKey) {
				entry.Value = safediag.RedactedMarker
			} else {
				scrubbed := safediag.ScrubValue(value, safediag.ScrubOptions{MaxBytes: safediag.MaxAuditHeaderValueBytes})
				entry.Value = scrubbed.Value
			}
			entries = append(entries, entry)
		}
	}
	sortAuditHeaderEntries(entries)
	return entries
}

type auditHeaderEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// sortAuditHeaderEntries orders entries by lowercase name then original value
// ordinal (stable), preserving duplicate values.
func sortAuditHeaderEntries(entries []auditHeaderEntry) {
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].Name != entries[right].Name {
			return entries[left].Name < entries[right].Name
		}
		return entries[left].Value < entries[right].Value
	})
}

func materializeRuntimeTelemetryEnvelopeTx(ctx context.Context, tx pgx.Tx, logPartitions *runtimeLogPartitionCache, envelope runtimeTelemetryEnvelope) (int, error) {
	envelope = normalizeRuntimeTelemetryEnvelopeTimestamps(envelope)
	for index := range envelope.RequestLogs {
		envelope.RequestLogs[index].ProxyAPIKeyAuthEnforcedAtRequest = envelope.ProxyKeyAuthEnforced
	}
	envelope.UsageEvent.ProxyAPIKeyAuthEnforcedAtRequest = envelope.ProxyKeyAuthEnforced
	if err := ensureRuntimeTelemetryPartitions(ctx, logPartitions, envelope); err != nil {
		return 0, err
	}
	requestLogID, err := insertRequestLogsAndUsageEventTx(ctx, tx, envelope.RequestLogs, envelope.AuditLogs, envelope.UsageEvent)
	if err != nil {
		return 0, err
	}
	if err := recordRuntimeProxyKeyUsageTx(ctx, tx, envelope.ProxyKeyUsage); err != nil {
		return 0, err
	}
	return requestLogID, nil
}

func normalizeRuntimeTelemetryEnvelopeTimestamps(envelope runtimeTelemetryEnvelope) runtimeTelemetryEnvelope {
	requestCreatedAtByAttempt := make(map[int]time.Time, len(envelope.RequestLogs))
	for index := range envelope.RequestLogs {
		envelope.RequestLogs[index].CreatedAt = envelope.RequestLogs[index].CreatedAt.UTC()
		requestCreatedAtByAttempt[envelope.RequestLogs[index].AttemptNumber] = envelope.RequestLogs[index].CreatedAt
	}
	for index := range envelope.AuditLogs {
		if createdAt, ok := requestCreatedAtByAttempt[envelope.AuditLogs[index].RequestLogAttemptNumber]; ok {
			envelope.AuditLogs[index].CreatedAt = createdAt
		} else {
			envelope.AuditLogs[index].CreatedAt = envelope.AuditLogs[index].CreatedAt.UTC()
		}
	}
	if len(envelope.RequestLogs) > 0 {
		envelope.UsageEvent.CreatedAt = envelope.RequestLogs[len(envelope.RequestLogs)-1].CreatedAt
	} else {
		envelope.UsageEvent.CreatedAt = envelope.UsageEvent.CreatedAt.UTC()
	}
	if len(envelope.AccountingAttempts) > 0 {
		for index := range envelope.AccountingAttempts {
			if createdAt, ok := requestCreatedAtByAttempt[envelope.AccountingAttempts[index].AttemptNumber]; ok {
				envelope.AccountingAttempts[index].ObservedAt = createdAt
			} else {
				envelope.AccountingAttempts[index].ObservedAt = envelope.AccountingAttempts[index].ObservedAt.UTC()
			}
		}
	}
	if !envelope.AccountingEvent.ObservedAt.IsZero() {
		envelope.AccountingEvent.ObservedAt = envelope.UsageEvent.CreatedAt.UTC()
	}
	return envelope
}

func ensureRuntimeTelemetryPartitions(ctx context.Context, logPartitions *runtimeLogPartitionCache, envelope runtimeTelemetryEnvelope) error {
	if logPartitions == nil {
		return fmt.Errorf("runtime log partition ensurer unavailable")
	}
	for _, requestLog := range envelope.RequestLogs {
		if err := logPartitions.EnsurePartitionForTime(ctx, "request_logs", requestLog.CreatedAt); err != nil {
			return err
		}
	}
	for _, auditLog := range envelope.AuditLogs {
		if err := logPartitions.EnsurePartitionForTime(ctx, "audit_logs", auditLog.CreatedAt); err != nil {
			return err
		}
	}
	if err := logPartitions.EnsurePartitionForTime(ctx, "usage_request_events", envelope.UsageEvent.CreatedAt); err != nil {
		return err
	}
	return nil
}

func insertRequestLogsAndUsageEventTx(ctx context.Context, tx pgx.Tx, requestLogs []requestLogInsert, auditLogs []auditLogInsert, usageEvent usageEventInsert) (int, error) {
	auditByAttempt := make(map[int]auditLogInsert, len(auditLogs))
	for _, auditLog := range auditLogs {
		auditByAttempt[auditLog.RequestLogAttemptNumber] = auditLog
	}
	var requestLogID int
	for _, requestLog := range requestLogs {
		err := tx.QueryRow(
			ctx,
			`INSERT INTO request_logs (
				profile_id, model_id, resolved_target_model_id, api_family, operation_name,
				row_kind, caller_request_id, url_scrub_provenance, metadata_redacted_fields, metadata_truncated_fields,
				 endpoint_id, connection_id, selected_terminal_target_id, proxy_api_key_id_snapshot, proxy_api_key_name_snapshot, proxy_api_key_attribution_state,
				ingress_request_id, attempt_number, attempt_trigger, attempt_result, is_winner, attempt_duration_ms, legacy_duration_ms,
				provider_correlation_id, endpoint_base_url, endpoint_description,
				upstream_status_code, gateway_status_code, legacy_status_code,
				error_source, error_code, failure_stage, error_detail, error_detail_redacted, error_detail_truncated,
				stream_error_detail, stream_error_detail_redacted, stream_error_detail_truncated,
				upstream_request_started, response_headers_received, first_body_or_stream_event_seen,
				is_stream, input_tokens, output_tokens, total_tokens, success_flag,
				cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
				input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros,
				total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol,
				fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
				pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used,
				pricing_status, unpriced_reason, pricing_resolution_kind, missing_price_components, pricing_evidence_trust,
				pricing_template_id_used, pricing_template_name_snapshot, pricing_template_revision_id_used, reporting_currency_epoch,
				request_path, created_at, caller_user_agent, upstream_user_agent,
				completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind,
				audit_enabled_at_request, audit_capture_bodies_at_request,
				request_generation_params, request_generation_params_status, upstream_operation_name, operation_translation_mode, upstream_request_path,
				proxy_api_key_auth_enforced_at_request
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, CASE WHEN $14::bigint IS NOT NULL AND $15::varchar IS NOT NULL THEN 'identified' WHEN $14::bigint IS NULL AND $15::varchar IS NULL THEN 'none' ELSE 'unknown' END, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51, $52, $53, $54, $55, $56, $57, $58, $59, $60, $61, $62, $63, $64, $65, $66, $67, $68, $69, $70, $71, $72, $73, $74, $75, $76, $77, $78, $79, $80, $81, $82, $83, $84, $85, $86, $87, $88, $89, $90, $91, $92) RETURNING id`,
			requestLog.ProfileID,
			requestLog.ModelID,
			nullableStringArg(requestLog.ResolvedTargetModelID),
			requestLog.APIFamily,
			requestLog.OperationName,
			requestLog.RowKind,
			nullableStringArg(requestLog.CallerRequestID),
			requestLog.URLScrubProvenance,
			notNullStringArrayArg(requestLog.MetadataRedactedFields),
			notNullStringArrayArg(requestLog.MetadataTruncatedFields),
			nullableIntArg(requestLog.EndpointID),
			nullableIntArg(requestLog.ConnectionID),
			nullableIntArg(requestLog.SelectedTerminalTargetID),
			nullableIntArg(requestLog.ProxyAPIKeyID),
			nullableStringArg(requestLog.ProxyAPIKeyNameSnapshot),
			requestLog.IngressRequestID,
			nullableAttemptNumberArg(requestLog.RowKind, requestLog.AttemptNumber),
			nullableStringArg(requestLog.AttemptTrigger),
			nullableStringArg(requestLog.AttemptResult),
			nullableBoolArg(requestLog.IsWinner),
			nullableIntArg(requestLog.AttemptDurationMS),
			nullableIntArg(requestLog.LegacyDurationMS),
			nullableStringArg(requestLog.ProviderCorrelationID),
			nullableStringArg(requestLog.EndpointBaseURL),
			nullableStringArg(requestLog.EndpointDescription),
			nullableIntArg(requestLog.UpstreamStatusCode),
			nullableIntArg(requestLog.GatewayStatusCode),
			nullableIntArg(requestLog.LegacyStatusCode),
			nullableStringArg(requestLog.ErrorSource),
			nullableStringArg(requestLog.ErrorCode),
			nullableStringArg(requestLog.FailureStage),
			nullableStringArg(requestLog.ErrorDetail),
			requestLog.ErrorDetailRedacted,
			requestLog.ErrorDetailTruncated,
			nullableStringArg(requestLog.StreamErrorDetail),
			requestLog.StreamErrorDetailRedacted,
			requestLog.StreamErrorDetailTruncated,
			nullableBoolArg(requestLog.UpstreamRequestStarted),
			nullableBoolArg(requestLog.ResponseHeadersReceived),
			nullableBoolArg(requestLog.FirstBodyOrStreamEventSeen),
			requestLog.IsStream,
			nullableIntArg(requestLog.InputTokens),
			nullableIntArg(requestLog.OutputTokens),
			nullableIntArg(requestLog.TotalTokens),
			requestLog.SuccessFlag,
			nullableIntArg(requestLog.CacheReadInputTokens),
			nullableIntArg(requestLog.CacheCreationInputTokens),
			nullableIntArg(requestLog.ReasoningTokens),
			nullableInt64Arg(requestLog.InputCostMicros),
			nullableInt64Arg(requestLog.OutputCostMicros),
			nullableInt64Arg(requestLog.CacheReadInputCostMicros),
			nullableInt64Arg(requestLog.CacheCreationInputCostMicros),
			nullableInt64Arg(requestLog.ReasoningCostMicros),
			nullableInt64Arg(requestLog.TotalCostOriginalMicros),
			nullableInt64Arg(requestLog.TotalCostUserCurrencyMicros),
			nullableStringArg(requestLog.CurrencyCodeOriginal),
			nullableStringArg(requestLog.ReportCurrencyCode),
			nullableStringArg(requestLog.ReportCurrencySymbol),
			nullableStringArg(requestLog.FXRateUsed),
			nullableStringArg(requestLog.FXRateSource),
			nullableStringArg(requestLog.PricingSnapshotUnit),
			nullableStringArg(requestLog.PricingSnapshotInput),
			nullableStringArg(requestLog.PricingSnapshotOutput),
			nullableStringArg(requestLog.PricingSnapshotCacheReadInput),
			nullableStringArg(requestLog.PricingSnapshotCacheCreationInput),
			nullableStringArg(requestLog.PricingSnapshotReasoning),
			nullableIntArg(requestLog.PricingConfigVersionUsed),
			requestLog.PricingStatus,
			nullableStringArg(requestLog.UnpricedReason),
			nullableStringArg(requestLog.PricingResolutionKind),
			nullableStringSliceArg(requestLog.MissingPriceComponents),
			requestLog.PricingEvidenceTrust,
			nullableIntArg(requestLog.PricingTemplateIDUsed),
			nullableStringArg(requestLog.PricingTemplateNameSnapshot),
			nullableInt64Arg(requestLog.PricingTemplateRevisionIDUsed),
			nullableIntArg(requestLog.ReportingCurrencyEpoch),
			requestLog.RequestPath,
			requestLog.CreatedAt.UTC(),
			nullableStringArg(requestLog.CallerUserAgent),
			nullableStringArg(requestLog.UpstreamUserAgent),
			nullableIntArg(requestLog.CompletionDurationMS),
			nullableIntArg(requestLog.TTFTMS),
			requestLog.StreamOutcome,
			nullableStringArg(requestLog.StreamErrorKind),
			requestLog.AuditEnabledAtRequest,
			requestLog.AuditCaptureBodiesAtRequest,
			nullableJSONArg(requestLog.RequestGenerationParams),
			nullableStringArg(requestLog.RequestGenerationParamsStatus),
			nullableStringArg(requestLog.UpstreamOperationName),
			nullableStringArg(requestLog.OperationTranslationMode),
			nullableStringArg(requestLog.UpstreamRequestPath),
			nullableBoolArg(requestLog.ProxyAPIKeyAuthEnforcedAtRequest),
		).Scan(&requestLogID)
		if err != nil {
			return 0, fmt.Errorf("insert request log: %w (row_kind=%s pricing_status=%s reason=%v resolution=%v components=%v trust=%s)", err, requestLog.RowKind, requestLog.PricingStatus, dereferenceString(requestLog.UnpricedReason), dereferenceString(requestLog.PricingResolutionKind), requestLog.MissingPriceComponents, requestLog.PricingEvidenceTrust)
		}
		if auditLog, ok := auditByAttempt[requestLog.AttemptNumber]; ok {
			auditLog.CreatedAt = requestLog.CreatedAt
			if err := insertRuntimeAuditLogTx(ctx, tx, requestLogID, requestLog.CreatedAt, requestLog.IngressRequestID, auditLog); err != nil {
				return 0, err
			}
		}
	}
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO usage_request_events (
			profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, operation_name,
			endpoint_id, connection_id, selected_terminal_target_id, proxy_api_key_name_snapshot,
			status_code, success_flag, input_tokens, output_tokens, total_tokens,
			cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens,
			input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol,
			fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
			pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used,
			attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind,
			pricing_status, unpriced_reason, pricing_resolution_kind, missing_price_components, pricing_evidence_trust,
			pricing_template_id_used, pricing_template_name_snapshot, pricing_template_revision_id_used, reporting_currency_epoch,
			expected_request_log_row_count, final_attempt_number, final_attempt_trigger, final_target_entry_trigger,
			same_target_retry_occurred, hedge_occurred, failover_occurred, routing_evidence_complete, final_error_code,
			ingress_started_at, ingress_completed_at, proxy_api_key_id_snapshot, proxy_api_key_attribution_state,
			upstream_operation_name, operation_translation_mode, upstream_request_path, endpoint_label_snapshot,
			proxy_api_key_auth_enforced_at_request
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51, $52, $53, $54, $55, $56, $57, $58, $59, $60, $61, $62, $63, $64, $65, $66, $67, $68, $69, $70, $71, $72)`,
		usageEvent.ProfileID,
		usageEvent.IngressRequestID,
		usageEvent.ModelID,
		nullableStringArg(usageEvent.ResolvedTargetModelID),
		usageEvent.APIFamily,
		usageEvent.OperationName,
		nullableIntArg(usageEvent.EndpointID),
		nullableIntArg(usageEvent.ConnectionID),
		nullableIntArg(usageEvent.SelectedTerminalTargetID),
		nullableStringArg(usageEvent.ProxyAPIKeyNameSnapshot),
		usageEvent.StatusCode,
		usageEvent.SuccessFlag,
		nullableIntArg(usageEvent.InputTokens),
		nullableIntArg(usageEvent.OutputTokens),
		nullableIntArg(usageEvent.TotalTokens),
		nullableIntArg(usageEvent.CacheReadInputTokens),
		nullableIntArg(usageEvent.CacheCreationInputTokens),
		nullableIntArg(usageEvent.ReasoningTokens),
		nullableInt64Arg(usageEvent.InputCostMicros),
		nullableInt64Arg(usageEvent.OutputCostMicros),
		nullableInt64Arg(usageEvent.CacheReadInputCostMicros),
		nullableInt64Arg(usageEvent.CacheCreationInputCostMicros),
		nullableInt64Arg(usageEvent.ReasoningCostMicros),
		nullableInt64Arg(usageEvent.TotalCostOriginalMicros),
		nullableInt64Arg(usageEvent.TotalCostUserCurrencyMicros),
		nullableStringArg(usageEvent.CurrencyCodeOriginal),
		nullableStringArg(usageEvent.ReportCurrencyCode),
		nullableStringArg(usageEvent.ReportCurrencySymbol),
		nullableStringArg(usageEvent.FXRateUsed),
		nullableStringArg(usageEvent.FXRateSource),
		nullableStringArg(usageEvent.PricingSnapshotUnit),
		nullableStringArg(usageEvent.PricingSnapshotInput),
		nullableStringArg(usageEvent.PricingSnapshotOutput),
		nullableStringArg(usageEvent.PricingSnapshotCacheReadInput),
		nullableStringArg(usageEvent.PricingSnapshotCacheCreationInput),
		nullableStringArg(usageEvent.PricingSnapshotReasoning),
		nullableIntArg(usageEvent.PricingConfigVersionUsed),
		usageEvent.AttemptCount,
		usageEvent.RequestPath,
		usageEvent.CreatedAt.UTC(),
		nullableIntArg(usageEvent.ResponseTimeMS),
		nullableIntArg(usageEvent.CompletionDurationMS),
		nullableIntArg(usageEvent.TTFTMS),
		usageEvent.StreamOutcome,
		nullableStringArg(usageEvent.StreamErrorKind),
		usageEvent.PricingStatus,
		nullableStringArg(usageEvent.UnpricedReason),
		nullableStringArg(usageEvent.PricingResolutionKind),
		nullableStringSliceArg(usageEvent.MissingPriceComponents),
		usageEvent.PricingEvidenceTrust,
		nullableIntArg(usageEvent.PricingTemplateIDUsed),
		nullableStringArg(usageEvent.PricingTemplateNameSnapshot),
		nullableInt64Arg(usageEvent.PricingTemplateRevisionIDUsed),
		nullableIntArg(usageEvent.ReportingCurrencyEpoch),
		nullableIntArg(usageEvent.ExpectedRequestLogRowCount),
		nullableIntArg(usageEvent.FinalAttemptNumber),
		nullableStringArg(usageEvent.FinalAttemptTrigger),
		nullableStringArg(usageEvent.FinalTargetEntryTrigger),
		usageEvent.SameTargetRetryOccurred,
		usageEvent.HedgeOccurred,
		usageEvent.FailoverOccurred,
		nullableBoolArg(usageEvent.RoutingEvidenceComplete),
		nullableStringArg(usageEvent.FinalErrorCode),
		nullableTimeArg(usageEvent.IngressStartedAt),
		nullableTimeArg(usageEvent.IngressCompletedAt),
		nullableIntArg(usageEvent.ProxyAPIKeyIDSnapshot),
		usageEvent.ProxyAPIKeyAttributionState,
		nullableStringArg(usageEvent.UpstreamOperationName),
		nullableStringArg(usageEvent.OperationTranslationMode),
		nullableStringArg(usageEvent.UpstreamRequestPath),
		usageEventEndpointLabelSnapshotForInsert(usageEvent),
		nullableBoolArg(usageEvent.ProxyAPIKeyAuthEnforcedAtRequest),
	); err != nil {
		return 0, fmt.Errorf("insert usage event: %w (ingress=%s status=%d pricing_status=%s trust=%s created=%s)", err, usageEvent.IngressRequestID, usageEvent.StatusCode, usageEvent.PricingStatus, usageEvent.PricingEvidenceTrust, usageEvent.CreatedAt.UTC().Format(time.RFC3339))
	}
	requestTimes := make([]time.Time, 0, len(requestLogs))
	for _, requestLog := range requestLogs {
		requestTimes = append(requestTimes, requestLog.CreatedAt)
	}
	auditTimes := make([]time.Time, 0, len(auditLogs))
	for _, auditLog := range auditLogs {
		auditTimes = append(auditTimes, auditLog.CreatedAt)
	}
	if err := statsdomain.RecordActualCoverageAppend(ctx, tx, "request_logs", requestTimes, usageEvent.CreatedAt); err != nil {
		return 0, fmt.Errorf("advance request-log coverage owner: %w", err)
	}
	if err := statsdomain.RecordActualCoverageAppend(ctx, tx, "audit_logs", auditTimes, usageEvent.CreatedAt); err != nil {
		return 0, fmt.Errorf("advance audit coverage owner: %w", err)
	}
	if err := statsdomain.RecordActualCoverageAppend(ctx, tx, "usage_request_events", []time.Time{usageEvent.CreatedAt}, usageEvent.CreatedAt); err != nil {
		return 0, fmt.Errorf("advance usage coverage owner: %w", err)
	}
	return requestLogID, nil
}

func insertRuntimeAuditLogTx(ctx context.Context, tx pgx.Tx, requestLogID int, requestLogCreatedAt time.Time, ingressRequestID string, auditLog auditLogInsert) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO audit_logs (
			request_log_id, request_log_created_at, ingress_request_id, profile_id, model_id,
			endpoint_id, connection_id, endpoint_base_url, endpoint_description,
			request_method, request_url, request_url_truncated, endpoint_base_url_truncated,
			request_headers, request_headers_scrub_provenance, request_headers_capture_status, request_headers_capture_limit_reason,
			response_headers, response_headers_scrub_provenance, response_headers_capture_status, response_headers_capture_limit_reason,
			request_body, request_body_encoding, request_body_capture_provenance, request_body_capture_end_state,
			request_body_capture_status, request_body_capture_limit_reason, request_body_truncated,
			request_body_bytes_observed, request_body_bytes_stored,
			response_body, response_body_encoding, response_body_capture_provenance, response_body_capture_end_state,
			response_body_capture_status, response_body_capture_limit_reason, response_body_truncated,
			response_body_bytes_observed, response_body_bytes_stored,
			row_kind, attempt_number, attempt_duration_ms, upstream_status_code,
			url_scrub_provenance, is_stream, created_at,
			audit_enabled_at_request, audit_capture_bodies_at_request
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48)`,
		requestLogID,
		requestLogCreatedAt.UTC(),
		ingressRequestID,
		auditLog.ProfileID,
		auditLog.ModelID,
		auditLog.EndpointID,
		auditLog.ConnectionID,
		auditLog.EndpointBaseURL,
		nullableStringArg(auditLog.EndpointDescription),
		auditLog.RequestMethod,
		auditLog.RequestURL,
		auditLog.RequestURLTruncated,
		auditLog.EndpointBaseURLTruncated,
		nullableStringArg(stringPtr(auditLog.RequestHeaders)),
		auditLog.RequestHeadersScrubProvenance,
		runtimeAuditHeadersCaptureStatus(auditLog.RequestHeaders),
		"none",
		nullableStringArg(auditLog.ResponseHeaders),
		auditLog.ResponseHeadersScrubProvenance,
		runtimeAuditHeadersCaptureStatusOptional(auditLog.ResponseHeaders),
		"none",
		nullableStringArg(auditLog.RequestBody),
		nullableStringArg(auditLog.RequestBodyEncoding),
		auditLog.RequestBodyCaptureProvenance,
		nullableStringArg(auditLog.RequestBodyCaptureEndState),
		auditLog.RequestBodyCaptureStatus,
		auditLog.RequestBodyCaptureLimitReason,
		auditLog.RequestBodyTruncated,
		nullableInt64Arg(auditLog.RequestBodyBytesObserved),
		nullableInt64Arg(auditLog.RequestBodyBytesStored),
		nullableStringArg(auditLog.ResponseBody),
		nullableStringArg(auditLog.ResponseBodyEncoding),
		auditLog.ResponseBodyCaptureProvenance,
		nullableStringArg(auditLog.ResponseBodyCaptureEndState),
		auditLog.ResponseBodyCaptureStatus,
		auditLog.ResponseBodyCaptureLimitReason,
		auditLog.ResponseBodyTruncated,
		nullableInt64Arg(auditLog.ResponseBodyBytesObserved),
		nullableInt64Arg(auditLog.ResponseBodyBytesStored),
		auditLog.RowKind,
		nullableIntArg(auditLog.AttemptNumber),
		nullableIntArg(auditLog.AttemptDurationMS),
		nullableIntArg(auditLog.UpstreamStatusCode),
		auditLog.URLScrubProvenance,
		auditLog.IsStream,
		auditLog.CreatedAt.UTC(),
		auditLog.AuditEnabledAtRequest,
		auditLog.AuditCaptureBodiesAtRequest,
	); err != nil {
		return fmt.Errorf("insert audit log for request log %d: %w (row_kind=%s status=%s bytes_observed=%d bytes_stored=%d truncated=%v body_nil=%v)", requestLogID, err, auditLog.RowKind, auditLog.ResponseBodyCaptureStatus, derefInt64(auditLog.ResponseBodyBytesObserved), derefInt64(auditLog.ResponseBodyBytesStored), auditLog.ResponseBodyTruncated, auditLog.ResponseBody == nil)
	}
	return nil
}

func runtimeAuditHeadersCaptureStatus(serialized string) string {
	if strings.TrimSpace(serialized) == "" || serialized == "{}" {
		return "not_requested"
	}
	return "captured"
}

func runtimeAuditHeadersCaptureStatusOptional(serialized *string) string {
	if serialized == nil {
		return "not_requested"
	}
	return runtimeAuditHeadersCaptureStatus(*serialized)
}

func extractResponseUsage(body []byte, rule runtimeUsageNormalizationRule) responseUsage {
	if len(body) == 0 || !rule.configured() {
		return responseUsage{}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return responseUsage{}
	}
	return extractResponseUsageFromPayload(payload, rule)
}

func extractResponseUsageFromPayload(payload map[string]any, rule runtimeUsageNormalizationRule) responseUsage {
	usage := responseUsage{}
	for _, carrier := range []runtimeUsageCarrier{runtimeUsageCarrierRootUsage, runtimeUsageCarrierResponseUsage, runtimeUsageCarrierMessageUsage, runtimeUsageCarrierRootUsageMetadata} {
		usagePayload, ok := runtimeUsagePayloadFromCarrier(payload, carrier)
		if !ok {
			continue
		}
		usage.mergeRuntimeUsagePayload(rule, carrier, usagePayload)
	}
	return usage.canonicalizedForRuntimeUsage(rule)
}

func runtimeUsagePayloadFromCarrier(payload map[string]any, carrier runtimeUsageCarrier) (map[string]any, bool) {
	switch carrier {
	case runtimeUsageCarrierRootUsage:
		usagePayload, ok := payload["usage"].(map[string]any)
		return usagePayload, ok
	case runtimeUsageCarrierResponseUsage:
		responsePayload, ok := payload["response"].(map[string]any)
		if !ok {
			return nil, false
		}
		usagePayload, ok := responsePayload["usage"].(map[string]any)
		return usagePayload, ok
	case runtimeUsageCarrierMessageUsage:
		messagePayload, ok := payload["message"].(map[string]any)
		if !ok {
			return nil, false
		}
		usagePayload, ok := messagePayload["usage"].(map[string]any)
		return usagePayload, ok
	case runtimeUsageCarrierRootUsageMetadata:
		usagePayload, ok := payload["usageMetadata"].(map[string]any)
		return usagePayload, ok
	default:
		return nil, false
	}
}

// billingStateUnpricedOnly preserves the legacy unpriced-reason derivation
// for non-2xx diagnostic rows without resurrecting billable/priced flags:
// the new writer never persists billable_flag/priced_flag (Requests SPEC §3.7).
func billingStateUnpricedOnly(success bool) *string {
	if !success {
		return nil
	}
	return stringPtr("missing_pricing_template")
}

func requestWantsStream(rawBody []byte, requestPath string) bool {
	if strings.Contains(strings.TrimSpace(requestPath), ":streamGenerateContent") {
		return true
	}
	return requestBodyWantsStream(rawBody)
}

func requestBodyWantsStream(rawBody []byte) bool {
	if len(rawBody) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return false
	}
	stream, ok := payload["stream"].(bool)
	return ok && stream
}

func durationMilliseconds(duration time.Duration) int {
	milliseconds := int(duration / time.Millisecond)
	if milliseconds < 1 {
		return 1
	}
	return milliseconds
}

func runtimeProxyKeyUsageSignalFromSnapshot(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *runtimeProxyKeyUsageSignal {
	if proxyKey == nil || proxyKey.ID <= 0 || proxyKey.LastUsedAt.IsZero() {
		return nil
	}
	return &runtimeProxyKeyUsageSignal{
		KeyID:      proxyKey.ID,
		LastUsedAt: proxyKey.LastUsedAt.UTC(),
		LastUsedIP: strings.TrimSpace(proxyKey.LastUsedIP),
	}
}

func runtimeProxyKeyAuthEnforcedFromContext(ctx context.Context) *bool {
	attribution, ok := requestcontext.RuntimeProxyKeyAttributionFromContext(ctx)
	if !ok {
		return nil
	}
	value := attribution.AuthEnforced
	return &value
}

func recordRuntimeProxyKeyUsageTx(ctx context.Context, tx pgx.Tx, signal *runtimeProxyKeyUsageSignal) error {
	if signal == nil {
		return nil
	}
	if err := proxykeyusage.RecordTx(ctx, tx, signal.KeyID, signal.LastUsedAt, signal.LastUsedIP); err != nil {
		return fmt.Errorf("record runtime proxy api key usage: %w", err)
	}
	return nil
}

func headerValuePointer(header http.Header, keys ...string) *string {
	for _, key := range keys {
		value := strings.TrimSpace(header.Get(key))
		if value != "" {
			return &value
		}
	}
	return nil
}

func headerMapValuePointer(header map[string]string, key string) *string {
	for headerKey, value := range header {
		if strings.EqualFold(headerKey, key) {
			return trimmedStringPointer(value)
		}
	}
	return nil
}

func proxyKeyIDPointer(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *int {
	if proxyKey == nil {
		return nil
	}
	return &proxyKey.ID
}

// runtimeProxyKeyAttributionState reports the immutable key attribution
// state: identified when a key was used, none when the request had no key,
// unknown for telemetry evidence gaps (Observe SPEC §3.5).
func runtimeProxyKeyAttributionState(proxyKey *requestcontext.RuntimeProxyKeySnapshot) string {
	if proxyKey == nil {
		return "none"
	}
	return "identified"
}

func proxyKeyNamePointer(proxyKey *requestcontext.RuntimeProxyKeySnapshot) *string {
	if proxyKey == nil {
		return nil
	}
	return &proxyKey.Name
}

func firstValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func nestedValue(values map[string]any, key string, nestedKey string) any {
	nested, ok := values[key].(map[string]any)
	if !ok {
		return nil
	}
	return nested[nestedKey]
}

func intPointerFromAny(value any) *int {
	switch typed := value.(type) {
	case float64:
		resolved := int(typed)
		return &resolved
	case json.Number:
		value, err := typed.Int64()
		if err != nil {
			return nil
		}
		resolved := int(value)
		return &resolved
	case int:
		resolved := typed
		return &resolved
	case int64:
		resolved := int(typed)
		return &resolved
	default:
		return nil
	}
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func trimmedStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// optionalTrimmedStringPointer returns a pointer to the trimmed value when
// non-empty (used for typed enums that may legitimately be empty, e.g. legacy
// rows with no trigger evidence).
func optionalTrimmedStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// nonNegativeIntPointer returns a pointer to value when value > 0.
func nonNegativeIntPointer(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// nullableAttemptNumberArg writes the attempt number only for real upstream
// rows; planning/admission diagnostic rows must keep NULL attempt fields
// (Requests SPEC §3.4: diagnostics never masquerade as attempt 1).
func nullableAttemptNumberArg(rowKind string, attemptNumber int) any {
	if rowKind != requestLogRowKindUpstream {
		return nil
	}
	return attemptNumber
}

// nullableStringSliceArg passes a string slice as a text[] column value. For
// columns that are NOT NULL DEFAULT '{}' (metadata provenance arrays) the
// empty slice maps to an empty array literal; for nullable columns (e.g.
// missing_price_components) an empty slice maps to SQL NULL so the pricing
// CHECKs that require NULL stay satisfiable.
func nullableStringSliceArg(value []string) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

// notNullStringArrayArg maps an empty slice to an empty array literal for
// NOT NULL text[] columns.
func notNullStringArrayArg(value []string) any {
	if len(value) == 0 {
		return []string{}
	}
	return value
}

func nullableIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Arg(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullableBoolArg(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableJSONArg(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(raw)
}

func intPtr(value int) *int {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
