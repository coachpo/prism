package runtimetest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

const customRequestParametersFixture = `{"provider":{"only":["deepinfra/turbo"],"allow_fallbacks":false},"temperature":0.25}`

func TestCustomRequestParametersNineOperationMatrix(t *testing.T) {
	operations := []struct {
		name         string
		apiFamily    string
		requestPath  func(route seededRuntimeRoute) string
		requestBody  func(route seededRuntimeRoute) map[string]any
		wantOverride func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute)
	}{
		{
			name:      "openai.chat_completions",
			apiFamily: "openai",
			requestPath: func(route seededRuntimeRoute) string {
				return "/v1/chat/completions"
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "matrix overlay"}}}
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				if requestModelID(t, request.Body) != route.TargetModelID {
					t.Fatalf("expected model rewrite to %q, got %q", route.TargetModelID, string(request.Body))
				}
			},
		},
		{
			name:      "openai.responses",
			apiFamily: "openai",
			requestPath: func(route seededRuntimeRoute) string {
				return "/v1/responses"
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "matrix overlay"}
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				if requestModelID(t, request.Body) != route.TargetModelID {
					t.Fatalf("expected model rewrite to %q, got %q", route.TargetModelID, string(request.Body))
				}
			},
		},
		{
			name:      "openai.responses.input_tokens",
			apiFamily: "openai",
			requestPath: func(route seededRuntimeRoute) string {
				return "/v1/responses/input_tokens"
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": "matrix overlay"}
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				if requestModelID(t, request.Body) != route.TargetModelID {
					t.Fatalf("expected model rewrite to %q, got %q", route.TargetModelID, string(request.Body))
				}
			},
		},
		{
			name:      "openai.responses.compact",
			apiFamily: "openai",
			requestPath: func(route seededRuntimeRoute) string {
				return "/v1/responses/compact"
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "input": []map[string]any{{"role": "user", "content": "matrix overlay"}}}
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				if requestModelID(t, request.Body) != route.TargetModelID {
					t.Fatalf("expected model rewrite to %q, got %q", route.TargetModelID, string(request.Body))
				}
			},
		},
		{
			name:      "anthropic.messages",
			apiFamily: "anthropic",
			requestPath: func(route seededRuntimeRoute) string {
				return "/v1/messages"
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "max_tokens": 64, "messages": []map[string]any{{"role": "user", "content": "matrix overlay"}}}
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				if requestModelID(t, request.Body) != route.TargetModelID {
					t.Fatalf("expected model rewrite to %q, got %q", route.TargetModelID, string(request.Body))
				}
			},
		},
		{
			name:      "anthropic.count_tokens",
			apiFamily: "anthropic",
			requestPath: func(route seededRuntimeRoute) string {
				return "/v1/messages/count_tokens"
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "matrix overlay"}}}
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				if requestModelID(t, request.Body) != route.TargetModelID {
					t.Fatalf("expected model rewrite to %q, got %q", route.TargetModelID, string(request.Body))
				}
			},
		},
		{
			name:      "gemini.generate_content",
			apiFamily: "gemini",
			requestPath: func(route seededRuntimeRoute) string {
				return fmt.Sprintf("/v1beta/models/%s:generateContent", route.PublicModelID)
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return routeMatrixGeminiBody(route.PublicModelID, "matrix overlay", 0.7)
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				// Path-bound: body model stays as sent.
			},
		},
		{
			name:      "gemini.stream_generate_content",
			apiFamily: "gemini",
			requestPath: func(route seededRuntimeRoute) string {
				return fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID)
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				body := routeMatrixGeminiBody(route.PublicModelID, "matrix overlay", 0.7)
				body["stream"] = true
				return body
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				// Path-bound: body model stays as sent.
			},
		},
		{
			name:      "gemini.count_tokens",
			apiFamily: "gemini",
			requestPath: func(route seededRuntimeRoute) string {
				return fmt.Sprintf("/v1beta/models/%s:countTokens", route.PublicModelID)
			},
			requestBody: func(route seededRuntimeRoute) map[string]any {
				return routeMatrixGeminiBody(route.PublicModelID, "matrix overlay", 0.7)
			},
			wantOverride: func(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute) {
				// Path-bound: body model stays as sent.
			},
		},
	}
	if len(operations) != 9 {
		t.Fatalf("matrix must cover exactly 9 provider-forwarded POST operations, got %d", len(operations))
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newRouteMatrixUpstream(t, "application/json", []byte(`{"ok":true}`))
			slug := routeMatrixSlug(operation.name)
			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:               profileID,
				APIFamily:               operation.apiFamily,
				PublicModelID:           "custom-params-matrix-public-" + slug + "-" + randomSuffix(),
				TargetModelID:           "custom-params-matrix-target-" + slug + "-" + randomSuffix(),
				EndpointBaseURL:         upstream.baseURL("/custom-params-matrix/" + slug),
				EndpointAPIKey:          "custom-params-matrix-key-" + slug,
				CustomHeaders:           map[string]any{"X-Route-Matrix": "custom-params-" + slug},
				CustomRequestParameters: runtimeStringPtr(customRequestParametersFixture),
				OpenAITextCapability:    routeMatrixOpenAITextCapability(operation.name),
			})
			response := harness.requestJSON(t, http.MethodPost, operation.requestPath(route), operation.requestBody(route), nil)
			assertStatus(t, response, http.StatusOK)

			upstreamRequest := upstream.lastRequest(t)
			operation.wantOverride(t, upstreamRequest, route)

			var body map[string]any
			if err := json.Unmarshal(upstreamRequest.Body, &body); err != nil {
				t.Fatalf("decode upstream body: %v", err)
			}
			provider := asMapRuntime(t, body["provider"])
			only := jsonStringListRuntime(t, provider["only"])
			if len(only) != 1 || only[0] != "deepinfra/turbo" || provider["allow_fallbacks"] != false {
				t.Fatalf("expected overlay provider to win for %s, got %+v", operation.name, body["provider"])
			}
			if body["temperature"] != 0.25 {
				t.Fatalf("expected overlay temperature to win for %s, got %+v", operation.name, body["temperature"])
			}
			// Client content and stream shape must survive.
			for _, protected := range []string{"messages", "input", "contents"} {
				if _, present := body[protected]; present {
					raw, _ := json.Marshal(body[protected])
					if !strings.Contains(string(raw), "matrix overlay") {
						t.Fatalf("expected client %s content to survive for %s, got %s", protected, operation.name, raw)
					}
				}
			}
			if strings.EqualFold(operation.name, "gemini.stream_generate_content") && body["stream"] != true {
				t.Fatalf("expected stream flag to survive for gemini streaming, got %+v", body["stream"])
			}
		})
	}
}

func TestCustomRequestParametersLocalModelsListUnaffected(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:               profileID,
		APIFamily:               "openai",
		PublicModelID:           "custom-params-models-public-" + randomSuffix(),
		TargetModelID:           "custom-params-models-target-" + randomSuffix(),
		EndpointBaseURL:         "http://127.0.0.1:1/unused",
		EndpointAPIKey:          "unused",
		CustomRequestParameters: runtimeStringPtr(customRequestParametersFixture),
	})
	response := harness.requestJSON(t, http.MethodGet, "/v1/models", nil, nil)
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	found := false
	for _, item := range payload["data"].([]any) {
		if asMapRuntime(t, item)["id"] == route.PublicModelID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected local /v1/models to list the configured public model, got %+v", payload["data"])
	}
}

func TestCustomRequestParametersFailoverPerAttemptIsolation(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "custom-params-failover-public-" + suffix
	targetModelID := "custom-params-failover-target-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-custom-params-secondary"})
	strategyID := harness.seedLegacyStrategy(t, profileID, "custom-params-fill-first-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "custom-params-primary-endpoint-"+suffix, primaryUpstream.baseURL("/custom-params/failover/primary"), "primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "custom-params-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/custom-params/failover/secondary"), "secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "custom-params-primary-connection-"+suffix, nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "custom-params-secondary-connection-"+suffix, nil, nil, 1)
	harness.updateConnectionCustomRequestParameters(t, profileID, primaryConnectionID, `{"temperature":0.1,"provider":{"only":["primary-provider"]}}`)
	harness.updateConnectionCustomRequestParameters(t, profileID, secondaryConnectionID, `{"temperature":0.9,"provider":{"only":["secondary-provider"]}}`)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "failover isolation"}}, "model": publicModelID, "temperature": 0.5},
		nil,
	)
	assertStatus(t, response, http.StatusOK)

	primaryRequests := primaryUpstream.requestsSnapshot()
	secondaryRequests := secondaryUpstream.requestsSnapshot()
	if len(primaryRequests) != 1 || len(secondaryRequests) != 1 {
		t.Fatalf("expected one attempt per upstream, got primary=%d secondary=%d", len(primaryRequests), len(secondaryRequests))
	}
	assertOverlayedBody(t, primaryRequests[0].Body, `{"only":["primary-provider"]}`)
	assertOverlayedBody(t, secondaryRequests[0].Body, `{"only":["secondary-provider"]}`)

	// The client temperature must not survive either attempt, and attempt 2
	// must not inherit attempt 1's parameters.
	for _, snapshot := range []upstreamRequestSnapshot{primaryRequests[0], secondaryRequests[0]} {
		var body map[string]any
		if err := json.Unmarshal(snapshot.Body, &body); err != nil {
			t.Fatalf("decode attempt body: %v", err)
		}
		if body["temperature"] == 0.5 {
			t.Fatalf("client temperature leaked into an overlayed attempt: %s", snapshot.Body)
		}
	}
}

func TestCustomRequestParametersSequentialCandidateIsolation(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "custom-params-candidate-public-" + suffix
	targetModelID := "custom-params-candidate-target-" + suffix
	firstUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "first candidate down"})
	secondUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-candidate-second"})
	strategyID := harness.seedLegacyStrategy(t, profileID, "custom-params-candidate-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	firstEndpointID := harness.seedEndpoint(t, profileID, "custom-params-candidate-first-endpoint-"+suffix, firstUpstream.baseURL("/custom-params/candidate/first"), "first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "custom-params-candidate-second-endpoint-"+suffix, secondUpstream.baseURL("/custom-params/candidate/second"), "second-key", 1)
	firstConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, firstEndpointID, "custom-params-candidate-first-connection-"+suffix, nil, nil, 0)
	secondConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondEndpointID, "custom-params-candidate-second-connection-"+suffix, nil, nil, 1)
	harness.updateConnectionCustomRequestParameters(t, profileID, firstConnectionID, `{"provider":{"only":["candidate-first-provider"]}}`)
	harness.updateConnectionCustomRequestParameters(t, profileID, secondConnectionID, `{"provider":{"only":["candidate-second-provider"]}}`)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "candidate isolation"}}, "model": publicModelID},
		nil,
	)
	assertStatus(t, response, http.StatusOK)

	// Both candidates are materialized up front with their own Connection
	// configuration; after the first fails, the second attempt must carry its
	// own overlay with no residue from the first.
	firstRequests := firstUpstream.requestsSnapshot()
	secondRequests := secondUpstream.requestsSnapshot()
	if len(firstRequests) != 1 || len(secondRequests) != 1 {
		t.Fatalf("expected both candidates to be attempted, got first=%d second=%d", len(firstRequests), len(secondRequests))
	}
	assertOverlayedBody(t, firstRequests[0].Body, `{"only":["candidate-first-provider"]}`)
	assertOverlayedBody(t, secondRequests[0].Body, `{"only":["candidate-second-provider"]}`)
}

func TestCustomRequestParametersGeminiStreamingMaterializesBody(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "text/event-stream", []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"materialized stream\"}]}}],\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":17,\"totalTokenCount\":28}}\n\n"))
	suffix := randomSuffix()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:               profileID,
		APIFamily:               "gemini",
		PublicModelID:           "custom-params-stream-public-" + suffix,
		TargetModelID:           "custom-params-stream-target-" + suffix,
		EndpointBaseURL:         upstream.baseURL("/custom-params/gemini-stream"),
		EndpointAPIKey:          "custom-params-stream-key",
		CustomRequestParameters: runtimeStringPtr(`{"generationConfig":{"temperature":0.42}}`),
	})
	requestPath := fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID)
	rawBody := mustMarshalBenchmarkJSON(t, routeMatrixGeminiBody(route.PublicModelID, strings.Repeat("streaming materialization ", 2048), 0.7))

	response := performRuntimeRawRequest(t, harness, http.MethodPost, requestPath, rawBody, "application/json")
	assertStatus(t, response, http.StatusOK)
	if !strings.Contains(readResponseBody(t, response), "materialized stream") {
		t.Fatalf("expected streamed response passthrough, got %s", readResponseBody(t, response))
	}

	upstreamRequest := upstream.lastRequest(t)
	var body map[string]any
	if err := json.Unmarshal(upstreamRequest.Body, &body); err != nil {
		t.Fatalf("decode materialized stream body: %v", err)
	}
	generationConfig := asMapRuntime(t, body["generationConfig"])
	if generationConfig["temperature"] != 0.42 {
		t.Fatalf("expected overlay to materialize the streaming body, got %+v", body["generationConfig"])
	}
	if !strings.Contains(fmt.Sprint(body["contents"]), "streaming materialization") {
		t.Fatalf("expected client contents to survive, got %+v", body["contents"])
	}
}

func TestCustomRequestParametersErrorBoundaries(t *testing.T) {
	t.Run("non object ingress rejected before transport", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "never"})
		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:               profileID,
			APIFamily:               "gemini",
			PublicModelID:           "custom-params-err-public-" + randomSuffix(),
			TargetModelID:           "custom-params-err-target-" + randomSuffix(),
			EndpointBaseURL:         upstream.baseURL("/custom-params/errors"),
			EndpointAPIKey:          "err-key",
			CustomRequestParameters: runtimeStringPtr(`{"provider":{"only":["x"]}}`),
		})
		requestPath := fmt.Sprintf("/v1beta/models/%s:generateContent", route.PublicModelID)
		response := performRuntimeRawRequest(t, harness, http.MethodPost, requestPath, []byte(`[1,2,3]`), "application/json")
		assertRuntimeErrorResponse(t, response, http.StatusBadRequest, "Request body must be a JSON object when custom request parameters are configured")
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected no provider transport after object validation failure, got %d requests", got)
		}
	})

	t.Run("gemini non identity encoding rejected 415", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "never"})
		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:               profileID,
			APIFamily:               "gemini",
			PublicModelID:           "custom-params-415-public-" + randomSuffix(),
			TargetModelID:           "custom-params-415-target-" + randomSuffix(),
			EndpointBaseURL:         upstream.baseURL("/custom-params/415"),
			EndpointAPIKey:          "err-key",
			CustomRequestParameters: runtimeStringPtr(`{"provider":{"only":["x"]}}`),
		})
		requestPath := fmt.Sprintf("/v1beta/models/%s:generateContent", route.PublicModelID)
		body := mustMarshalBenchmarkJSON(t, routeMatrixGeminiBody(route.PublicModelID, "gzip body", 0.7))
		request, requestErr := http.NewRequest(http.MethodPost, harness.url+requestPath, bytes.NewReader(body))
		if requestErr != nil {
			t.Fatalf("build gzip runtime request: %v", requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Content-Encoding", "gzip")
		response, requestErr := harness.client.Do(request)
		if requestErr != nil {
			t.Fatalf("perform gzip runtime request: %v", requestErr)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		assertRuntimeErrorResponse(t, response, http.StatusUnsupportedMediaType, "Content-Encoding is not supported when custom request parameters are configured")
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected no provider transport after 415, got %d requests", got)
		}
	})

	t.Run("gemini non identity encoding without config keeps existing behavior", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "passthrough"})
		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:       profileID,
			APIFamily:       "gemini",
			PublicModelID:   "custom-params-no415-public-" + randomSuffix(),
			TargetModelID:   "custom-params-no415-target-" + randomSuffix(),
			EndpointBaseURL: upstream.baseURL("/custom-params/no415"),
			EndpointAPIKey:  "err-key",
		})
		requestPath := fmt.Sprintf("/v1beta/models/%s:generateContent", route.PublicModelID)
		body := mustMarshalBenchmarkJSON(t, routeMatrixGeminiBody(route.PublicModelID, "gzip body", 0.7))
		request, requestErr := http.NewRequest(http.MethodPost, harness.url+requestPath, bytes.NewReader(body))
		if requestErr != nil {
			t.Fatalf("build gzip runtime request: %v", requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Content-Encoding", "gzip")
		response, requestErr := harness.client.Do(request)
		if requestErr != nil {
			t.Fatalf("perform gzip runtime request: %v", requestErr)
		}
		t.Cleanup(func() { _ = response.Body.Close() })
		assertStatus(t, response, http.StatusOK)
	})

	t.Run("merged body over limit rejected 413", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "never"})
		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:               profileID,
			APIFamily:               "openai",
			PublicModelID:           "custom-params-413-public-" + randomSuffix(),
			TargetModelID:           "custom-params-413-target-" + randomSuffix(),
			EndpointBaseURL:         upstream.baseURL("/custom-params/413"),
			EndpointAPIKey:          "err-key",
			CustomRequestParameters: runtimeStringPtr(`{"padding":"` + strings.Repeat("x", 65500) + `"}`),
			OpenAITextCapability:    runtimeStringPtr("chat_completions_only"),
		})
		// Ingress body under the 20 MiB limit; the configured overlay (up to
		// 64 KiB) pushes the merged body past it.
		ingressPadding := strings.Repeat("y", 20*1024*1024-65536+2048)
		body := map[string]any{
			"model":    route.PublicModelID,
			"messages": []map[string]any{{"role": "user", "content": ingressPadding}},
		}
		rawBody := mustMarshalBenchmarkJSON(t, body)
		response := performRuntimeRawRequest(t, harness, http.MethodPost, "/v1/chat/completions", rawBody, "application/json")
		if response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413 for oversized merged body, got %d with body %s", response.StatusCode, readResponseBody(t, response))
		}
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected no provider transport after 413, got %d requests", got)
		}
	})
}

func TestCustomRequestParametersHeaderCleanup(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-headers"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:               profileID,
		APIFamily:               "openai",
		PublicModelID:           "custom-params-headers-public-" + randomSuffix(),
		TargetModelID:           "custom-params-headers-target-" + randomSuffix(),
		EndpointBaseURL:         upstream.baseURL("/custom-params/headers"),
		EndpointAPIKey:          "header-key",
		CustomHeaders:           map[string]any{"Content-MD5": "stale-md5-from-connection", "Digest": "sha-256=stale-from-connection"},
		CustomRequestParameters: runtimeStringPtr(`{"provider":{"only":["deepinfra/turbo"]}}`),
		OpenAITextCapability:    runtimeStringPtr("chat_completions_only"),
	})

	request, requestErr := http.NewRequest(http.MethodPost, harness.url+"/v1/chat/completions", bytes.NewReader(mustMarshalBenchmarkJSON(t, map[string]any{
		"model":    route.PublicModelID,
		"messages": []map[string]any{{"role": "user", "content": "header cleanup"}},
	})))
	if requestErr != nil {
		t.Fatalf("build header cleanup request: %v", requestErr)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-MD5", "stale-md5-from-client")
	request.Header.Set("Content-Digest", "sha-256=stale-digest-from-client")
	request.Header.Set("Digest", "sha-256=stale-digest-from-client-2")
	response, requestErr := harness.client.Do(request)
	if requestErr != nil {
		t.Fatalf("perform header cleanup request: %v", requestErr)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	assertStatus(t, response, http.StatusOK)

	captured := upstream.lastRequest(t)
	for _, stale := range []string{"Content-MD5", "Content-Digest", "Digest", "Content-Encoding"} {
		if value := captured.Headers.Get(stale); value != "" {
			t.Fatalf("expected stale body-dependent header %s to be stripped, got %q", stale, value)
		}
	}
	if value := captured.Headers.Get("Content-Length"); value != fmt.Sprint(len(captured.Body)) {
		t.Fatalf("expected Content-Length to match the merged body (%d), got %q", len(captured.Body), value)
	}
	var body map[string]any
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("decode cleaned body: %v", err)
	}
	provider := asMapRuntime(t, body["provider"])
	if jsonStringListRuntime(t, provider["only"])[0] != "deepinfra/turbo" {
		t.Fatalf("expected overlay to survive header cleanup, got %+v", body["provider"])
	}
}

func TestCustomRequestParametersSnapshotRefreshAppliesNewValue(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-snapshot"})
	suffix := randomSuffix()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:               profileID,
		APIFamily:               "openai",
		PublicModelID:           "custom-params-snapshot-public-" + suffix,
		TargetModelID:           "custom-params-snapshot-target-" + suffix,
		EndpointBaseURL:         upstream.baseURL("/custom-params/snapshot"),
		EndpointAPIKey:          "snapshot-key",
		CustomRequestParameters: runtimeStringPtr(`{"provider":{"only":["first-provider"]}}`),
		OpenAITextCapability:    runtimeStringPtr("chat_completions_only"),
	})

	first := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "snapshot one"}}}, nil)
	assertStatus(t, first, http.StatusOK)
	assertOverlayedBody(t, upstream.lastRequest(t).Body, `{"only":["first-provider"]}`)

	// Commit a new value and refresh the planning snapshot; the next request
	// must use the new immutable snapshot.
	harness.updateConnectionCustomRequestParameters(t, profileID, route.ConnectionID, `{"provider":{"only":["second-provider"]}}`)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	second := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "snapshot two"}}}, nil)
	assertStatus(t, second, http.StatusOK)
	requests := upstream.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected two captured requests, got %d", len(requests))
	}
	assertOverlayedBody(t, requests[1].Body, `{"only":["second-provider"]}`)
}

func TestCustomRequestParametersAuditCapturesMergedBody(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	seedRuntimeAuditFamilySetting(t, harness, profileID, "openai", true, true)
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-audit"})
	suffix := randomSuffix()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:               profileID,
		APIFamily:               "openai",
		PublicModelID:           "custom-params-audit-public-" + suffix,
		TargetModelID:           "custom-params-audit-target-" + suffix,
		EndpointBaseURL:         upstream.baseURL("/custom-params/audit"),
		EndpointAPIKey:          "audit-key",
		CustomRequestParameters: runtimeStringPtr(`{"provider":{"only":["deepinfra/turbo"]}}`),
		OpenAITextCapability:    runtimeStringPtr("chat_completions_only"),
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "audit merged body"}}}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	var requestBody sqlNullStringForTest
	waitForRuntimeAuditRequestBody(t, harness, profileID, &requestBody)
	if !requestBody.valid || !strings.Contains(requestBody.value, "deepinfra/turbo") {
		t.Fatalf("expected audit body capture to store the merged upstream body, got %q", requestBody.value)
	}
}

func waitForRuntimeAuditRequestBody(t *testing.T, harness *runtimeHarness, profileID int, target *sqlNullStringForTest) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var requestBody sqlNullStringForTest
		if err := harness.conn.QueryRow(context.Background(), `SELECT request_body FROM audit_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&requestBody); err == nil && requestBody.valid {
			*target = requestBody
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("audit request body never became available for profile %d", profileID)
}

func TestCustomRequestParametersGenerationParamsPerAttempt(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "custom-params-genparams-public-" + suffix
	targetModelID := "custom-params-genparams-target-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "primary down"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-genparams-secondary"})
	strategyID := harness.seedLegacyStrategy(t, profileID, "custom-params-genparams-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "custom-params-genparams-primary-endpoint-"+suffix, primaryUpstream.baseURL("/custom-params/genparams/primary"), "primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "custom-params-genparams-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/custom-params/genparams/secondary"), "secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "custom-params-genparams-primary-connection-"+suffix, nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "custom-params-genparams-secondary-connection-"+suffix, nil, nil, 1)
	harness.updateConnectionCustomRequestParameters(t, profileID, primaryConnectionID, `{"temperature":0.1,"max_completion_tokens":11}`)
	harness.updateConnectionCustomRequestParameters(t, profileID, secondaryConnectionID, `{"temperature":0.9,"max_completion_tokens":99}`)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{"messages": []map[string]any{{"role": "user", "content": "per attempt gen params"}}, "model": publicModelID, "temperature": 0.5, "max_completion_tokens": 50},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)
	rows, err := harness.conn.Query(context.Background(), `SELECT connection_id, request_generation_params FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number ASC`, profileID, ingressRequestID)
	if err != nil {
		t.Fatalf("load per-attempt generation parameters: %v", err)
	}
	defer rows.Close()
	type generationRow struct {
		connectionID int
		raw          sqlNullStringForTest
	}
	generationRows := make([]generationRow, 0, 2)
	for rows.Next() {
		var row generationRow
		if err := rows.Scan(&row.connectionID, &row.raw); err != nil {
			t.Fatalf("scan per-attempt generation parameters: %v", err)
		}
		generationRows = append(generationRows, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate per-attempt generation parameters: %v", err)
	}
	if len(generationRows) != 2 {
		t.Fatalf("expected two request-log rows, got %d", len(generationRows))
	}
	for _, row := range generationRows {
		if !row.raw.valid {
			t.Fatalf("expected generation parameters on attempt connection %d, got null", row.connectionID)
		}
		var params map[string]any
		if err := json.Unmarshal([]byte(row.raw.value), &params); err != nil {
			t.Fatalf("decode generation parameters: %v", err)
		}
		switch row.connectionID {
		case primaryConnectionID:
			if params["temperature"] != 0.1 || params["max_output_tokens"] != float64(11) {
				t.Fatalf("expected primary attempt generation snapshot to reflect its own overlay, got %+v", params)
			}
		case secondaryConnectionID:
			if params["temperature"] != 0.9 || params["max_output_tokens"] != float64(99) {
				t.Fatalf("expected secondary attempt generation snapshot to reflect its own overlay, got %+v", params)
			}
		default:
			t.Fatalf("unexpected connection id %d in generation rows", row.connectionID)
		}
	}
}

func TestCustomRequestParametersWithoutConfigPreservesExistingFastPath(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "text/event-stream", []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"fast path\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":11,\"totalTokenCount\":18}}\n\n"))
	suffix := randomSuffix()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "custom-params-fastpath-public-" + suffix,
		TargetModelID:   "custom-params-fastpath-target-" + suffix,
		EndpointBaseURL: upstream.baseURL("/custom-params/fastpath"),
		EndpointAPIKey:  "fastpath-key",
	})
	requestPath := fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID)
	rawBody := mustMarshalBenchmarkJSON(t, routeMatrixGeminiBody(route.PublicModelID, strings.Repeat("fast path ", 4096), 0.67))

	result := performSplitRuntimeRequestExpectingUpstreamStart(t, harness.client, harness.url+requestPath, rawBody, upstream.started)
	if result.Err != nil {
		t.Fatalf("expected split Gemini stream request to succeed: %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected fast-path stream status 200, got %d with body %s", result.StatusCode, result.Body)
	}
	if !strings.Contains(result.Body, "fast path") {
		t.Fatalf("expected fast-path stream response passthrough, got %q", result.Body)
	}
}

func assertOverlayedBody(t *testing.T, rawBody []byte, wantProviderJSON string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rawBody, &body); err != nil {
		t.Fatalf("decode overlayed body: %v", err)
	}
	raw, err := json.Marshal(body["provider"])
	if err != nil {
		t.Fatalf("marshal provider value: %v", err)
	}
	if string(raw) != wantProviderJSON {
		t.Fatalf("expected provider value %s, got %s in body %s", wantProviderJSON, raw, rawBody)
	}
}

func assertRuntimeErrorResponse(t *testing.T, response *http.Response, wantStatus int, wantDetail string) {
	t.Helper()
	if response.StatusCode != wantStatus {
		t.Fatalf("expected status %d, got %d with body %s", wantStatus, response.StatusCode, readResponseBody(t, response))
	}
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if detail, _ := payload["detail"].(string); detail != wantDetail {
		t.Fatalf("expected detail %q, got %+v", wantDetail, payload)
	}
}

func jsonStringListRuntime(t *testing.T, raw any) []string {
	t.Helper()
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected JSON array, got %T", raw)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("expected JSON string element, got %T", item)
		}
		result = append(result, text)
	}
	return result
}

type sqlNullStringForTest struct {
	value string
	valid bool
}

func (value *sqlNullStringForTest) Scan(raw any) error {
	if raw == nil {
		value.valid = false
		return nil
	}
	switch typed := raw.(type) {
	case string:
		value.value = typed
	case []byte:
		value.value = string(typed)
	default:
		return fmt.Errorf("unexpected scan type %T", raw)
	}
	value.valid = true
	return nil
}
