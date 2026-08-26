package runtime

import (
	"bytes"
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSSEStreamHooksByOperation(t *testing.T) {
	tests := []struct {
		name         string
		requestPath  string
		stream       string
		wantProvider string
		wantKind     operationResponseKind
		wantOutcome  string
		wantUsage    responseUsage
	}{
		{
			name:         "openai responses completed owns response terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998}},\"delta\":\"partial\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":8,\"total_tokens\":13,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(3), OutputTokens: intPtr(5), TotalTokens: intPtr(13), CacheReadInputTokens: intPtr(2), ReasoningTokens: intPtr(3)},
		},
		{
			name:         "openai responses incomplete owns provider incomplete terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.incomplete\ndata: {\"type\":\"response.incomplete\"}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeProviderIncomplete,
		},
		{
			name:         "openai responses failed owns provider incomplete terminal",
			requestPath:  "/v1/responses",
			stream:       "event: response.failed\ndata: {\"type\":\"response.failed\"}\n\n",
			wantProvider: "openai",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeProviderIncomplete,
		},
		{
			name:         "anthropic messages owns message stop",
			requestPath:  "/v1/messages",
			stream:       "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7,\"total_tokens\":18}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			wantProvider: "anthropic",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(11), OutputTokens: intPtr(7), TotalTokens: intPtr(18)},
		},
		{
			name:         "gemini stream generate owns usage metadata terminal",
			requestPath:  "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			stream:       "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":25,\"cachedContentTokenCount\":3,\"thoughtsTokenCount\":5}}\n\n",
			wantProvider: "gemini",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
			wantUsage:    responseUsage{InputTokens: intPtr(4), OutputTokens: intPtr(13), TotalTokens: intPtr(25), CacheReadInputTokens: intPtr(3), ReasoningTokens: intPtr(5)},
		},
		{
			name:         "gemini stream generate owns done terminal",
			requestPath:  "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			stream:       "data: {\"done\":true}\n\n",
			wantProvider: "gemini",
			wantKind:     operationResponseKindTextGeneration,
			wantOutcome:  runtimeStreamOutcomeCompleted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			hooks, ok := streamHooksForProxyResponse(operation, true)
			if !ok {
				t.Fatalf("expected stream hooks for %s", operation.Name)
			}
			if hooks.Provider != test.wantProvider || hooks.Kind != test.wantKind {
				t.Fatalf("expected %s/%s stream hooks, got %+v", test.wantProvider, test.wantKind, hooks)
			}
			var forwarded bytes.Buffer
			capture, err := proxyEventStreamAndCaptureCompletedResponse(operation, context.Background(), &forwarded, strings.NewReader(test.stream), time.Now, false)
			if err != nil {
				t.Fatalf("proxy SSE stream: %v", err)
			}
			if forwarded.String() != test.stream {
				t.Fatalf("expected SSE stream to pass through unchanged, got %q", forwarded.String())
			}
			if capture.StreamOutcome != test.wantOutcome {
				t.Fatalf("expected outcome %q, got %+v", test.wantOutcome, capture)
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.wantUsage) {
				t.Fatalf("expected usage %+v, got %+v", test.wantUsage, got)
			}
		})
	}

	responsesHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation)
	if got := responsesHooks.terminalSignal("message_stop", map[string]any{"type": "message_stop"}); got != sseTerminalSignalNone {
		t.Fatalf("expected OpenAI responses hook to ignore Anthropic message_stop, got %d", got)
	}
	anthropicHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1/messages").Operation)
	if got := anthropicHooks.terminalSignal("response.completed", map[string]any{"type": "response.completed"}); got != sseTerminalSignalNone {
		t.Fatalf("expected Anthropic hook to ignore OpenAI response.completed, got %d", got)
	}
	geminiHooks, _ := streamHooksForOperation(mustResolveRuntimeOperation(t, http.MethodPost, "/v1beta/models/gemini-2.5-pro:streamGenerateContent").Operation)
	if got := geminiHooks.terminalSignal("response.completed", map[string]any{"type": "response.completed"}); got != sseTerminalSignalNone {
		t.Fatalf("expected Gemini hook to ignore OpenAI response.completed, got %d", got)
	}
}

func TestNonStreamOperationsCannotUseSSEHooks(t *testing.T) {
	responsesOperation := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/responses").Operation
	if _, ok := streamHooksForProxyResponse(responsesOperation, false); ok {
		t.Fatal("expected non-stream OpenAI responses request to skip SSE hook selection")
	}

	geminiStreamPath := "/v1beta/models/gemini-2.5-pro:streamGenerateContent"
	geminiStreamOperation := mustResolveRuntimeOperation(t, http.MethodPost, geminiStreamPath).Operation
	if !requestWantsStreamForOperation(geminiStreamOperation, nil, geminiStreamPath) {
		t.Fatal("expected Gemini streamGenerateContent path to imply streaming")
	}
	if _, ok := streamHooksForProxyResponse(geminiStreamOperation, true); !ok {
		t.Fatal("expected Gemini streamGenerateContent to select SSE hooks")
	}

	tests := []struct {
		name        string
		requestPath string
	}{
		{name: "anthropic count tokens", requestPath: "/v1/messages/count_tokens"},
		{name: "gemini generate content", requestPath: "/v1beta/models/gemini-2.5-pro:generateContent"},
		{name: "gemini count tokens", requestPath: "/v1beta/models/gemini-2.5-pro:countTokens"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			if _, ok := streamHooksForOperation(operation); ok {
				t.Fatalf("expected no SSE hooks for %s", operation.Name)
			}
			if _, ok := streamHooksForProxyResponse(operation, true); ok {
				t.Fatalf("expected %s to skip SSE parser dispatch even when forced streaming", operation.Name)
			}
		})
	}
}

func TestProxyNonEventResponseAndCaptureUsageAcceptsOnlySupportedUsageSchemaPaths(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		payload     string
		want        responseUsage
	}{
		{
			name:        "keeps top-level usage and ignores nested spoofed usage object",
			requestPath: "/v1/chat/completions",
			payload:     `{"id":"chatcmpl-secure-stream","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_json","value":{"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}}]}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`,
			want: responseUsage{
				InputTokens:  intPtr(7),
				OutputTokens: intPtr(13),
				TotalTokens:  intPtr(20),
			},
		},
		{
			name:        "keeps response usage and ignores nested spoofed usage object",
			requestPath: "/v1/responses",
			payload:     `{"response":{"id":"resp-secure-stream","output":[{"type":"message","content":[{"type":"output_text","text":"hello","usage":{"input_tokens":999,"output_tokens":999,"total_tokens":1998}}]}],"usage":{"input_tokens":5,"output_tokens":8,"total_tokens":13}}}`,
			want: responseUsage{
				InputTokens:  intPtr(5),
				OutputTokens: intPtr(8),
				TotalTokens:  intPtr(13),
			},
		},
		{
			name:        "keeps top-level usage metadata and ignores nested spoofed usage metadata object",
			requestPath: "/v1beta/models/gemini-2.5-pro:generateContent",
			payload:     `{"candidates":[{"content":{"parts":[{"text":"hello"},{"metadata":{"usageMetadata":{"promptTokenCount":999,"candidatesTokenCount":999,"totalTokenCount":1998,"cachedContentTokenCount":777,"thoughtsTokenCount":666}}}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":25,"cachedContentTokenCount":3,"thoughtsTokenCount":5}}`,
			want: responseUsage{
				InputTokens:          intPtr(4),
				OutputTokens:         intPtr(13),
				TotalTokens:          intPtr(25),
				CacheReadInputTokens: intPtr(3),
				ReasoningTokens:      intPtr(5),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			var forwarded bytes.Buffer
			capture, err := proxyNonEventResponseAndCaptureByOperation(operation, &forwarded, strings.NewReader(test.payload), "application/json", time.Now, false)
			if err != nil {
				t.Fatalf("capture streamed non-sse usage: %v", err)
			}
			if forwarded.String() != test.payload {
				t.Fatalf("expected streamed response body to pass through unchanged, got %q", forwarded.String())
			}
			if got := capture.extractedUsage(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected extracted usage %+v, got %+v", test.want, got)
			}
		})
	}
}
