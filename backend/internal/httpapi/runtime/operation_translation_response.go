package runtime

import (
	"fmt"
	"io"

	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

const openAITranslatedNonStreamResponseBodyLimit int64 = 8 * 1024 * 1024

func translateOpenAIResponse(rawBody []byte, mode TranslationMode, requestedModelID string) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	return translateOpenAIResponseWithToolContext(rawBody, mode, requestedModelID, nil)
}

func translateOpenAIResponseWithToolContext(rawBody []byte, mode TranslationMode, requestedModelID string, toolContext *openai.ToolContext) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	if mode == "" || mode == TranslationModeNone {
		return append([]byte(nil), rawBody...), responseUsage{}, runtimeUsageNormalizationRule{}, nil
	}
	body, usageEnvelope, _, err := openai.TranslateResponseWithToolContext(rawBody, providerTranslationMode(mode), requestedModelID, toolContext)
	if err != nil {
		if domainErr := domainErrorFromProviderAdapterError(err); domainErr != nil {
			return nil, responseUsage{}, runtimeUsageNormalizationRule{}, domainErr
		}
		return nil, responseUsage{}, runtimeUsageNormalizationRule{}, err
	}
	usage, usageRule := responseUsageFromProviderEnvelope(usageEnvelope)
	return body, usage, usageRule, nil
}

func translateOpenAIResponsesUpstreamToChatClientResponseWithRequestedModel(rawBody []byte, requestedModelID string) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	return translateOpenAIResponse(rawBody, TranslationModeOpenAIChatCompletionsToResponses, requestedModelID)
}

func translateOpenAIChatUpstreamToResponsesClientResponseWithRequestedModel(rawBody []byte, requestedModelID string) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	return translateOpenAIResponse(rawBody, TranslationModeOpenAIResponsesToChatCompletions, requestedModelID)
}

func buildOpenAIChatCompletionsUsagePayload(usage responseUsage) map[string]any {
	return buildOpenAIUsagePayload("prompt_tokens", "completion_tokens", "prompt_tokens_details", "completion_tokens_details", usage)
}

func buildOpenAIUsagePayload(inputKey string, outputKey string, inputDetailsKey string, outputDetailsKey string, usage responseUsage) map[string]any {
	usage = usage.normalized()
	payload := map[string]any{}
	if inputTokens := openAIParentInputTokens(usage); inputTokens != nil {
		payload[inputKey] = *inputTokens
	}
	if outputTokens := openAIParentOutputTokens(usage); outputTokens != nil {
		payload[outputKey] = *outputTokens
	}
	if usage.TotalTokens != nil {
		payload["total_tokens"] = *usage.TotalTokens
	}
	if usage.CacheReadInputTokens != nil {
		payload[inputDetailsKey] = map[string]any{"cached_tokens": *usage.CacheReadInputTokens}
	}
	if usage.ReasoningTokens != nil {
		payload[outputDetailsKey] = map[string]any{"reasoning_tokens": *usage.ReasoningTokens}
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func openAIParentInputTokens(usage responseUsage) *int {
	if usage.InputTokens == nil && usage.CacheReadInputTokens == nil && usage.CacheCreationInputTokens == nil {
		return nil
	}
	total := intValue(usage.InputTokens) + intValue(usage.CacheReadInputTokens) + intValue(usage.CacheCreationInputTokens)
	return &total
}

func openAIParentOutputTokens(usage responseUsage) *int {
	if usage.OutputTokens == nil && usage.ReasoningTokens == nil {
		return nil
	}
	total := intValue(usage.OutputTokens) + intValue(usage.ReasoningTokens)
	return &total
}

func firstOpenAIResponseTranslationValue(payload map[string]any, key string) any {
	if payload == nil {
		return nil
	}
	if value, ok := payload[key]; ok {
		return value
	}
	responsePayload, ok := payload["response"].(map[string]any)
	if !ok {
		return nil
	}
	return responsePayload[key]
}

func readBoundedResponseBody(src io.Reader, limit int64) ([]byte, error) {
	if src == nil {
		return nil, nil
	}
	if limit <= 0 {
		return io.ReadAll(src)
	}
	body, err := io.ReadAll(io.LimitReader(src, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("translated OpenAI response exceeded %d-byte limit", limit)
	}
	return body, nil
}
