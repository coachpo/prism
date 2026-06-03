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
		for _, line := range strings.Split(chunk, "\n") {
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
