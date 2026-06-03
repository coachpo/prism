package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"

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
	usage := responseUsage{}
	if inputTokens := intPointerFromAny(usagePayload["input_tokens"]); inputTokens != nil {
		usage.InputTokens = inputTokens
	}
	if outputTokens := intPointerFromAny(usagePayload["output_tokens"]); outputTokens != nil {
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
	inputTokens := intPointerFromAny(usagePayload["promptTokenCount"])
	cacheReadTokens := intPointerFromAny(usagePayload["cachedContentTokenCount"])
	outputTokens := intPointerFromAny(usagePayload["candidatesTokenCount"])
	reasoningTokens := intPointerFromAny(usagePayload["thoughtsTokenCount"])
	totalTokens := intPointerFromAny(usagePayload["totalTokenCount"])
	usage := usageFromParentTotals(inputTokens, cacheReadTokens, outputTokens, reasoningTokens, totalTokens)
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
	VendorID                          *int
	VendorKey                         *string
	VendorName                        *string
	EndpointID                        *int
	ConnectionID                      *int
	SelectedTerminalTargetID          *int
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
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
	BillableFlag                      *bool
	PricedFlag                        *bool
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
	ContextRouting                    *runtimeContextRoutingDecision
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
	ConnectionID                      *int
	SelectedTerminalTargetID          *int
	ProxyAPIKeyID                     *int
	ProxyAPIKeyNameSnapshot           *string
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
	ContextRouting                    *runtimeContextRoutingDecision
}

func (requestLog *requestLogInsert) applyRuntimePricingResult(pricingResult runtimePricingResult) {
	requestLog.BillableFlag = boolPtr(pricingResult.Billable)
	requestLog.PricedFlag = boolPtr(pricingResult.Priced)
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
}

func (usageEvent *usageEventInsert) applyRuntimePricingResult(pricingResult runtimePricingResult) {
	usageEvent.BillableFlag = boolPtr(pricingResult.Billable)
	usageEvent.PricedFlag = boolPtr(pricingResult.Priced)
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
	VendorID                    *int      `json:"vendor_id,omitempty"`
	ModelID                     string    `json:"model_id"`
	EndpointID                  int       `json:"endpoint_id"`
	ConnectionID                int       `json:"connection_id"`
	EndpointBaseURL             string    `json:"endpoint_base_url"`
	EndpointDescription         *string   `json:"endpoint_description,omitempty"`
	RequestMethod               string    `json:"request_method"`
	RequestURL                  string    `json:"request_url"`
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
}

type runtimeProxyKeyUsageSignal struct {
	KeyID      int       `json:"key_id"`
	LastUsedAt time.Time `json:"last_used_at"`
	LastUsedIP string    `json:"last_used_ip,omitempty"`
}

type runtimeTelemetryEnvelope struct {
	RequestLogs   []requestLogInsert          `json:"request_logs"`
	AuditLogs     []auditLogInsert            `json:"audit_logs,omitempty"`
	UsageEvent    usageEventInsert            `json:"usage_event"`
	ProxyKeyUsage *runtimeProxyKeyUsageSignal `json:"proxy_key_usage,omitempty"`
	TraceContext  runtimeTraceContext         `json:"trace_context,omitempty"`
}

const (
	runtimeMetricScopeName = "github.com/coachpo/prism/backend/internal/httpapi/runtime"

	runtimeMetricOperationUnknown = "unknown"

	runtimeMetricAttrOperationName  = "operation_name"
	runtimeMetricAttrStatusClass    = "status_class"
	runtimeMetricAttrStreamOutcome  = "stream_outcome"
	runtimeMetricAttrFailoverReason = "failover_reason"
	runtimeMetricAttrFeedbackKind   = "feedback_kind"
	runtimeMetricAttrEnqueueStatus  = "enqueue_status"

	runtimeOutboxEnqueueAccepted     = "accepted"
	runtimeOutboxEnqueueFailed       = "failed"
	runtimeOutboxEnqueueNotSubmitted = "not_submitted"
)

type runtimeMetrics struct {
	requestCount         otelmetric.Int64Counter
	requestLatency       otelmetric.Float64Histogram
	requestAttemptCount  otelmetric.Int64Histogram
	statusClassCount     otelmetric.Int64Counter
	streamOutcomeCount   otelmetric.Int64Counter
	failoverCount        otelmetric.Int64Counter
	hedgeCount           otelmetric.Int64Counter
	ttft                 otelmetric.Float64Histogram
	completionDuration   otelmetric.Float64Histogram
	feedbackEnqueueCount otelmetric.Int64Counter
	outboxEnqueueCount   otelmetric.Int64Counter
}

type runtimeMetricAttributePolicy struct{}

var runtimeMetricPolicy runtimeMetricAttributePolicy

func newRuntimeMetrics() *runtimeMetrics {
	meter := otel.Meter(runtimeMetricScopeName)
	metrics := &runtimeMetrics{}
	metrics.requestCount, _ = meter.Int64Counter("prism.runtime.request.count", otelmetric.WithDescription("Runtime proxy requests completed."))
	metrics.requestLatency, _ = meter.Float64Histogram("prism.runtime.request.latency", otelmetric.WithDescription("Runtime proxy request latency."), otelmetric.WithUnit("ms"))
	metrics.requestAttemptCount, _ = meter.Int64Histogram("prism.runtime.request.attempt_count", otelmetric.WithDescription("Runtime upstream attempts per completed request."))
	metrics.statusClassCount, _ = meter.Int64Counter("prism.runtime.status_class.count", otelmetric.WithDescription("Runtime proxy responses by bounded status class."))
	metrics.streamOutcomeCount, _ = meter.Int64Counter("prism.runtime.stream.outcome.count", otelmetric.WithDescription("Runtime stream outcomes by bounded classification."))
	metrics.failoverCount, _ = meter.Int64Counter("prism.runtime.failover.count", otelmetric.WithDescription("Runtime failover events by bounded reason."))
	metrics.hedgeCount, _ = meter.Int64Counter("prism.runtime.hedge.count", otelmetric.WithDescription("Runtime hedge attempts launched."))
	metrics.ttft, _ = meter.Float64Histogram("prism.runtime.request.ttft", otelmetric.WithDescription("Runtime streaming time to first meaningful token."), otelmetric.WithUnit("ms"))
	metrics.completionDuration, _ = meter.Float64Histogram("prism.runtime.request.completion_duration", otelmetric.WithDescription("Runtime request completion duration."), otelmetric.WithUnit("ms"))
	metrics.feedbackEnqueueCount, _ = meter.Int64Counter("prism.runtime.feedback.enqueue.count", otelmetric.WithDescription("Runtime feedback enqueue results."))
	metrics.outboxEnqueueCount, _ = meter.Int64Counter("prism.runtime.outbox.enqueue.count", otelmetric.WithDescription("Runtime telemetry outbox enqueue results."))
	return metrics
}

func (metrics *runtimeMetrics) recordRequest(ctx context.Context, event usageEventInsert) {
	if metrics == nil {
		return
	}
	attrs := runtimeMetricPolicy.requestAttributes(event.OperationName, event.StatusCode, event.StreamOutcome)
	if metrics.requestCount != nil {
		metrics.requestCount.Add(ctx, 1, otelmetric.WithAttributes(attrs...))
	}
	if metrics.statusClassCount != nil {
		metrics.statusClassCount.Add(ctx, 1, otelmetric.WithAttributes(runtimeMetricPolicy.statusAttributes(event.OperationName, event.StatusCode)...))
	}
	if metrics.streamOutcomeCount != nil {
		metrics.streamOutcomeCount.Add(ctx, 1, otelmetric.WithAttributes(runtimeMetricPolicy.streamAttributes(event.OperationName, event.StreamOutcome)...))
	}
	if metrics.requestLatency != nil && event.ResponseTimeMS != nil {
		metrics.requestLatency.Record(ctx, float64(*event.ResponseTimeMS), otelmetric.WithAttributes(attrs...))
	}
	if metrics.requestAttemptCount != nil && event.AttemptCount > 0 {
		metrics.requestAttemptCount.Record(ctx, int64(event.AttemptCount), otelmetric.WithAttributes(attrs...))
	}
	if metrics.ttft != nil && event.TTFTMS != nil {
		metrics.ttft.Record(ctx, float64(*event.TTFTMS), otelmetric.WithAttributes(runtimeMetricPolicy.streamAttributes(event.OperationName, event.StreamOutcome)...))
	}
	if metrics.completionDuration != nil && event.CompletionDurationMS != nil {
		metrics.completionDuration.Record(ctx, float64(*event.CompletionDurationMS), otelmetric.WithAttributes(attrs...))
	}
}

func (metrics *runtimeMetrics) recordFailover(ctx context.Context, operationName string, reason string) {
	if metrics == nil || metrics.failoverCount == nil {
		return
	}
	metrics.failoverCount.Add(ctx, 1, otelmetric.WithAttributes(runtimeMetricPolicy.failoverAttributes(operationName, reason)...))
}

func (metrics *runtimeMetrics) recordHedge(ctx context.Context, operationName string, count int64) {
	if metrics == nil || metrics.hedgeCount == nil || count <= 0 {
		return
	}
	metrics.hedgeCount.Add(ctx, count, otelmetric.WithAttributes(runtimeMetricPolicy.operationAttributes(operationName)...))
}

func (metrics *runtimeMetrics) recordFeedbackEnqueue(ctx context.Context, operationName string, kind runtimeFeedbackKind, status RuntimeFeedbackEnqueueStatus) {
	if metrics == nil || metrics.feedbackEnqueueCount == nil {
		return
	}
	metrics.feedbackEnqueueCount.Add(ctx, 1, otelmetric.WithAttributes(runtimeMetricPolicy.feedbackAttributes(operationName, kind, status)...))
}

func (metrics *runtimeMetrics) recordOutboxEnqueue(ctx context.Context, operationName string, status string) {
	if metrics == nil || metrics.outboxEnqueueCount == nil {
		return
	}
	metrics.outboxEnqueueCount.Add(ctx, 1, otelmetric.WithAttributes(runtimeMetricPolicy.outboxAttributes(operationName, status)...))
}

func (policy runtimeMetricAttributePolicy) requestAttributes(operationName string, statusCode int, streamOutcome string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeMetricAttrOperationName, policy.operationName(operationName)),
		attribute.String(runtimeMetricAttrStatusClass, policy.statusClass(statusCode)),
		attribute.String(runtimeMetricAttrStreamOutcome, policy.streamOutcome(streamOutcome)),
	}
}

func (policy runtimeMetricAttributePolicy) statusAttributes(operationName string, statusCode int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeMetricAttrOperationName, policy.operationName(operationName)),
		attribute.String(runtimeMetricAttrStatusClass, policy.statusClass(statusCode)),
	}
}

func (policy runtimeMetricAttributePolicy) streamAttributes(operationName string, streamOutcome string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeMetricAttrOperationName, policy.operationName(operationName)),
		attribute.String(runtimeMetricAttrStreamOutcome, policy.streamOutcome(streamOutcome)),
	}
}

func (policy runtimeMetricAttributePolicy) failoverAttributes(operationName string, reason string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeMetricAttrOperationName, policy.operationName(operationName)),
		attribute.String(runtimeMetricAttrFailoverReason, policy.failoverReason(reason)),
	}
}

func (policy runtimeMetricAttributePolicy) feedbackAttributes(operationName string, kind runtimeFeedbackKind, status RuntimeFeedbackEnqueueStatus) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeMetricAttrOperationName, policy.operationName(operationName)),
		attribute.String(runtimeMetricAttrFeedbackKind, policy.feedbackKind(kind)),
		attribute.String(runtimeMetricAttrEnqueueStatus, policy.feedbackEnqueueStatus(status)),
	}
}

func (policy runtimeMetricAttributePolicy) outboxAttributes(operationName string, status string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(runtimeMetricAttrOperationName, policy.operationName(operationName)),
		attribute.String(runtimeMetricAttrEnqueueStatus, policy.outboxEnqueueStatus(status)),
	}
}

func (policy runtimeMetricAttributePolicy) operationAttributes(operationName string) []attribute.KeyValue {
	return []attribute.KeyValue{attribute.String(runtimeMetricAttrOperationName, policy.operationName(operationName))}
}

func (policy runtimeMetricAttributePolicy) operationName(value string) string {
	trimmed := strings.TrimSpace(value)
	for _, operation := range runtimeOperationCatalog {
		if trimmed == operation.Name {
			return operation.Name
		}
	}
	return runtimeMetricOperationUnknown
}

func (policy runtimeMetricAttributePolicy) statusClass(statusCode int) string {
	if statusCode >= 100 && statusCode <= 599 {
		return fmt.Sprintf("%dxx", statusCode/100)
	}
	return runtimeMetricOperationUnknown
}

func (policy runtimeMetricAttributePolicy) streamOutcome(outcome string) string {
	return runtimeStreamOutcomeForTelemetry(outcome)
}

func (policy runtimeMetricAttributePolicy) failoverReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "http", "transport":
		return strings.TrimSpace(reason)
	default:
		return runtimeMetricOperationUnknown
	}
}

func (policy runtimeMetricAttributePolicy) feedbackKind(kind runtimeFeedbackKind) string {
	switch kind {
	case runtimeFeedbackAdmissionRejected, runtimeFeedbackUnbanned, runtimeFeedbackSuccessRecovery, runtimeFeedbackFailoverHTTP, runtimeFeedbackTransportFailure:
		return string(kind)
	default:
		return runtimeMetricOperationUnknown
	}
}

func (policy runtimeMetricAttributePolicy) feedbackEnqueueStatus(status RuntimeFeedbackEnqueueStatus) string {
	switch status {
	case RuntimeFeedbackAccepted, RuntimeFeedbackDroppedInvalid, RuntimeFeedbackDroppedUnavailable, RuntimeFeedbackDroppedBackpressure:
		return string(status)
	default:
		return runtimeMetricOperationUnknown
	}
}

func (policy runtimeMetricAttributePolicy) outboxEnqueueStatus(status string) string {
	switch strings.TrimSpace(status) {
	case runtimeOutboxEnqueueAccepted, runtimeOutboxEnqueueFailed, runtimeOutboxEnqueueNotSubmitted:
		return strings.TrimSpace(status)
	default:
		return runtimeMetricOperationUnknown
	}
}

func (s *Service) recordRuntimeActivity(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	ctx := runtimeMetricContext(request)
	ctx, span := startRuntimeSpan(ctx, "runtime.activity.record", runtimeTracePlanAttributes(plan)...)
	defer span.End()
	envelope := s.buildRuntimeTelemetryEnvelope(plan, result, request, startedAt, responseCapture)
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	s.recordRuntimeMetricsForEnvelope(ctx, envelope)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		runtimeTraceMarkError(span, "runtime_activity_submit_rejected")
		s.recordRuntimeOutboxEnqueue(ctx, envelope.UsageEvent.OperationName, runtimeOutboxEnqueueNotSubmitted)
		slog.Error("failed to accept runtime activity telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func (s *Service) recordRuntimePlanningFailure(request *http.Request, startedAt time.Time, err error) {
	if s == nil || s.runtimeSideEffects == nil {
		return
	}
	var runtimeErr *domainError
	if !errors.As(err, &runtimeErr) || runtimeErr == nil || runtimeErr.PlanningFailure == nil {
		return
	}
	ctx := runtimeMetricContext(request)
	ctx, span := startRuntimeSpan(ctx, "runtime.activity.record_planning_failure", runtimeTracePlanningFailureAttributes(*runtimeErr.PlanningFailure)...)
	defer span.End()
	envelope := s.buildRuntimePlanningFailureTelemetryEnvelope(*runtimeErr.PlanningFailure, request, startedAt, runtimeErr)
	envelope.TraceContext = runtimeTraceContextFromContext(ctx)
	s.recordRuntimeMetricsForEnvelope(ctx, envelope)
	intent := RuntimeActivityIntent{Envelope: envelope, TraceContext: envelope.TraceContext}
	if submit := s.runtimeSideEffects.SubmitRuntimeActivityContext(ctx, intent); submit.Status != RuntimeSideEffectAccepted {
		runtimeTraceMarkError(span, "runtime_activity_submit_rejected")
		s.recordRuntimeOutboxEnqueue(ctx, envelope.UsageEvent.OperationName, runtimeOutboxEnqueueNotSubmitted)
		slog.Error("failed to accept runtime planning-failure telemetry intent", "reason", submit.Reason, "profile_id", envelope.UsageEvent.ProfileID, "ingress_request_id", envelope.UsageEvent.IngressRequestID)
	}
}

func runtimeMetricContext(request *http.Request) context.Context {
	if request != nil && request.Context() != nil {
		return request.Context()
	}
	return context.Background()
}

func (s *Service) recordRuntimeMetricsForEnvelope(ctx context.Context, envelope runtimeTelemetryEnvelope) {
	if s == nil || s.runtimeMetrics == nil {
		return
	}
	s.runtimeMetrics.recordRequest(ctx, envelope.UsageEvent)
}

func (s *Service) recordRuntimeFeedbackEnqueue(ctx context.Context, operationName string, kind runtimeFeedbackKind, status RuntimeFeedbackEnqueueStatus) {
	if s == nil || s.runtimeMetrics == nil {
		return
	}
	s.runtimeMetrics.recordFeedbackEnqueue(ctx, operationName, kind, status)
}

func (s *Service) recordRuntimeOutboxEnqueue(ctx context.Context, operationName string, status string) {
	if s == nil || s.runtimeMetrics == nil {
		return
	}
	s.runtimeMetrics.recordOutboxEnqueue(ctx, operationName, status)
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
	streamErrorKind      *string
	streamErrorDetail    *string
}

type runtimeTelemetryEnvelopeContext struct {
	runtimeTelemetryPricingTimingContext
	ingressRequestID          string
	proxyKey                  *requestcontext.RuntimeProxyKeySnapshot
	callerUserAgent           *string
	requestGenerationSnapshot requestGenerationParamsSnapshot
	attempts                  []executionAttempt
	capturedRequestBody       *string
	capturedResponseBody      *string
}

type runtimeTelemetryAttemptContext struct {
	attempt        executionAttempt
	attemptNumber  int
	isFinal        bool
	success        bool
	billableFlag   *bool
	pricedFlag     *bool
	unpricedReason *string
	createdAt      time.Time
	responseTimeMS int
}

func (s *Service) buildRuntimeTelemetryEnvelope(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelope {
	telemetry := s.buildRuntimeTelemetryEnvelopeContext(plan, result, request, startedAt, responseCapture)
	requestLogs := buildRuntimeRequestLogRows(plan, request, telemetry)
	auditLogs := buildRuntimeAuditLogRows(plan, request, telemetry)
	usageEvent := buildRuntimeUsageEvent(plan, result, request, telemetry, len(requestLogs))
	return runtimeTelemetryEnvelope{
		RequestLogs:   requestLogs,
		AuditLogs:     auditLogs,
		UsageEvent:    usageEvent,
		ProxyKeyUsage: runtimeProxyKeyUsageSignalFromSnapshot(telemetry.proxyKey),
	}
}

func (s *Service) buildRuntimePlanningFailureTelemetryEnvelope(failure runtimePlanningFailureTelemetry, request *http.Request, startedAt time.Time, runtimeErr *domainError) runtimeTelemetryEnvelope {
	requestCompletedAt := s.nowUTC()
	responseTimeMS := durationMilliseconds(requestCompletedAt.Sub(startedAt))
	billableFlag, pricedFlag, unpricedReason := billingState(false)
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	ingressRequestID := strings.TrimSpace(middleware.GetReqID(request.Context()))
	if ingressRequestID == "" {
		ingressRequestID = fmt.Sprintf("runtime-%d", requestCompletedAt.UnixNano())
	}
	reportCurrencyCode := runtimeOptionalTrimmedString(failure.ReportCurrencySnapshot.Code)
	reportCurrencySymbol := runtimeOptionalTrimmedString(failure.ReportCurrencySnapshot.Symbol)
	requestGenerationSnapshot := failure.RequestGenerationParams.clone()
	selectedTerminalTargetID := cloneRuntimeIntPointer(failure.SelectedTerminalTargetID)
	if selectedTerminalTargetID == nil && failure.ContextRouting != nil {
		selectedTerminalTargetID = cloneRuntimeIntPointer(failure.ContextRouting.SelectedTerminalTargetID)
	}
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
		VendorID:                      failure.RequestedVendorID,
		VendorKey:                     failure.RequestedVendorKey,
		VendorName:                    failure.RequestedVendorName,
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
		BillableFlag:                  billableFlag,
		PricedFlag:                    pricedFlag,
		UnpricedReason:                unpricedReason,
		ReportCurrencyCode:            reportCurrencyCode,
		ReportCurrencySymbol:          reportCurrencySymbol,
		RequestPath:                   failure.RequestPath,
		UpstreamRequestPath:           cloneRuntimeStringPointer(failure.UpstreamRequestPath),
		ErrorDetail:                   nil,
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
		ContextRouting:                cloneRuntimeContextRoutingDecision(failure.ContextRouting),
	}
	usageEvent := usageEventInsert{
		ProfileID:                failure.ProfileID,
		IngressRequestID:         ingressRequestID,
		ModelID:                  failure.RequestedModelID,
		ResolvedTargetModelID:    resolvedTargetModelID,
		APIFamily:                failure.APIFamily,
		OperationName:            strings.TrimSpace(failure.RuntimeOperation.Name),
		UpstreamOperationName:    cloneRuntimeStringPointer(failure.UpstreamOperationName),
		OperationTranslationMode: cloneRuntimeStringPointer(failure.OperationTranslationMode),
		EndpointID:               nil,
		ConnectionID:             nil,
		SelectedTerminalTargetID: selectedTerminalTargetID,
		ProxyAPIKeyID:            proxyKeyIDPointer(proxyKey),
		ProxyAPIKeyNameSnapshot:  proxyKeyNamePointer(proxyKey),
		StatusCode:               runtimeErr.StatusCode,
		SuccessFlag:              false,
		BillableFlag:             billableFlag,
		PricedFlag:               pricedFlag,
		UnpricedReason:           unpricedReason,
		ReportCurrencyCode:       reportCurrencyCode,
		ReportCurrencySymbol:     reportCurrencySymbol,
		AttemptCount:             1,
		RequestPath:              failure.RequestPath,
		UpstreamRequestPath:      cloneRuntimeStringPointer(failure.UpstreamRequestPath),
		CreatedAt:                requestCompletedAt,
		ResponseTimeMS:           intPtr(responseTimeMS),
		CompletionDurationMS:     completionDurationMS,
		TTFTMS:                   nil,
		StreamOutcome:            runtimeStreamOutcomeNotStreaming,
		StreamErrorKind:          nil,
		ContextRouting:           cloneRuntimeContextRoutingDecision(failure.ContextRouting),
	}
	return runtimeTelemetryEnvelope{
		RequestLogs:   []requestLogInsert{requestLog},
		UsageEvent:    usageEvent,
		ProxyKeyUsage: runtimeProxyKeyUsageSignalFromSnapshot(proxyKey),
	}
}

func (s *Service) buildRuntimeTelemetryEnvelopeContext(plan requestPlan, result executionResult, request *http.Request, startedAt time.Time, responseCapture runtimeResponseCapture) runtimeTelemetryEnvelopeContext {
	pricingTiming := s.buildRuntimeTelemetryPricingTimingContext(plan, result, startedAt, responseCapture)
	ingressRequestID := strings.TrimSpace(middleware.GetReqID(request.Context()))
	if ingressRequestID == "" {
		ingressRequestID = fmt.Sprintf("runtime-%d", pricingTiming.requestCompletedAt.UnixNano())
	}
	proxyKey, _ := requestcontext.RuntimeProxyKeyFromContext(request.Context())
	return runtimeTelemetryEnvelopeContext{
		runtimeTelemetryPricingTimingContext: pricingTiming,
		ingressRequestID:                     ingressRequestID,
		proxyKey:                             proxyKey,
		callerUserAgent:                      trimmedStringPointer(request.UserAgent()),
		requestGenerationSnapshot:            plan.RequestGenerationParamsSnapshot(),
		attempts:                             runtimeTelemetryAttempts(plan, result, request, pricingTiming),
		capturedResponseBody:                 runtimeCapturedAuditBody(result.AuditEnabledAtRequest && result.AuditCaptureBodiesAtRequest, responseCapture.AuditBody),
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
		streamErrorKind:      responseCapture.StreamErrorKind,
		streamErrorDetail:    responseCapture.StreamErrorDetail,
	}
}

func runtimeTelemetryAttempts(plan requestPlan, result executionResult, request *http.Request, pricingTiming runtimeTelemetryPricingTimingContext) []executionAttempt {
	if len(result.Attempts) > 0 {
		return result.Attempts
	}
	selectedAttempt := firstTerminalAttempt(plan)
	return []executionAttempt{{
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
	}}
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
	attemptSuccess := attempt.StatusCode >= 200 && attempt.StatusCode <= 299
	attemptBillableFlag, attemptPricedFlag, attemptUnpricedReason := billingState(attemptSuccess)
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
		attemptNumber:  index + 1,
		isFinal:        isFinal,
		success:        attemptSuccess,
		billableFlag:   attemptBillableFlag,
		pricedFlag:     attemptPricedFlag,
		unpricedReason: attemptUnpricedReason,
		createdAt:      attemptCreatedAt.UTC(),
		responseTimeMS: attemptResponseTimeMS,
	}
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
	requestLog := requestLogInsert{
		ProfileID:                     plan.ProfileID,
		ModelID:                       plan.RequestedModelID,
		ResolvedTargetModelID:         resolvedTargetModelIDForAttempt(plan, attempt.attempt),
		APIFamily:                     plan.APIFamily,
		OperationName:                 telemetry.operationName,
		UpstreamOperationName:         trimmedStringPointer(attempt.attempt.UpstreamOperationName),
		OperationTranslationMode:      runtimeTranslationModePointer(attempt.attempt.OperationTranslationMode),
		VendorID:                      plan.RequestedVendorID,
		VendorKey:                     plan.RequestedVendorKey,
		VendorName:                    plan.RequestedVendorName,
		EndpointID:                    intPtr(attempt.attempt.Connection.Endpoint.ID),
		ConnectionID:                  intPtr(attempt.attempt.Connection.ID),
		SelectedTerminalTargetID:      plan.selectedTerminalTargetID(),
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
		BillableFlag:                  attempt.billableFlag,
		PricedFlag:                    attempt.pricedFlag,
		UnpricedReason:                attempt.unpricedReason,
		ReportCurrencyCode:            telemetry.reportCurrencyCode,
		ReportCurrencySymbol:          telemetry.reportCurrencySymbol,
		RequestPath:                   request.URL.Path,
		UpstreamRequestPath:           trimmedStringPointer(attempt.attempt.UpstreamRequestPath),
		ErrorDetail:                   nil,
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
		ContextRouting:                cloneRuntimeContextRoutingDecision(plan.ContextRouting),
	}
	if attempt.isFinal {
		applyRuntimeFinalAttemptTelemetry(&requestLog, telemetry, attempt)
	}
	return requestLog
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

func buildRuntimeAuditLogRows(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext) []auditLogInsert {
	auditLogs := make([]auditLogInsert, 0, len(telemetry.attempts))
	for index := range telemetry.attempts {
		attempt := telemetry.attemptContext(index)
		if !attempt.attempt.AuditEnabledAtRequest {
			continue
		}
		auditLogs = append(auditLogs, buildRuntimeAuditLogRow(plan, request, telemetry, attempt))
	}
	return auditLogs
}

func buildRuntimeAuditLogRow(plan requestPlan, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, attempt runtimeTelemetryAttemptContext) auditLogInsert {
	auditLog := auditLogInsert{
		RequestLogAttemptNumber:     attempt.attemptNumber,
		ProfileID:                   plan.ProfileID,
		VendorID:                    plan.RequestedVendorID,
		ModelID:                     plan.RequestedModelID,
		EndpointID:                  attempt.attempt.Connection.Endpoint.ID,
		ConnectionID:                attempt.attempt.Connection.ID,
		EndpointBaseURL:             attempt.attempt.Connection.Endpoint.BaseURL,
		EndpointDescription:         attempt.attempt.Connection.Endpoint.Name,
		RequestMethod:               request.Method,
		RequestURL:                  runtimeAuditRequestURL(attempt.attempt.RequestURL, request),
		RequestHeaders:              marshalAuditHeaders(attempt.attempt.RequestHeaders),
		RequestBody:                 runtimeCapturedAuditBody(attempt.attempt.AuditCaptureBodiesAtRequest, attempt.attempt.RequestBody),
		RequestBodyStored:           attempt.attempt.AuditCaptureBodiesAtRequest && len(attempt.attempt.RequestBody) > 0,
		ResponseStatus:              attempt.attempt.StatusCode,
		ResponseHeaders:             marshalAuditHTTPHeaders(attempt.attempt.ResponseHeaders),
		ResponseBody:                nil,
		ResponseBodyStored:          false,
		IsStream:                    telemetry.isStream,
		DurationMS:                  attempt.responseTimeMS,
		CreatedAt:                   attempt.createdAt,
		AuditEnabledAtRequest:       attempt.attempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: attempt.attempt.AuditCaptureBodiesAtRequest,
	}
	if attempt.isFinal && attempt.attempt.AuditCaptureBodiesAtRequest {
		auditLog.ResponseBody = telemetry.capturedResponseBody
		auditLog.ResponseBodyStored = telemetry.capturedResponseBody != nil
	}
	return auditLog
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

func buildRuntimeUsageEvent(plan requestPlan, result executionResult, request *http.Request, telemetry runtimeTelemetryEnvelopeContext, requestLogCount int) usageEventInsert {
	attemptCount := requestLogCount
	if attemptCount < 1 {
		attemptCount = 1
	}
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
		ConnectionID:             intPtr(result.Connection.ID),
		SelectedTerminalTargetID: plan.selectedTerminalTargetID(),
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
		ContextRouting:           cloneRuntimeContextRoutingDecision(plan.ContextRouting),
	}
	usageEvent.applyRuntimePricingResult(telemetry.pricingResult)
	return usageEvent
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

func marshalAuditHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	sanitized := make(map[string]string, len(headers))
	for key, value := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		if _, ok := clientAuthHeaders[normalizedKey]; ok {
			sanitized[normalizedKey] = "[REDACTED]"
			continue
		}
		sanitized[normalizedKey] = value
	}
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func marshalAuditHTTPHeaders(headers http.Header) *string {
	if len(headers) == 0 {
		return nil
	}
	flattened := make(map[string]string, len(headers))
	for key, values := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		flattened[normalizedKey] = strings.Join(values, ", ")
	}
	encoded, err := json.Marshal(flattened)
	if err != nil {
		return nil
	}
	resolved := string(encoded)
	return &resolved
}

func materializeRuntimeTelemetryEnvelopeTx(ctx context.Context, tx pgx.Tx, logPartitions *runtimeLogPartitionCache, envelope runtimeTelemetryEnvelope) (int, error) {
	envelope = normalizeRuntimeTelemetryEnvelopeTimestamps(envelope)
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
			`INSERT INTO request_logs (profile_id, model_id, resolved_target_model_id, api_family, operation_name, vendor_id, vendor_key, vendor_name, endpoint_id, connection_id, selected_terminal_target_id, proxy_api_key_id, proxy_api_key_name_snapshot, ingress_request_id, attempt_number, provider_correlation_id, endpoint_base_url, status_code, response_time_ms, is_stream, input_tokens, output_tokens, total_tokens, success_flag, billable_flag, priced_flag, unpriced_reason, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, request_path, error_detail, endpoint_description, created_at, caller_user_agent, upstream_user_agent, completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind, stream_error_detail, audit_enabled_at_request, audit_capture_bodies_at_request, request_generation_params, request_generation_params_status, context_routing, upstream_operation_name, operation_translation_mode, upstream_request_path) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51, $52, $53, $54, $55, $56, $57, $58, $59, $60, $61, $62, $63, $64, $65, $66, $67, $68) RETURNING id`,
			requestLog.ProfileID,
			requestLog.ModelID,
			nullableStringArg(requestLog.ResolvedTargetModelID),
			requestLog.APIFamily,
			requestLog.OperationName,
			nullableIntArg(requestLog.VendorID),
			nullableStringArg(requestLog.VendorKey),
			nullableStringArg(requestLog.VendorName),
			nullableIntArg(requestLog.EndpointID),
			nullableIntArg(requestLog.ConnectionID),
			nullableIntArg(requestLog.SelectedTerminalTargetID),
			nullableIntArg(requestLog.ProxyAPIKeyID),
			nullableStringArg(requestLog.ProxyAPIKeyNameSnapshot),
			requestLog.IngressRequestID,
			requestLog.AttemptNumber,
			nullableStringArg(requestLog.ProviderCorrelationID),
			nullableStringArg(requestLog.EndpointBaseURL),
			requestLog.StatusCode,
			requestLog.ResponseTimeMS,
			requestLog.IsStream,
			nullableIntArg(requestLog.InputTokens),
			nullableIntArg(requestLog.OutputTokens),
			nullableIntArg(requestLog.TotalTokens),
			requestLog.SuccessFlag,
			nullableBoolArg(requestLog.BillableFlag),
			nullableBoolArg(requestLog.PricedFlag),
			nullableStringArg(requestLog.UnpricedReason),
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
			requestLog.RequestPath,
			nullableStringArg(requestLog.ErrorDetail),
			nullableStringArg(requestLog.EndpointDescription),
			requestLog.CreatedAt.UTC(),
			nullableStringArg(requestLog.CallerUserAgent),
			nullableStringArg(requestLog.UpstreamUserAgent),
			nullableIntArg(requestLog.CompletionDurationMS),
			nullableIntArg(requestLog.TTFTMS),
			requestLog.StreamOutcome,
			nullableStringArg(requestLog.StreamErrorKind),
			nullableStringArg(requestLog.StreamErrorDetail),
			requestLog.AuditEnabledAtRequest,
			requestLog.AuditCaptureBodiesAtRequest,
			nullableJSONArg(requestLog.RequestGenerationParams),
			nullableStringArg(requestLog.RequestGenerationParamsStatus),
			nullableJSONArg(requestLog.ContextRouting),
			nullableStringArg(requestLog.UpstreamOperationName),
			nullableStringArg(requestLog.OperationTranslationMode),
			nullableStringArg(requestLog.UpstreamRequestPath),
		).Scan(&requestLogID)
		if err != nil {
			return 0, fmt.Errorf("insert request log: %w", err)
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
		`INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, resolved_target_model_id, api_family, operation_name, endpoint_id, connection_id, selected_terminal_target_id, proxy_api_key_id, proxy_api_key_name_snapshot, status_code, success_flag, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens, input_cost_micros, output_cost_micros, cache_read_input_cost_micros, cache_creation_input_cost_micros, reasoning_cost_micros, total_cost_original_micros, total_cost_user_currency_micros, currency_code_original, report_currency_code, report_currency_symbol, fx_rate_used, fx_rate_source, pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output, pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input, pricing_snapshot_reasoning, pricing_config_version_used, attempt_count, request_path, created_at, response_time_ms, completion_duration_ms, ttft_ms, stream_outcome, stream_error_kind, billable_flag, priced_flag, unpriced_reason, context_routing, upstream_operation_name, operation_translation_mode, upstream_request_path) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, $32, $33, $34, $35, $36, $37, $38, $39, $40, $41, $42, $43, $44, $45, $46, $47, $48, $49, $50, $51, $52, $53)`,
		usageEvent.ProfileID,
		usageEvent.IngressRequestID,
		usageEvent.ModelID,
		nullableStringArg(usageEvent.ResolvedTargetModelID),
		usageEvent.APIFamily,
		usageEvent.OperationName,
		nullableIntArg(usageEvent.EndpointID),
		nullableIntArg(usageEvent.ConnectionID),
		nullableIntArg(usageEvent.SelectedTerminalTargetID),
		nullableIntArg(usageEvent.ProxyAPIKeyID),
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
		nullableBoolArg(usageEvent.BillableFlag),
		nullableBoolArg(usageEvent.PricedFlag),
		nullableStringArg(usageEvent.UnpricedReason),
		nullableJSONArg(usageEvent.ContextRouting),
		nullableStringArg(usageEvent.UpstreamOperationName),
		nullableStringArg(usageEvent.OperationTranslationMode),
		nullableStringArg(usageEvent.UpstreamRequestPath),
	); err != nil {
		return 0, fmt.Errorf("insert usage event: %w", err)
	}
	return requestLogID, nil
}

func insertRuntimeAuditLogTx(ctx context.Context, tx pgx.Tx, requestLogID int, requestLogCreatedAt time.Time, ingressRequestID string, auditLog auditLogInsert) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO audit_logs (request_log_id, request_log_created_at, ingress_request_id, profile_id, vendor_id, model_id, endpoint_id, connection_id, endpoint_base_url, endpoint_description, request_method, request_url, request_headers, request_body, request_body_stored, response_status, response_headers, response_body, response_body_stored, is_stream, duration_ms, created_at, audit_enabled_at_request, audit_capture_bodies_at_request) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)`,
		requestLogID,
		requestLogCreatedAt.UTC(),
		ingressRequestID,
		auditLog.ProfileID,
		nullableIntArg(auditLog.VendorID),
		auditLog.ModelID,
		auditLog.EndpointID,
		auditLog.ConnectionID,
		auditLog.EndpointBaseURL,
		nullableStringArg(auditLog.EndpointDescription),
		auditLog.RequestMethod,
		auditLog.RequestURL,
		auditLog.RequestHeaders,
		nullableStringArg(auditLog.RequestBody),
		auditLog.RequestBodyStored,
		auditLog.ResponseStatus,
		nullableStringArg(auditLog.ResponseHeaders),
		nullableStringArg(auditLog.ResponseBody),
		auditLog.ResponseBodyStored,
		auditLog.IsStream,
		auditLog.DurationMS,
		auditLog.CreatedAt.UTC(),
		auditLog.AuditEnabledAtRequest,
		auditLog.AuditCaptureBodiesAtRequest,
	); err != nil {
		return fmt.Errorf("insert audit log for request log %d: %w", requestLogID, err)
	}
	return nil
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

func billingState(success bool) (*bool, *bool, *string) {
	if !success {
		return boolPtr(false), boolPtr(false), nil
	}
	return boolPtr(true), boolPtr(false), stringPtr("missing_pricing_template")
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

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
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
