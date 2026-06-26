package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

const (
	OpenAIStreamTranslationUnsupportedErrorCode = "openai_stream_translation_unsupported"
	OpenAIStreamTranslationUnsupportedDetail    = "Prism cannot translate this OpenAI stream shape for the selected target."
)

type StreamTranslator interface {
	ConsumeEvent(string, map[string]any) ([][]byte, error)
	ConsumeDone() ([][]byte, error)
}

func NewStreamTranslatorWithToolContext(mode provider.TranslationMode, requestedModelID string, toolContext *ToolContext) (StreamTranslator, error) {
	switch normalizedTranslationMode(mode) {
	case provider.TranslationModeOpenAIResponsesToChatCompletions:
		return &ChatUpstreamToResponsesClientStreamTranslator{requestedModelID: requestedModelID, messageRole: "assistant", messageItemID: "msg_0", inlineThink: NewInlineThinkState(), tools: map[int]*ToolCallState{}, requestToolContext: toolContext}, nil
	case provider.TranslationModeOpenAIChatCompletionsToResponses:
		return &ResponsesUpstreamToChatClientStreamTranslator{requestedModelID: requestedModelID, messageRole: "assistant", tools: map[int]*ToolCallState{}}, nil
	default:
		return nil, fmt.Errorf("unsupported translation mode %q", mode)
	}
}

func unsupportedOpenAIStreamTranslationShapeError(mode provider.TranslationMode, reason string) error {
	return UnsupportedOpenAITranslationError(http.StatusBadGateway, OpenAIStreamTranslationUnsupportedErrorCode, OpenAIStreamTranslationUnsupportedDetail, mode, normalizeUnsupportedReason(reason))
}

func streamTranslationErrorFromResponseError(mode provider.TranslationMode, err error, fallback string) error {
	if err == nil {
		return nil
	}
	var adapterErr *provider.AdapterError
	if errors.As(err, &adapterErr) && adapterErr != nil && adapterErr.Code == responseTranslationUnsupportedErrorCode {
		reason := fallback
		if adapterErr.Fields != nil {
			if value := stringValue(adapterErr.Fields["unsupported_reason"]); strings.TrimSpace(value) != "" {
				reason = value
			}
		}
		return unsupportedOpenAIStreamTranslationShapeError(mode, reason)
	}
	return err
}

func unsupportedResponsesStreamEventReason(eventType string) string {
	normalized := strings.ToLower(strings.TrimSpace(eventType))
	if normalized == "" {
		return "responses_stream_event"
	}
	normalized = strings.NewReplacer(".", "_", "-", "_").Replace(normalized)
	return "responses_stream_" + normalized
}

func usageHasValues(usage provider.UsageEnvelope) bool {
	return usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil || usage.CacheReadInputTokens != nil || usage.CacheCreationInputTokens != nil || usage.ReasoningTokens != nil
}

type ChatUpstreamToResponsesClientStreamTranslator struct {
	responseID         string
	upstreamModel      string
	requestedModelID   string
	serviceTier        string
	systemFingerprint  string
	created            *int
	errorPayload       map[string]any
	messageRole        string
	messageItemID      string
	messageOutputIndex *int
	nextOutputIndex    int
	createdSent        bool
	messageAdded       bool
	finishReason       string
	terminalUsage      provider.UsageEnvelope
	text               strings.Builder
	reasoning          ReasoningItemState
	inlineThink        InlineThinkState
	tools              map[int]*ToolCallState
	requestToolContext *ToolContext
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) ConsumeEvent(_ string, payload map[string]any) ([][]byte, error) {
	translator.captureChatMetadata(payload)
	if errPayload, ok := payload["error"].(map[string]any); ok {
		translator.errorPayload = errPayload
	}
	rawChoices, hasChoices := payload["choices"]
	choices, ok := rawChoices.([]any)
	if !hasChoices || !ok {
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_choices_stream")
	}
	if len(choices) == 0 {
		if usage := extractResponseUsageFromPayload(payload, OperationChatCompletions); usageHasValues(usage) {
			translator.terminalUsage = usage
			return nil, nil
		}
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_choices_stream")
	}
	if len(choices) > 1 {
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_multi_choice_stream")
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_choice_stream")
	}
	if finishReason := firstNonEmptyString(choice["finish_reason"], choice["finishReason"]); finishReason != "" {
		translator.finishReason = finishReason
		if usage := extractResponseUsageFromPayload(payload, OperationChatCompletions); usageHasValues(usage) {
			translator.terminalUsage = usage
		}
	}
	delta, ok := choice["delta"].(map[string]any)
	if !ok {
		if message, messageOK := choice["message"].(map[string]any); messageOK {
			return translator.consumeChatMessagePayload(message)
		}
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_choice_stream")
	}
	return translator.consumeChatMessagePayload(delta)
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) consumeChatMessagePayload(delta map[string]any) ([][]byte, error) {
	translator.messageRole = firstNonEmptyString(delta["role"], translator.messageRole, "assistant")
	frames := make([][]byte, 0, 4)
	if reasoning := ExtractReasoningFieldText(delta); reasoning != nil {
		reasoningFrames, err := translator.consumeChatReasoningDelta(reasoning.Text)
		if err != nil {
			return nil, err
		}
		frames = append(frames, reasoningFrames...)
	}
	if content := stringValue(delta["content"]); content != "" {
		contentFrames, err := translator.consumeChatContentDelta(content)
		if err != nil {
			return nil, err
		}
		frames = append(frames, contentFrames...)
	}
	if fieldHasValue(delta, "tool_calls") {
		toolFrames, err := translator.consumeChatToolCallDeltas(delta["tool_calls"])
		if err != nil {
			return nil, err
		}
		frames = append(frames, toolFrames...)
	}
	if fieldHasValue(delta, "function_call") {
		toolFrames, err := translator.consumeChatLegacyFunctionCallDelta(delta["function_call"])
		if err != nil {
			return nil, err
		}
		frames = append(frames, toolFrames...)
	}
	if fieldHasValue(delta, "audio") {
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_audio_stream")
	}
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) consumeChatToolCallDeltas(value any) ([][]byte, error) {
	rawCalls, ok := value.([]any)
	if !ok {
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_tool_call_stream")
	}
	frames := make([][]byte, 0, len(rawCalls)*2)
	for _, rawCall := range rawCalls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_tool_call_stream")
		}
		index := intValue(intPointerFromAny(call["index"]))
		function, _ := call["function"].(map[string]any)
		callFrames, err := translator.consumeChatToolDelta(index, firstNonEmptyString(call["id"], call["call_id"]), stringValue(function["name"]), stringValue(function["arguments"]), "")
		if err != nil {
			return nil, err
		}
		frames = append(frames, callFrames...)
	}
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) consumeChatLegacyFunctionCallDelta(value any) ([][]byte, error) {
	function, ok := value.(map[string]any)
	if !ok {
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIResponsesToChatCompletions, "chat_tool_call_stream")
	}
	return translator.consumeChatToolDelta(0, "call_0", stringValue(function["name"]), stringValue(function["arguments"]), "")
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) consumeChatToolDelta(index int, callID string, name string, arguments string, reasoning string) ([][]byte, error) {
	if translator.tools == nil {
		translator.tools = map[int]*ToolCallState{}
	}
	tool := translator.tools[index]
	if tool == nil {
		outputIndex := index
		tool = &ToolCallState{OutputIndex: &outputIndex, ItemID: responseToolItemID(firstNonEmptyString(callID, fmt.Sprintf("call_%d", index)), name, translator.toolContext())}
		translator.tools[index] = tool
	}
	tool.ApplyDelta(callID, name, arguments, reasoning)
	frames := make([][]byte, 0, 3)
	if frame, err := translator.ensureCreatedFrame(); err != nil {
		return nil, err
	} else if len(frame) > 0 {
		frames = append(frames, frame)
	}
	if !tool.Added {
		tool.Added = true
		item := ResponseToolCallItemFromChatName(tool.ItemID, "in_progress", firstNonEmptyString(tool.CallID, fmt.Sprintf("call_%d", index)), tool.Name, "", strings.TrimSpace(tool.ReasoningContent.String()), translator.toolContext())
		frame, err := marshalOpenAIResponsesSSEEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": index, "item": item})
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	if arguments != "" {
		frame, err := marshalOpenAIResponsesSSEEvent("response.function_call_arguments.delta", map[string]any{"type": "response.function_call_arguments.delta", "output_index": index, "item_id": tool.ItemID, "delta": arguments})
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) toolContext() *ToolContext {
	return translator.requestToolContext
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) ConsumeDone() ([][]byte, error) {
	frames, err := translator.flushChatInlineThink()
	if err != nil {
		return nil, err
	}
	if frame, err := translator.ensureCreatedFrame(); err != nil {
		return nil, err
	} else if len(frame) > 0 {
		frames = append(frames, frame)
	}
	completedFrames, err := translator.buildCompletedFrames(nil)
	if err != nil {
		return nil, err
	}
	frames = append(frames, completedFrames...)
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) buildCompletedFrames(prefix []byte) ([][]byte, error) {
	translatedResponse, err := translator.completedResponsesPayload()
	if err != nil {
		return nil, err
	}
	frames := make([][]byte, 0, 2)
	if len(prefix) > 0 {
		frames = append(frames, prefix)
	}
	completedFrame, err := marshalOpenAIResponsesSSEEvent("response.completed", map[string]any{"type": "response.completed", "response": translatedResponse})
	if err != nil {
		return nil, err
	}
	frames = append(frames, completedFrame)
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) completedResponsesPayload() (map[string]any, error) {
	message := map[string]any{"role": firstNonEmptyString(translator.messageRole, "assistant"), "content": translator.text.String()}
	if reasoning := strings.TrimSpace(translator.reasoning.Text.String()); reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if toolCalls := translator.completedChatToolCalls(); len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
		if strings.TrimSpace(stringValue(message["content"])) == "" {
			message["content"] = nil
		}
	}
	finishReason := strings.TrimSpace(translator.finishReason)
	if finishReason == "" {
		finishReason = "stop"
	}
	payload := map[string]any{"object": "chat.completion", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finishReason}}}
	if strings.TrimSpace(translator.responseID) != "" {
		payload["id"] = translator.responseID
	}
	if strings.TrimSpace(translator.upstreamModel) != "" {
		payload["model"] = translator.upstreamModel
	}
	if translator.created != nil {
		payload["created"] = *translator.created
	}
	if strings.TrimSpace(translator.serviceTier) != "" {
		payload["service_tier"] = translator.serviceTier
	}
	if strings.TrimSpace(translator.systemFingerprint) != "" {
		payload["system_fingerprint"] = translator.systemFingerprint
	}
	if usagePayload := buildOpenAIChatCompletionsUsagePayload(translator.terminalUsage); len(usagePayload) > 0 {
		payload["usage"] = usagePayload
	}
	if errPayload := translator.terminalErrorPayload(); len(errPayload) > 0 {
		payload["error"] = errPayload
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal translated %s stream payload: %w", provider.TranslationModeOpenAIResponsesToChatCompletions, err)
	}
	translatedBody, _, _, err := translateResponseWithToolContext(rawPayload, provider.TranslationModeOpenAIResponsesToChatCompletions, translator.requestedModelID, translator.toolContext())
	if err != nil {
		return nil, streamTranslationErrorFromResponseError(provider.TranslationModeOpenAIResponsesToChatCompletions, err, "chat_terminal_payload")
	}
	translated := map[string]any{}
	if err := json.Unmarshal(translatedBody, &translated); err != nil {
		return nil, fmt.Errorf("decode translated %s stream payload: %w", provider.TranslationModeOpenAIResponsesToChatCompletions, err)
	}
	return translated, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) completedChatToolCalls() []any {
	if len(translator.tools) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(translator.tools))
	for index := range translator.tools {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	out := make([]any, 0, len(indexes))
	for _, index := range indexes {
		tool := translator.tools[index]
		out = append(out, map[string]any{"id": firstNonEmptyString(tool.CallID, fmt.Sprintf("call_%d", index)), "type": "function", "function": map[string]any{"name": tool.Name, "arguments": tool.CanonicalArguments()}})
	}
	return out
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) clientFacingModel() string {
	return firstNonEmptyString(translator.requestedModelID, translator.upstreamModel)
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) terminalErrorPayload() map[string]any {
	if translator.errorPayload == nil {
		return nil
	}
	return translator.errorPayload
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) ensureCreatedFrame() ([]byte, error) {
	if translator.createdSent {
		return nil, nil
	}
	response := map[string]any{"id": firstNonEmptyString(translator.responseID, "response_0"), "object": "response", "status": "in_progress", "output": []any{}}
	if model := translator.clientFacingModel(); model != "" {
		response["model"] = model
	}
	if translator.created != nil {
		response["created_at"] = *translator.created
	}
	if strings.TrimSpace(translator.serviceTier) != "" {
		response["service_tier"] = translator.serviceTier
	}
	translator.createdSent = true
	return marshalOpenAIResponsesSSEEvent("response.created", map[string]any{"type": "response.created", "response": response})
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) ensureMessageAddedFrame() ([]byte, error) {
	if translator.messageAdded {
		return nil, nil
	}
	translator.messageAdded = true
	outputIndex := translator.messageOutputIndexValue()
	return marshalOpenAIResponsesSSEEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": outputIndex, "item": map[string]any{"id": translator.messageItemID, "type": "message", "role": firstNonEmptyString(translator.messageRole, "assistant"), "content": []any{}}})
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) allocateOutputIndex() int {
	index := translator.nextOutputIndex
	translator.nextOutputIndex++
	return index
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) messageOutputIndexValue() int {
	if translator.messageOutputIndex == nil {
		index := translator.allocateOutputIndex()
		translator.messageOutputIndex = &index
	}
	return *translator.messageOutputIndex
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) reasoningOutputIndexValue() int {
	if translator.reasoning.OutputIndex == nil {
		index := translator.allocateOutputIndex()
		translator.reasoning.OutputIndex = &index
	}
	return *translator.reasoning.OutputIndex
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) reasoningItemID() string {
	if strings.TrimSpace(translator.reasoning.ItemID) == "" {
		translator.reasoning.ItemID = responseReasoningID(firstNonEmptyString(translator.responseID, "stream"))
	}
	return translator.reasoning.ItemID
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) consumeChatReasoningDelta(delta string) ([][]byte, error) {
	delta = strings.TrimSpace(delta)
	if delta == "" {
		return nil, nil
	}
	translator.reasoning.Text.WriteString(delta)
	frames := make([][]byte, 0, 3)
	if frame, err := translator.ensureCreatedFrame(); err != nil {
		return nil, err
	} else if len(frame) > 0 {
		frames = append(frames, frame)
	}
	if !translator.reasoning.Added {
		translator.reasoning.Added = true
		frame, err := marshalOpenAIResponsesSSEEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": translator.reasoningOutputIndexValue(), "item": map[string]any{"id": translator.reasoningItemID(), "type": "reasoning", "summary": []any{}}})
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	frame, err := marshalOpenAIResponsesSSEEvent("response.reasoning_text.delta", map[string]any{"type": "response.reasoning_text.delta", "output_index": translator.reasoningOutputIndexValue(), "item_id": translator.reasoningItemID(), "summary_index": 0, "delta": delta})
	if err != nil {
		return nil, err
	}
	frames = append(frames, frame)
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) consumeChatContentDelta(content string) ([][]byte, error) {
	decision, reasoning, text := translator.inlineThink.Push(content)
	frames := make([][]byte, 0, 4)
	if decision == InlineThinkReasoningDecision && reasoning != "" {
		reasoningFrames, err := translator.consumeChatReasoningDelta(reasoning)
		if err != nil {
			return nil, err
		}
		frames = append(frames, reasoningFrames...)
	}
	if decision == InlineThinkNeedMore {
		return frames, nil
	}
	if text == "" {
		return frames, nil
	}
	textFrames, err := translator.emitChatTextDelta(text)
	if err != nil {
		return nil, err
	}
	frames = append(frames, textFrames...)
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) flushChatInlineThink() ([][]byte, error) {
	reasoning, text := translator.inlineThink.FlushAtBoundary()
	frames := make([][]byte, 0, 4)
	if reasoning != "" {
		reasoningFrames, err := translator.consumeChatReasoningDelta(reasoning)
		if err != nil {
			return nil, err
		}
		frames = append(frames, reasoningFrames...)
	}
	if text != "" {
		textFrames, err := translator.emitChatTextDelta(text)
		if err != nil {
			return nil, err
		}
		frames = append(frames, textFrames...)
	}
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) emitChatTextDelta(content string) ([][]byte, error) {
	frames := make([][]byte, 0, 3)
	if frame, err := translator.ensureCreatedFrame(); err != nil {
		return nil, err
	} else if len(frame) > 0 {
		frames = append(frames, frame)
	}
	if frame, err := translator.ensureMessageAddedFrame(); err != nil {
		return nil, err
	} else if len(frame) > 0 {
		frames = append(frames, frame)
	}
	translator.text.WriteString(content)
	frame, err := marshalOpenAIResponsesSSEEvent("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "output_index": translator.messageOutputIndexValue(), "item_id": translator.messageItemID, "content_index": 0, "delta": content})
	if err != nil {
		return nil, err
	}
	frames = append(frames, frame)
	return frames, nil
}

func (translator *ChatUpstreamToResponsesClientStreamTranslator) captureChatMetadata(payload map[string]any) {
	translator.responseID = firstNonEmptyString(payload["id"], translator.responseID)
	translator.upstreamModel = firstNonEmptyString(payload["model"], translator.upstreamModel)
	translator.serviceTier = firstNonEmptyString(payload["service_tier"], translator.serviceTier)
	translator.systemFingerprint = firstNonEmptyString(payload["system_fingerprint"], translator.systemFingerprint)
	if translator.created == nil {
		translator.created = intPointerFromAny(payload["created"])
	}
}

type ResponsesUpstreamToChatClientStreamTranslator struct {
	responseID       string
	model            string
	requestedModelID string
	serviceTier      string
	created          *int
	messageRole      string
	roleSent         bool
	text             strings.Builder
	reasoning        strings.Builder
	tools            map[int]*ToolCallState
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) ConsumeEvent(event string, payload map[string]any) ([][]byte, error) {
	translator.captureResponsesMetadata(payload)
	eventType := translatedResponsesEventType(event, payload)
	switch eventType {
	case "response.created", "response.in_progress":
		return nil, nil
	case "response.output_item.added":
		return translator.consumeResponsesOutputItemAdded(payload)
	case "response.output_item.done":
		return translator.consumeResponsesOutputItemLifecycle(payload)
	case "response.content_part.added", "response.content_part.done":
		return translator.consumeResponsesContentPartLifecycle(payload)
	case "response.output_text.done":
		return translator.consumeResponsesTextLifecycle(payload)
	case "response.output_text.delta":
		return translator.consumeResponsesTextDelta(payload)
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.reasoning.delta":
		return translator.consumeResponsesReasoningDelta(payload)
	case "response.reasoning_text.done", "response.reasoning_summary_text.done":
		return nil, nil
	case "response.function_call_arguments.delta":
		return translator.consumeResponsesFunctionCallArgumentsDelta(payload)
	case "response.completed":
		return translator.consumeResponsesTerminalEvent(payload, true)
	case "response.incomplete":
		return translator.consumeResponsesTerminalEvent(payload, false)
	case "response.failed":
		return translator.consumeResponsesFailedEvent(payload)
	case "error":
		return translator.consumeResponsesErrorEvent(payload)
	case "":
		return nil, nil
	default:
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, unsupportedResponsesStreamEventReason(eventType))
	}
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) ConsumeDone() ([][]byte, error) {
	return nil, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesOutputItemAdded(payload map[string]any) ([][]byte, error) {
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return nil, nil
	}
	if err := translator.validateResponsesOutputItemLifecycle(item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(stringValue(item["type"])) == "function_call" {
		return translator.consumeResponsesFunctionCallItem(payload, item, false)
	}
	translator.messageRole = firstNonEmptyString(item["role"], translator.messageRole, "assistant")
	if !translator.roleSent {
		frame, err := translator.chatRoleChunk()
		if err != nil {
			return nil, err
		}
		return [][]byte{frame}, nil
	}
	return nil, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesOutputItemLifecycle(payload map[string]any) ([][]byte, error) {
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return nil, nil
	}
	if err := translator.validateResponsesOutputItemLifecycle(item); err != nil {
		return nil, err
	}
	if strings.TrimSpace(stringValue(item["type"])) == "function_call" {
		return translator.consumeResponsesFunctionCallItem(payload, item, true)
	}
	return nil, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) validateResponsesOutputItemLifecycle(item map[string]any) error {
	switch strings.TrimSpace(stringValue(item["type"])) {
	case "message":
		return validateResponsesMessageContentLifecycle(item["content"])
	case "reasoning":
		return nil
	case "function_call":
		return nil
	default:
		return unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_output_item")
	}
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesContentPartLifecycle(payload map[string]any) ([][]byte, error) {
	part, err := responsesLifecyclePart(payload)
	if err != nil || part == nil {
		return nil, err
	}
	return nil, validateResponsesContentPartLifecycle(part)
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesTextLifecycle(payload map[string]any) ([][]byte, error) {
	part, err := responsesLifecyclePart(payload)
	if err != nil || part == nil {
		return nil, err
	}
	return nil, validateResponsesContentPartLifecycle(part)
}

func responsesLifecyclePart(payload map[string]any) (map[string]any, error) {
	rawPart, ok := payload["part"]
	if !ok || rawPart == nil {
		return nil, nil
	}
	part, ok := rawPart.(map[string]any)
	if !ok {
		return nil, unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_content_part")
	}
	return part, nil
}

func validateResponsesMessageContentLifecycle(value any) error {
	switch typed := value.(type) {
	case nil, string:
		return nil
	case []any:
		for _, rawPart := range typed {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_content_part")
			}
			if err := validateResponsesContentPartLifecycle(part); err != nil {
				return err
			}
		}
		return nil
	default:
		return unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_content")
	}
}

func validateResponsesContentPartLifecycle(part map[string]any) error {
	switch strings.ToLower(strings.TrimSpace(stringValue(part["type"]))) {
	case "", "input_text", "output_text", "text":
		return nil
	case "function_call", "function_call_output":
		return unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_function_call")
	default:
		return unsupportedOpenAIStreamTranslationShapeError(provider.TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_content_part_type")
	}
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesFailedEvent(payload map[string]any) ([][]byte, error) {
	translator.captureResponsesMetadata(payload)
	errorPayload := firstOpenAIResponseTranslationValue(payload, "error")
	if errorPayload == nil {
		errorPayload = firstValue(payload, "error", "message")
	}
	if errorPayload == nil {
		errorPayload = map[string]any{"type": "response_failed"}
	}
	return translator.chatTerminalFailureFrames(errorPayload)
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesErrorEvent(payload map[string]any) ([][]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	errorPayload := firstValue(payload, "error", "message")
	if errorPayload == nil {
		errorPayload = payload
	}
	return translator.chatTerminalFailureFrames(errorPayload)
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) chatTerminalFailureFrames(errorPayload any) ([][]byte, error) {
	frame, err := translator.marshalChatChunk(map[string]any{"error": errorPayload})
	if err != nil {
		return nil, err
	}
	return [][]byte{frame, []byte("data: [DONE]\n\n")}, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesTextDelta(payload map[string]any) ([][]byte, error) {
	delta := stringValue(payload["delta"])
	if delta == "" {
		return nil, nil
	}
	translator.text.WriteString(delta)
	return translator.chatContentChunk(delta)
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesReasoningDelta(payload map[string]any) ([][]byte, error) {
	delta := firstNonEmptyString(payload["delta"], payload["text"])
	if delta == "" {
		return nil, nil
	}
	translator.reasoning.WriteString(delta)
	return translator.chatReasoningChunk(delta)
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesFunctionCallItem(payload map[string]any, item map[string]any, done bool) ([][]byte, error) {
	index := intValue(intPointerFromAny(payload["output_index"]))
	if translator.tools == nil {
		translator.tools = map[int]*ToolCallState{}
	}
	tool := translator.tools[index]
	if tool == nil {
		outputIndex := index
		tool = &ToolCallState{OutputIndex: &outputIndex, ItemID: stringValue(item["id"])}
		translator.tools[index] = tool
	}
	tool.ItemID = firstNonEmptyString(tool.ItemID, item["id"])
	tool.ApplyDelta(firstNonEmptyString(item["call_id"], item["id"]), stringValue(item["name"]), stringValue(item["arguments"]), "")
	if done {
		tool.Done = true
	}
	return translator.chatToolCallChunk(index, tool, "")
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesFunctionCallArgumentsDelta(payload map[string]any) ([][]byte, error) {
	index := intValue(intPointerFromAny(payload["output_index"]))
	if translator.tools == nil {
		translator.tools = map[int]*ToolCallState{}
	}
	tool := translator.tools[index]
	if tool == nil {
		outputIndex := index
		tool = &ToolCallState{OutputIndex: &outputIndex, ItemID: stringValue(payload["item_id"])}
		translator.tools[index] = tool
	}
	tool.ItemID = firstNonEmptyString(tool.ItemID, payload["item_id"])
	delta := stringValue(payload["delta"])
	tool.ApplyDelta("", "", delta, "")
	return translator.chatToolCallChunk(index, tool, delta)
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) consumeResponsesTerminalEvent(payload map[string]any, completed bool) ([][]byte, error) {
	translator.captureResponsesMetadata(payload)
	frames := make([][]byte, 0, 6)
	translatedPayload, err := translator.translatedCompletedChatPayload(payload)
	if err != nil {
		return nil, err
	}
	frames, err = translator.emitResidualChatFrames(frames, translatedPayload)
	if err != nil {
		return nil, err
	}
	finishReason := "length"
	if completed {
		finishReason = translatedChatFinishReason(translatedPayload)
		if finishReason == "" {
			finishReason = "stop"
		}
	}
	finishFrame, err := translator.chatFinishChunk(finishReason)
	if err != nil {
		return nil, err
	}
	frames = append(frames, finishFrame)
	if completed {
		if usagePayload, ok := translatedPayload["usage"].(map[string]any); ok && len(usagePayload) > 0 {
			usageFrame, err := translator.chatUsageChunk(usagePayload)
			if err != nil {
				return nil, err
			}
			frames = append(frames, usageFrame)
		}
	}
	frames = append(frames, []byte("data: [DONE]\n\n"))
	return frames, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) translatedCompletedChatPayload(payload map[string]any) (map[string]any, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal translated %s terminal payload: %w", provider.TranslationModeOpenAIChatCompletionsToResponses, err)
	}
	translatedBody, _, _, err := translateResponse(rawPayload, provider.TranslationModeOpenAIChatCompletionsToResponses, translator.requestedModelID)
	if err != nil {
		return nil, streamTranslationErrorFromResponseError(provider.TranslationModeOpenAIChatCompletionsToResponses, err, "responses_terminal_payload")
	}
	translated := map[string]any{}
	if err := json.Unmarshal(translatedBody, &translated); err != nil {
		return nil, fmt.Errorf("decode translated %s terminal payload: %w", provider.TranslationModeOpenAIChatCompletionsToResponses, err)
	}
	return translated, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) emitResidualChatFrames(frames [][]byte, translatedPayload map[string]any) ([][]byte, error) {
	if role := translatedChatRole(translatedPayload); role != "" {
		translator.messageRole = role
	}
	if suffix := translatedChatReasoningSuffix(translatedPayload, translator.reasoning.String()); suffix != "" {
		translator.reasoning.WriteString(suffix)
		reasoningFrames, err := translator.chatReasoningChunk(suffix)
		if err != nil {
			return nil, err
		}
		frames = append(frames, reasoningFrames...)
	}
	if suffix := translatedChatContentSuffix(translatedPayload, translator.text.String()); suffix != "" {
		translator.text.WriteString(suffix)
		contentFrames, err := translator.chatContentChunk(suffix)
		if err != nil {
			return nil, err
		}
		frames = append(frames, contentFrames...)
	}
	toolFrames, err := translator.emitResidualChatToolFrames(translatedPayload)
	if err != nil {
		return nil, err
	}
	frames = append(frames, toolFrames...)
	return frames, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) emitResidualChatToolFrames(translatedPayload map[string]any) ([][]byte, error) {
	toolCalls := translatedChatToolCalls(translatedPayload)
	if len(toolCalls) == 0 {
		return nil, nil
	}
	frames := make([][]byte, 0, len(toolCalls)+1)
	for index, toolCall := range toolCalls {
		if existing := translator.tools[index]; existing != nil && existing.Added {
			continue
		}
		function, _ := toolCall["function"].(map[string]any)
		outputIndex := index
		tool := &ToolCallState{OutputIndex: &outputIndex, ItemID: stringValue(toolCall["id"])}
		tool.ApplyDelta(stringValue(toolCall["id"]), stringValue(function["name"]), stringValue(function["arguments"]), "")
		if translator.tools == nil {
			translator.tools = map[int]*ToolCallState{}
		}
		translator.tools[index] = tool
		toolFrames, err := translator.chatToolCallChunk(index, tool, tool.CanonicalArguments())
		if err != nil {
			return nil, err
		}
		frames = append(frames, toolFrames...)
	}
	return frames, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) captureResponsesMetadata(payload map[string]any) {
	translator.responseID = firstNonEmptyString(firstOpenAIResponseTranslationValue(payload, "id"), translator.responseID)
	translator.model = firstNonEmptyString(firstOpenAIResponseTranslationValue(payload, "model"), translator.model)
	translator.serviceTier = firstNonEmptyString(firstOpenAIResponseTranslationValue(payload, "service_tier"), translator.serviceTier)
	if translator.created == nil {
		translator.created = intPointerFromAny(firstOpenAIResponseTranslationValue(payload, "created_at"))
		if translator.created == nil {
			translator.created = intPointerFromAny(firstOpenAIResponseTranslationValue(payload, "created"))
		}
	}
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) clientFacingModel() string {
	return firstNonEmptyString(translator.requestedModelID, translator.model)
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) chatRoleChunk() ([]byte, error) {
	translator.roleSent = true
	return translator.marshalChatChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": firstNonEmptyString(translator.messageRole, "assistant")}}}})
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) chatContentChunk(content string) ([][]byte, error) {
	if content == "" {
		return nil, nil
	}
	frames := make([][]byte, 0, 2)
	if !translator.roleSent {
		frame, err := translator.chatRoleChunk()
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	frame, err := translator.marshalChatChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}}}})
	if err != nil {
		return nil, err
	}
	frames = append(frames, frame)
	return frames, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) chatReasoningChunk(content string) ([][]byte, error) {
	if content == "" {
		return nil, nil
	}
	frames := make([][]byte, 0, 2)
	if !translator.roleSent {
		frame, err := translator.chatRoleChunk()
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	frame, err := translator.marshalChatChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": content}}}})
	if err != nil {
		return nil, err
	}
	frames = append(frames, frame)
	return frames, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) chatToolCallChunk(index int, tool *ToolCallState, argumentsDelta string) ([][]byte, error) {
	frames := make([][]byte, 0, 2)
	if !translator.roleSent {
		frame, err := translator.chatRoleChunk()
		if err != nil {
			return nil, err
		}
		frames = append(frames, frame)
	}
	function := map[string]any{}
	if !tool.Added && strings.TrimSpace(tool.Name) != "" {
		function["name"] = tool.Name
	}
	if argumentsDelta != "" {
		function["arguments"] = argumentsDelta
	}
	delta := map[string]any{"index": index, "type": "function", "function": function}
	if !tool.Added {
		delta["id"] = firstNonEmptyString(tool.CallID, tool.ItemID, fmt.Sprintf("call_%d", index))
		tool.Added = true
	}
	frame, err := translator.marshalChatChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{delta}}}}})
	if err != nil {
		return nil, err
	}
	frames = append(frames, frame)
	return frames, nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) marshalChatChunk(payload map[string]any) ([]byte, error) {
	chunk := map[string]any{"object": "chat.completion.chunk"}
	if strings.TrimSpace(translator.responseID) != "" {
		chunk["id"] = translator.responseID
	}
	if model := translator.clientFacingModel(); model != "" {
		chunk["model"] = model
	}
	if translator.created != nil {
		chunk["created"] = *translator.created
	}
	if strings.TrimSpace(translator.serviceTier) != "" {
		chunk["service_tier"] = translator.serviceTier
	}
	maps.Copy(chunk, payload)
	rawChunk, err := json.Marshal(chunk)
	if err != nil {
		return nil, fmt.Errorf("marshal translated %s chat chunk: %w", provider.TranslationModeOpenAIChatCompletionsToResponses, err)
	}
	return []byte("data: " + string(rawChunk) + "\n\n"), nil
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) chatFinishChunk(finishReason string) ([]byte, error) {
	return translator.marshalChatChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}}})
}

func (translator *ResponsesUpstreamToChatClientStreamTranslator) chatUsageChunk(usage map[string]any) ([]byte, error) {
	return translator.marshalChatChunk(map[string]any{"choices": []any{}, "usage": usage})
}

func translatedResponsesEventType(event string, payload map[string]any) string {
	if trimmed := strings.TrimSpace(event); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(stringValue(payload["type"]))
}

func translatedChatRole(payload map[string]any) string {
	message, ok := translatedChatMessage(payload)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue(message["role"]))
}

func translatedChatFinishReason(payload map[string]any) string {
	choice, ok := translatedChatChoice(payload)
	if !ok {
		return ""
	}
	return firstNonEmptyString(choice["finish_reason"], choice["finishReason"])
}

func translatedChatContentSuffix(payload map[string]any, emitted string) string {
	message, ok := translatedChatMessage(payload)
	if !ok {
		return ""
	}
	full := stringValue(message["content"])
	if full == "" {
		return ""
	}
	if !strings.HasPrefix(full, emitted) {
		return full
	}
	return full[len(emitted):]
}

func translatedChatReasoningSuffix(payload map[string]any, emitted string) string {
	message, ok := translatedChatMessage(payload)
	if !ok {
		return ""
	}
	full := stringValue(message["reasoning_content"])
	if full == "" {
		return ""
	}
	if !strings.HasPrefix(full, emitted) {
		return full
	}
	return full[len(emitted):]
}

func translatedChatToolCalls(payload map[string]any) []map[string]any {
	message, ok := translatedChatMessage(payload)
	if !ok {
		return nil
	}
	rawToolCalls, ok := message["tool_calls"].([]any)
	if !ok {
		return nil
	}
	translated := make([]map[string]any, 0, len(rawToolCalls))
	for _, rawToolCall := range rawToolCalls {
		toolCall, ok := rawToolCall.(map[string]any)
		if !ok {
			continue
		}
		translated = append(translated, toolCall)
	}
	return translated
}

func translatedChatChoice(payload map[string]any) (map[string]any, bool) {
	rawChoices, ok := payload["choices"].([]any)
	if !ok || len(rawChoices) == 0 {
		return nil, false
	}
	choice, ok := rawChoices[0].(map[string]any)
	return choice, ok
}

func translatedChatMessage(payload map[string]any) (map[string]any, bool) {
	choice, ok := translatedChatChoice(payload)
	if !ok {
		return nil, false
	}
	message, ok := choice["message"].(map[string]any)
	return message, ok
}

func marshalOpenAIResponsesSSEEvent(event string, payload map[string]any) ([]byte, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal translated OpenAI SSE event %s: %w", strings.TrimSpace(event), err)
	}
	if strings.TrimSpace(event) == "" {
		return []byte("data: " + string(rawPayload) + "\n\n"), nil
	}
	return []byte("event: " + strings.TrimSpace(event) + "\n" + "data: " + string(rawPayload) + "\n\n"), nil
}
