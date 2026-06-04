package runtime_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestOperationRouteMatrixTranslatedOpenAINonStreamResponses(t *testing.T) {
	t.Run("chat ingress translated from responses upstream", func(t *testing.T) {
		harness := newEnforcedRuntimeHarness(t)
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
		harness := newEnforcedRuntimeHarness(t)
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
		if got, ok := payload["model"].(string); !ok || got != route.PublicModelID {
			t.Fatalf("expected translated responses payload model %q, got %+v", route.PublicModelID, payload["model"])
		}
		if got, ok := payload["model"].(string); !ok || got == route.TargetModelID {
			t.Fatalf("expected translated responses payload model to avoid target %q, got %+v", route.TargetModelID, payload["model"])
		}
		assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, route.TargetModelID)
	})
}

func TestOperationRouteMatrixTranslatedOpenAISafeOnlyDeepSeekFallback(t *testing.T) {
	t.Run("responses ingress text-only fallback can translate to deepseek chat-only tier", func(t *testing.T) {
		harness := newEnforcedRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		primaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "deepseek-safe-only-primary"})
		secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "deepseek-safe-only-secondary"})
		deepseekUpstream := newTranslatedRouteMatrixUpstream(t, `{"id":"chatcmpl_deepseek_safe_only","model":"deepseek-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"deepseek translated via chat"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":6,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":4},"completion_tokens_details":{"reasoning_tokens":3}}}`, http.Header{
			"Content-Type":     []string{"application/json"},
			"Content-Encoding": []string{"gzip"},
			"Digest":           []string{"sha-256=deepseek-safe-only"},
			"ETag":             []string{`"deepseek-safe-only"`},
			"X-Request-Id":     []string{"deepseek-safe-only-upstream"},
		})
		route := seedDeepSeekSafeOnlyFacadeRoute(t, harness, profileID, "gpt-5.5", primaryUpstream.baseURL("/deepseek-safe-only/primary"), secondaryUpstream.baseURL("/deepseek-safe-only/secondary"), deepseekUpstream.baseURL("/deepseek-safe-only/deepseek"))

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
			"model": route.PublicModelID,
			"input": "deepseek text-safe fallback",
		}, nil)
		assertStatus(t, response, http.StatusOK)
		assertTranslatedOpenAISafeHeaders(t, response, "deepseek-safe-only-upstream")
		var payload map[string]any
		decodeJSONResponse(t, response, &payload)
		output := payload["output"].([]any)
		message := output[0].(map[string]any)
		parts := message["content"].([]any)
		if len(parts) != 1 || parts[0].(map[string]any)["text"].(string) != "deepseek translated via chat" {
			t.Fatalf("expected translated deepseek output_text, got %+v", message["content"])
		}
		if got := len(primaryUpstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected inactive primary tier to avoid upstream requests, got %d", got)
		}
		if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected inactive secondary tier to avoid upstream requests, got %d", got)
		}
		request := deepseekUpstream.lastRequest(t)
		if request.Path != "/deepseek-safe-only/deepseek/v1/chat/completions" {
			t.Fatalf("expected translated deepseek upstream chat path, got %q", request.Path)
		}
		if got := requestModelID(t, request.Body); got != route.DeepSeekTargetModelID {
			t.Fatalf("expected deepseek upstream request model %q, got %q", route.DeepSeekTargetModelID, got)
		}
		assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.responses")
		assertLatestTranslatedIngressPlanningAttribution(t, harness.conn, profileID, "/v1/responses", route.DeepSeekConnectionID, "openai_responses_heuristic_v1")
		assertLatestRuntimeUsageRows(t, harness.conn, profileID, false, runtimePersistedUsageRow{
			InputTokens:          runtimeNullInt64(6),
			OutputTokens:         runtimeNullInt64(3),
			TotalTokens:          runtimeNullInt64(16),
			CacheReadInputTokens: runtimeNullInt64(4),
			ReasoningTokens:      runtimeNullInt64(3),
		})
		assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, route.DeepSeekTargetModelID)
	})

	t.Run("responses image input on deepseek-only tier rejects before transport", func(t *testing.T) {
		harness := newEnforcedRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		primaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "deepseek-image-primary"})
		secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "deepseek-image-secondary"})
		deepseekUpstream := newTranslatedRouteMatrixUpstream(t, `{"id":"chatcmpl_should_not_run","model":"deepseek-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"should not execute"},"finish_reason":"stop"}]}`, http.Header{"Content-Type": []string{"application/json"}})
		route := seedDeepSeekSafeOnlyFacadeRoute(t, harness, profileID, "gpt-5.5", primaryUpstream.baseURL("/deepseek-image/primary"), secondaryUpstream.baseURL("/deepseek-image/secondary"), deepseekUpstream.baseURL("/deepseek-image/deepseek"))

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
			"model": route.PublicModelID,
			"input": []map[string]any{{
				"type":      "input_image",
				"image_url": "https://example.invalid/deepseek-image.png",
			}},
		}, nil)
		assertStatus(t, response, http.StatusBadRequest)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "openai_request_translation_unsupported" || payload["detail"] != "Prism cannot translate this OpenAI request shape for the selected target." || payload["unsupported_reason"] != "responses_input_image" {
			t.Fatalf("expected responses image translated rejection payload, got %+v", payload)
		}
		if got := len(primaryUpstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected inactive primary tier to avoid upstream requests, got %d", got)
		}
		if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected inactive secondary tier to avoid upstream requests, got %d", got)
		}
		if got := len(deepseekUpstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected image-bearing request to stop before deepseek upstream transport, got %d", got)
		}
	})

	t.Run("chat image part on responses-only target rejects before transport", func(t *testing.T) {
		harness := newEnforcedRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chat-image-should-not-run"})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "chat-image-public", "chat-image-target", upstream.baseURL("/chat-image/rejection"), "chat-image-key", "responses_reasoning_none")

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model": route.PublicModelID,
			"messages": []map[string]any{{
				"role": "user",
				"content": []map[string]any{{
					"type":      "image_url",
					"image_url": map[string]any{"url": "https://example.invalid/chat-image.png"},
				}},
			}},
		}, nil)
		assertStatus(t, response, http.StatusBadRequest)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "openai_request_translation_unsupported" || payload["detail"] != "Prism cannot translate this OpenAI request shape for the selected target." || payload["unsupported_reason"] != "chat_image_part" {
			t.Fatalf("expected chat image-part translated rejection payload, got %+v", payload)
		}
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected chat image-part rejection to stop before upstream, got %d upstream requests", got)
		}
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

func (u *translatedRouteMatrixUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
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

type seededDeepSeekSafeOnlyFacadeRoute struct {
	PublicModelID          string
	PrimaryTargetModelID   string
	SecondaryTargetModelID string
	DeepSeekTargetModelID  string
	PrimaryConnectionID    int
	SecondaryConnectionID  int
	DeepSeekConnectionID   int
}

func seedDeepSeekSafeOnlyFacadeRoute(t *testing.T, harness *runtimeHarness, profileID int, publicModelPrefix string, primaryBaseURL string, secondaryBaseURL string, deepseekBaseURL string) seededDeepSeekSafeOnlyFacadeRoute {
	t.Helper()
	suffix := randomSuffix()
	responsesVariant := "responses_reasoning_none"
	chatVariant := "chat_completions_reasoning_none"
	route := seedOpenAIFacadeRoute(t, harness, profileID, publicModelPrefix+"-"+suffix, []facadeTargetSeed{
		{ModelID: "gpt-5.5-primary-" + suffix, EndpointBaseURL: primaryBaseURL, EndpointAPIKey: "deepseek-safe-only-primary-key-" + suffix, Weight: 1, OpenAIProbeEndpointVariant: &responsesVariant},
		{ModelID: "gpt-5.4-" + suffix, EndpointBaseURL: secondaryBaseURL, EndpointAPIKey: "deepseek-safe-only-secondary-key-" + suffix, Weight: 1, OpenAIProbeEndpointVariant: &responsesVariant},
		{ModelID: "deepseek-v4-flash-" + suffix, EndpointBaseURL: deepseekBaseURL, EndpointAPIKey: "deepseek-safe-only-deepseek-key-" + suffix, Weight: 1, OpenAIProbeEndpointVariant: &chatVariant},
	})
	deactivateRuntimeHarnessConnections(t, harness, route.ConnectionIDs[0], route.ConnectionIDs[1])
	return seededDeepSeekSafeOnlyFacadeRoute{
		PublicModelID:          route.PublicModelID,
		PrimaryTargetModelID:   route.TargetModelIDs[0],
		SecondaryTargetModelID: route.TargetModelIDs[1],
		DeepSeekTargetModelID:  route.TargetModelIDs[2],
		PrimaryConnectionID:    route.ConnectionIDs[0],
		SecondaryConnectionID:  route.ConnectionIDs[1],
		DeepSeekConnectionID:   route.ConnectionIDs[2],
	}
}

func deactivateRuntimeHarnessConnections(t *testing.T, harness *runtimeHarness, connectionIDs ...int) {
	t.Helper()
	if len(connectionIDs) == 0 {
		t.Fatal("expected at least one connection to deactivate")
	}
	now := time.Now().UTC()
	for _, connectionID := range connectionIDs {
		if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET is_active = FALSE, updated_at = $2 WHERE id = $1`, connectionID, now); err != nil {
			t.Fatalf("deactivate runtime harness connection %d: %v", connectionID, err)
		}
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{harness.profileIDForConnection(t, connectionIDs[0])}})
}

// correction note: removed duplicated assertion block and kept the requested-vs-resolved mismatch checks only.
