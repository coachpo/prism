package runtime_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestResponsesTranslatedStreamingPreservesIngressDialectAndUsage(t *testing.T) {
	t.Run("chat ingress translated from responses upstream", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		stream := "event: response.created\n" +
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_runtime_stream\",\"model\":\"responses-target\",\"created_at\":1700000000}}\n\n" +
			"event: response.output_item.added\n" +
			"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[]}}\n\n" +
			"event: response.output_text.delta\n" +
			"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_1\",\"delta\":\"hello runtime\"}\n\n" +
			"event: response.completed\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_runtime_stream\",\"model\":\"responses-target\",\"created_at\":1700000000,\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello runtime\"}]}],\"usage\":{\"input_tokens\":10,\"output_tokens\":6,\"total_tokens\":16,\"input_tokens_details\":{\"cached_tokens\":4},\"output_tokens_details\":{\"reasoning_tokens\":3}}}}\n\n"
		upstream := newTranslatedStreamingUpstream(t, stream, http.Header{
			"Content-Type":     []string{"text/event-stream"},
			"Content-Encoding": []string{"gzip"},
			"Digest":           []string{"sha-256=responses-stream"},
			"ETag":             []string{`"responses-stream"`},
			"X-Request-Id":     []string{"responses-upstream-stream"},
		})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "responses-translated-stream-chat-public", "responses-translated-stream-responses-target", upstream.baseURL("/responses/translated/stream/chat"), "responses-translated-stream-chat-key", "responses_reasoning_none")
		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model":    route.PublicModelID,
			"messages": []map[string]any{{"role": "user", "content": "translated runtime stream"}},
			"stream":   true,
		}, nil)
		assertStatus(t, response, http.StatusOK)
		assertTranslatedOpenAISafeHeaders(t, response, "responses-upstream-stream")
		if got := response.Header.Get("Content-Type"); !strings.Contains(strings.ToLower(got), "text/event-stream") {
			t.Fatalf("expected translated streaming content-type text/event-stream, got %q", got)
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read translated chat stream body: %v", err)
		}
		payload := string(body)
		if strings.Contains(payload, "event: response.output_text.delta") || !strings.Contains(payload, "chat.completion.chunk") || !strings.Contains(payload, "data: [DONE]") {
			t.Fatalf("expected translated chat SSE body without raw responses framing, got %q", payload)
		}
		if request := upstream.lastRequest(t); request.Path != "/responses/translated/stream/chat/v1/responses" {
			t.Fatalf("expected translated upstream responses path, got %q", request.Path)
		}
		assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.chat_completions")
		assertLatestTranslatedIngressPlanningAttribution(t, harness.conn, profileID, "/v1/chat/completions", route.ConnectionID, "openai_chat_heuristic_v1")
		assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{InputTokens: runtimeNullInt64(6), OutputTokens: runtimeNullInt64(3), TotalTokens: runtimeNullInt64(16), CacheReadInputTokens: runtimeNullInt64(4), ReasoningTokens: runtimeNullInt64(3)})
		assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, true)
		streamRow := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
		if streamRow.StreamOutcome != "completed" {
			t.Fatalf("expected translated chat stream_outcome completed, got %+v", streamRow)
		}
	})

	t.Run("responses ingress translated from chat upstream", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		stream := "data: {\"id\":\"chatcmpl_runtime_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello runtime\"}}]}\n\n" +
			"data: {\"id\":\"chatcmpl_runtime_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"id\":\"chatcmpl_runtime_stream\",\"object\":\"chat.completion.chunk\",\"created\":1700000000,\"model\":\"chat-target\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":6,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
			"data: [DONE]\n\n"
		upstream := newTranslatedStreamingUpstream(t, stream, http.Header{
			"Content-Type":     []string{"text/event-stream"},
			"Content-Encoding": []string{"gzip"},
			"Digest":           []string{"sha-256=chat-stream"},
			"ETag":             []string{`"chat-stream"`},
			"X-Request-Id":     []string{"chat-upstream-stream"},
		})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "responses-translated-stream-responses-public", "responses-translated-stream-chat-target", upstream.baseURL("/responses/translated/stream/responses"), "responses-translated-stream-responses-key", "chat_completions_reasoning_none")
		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
			"model":  route.PublicModelID,
			"input":  "translated runtime responses stream",
			"stream": true,
		}, nil)
		assertStatus(t, response, http.StatusOK)
		assertTranslatedOpenAISafeHeaders(t, response, "chat-upstream-stream")
		if got := response.Header.Get("Content-Type"); !strings.Contains(strings.ToLower(got), "text/event-stream") {
			t.Fatalf("expected translated streaming content-type text/event-stream, got %q", got)
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read translated responses stream body: %v", err)
		}
		payload := string(body)
		if strings.Contains(payload, "data: [DONE]") || !strings.Contains(payload, "event: response.created") || !strings.Contains(payload, "event: response.completed") {
			t.Fatalf("expected translated responses SSE body without raw chat DONE sentinel, got %q", payload)
		}
		if request := upstream.lastRequest(t); request.Path != "/responses/translated/stream/responses/v1/chat/completions" {
			t.Fatalf("expected translated upstream chat path, got %q", request.Path)
		}
		assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.responses")
		assertLatestTranslatedIngressPlanningAttribution(t, harness.conn, profileID, "/v1/responses", route.ConnectionID, "openai_responses_heuristic_v1")
		assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{InputTokens: runtimeNullInt64(6), OutputTokens: runtimeNullInt64(3), TotalTokens: runtimeNullInt64(16), CacheReadInputTokens: runtimeNullInt64(4), ReasoningTokens: runtimeNullInt64(3)})
		assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, true)
		streamRow := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
		if streamRow.StreamOutcome != "completed" {
			t.Fatalf("expected translated responses stream_outcome completed, got %+v", streamRow)
		}
	})
}

type translatedStreamingUpstream struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []upstreamRequestSnapshot
}

func newTranslatedStreamingUpstream(t *testing.T, responseBody string, responseHeaders http.Header) *translatedStreamingUpstream {
	t.Helper()
	upstream := &translatedStreamingUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read translated streaming upstream body: %v", err)
		}
		_ = r.Body.Close()
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		upstream.mu.Unlock()
		for key, values := range responseHeaders {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, responseBody)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *translatedStreamingUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *translatedStreamingUpstream) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.requests) == 0 {
		t.Fatal("expected at least one translated streaming upstream request")
	}
	return u.requests[len(u.requests)-1]
}
