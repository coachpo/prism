package openai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

func extractResponseUsageByOperation(operation provider.Operation, body []byte) (provider.UsageEnvelope, string) {
	switch strings.TrimSpace(operation.Name) {
	case OperationChatCompletions:
		return extractResponseUsage(body, OperationChatCompletions), OperationChatCompletions
	case OperationResponses, OperationResponsesCompact:
		return extractResponseUsage(body, OperationResponses), OperationResponses
	case OperationResponsesInputTokens:
		return extractTokenCountUsage(body), OperationResponsesInputTokens
	default:
		return provider.UsageEnvelope{}, ""
	}
}

func extractResponseUsage(body []byte, rule string) provider.UsageEnvelope {
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return provider.UsageEnvelope{}
	}
	usagePayload, _ := responsePayloadValue(payload, "usage").(map[string]any)
	if usagePayload == nil {
		return provider.UsageEnvelope{}
	}
	usage := provider.UsageEnvelope{NormalizationRule: rule}
	switch rule {
	case OperationChatCompletions:
		if details, _ := usagePayload["prompt_tokens_details"].(map[string]any); details != nil {
			usage.CacheReadInputTokens = responseIntPointer(details["cached_tokens"])
		}
		if details, _ := usagePayload["completion_tokens_details"].(map[string]any); details != nil {
			usage.ReasoningTokens = responseIntPointer(details["reasoning_tokens"])
		}
		if input := responseIntPointer(usagePayload["prompt_tokens"]); input != nil {
			base := *input - responseIntValue(usage.CacheReadInputTokens) - responseIntValue(usage.CacheCreationInputTokens)
			usage.InputTokens = &base
		}
		if output := responseIntPointer(usagePayload["completion_tokens"]); output != nil {
			base := *output - responseIntValue(usage.ReasoningTokens)
			usage.OutputTokens = &base
		}
	case OperationResponses:
		if details, _ := usagePayload["input_tokens_details"].(map[string]any); details != nil {
			usage.CacheReadInputTokens = responseIntPointer(details["cached_tokens"])
		}
		if details, _ := usagePayload["output_tokens_details"].(map[string]any); details != nil {
			usage.ReasoningTokens = responseIntPointer(details["reasoning_tokens"])
		}
		if input := responseIntPointer(usagePayload["input_tokens"]); input != nil {
			base := *input - responseIntValue(usage.CacheReadInputTokens) - responseIntValue(usage.CacheCreationInputTokens)
			usage.InputTokens = &base
		}
		if output := responseIntPointer(usagePayload["output_tokens"]); output != nil {
			base := *output - responseIntValue(usage.ReasoningTokens)
			usage.OutputTokens = &base
		}
	}
	usage.TotalTokens = responseIntPointer(usagePayload["total_tokens"])
	return normalizeUsage(usage)
}

func extractTokenCountUsage(body []byte) provider.UsageEnvelope {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return provider.UsageEnvelope{}
	}
	usage := provider.UsageEnvelope{
		InputTokens:              responseIntPointer(payload["input_tokens"]),
		CacheReadInputTokens:     responseIntPointer(firstResponseValue(payload, "cache_read_input_tokens", "cachedContentTokenCount")),
		CacheCreationInputTokens: responseIntPointer(payload["cache_creation_input_tokens"]),
		NormalizationRule:        OperationResponsesInputTokens,
	}
	if total := responseIntPointer(firstResponseValue(payload, "total_tokens", "totalTokens")); total != nil {
		usage.TotalTokens = total
		if usage.InputTokens == nil {
			usage.InputTokens = total
		}
	}
	return normalizeUsage(usage)
}

func normalizeUsage(usage provider.UsageEnvelope) provider.UsageEnvelope {
	if usage.TotalTokens == nil && (usage.InputTokens != nil || usage.OutputTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil || usage.ReasoningTokens != nil) {
		total := responseIntValue(usage.InputTokens) + responseIntValue(usage.OutputTokens) + responseIntValue(usage.CacheReadInputTokens) + responseIntValue(usage.CacheCreationInputTokens) + responseIntValue(usage.ReasoningTokens)
		usage.TotalTokens = &total
	}
	return usage
}

func responsePayloadValue(payload map[string]any, key string) any {
	if value, ok := payload[key]; ok {
		return value
	}
	response, _ := payload["response"].(map[string]any)
	return response[key]
}

func firstResponseValue(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

func responseIntPointer(value any) *int {
	switch typed := value.(type) {
	case int:
		return &typed
	case int64:
		converted := int(typed)
		return &converted
	case float64:
		if typed != float64(int(typed)) {
			return nil
		}
		converted := int(typed)
		return &converted
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return nil
		}
		converted := int(parsed)
		return &converted
	default:
		return nil
	}
}

func responseIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

type overflowEvidence struct {
	code    *string
	message *string
}

func ClassifyOverflowResponse(statusCode int, rawBody []byte) provider.OverflowClassification {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusRequestEntityTooLarge && statusCode != http.StatusUnprocessableEntity && statusCode != http.StatusTooManyRequests {
		return provider.OverflowClassification{}
	}
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return provider.OverflowClassification{}
	}
	evidence, ok := overflowEvidenceFromPayload(payload)
	if !ok {
		return provider.OverflowClassification{}
	}
	if evidence.code != nil {
		code := strings.ToLower(strings.TrimSpace(*evidence.code))
		if code == "context_length_exceeded" || code == "context_too_large" {
			return provider.OverflowClassification{Promotable: true, ErrorCode: code, Classifier: "error_code"}
		}
		return provider.OverflowClassification{}
	}
	message := normalizedOverflowMessage(evidence.message)
	if message == "" || overflowMessageRejected(message) {
		return provider.OverflowClassification{}
	}
	if containsOverflowFragment(message, "maximum context length", "max context length", "context length", "context window", "too many tokens") && containsOverflowFragment(message, "exceeded", "exceeds", "too large", "too long", "over limit", "over the limit") {
		return provider.OverflowClassification{Promotable: true, Classifier: "message_text"}
	}
	return provider.OverflowClassification{}
}

func overflowEvidenceFromPayload(payload map[string]any) (overflowEvidence, bool) {
	if errorPayload, ok := payload["error"].(map[string]any); ok {
		evidence := overflowEvidence{code: responseStringPointer(errorPayload["code"])}
		if message := responseStringPointer(firstResponseValue(errorPayload, "message", "detail")); message != nil {
			evidence.message = message
		} else {
			evidence.message = responseStringPointer(firstResponseValue(payload, "message", "detail"))
		}
		return evidence, evidence.code != nil || evidence.message != nil
	}
	evidence := overflowEvidence{code: responseStringPointer(payload["code"]), message: responseStringPointer(firstResponseValue(payload, "detail", "message", "error"))}
	return evidence, evidence.code != nil || evidence.message != nil
}

func responseStringPointer(value any) *string {
	text, _ := value.(string)
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizedOverflowMessage(message *string) string {
	if message == nil {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(*message), " "))
}

func overflowMessageRejected(message string) bool {
	return containsOverflowFragment(message, "model_not_found", "model not found", "unknown model", "unknown provider", "does not exist", "invalid_api_key", "invalid api key", "incorrect api key", "authentication", "unauthorized", "permission denied", "forbidden", "insufficient_quota", "insufficient quota", "quota exceeded", "credit balance", "balance", "billing", "hard limit", "rate_limit", "rate limit", "too many requests", "tokens per minute", "per minute", "retry after", "server overloaded", "overloaded", "capacity exceeded", "capacity exhausted", "temporarily unavailable", "try again later", "moderation", "safety")
}

func containsOverflowFragment(message string, fragments ...string) bool {
	for _, fragment := range fragments {
		if fragment != "" && strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}
