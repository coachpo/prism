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
	requestTranslationUnsupportedErrorCode = "openai_request_translation_unsupported"
	requestTranslationUnsupportedDetail    = "Prism cannot translate this OpenAI request shape for the selected target."
)

func requestTranslationUnsupportedError(mode provider.TranslationMode, reason string) *provider.AdapterError {
	fields := map[string]any{}
	if strings.TrimSpace(string(mode)) != "" {
		fields["translation_mode"] = string(mode)
	}
	if strings.TrimSpace(reason) != "" {
		fields["unsupported_reason"] = strings.TrimSpace(reason)
	}
	return &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: requestTranslationUnsupportedErrorCode, Detail: requestTranslationUnsupportedDetail, Fields: fields}
}

func unsupportedTranslationMode(mode provider.TranslationMode) error {
	return fmt.Errorf("unsupported translation mode %q", mode)
}

func translateRequest(rawBody []byte, mode provider.TranslationMode, targetModelID string) (string, []byte, error) {
	switch mode {
	case provider.TranslationModeOpenAIResponsesToChatCompletions:
		body, err := translateResponsesToChatRequest(rawBody, targetModelID)
		return "/v1/chat/completions", body, err
	case provider.TranslationModeOpenAIChatCompletionsToResponses:
		body, err := translateChatToResponsesRequest(rawBody, targetModelID)
		return "/v1/responses", body, err
	case provider.TranslationModeNone:
		return "", nil, nil
	default:
		return "", nil, unsupportedTranslationMode(mode)
	}
}

func translateResponsesToChatRequest(rawBody []byte, targetModelID string) ([]byte, error) {
	payload, err := decodeOpenAITranslationPayload(rawBody, provider.TranslationModeOpenAIResponsesToChatCompletions)
	if err != nil {
		return nil, err
	}
	if err := rejectResponsesTranslationUnsupportedFields(payload); err != nil {
		return nil, err
	}
	translated := map[string]any{"model": targetModelID}
	copyTranslationField(payload, translated, "temperature", "top_p", "seed", "store", "metadata", "user", "service_tier", "stream")
	toolContext := BuildToolContextFromResponsesPayload(payload)
	if instructions := trimmedStringFromAny(payload["instructions"]); instructions != nil {
		translated["messages"] = appendChatTranslationMessage(nil, "system", *instructions)
	}
	messages, err := translateResponsesInputToChatMessages(payload["input"], toolContext)
	if err != nil {
		return nil, err
	}
	translated["messages"] = appendChatTranslationMessages(translated["messages"], messages)
	if tools := toolContext.ChatTools(); len(tools) > 0 {
		translated["tools"] = mapsFromAnyMaps(tools)
		if toolChoice, ok := translateResponsesToolChoiceToChat(payload["tool_choice"], toolContext); ok {
			translated["tool_choice"] = toolChoice
		}
		copyTranslationField(payload, translated, "parallel_tool_calls")
	}
	if maxOutputTokens := intPointerFromAny(payload["max_output_tokens"]); maxOutputTokens != nil {
		translated["max_completion_tokens"] = *maxOutputTokens
	}
	if effort, err := translateResponsesReasoningEffort(payload["reasoning"]); err != nil {
		return nil, err
	} else if effort != nil {
		translated["reasoning_effort"] = *effort
	}
	return marshalTranslatedOpenAIRequest(translated, provider.TranslationModeOpenAIResponsesToChatCompletions)
}

func translateChatToResponsesRequest(rawBody []byte, targetModelID string) ([]byte, error) {
	payload, err := decodeOpenAITranslationPayload(rawBody, provider.TranslationModeOpenAIChatCompletionsToResponses)
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
	copyChatToolsToResponses(payload, translated)
	return marshalTranslatedOpenAIRequest(translated, provider.TranslationModeOpenAIChatCompletionsToResponses)
}

func decodeOpenAITranslationPayload(rawBody []byte, mode provider.TranslationMode) (map[string]any, error) {
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, requestTranslationUnsupportedError(mode, "invalid_json")
	}
	return payload, nil
}

func rejectResponsesTranslationUnsupportedFields(payload map[string]any) error {
	if fieldHasValue(payload, "previous_response_id") {
		return requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_previous_response_id")
	}
	if fieldHasValue(payload, "conversation") {
		return requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_conversation")
	}
	if fieldHasValue(payload, "include") {
		return requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_include")
	}
	if fieldHasValue(payload, "text") {
		return requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_text")
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
			return requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_reasoning_"+strings.TrimSpace(key))
		}
	}
	return nil
}

func rejectChatTranslationUnsupportedFields(payload map[string]any) error {
	if n := intPointerFromAny(payload["n"]); n != nil && *n > 1 {
		return requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_multi_choice")
	}
	for _, key := range []string{"audio", "logprobs", "top_logprobs", "stream_options", "modalities", "prediction"} {
		if fieldHasValue(payload, key) {
			return requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_"+key)
		}
	}
	return nil
}

func translateResponsesInputToChatMessages(value any, toolContext *ToolContext) ([]any, error) {
	messages := make([]any, 0)
	pendingToolCalls := make([]any, 0)
	var pendingReasoning *string
	flushPendingToolCalls := func() {
		if len(pendingToolCalls) == 0 {
			return
		}
		message := map[string]any{"role": "assistant", "content": nil, "tool_calls": pendingToolCalls}
		if pendingReasoning != nil {
			AppendReasoningContent(message, *pendingReasoning)
			pendingReasoning = nil
		} else {
			AppendReasoningContent(message, "tool call")
		}
		messages = append(messages, message)
		pendingToolCalls = nil
	}
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []any{map[string]any{"role": "user", "content": typed}}, nil
	case map[string]any:
		translated, err := translateResponsesInputItemToChatMessages(typed, toolContext, &pendingToolCalls, &pendingReasoning, flushPendingToolCalls)
		if err != nil {
			return nil, err
		}
		messages = append(messages, translated...)
	case []any:
		for _, item := range typed {
			itemMap, ok := item.(map[string]any)
			if !ok {
				return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_input_item")
			}
			translated, err := translateResponsesInputItemToChatMessages(itemMap, toolContext, &pendingToolCalls, &pendingReasoning, flushPendingToolCalls)
			if err != nil {
				return nil, err
			}
			messages = append(messages, translated...)
		}
	default:
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_input")
	}
	flushPendingToolCalls()
	return collapseSystemMessagesToHead(messages), nil
}

func translateResponsesInputItemToChatMessages(item map[string]any, toolContext *ToolContext, pendingToolCalls *[]any, pendingReasoning **string, flushPendingToolCalls func()) ([]any, error) {
	itemType := strings.ToLower(strings.TrimSpace(stringValue(item["type"])))
	switch itemType {
	case "function_call":
		appendPendingReasoning(pendingReasoning, ExtractReasoningFieldText(item))
		*pendingToolCalls = append(*pendingToolCalls, responsesFunctionCallToChatToolCall(item, toolContext))
		return nil, nil
	case "custom_tool_call":
		appendPendingReasoning(pendingReasoning, ExtractReasoningFieldText(item))
		*pendingToolCalls = append(*pendingToolCalls, responsesCustomToolCallToChatToolCall(item))
		return nil, nil
	case "tool_search_call":
		appendPendingReasoning(pendingReasoning, ExtractReasoningFieldText(item))
		*pendingToolCalls = append(*pendingToolCalls, responsesToolSearchCallToChatToolCall(item))
		return nil, nil
	case "function_call_output", "custom_tool_call_output", "tool_search_output":
		flushPendingToolCalls()
		return []any{responsesToolOutputToChatMessage(item)}, nil
	case "reasoning":
		appendPendingReasoning(pendingReasoning, ExtractReasoningSummaryText(item))
		return nil, nil
	case "input_text", "input_image", "input_file", "input_audio":
		flushPendingToolCalls()
		role := responsesRoleToChatRole(stringValue(item["role"]))
		return []any{map[string]any{"role": role, "content": translateResponsesMessageContentPartsToChat([]any{item}, role)}}, nil
	case "message", "":
		flushPendingToolCalls()
		message, err := translateResponsesMessageToChatMessage(item)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(stringValue(message["role"])) == "assistant" {
			appendPendingReasoning(pendingReasoning, ExtractReasoningFieldText(item))
			if *pendingReasoning != nil {
				AppendReasoningContent(message, **pendingReasoning)
				*pendingReasoning = nil
			}
		}
		return []any{message}, nil
	default:
		flushPendingToolCalls()
		if fieldHasValue(item, "role") || fieldHasValue(item, "content") {
			message, err := translateResponsesMessageToChatMessage(item)
			if err != nil {
				return nil, err
			}
			return []any{message}, nil
		}
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_input_type")
	}
}

func appendPendingReasoning(target **string, reasoning *ReasoningText) {
	if reasoning == nil || strings.TrimSpace(reasoning.Text) == "" {
		return
	}
	text := strings.TrimSpace(reasoning.Text)
	if *target == nil {
		*target = &text
		return
	}
	combined := strings.TrimSpace(**target) + "\n\n" + text
	*target = &combined
}

func responsesFunctionCallToChatToolCall(item map[string]any, toolContext *ToolContext) map[string]any {
	callID := firstNonEmptyString(item["call_id"], item["id"])
	name := stringValue(item["name"])
	chatName := name
	if toolContext != nil {
		chatName = toolContext.ChatNameForResponseFunction(name, stringValue(item["namespace"]))
	}
	return map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": chatName, "arguments": canonicalToolArguments(item["arguments"])}}
}

func responsesCustomToolCallToChatToolCall(item map[string]any) map[string]any {
	callID := firstNonEmptyString(item["call_id"], item["id"])
	return map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": stringValue(item["name"]), "arguments": canonicalJSONString(map[string]any{customToolInputField: item["input"]})}}
}

func responsesToolSearchCallToChatToolCall(item map[string]any) map[string]any {
	callID := firstNonEmptyString(item["call_id"], item["id"])
	return map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": toolSearchProxyName, "arguments": canonicalToolArguments(item["arguments"])}}
}

func responsesToolOutputToChatMessage(item map[string]any) map[string]any {
	content := ""
	if strings.TrimSpace(stringValue(item["type"])) == "function_call_output" {
		content = canonicalToolArguments(item["output"])
	} else {
		content = canonicalJSONString(item)
	}
	return map[string]any{"role": "tool", "tool_call_id": stringValue(item["call_id"]), "content": content}
}

func translateResponsesToolChoiceToChat(value any, toolContext *ToolContext) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, false
		}
		return typed, true
	case map[string]any:
		switch strings.TrimSpace(stringValue(typed["type"])) {
		case "function":
			name := stringValue(typed["name"])
			if function, _ := typed["function"].(map[string]any); function != nil && name == "" {
				name = stringValue(function["name"])
			}
			chatName := name
			if toolContext != nil {
				chatName = toolContext.ChatNameForResponseFunction(name, stringValue(typed["namespace"]))
			}
			return map[string]any{"type": "function", "function": map[string]any{"name": chatName}}, true
		case "custom":
			return map[string]any{"type": "function", "function": map[string]any{"name": stringValue(typed["name"])}}, true
		case "tool_search":
			return map[string]any{"type": "function", "function": map[string]any{"name": toolSearchProxyName}}, true
		default:
			return cloneAnyMap(typed), true
		}
	default:
		return typed, true
	}
}

func mapsFromAnyMaps(values []map[string]any) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func collapseSystemMessagesToHead(messages []any) []any {
	systemParts := make([]string, 0)
	rest := make([]any, 0, len(messages))
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		if strings.TrimSpace(stringValue(message["role"])) == "system" {
			if text := strings.TrimSpace(stringValue(message["content"])); text != "" {
				systemParts = append(systemParts, text)
				continue
			}
		}
		rest = append(rest, raw)
	}
	if len(systemParts) == 0 {
		return rest
	}
	out := []any{map[string]any{"role": "system", "content": strings.Join(systemParts, "\n\n")}}
	return append(out, rest...)
}

func responsesRoleToChatRole(role string) string {
	switch strings.TrimSpace(role) {
	case "system", "developer":
		return "system"
	case "assistant", "tool":
		return strings.TrimSpace(role)
	default:
		return "user"
	}
}

func translateResponsesMessageToChatMessage(item map[string]any) (map[string]any, error) {
	role := strings.TrimSpace(stringValue(item["role"]))
	if role == "" {
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_message_role")
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
				return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_message_part")
			}
			switch strings.ToLower(strings.TrimSpace(stringValue(part["type"]))) {
			case "input_text", "output_text", "text", "refusal", "input_image", "input_file", "input_audio":
				parts = append(parts, part)
			default:
				return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_message_part_type")
			}
		}
		return translateResponsesMessageContentPartsToChat(parts, role), nil
	default:
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_message_content")
	}
}

func translateResponsesMessageContentPartsToChat(parts []any, role string) any {
	contentParts := make([]ResponsesContentPart, 0, len(parts))
	for _, rawPart := range parts {
		part, _ := rawPart.(map[string]any)
		switch strings.ToLower(strings.TrimSpace(stringValue(part["type"]))) {
		case "input_text", "output_text", "text":
			contentParts = append(contentParts, ResponsesContentPart{Kind: ResponsesContentPartKind(strings.TrimSpace(stringValue(part["type"]))), Text: stringValue(part["text"])})
		case "refusal":
			contentParts = append(contentParts, ResponsesContentPart{Kind: ResponsesContentPartRefusal, Refusal: stringValue(part["refusal"])})
		case "input_image":
			contentParts = append(contentParts, ResponsesContentPart{Kind: ResponsesContentPartInputImage, ImageURL: responseImagePartToChatImageURL(part)})
		case "input_file":
			if file := responseFilePartToChatFile(part); len(file) > 0 {
				contentParts = append(contentParts, ResponsesContentPart{Kind: ResponsesContentPartInputFile, File: file})
			}
		case "input_audio":
			if audio, _ := part["input_audio"].(map[string]any); len(audio) > 0 {
				contentParts = append(contentParts, ResponsesContentPart{Kind: ResponsesContentPartInputAudio, InputAudio: audio})
			}
		}
	}
	content := ResponsesContentPartsToChatContent(contentParts)
	if role != "user" {
		if text := strings.TrimSpace(stringValue(content)); text != "" {
			return text
		}
	}
	return content
}

func responseImagePartToChatImageURL(part map[string]any) map[string]any {
	switch image := part["image_url"].(type) {
	case map[string]any:
		return cloneAnyMap(image)
	case string:
		return map[string]any{"url": image}
	default:
		if url := stringValue(part["url"]); url != "" {
			return map[string]any{"url": url}
		}
	}
	return nil
}

func responseFilePartToChatFile(part map[string]any) map[string]any {
	file := map[string]any{}
	for _, key := range []string{"file_id", "file_data", "filename"} {
		if value, ok := part[key]; ok && value != nil {
			file[key] = value
		}
	}
	return file
}

func translateResponsesReasoningEffort(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	reasoning, ok := value.(map[string]any)
	if !ok {
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_reasoning")
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
			return nil, nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_message")
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
				return "", requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_instruction_content")
			}
			parts = append(parts, stringValue(part["text"]))
		}
		return strings.Join(parts, "\n\n"), nil
	default:
		return "", requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_instruction_content")
	}
}

func translateChatMessagesToResponsesInput(messages []any) ([]any, error) {
	items := make([]any, 0, len(messages))
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_message")
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
		content, err := translateChatContentToResponses(message["content"])
		if err != nil {
			return nil, err
		}
		items := make([]any, 0, 1)
		if reasoning := ExtractReasoningFieldText(message); reasoning != nil && role == "assistant" {
			items = append(items, map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": reasoning.Text}}})
		}
		if content != nil && valueHasMeaning(content) {
			messageItem := map[string]any{"type": "message", "role": role, "content": content}
			items = append(items, messageItem)
		}
		if role == "assistant" && fieldHasValue(message, "tool_calls") {
			toolItems, err := translateChatToolCallsToResponses(message["tool_calls"])
			if err != nil {
				return nil, err
			}
			items = append(items, toolItems...)
		}
		if role == "assistant" && fieldHasValue(message, "function_call") {
			items = append(items, translateChatLegacyFunctionCallToResponses(message["function_call"]))
		}
		return items, nil
	case "tool":
		return []any{map[string]any{"type": "function_call_output", "call_id": stringValue(message["tool_call_id"]), "output": message["content"]}}, nil
	default:
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_message_role")
	}
}

func translateChatToolCallsToResponses(value any) ([]any, error) {
	toolCalls, ok := value.([]any)
	if !ok {
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_calls")
	}
	items := make([]any, 0, len(toolCalls))
	for _, rawToolCall := range toolCalls {
		toolCall, ok := rawToolCall.(map[string]any)
		if !ok {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_call")
		}
		function, _ := toolCall["function"].(map[string]any)
		items = append(items, map[string]any{"type": "function_call", "status": "completed", "call_id": firstNonEmptyString(toolCall["id"], toolCall["call_id"]), "name": stringValue(function["name"]), "arguments": canonicalToolArguments(function["arguments"])})
	}
	return items, nil
}

func translateChatLegacyFunctionCallToResponses(value any) map[string]any {
	function, _ := value.(map[string]any)
	return map[string]any{"type": "function_call", "status": "completed", "call_id": firstNonEmptyString(function["id"], "call_0"), "name": stringValue(function["name"]), "arguments": canonicalToolArguments(function["arguments"])}
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
				return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part")
			}
			switch {
			case isChatTextPart(part):
				parts = append(parts, map[string]any{"type": "input_text", "text": stringValue(part["text"])})
			case isChatImagePart(part):
				imageURL, _ := part["image_url"].(map[string]any)
				parts = append(parts, map[string]any{"type": "input_image", "image_url": imageURL})
			case isChatFilePart(part):
				file, _ := part["file"].(map[string]any)
				filePart := map[string]any{"type": "input_file"}
				copyTranslationField(file, filePart, "file_id", "file_data", "filename")
				parts = append(parts, filePart)
			case isChatAudioPart(part):
				parts = append(parts, map[string]any{"type": "input_audio", "input_audio": part["input_audio"]})
			default:
				return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_content_part_type")
			}
		}
		return parts, nil
	default:
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_content")
	}
}

func isChatTextPart(part map[string]any) bool {
	return strings.TrimSpace(stringValue(part["type"])) == "text"
}

func isChatImagePart(part map[string]any) bool {
	return strings.TrimSpace(stringValue(part["type"])) == "image_url"
}

func isChatFilePart(part map[string]any) bool {
	return strings.TrimSpace(stringValue(part["type"])) == "file"
}

func isChatAudioPart(part map[string]any) bool {
	return strings.TrimSpace(stringValue(part["type"])) == "input_audio"
}

func copyChatToolsToResponses(source map[string]any, target map[string]any) {
	if tools, ok := translateChatToolsToResponses(source["tools"]); ok {
		target["tools"] = tools
	}
	if choice, ok := translateChatToolChoiceToResponses(source["tool_choice"]); ok {
		target["tool_choice"] = choice
	}
	copyTranslationField(source, target, "parallel_tool_calls", "response_format")
}

func translateChatToolsToResponses(value any) ([]any, bool) {
	tools, ok := value.([]any)
	if !ok || len(tools) == 0 {
		return nil, false
	}
	out := make([]any, 0, len(tools))
	for _, rawTool := range tools {
		tool, _ := rawTool.(map[string]any)
		if strings.TrimSpace(stringValue(tool["type"])) == "function" {
			function, _ := tool["function"].(map[string]any)
			responseTool := cloneAnyMap(function)
			responseTool["type"] = "function"
			out = append(out, responseTool)
		}
	}
	return out, len(out) > 0
}

func translateChatToolChoiceToResponses(value any) (any, bool) {
	switch typed := value.(type) {
	case nil:
		return nil, false
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil, false
		}
		return typed, true
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) == "function" {
			function, _ := typed["function"].(map[string]any)
			return map[string]any{"type": "function", "name": stringValue(function["name"])}, true
		}
		return cloneAnyMap(typed), true
	default:
		return typed, true
	}
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

func marshalTranslatedOpenAIRequest(payload map[string]any, mode provider.TranslationMode) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, requestTranslationUnsupportedError(mode, "marshal_failure")
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

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func trimmedStringFromAny(value any) *string {
	trimmed := strings.TrimSpace(stringValue(value))
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func intPointerFromAny(value any) *int {
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

func firstValue(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
