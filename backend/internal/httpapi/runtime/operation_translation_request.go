package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
)

func translateOpenAIRequest(rawBody []byte, mode TranslationMode, targetModelID string) (string, []byte, error) {
	switch mode {
	case TranslationModeOpenAIResponsesToChatCompletions:
		body, err := translateOpenAIResponsesToChatRequest(rawBody, targetModelID)
		return "/v1/chat/completions", body, err
	case TranslationModeOpenAIChatCompletionsToResponses:
		body, err := translateOpenAIChatToResponsesRequest(rawBody, targetModelID)
		return "/v1/responses", body, err
	case TranslationModeNone:
		return "", nil, nil
	default:
		return "", nil, unsupportedTranslationModeError(mode)
	}
}

func translateOpenAIResponsesToChatRequest(rawBody []byte, targetModelID string) ([]byte, error) {
	payload, err := decodeOpenAITranslationPayload(rawBody, TranslationModeOpenAIResponsesToChatCompletions)
	if err != nil {
		return nil, err
	}
	if err := rejectResponsesTranslationUnsupportedFields(payload); err != nil {
		return nil, err
	}
	translated := map[string]any{"model": targetModelID}
	copyTranslationField(payload, translated, "temperature", "top_p", "seed", "store", "metadata", "user", "service_tier", "stream")
	if instructions := trimmedStringFromAny(payload["instructions"]); instructions != nil {
		translated["messages"] = appendChatTranslationMessage(nil, "system", *instructions)
	}
	messages, err := translateResponsesInputToChatMessages(payload["input"])
	if err != nil {
		return nil, err
	}
	translated["messages"] = appendChatTranslationMessages(translated["messages"], messages)
	if maxOutputTokens := intPointerFromAny(payload["max_output_tokens"]); maxOutputTokens != nil {
		translated["max_completion_tokens"] = *maxOutputTokens
	}
	if effort, err := translateResponsesReasoningEffort(payload["reasoning"]); err != nil {
		return nil, err
	} else if effort != nil {
		translated["reasoning_effort"] = *effort
	}
	return marshalTranslatedOpenAIRequest(translated, TranslationModeOpenAIResponsesToChatCompletions)
}

func translateOpenAIChatToResponsesRequest(rawBody []byte, targetModelID string) ([]byte, error) {
	payload, err := decodeOpenAITranslationPayload(rawBody, TranslationModeOpenAIChatCompletionsToResponses)
	if err != nil {
		return nil, err
	}
	if err := rejectChatTranslationUnsupportedFields(payload); err != nil {
		return nil, err
	}
	translated := map[string]any{"model": targetModelID}
	copyTranslationField(payload, translated, "temperature", "top_p", "seed", "store", "metadata", "user", "service_tier", "stream")
	messagesValue, _ := payload["messages"].([]any)
	instructions, remainingMessages, err := extractLeadingChatInstructions(messagesValue)
	if err != nil {
		return nil, err
	}
	if instructions != nil {
		translated["instructions"] = *instructions
	}
	inputItems, err := translateChatMessagesToResponsesInput(remainingMessages)
	if err != nil {
		return nil, err
	}
	if len(inputItems) > 0 {
		translated["input"] = inputItems
	}
	if maxCompletionTokens := intPointerFromAny(payload["max_completion_tokens"]); maxCompletionTokens != nil {
		translated["max_output_tokens"] = *maxCompletionTokens
	} else if maxTokens := intPointerFromAny(payload["max_tokens"]); maxTokens != nil {
		translated["max_output_tokens"] = *maxTokens
	}
	if effort := trimmedStringFromAny(payload["reasoning_effort"]); effort != nil {
		translated["reasoning"] = map[string]any{"effort": *effort}
	}
	return marshalTranslatedOpenAIRequest(translated, TranslationModeOpenAIChatCompletionsToResponses)
}

func decodeOpenAITranslationPayload(rawBody []byte, mode TranslationMode) (map[string]any, error) {
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, openAIRequestTranslationUnsupportedDomainError(mode, "invalid_json")
	}
	return payload, nil
}

func rejectResponsesTranslationUnsupportedFields(payload map[string]any) error {
	if fieldHasValue(payload, "previous_response_id") {
		return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_previous_response_id")
	}
	if fieldHasValue(payload, "conversation") {
		return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_conversation")
	}
	if fieldHasValue(payload, "include") {
		return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_include")
	}
	if fieldHasValue(payload, "text") {
		return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_text")
	}
	for _, key := range []string{"tools", "tool_choice", "parallel_tool_calls"} {
		if fieldHasValue(payload, key) {
			return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_"+key)
		}
	}
	return rejectUnsupportedResponsesReasoning(payload["reasoning"])
}

func rejectUnsupportedResponsesReasoning(value any) error {
	reasoning, ok := value.(map[string]any)
	if !ok || len(reasoning) == 0 {
		return nil
	}
	for key, item := range reasoning {
		if strings.TrimSpace(key) == "effort" {
			continue
		}
		if valueHasMeaning(item) {
			return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_reasoning_"+strings.TrimSpace(key))
		}
	}
	return nil
}

func rejectChatTranslationUnsupportedFields(payload map[string]any) error {
	if n := intPointerFromAny(payload["n"]); n != nil && *n > 1 {
		return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_multi_choice")
	}
	for _, key := range []string{"response_format", "audio", "logprobs", "top_logprobs", "stream_options", "modalities", "prediction", "tools", "tool_choice", "parallel_tool_calls"} {
		if fieldHasValue(payload, key) {
			return openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_"+key)
		}
	}
	return nil
}

func translateResponsesInputToChatMessages(value any) ([]any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []any{map[string]any{"role": "user", "content": typed}}, nil
	case []any:
		messages := make([]any, 0, len(typed))
		for _, item := range typed {
			translated, err := translateResponsesInputItemToChatMessages(item)
			if err != nil {
				return nil, err
			}
			messages = append(messages, translated...)
		}
		return messages, nil
	default:
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_input")
	}
}

func translateResponsesInputItemToChatMessages(value any) ([]any, error) {
	item, ok := value.(map[string]any)
	if !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_input_item")
	}
	switch strings.ToLower(strings.TrimSpace(stringValue(item["type"]))) {
	case "message":
		message, err := translateResponsesMessageToChatMessage(item)
		if err != nil {
			return nil, err
		}
		return []any{message}, nil
	case "input_text":
		return []any{map[string]any{"role": "user", "content": stringValue(item["text"])}}, nil
	case "input_image":
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_input_image")
	case "function_call":
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_function_call")
	case "function_call_output":
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_function_call_output")
	default:
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_input_type")
	}
}

func translateResponsesMessageToChatMessage(item map[string]any) (map[string]any, error) {
	role := strings.TrimSpace(stringValue(item["role"]))
	if role == "" {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_message_role")
	}
	content, err := translateResponsesMessageContentToChat(item["content"], role)
	if err != nil {
		return nil, err
	}
	return map[string]any{"role": role, "content": content}, nil
}

func translateResponsesMessageContentToChat(value any, role string) (any, error) {
	switch typed := value.(type) {
	case nil, string:
		return typed, nil
	case []any:
		parts := make([]any, 0, len(typed))
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_message_part")
			}
			switch strings.ToLower(strings.TrimSpace(stringValue(part["type"]))) {
			case "input_text", "output_text", "text":
				parts = append(parts, map[string]any{"type": "text", "text": stringValue(part["text"])})
			case "input_image":
				return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_input_image")
			default:
				return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_message_part_type")
			}
		}
		if len(parts) == 1 {
			if textPart, ok := parts[0].(map[string]any); ok && len(textPart) == 2 && textPart["type"] == "text" && role != "user" {
				return stringValue(textPart["text"]), nil
			}
		}
		return parts, nil
	default:
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_message_content")
	}
}

func translateResponsesReasoningEffort(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	reasoning, ok := value.(map[string]any)
	if !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_reasoning")
	}
	if effort := trimmedStringFromAny(reasoning["effort"]); effort != nil {
		return effort, nil
	}
	return nil, nil
}

func extractLeadingChatInstructions(messages []any) (*string, []any, error) {
	if len(messages) == 0 {
		return nil, nil, nil
	}
	instructionParts := make([]string, 0)
	index := 0
	for ; index < len(messages); index++ {
		message, ok := messages[index].(map[string]any)
		if !ok {
			return nil, nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_message")
		}
		role := strings.TrimSpace(stringValue(message["role"]))
		if role != "system" && role != "developer" {
			break
		}
		text, err := extractChatTextContent(message["content"])
		if err != nil {
			return nil, nil, err
		}
		instructionParts = append(instructionParts, text)
	}
	if len(instructionParts) == 0 {
		return nil, messages, nil
	}
	combined := strings.Join(instructionParts, "\n\n")
	return &combined, messages[index:], nil
}

func extractChatTextContent(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case []any:
		parts := make([]string, 0, len(typed))
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok || !isChatTextPart(part) {
				return "", openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_instruction_content")
			}
			parts = append(parts, stringValue(part["text"]))
		}
		return strings.Join(parts, "\n\n"), nil
	default:
		return "", openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_instruction_content")
	}
}

func translateChatMessagesToResponsesInput(messages []any) ([]any, error) {
	items := make([]any, 0, len(messages))
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_message")
		}
		translated, err := translateChatMessageToResponsesInput(message)
		if err != nil {
			return nil, err
		}
		items = append(items, translated...)
	}
	return items, nil
}

func translateChatMessageToResponsesInput(message map[string]any) ([]any, error) {
	role := strings.TrimSpace(stringValue(message["role"]))
	switch role {
	case "user", "assistant", "system", "developer":
		if fieldHasValue(message, "function_call") {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_legacy_function_call")
		}
		content, err := translateChatContentToResponses(message["content"])
		if err != nil {
			return nil, err
		}
		items := make([]any, 0, 1)
		if content != nil && valueHasMeaning(content) {
			items = append(items, map[string]any{"type": "message", "role": role, "content": content})
		}
		if role == "assistant" && fieldHasValue(message, "tool_calls") {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_calls")
		}
		return items, nil
	case "tool":
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_message")
	default:
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_message_role")
	}
}

func translateChatContentToResponses(value any) (any, error) {
	switch typed := value.(type) {
	case nil, string:
		return typed, nil
	case []any:
		parts := make([]any, 0, len(typed))
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part")
			}
			switch {
			case isChatTextPart(part):
				parts = append(parts, map[string]any{"type": "input_text", "text": stringValue(part["text"])})
			case isChatImagePart(part):
				return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_image_part")
			default:
				return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part_type")
			}
		}
		return parts, nil
	default:
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content")
	}
}

func isChatTextPart(part map[string]any) bool {
	return strings.TrimSpace(stringValue(part["type"])) == "text"
}

func isChatImagePart(part map[string]any) bool {
	return strings.TrimSpace(stringValue(part["type"])) == "image_url"
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(stringValue(value)); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func copyTranslationField(source map[string]any, target map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := source[key]; ok && value != nil {
			target[key] = value
		}
	}
}

func appendChatTranslationMessage(existing any, role string, content string) []any {
	messages, _ := existing.([]any)
	return append(messages, map[string]any{"role": role, "content": content})
}

func appendChatTranslationMessages(existing any, messages []any) []any {
	current, _ := existing.([]any)
	return append(current, messages...)
}

func marshalTranslatedOpenAIRequest(payload map[string]any, mode TranslationMode) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, openAIRequestTranslationUnsupportedDomainError(mode, "marshal_failure")
	}
	return raw, nil
}

func fieldHasValue(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	return valueHasMeaning(value)
}

func valueHasMeaning(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}
