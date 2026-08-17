package runtime

import (
	"encoding/json"
	"strings"

	anthropicprovider "github.com/coachpo/prism/backend/internal/gateway/provider/anthropic"
	geminiprovider "github.com/coachpo/prism/backend/internal/gateway/provider/gemini"
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
	return usage, usage.hasValues() || usage.discarded
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
	if parsed.discarded {
		usage.discarded = true
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
