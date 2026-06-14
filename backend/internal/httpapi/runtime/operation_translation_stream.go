package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"
)

type openAIStreamTranslator interface {
	consumeEvent(string, map[string]any) ([][]byte, error)
	consumeDone() ([][]byte, error)
}

func proxyEventStreamAndCaptureCompletedResponseByOperation(operation RuntimeOperation, translationMode TranslationMode, requestedModelID string, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if translationMode == "" || translationMode == TranslationModeNone {
		return proxyEventStreamAndCaptureCompletedResponse(operation, ctx, dst, src, now, captureAuditBody)
	}
	streamHooks, translator, err := newOpenAIStreamTranslator(translationMode, requestedModelID)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	reader := bufio.NewReader(src)
	capture := sseCompletedResponseCapture{streamHooks: streamHooks}
	var auditBuffer bytes.Buffer
	var currentEvent string
	currentDataLines := make([]string, 0, 4)

	captureResult := func(classification sseStreamClassification) runtimeResponseCapture {
		responseCapture := capture.runtimeResponseCapture(classification)
		if captureAuditBody {
			responseCapture.AuditBody = append([]byte(nil), auditBuffer.Bytes()...)
		}
		return responseCapture
	}
	flushEvent := func() (error, error) {
		if len(currentDataLines) == 0 {
			currentEvent = ""
			currentDataLines = currentDataLines[:0]
			return nil, nil
		}
		payloadBytes := []byte(strings.Join(currentDataLines, "\n"))
		eventName := currentEvent
		currentEvent = ""
		currentDataLines = currentDataLines[:0]
		var frames [][]byte
		if strings.TrimSpace(string(payloadBytes)) == "[DONE]" {
			frames, err = translator.consumeDone()
		} else {
			payload := map[string]any{}
			if unmarshalErr := json.Unmarshal(payloadBytes, &payload); unmarshalErr == nil {
				frames, err = translator.consumeEvent(eventName, payload)
			}
		}
		if err != nil {
			return nil, err
		}
		for _, frame := range frames {
			if len(frame) == 0 {
				continue
			}
			if _, writeErr := dst.Write(frame); writeErr != nil {
				return writeErr, nil
			}
		}
		return nil, nil
	}
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			observedAt := now()
			capture.consumeLine(line, observedAt)
			if captureAuditBody {
				auditBuffer.Write(line)
			}
			trimmed := strings.TrimRight(string(line), "\r\n")
			switch {
			case trimmed == "":
				if writeErr, translateErr := flushEvent(); writeErr != nil {
					return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, writeErr)), writeErr
				} else if translateErr != nil {
					return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, translateErr, nil)), translateErr
				}
			case strings.HasPrefix(trimmed, "event:"):
				currentEvent = trimSSEFieldValue(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				currentDataLines = append(currentDataLines, trimSSEFieldValue(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if readErr == nil {
			continue
		}
		capture.finishEvent(now())
		if writeErr, translateErr := flushEvent(); writeErr != nil {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, writeErr)), writeErr
		} else if translateErr != nil {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, translateErr, nil)), translateErr
		}
		if errors.Is(readErr, io.EOF) {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, nil)), nil
		}
		return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, readErr, nil)), readErr
	}
}

func newOpenAIStreamTranslator(mode TranslationMode, requestedModelID string) (operationStreamHooks, openAIStreamTranslator, error) {
	switch mode {
	case TranslationModeOpenAIResponsesToChatCompletions:
		if hooks, ok := translatedOpenAIStreamHooksForMode(mode); ok {
			return hooks, &openAIChatToResponsesStreamTranslator{requestedModelID: requestedModelID, messageRole: "assistant", messageItemID: "msg_0"}, nil
		}
	case TranslationModeOpenAIChatCompletionsToResponses:
		if hooks, ok := translatedOpenAIStreamHooksForMode(mode); ok {
			return hooks, &openAIResponsesToChatStreamTranslator{requestedModelID: requestedModelID, messageRole: "assistant"}, nil
		}
	}
	return operationStreamHooks{}, nil, unsupportedTranslationModeError(mode)
}

func translatedOpenAIStreamHooksForMode(mode TranslationMode) (operationStreamHooks, bool) {
	switch mode {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return operationStreamHooksByCollectionID[openAIUpstreamOperationChatCompletions], true
	case TranslationModeOpenAIChatCompletionsToResponses:
		return operationStreamHooksByCollectionID[openAIUpstreamOperationResponses], true
	default:
		return operationStreamHooks{}, false
	}
}

func unsupportedOpenAIStreamTranslationShapeError(mode TranslationMode, reason string) error {
	return openAIStreamTranslationUnsupportedDomainError(mode, normalizedTranslationUnsupportedReason(reason))
}

func streamTranslationErrorFromResponseError(mode TranslationMode, err error, fallback string) error {
	if err == nil {
		return nil
	}
	if _, ok := translationUnsupportedDomainError(err, openAIResponseTranslationUnsupportedErrorCode); ok {
		return unsupportedOpenAIStreamTranslationShapeError(mode, translationUnsupportedReasonFromError(err, openAIResponseTranslationUnsupportedErrorCode, fallback))
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

type openAIChatToResponsesStreamTranslator struct {
	responseID        string
	upstreamModel     string
	requestedModelID  string
	serviceTier       string
	systemFingerprint string
	created           *int
	errorPayload      map[string]any
	messageRole       string
	messageItemID     string
	createdSent       bool
	messageAdded      bool
	finishReason      string
	terminalUsage     responseUsage
	text              strings.Builder
}

func (translator *openAIChatToResponsesStreamTranslator) consumeEvent(_ string, payload map[string]any) ([][]byte, error) {
	translator.captureChatMetadata(payload)
	if errPayload, ok := payload["error"].(map[string]any); ok {
		translator.errorPayload = errPayload
	}
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		if usage := extractResponseUsageFromPayload(payload, runtimeUsageRuleOpenAIChatCompletions); usage.hasValues() {
			translator.terminalUsage = usage
		}
		return nil, nil
	}
	if len(choices) > 1 {
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "chat_multi_choice_stream")
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "chat_choice_stream")
	}
	if finishReason := firstNonEmptyString(choice["finish_reason"], choice["finishReason"]); finishReason != "" {
		translator.finishReason = finishReason
	}
	delta, ok := choice["delta"].(map[string]any)
	if !ok {
		return nil, nil
	}
	translator.messageRole = firstNonEmptyString(delta["role"], translator.messageRole, "assistant")
	frames := make([][]byte, 0, 4)
	if content := stringValue(delta["content"]); content != "" {
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
		if frame, err := marshalOpenAIResponsesSSEEvent("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "output_index": 0, "item_id": translator.messageItemID, "content_index": 0, "delta": content}); err != nil {
			return nil, err
		} else {
			frames = append(frames, frame)
		}
	}
	if fieldHasValue(delta, "tool_calls") || fieldHasValue(delta, "function_call") {
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "chat_tool_call_stream")
	}
	if fieldHasValue(delta, "audio") {
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIResponsesToChatCompletions, "chat_audio_stream")
	}
	return frames, nil
}

func (translator *openAIChatToResponsesStreamTranslator) consumeDone() ([][]byte, error) {
	if frame, err := translator.ensureCreatedFrame(); err != nil {
		return nil, err
	} else if len(frame) > 0 {
		return translator.buildCompletedFrames(frame)
	}
	return translator.buildCompletedFrames(nil)
}

func (translator *openAIChatToResponsesStreamTranslator) buildCompletedFrames(prefix []byte) ([][]byte, error) {
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

func (translator *openAIChatToResponsesStreamTranslator) completedResponsesPayload() (map[string]any, error) {
	message := map[string]any{"role": firstNonEmptyString(translator.messageRole, "assistant"), "content": translator.text.String()}
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
		return nil, fmt.Errorf("marshal translated %s stream payload: %w", TranslationModeOpenAIResponsesToChatCompletions, err)
	}
	translatedBody, _, _, err := translateOpenAIChatToResponsesResponseWithRequestedModel(rawPayload, translator.requestedModelID)
	if err != nil {
		return nil, streamTranslationErrorFromResponseError(TranslationModeOpenAIResponsesToChatCompletions, err, "chat_terminal_payload")
	}
	translated := map[string]any{}
	if err := json.Unmarshal(translatedBody, &translated); err != nil {
		return nil, fmt.Errorf("decode translated %s stream payload: %w", TranslationModeOpenAIResponsesToChatCompletions, err)
	}
	return translated, nil
}

func (translator *openAIChatToResponsesStreamTranslator) clientFacingModel() string {
	return firstNonEmptyString(translator.requestedModelID, translator.upstreamModel)
}

func (translator *openAIChatToResponsesStreamTranslator) terminalErrorPayload() map[string]any {
	if translator.errorPayload == nil {
		return nil
	}
	return translator.errorPayload
}

func (translator *openAIChatToResponsesStreamTranslator) ensureCreatedFrame() ([]byte, error) {
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

func (translator *openAIChatToResponsesStreamTranslator) ensureMessageAddedFrame() ([]byte, error) {
	if translator.messageAdded {
		return nil, nil
	}
	translator.messageAdded = true
	return marshalOpenAIResponsesSSEEvent("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": translator.messageItemID, "type": "message", "role": firstNonEmptyString(translator.messageRole, "assistant"), "content": []any{}}})
}

func (translator *openAIChatToResponsesStreamTranslator) captureChatMetadata(payload map[string]any) {
	translator.responseID = firstNonEmptyString(payload["id"], translator.responseID)
	translator.upstreamModel = firstNonEmptyString(payload["model"], translator.upstreamModel)
	translator.serviceTier = firstNonEmptyString(payload["service_tier"], translator.serviceTier)
	translator.systemFingerprint = firstNonEmptyString(payload["system_fingerprint"], translator.systemFingerprint)
	if translator.created == nil {
		translator.created = intPointerFromAny(payload["created"])
	}
}

type openAIResponsesToChatStreamTranslator struct {
	responseID       string
	model            string
	requestedModelID string
	serviceTier      string
	created          *int
	messageRole      string
	roleSent         bool
	text             strings.Builder
}

func (translator *openAIResponsesToChatStreamTranslator) consumeEvent(event string, payload map[string]any) ([][]byte, error) {
	translator.captureResponsesMetadata(payload)
	eventType := translatedResponsesEventType(event, payload)
	switch eventType {
	case "response.created", "response.in_progress":
		return nil, nil
	case "response.output_item.added":
		return translator.consumeResponsesOutputItemAdded(payload)
	case "response.output_text.delta":
		return translator.consumeResponsesTextDelta(payload)
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
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, unsupportedResponsesStreamEventReason(eventType))
	}
}

func (translator *openAIResponsesToChatStreamTranslator) consumeDone() ([][]byte, error) {
	return nil, nil
}

func (translator *openAIResponsesToChatStreamTranslator) consumeResponsesOutputItemAdded(payload map[string]any) ([][]byte, error) {
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return nil, nil
	}
	itemType := strings.TrimSpace(stringValue(item["type"]))
	switch itemType {
	case "message":
		translator.messageRole = firstNonEmptyString(item["role"], translator.messageRole, "assistant")
		if !translator.roleSent {
			frame, err := translator.chatRoleChunk()
			if err != nil {
				return nil, err
			}
			return [][]byte{frame}, nil
		}
		return nil, nil
	case "function_call":
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_function_call")
	default:
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_output_item")
	}
}

func (translator *openAIResponsesToChatStreamTranslator) consumeResponsesFailedEvent(payload map[string]any) ([][]byte, error) {
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

func (translator *openAIResponsesToChatStreamTranslator) consumeResponsesErrorEvent(payload map[string]any) ([][]byte, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	errorPayload := firstValue(payload, "error", "message")
	if errorPayload == nil {
		errorPayload = payload
	}
	return translator.chatTerminalFailureFrames(errorPayload)
}

func (translator *openAIResponsesToChatStreamTranslator) chatTerminalFailureFrames(errorPayload any) ([][]byte, error) {
	frame, err := translator.marshalChatChunk(map[string]any{"error": errorPayload})
	if err != nil {
		return nil, err
	}
	return [][]byte{frame, []byte("data: [DONE]\n\n")}, nil
}

func (translator *openAIResponsesToChatStreamTranslator) consumeResponsesTextDelta(payload map[string]any) ([][]byte, error) {
	delta := stringValue(payload["delta"])
	if delta == "" {
		return nil, nil
	}
	translator.text.WriteString(delta)
	return translator.chatContentChunk(delta)
}

func (translator *openAIResponsesToChatStreamTranslator) consumeResponsesTerminalEvent(payload map[string]any, completed bool) ([][]byte, error) {
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

func (translator *openAIResponsesToChatStreamTranslator) translatedCompletedChatPayload(payload map[string]any) (map[string]any, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal translated %s terminal payload: %w", TranslationModeOpenAIChatCompletionsToResponses, err)
	}
	translatedBody, _, _, err := translateOpenAIResponsesToChatResponseWithRequestedModel(rawPayload, translator.requestedModelID)
	if err != nil {
		return nil, streamTranslationErrorFromResponseError(TranslationModeOpenAIChatCompletionsToResponses, err, "responses_terminal_payload")
	}
	translated := map[string]any{}
	if err := json.Unmarshal(translatedBody, &translated); err != nil {
		return nil, fmt.Errorf("decode translated %s terminal payload: %w", TranslationModeOpenAIChatCompletionsToResponses, err)
	}
	return translated, nil
}

func (translator *openAIResponsesToChatStreamTranslator) emitResidualChatFrames(frames [][]byte, translatedPayload map[string]any) ([][]byte, error) {
	if role := translatedChatRole(translatedPayload); role != "" {
		translator.messageRole = role
	}
	if suffix := translatedChatContentSuffix(translatedPayload, translator.text.String()); suffix != "" {
		translator.text.WriteString(suffix)
		contentFrames, err := translator.chatContentChunk(suffix)
		if err != nil {
			return nil, err
		}
		frames = append(frames, contentFrames...)
	}
	if len(translatedChatToolCalls(translatedPayload)) > 0 {
		return nil, unsupportedOpenAIStreamTranslationShapeError(TranslationModeOpenAIChatCompletionsToResponses, "responses_stream_function_call")
	}
	return frames, nil
}

func (translator *openAIResponsesToChatStreamTranslator) captureResponsesMetadata(payload map[string]any) {
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

func (translator *openAIResponsesToChatStreamTranslator) clientFacingModel() string {
	return firstNonEmptyString(translator.requestedModelID, translator.model)
}

func (translator *openAIResponsesToChatStreamTranslator) chatRoleChunk() ([]byte, error) {
	translator.roleSent = true
	return translator.marshalChatChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": firstNonEmptyString(translator.messageRole, "assistant")}}}})
}

func (translator *openAIResponsesToChatStreamTranslator) chatContentChunk(content string) ([][]byte, error) {
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

func (translator *openAIResponsesToChatStreamTranslator) marshalChatChunk(payload map[string]any) ([]byte, error) {
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
		return nil, fmt.Errorf("marshal translated %s chat chunk: %w", TranslationModeOpenAIChatCompletionsToResponses, err)
	}
	return []byte("data: " + string(rawChunk) + "\n\n"), nil
}

func (translator *openAIResponsesToChatStreamTranslator) chatFinishChunk(finishReason string) ([]byte, error) {
	return translator.marshalChatChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}}})
}

func (translator *openAIResponsesToChatStreamTranslator) chatUsageChunk(usage map[string]any) ([]byte, error) {
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
