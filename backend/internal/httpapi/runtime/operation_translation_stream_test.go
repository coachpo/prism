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

func TestTranslateOpenAIStreamRejectsUnsupportedShape(t *testing.T) {
	tests := []struct {
		name        string
		ingressPath string
		mode        TranslationMode
		stream      string
		reason      string
	}{
		{
			name:        "chat tool delta",
			ingressPath: "/v1/responses",
			mode:        TranslationModeOpenAIResponsesToChatCompletions,
			stream: "data: {\"id\":\"chatcmpl_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{}\"}}]}}]}\n\n" +
				"data: [DONE]\n\n",
			reason: "chat_tool_call_stream",
		},
		{
			name:        "responses function event",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.function_call_arguments.delta\n" +
				"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"call_1\",\"delta\":\"{}\"}\n\n",
			reason: "responses_stream_response_function_call_arguments_delta",
		},
		{
			name:        "responses output item added function call",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_item.added\n" +
				"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"name\":\"lookup\",\"arguments\":\"{}\"}}\n\n",
			reason: "responses_stream_function_call",
		},
		{
			name:        "responses output item done function call",
			ingressPath: "/v1/chat/completions",
			mode:        TranslationModeOpenAIChatCompletionsToResponses,
			stream: "event: response.output_item.done\n" +
				"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"call_1\",\"type\":\"function_call\",\"name\":\"lookup\",\"arguments\":\"{}\"}}\n\n",
			reason: "responses_stream_function_call",
		},
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
