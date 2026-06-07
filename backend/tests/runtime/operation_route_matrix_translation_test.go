package runtime_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOperationRouteMatrixTranslatedOpenAITargetsAreNotSelectedByGenericPlanner(t *testing.T) {
	t.Run("chat ingress does not translate to responses-only target", func(t *testing.T) {
		harness := newEnforcedRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"resp_should_not_run"}`, http.Header{"Content-Type": []string{"application/json"}})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-chat-native-only-public", "route-matrix-chat-native-only-target", upstream.baseURL("/route-matrix/native-only/chat"), "route-matrix-chat-native-only-key", "responses_reasoning_none")

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model":                 route.PublicModelID,
			"messages":              []map[string]any{{"role": "user", "content": "native-only chat ingress"}},
			"max_completion_tokens": 64,
		}, nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
		if detail := runtimeResponseDetail(t, response); detail != "No eligible targets available for model '"+route.PublicModelID+"'." {
			t.Fatalf("expected no eligible target detail, got %q", detail)
		}
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected generic planner to avoid non-native upstream calls, got %d", got)
		}
	})

	t.Run("responses ingress does not translate to chat-only target", func(t *testing.T) {
		harness := newEnforcedRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"chatcmpl_should_not_run"}`, http.Header{"Content-Type": []string{"application/json"}})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-responses-native-only-public", "route-matrix-responses-native-only-target", upstream.baseURL("/route-matrix/native-only/responses"), "route-matrix-responses-native-only-key", "chat_completions_reasoning_none")

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
			"model":             route.PublicModelID,
			"input":             "native-only responses ingress",
			"max_output_tokens": 64,
		}, nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
		if detail := runtimeResponseDetail(t, response); detail != "No eligible targets available for model '"+route.PublicModelID+"'." {
			t.Fatalf("expected no eligible target detail, got %q", detail)
		}
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected generic planner to avoid non-native upstream calls, got %d", got)
		}
	})
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

func (u *translatedRouteMatrixUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}
