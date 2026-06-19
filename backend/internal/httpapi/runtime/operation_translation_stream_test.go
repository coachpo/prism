package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type translatedSSEEvent struct {
	event   string
	payload map[string]any
}

func parseTranslatedSSEEvents(t *testing.T, body string) []translatedSSEEvent {
	t.Helper()
	chunks := strings.Split(body, "\n\n")
	events := make([]translatedSSEEvent, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var eventName string
		var dataLines []string
		for line := range strings.SplitSeq(chunk, "\n") {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if len(dataLines) == 0 {
			continue
		}
		data := strings.Join(dataLines, "\n")
		if strings.TrimSpace(data) == "[DONE]" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Fatalf("decode translated SSE event %q payload: %v", eventName, err)
		}
		events = append(events, translatedSSEEvent{event: eventName, payload: payload})
	}
	return events
}

func translatedSSEEventPayload(t *testing.T, events []translatedSSEEvent, eventName string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event.event == eventName {
			return event.payload
		}
	}
	t.Fatalf("expected translated SSE event %q, got %+v", eventName, events)
	return nil
}

func TestTranslateOpenAIResponsesToChatStream(t *testing.T) {
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stream\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"hello \"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_stream\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello \"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":6,\"total_tokens\":16,\"input_tokens_details\":{\"cached_tokens\":4},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate responses stream to chat: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted || capture.FirstMeaningfulPayloadAt == nil || capture.CompletedAt == nil {
		t.Fatalf("expected completed translated chat stream with TTFT and completion timing, got %+v", capture)
	}
	wantUsage := generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3)
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected canonical responses usage %+v, got %+v", wantUsage, got)
	}
	if string(capture.AuditBody) != stream {
		t.Fatalf("expected raw upstream responses SSE audit body, got %q", string(capture.AuditBody))
	}
	forwardedBody := forwarded.String()
	if strings.Contains(forwardedBody, "event: response.output_text.delta") || strings.Contains(forwardedBody, "event: response.completed") {
		t.Fatalf("expected translated chat stream without raw responses event framing, got %q", forwardedBody)
	}
	if !strings.Contains(forwardedBody, "chat.completion.chunk") || !strings.Contains(forwardedBody, "\"finish_reason\":\"stop\"") || !strings.Contains(forwardedBody, "data: [DONE]") {
		t.Fatalf("expected translated chat stream chunks, finish_reason, and DONE sentinel, got %q", forwardedBody)
	}
	if !strings.Contains(forwardedBody, "\"usage\":{") || !strings.Contains(forwardedBody, "\"prompt_tokens\":10") || !strings.Contains(forwardedBody, "\"completion_tokens\":6") || !strings.Contains(forwardedBody, "\"total_tokens\":16") {
		t.Fatalf("expected translated chat usage payload from terminal responses usage, got %q", forwardedBody)
	}
}

func TestTranslateOpenAIResponsesToChatStreamAcceptsInProgressLifecycleEvent(t *testing.T) {
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_in_progress\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
		"event: response.in_progress\n" +
		"data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_in_progress\",\"model\":\"responses-target\"}}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"hello lifecycle\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_in_progress\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello lifecycle\"}]}]}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate responses stream with in_progress lifecycle event to chat: %v", err)
	}
	forwardedBody := forwarded.String()
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed translated stream after in_progress lifecycle, got %+v", capture)
	}
	if strings.Contains(forwardedBody, "event: response.in_progress") || strings.Contains(forwardedBody, "responses_stream_response_in_progress") {
		t.Fatalf("expected response.in_progress to be absorbed instead of forwarded or rejected, got %q", forwardedBody)
	}
	if !strings.Contains(forwardedBody, "chat.completion.chunk") || !strings.Contains(forwardedBody, "hello lifecycle") || !strings.Contains(forwardedBody, "data: [DONE]") {
		t.Fatalf("expected translated Chat chunks and DONE sentinel, got %q", forwardedBody)
	}
}

func TestTranslateOpenAIResponsesToChatStreamAcceptsContentPartAndOutputItemDoneLifecycleEvents(t *testing.T) {
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_text_lifecycle\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
		"event: response.content_part.added\n" +
		"data: {\"type\":\"response.content_part.added\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"output_text\",\"text\":\"\"}}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"delta\":\"hello lifecycle\"}\n\n" +
		"event: response.output_text.done\n" +
		"data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"text\":\"hello lifecycle\"}\n\n" +
		"event: response.content_part.done\n" +
		"data: {\"type\":\"response.content_part.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"text\",\"text\":\"hello lifecycle\"}}\n\n" +
		"event: response.output_item.done\n" +
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello lifecycle\"}]}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_text_lifecycle\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello lifecycle\"}]}]}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate responses stream with text lifecycle events to chat: %v", err)
	}
	forwardedBody := forwarded.String()
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed translated stream after text lifecycle events, got %+v", capture)
	}
	for _, rawEvent := range []string{"event: response.content_part.added", "event: response.content_part.done", "event: response.output_text.done", "event: response.output_item.done"} {
		if strings.Contains(forwardedBody, rawEvent) {
			t.Fatalf("expected lifecycle event %s to be absorbed, got %q", rawEvent, forwardedBody)
		}
	}
	if !strings.Contains(forwardedBody, "chat.completion.chunk") || !strings.Contains(forwardedBody, "hello lifecycle") || !strings.Contains(forwardedBody, "data: [DONE]") {
		t.Fatalf("expected translated Chat chunks and DONE sentinel, got %q", forwardedBody)
	}
}

func TestTranslateOpenAIResponsesToChatStreamRequestedModelPreserved(t *testing.T) {
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_public_model\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"public model\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_public_model\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"public model\"}]}]}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "responses-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("translate responses stream to chat with requested public model: %v", err)
	}
	forwardedBody := forwarded.String()
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed translated stream, got %+v", capture)
	}
	if strings.Contains(forwardedBody, "responses-target") || !strings.Contains(forwardedBody, "\"model\":\"responses-public\"") {
		t.Fatalf("expected translated Chat chunks to preserve requested public model and hide resolved target, got %q", forwardedBody)
	}
	for _, event := range parseTranslatedSSEEvents(t, forwardedBody) {
		if got := stringValue(event.payload["model"]); got != "responses-public" {
			t.Fatalf("expected translated Chat chunk model responses-public, got payload %+v", event.payload)
		}
	}
}

func TestTranslateOpenAIResponsesToChatStreamHandlesFailedTerminalEvent(t *testing.T) {
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_failed\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
		"event: response.failed\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failed\",\"model\":\"responses-target\",\"status\":\"failed\",\"error\":{\"message\":\"upstream failed\",\"type\":\"server_error\",\"code\":\"bad_gateway\"}}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "responses-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("translate failed responses stream to chat: %v", err)
	}
	forwardedBody := forwarded.String()
	if capture.StreamOutcome != runtimeStreamOutcomeProviderIncomplete || capture.StreamErrorKind != nil {
		t.Fatalf("expected failed terminal event to produce provider_incomplete stream behavior, got %+v", capture)
	}
	if strings.Contains(forwardedBody, "event: response.failed") || strings.Contains(forwardedBody, "responses_stream_response_failed") {
		t.Fatalf("expected response.failed to avoid raw forwarding and unknown-event rejection, got %q", forwardedBody)
	}
	if !strings.Contains(forwardedBody, "chat.completion.chunk") || !strings.Contains(forwardedBody, "upstream failed") || !strings.Contains(forwardedBody, "data: [DONE]") {
		t.Fatalf("expected translated Chat error chunk and DONE sentinel, got %q", forwardedBody)
	}
}

func TestTranslateOpenAIResponsesToChatStreamAcceptsReasoningDeltas(t *testing.T) {
	stream := "event: response.reasoning_text.delta\n" +
		"data: {\"type\":\"response.reasoning_text.delta\",\"output_index\":0,\"item_id\":\"rs_1\",\"delta\":\"plan\"}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":1,\"item_id\":\"msg_1\",\"delta\":\"answer\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_reasoning\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"plan\"}]},{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"answer\"}]}]}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "chat-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate responses stream reasoning deltas to chat: %v", err)
	}
	forwardedBody := forwarded.String()
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed stream outcome, got %+v", capture)
	}
	if !strings.Contains(forwardedBody, `"reasoning_content":"plan"`) || !strings.Contains(forwardedBody, `"content":"answer"`) || strings.Contains(forwardedBody, "response.reasoning_text.delta") {
		t.Fatalf("expected translated Chat reasoning_content and visible content only, got %q", forwardedBody)
	}
}

func TestTranslateOpenAIChatToResponsesStreamRequestedEqualsResolved(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIResponsesToChatCompletions, "chat-target", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate chat stream to responses: %v", err)
	}
	events := parseTranslatedSSEEvents(t, forwarded.String())
	created := translatedSSEEventPayload(t, events, "response.created")
	completed := translatedSSEEventPayload(t, events, "response.completed")
	createdResponse := created["response"].(map[string]any)
	completedResponse := completed["response"].(map[string]any)
	if got := stringValue(createdResponse["model"]); got != "chat-target" {
		t.Fatalf("expected translated response.created.response.model to preserve equal requested/resolved model, got %q", got)
	}
	if got := stringValue(completedResponse["model"]); got != "chat-target" {
		t.Fatalf("expected translated response.completed.response.model to preserve equal requested/resolved model, got %q", got)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed stream outcome, got %+v", capture)
	}
}

func TestTranslateOpenAIChatToResponsesStreamCapturesTerminalChoiceUsage(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIResponsesToChatCompletions, "responses-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate chat stream to responses: %v", err)
	}
	wantUsage := generationResponseHookTestUsageWithCacheAndReasoning(6, 3, 16, 4, 3)
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("expected terminal choice usage to populate capture, want %+v got %+v", wantUsage, got)
	}
	events := parseTranslatedSSEEvents(t, forwarded.String())
	completed := translatedSSEEventPayload(t, events, "response.completed")
	response := completed["response"].(map[string]any)
	usage, ok := response["usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected translated response.completed.response.usage to be present, got %+v", response)
	}
	if got := intValue(intPointerFromAny(usage["input_tokens"])); got != 10 {
		t.Fatalf("expected translated response.completed.response.usage.input_tokens to match upstream terminal choice usage, got %d", got)
	}
	if got := intValue(intPointerFromAny(usage["output_tokens"])); got != 6 {
		t.Fatalf("expected translated response.completed.response.usage.output_tokens to match upstream terminal choice usage, got %d", got)
	}
	if got := intValue(intPointerFromAny(usage["total_tokens"])); got != 16 {
		t.Fatalf("expected translated response.completed.response.usage.total_tokens to match upstream terminal choice usage, got %d", got)
	}
	if got := intValue(intPointerFromAny(nestedValue(usage, "input_tokens_details", "cached_tokens"))); got != 4 {
		t.Fatalf("expected translated response.completed.response.usage.input_tokens_details.cached_tokens to match upstream terminal choice usage, got %d", got)
	}
	if got := intValue(intPointerFromAny(nestedValue(usage, "output_tokens_details", "reasoning_tokens"))); got != 3 {
		t.Fatalf("expected translated response.completed.response.usage.output_tokens_details.reasoning_tokens to match upstream terminal choice usage, got %d", got)
	}
	if !strings.Contains(forwarded.String(), "event: response.completed") {
		t.Fatalf("expected forwarded SSE to include response.completed, got %q", forwarded.String())
	}
}

func TestTranslateOpenAIChatToResponsesStream(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIResponsesToChatCompletions, "responses-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate chat stream to responses: %v", err)
	}
	events := parseTranslatedSSEEvents(t, forwarded.String())
	created := translatedSSEEventPayload(t, events, "response.created")
	completed := translatedSSEEventPayload(t, events, "response.completed")
	createdResponse := created["response"].(map[string]any)
	completedResponse := completed["response"].(map[string]any)
	if got := stringValue(createdResponse["model"]); got != "responses-public" {
		t.Fatalf("expected translated response.created.response.model to normalize to requested public model, got %q", got)
	}
	if got := stringValue(completedResponse["model"]); got != "responses-public" {
		t.Fatalf("expected translated response.completed.response.model to normalize to requested public model, got %q", got)
	}
	if got := stringValue(createdResponse["model"]); got == "chat-target" {
		t.Fatalf("expected translated response.created.response.model to avoid leaking resolved target model, got %q", got)
	}
	if got := stringValue(completedResponse["model"]); got == "chat-target" {
		t.Fatalf("expected translated response.completed.response.model to avoid leaking resolved target model, got %q", got)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed stream outcome, got %+v", capture)
	}
}

func TestTranslateOpenAIChatToResponsesStreamAcceptsReasoningAndThinkDeltas(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"plan\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"<thi\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"nk>hidden</think>answer\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIResponsesToChatCompletions, "responses-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate chat reasoning and think stream to responses: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed stream outcome, got %+v", capture)
	}
	forwardedBody := forwarded.String()
	if !strings.Contains(forwardedBody, "response.reasoning_text.delta") || !strings.Contains(forwardedBody, `"delta":"plan"`) || !strings.Contains(forwardedBody, `"delta":"hidden"`) || !strings.Contains(forwardedBody, `"delta":"answer"`) || strings.Contains(forwardedBody, "<think>") {
		t.Fatalf("expected reasoning deltas plus visible answer without think tag leak, got %q", forwardedBody)
	}
	completed := translatedSSEEventPayload(t, parseTranslatedSSEEvents(t, forwardedBody), "response.completed")
	response := completed["response"].(map[string]any)
	output := response["output"].([]any)
	if len(output) < 2 || stringValue(output[0].(map[string]any)["type"]) != "reasoning" || stringValue(output[1].(map[string]any)["type"]) != "message" {
		t.Fatalf("expected terminal Responses output to keep reasoning before visible message, got %+v", output)
	}
}

func TestTranslateOpenAIChatToResponsesStreamPreservesErrorPayload(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-public\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello \"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-public\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-public\",\"error\":{\"message\":\"upstream failed\",\"type\":\"server_error\",\"code\":\"bad_gateway\"},\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16}}\n\n" +
		"data: [DONE]\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIResponsesToChatCompletions, "chat-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate chat stream to responses with error payload: %v", err)
	}
	events := parseTranslatedSSEEvents(t, forwarded.String())
	completed := translatedSSEEventPayload(t, events, "response.completed")
	completedResponse := completed["response"].(map[string]any)
	if got := stringValue(completedResponse["model"]); got != "chat-public" {
		t.Fatalf("expected equal requested/resolved model to remain unchanged during error payload preservation, got %q", got)
	}
	if got := stringValue(completedResponse["model"]); got == "chat-target" {
		t.Fatalf("expected translated error payload to avoid leaking resolved target model, got %q", got)
	}
	errorPayload, ok := completedResponse["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected translated response.completed.response to preserve error payload, got %+v", completedResponse)
	}
	if got := stringValue(errorPayload["message"]); got != "upstream failed" {
		t.Fatalf("expected preserved error payload message, got %q", got)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed stream outcome, got %+v", capture)
	}
}

func TestTranslateOpenAIChatToResponsesStreamAcceptsToolDeltas(t *testing.T) {
	rawRequest := []byte(`{"model":"responses-public","input":"use tools","tools":[{"type":"custom","name":"exec"},{"type":"tool_search"},{"type":"namespace","name":"mcp__apps__gmail","tools":[{"type":"function","name":"_search_emails","parameters":{"type":"object"}}]}]}`)
	stream := "data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"call_namespace\",\"type\":\"function\",\"function\":{\"name\":\"mcp__apps__gmail___search_emails\",\"arguments\":\"{\\\"query\\\":\\\"from:alerts\\\"}\"}},{\"index\":0,\"id\":\"call_custom\",\"type\":\"function\",\"function\":{\"name\":\"exec\",\"arguments\":\"{\\\"input\\\":\\\"ls -la\\\"}\"}},{\"index\":2,\"id\":\"call_search\",\"type\":\"function\",\"function\":{\"name\":\"tool_search\",\"arguments\":\"{\\\"query\\\":\\\"gmail\\\",\\\"limit\\\":3}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	metadata := runtimeFinalResponseTranslationMetadata{RequestedModelID: "responses-public", ResponseTranslationDirection: runtimeFinalResponseTranslationDirectionChatUpstreamToResponsesClient}
	capture, err := NewCodingAgentFormatBridge().ProxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBody(operation, metadata, rawRequest, context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate chat stream tool deltas to responses: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed stream outcome, got %+v", capture)
	}
	events := parseTranslatedSSEEvents(t, forwarded.String())
	completed := translatedSSEEventPayload(t, events, "response.completed")
	response := completed["response"].(map[string]any)
	output := response["output"].([]any)
	if len(output) != 3 {
		t.Fatalf("expected completed Responses tool calls sorted by tool_call index, got %+v", output)
	}
	custom := output[0].(map[string]any)
	if custom["type"] != "custom_tool_call" || custom["name"] != "exec" || custom["input"] != "ls -la" {
		t.Fatalf("expected custom tool reconstruction from stream context, got %+v", custom)
	}
	namespace := output[1].(map[string]any)
	if namespace["type"] != "function_call" || namespace["name"] != "_search_emails" || namespace["namespace"] != "mcp__apps__gmail" {
		t.Fatalf("expected namespace tool reconstruction from stream context, got %+v", namespace)
	}
	toolSearch := output[2].(map[string]any)
	if toolSearch["type"] != "tool_search_call" || toolSearch["execution"] != "client" || stringValue(nestedValue(toolSearch, "arguments", "query")) != "gmail" {
		t.Fatalf("expected tool_search reconstruction from stream context, got %+v", toolSearch)
	}
	if _, hasUsage := response["usage"]; hasUsage {
		t.Fatalf("expected stream without provider usage to omit terminal usage instead of failing, got %+v", response)
	}
}

func TestTranslateOpenAIResponsesToChatStreamAcceptsFunctionCallDeltas(t *testing.T) {
	stream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_tools\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
		"event: response.output_item.added\n" +
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"\"}}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"delta\":\"{\\\"q\\\":\"}\n\n" +
		"event: response.function_call_arguments.delta\n" +
		"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"fc_1\",\"delta\":\"\\\"x\\\"}\"}\n\n" +
		"event: response.output_item.done\n" +
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tools\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"x\\\"}\"}]}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "chat-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, true)
	if err != nil {
		t.Fatalf("translate responses stream function call deltas to chat: %v", err)
	}
	forwardedBody := forwarded.String()
	if capture.StreamOutcome != runtimeStreamOutcomeCompleted {
		t.Fatalf("expected completed stream outcome, got %+v", capture)
	}
	if !strings.Contains(forwardedBody, "\"tool_calls\"") || !strings.Contains(forwardedBody, "\"name\":\"lookup\"") || !strings.Contains(forwardedBody, "\"finish_reason\":\"tool_calls\"") {
		t.Fatalf("expected translated Chat tool-call chunks and tool_calls finish, got %q", forwardedBody)
	}
}

func TestTranslateOpenAIStreamRejectsUnsupportedShape(t *testing.T) {
	tests := []struct {
		name        string
		ingressPath string
		mode        TranslationMode
		stream      string
		reason      string
	}{
		{
			name:        "responses output item done unsupported type",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_item.done\n" +
				"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"img_1\",\"type\":\"image_generation_call\"}}\n\n",
			reason: "responses_stream_output_item",
		},
		{
			name:        "responses output item done message unsupported content part",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_item.done\n" +
				"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_image\",\"image_url\":\"https://example.invalid/image.png\"}]}}\n\n",
			reason: "responses_stream_content_part_type",
		},
		{
			name:        "responses output item done message function call content part",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_item.done\n" +
				"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"function_call_output\",\"call_id\":\"call_1\",\"output\":\"{}\"}]}}\n\n",
			reason: "responses_stream_function_call",
		},
		{
			name:        "responses content part added function call",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.content_part.added\n" +
				"data: {\"type\":\"response.content_part.added\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"function_call\",\"name\":\"lookup\",\"arguments\":\"{}\"}}\n\n",
			reason: "responses_stream_function_call",
		},
		{
			name:        "responses content part added unsupported type",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.content_part.added\n" +
				"data: {\"type\":\"response.content_part.added\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"output_image\",\"image_url\":\"https://example.invalid/image.png\"}}\n\n",
			reason: "responses_stream_content_part_type",
		},
		{
			name:        "responses content part added malformed part",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.content_part.added\n" +
				"data: {\"type\":\"response.content_part.added\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":\"not-object\"}\n\n",
			reason: "responses_stream_content_part",
		},
		{
			name:        "responses content part done function call output",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.content_part.done\n" +
				"data: {\"type\":\"response.content_part.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"function_call_output\",\"call_id\":\"call_1\",\"output\":\"{}\"}}\n\n",
			reason: "responses_stream_function_call",
		},
		{
			name:        "responses content part done unsupported type",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.content_part.done\n" +
				"data: {\"type\":\"response.content_part.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"output_audio\",\"audio\":\"AAAA\"}}\n\n",
			reason: "responses_stream_content_part_type",
		},
		{
			name:        "responses output text done function call part",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_text.done\n" +
				"data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":{\"type\":\"function_call\",\"name\":\"lookup\",\"arguments\":\"{}\"}}\n\n",
			reason: "responses_stream_function_call",
		},
		{
			name:        "responses output text done malformed part",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_text.done\n" +
				"data: {\"type\":\"response.output_text.done\",\"output_index\":0,\"item_id\":\"msg_1\",\"content_index\":0,\"part\":\"not-object\"}\n\n",
			reason: "responses_stream_content_part",
		},
		{
			name:        "chat upstream chunk missing choices",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			stream: "data: {\"id\":\"chatcmpl_bad\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\"}\n\n" +
				"data: [DONE]\n\n",
			reason: "chat_choices_stream",
		},
		{
			name:        "chat upstream chunk non-array choices",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			stream: "data: {\"id\":\"chatcmpl_bad\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":{}}\n\n" +
				"data: [DONE]\n\n",
			reason: "chat_choices_stream",
		},
		{
			name:        "chat upstream choice missing delta and message",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			stream: "data: {\"id\":\"chatcmpl_bad\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0}]}\n\n" +
				"data: [DONE]\n\n",
			reason: "chat_choice_stream",
		},
		{
			name:        "chat upstream malformed tool arguments payload",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			stream: "data: {\"id\":\"chatcmpl_tools\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_custom\",\"type\":\"function\",\"function\":{\"name\":\"exec\",\"arguments\":\"{\"input\":\"ls -la\"}\"}}]}}]}\n\n" +
				"data: [DONE]\n\n",
			reason: "chat_stream_payload",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.ingressPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, test.mode, "responses-public", context.Background(), &forwarded, strings.NewReader(test.stream), fixedResponseHookTestNow, true)
			if err == nil {
				t.Fatalf("expected unsupported stream translation error, got capture %+v and body %q", capture, forwarded.String())
			}
			var domainErr *domainError
			if !errors.As(err, &domainErr) {
				t.Fatalf("expected domain error, got %v", err)
			}
			if domainErr.StatusCode != http.StatusBadGateway || domainErr.ErrorCode != openAIStreamTranslationUnsupportedErrorCode || domainErr.Detail != openAIStreamTranslationUnsupportedDetail {
				t.Fatalf("expected pinned stream translation 502 contract, got %+v", domainErr)
			}
			if got := stringValue(domainErr.Fields["unsupported_reason"]); got != test.reason {
				t.Fatalf("expected unsupported reason %q, got %+v", test.reason, domainErr.Fields)
			}
		})
	}
}

func TestTranslateOpenAIResponsesToChatStreamPreservesIncompleteClassification(t *testing.T) {
	stream := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"partial\"}\n\n" +
		"event: response.incomplete\n" +
		"data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_incomplete\",\"model\":\"responses-target\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"partial\"}]}]}}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIChatCompletionsToResponses, "", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("translate incomplete responses stream to chat: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeProviderIncomplete || capture.StreamErrorKind != nil || capture.CompletedAt == nil {
		t.Fatalf("expected translated incomplete stream to preserve provider_incomplete classification, got %+v", capture)
	}
	if got := capture.extractedUsage(); !reflect.DeepEqual(got, responseUsage{}) {
		t.Fatalf("expected incomplete translated stream to discard canonical usage, got %+v", got)
	}
	if !strings.Contains(forwarded.String(), "\"finish_reason\":\"length\"") || !strings.Contains(forwarded.String(), "data: [DONE]") {
		t.Fatalf("expected translated incomplete chat stream to surface length-style terminal chunk and DONE sentinel, got %q", forwarded.String())
	}
}

func TestTranslateOpenAIChatToResponsesStreamPreservesMissingTerminalClassification(t *testing.T) {
	stream := "data: {\"id\":\"chatcmpl_missing_terminal\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"partial\"}}]}\n\n"
	operation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	var forwarded bytes.Buffer
	capture, err := proxyEventStreamAndCaptureCompletedResponseByOperation(operation, TranslationModeOpenAIResponsesToChatCompletions, "responses-public", context.Background(), &forwarded, strings.NewReader(stream), fixedResponseHookTestNow, false)
	if err != nil {
		t.Fatalf("translate missing-terminal chat stream to responses: %v", err)
	}
	if capture.StreamOutcome != runtimeStreamOutcomeUpstreamEndedWithoutTerminal || capture.StreamErrorKind == nil || *capture.StreamErrorKind != runtimeStreamErrorKindMissingTerminalEvent {
		t.Fatalf("expected translated missing-terminal stream classification to stay upstream_ended_without_terminal, got %+v", capture)
	}
	if strings.Contains(forwarded.String(), "event: response.completed") {
		t.Fatalf("expected translated responses stream without upstream terminal to avoid synthesized response.completed, got %q", forwarded.String())
	}
}
