package runtime_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOperationRouteMatrixTranslatedOpenAINonStreamResponses(t *testing.T) {
	t.Run("chat ingress translated from responses upstream", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"resp_route","model":"responses-target","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"translated via responses"}]}],"usage":{"input_tokens":10,"output_tokens":6,"total_tokens":16,"input_tokens_details":{"cached_tokens":4},"output_tokens_details":{"reasoning_tokens":3}}}`, http.Header{
			"Content-Type":     []string{"application/json"},
			"Content-Encoding": []string{"gzip"},
			"Digest":           []string{"sha-256=responses-route"},
			"ETag":             []string{`"responses-route"`},
			"X-Request-Id":     []string{"responses-upstream-route"},
		})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-translated-chat-public", "route-matrix-translated-responses-target", upstream.baseURL("/route-matrix/translated/chat"), "route-matrix-translated-chat-key", "responses_reasoning_none")

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model":                 route.PublicModelID,
			"messages":              []map[string]any{{"role": "user", "content": "translated chat ingress"}},
			"max_completion_tokens": 64,
		}, nil)
		assertStatus(t, response, http.StatusOK)
		assertTranslatedOpenAISafeHeaders(t, response, "responses-upstream-route")
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		choices := payload["choices"].([]any)
		message := choices[0].(map[string]any)["message"].(map[string]any)
		if got := message["content"].(string); got != "translated via responses" {
			t.Fatalf("expected translated chat content, got %q", got)
		}
		usagePayload := payload["usage"].(map[string]any)
		if got := jsonInt(t, usagePayload["prompt_tokens"]); got != 10 {
			t.Fatalf("expected translated chat prompt_tokens=10, got %+v", usagePayload)
		}
		if got := jsonInt(t, usagePayload["completion_tokens"]); got != 6 {
			t.Fatalf("expected translated chat completion_tokens=6, got %+v", usagePayload)
		}
		if upstreamRequest := upstream.lastRequest(t); upstreamRequest.Path != "/route-matrix/translated/chat/v1/responses" {
			t.Fatalf("expected translated responses upstream path, got %q", upstreamRequest.Path)
		}
		assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.chat_completions")
		assertLatestTranslatedIngressPlanningAttribution(t, harness.conn, profileID, "/v1/chat/completions", route.ConnectionID, "openai_chat_heuristic_v1")
		assertLatestRuntimeUsageRows(t, harness.conn, profileID, false, runtimePersistedUsageRow{
			InputTokens:          runtimeNullInt64(6),
			OutputTokens:         runtimeNullInt64(3),
			TotalTokens:          runtimeNullInt64(16),
			CacheReadInputTokens: runtimeNullInt64(4),
			ReasoningTokens:      runtimeNullInt64(3),
		})
	})

	t.Run("responses ingress translated from chat upstream", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"chatcmpl_route","model":"chat-target","choices":[{"index":0,"message":{"role":"assistant","content":"translated via chat"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`, http.Header{
			"Content-Type":     []string{"application/json"},
			"Content-Encoding": []string{"gzip"},
			"Digest":           []string{"sha-256=chat-route"},
			"ETag":             []string{`"chat-route"`},
			"X-Request-Id":     []string{"chat-upstream-route"},
		})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-translated-responses-public", "route-matrix-translated-chat-target", upstream.baseURL("/route-matrix/translated/responses"), "route-matrix-translated-responses-key", "chat_completions_reasoning_none")

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
			"model":             route.PublicModelID,
			"input":             "translated responses ingress",
			"max_output_tokens": 64,
		}, nil)
		assertStatus(t, response, http.StatusOK)
		assertTranslatedOpenAISafeHeaders(t, response, "chat-upstream-route")
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		output := payload["output"].([]any)
		message := output[0].(map[string]any)
		parts := message["content"].([]any)
		if len(parts) != 1 || parts[0].(map[string]any)["text"].(string) != "translated via chat" {
			t.Fatalf("expected translated responses output_text, got %+v", message["content"])
		}
		usagePayload := payload["usage"].(map[string]any)
		if got := jsonInt(t, usagePayload["input_tokens"]); got != 10 {
			t.Fatalf("expected translated responses input_tokens=10, got %+v", usagePayload)
		}
		if got := jsonInt(t, usagePayload["output_tokens"]); got != 6 {
			t.Fatalf("expected translated responses output_tokens=6, got %+v", usagePayload)
		}
		if upstreamRequest := upstream.lastRequest(t); upstreamRequest.Path != "/route-matrix/translated/responses/v1/chat/completions" {
			t.Fatalf("expected translated chat upstream path, got %q", upstreamRequest.Path)
		}
		assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.responses")
		assertLatestTranslatedIngressPlanningAttribution(t, harness.conn, profileID, "/v1/responses", route.ConnectionID, "openai_responses_heuristic_v1")
		assertLatestRuntimeUsageRows(t, harness.conn, profileID, false, runtimePersistedUsageRow{
			InputTokens:          runtimeNullInt64(6),
			OutputTokens:         runtimeNullInt64(3),
			TotalTokens:          runtimeNullInt64(16),
			CacheReadInputTokens: runtimeNullInt64(4),
			ReasoningTokens:      runtimeNullInt64(3),
		})
	})
}

func assertTranslatedOpenAISafeHeaders(t *testing.T, response *http.Response, wantRequestID string) {
	t.Helper()
	if response.Header.Get("Content-Encoding") != "" || response.Header.Get("Digest") != "" || response.Header.Get("ETag") != "" {
		t.Fatalf("expected translated downstream response to drop unsafe entity headers, got %v", response.Header)
	}
	if got := response.Header.Get("X-Request-Id"); got != wantRequestID {
		t.Fatalf("expected safe request id header %q, got %q", wantRequestID, got)
	}
}

type translatedRouteMatrixUpstream struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []upstreamRequestSnapshot
}

func newTranslatedRouteMatrixUpstream(t *testing.T, responseBody string, responseHeaders http.Header) *translatedRouteMatrixUpstream {
	t.Helper()
	upstream := &translatedRouteMatrixUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read translated route-matrix upstream body: %v", err)
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

func (u *translatedRouteMatrixUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *translatedRouteMatrixUpstream) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.requests) == 0 {
		t.Fatal("expected at least one translated route-matrix upstream request")
	}
	return u.requests[len(u.requests)-1]
}
