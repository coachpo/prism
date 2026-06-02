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
	copyTranslationField(payload, translated, "temperature", "top_p", "seed", "parallel_tool_calls", "store", "metadata", "user", "service_tier", "stream")
	if instructions := trimmedStringFromAny(payload["instructions"]); instructions != nil {
		translated["messages"] = appendChatTranslationMessage(nil, "system", *instructions)
	}
	messages, err := translateResponsesInputToChatMessages(payload["input"])
	if err != nil {
		return nil, err
	}
	translated["messages"] = appendChatTranslationMessages(translated["messages"], messages)
	if tools, err := translateResponsesToolsToChat(payload["tools"]); err != nil {
		return nil, err
	} else if len(tools) > 0 {
		translated["tools"] = tools
	}
	if toolChoice, err := translateResponsesToolChoiceToChat(payload["tool_choice"]); err != nil {
		return nil, err
	} else if toolChoice != nil {
		translated["tool_choice"] = toolChoice
	}
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
	copyTranslationField(payload, translated, "temperature", "top_p", "seed", "parallel_tool_calls", "store", "metadata", "user", "service_tier", "stream")
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
	if tools, err := translateChatToolsToResponses(payload["tools"]); err != nil {
		return nil, err
	} else if len(tools) > 0 {
		translated["tools"] = tools
	}
	if toolChoice, err := translateChatToolChoiceToResponses(payload["tool_choice"]); err != nil {
		return nil, err
	} else if toolChoice != nil {
		translated["tool_choice"] = toolChoice
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
	for _, key := range []string{"response_format", "audio", "logprobs", "top_logprobs", "stream_options", "modalities", "prediction"} {
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
		part, err := translateResponsesImagePartToChat(item)
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{"role": "user", "content": []any{part}}}, nil
	case "function_call":
		toolCall, err := translateResponsesFunctionCallToChat(item)
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{"role": "assistant", "content": "", "tool_calls": []any{toolCall}}}, nil
	case "function_call_output":
		toolMessage, err := translateResponsesFunctionCallOutputToChat(item)
		if err != nil {
			return nil, err
		}
		return []any{toolMessage}, nil
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
				if role != "user" {
					return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_non_user_image")
				}
				translatedPart, err := translateResponsesImagePartToChat(part)
				if err != nil {
					return nil, err
				}
				parts = append(parts, translatedPart)
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

func translateResponsesImagePartToChat(item map[string]any) (map[string]any, error) {
	imageURL := strings.TrimSpace(stringValue(item["image_url"]))
	if imageURL == "" {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_input_image")
	}
	translated := map[string]any{"type": "image_url", "image_url": map[string]any{"url": imageURL}}
	if detail := trimmedStringFromAny(item["detail"]); detail != nil {
		translated["image_url"].(map[string]any)["detail"] = *detail
	}
	return translated, nil
}

func translateResponsesFunctionCallToChat(item map[string]any) (map[string]any, error) {
	name := strings.TrimSpace(stringValue(item["name"]))
	callID := strings.TrimSpace(firstNonEmptyString(item["call_id"], item["id"]))
	if name == "" || callID == "" {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_function_call")
	}
	arguments, err := jsonStringValue(item["arguments"], TranslationModeOpenAIResponsesToChatCompletions, "responses_function_call_arguments")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":   callID,
		"type": "function",
		"function": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}, nil
}

func translateResponsesFunctionCallOutputToChat(item map[string]any) (map[string]any, error) {
	callID := strings.TrimSpace(firstNonEmptyString(item["call_id"], item["tool_call_id"]))
	output, ok := item["output"].(string)
	if callID == "" || !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_function_call_output")
	}
	return map[string]any{"role": "tool", "tool_call_id": callID, "content": output}, nil
}

func translateResponsesToolsToChat(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	tools, ok := value.([]any)
	if !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_tools")
	}
	translated := make([]any, 0, len(tools))
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(tool["type"])) != "function" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_non_function_tool")
		}
		name := strings.TrimSpace(stringValue(tool["name"]))
		if name == "" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_function_tool_name")
		}
		translatedTool := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": stringValue(tool["description"]),
				"parameters":  firstValue(tool, "parameters", "input_schema"),
			},
		}
		if strict, ok := boolPointerFromAny(tool["strict"]); ok {
			translatedTool["function"].(map[string]any)["strict"] = *strict
		}
		translated = append(translated, translatedTool)
	}
	return translated, nil
}

func translateResponsesToolChoiceToChat(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		normalized := strings.TrimSpace(typed)
		if normalized == "" || normalized == "auto" || normalized == "none" {
			return normalized, nil
		}
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) != "function" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_tool_choice")
		}
		name := strings.TrimSpace(firstNonEmptyString(typed["name"], nestedValue(typed, "function", "name")))
		if name == "" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_tool_choice")
		}
		return map[string]any{"type": "function", "function": map[string]any{"name": name}}, nil
	}
	return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIResponsesToChatCompletions, "responses_tool_choice")
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
		content, err := translateChatContentToResponses(message["content"], role)
		if err != nil {
			return nil, err
		}
		items := make([]any, 0, 1)
		if content != nil && valueHasMeaning(content) {
			items = append(items, map[string]any{"type": "message", "role": role, "content": content})
		}
		if role == "assistant" && fieldHasValue(message, "tool_calls") {
			toolCalls, err := translateChatToolCallsToResponses(message["tool_calls"])
			if err != nil {
				return nil, err
			}
			items = append(items, toolCalls...)
		}
		return items, nil
	case "tool":
		toolOutput, err := translateChatToolMessageToResponses(message)
		if err != nil {
			return nil, err
		}
		return []any{toolOutput}, nil
	default:
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_message_role")
	}
}

func translateChatContentToResponses(value any, role string) (any, error) {
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
			case isChatImagePart(part) && role == "user":
				translatedPart, err := translateChatImagePartToResponses(part)
				if err != nil {
					return nil, err
				}
				parts = append(parts, translatedPart)
			default:
				return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part_type")
			}
		}
		return parts, nil
	default:
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_content")
	}
}

func translateChatImagePartToResponses(part map[string]any) (map[string]any, error) {
	image, ok := part["image_url"].(map[string]any)
	if !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_image_part")
	}
	imageURL := strings.TrimSpace(stringValue(image["url"]))
	if imageURL == "" {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_image_part")
	}
	translated := map[string]any{"type": "input_image", "image_url": imageURL}
	detailValue := part["detail"]
	if detailValue == nil {
		detailValue = image["detail"]
	}
	if detail := trimmedStringFromAny(detailValue); detail != nil {
		translated["detail"] = *detail
	}
	return translated, nil
}

func translateChatToolCallsToResponses(value any) ([]any, error) {
	toolCalls, ok := value.([]any)
	if !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_calls")
	}
	translated := make([]any, 0, len(toolCalls))
	for _, rawToolCall := range toolCalls {
		toolCall, ok := rawToolCall.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(toolCall["type"])) != "function" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_non_function_tool_call")
		}
		function, ok := toolCall["function"].(map[string]any)
		if !ok {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_function_tool_call")
		}
		arguments, err := jsonStringValue(function["arguments"], TranslationModeOpenAIChatCompletionsToResponses, "chat_function_tool_call_arguments")
		if err != nil {
			return nil, err
		}
		callID := strings.TrimSpace(stringValue(toolCall["id"]))
		name := strings.TrimSpace(stringValue(function["name"]))
		if callID == "" || name == "" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_function_tool_call")
		}
		translated = append(translated, map[string]any{"type": "function_call", "call_id": callID, "name": name, "arguments": arguments})
	}
	return translated, nil
}

func translateChatToolMessageToResponses(message map[string]any) (map[string]any, error) {
	callID := strings.TrimSpace(firstNonEmptyString(message["tool_call_id"], message["call_id"]))
	content, ok := message["content"].(string)
	if callID == "" || !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_message")
	}
	return map[string]any{"type": "function_call_output", "call_id": callID, "output": content}, nil
}

func translateChatToolsToResponses(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	tools, ok := value.([]any)
	if !ok {
		return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tools")
	}
	translated := make([]any, 0, len(tools))
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(tool["type"])) != "function" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_non_function_tool")
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_function_tool")
		}
		name := strings.TrimSpace(stringValue(function["name"]))
		if name == "" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_function_tool")
		}
		translatedTool := map[string]any{"type": "function", "name": name, "description": stringValue(function["description"]), "parameters": function["parameters"]}
		if strict, ok := boolPointerFromAny(firstValue(function, "strict", "schema_strict")); ok {
			translatedTool["strict"] = *strict
		}
		translated = append(translated, translatedTool)
	}
	return translated, nil
}

func translateChatToolChoiceToResponses(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		normalized := strings.TrimSpace(typed)
		if normalized == "" || normalized == "auto" || normalized == "none" {
			return normalized, nil
		}
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) != "function" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
		}
		function, _ := typed["function"].(map[string]any)
		name := strings.TrimSpace(firstNonEmptyString(typed["name"], firstValue(function, "name")))
		if name == "" {
			return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
		}
		return map[string]any{"type": "function", "name": name}, nil
	}
	return nil, openAIRequestTranslationUnsupportedDomainError(TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
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

func jsonStringValue(value any, mode TranslationMode, reason string) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return "", openAIRequestTranslationUnsupportedDomainError(mode, reason)
		}
		return string(raw), nil
	}
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
