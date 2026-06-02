package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const openAITranslatedNonStreamResponseBodyLimit int64 = 8 * 1024 * 1024

func translateOpenAIResponse(rawBody []byte, mode TranslationMode) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	switch mode {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return translateOpenAIChatToResponsesResponse(rawBody)
	case TranslationModeOpenAIChatCompletionsToResponses:
		return translateOpenAIResponsesToChatResponse(rawBody)
	case "", TranslationModeNone:
		return append([]byte(nil), rawBody...), responseUsage{}, runtimeUsageNormalizationRule{}, nil
	default:
		return nil, responseUsage{}, runtimeUsageNormalizationRule{}, unsupportedTranslationModeError(mode)
	}
}

func translateOpenAIResponsesToChatResponse(rawBody []byte) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	payload, err := decodeOpenAIResponseTranslationPayload(rawBody)
	if err != nil {
		return nil, responseUsage{}, runtimeUsageRuleOpenAIResponses, err
	}
	usage := extractResponseUsageFromPayload(payload, runtimeUsageRuleOpenAIResponses)
	if _, ok := payload["error"]; ok {
		body, err := marshalTranslatedOpenAIResponse(payload, TranslationModeOpenAIResponsesToChatCompletions)
		return body, usage, runtimeUsageRuleOpenAIResponses, err
	}
	output, ok := firstOpenAIResponseTranslationValue(payload, "output").([]any)
	if !ok {
		return nil, usage, runtimeUsageRuleOpenAIResponses, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "responses_output")
	}
	choice, err := translateResponsesOutputToChatChoice(output)
	if err != nil {
		return nil, usage, runtimeUsageRuleOpenAIResponses, err
	}
	translated := map[string]any{
		"object":  "chat.completion",
		"choices": []any{choice},
	}
	copyOpenAIResponseTranslationFields(payload, translated, "id", "model", "service_tier", "system_fingerprint")
	if createdAt := intPointerFromAny(firstOpenAIResponseTranslationValue(payload, "created_at")); createdAt != nil {
		translated["created"] = *createdAt
	} else if created := intPointerFromAny(firstOpenAIResponseTranslationValue(payload, "created")); created != nil {
		translated["created"] = *created
	}
	if usagePayload := buildOpenAIChatCompletionsUsagePayload(usage); len(usagePayload) > 0 {
		translated["usage"] = usagePayload
	}
	body, err := marshalTranslatedOpenAIResponse(translated, TranslationModeOpenAIResponsesToChatCompletions)
	return body, usage, runtimeUsageRuleOpenAIResponses, err
}

func translateOpenAIChatToResponsesResponse(rawBody []byte) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	payload, err := decodeOpenAIResponseTranslationPayload(rawBody)
	if err != nil {
		return nil, responseUsage{}, runtimeUsageRuleOpenAIChatCompletions, err
	}
	usage := extractResponseUsageFromPayload(payload, runtimeUsageRuleOpenAIChatCompletions)
	if _, ok := payload["error"]; ok {
		body, err := marshalTranslatedOpenAIResponse(payload, TranslationModeOpenAIChatCompletionsToResponses)
		return body, usage, runtimeUsageRuleOpenAIChatCompletions, err
	}
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, usage, runtimeUsageRuleOpenAIChatCompletions, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "chat_choices")
	}
	if len(choices) > 1 {
		return nil, usage, runtimeUsageRuleOpenAIChatCompletions, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "chat_multi_choice")
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, usage, runtimeUsageRuleOpenAIChatCompletions, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "chat_choice")
	}
	output, err := translateChatChoiceToResponsesOutput(choice)
	if err != nil {
		return nil, usage, runtimeUsageRuleOpenAIChatCompletions, err
	}
	translated := map[string]any{
		"object": "response",
		"status": "completed",
		"output": output,
	}
	copyOpenAIResponseTranslationFields(payload, translated, "id", "model", "service_tier")
	if created := intPointerFromAny(payload["created"]); created != nil {
		translated["created_at"] = *created
	}
	if usagePayload := buildOpenAIResponsesUsagePayload(usage); len(usagePayload) > 0 {
		translated["usage"] = usagePayload
	}
	body, err := marshalTranslatedOpenAIResponse(translated, TranslationModeOpenAIChatCompletionsToResponses)
	return body, usage, runtimeUsageRuleOpenAIChatCompletions, err
}

func translateResponsesOutputToChatChoice(output []any) (map[string]any, error) {
	message := map[string]any{"role": "assistant", "content": ""}
	toolCalls := make([]any, 0)
	seenMessage := false
	for _, rawItem := range output {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "responses_output_item")
		}
		switch strings.TrimSpace(stringValue(item["type"])) {
		case "message":
			if seenMessage {
				return nil, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "responses_output_multiple_messages")
			}
			role := firstNonEmptyString(item["role"], "assistant")
			content, err := translateResponsesMessageContentToChat(item["content"], role)
			if err != nil {
				return nil, err
			}
			message["role"] = role
			if content == nil {
				message["content"] = ""
			} else {
				message["content"] = content
			}
			seenMessage = true
		case "function_call":
			toolCall, err := translateResponsesFunctionCallToChat(item)
			if err != nil {
				return nil, err
			}
			toolCalls = append(toolCalls, toolCall)
		default:
			return nil, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "responses_output_type")
		}
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	return map[string]any{"index": 0, "message": message, "finish_reason": finishReason}, nil
}

func translateChatChoiceToResponsesOutput(choice map[string]any) ([]any, error) {
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return nil, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "chat_message")
	}
	role := firstNonEmptyString(message["role"], "assistant")
	output := make([]any, 0, 2)
	content, err := translateChatResponseContentToResponsesOutput(message["content"])
	if err != nil {
		return nil, err
	}
	if valueHasMeaning(content) {
		output = append(output, map[string]any{"type": "message", "role": role, "content": content})
	}
	if fieldHasValue(message, "tool_calls") {
		toolCalls, err := translateChatToolCallsToResponses(message["tool_calls"])
		if err != nil {
			return nil, err
		}
		output = append(output, toolCalls...)
	}
	if len(output) == 0 {
		output = append(output, map[string]any{"type": "message", "role": role, "content": []any{}})
	}
	return output, nil
}

func translateChatResponseContentToResponsesOutput(value any) ([]any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return []any{map[string]any{"type": "output_text", "text": typed}}, nil
	case []any:
		parts := make([]any, 0, len(typed))
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part")
			}
			switch strings.TrimSpace(stringValue(part["type"])) {
			case "text", "output_text":
				parts = append(parts, map[string]any{"type": "output_text", "text": stringValue(part["text"])})
			default:
				return nil, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part_type")
			}
		}
		if len(parts) == 0 {
			return nil, nil
		}
		return parts, nil
	default:
		return nil, unsupportedOpenAIResponseTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content")
	}
}

func buildOpenAIChatCompletionsUsagePayload(usage responseUsage) map[string]any {
	return buildOpenAIUsagePayload("prompt_tokens", "completion_tokens", "prompt_tokens_details", "completion_tokens_details", usage)
}

func buildOpenAIResponsesUsagePayload(usage responseUsage) map[string]any {
	return buildOpenAIUsagePayload("input_tokens", "output_tokens", "input_tokens_details", "output_tokens_details", usage)
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

func copyOpenAIResponseTranslationFields(source map[string]any, target map[string]any, keys ...string) {
	for _, key := range keys {
		if value := firstOpenAIResponseTranslationValue(source, key); value != nil {
			target[key] = value
		}
	}
}

func decodeOpenAIResponseTranslationPayload(rawBody []byte) (map[string]any, error) {
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode translated OpenAI response: %w", err)
	}
	return payload, nil
}

func marshalTranslatedOpenAIResponse(payload map[string]any, mode TranslationMode) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal translated OpenAI response for %s: %w", mode, err)
	}
	return raw, nil
}

func unsupportedOpenAIResponseTranslationShapeError(mode TranslationMode, reason string) error {
	return fmt.Errorf("unsupported translated %s response shape: %s", mode, strings.TrimSpace(reason))
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
