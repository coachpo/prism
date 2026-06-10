package runtime_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOperationRouteMatrixOpenAITextCapabilityMatrix(t *testing.T) {
	tests := []struct {
		name                  string
		requestPath           string
		requestBody           func(seededRuntimeRoute) map[string]any
		upstreamResponse      string
		probeVariant          string
		textCapability        string
		wantUpstreamPath      string
		wantOperationName     string
		wantUpstreamOperation string
		wantTranslationMode   string
	}{
		{
			name:        "chat ingress stays native on chat-only target",
			requestPath: "/v1/chat/completions",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "native chat ingress"}}, "max_completion_tokens": 64}
			},
			upstreamResponse:      `{"id":"chatcmpl_native_chat","object":"chat.completion","created":1700000001,"model":"chat-only-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"native chat ingress"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
			probeVariant:          "chat_completions_reasoning_none",
			textCapability:        "chat_completions_only",
			wantUpstreamPath:      "/v1/chat/completions",
			wantOperationName:     "openai.chat_completions",
			wantUpstreamOperation: "openai.chat_completions",
			wantTranslationMode:   "none",
		},
		{
			name:        "responses ingress translates to chat-only target",
			requestPath: "/v1/responses",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "translated responses ingress", "max_output_tokens": 64}
			},
			upstreamResponse:      `{"id":"chatcmpl_translated_responses","object":"chat.completion","created":1700000002,"model":"chat-only-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"translated responses ingress"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
			probeVariant:          "chat_completions_reasoning_none",
			textCapability:        "chat_completions_only",
			wantUpstreamPath:      "/v1/chat/completions",
			wantOperationName:     "openai.responses",
			wantUpstreamOperation: "openai.chat_completions",
			wantTranslationMode:   "openai_responses_to_chat_completions",
		},
		{
			name:        "chat ingress translates to responses-only target",
			requestPath: "/v1/chat/completions",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "translated chat ingress"}}, "max_completion_tokens": 64}
			},
			upstreamResponse:      `{"id":"resp_translated_chat","object":"response","created_at":1700000003,"model":"responses-only-upstream","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"translated chat ingress"}]}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`,
			probeVariant:          "responses_reasoning_none",
			textCapability:        "responses_only",
			wantUpstreamPath:      "/v1/responses",
			wantOperationName:     "openai.chat_completions",
			wantUpstreamOperation: "openai.responses",
			wantTranslationMode:   "openai_chat_completions_to_responses",
		},
		{
			name:        "responses ingress stays native on responses-only target",
			requestPath: "/v1/responses",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "native responses ingress", "max_output_tokens": 64}
			},
			upstreamResponse:      `{"id":"resp_native_responses","object":"response","created_at":1700000004,"model":"responses-only-upstream","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"native responses ingress"}]}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`,
			probeVariant:          "responses_reasoning_none",
			textCapability:        "responses_only",
			wantUpstreamPath:      "/v1/responses",
			wantOperationName:     "openai.responses",
			wantUpstreamOperation: "openai.responses",
			wantTranslationMode:   "none",
		},
		{
			name:        "chat ingress stays native on dual-native target",
			requestPath: "/v1/chat/completions",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "dual native chat ingress"}}, "max_completion_tokens": 64}
			},
			upstreamResponse:      `{"id":"chatcmpl_dual_native_chat","object":"chat.completion","created":1700000005,"model":"dual-native-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"dual native chat ingress"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
			probeVariant:          "responses_reasoning_none",
			textCapability:        "dual_native",
			wantUpstreamPath:      "/v1/chat/completions",
			wantOperationName:     "openai.chat_completions",
			wantUpstreamOperation: "openai.chat_completions",
			wantTranslationMode:   "none",
		},
		{
			name:        "responses ingress stays native on dual-native target",
			requestPath: "/v1/responses",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "dual native responses ingress", "max_output_tokens": 64}
			},
			upstreamResponse:      `{"id":"resp_dual_native_responses","object":"response","created_at":1700000006,"model":"dual-native-upstream","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"dual native responses ingress"}]}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`,
			probeVariant:          "chat_completions_reasoning_none",
			textCapability:        "dual_native",
			wantUpstreamPath:      "/v1/responses",
			wantOperationName:     "openai.responses",
			wantUpstreamOperation: "openai.responses",
			wantTranslationMode:   "none",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newEnforcedRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newTranslatedRouteMatrixUpstream(t, test.upstreamResponse, http.Header{"Content-Type": []string{"application/json"}})
			endpointAPIKey := "route-matrix-capability-key-" + routeMatrixSlug(test.name)
			route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "route-matrix-capability-public", "route-matrix-capability-target", upstream.baseURL(""), endpointAPIKey, test.probeVariant, test.textCapability)

			response := harness.requestJSON(t, http.MethodPost, test.requestPath, test.requestBody(route), nil)
			assertStatus(t, response, http.StatusOK)
			requests := upstream.requestsSnapshot()
			if len(requests) != 1 {
				t.Fatalf("expected one upstream request, got %d", len(requests))
			}
			assertTranslatedRouteMatrixUpstreamRequest(t, requests[0], route, test.wantUpstreamPath, endpointAPIKey)
			assertRouteMatrixPersistedAttribution(t, harness, profileID, test.wantOperationName, routeMatrixPersistedAttributionExpectation{
				upstreamOperationName: test.wantUpstreamOperation,
				translationMode:       test.wantTranslationMode,
				upstreamRequestPath:   test.wantUpstreamPath,
			})
		})
	}
}

func TestOperationRouteMatrixResponsesAdjunctCapabilityMatrix(t *testing.T) {
	tests := []struct {
		name             string
		requestPath      string
		requestBody      func(seededRuntimeRoute) map[string]any
		upstreamResponse string
		operationName    string
	}{
		{
			name:        "input tokens",
			requestPath: "/v1/responses/input_tokens",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "count input tokens", "stream": true}
			},
			upstreamResponse: `{"input_tokens":17,"total_tokens":17}`,
			operationName:    "openai.responses.input_tokens",
		},
		{
			name:        "compact",
			requestPath: "/v1/responses/compact",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "compact responses context", "stream": true}
			},
			upstreamResponse: `{"id":"resp_compact_capability","object":"response","status":"completed","usage":{"input_tokens":17,"output_tokens":2,"total_tokens":19}}`,
			operationName:    "openai.responses.compact",
		},
	}

	for _, test := range tests {
		for _, capability := range []string{"responses_only", "dual_native"} {
			t.Run(test.name+" native "+capability, func(t *testing.T) {
				harness := newRuntimeHarness(t)
				profileID := harness.activeProfileID(t)
				upstream := newTranslatedRouteMatrixUpstream(t, test.upstreamResponse, http.Header{"Content-Type": []string{"application/json"}})
				endpointAPIKey := "route-matrix-adjunct-key-" + routeMatrixSlug(test.name) + "-" + capability
				probeVariant := "responses_reasoning_none"
				route := harness.seedProxyRoute(t, runtimeRouteSeed{
					ProfileID:                  profileID,
					APIFamily:                  "openai",
					PublicModelID:              "route-matrix-adjunct-public-" + routeMatrixSlug(test.name),
					TargetModelID:              "route-matrix-adjunct-target-" + routeMatrixSlug(test.name),
					EndpointBaseURL:            upstream.baseURL(""),
					EndpointAPIKey:             endpointAPIKey,
					OpenAIProbeEndpointVariant: &probeVariant,
					OpenAITextCapability:       runtimeStringPtr(capability),
				})

				response := harness.requestJSON(t, http.MethodPost, test.requestPath, test.requestBody(route), nil)
				assertStatus(t, response, http.StatusOK)
				requests := upstream.requestsSnapshot()
				if len(requests) != 1 {
					t.Fatalf("expected one native adjunct upstream request, got %d", len(requests))
				}
				assertTranslatedRouteMatrixUpstreamRequest(t, requests[0], route, test.requestPath, endpointAPIKey)
				assertRouteMatrixPersistedAttribution(t, harness, profileID, test.operationName, routeMatrixPersistedAttributionExpectation{
					upstreamOperationName: test.operationName,
					translationMode:       "none",
					upstreamRequestPath:   test.requestPath,
				})
			})
		}

		t.Run(test.name+" rejects chat-only", func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newTranslatedRouteMatrixUpstream(t, test.upstreamResponse, http.Header{"Content-Type": []string{"application/json"}})
			probeVariant := "chat_completions_reasoning_none"
			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:                  profileID,
				APIFamily:                  "openai",
				PublicModelID:              "route-matrix-adjunct-chat-only-public-" + routeMatrixSlug(test.name),
				TargetModelID:              "route-matrix-adjunct-chat-only-target-" + routeMatrixSlug(test.name),
				EndpointBaseURL:            upstream.baseURL(""),
				EndpointAPIKey:             "route-matrix-adjunct-chat-only-key",
				OpenAIProbeEndpointVariant: &probeVariant,
				OpenAITextCapability:       runtimeStringPtr("chat_completions_only"),
			})

			response := harness.requestJSON(t, http.MethodPost, test.requestPath, test.requestBody(route), nil)
			assertTranslatedRouteMatrixNoEligibleTargets(t, response, route.PublicModelID)
			if got := len(upstream.requestsSnapshot()); got != 0 {
				t.Fatalf("expected chat-only adjunct target to reject before provider transport, got %d upstream calls", got)
			}
		})
	}
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
