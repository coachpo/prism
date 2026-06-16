package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

const (
	responseTranslationUnsupportedErrorCode = "openai_response_translation_unsupported"
	responseTranslationUnsupportedDetail    = "Prism cannot translate this OpenAI response shape for the selected target."
)

func responseTranslationUnsupportedError(mode provider.TranslationMode, reason string) *provider.AdapterError {
	fields := map[string]any{}
	if strings.TrimSpace(string(mode)) != "" {
		fields["translation_mode"] = string(mode)
	}
	if strings.TrimSpace(reason) != "" {
		fields["unsupported_reason"] = strings.TrimSpace(reason)
	}
	return &provider.AdapterError{HTTPStatus: http.StatusBadGateway, Code: responseTranslationUnsupportedErrorCode, Detail: responseTranslationUnsupportedDetail, Fields: fields}
}

func translateResponse(rawBody []byte, mode provider.TranslationMode, requestedModelID string) ([]byte, provider.UsageEnvelope, string, error) {
	switch mode {
	case provider.TranslationModeOpenAIResponsesToChatCompletions:
		return translateChatToResponsesResponseWithRequestedModel(rawBody, requestedModelID)
	case provider.TranslationModeOpenAIChatCompletionsToResponses:
		return translateResponsesToChatResponseWithRequestedModel(rawBody, requestedModelID)
	case "", provider.TranslationModeNone:
		return append([]byte(nil), rawBody...), provider.UsageEnvelope{}, "", nil
	default:
		return nil, provider.UsageEnvelope{}, "", unsupportedTranslationMode(mode)
	}
}

func translateResponsesToChatResponseWithRequestedModel(rawBody []byte, requestedModelID string) ([]byte, provider.UsageEnvelope, string, error) {
	payload, err := decodeOpenAIResponseTranslationPayload(rawBody)
	if err != nil {
		return nil, provider.UsageEnvelope{}, OperationResponses, err
	}
	usage := extractResponseUsageFromPayload(payload, OperationResponses)
	if fieldHasValue(payload, "error") {
		body, err := marshalTranslatedOpenAIResponse(payload, provider.TranslationModeOpenAIResponsesToChatCompletions)
		return body, usage, OperationResponses, err
	}
	output, ok := firstOpenAIResponseTranslationValue(payload, "output").([]any)
	if !ok {
		return nil, usage, OperationResponses, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_output")
	}
	choice, err := translateResponsesOutputToChatChoice(output)
	if err != nil {
		return nil, usage, OperationResponses, err
	}
	translated := map[string]any{
		"object":  "chat.completion",
		"choices": []any{choice},
	}
	copyOpenAIResponseTranslationFields(payload, translated, "id", "model", "service_tier", "system_fingerprint")
	normalizeTranslatedResponsesPublicModel(translated, requestedModelID)
	if createdAt := intPointerFromAny(firstOpenAIResponseTranslationValue(payload, "created_at")); createdAt != nil {
		translated["created"] = *createdAt
	} else if created := intPointerFromAny(firstOpenAIResponseTranslationValue(payload, "created")); created != nil {
		translated["created"] = *created
	}
	if usagePayload := buildOpenAIChatCompletionsUsagePayload(usage); len(usagePayload) > 0 {
		translated["usage"] = usagePayload
	}
	body, err := marshalTranslatedOpenAIResponse(translated, provider.TranslationModeOpenAIResponsesToChatCompletions)
	return body, usage, OperationResponses, err
}

func translateChatToResponsesResponseWithRequestedModel(rawBody []byte, requestedModelID string) ([]byte, provider.UsageEnvelope, string, error) {
	payload, err := decodeOpenAIResponseTranslationPayload(rawBody)
	if err != nil {
		return nil, provider.UsageEnvelope{}, OperationChatCompletions, err
	}
	usage := extractResponseUsageFromPayload(payload, OperationChatCompletions)
	if fieldHasValue(payload, "error") {
		normalizeTranslatedResponsesPublicModel(payload, requestedModelID)
		body, err := marshalTranslatedOpenAIResponse(payload, provider.TranslationModeOpenAIChatCompletionsToResponses)
		return body, usage, OperationChatCompletions, err
	}
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, usage, OperationChatCompletions, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_choices")
	}
	if len(choices) > 1 {
		return nil, usage, OperationChatCompletions, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_multi_choice")
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, usage, OperationChatCompletions, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_choice")
	}
	output, err := translateChatChoiceToResponsesOutput(choice)
	if err != nil {
		return nil, usage, OperationChatCompletions, err
	}
	translated := map[string]any{
		"object": "response",
		"status": responseStatusFromChatFinishReason(stringValue(choice["finish_reason"])),
		"output": output,
	}
	if stringValue(choice["finish_reason"]) == "length" {
		translated["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	copyOpenAIResponseTranslationFields(payload, translated, "id", "model", "service_tier")
	normalizeTranslatedResponsesPublicModel(translated, requestedModelID)
	if created := intPointerFromAny(payload["created"]); created != nil {
		translated["created_at"] = *created
	}
	if usagePayload := buildOpenAIResponsesUsagePayload(usage); len(usagePayload) > 0 {
		translated["usage"] = usagePayload
	}
	body, err := marshalTranslatedOpenAIResponse(translated, provider.TranslationModeOpenAIChatCompletionsToResponses)
	return body, usage, OperationChatCompletions, err
}

func normalizeTranslatedResponsesPublicModel(payload map[string]any, requestedModelID string) {
	if payload == nil {
		return
	}
	requestedModelID = strings.TrimSpace(requestedModelID)
	if requestedModelID == "" {
		return
	}
	payload["model"] = requestedModelID
}

func translateResponsesOutputToChatChoice(output []any) (map[string]any, error) {
	message := map[string]any{"role": "assistant", "content": ""}
	seenMessage := false
	toolCalls := make([]any, 0)
	finishReason := "stop"
	for _, rawItem := range output {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_output_item")
		}
		switch strings.TrimSpace(stringValue(item["type"])) {
		case "reasoning":
			if reasoning := ExtractReasoningSummaryText(item); reasoning != nil {
				AppendReasoningContent(message, reasoning.Text)
			}
		case "message":
			if seenMessage {
				return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_output_multiple_messages")
			}
			role := firstNonEmptyString(item["role"], "assistant")
			content, refusal, err := translateResponsesMessageContentToChatResponse(item["content"], role)
			if err != nil {
				return nil, err
			}
			message["role"] = role
			if content == nil {
				message["content"] = ""
			} else {
				message["content"] = content
			}
			if refusal != nil {
				message["refusal"] = *refusal
			}
			seenMessage = true
		case "function_call":
			toolCalls = append(toolCalls, responsesFunctionCallOutputItemToChatToolCall(item))
			finishReason = "tool_calls"
		case "custom_tool_call":
			toolCalls = append(toolCalls, responsesCustomToolCallOutputItemToChatToolCall(item))
			finishReason = "tool_calls"
		case "tool_search_call":
			toolCalls = append(toolCalls, responsesToolSearchOutputItemToChatToolCall(item))
			finishReason = "tool_calls"
		default:
			return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_output_type")
		}
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		if !seenMessage {
			message["content"] = nil
		}
	}
	if !seenMessage && len(toolCalls) == 0 {
		return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_output_message")
	}
	return map[string]any{"index": 0, "message": message, "finish_reason": finishReason}, nil
}

func responsesFunctionCallOutputItemToChatToolCall(item map[string]any) map[string]any {
	return map[string]any{"id": firstNonEmptyString(item["call_id"], item["id"]), "type": "function", "function": map[string]any{"name": stringValue(item["name"]), "arguments": canonicalToolArguments(item["arguments"])}}
}

func responsesCustomToolCallOutputItemToChatToolCall(item map[string]any) map[string]any {
	return map[string]any{"id": firstNonEmptyString(item["call_id"], item["id"]), "type": "function", "function": map[string]any{"name": stringValue(item["name"]), "arguments": canonicalJSONString(map[string]any{customToolInputField: item["input"]})}}
}

func responsesToolSearchOutputItemToChatToolCall(item map[string]any) map[string]any {
	return map[string]any{"id": firstNonEmptyString(item["call_id"], item["id"]), "type": "function", "function": map[string]any{"name": toolSearchProxyName, "arguments": canonicalToolArguments(item["arguments"])}}
}

func translateResponsesMessageContentToChatResponse(value any, role string) (any, *string, error) {
	switch typed := value.(type) {
	case nil, string:
		return typed, nil, nil
	case []any:
		parts := make([]any, 0, len(typed))
		var refusal *string
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_message_part")
			}
			switch strings.ToLower(strings.TrimSpace(stringValue(part["type"]))) {
			case "input_text", "output_text", "text":
				parts = append(parts, map[string]any{"type": "text", "text": stringValue(part["text"])})
			case "refusal":
				if value := trimmedStringFromAny(part["refusal"]); value != nil {
					refusal = value
				}
			default:
				return nil, nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_message_part_type")
			}
		}
		if len(parts) == 0 {
			return nil, refusal, nil
		}
		if len(parts) == 1 {
			if textPart, ok := parts[0].(map[string]any); ok && len(textPart) == 2 && textPart["type"] == "text" && role != "user" {
				return stringValue(textPart["text"]), refusal, nil
			}
		}
		return parts, refusal, nil
	default:
		return nil, nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_message_content")
	}
}

func translateChatChoiceToResponsesOutput(choice map[string]any) ([]any, error) {
	message, ok := choice["message"].(map[string]any)
	if !ok {
		return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_message")
	}
	role := firstNonEmptyString(message["role"], "assistant")
	output := make([]any, 0, 1)
	reasoning := ExtractReasoningFieldText(message)
	if reasoning == nil {
		if content := stringValue(message["content"]); content != "" {
			if think, _, ok := splitLeadingThinkBlock(content); ok && think != "" {
				reasoning = &ReasoningText{Text: think}
			}
		}
	}
	if reasoning != nil {
		output = append(output, map[string]any{"id": responseReasoningID(payloadStringID(choice)), "type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": reasoning.Text}}})
	}
	content, err := translateChatResponseContentToResponsesOutput(message["content"])
	if err != nil {
		return nil, err
	}
	if refusal := trimmedStringFromAny(message["refusal"]); refusal != nil {
		content = append(content, map[string]any{"type": "refusal", "refusal": *refusal})
	}
	if valueHasMeaning(content) {
		output = append(output, map[string]any{"type": "message", "role": role, "content": content})
	}
	if fieldHasValue(message, "tool_calls") {
		toolItems, err := translateChatResponseToolCallsToResponsesOutput(message["tool_calls"], reasoning)
		if err != nil {
			return nil, err
		}
		output = append(output, toolItems...)
	}
	if fieldHasValue(message, "function_call") {
		output = append(output, translateChatResponseLegacyFunctionCallToResponsesOutput(message["function_call"], reasoning))
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
		if _, answer, ok := splitLeadingThinkBlock(typed); ok {
			typed = answer
		}
		if strings.TrimSpace(typed) == "" {
			return nil, nil
		}
		return []any{map[string]any{"type": "output_text", "text": typed}}, nil
	case []any:
		parts := make([]any, 0, len(typed))
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part")
			}
			switch strings.TrimSpace(stringValue(part["type"])) {
			case "text", "output_text":
				parts = append(parts, map[string]any{"type": "output_text", "text": stringValue(part["text"])})
			default:
				return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part_type")
			}
		}
		if len(parts) == 0 {
			return nil, nil
		}
		return parts, nil
	default:
		return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_content")
	}
}

func translateChatResponseToolCallsToResponsesOutput(value any, reasoning *ReasoningText) ([]any, error) {
	toolCalls, ok := value.([]any)
	if !ok {
		return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_calls")
	}
	items := make([]any, 0, len(toolCalls))
	for index, rawToolCall := range toolCalls {
		toolCall, ok := rawToolCall.(map[string]any)
		if !ok {
			return nil, unsupportedResponseTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_call")
		}
		function, _ := toolCall["function"].(map[string]any)
		callID := firstNonEmptyString(toolCall["id"], toolCall["call_id"], fmt.Sprintf("call_%d", index))
		item := ResponseToolCallItemFromChatName(responseToolItemID(callID, stringValue(function["name"]), nil), "completed", callID, stringValue(function["name"]), canonicalToolArguments(function["arguments"]), reasoningTextValue(reasoning), nil)
		items = append(items, item)
	}
	return items, nil
}

func translateChatResponseLegacyFunctionCallToResponsesOutput(value any, reasoning *ReasoningText) map[string]any {
	function, _ := value.(map[string]any)
	callID := firstNonEmptyString(function["id"], "call_0")
	return ResponseToolCallItemFromChatName(responseToolItemID(callID, stringValue(function["name"]), nil), "completed", callID, stringValue(function["name"]), canonicalToolArguments(function["arguments"]), reasoningTextValue(reasoning), nil)
}

func reasoningTextValue(reasoning *ReasoningText) string {
	if reasoning == nil {
		return ""
	}
	return reasoning.Text
}

func responseToolItemID(callID string, chatName string, context *ToolContext) string {
	if context != nil && context.IsCustomToolChatName(chatName) {
		return "ctc_" + callID
	}
	return "fc_" + callID
}

func payloadStringID(choice map[string]any) string {
	if id := strings.TrimSpace(stringValue(choice["id"])); id != "" {
		return id
	}
	return "resp_chat"
}

func responseReasoningID(responseID string) string {
	if strings.HasPrefix(responseID, "rs_") {
		return responseID
	}
	return "rs_" + responseID
}

func buildOpenAIChatCompletionsUsagePayload(usage provider.UsageEnvelope) map[string]any {
	return buildOpenAIUsagePayload("prompt_tokens", "completion_tokens", "prompt_tokens_details", "completion_tokens_details", usage)
}

func buildOpenAIResponsesUsagePayload(usage provider.UsageEnvelope) map[string]any {
	return buildOpenAIUsagePayload("input_tokens", "output_tokens", "input_tokens_details", "output_tokens_details", usage)
}

func buildOpenAIUsagePayload(inputKey string, outputKey string, inputDetailsKey string, outputDetailsKey string, usage provider.UsageEnvelope) map[string]any {
	usage = normalizeUsage(usage)
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

func openAIParentInputTokens(usage provider.UsageEnvelope) *int {
	if usage.InputTokens == nil && usage.CacheReadInputTokens == nil && usage.CacheCreationInputTokens == nil {
		return nil
	}
	total := intValue(usage.InputTokens) + intValue(usage.CacheReadInputTokens) + intValue(usage.CacheCreationInputTokens)
	return &total
}

func openAIParentOutputTokens(usage provider.UsageEnvelope) *int {
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

func marshalTranslatedOpenAIResponse(payload map[string]any, mode provider.TranslationMode) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal translated OpenAI response for %s: %w", mode, err)
	}
	return raw, nil
}

func unsupportedResponseTranslationShapeError(mode provider.TranslationMode, reason string) error {
	return responseTranslationUnsupportedError(mode, normalizeUnsupportedReason(reason))
}

func responseStatusFromChatFinishReason(finishReason string) string {
	if strings.TrimSpace(finishReason) == "length" {
		return "incomplete"
	}
	return "completed"
}

func extractResponseUsageByOperation(operation provider.Operation, body []byte) (provider.UsageEnvelope, string) {
	switch strings.TrimSpace(operation.Name) {
	case OperationChatCompletions:
		payload, err := decodeOpenAIResponseTranslationPayload(body)
		if err != nil {
			return provider.UsageEnvelope{}, OperationChatCompletions
		}
		return extractResponseUsageFromPayload(payload, OperationChatCompletions), OperationChatCompletions
	case OperationResponses, OperationResponsesCompact:
		payload, err := decodeOpenAIResponseTranslationPayload(body)
		if err != nil {
			return provider.UsageEnvelope{}, OperationResponses
		}
		return extractResponseUsageFromPayload(payload, OperationResponses), OperationResponses
	case OperationResponsesInputTokens:
		return extractTokenCountUsage(body), OperationResponsesInputTokens
	default:
		return provider.UsageEnvelope{}, ""
	}
}

func extractResponseUsageFromPayload(payload map[string]any, rule string) provider.UsageEnvelope {
	usagePayload, _ := firstOpenAIResponseTranslationValue(payload, "usage").(map[string]any)
	if usagePayload == nil {
		return provider.UsageEnvelope{}
	}
	usage := provider.UsageEnvelope{}
	switch rule {
	case OperationChatCompletions:
		if details, _ := usagePayload["prompt_tokens_details"].(map[string]any); details != nil {
			usage.CacheReadInputTokens = intPointerFromAny(details["cached_tokens"])
		}
		if details, _ := usagePayload["completion_tokens_details"].(map[string]any); details != nil {
			usage.ReasoningTokens = intPointerFromAny(details["reasoning_tokens"])
		}
		if input := intPointerFromAny(usagePayload["prompt_tokens"]); input != nil {
			base := *input - intValue(usage.CacheReadInputTokens) - intValue(usage.CacheCreationInputTokens)
			usage.InputTokens = &base
		}
		if output := intPointerFromAny(usagePayload["completion_tokens"]); output != nil {
			base := *output - intValue(usage.ReasoningTokens)
			usage.OutputTokens = &base
		}
	case OperationResponses:
		if details, _ := usagePayload["input_tokens_details"].(map[string]any); details != nil {
			usage.CacheReadInputTokens = intPointerFromAny(details["cached_tokens"])
		}
		if details, _ := usagePayload["output_tokens_details"].(map[string]any); details != nil {
			usage.ReasoningTokens = intPointerFromAny(details["reasoning_tokens"])
		}
		if input := intPointerFromAny(usagePayload["input_tokens"]); input != nil {
			base := *input - intValue(usage.CacheReadInputTokens) - intValue(usage.CacheCreationInputTokens)
			usage.InputTokens = &base
		}
		if output := intPointerFromAny(usagePayload["output_tokens"]); output != nil {
			base := *output - intValue(usage.ReasoningTokens)
			usage.OutputTokens = &base
		}
	}
	usage.TotalTokens = intPointerFromAny(usagePayload["total_tokens"])
	usage.NormalizationRule = rule
	return normalizeUsage(usage)
}

func extractTokenCountUsage(body []byte) provider.UsageEnvelope {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return provider.UsageEnvelope{}
	}
	usage := provider.UsageEnvelope{InputTokens: intPointerFromAny(payload["input_tokens"]), CacheReadInputTokens: intPointerFromAny(firstValue(payload, "cache_read_input_tokens", "cachedContentTokenCount")), CacheCreationInputTokens: intPointerFromAny(payload["cache_creation_input_tokens"]), NormalizationRule: OperationResponsesInputTokens}
	if total := intPointerFromAny(firstValue(payload, "total_tokens", "totalTokens")); total != nil {
		usage.TotalTokens = total
		if usage.InputTokens == nil {
			usage.InputTokens = total
		}
	}
	return normalizeUsage(usage)
}

func normalizeUsage(usage provider.UsageEnvelope) provider.UsageEnvelope {
	if usage.TotalTokens == nil && (usage.InputTokens != nil || usage.OutputTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil || usage.ReasoningTokens != nil) {
		total := intValue(usage.InputTokens) + intValue(usage.OutputTokens) + intValue(usage.CacheReadInputTokens) + intValue(usage.CacheCreationInputTokens) + intValue(usage.ReasoningTokens)
		usage.TotalTokens = &total
	}
	return usage
}

type overflowEvidence struct {
	code    *string
	message *string
}

func ClassifyOverflowResponse(statusCode int, rawBody []byte, translationMode provider.TranslationMode) provider.OverflowClassification {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusRequestEntityTooLarge && statusCode != http.StatusUnprocessableEntity && statusCode != http.StatusTooManyRequests {
		return provider.OverflowClassification{}
	}
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return provider.OverflowClassification{}
	}
	evidence, ok := overflowEvidenceFromPayload(payload, translationMode)
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

func overflowEvidenceFromPayload(payload map[string]any, translationMode provider.TranslationMode) (overflowEvidence, bool) {
	errorPayload, hasErrorObject := payload["error"].(map[string]any)
	if translationMode != "" && translationMode != provider.TranslationModeNone && !hasErrorObject {
		return overflowEvidence{}, false
	}
	if hasErrorObject {
		evidence := overflowEvidence{code: trimmedStringFromAny(errorPayload["code"])}
		if message := trimmedStringFromAny(firstValue(errorPayload, "message", "detail")); message != nil {
			evidence.message = message
		} else {
			evidence.message = trimmedStringFromAny(firstValue(payload, "message", "detail"))
		}
		return evidence, evidence.code != nil || evidence.message != nil
	}
	evidence := overflowEvidence{code: trimmedStringFromAny(payload["code"]), message: trimmedStringFromAny(firstValue(payload, "detail", "message", "error"))}
	return evidence, evidence.code != nil || evidence.message != nil
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
