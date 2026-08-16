package runtimetest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestOperationRouteMatrixOpenAITextCapabilityMatrix(t *testing.T) {
	tests := []struct {
		name                  string
		requestPath           string
		requestBody           func(seededRuntimeRoute) map[string]any
		upstreamResponse      string
		textCapability        string
		requestedModelMode    string
		wantUpstreamPath      string
		wantOperationName     string
		wantUpstreamOperation string
		wantTranslationMode   string
		wantStatus            int
	}{
		{
			name:        "chat ingress stays native on chat-only target",
			requestPath: "/v1/chat/completions",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "native chat ingress"}}, "max_completion_tokens": 64}
			},
			upstreamResponse:      `{"id":"chatcmpl_native_chat","object":"chat.completion","created":1700000001,"model":"chat-only-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"native chat ingress"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
			textCapability:        "chat_completions_only",
			wantUpstreamPath:      "/v1/chat/completions",
			wantOperationName:     "openai.chat_completions",
			wantUpstreamOperation: "openai.chat_completions",
			wantTranslationMode:   "none",
			wantStatus:            http.StatusOK,
		},
		{
			name:        "responses ingress rejects chat-only target",
			requestPath: "/v1/responses",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "rejected responses ingress", "max_output_tokens": 64}
			},
			textCapability:     "chat_completions_only",
			requestedModelMode: "dual_native",
			wantStatus:         http.StatusBadRequest,
		},
		{
			name:        "chat ingress rejects responses-only target",
			requestPath: "/v1/chat/completions",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "rejected chat ingress"}}, "max_completion_tokens": 64}
			},
			textCapability:     "responses_only",
			requestedModelMode: "dual_native",
			wantStatus:         http.StatusBadRequest,
		},
		{
			name:        "responses ingress stays native on responses-only target",
			requestPath: "/v1/responses",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "native responses ingress", "max_output_tokens": 64}
			},
			upstreamResponse:      `{"id":"resp_native_responses","object":"response","created_at":1700000004,"model":"responses-only-upstream","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"native responses ingress"}]}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`,
			textCapability:        "responses_only",
			wantUpstreamPath:      "/v1/responses",
			wantOperationName:     "openai.responses",
			wantUpstreamOperation: "openai.responses",
			wantTranslationMode:   "none",
			wantStatus:            http.StatusOK,
		},
		{
			name:        "chat ingress stays native on dual-native target",
			requestPath: "/v1/chat/completions",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "dual native chat ingress"}}, "max_completion_tokens": 64}
			},
			upstreamResponse:      `{"id":"chatcmpl_dual_native_chat","object":"chat.completion","created":1700000005,"model":"dual-native-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"dual native chat ingress"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`,
			textCapability:        "dual_native",
			wantUpstreamPath:      "/v1/chat/completions",
			wantOperationName:     "openai.chat_completions",
			wantUpstreamOperation: "openai.chat_completions",
			wantTranslationMode:   "none",
			wantStatus:            http.StatusOK,
		},
		{
			name:        "responses ingress stays native on dual-native target",
			requestPath: "/v1/responses",
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "dual native responses ingress", "max_output_tokens": 64}
			},
			upstreamResponse:      `{"id":"resp_dual_native_responses","object":"response","created_at":1700000006,"model":"dual-native-upstream","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"dual native responses ingress"}]}],"usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`,
			textCapability:        "dual_native",
			wantUpstreamPath:      "/v1/responses",
			wantOperationName:     "openai.responses",
			wantUpstreamOperation: "openai.responses",
			wantTranslationMode:   "none",
			wantStatus:            http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newEnforcedRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newCapabilityRouteMatrixUpstream(t, test.upstreamResponse, http.Header{"Content-Type": []string{"application/json"}})
			endpointAPIKey := "route-matrix-capability-key-" + routeMatrixSlug(test.name)
			seed := runtimeRouteSeed{
				ProfileID:            profileID,
				APIFamily:            "openai",
				PublicModelID:        "route-matrix-capability-public",
				TargetModelID:        "route-matrix-capability-target",
				EndpointBaseURL:      upstream.baseURL(""),
				EndpointAPIKey:       endpointAPIKey,
				OpenAITextCapability: runtimeStringPtr(test.textCapability),
			}
			if test.requestedModelMode != "" {
				seed.OpenAIAcceptedFormat = runtimeStringPtr(test.requestedModelMode)
			}
			route := harness.seedProxyRoute(t, seed)

			response := harness.requestJSON(t, http.MethodPost, test.requestPath, test.requestBody(route), nil)
			if test.wantStatus != http.StatusOK {
				assertRouteMatrixUnsupportedWire(t, response)
				if got := len(upstream.requestsSnapshot()); got != 0 {
					t.Fatalf("expected incompatible target to reject before provider transport, got %d upstream calls", got)
				}
				return
			}
			assertStatus(t, response, test.wantStatus)
			requests := upstream.requestsSnapshot()
			if len(requests) != 1 {
				t.Fatalf("expected one upstream request, got %d", len(requests))
			}
			assertCapabilityRouteMatrixUpstreamRequest(t, requests[0], route, test.wantUpstreamPath, endpointAPIKey)
			assertRouteMatrixPersistedAttribution(t, harness, profileID, route.ConnectionID, test.wantOperationName, routeMatrixPersistedAttributionExpectation{
				upstreamOperationName: test.wantUpstreamOperation,
				translationMode:       test.wantTranslationMode,
				upstreamRequestPath:   test.wantUpstreamPath,
			})
		})
	}
}

func TestOperationRouteMatrixResponsesRejectsChatOnlyRequestedModelFormat(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newCapabilityRouteMatrixUpstream(t, `{"id":"unused"}`, http.Header{"Content-Type": []string{"application/json"}})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:            profileID,
		APIFamily:            "openai",
		PublicModelID:        "route-matrix-chat-format-public-" + randomSuffix(),
		TargetModelID:        "route-matrix-chat-format-target-" + randomSuffix(),
		EndpointBaseURL:      upstream.baseURL(""),
		EndpointAPIKey:       "route-matrix-chat-format-key",
		OpenAITextCapability: runtimeStringPtr("dual_native"),
	})
	tag, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET openai_accepted_format = 'chat_completions_only', updated_at = NOW() WHERE profile_id = $1 AND model_id = $2`, profileID, route.PublicModelID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("set requested model accepted format: rows=%d err=%v", tag.RowsAffected(), err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "reject model wire"}, nil)
	assertRouteMatrixOperationNotSupported(t, response)
	if got := len(upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected requested-model wire mismatch to reject before provider transport, got %d calls", got)
	}
}

func assertRouteMatrixOperationNotSupported(t *testing.T, response *http.Response) {
	t.Helper()
	assertStatus(t, response, http.StatusBadRequest)
	payload := runtimeResponsePayload(t, response)
	want := map[string]string{
		"error":            "openai_operation_not_supported",
		"detail":           "The requested model does not accept this OpenAI operation.",
		"translation_mode": "none",
	}
	for key, value := range want {
		if got, _ := payload[key].(string); got != value {
			t.Fatalf("expected response %s=%q, got %+v", key, value, payload)
		}
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
				upstream := newCapabilityRouteMatrixUpstream(t, test.upstreamResponse, http.Header{"Content-Type": []string{"application/json"}})
				endpointAPIKey := "route-matrix-adjunct-key-" + routeMatrixSlug(test.name) + "-" + capability
				route := harness.seedProxyRoute(t, runtimeRouteSeed{
					ProfileID:            profileID,
					APIFamily:            "openai",
					PublicModelID:        "route-matrix-adjunct-public-" + routeMatrixSlug(test.name),
					TargetModelID:        "route-matrix-adjunct-target-" + routeMatrixSlug(test.name),
					EndpointBaseURL:      upstream.baseURL(""),
					EndpointAPIKey:       endpointAPIKey,
					OpenAITextCapability: runtimeStringPtr(capability),
				})

				response := harness.requestJSON(t, http.MethodPost, test.requestPath, test.requestBody(route), nil)
				assertStatus(t, response, http.StatusOK)
				requests := upstream.requestsSnapshot()
				if len(requests) != 1 {
					t.Fatalf("expected one native adjunct upstream request, got %d", len(requests))
				}
				assertCapabilityRouteMatrixUpstreamRequest(t, requests[0], route, test.requestPath, endpointAPIKey)
				assertRouteMatrixPersistedAttribution(t, harness, profileID, route.ConnectionID, test.operationName, routeMatrixPersistedAttributionExpectation{
					upstreamOperationName: test.operationName,
					translationMode:       "none",
					upstreamRequestPath:   test.requestPath,
				})
			})
		}

		t.Run(test.name+" rejects chat-only", func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newCapabilityRouteMatrixUpstream(t, test.upstreamResponse, http.Header{"Content-Type": []string{"application/json"}})
			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:            profileID,
				APIFamily:            "openai",
				PublicModelID:        "route-matrix-adjunct-chat-only-public-" + routeMatrixSlug(test.name),
				TargetModelID:        "route-matrix-adjunct-chat-only-target-" + routeMatrixSlug(test.name),
				EndpointBaseURL:      upstream.baseURL(""),
				EndpointAPIKey:       "route-matrix-adjunct-chat-only-key",
				OpenAITextCapability: runtimeStringPtr("chat_completions_only"),
				OpenAIAcceptedFormat: runtimeStringPtr("dual_native"),
			})

			response := harness.requestJSON(t, http.MethodPost, test.requestPath, test.requestBody(route), nil)
			assertRouteMatrixUnsupportedWire(t, response)
			if got := len(upstream.requestsSnapshot()); got != 0 {
				t.Fatalf("expected chat-only adjunct target to reject before provider transport, got %d upstream calls", got)
			}
		})
	}
}

func assertCapabilityRouteMatrixUpstreamRequest(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute, wantPath string, endpointAPIKey string) {
	t.Helper()
	if request.Method != http.MethodPost {
		t.Fatalf("expected upstream POST, got %s", request.Method)
	}
	if request.Path != wantPath {
		t.Fatalf("expected upstream path %q, got %q", wantPath, request.Path)
	}
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected upstream body model %q, got %q in %s", route.TargetModelID, got, string(request.Body))
	}
	if request.Headers.Get("Authorization") != "Bearer "+endpointAPIKey {
		t.Fatalf("expected upstream bearer auth %q, got %q", "Bearer "+endpointAPIKey, request.Headers.Get("Authorization"))
	}
}

func assertRouteMatrixUnsupportedWire(t *testing.T, response *http.Response) {
	t.Helper()
	if response.StatusCode == http.StatusBadRequest {
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "openai_request_translation_unsupported" {
			t.Fatalf("expected native incompatibility error, got %+v", payload)
		}
		return
	}
	assertStatus(t, response, http.StatusServiceUnavailable)
	payload := runtimeResponsePayload(t, response)
	want := map[string]string{
		"error":  "openai_no_compatible_terminal_target",
		"detail": "No configured terminal target can natively serve this OpenAI operation for the requested model.",
	}
	for key, value := range want {
		if got, _ := payload[key].(string); got != value {
			t.Fatalf("expected response %s=%q, got %+v", key, value, payload)
		}
	}
}

type capabilityRouteMatrixUpstream struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []upstreamRequestSnapshot
}

func newCapabilityRouteMatrixUpstream(t *testing.T, responseBody string, responseHeaders http.Header) *capabilityRouteMatrixUpstream {
	t.Helper()
	upstream := &capabilityRouteMatrixUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read route-matrix upstream body: %v", err)
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

func (u *capabilityRouteMatrixUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *capabilityRouteMatrixUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}
