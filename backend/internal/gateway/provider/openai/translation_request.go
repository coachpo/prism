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
	path, body, _, err := translateRequestWithLoss(rawBody, mode, targetModelID)
	return path, body, err
}

func translateRequestWithLoss(rawBody []byte, mode provider.TranslationMode, targetModelID string) (string, []byte, *provider.TranslationLoss, error) {
	switch mode {
	case provider.TranslationModeOpenAIResponsesToChatCompletions:
		body, loss, err := translateResponsesToChatRequest(rawBody, targetModelID)
		return "/v1/chat/completions", body, loss, err
	case provider.TranslationModeOpenAIChatCompletionsToResponses:
		body, loss, err := translateChatToResponsesRequest(rawBody, targetModelID)
		return "/v1/responses", body, loss, err
	case provider.TranslationModeNone:
		return "", nil, nil, nil
	default:
		return "", nil, nil, unsupportedTranslationMode(mode)
	}
}

func translateResponsesToChatRequest(rawBody []byte, targetModelID string) ([]byte, *provider.TranslationLoss, error) {
	payload, err := decodeOpenAITranslationPayload(rawBody, provider.TranslationModeOpenAIResponsesToChatCompletions)
	if err != nil {
		return nil, nil, err
	}
	policy, err := applyResponsesToChatFieldPolicy(payload)
	if err != nil {
		return nil, nil, err
	}
	translated := map[string]any{"model": targetModelID}
	copyTranslationField(payload, translated, "temperature", "top_p", "seed", "store", "metadata", "user", "service_tier", "stream")
	if boolValue(payload["stream"]) {
		translated["stream_options"] = map[string]any{"include_usage": true}
	}
	if responseFormat := translateResponsesTextFormatToChat(payload["text"]); responseFormat != nil {
		translated["response_format"] = responseFormat
	}
	toolContext := BuildToolContextFromResponsesPayload(payload)
	translatedTools := toolContext.ChatTools()
	policy.droppedFields = append(policy.droppedFields, droppedResponsesToolFields(payload)...)
	if instructions := trimmedStringFromAny(payload["instructions"]); instructions != nil {
		translated["messages"] = appendChatTranslationMessage(nil, "system", *instructions)
	}
	messages, err := translateResponsesInputToChatMessages(payload["input"], toolContext)
	if err != nil {
		return nil, nil, err
	}
	translated["messages"] = appendChatTranslationMessages(translated["messages"], messages)
	if len(translatedTools) > 0 {
		translated["tools"] = mapsFromAnyMaps(translatedTools)
		if toolChoice, ok := translateResponsesToolChoiceToChat(payload["tool_choice"], toolContext); ok {
			translated["tool_choice"] = toolChoice
		} else if fieldHasValue(payload, "tool_choice") {
			policy.droppedFields = append(policy.droppedFields, "responses_tool_choice")
		}
		copyTranslationField(payload, translated, "parallel_tool_calls")
	} else {
		if fieldHasValue(payload, "tool_choice") {
			policy.droppedFields = append(policy.droppedFields, "responses_tool_choice")
		}
		if fieldHasValue(payload, "parallel_tool_calls") {
			policy.droppedFields = append(policy.droppedFields, "responses_parallel_tool_calls")
		}
	}
	if maxOutputTokens := intPointerFromAny(payload["max_output_tokens"]); maxOutputTokens != nil {
		translated["max_completion_tokens"] = *maxOutputTokens
	}
	if effort, err := translateResponsesReasoningEffort(payload["reasoning"]); err != nil {
		return nil, nil, err
	} else if effort != nil {
		translated["reasoning_effort"] = *effort
	}
	if policy.requiresRunnableResidualInput && !chatRequestHasRunnableResidualInput(translated) {
		return nil, nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_stateful_continuation_without_runnable_input")
	}
	body, err := marshalTranslatedOpenAIRequest(translated, provider.TranslationModeOpenAIResponsesToChatCompletions)
	return body, translationLossFromFieldPolicy(policy.droppedFields, policy.mappedFields), err
}

func translateChatToResponsesRequest(rawBody []byte, targetModelID string) ([]byte, *provider.TranslationLoss, error) {
	payload, err := decodeOpenAITranslationPayload(rawBody, provider.TranslationModeOpenAIChatCompletionsToResponses)
	if err != nil {
		return nil, nil, err
	}
	if err := rejectChatTranslationUnsupportedFields(payload); err != nil {
		return nil, nil, err
	}
	fieldLoss := buildChatToResponsesFieldLoss(payload)
	translated := map[string]any{"model": targetModelID}
	copyTranslationField(payload, translated, "temperature", "top_p", "seed", "store", "metadata", "user", "service_tier", "stream")
	messagesValue, _ := payload["messages"].([]any)
	instructions, remainingMessages, err := extractLeadingChatInstructions(messagesValue)
	if err != nil {
		return nil, nil, err
	}
	if instructions != nil {
		translated["instructions"] = *instructions
	}
	inputItems, err := translateChatMessagesToResponsesInput(remainingMessages)
	if err != nil {
		return nil, nil, err
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
	if responseFormat, err := translateChatResponseFormatToResponses(payload["response_format"]); err != nil {
		return nil, nil, err
	} else if responseFormat != nil {
		translated["response_format"] = responseFormat
	}
	tools, err := translateChatToolsToResponsesRequest(payload["tools"])
	if err != nil {
		return nil, nil, err
	}
	if len(tools) > 0 {
		translated["tools"] = tools
	}
	if toolChoice, err := translateChatToolChoiceToResponsesRequest(payload["tool_choice"], tools); err != nil {
		return nil, nil, err
	} else if toolChoice != nil {
		translated["tool_choice"] = toolChoice
	}
	body, err := marshalTranslatedOpenAIRequest(translated, provider.TranslationModeOpenAIChatCompletionsToResponses)
	return body, translationLossFromFieldPolicy(fieldLoss.droppedFields, fieldLoss.mappedFields), err
}

func translationLossFromFieldPolicy(droppedFields []string, mappedFields []string) *provider.TranslationLoss {
	dropped := normalizedTranslationLossFields(droppedFields)
	mapped := normalizedTranslationLossFields(mappedFields)
	if len(dropped) == 0 && len(mapped) == 0 {
		return nil
	}
	return &provider.TranslationLoss{DroppedFields: dropped, MappedFields: mapped}
}

func normalizedTranslationLossFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
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

type responsesToChatFieldPolicy struct {
	droppedFields                 []string
	mappedFields                  []string
	requiresRunnableResidualInput bool
}

type chatToResponsesFieldLoss struct {
	droppedFields []string
	mappedFields  []string
}

func buildChatToResponsesFieldLoss(payload map[string]any) chatToResponsesFieldLoss {
	loss := chatToResponsesFieldLoss{}
	for _, key := range []string{"logprobs", "top_logprobs", "stream_options"} {
		if fieldHasValue(payload, key) {
			loss.droppedFields = append(loss.droppedFields, "chat_"+key)
		}
	}
	if fieldHasValue(payload, "max_completion_tokens") {
		loss.mappedFields = append(loss.mappedFields, "chat_max_completion_tokens")
	} else if fieldHasValue(payload, "max_tokens") {
		loss.mappedFields = append(loss.mappedFields, "chat_max_tokens")
	}
	for _, key := range []string{"reasoning_effort", "response_format", "tools", "tool_choice"} {
		if fieldHasValue(payload, key) {
			loss.mappedFields = append(loss.mappedFields, "chat_"+key)
		}
	}
	return loss
}

func applyResponsesToChatFieldPolicy(payload map[string]any) (responsesToChatFieldPolicy, error) {
	policy := responsesToChatFieldPolicy{}
	allowedTopLevel := map[string]struct{}{
		"model": {}, "instructions": {}, "input": {}, "tools": {}, "tool_choice": {}, "parallel_tool_calls": {}, "max_output_tokens": {},
		"temperature": {}, "top_p": {}, "seed": {}, "store": {}, "metadata": {}, "user": {}, "service_tier": {}, "stream": {},
		"include": {}, "text": {}, "previous_response_id": {}, "conversation": {}, "reasoning": {},
	}
	for key, value := range payload {
		if _, ok := allowedTopLevel[key]; !ok && valueHasMeaning(value) {
			return policy, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_unknown_field")
		}
	}
	if fieldHasValue(payload, "include") {
		policy.droppedFields = append(policy.droppedFields, "responses_include")
	}
	if fieldHasValue(payload, "previous_response_id") {
		policy.droppedFields = append(policy.droppedFields, "responses_previous_response_id")
		policy.requiresRunnableResidualInput = true
	}
	if fieldHasValue(payload, "conversation") {
		policy.droppedFields = append(policy.droppedFields, "responses_conversation")
		policy.requiresRunnableResidualInput = true
	}
	textPolicy, err := applyResponsesTextPolicy(payload["text"])
	if err != nil {
		return policy, err
	}
	policy.droppedFields = append(policy.droppedFields, textPolicy.droppedFields...)
	policy.mappedFields = append(policy.mappedFields, textPolicy.mappedFields...)
	reasoningPolicy, err := applyResponsesReasoningPolicy(payload["reasoning"])
	if err != nil {
		return policy, err
	}
	policy.droppedFields = append(policy.droppedFields, reasoningPolicy.droppedFields...)
	policy.mappedFields = append(policy.mappedFields, reasoningPolicy.mappedFields...)
	return policy, nil
}

func droppedResponsesToolFields(payload map[string]any) []string {
	tools, ok := payload["tools"].([]any)
	if !ok {
		return nil
	}
	dropped := make([]string, 0)
	for index, rawTool := range tools {
		context := NewToolContext()
		if tool, _ := rawTool.(map[string]any); tool != nil {
			context.AddResponseTool(tool)
		} else if name := strings.TrimSpace(stringValue(rawTool)); name != "" {
			context.AddResponseTool(map[string]any{"type": string(ToolKindCustom), "name": name})
		}
		if len(context.ChatTools()) == 0 {
			dropped = append(dropped, fmt.Sprintf("responses_tools.%d", index))
		}
	}
	return dropped
}

func applyResponsesTextPolicy(value any) (responsesToChatFieldPolicy, error) {
	policy := responsesToChatFieldPolicy{}
	if value == nil {
		return policy, nil
	}
	text, ok := value.(map[string]any)
	if !ok {
		if valueHasMeaning(value) {
			return policy, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_text")
		}
		return policy, nil
	}
	for key, item := range text {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "format" {
			if valueHasMeaning(item) {
				if _, err := validateResponsesTextFormat(item); err != nil {
					return policy, err
				}
				policy.mappedFields = append(policy.mappedFields, "responses_text.format")
			}
			continue
		}
		if valueHasMeaning(item) {
			policy.droppedFields = append(policy.droppedFields, "responses_text."+trimmedKey)
		}
	}
	return policy, nil
}

func applyResponsesReasoningPolicy(value any) (responsesToChatFieldPolicy, error) {
	policy := responsesToChatFieldPolicy{}
	if value == nil {
		return policy, nil
	}
	reasoning, ok := value.(map[string]any)
	if !ok {
		if valueHasMeaning(value) {
			return policy, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_reasoning")
		}
		return policy, nil
	}
	for key, item := range reasoning {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "effort" {
			if valueHasMeaning(item) {
				policy.mappedFields = append(policy.mappedFields, "responses_reasoning.effort")
			}
			continue
		}
		if valueHasMeaning(item) {
			policy.droppedFields = append(policy.droppedFields, "responses_reasoning."+trimmedKey)
		}
	}
	return policy, nil
}

func translateResponsesTextFormatToChat(value any) any {
	format, err := validateResponsesTextFormat(nestedAny(value, "format"))
	if err != nil {
		return nil
	}
	if format == nil {
		return nil
	}
	return format
}

func validateResponsesTextFormat(value any) (map[string]any, error) {
	if value == nil || !valueHasMeaning(value) {
		return nil, nil
	}
	format, ok := value.(map[string]any)
	if !ok {
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_text_format")
	}
	formatType := strings.TrimSpace(stringValue(format["type"]))
	switch formatType {
	case "json_schema":
		if !fieldHasValue(format, "json_schema") || hasMeaningfulKeysOutside(format, "type", "json_schema") {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_text_format")
		}
		return map[string]any{"type": "json_schema", "json_schema": cloneJSONValue(format["json_schema"])}, nil
	case "json_object":
		if hasMeaningfulKeysOutside(format, "type") {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_text_format")
		}
		return map[string]any{"type": "json_object"}, nil
	default:
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIResponsesToChatCompletions, "responses_text_format")
	}
}

func chatRequestHasRunnableResidualInput(payload map[string]any) bool {
	if messages, _ := payload["messages"].([]any); len(messages) > 0 {
		for _, rawMessage := range messages {
			message, _ := rawMessage.(map[string]any)
			if chatMessageHasRunnableContent(message) {
				return true
			}
		}
	}
	if tools, _ := payload["tools"].([]any); len(tools) > 0 {
		return true
	}
	return false
}

func chatMessageHasRunnableContent(message map[string]any) bool {
	if message == nil {
		return false
	}
	if valueHasMeaning(message["content"]) || fieldHasValue(message, "tool_calls") || fieldHasValue(message, "tool_call_id") || fieldHasValue(message, "function_call") {
		return true
	}
	return false
}

func hasMeaningfulKeysOutside(payload map[string]any, allowedKeys ...string) bool {
	allowed := map[string]struct{}{}
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}
	for key, value := range payload {
		if _, ok := allowed[key]; !ok && valueHasMeaning(value) {
			return true
		}
	}
	return false
}

func nestedAny(value any, key string) any {
	payload, _ := value.(map[string]any)
	if payload == nil {
		return nil
	}
	return payload[key]
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneJSONValue(item))
		}
		return out
	default:
		return typed
	}
}

func rejectChatTranslationUnsupportedFields(payload map[string]any) error {
	if n := intPointerFromAny(payload["n"]); n != nil && *n > 1 {
		return requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_multi_choice")
	}
	for _, key := range []string{"audio", "modalities", "prediction"} {
		if fieldHasValue(payload, key) {
			return requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_"+key)
		}
	}
	allowedTopLevel := map[string]struct{}{
		"model":                 {},
		"messages":              {},
		"tools":                 {},
		"tool_choice":           {},
		"max_completion_tokens": {},
		"max_tokens":            {},
		"temperature":           {},
		"top_p":                 {},
		"seed":                  {},
		"store":                 {},
		"metadata":              {},
		"user":                  {},
		"service_tier":          {},
		"stream":                {},
		"reasoning_effort":      {},
		"response_format":       {},
		"n":                     {},
		"logprobs":              {},
		"top_logprobs":          {},
		"stream_options":        {},
		"audio":                 {},
		"modalities":            {},
		"prediction":            {},
	}
	for key, value := range payload {
		if _, ok := allowedTopLevel[key]; !ok && valueHasMeaning(value) {
			return requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_unknown_field")
		}
	}
	return nil
}

func translateChatResponseFormatToResponses(value any) (any, error) {
	if value == nil || !valueHasMeaning(value) {
		return nil, nil
	}
	format, ok := value.(map[string]any)
	if !ok {
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_response_format")
	}
	formatType := strings.TrimSpace(stringValue(format["type"]))
	switch formatType {
	case "json_schema":
		if !fieldHasValue(format, "json_schema") {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_response_format")
		}
		return cloneAnyMap(format), nil
	case "json_object":
		if hasMeaningfulKeysOutside(format, "type") {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_response_format")
		}
		return map[string]any{"type": "json_object"}, nil
	default:
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_response_format")
	}
}

func translateChatToolsToResponsesRequest(value any) ([]any, error) {
	if value == nil || !valueHasMeaning(value) {
		return nil, nil
	}
	tools, ok := value.([]any)
	if !ok {
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tools")
	}
	out := make([]any, 0, len(tools))
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok || hasMeaningfulKeysOutside(tool, "type", "function") {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tools")
		}
		if strings.TrimSpace(stringValue(tool["type"])) != "function" {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tools")
		}
		function, ok := tool["function"].(map[string]any)
		if !ok || !valueHasMeaning(function["name"]) {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tools")
		}
		translatedTool := cloneAnyMap(function)
		translatedTool["type"] = "function"
		out = append(out, translatedTool)
	}
	return out, nil
}

func translateChatToolChoiceToResponsesRequest(value any, tools []any) (any, error) {
	if value == nil {
		return nil, nil
	}
	availableTools := map[string]struct{}{}
	for _, rawTool := range tools {
		tool, _ := rawTool.(map[string]any)
		name := strings.TrimSpace(stringValue(tool["name"]))
		if name != "" {
			availableTools[name] = struct{}{}
		}
	}
	switch typed := value.(type) {
	case string:
		switch strings.TrimSpace(typed) {
		case "":
			return nil, nil
		case "auto", "none", "required":
			return typed, nil
		default:
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
		}
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) != "function" {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
		}
		name := stringValue(typed["name"])
		if function, _ := typed["function"].(map[string]any); function != nil && name == "" {
			name = stringValue(function["name"])
		}
		if name == "" {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
		}
		if _, ok := availableTools[name]; !ok {
			return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
		}
		return map[string]any{"type": "function", "name": name}, nil
	default:
		return nil, requestTranslationUnsupportedError(provider.TranslationModeOpenAIChatCompletionsToResponses, "chat_tool_choice")
	}
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
			if _, ok := toolContext.LookupChatName(chatName); !ok {
				return nil, false
			}
			return map[string]any{"type": "function", "function": map[string]any{"name": chatName}}, true
		case "custom":
			name := stringValue(typed["name"])
			if _, ok := toolContext.LookupChatName(name); !ok {
				return nil, false
			}
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}, true
		case "tool_search":
			if _, ok := toolContext.LookupChatName(toolSearchProxyName); !ok {
				return nil, false
			}
			return map[string]any{"type": "function", "function": map[string]any{"name": toolSearchProxyName}}, true
		default:
			return nil, false
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

func boolValue(value any) bool {
	boolean, _ := value.(bool)
	return boolean
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
