package runtime_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestOperationRouteMatrixSafeOnlyResponsesIngressRoutesToChatOnlyTarget(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"chatcmpl_route_matrix","object":"chat.completion","created":1700000001,"model":"chat-only-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"translated responses ingress"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`, http.Header{"Content-Type": []string{"application/json"}})
	endpointAPIKey := "route-matrix-responses-safe-only-key"
	route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-responses-safe-only-public", "route-matrix-responses-safe-only-target", upstream.baseURL(""), endpointAPIKey, "chat_completions_reasoning_none")

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"model":             route.PublicModelID,
		"input":             "safe-only responses ingress",
		"max_output_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	requests := upstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one translated upstream request, got %d", len(requests))
	}
	assertTranslatedRouteMatrixUpstreamRequest(t, requests[0], route, "/v1/chat/completions", endpointAPIKey)
}

func TestOperationRouteMatrixSafeOnlyChatIngressRoutesToResponsesOnlyTarget(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"resp_route_matrix","object":"response","created_at":1700000002,"model":"responses-only-upstream","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"translated chat ingress"}]}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`, http.Header{"Content-Type": []string{"application/json"}})
	endpointAPIKey := "route-matrix-chat-safe-only-key"
	route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-chat-safe-only-public", "route-matrix-chat-safe-only-target", upstream.baseURL(""), endpointAPIKey, "responses_reasoning_none")

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":                 route.PublicModelID,
		"messages":              []map[string]any{{"role": "user", "content": "safe-only chat ingress"}},
		"max_completion_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	requests := upstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected one translated upstream request, got %d", len(requests))
	}
	assertTranslatedRouteMatrixUpstreamRequest(t, requests[0], route, "/v1/responses", endpointAPIKey)
}

func TestOperationRouteMatrixOffModeResponsesIngressRejectsChatOnlyTarget(t *testing.T) {
	harness := newOffModeTranslatedRouteMatrixHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"chatcmpl_should_not_run"}`, http.Header{"Content-Type": []string{"application/json"}})
	route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-responses-off-public", "route-matrix-responses-off-target", upstream.baseURL(""), "route-matrix-responses-off-key", "chat_completions_reasoning_none")

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"model":             route.PublicModelID,
		"input":             "off-mode responses ingress",
		"max_output_tokens": 64,
	}, nil)
	assertTranslatedRouteMatrixNoEligibleTargets(t, response, route.PublicModelID)
	if got := len(upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected off mode to avoid translated upstream calls, got %d", got)
	}
}

func TestOperationRouteMatrixOffModeChatIngressRejectsResponsesOnlyTarget(t *testing.T) {
	harness := newOffModeTranslatedRouteMatrixHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newTranslatedRouteMatrixUpstream(t, `{"id":"resp_should_not_run"}`, http.Header{"Content-Type": []string{"application/json"}})
	route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-chat-off-public", "route-matrix-chat-off-target", upstream.baseURL(""), "route-matrix-chat-off-key", "responses_reasoning_none")

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":                 route.PublicModelID,
		"messages":              []map[string]any{{"role": "user", "content": "off-mode chat ingress"}},
		"max_completion_tokens": 64,
	}, nil)
	assertTranslatedRouteMatrixNoEligibleTargets(t, response, route.PublicModelID)
	if got := len(upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected off mode to avoid translated upstream calls, got %d", got)
	}
}

func newOffModeTranslatedRouteMatrixHarness(tb testing.TB) *runtimeHarness {
	tb.Helper()
	return newRuntimeHarnessWithConfig(tb, runtimeHarnessConfig{SettingsMutator: func(settings *config.Settings) {
		settings.OpenAITerminalTranslationMode = config.OpenAITerminalTranslationModeOff
	}})
}

func assertTranslatedRouteMatrixUpstreamRequest(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute, wantPath string, endpointAPIKey string) {
	t.Helper()
	if request.Method != http.MethodPost {
		t.Fatalf("expected translated upstream POST, got %s", request.Method)
	}
	if request.Path != wantPath {
		t.Fatalf("expected translated upstream path %q, got %q", wantPath, request.Path)
	}
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected translated upstream body model %q, got %q in %s", route.TargetModelID, got, string(request.Body))
	}
	if request.Headers.Get("Authorization") != "Bearer "+endpointAPIKey {
		t.Fatalf("expected translated upstream bearer auth %q, got %q", "Bearer "+endpointAPIKey, request.Headers.Get("Authorization"))
	}
}

func assertTranslatedRouteMatrixNoEligibleTargets(t *testing.T, response *http.Response, publicModelID string) {
	t.Helper()
	assertStatus(t, response, http.StatusServiceUnavailable)
	if detail := runtimeResponseDetail(t, response); detail != "No eligible targets available for model '"+publicModelID+"'." {
		t.Fatalf("expected no eligible target detail, got %q", detail)
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

func (u *translatedRouteMatrixUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}
