package runtime_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type runtimeOperationRouteMatrixCase struct {
	name                 string
	apiFamily            string
	operationName        string
	responsePayload      map[string]any
	responseContentType  string
	responseBody         string
	requestPath          func(seededRuntimeRoute) string
	requestBody          func(seededRuntimeRoute, string) any
	rawRequestBody       func(*testing.T, seededRuntimeRoute) ([]byte, string)
	wantUpstreamPath     func(string, seededRuntimeRoute) string
	assertModelSource    func(*testing.T, upstreamRequestSnapshot, seededRuntimeRoute, string)
	generationParams     routeMatrixGenerationParamsExpectation
	usage                routeMatrixUsageExpectation
	persistedAttribution *routeMatrixPersistedAttributionExpectation
	responseContains     string
}

type routeMatrixGenerationParamsExpectation struct {
	status string
	params map[string]any
}

type routeMatrixUsageExpectation struct {
	isStream                 bool
	streamOutcome            string
	inputTokens              *int64
	outputTokens             *int64
	totalTokens              *int64
	cacheReadInputTokens     *int64
	cacheCreationInputTokens *int64
	reasoningTokens          *int64
}

type routeMatrixPersistedAttributionExpectation struct {
	upstreamOperationName string
	translationMode       string
	upstreamRequestPath   string
}

func TestRuntimeOperationRouteMatrixSupportedOperations(t *testing.T) {
	tests := []runtimeOperationRouteMatrixCase{
		{
			name:          "OpenAIChatCompletions",
			apiFamily:     "openai",
			operationName: "openai.chat_completions",
			responsePayload: map[string]any{
				"id":    "route-matrix-chat",
				"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 20},
			},
			requestPath: routeMatrixStaticRequestPath("/v1/chat/completions"),
			requestBody: func(route seededRuntimeRoute, _ string) any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "route matrix chat"}}, "temperature": 0.11}
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/chat/completions"),
			assertModelSource: assertRouteMatrixBodyModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "complete", params: map[string]any{"provider": "openai", "temperature": 0.11}},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(7), outputTokens: routeMatrixInt64(13), totalTokens: routeMatrixInt64(20)},
			persistedAttribution: &routeMatrixPersistedAttributionExpectation{
				upstreamOperationName: "openai.chat_completions",
				translationMode:       "none",
				upstreamRequestPath:   "/v1/chat/completions",
			},
			responseContains: "route-matrix-chat",
		},
		{
			name:          "OpenAIResponses",
			apiFamily:     "openai",
			operationName: "openai.responses",
			responsePayload: map[string]any{
				"id":       "route-matrix-responses",
				"response": map[string]any{"usage": map[string]any{"input_tokens": 19, "output_tokens": 23, "total_tokens": 42}},
			},
			requestPath: routeMatrixStaticRequestPath("/v1/responses"),
			requestBody: func(route seededRuntimeRoute, _ string) any {
				return map[string]any{"model": route.PublicModelID, "input": "route matrix responses", "temperature": 0.22}
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/responses"),
			assertModelSource: assertRouteMatrixBodyModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "complete", params: map[string]any{"provider": "openai", "temperature": 0.22}},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(19), outputTokens: routeMatrixInt64(23), totalTokens: routeMatrixInt64(42)},
			persistedAttribution: &routeMatrixPersistedAttributionExpectation{
				upstreamOperationName: "openai.responses",
				translationMode:       "none",
				upstreamRequestPath:   "/v1/responses",
			},
			responseContains: "route-matrix-responses",
		},
		{
			name:          "OpenAIResponsesInputTokens",
			apiFamily:     "openai",
			operationName: "openai.responses.input_tokens",
			responsePayload: map[string]any{
				"input_tokens": 17,
				"total_tokens": 17,
			},
			requestPath: routeMatrixStaticRequestPath("/v1/responses/input_tokens"),
			requestBody: func(route seededRuntimeRoute, _ string) any {
				return map[string]any{"model": route.PublicModelID, "input": "route matrix input tokens", "stream": true}
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/responses/input_tokens"),
			assertModelSource: assertRouteMatrixBodyModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "missing"},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(17), totalTokens: routeMatrixInt64(17)},
			persistedAttribution: &routeMatrixPersistedAttributionExpectation{
				upstreamOperationName: "openai.responses.input_tokens",
				translationMode:       "none",
				upstreamRequestPath:   "/v1/responses/input_tokens",
			},
			responseContains: "input_tokens",
		},
		{
			name:          "OpenAIResponsesCompact",
			apiFamily:     "openai",
			operationName: "openai.responses.compact",
			responsePayload: map[string]any{
				"id":       "route-matrix-compact",
				"response": map[string]any{"usage": map[string]any{"input_tokens": 29, "output_tokens": 3, "total_tokens": 32}},
			},
			requestPath: routeMatrixStaticRequestPath("/v1/responses/compact"),
			requestBody: func(route seededRuntimeRoute, _ string) any {
				return map[string]any{"model": route.PublicModelID, "input": "route matrix compact", "stream": true}
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/responses/compact"),
			assertModelSource: assertRouteMatrixBodyModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "missing"},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(29), outputTokens: routeMatrixInt64(3), totalTokens: routeMatrixInt64(32)},
			persistedAttribution: &routeMatrixPersistedAttributionExpectation{
				upstreamOperationName: "openai.responses.compact",
				translationMode:       "none",
				upstreamRequestPath:   "/v1/responses/compact",
			},
			responseContains: "route-matrix-compact",
		},
		{
			name:          "OpenAIImageGenerations",
			apiFamily:     "openai",
			operationName: "openai.images.generations",
			responsePayload: map[string]any{
				"created": 1,
				"data":    []map[string]any{{"url": "https://images.invalid/generated.png"}},
				"usage":   map[string]any{"prompt_tokens": 999, "completion_tokens": 999, "total_tokens": 1998},
			},
			requestPath: routeMatrixStaticRequestPath("/v1/images/generations"),
			requestBody: func(route seededRuntimeRoute, _ string) any {
				return map[string]any{"model": route.PublicModelID, "prompt": "route matrix image generation", "temperature": 0.77, "stream": true}
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/images/generations"),
			assertModelSource: assertRouteMatrixBodyModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "missing"},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming"},
			responseContains:  "generated.png",
		},
		{
			name:            "OpenAIImageEdits",
			apiFamily:       "openai",
			operationName:   "openai.images.edits",
			responsePayload: map[string]any{"created": 1, "data": []map[string]any{{"url": "https://images.invalid/edited.png"}}, "usage": map[string]any{"prompt_tokens": 999, "completion_tokens": 999, "total_tokens": 1998}},
			requestPath:     routeMatrixStaticRequestPath("/v1/images/edits"),
			rawRequestBody: func(t *testing.T, route seededRuntimeRoute) ([]byte, string) {
				return newRuntimeImageEditMultipartBody(t, route.PublicModelID)
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/images/edits"),
			assertModelSource: assertRouteMatrixMultipartImageEditBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "missing"},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming"},
			responseContains:  "edited.png",
		},
		{
			name:          "AnthropicMessages",
			apiFamily:     "anthropic",
			operationName: "anthropic.messages",
			responsePayload: map[string]any{
				"id":    "route-matrix-anthropic",
				"type":  "message",
				"usage": map[string]any{"input_tokens": 5, "output_tokens": 8},
			},
			requestPath: routeMatrixStaticRequestPath("/v1/messages"),
			requestBody: func(route seededRuntimeRoute, _ string) any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "route matrix anthropic"}}, "max_tokens": 64, "temperature": 0.33}
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/messages"),
			assertModelSource: assertRouteMatrixBodyModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "complete", params: map[string]any{"provider": "anthropic", "temperature": 0.33, "max_output_tokens": float64(64), "max_output_tokens_source": "max_tokens"}},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(5), outputTokens: routeMatrixInt64(8), totalTokens: routeMatrixInt64(13)},
			responseContains:  "route-matrix-anthropic",
		},
		{
			name:          "AnthropicCountTokens",
			apiFamily:     "anthropic",
			operationName: "anthropic.count_tokens",
			responsePayload: map[string]any{
				"input_tokens": 31,
				"total_tokens": 31,
				"usage":        map[string]any{"input_tokens": 999, "output_tokens": 999, "total_tokens": 1998},
			},
			requestPath: routeMatrixStaticRequestPath("/v1/messages/count_tokens"),
			requestBody: func(route seededRuntimeRoute, _ string) any {
				return map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "route matrix anthropic count"}}, "stream": true}
			},
			wantUpstreamPath:  routeMatrixStaticUpstreamPath("/v1/messages/count_tokens"),
			assertModelSource: assertRouteMatrixBodyModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "missing"},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(31), totalTokens: routeMatrixInt64(31)},
			responseContains:  "input_tokens",
		},
		{
			name:          "GeminiGenerateContent",
			apiFamily:     "gemini",
			operationName: "gemini.generate_content",
			responsePayload: map[string]any{
				"responseId":    "route-matrix-gemini-generate",
				"usageMetadata": map[string]any{"promptTokenCount": 11, "candidatesTokenCount": 17, "totalTokenCount": 28, "cachedContentTokenCount": 4, "thoughtsTokenCount": 6},
			},
			requestPath: func(route seededRuntimeRoute) string {
				return fmt.Sprintf("/v1beta/models/%s:generateContent", route.PublicModelID)
			},
			requestBody: func(_ seededRuntimeRoute, ignoredBodyModel string) any {
				return routeMatrixGeminiBody(ignoredBodyModel, "route matrix gemini generate", 0.44)
			},
			wantUpstreamPath:  routeMatrixGeminiUpstreamPath(":generateContent"),
			assertModelSource: assertRouteMatrixPathModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "complete", params: map[string]any{"provider": "gemini", "temperature": 0.44}},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(7), outputTokens: routeMatrixInt64(11), totalTokens: routeMatrixInt64(28), cacheReadInputTokens: routeMatrixInt64(4), reasoningTokens: routeMatrixInt64(6)},
			responseContains:  "route-matrix-gemini-generate",
		},
		{
			name:                "GeminiStreamGenerateContent",
			apiFamily:           "gemini",
			operationName:       "gemini.stream_generate_content",
			responseContentType: "text/event-stream",
			responseBody:        "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"route matrix gemini stream\"}]}}],\"usageMetadata\":{\"promptTokenCount\":13,\"candidatesTokenCount\":21,\"totalTokenCount\":34,\"cachedContentTokenCount\":5,\"thoughtsTokenCount\":8}}\n\n",
			requestPath: func(route seededRuntimeRoute) string {
				return fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID)
			},
			requestBody: func(_ seededRuntimeRoute, ignoredBodyModel string) any {
				return routeMatrixGeminiBody(ignoredBodyModel, "route matrix gemini stream", 0.55)
			},
			wantUpstreamPath:  routeMatrixGeminiUpstreamPath(":streamGenerateContent"),
			assertModelSource: assertRouteMatrixPathModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "complete", params: map[string]any{"provider": "gemini", "temperature": 0.55}},
			usage:             routeMatrixUsageExpectation{isStream: true, streamOutcome: "completed", inputTokens: routeMatrixInt64(8), outputTokens: routeMatrixInt64(13), totalTokens: routeMatrixInt64(34), cacheReadInputTokens: routeMatrixInt64(5), reasoningTokens: routeMatrixInt64(8)},
			responseContains:  "route matrix gemini stream",
		},
		{
			name:          "GeminiCountTokens",
			apiFamily:     "gemini",
			operationName: "gemini.count_tokens",
			responsePayload: map[string]any{
				"totalTokens":             41,
				"cachedContentTokenCount": 3,
				"usageMetadata":           map[string]any{"promptTokenCount": 999, "candidatesTokenCount": 999, "totalTokenCount": 1998, "cachedContentTokenCount": 777, "thoughtsTokenCount": 666},
			},
			requestPath: func(route seededRuntimeRoute) string {
				return fmt.Sprintf("/v1beta/models/%s:countTokens", route.PublicModelID)
			},
			requestBody: func(_ seededRuntimeRoute, ignoredBodyModel string) any {
				body := routeMatrixGeminiBody(ignoredBodyModel, "route matrix gemini count", 0.66)
				body["stream"] = true
				return body
			},
			wantUpstreamPath:  routeMatrixGeminiUpstreamPath(":countTokens"),
			assertModelSource: assertRouteMatrixPathModelBinding,
			generationParams:  routeMatrixGenerationParamsExpectation{status: "missing"},
			usage:             routeMatrixUsageExpectation{streamOutcome: "not_streaming", inputTokens: routeMatrixInt64(41), totalTokens: routeMatrixInt64(41), cacheReadInputTokens: routeMatrixInt64(3)},
			responseContains:  "totalTokens",
		},
	}
	if len(tests) != 11 {
		t.Fatalf("route matrix must cover exactly 11 registered POST operations, got %d", len(tests))
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newRouteMatrixUpstream(t, test.responseContentType, test.routeMatrixResponseBody(t))
			slug := routeMatrixSlug(test.operationName)
			endpointPrefix := "/route-matrix/" + slug
			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:       profileID,
				APIFamily:       test.apiFamily,
				PublicModelID:   "route-matrix-public-" + slug + "-" + randomSuffix(),
				TargetModelID:   "route-matrix-target-" + slug + "-" + randomSuffix(),
				EndpointBaseURL: upstream.baseURL(endpointPrefix),
				EndpointAPIKey:  "route-matrix-key-" + slug,
				CustomHeaders:   map[string]any{"X-Route-Matrix": "route-matrix-" + slug},
			})
			ignoredBodyModel := "route-matrix-body-model-" + slug
			requestPath := test.requestPath(route)

			var response *http.Response
			if test.rawRequestBody != nil {
				rawBody, contentType := test.rawRequestBody(t, route)
				response = performRuntimeRawRequest(t, harness, http.MethodPost, requestPath, rawBody, contentType)
			} else {
				response = harness.requestJSON(t, http.MethodPost, requestPath, test.requestBody(route, ignoredBodyModel), nil)
			}

			assertStatus(t, response, http.StatusOK)
			responseBody := readResponseBody(t, response)
			if test.responseContains != "" && !strings.Contains(responseBody, test.responseContains) {
				t.Fatalf("expected downstream response to contain %q, got %q", test.responseContains, responseBody)
			}
			upstreamRequest := upstream.lastRequest(t)
			assertRouteMatrixSharedCoreForwarding(t, upstreamRequest, route, test.apiFamily, test.wantUpstreamPath(endpointPrefix, route), "route-matrix-"+slug)
			test.assertModelSource(t, upstreamRequest, route, ignoredBodyModel)
			assertRouteMatrixGoldenUpstreamRequest(t, test.operationName, upstreamRequest, route)
			assertRouteMatrixSharedCorePersistence(t, harness, profileID, route, test.operationName, requestPath)
			assertRouteMatrixUsage(t, harness, profileID, test.usage)
			assertRouteMatrixGenerationParams(t, harness, profileID, test.generationParams)
			if test.persistedAttribution != nil {
				assertRouteMatrixPersistedAttribution(t, harness, profileID, test.operationName, *test.persistedAttribution)
			}
		})
	}
}

func TestGeminiStreamGenerateUsesPathStreaming(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "text/event-stream", []byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"split stream\"}]}}],\"usageMetadata\":{\"promptTokenCount\":11,\"candidatesTokenCount\":17,\"totalTokenCount\":28,\"cachedContentTokenCount\":4,\"thoughtsTokenCount\":6}}\n\n"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "route-matrix-stream-public-" + randomSuffix(),
		TargetModelID:   "route-matrix-stream-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/route-matrix/gemini-stream-path"),
		EndpointAPIKey:  "route-matrix-stream-key",
		CustomHeaders:   map[string]any{"X-Route-Matrix": "route-matrix-gemini-stream-path"},
	})
	ignoredBodyModel := "route-matrix-stream-ignored-body"
	requestPath := fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID)
	rawBody := mustMarshalBenchmarkJSON(t, routeMatrixGeminiBody(ignoredBodyModel, strings.Repeat("route matrix streaming ", 4096), 0.67))

	result := performSplitRuntimeRequestExpectingUpstreamStart(t, harness.client, harness.url+requestPath, rawBody, upstream.started)
	if result.Err != nil {
		t.Fatalf("expected split Gemini stream request to succeed: %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected split Gemini stream status 200, got %d with body %s", result.StatusCode, result.Body)
	}
	if !strings.Contains(result.Body, "split stream") {
		t.Fatalf("expected split stream response body to pass through, got %q", result.Body)
	}

	upstreamRequest := upstream.lastRequest(t)
	assertRouteMatrixSharedCoreForwarding(t, upstreamRequest, route, "gemini", "/route-matrix/gemini-stream-path/v1beta/models/"+route.TargetModelID+":streamGenerateContent", "route-matrix-gemini-stream-path")
	assertRouteMatrixPathModelBinding(t, upstreamRequest, route, ignoredBodyModel)
	assertRouteMatrixSharedCorePersistence(t, harness, profileID, route, "gemini.stream_generate_content", requestPath)
	assertRouteMatrixUsage(t, harness, profileID, routeMatrixUsageExpectation{isStream: true, streamOutcome: "completed", inputTokens: routeMatrixInt64(7), outputTokens: routeMatrixInt64(11), totalTokens: routeMatrixInt64(28), cacheReadInputTokens: routeMatrixInt64(4), reasoningTokens: routeMatrixInt64(6)})
	assertRouteMatrixGenerationParams(t, harness, profileID, routeMatrixGenerationParamsExpectation{status: "complete", params: map[string]any{"provider": "gemini", "temperature": 0.67}})
}

func TestOpenAIImageEditsMultipartForwarding(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "application/json", []byte(`{"created":1,"data":[{"url":"https://images.invalid/edge-edit.png"}],"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}`))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "route-matrix-image-edit-public-" + randomSuffix(),
		TargetModelID:   "route-matrix-image-edit-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/route-matrix/openai-image-edit-edge"),
		EndpointAPIKey:  "route-matrix-image-edit-key",
		CustomHeaders:   map[string]any{"X-Route-Matrix": "route-matrix-openai-image-edit-edge"},
	})
	requestPath := "/v1/images/edits"
	rawBody, contentType := newRuntimeImageEditMultipartBody(t, route.PublicModelID)

	response := performRuntimeRawRequest(t, harness, http.MethodPost, requestPath, rawBody, contentType)
	assertStatus(t, response, http.StatusOK)
	if responseBody := readResponseBody(t, response); !strings.Contains(responseBody, "edge-edit.png") {
		t.Fatalf("expected image edit response to pass through, got %q", responseBody)
	}

	upstreamRequest := upstream.lastRequest(t)
	assertRouteMatrixSharedCoreForwarding(t, upstreamRequest, route, "openai", "/route-matrix/openai-image-edit-edge/v1/images/edits", "route-matrix-openai-image-edit-edge")
	assertRouteMatrixMultipartImageEditBinding(t, upstreamRequest, route, "")
	assertRouteMatrixSharedCorePersistence(t, harness, profileID, route, "openai.images.edits", requestPath)
	assertRouteMatrixUsage(t, harness, profileID, routeMatrixUsageExpectation{streamOutcome: "not_streaming"})
	assertRouteMatrixGenerationParams(t, harness, profileID, routeMatrixGenerationParamsExpectation{status: "missing"})
}

type routeMatrixUpstream struct {
	server              *httptest.Server
	mu                  sync.Mutex
	requests            []upstreamRequestSnapshot
	responseContentType string
	responseBody        []byte
	started             chan struct{}
	startOnce           sync.Once
}

func newRouteMatrixUpstream(t *testing.T, contentType string, body []byte) *routeMatrixUpstream {
	t.Helper()
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	upstream := &routeMatrixUpstream{responseContentType: contentType, responseBody: append([]byte(nil), body...), started: make(chan struct{})}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.startOnce.Do(func() { close(upstream.started) })
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read route-matrix upstream request body: %v", err)
		}
		_ = r.Body.Close()
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, upstreamRequestSnapshot{Method: r.Method, URL: r.URL.String(), Path: r.URL.Path, Query: r.URL.RawQuery, Headers: r.Header.Clone(), Body: append([]byte(nil), requestBody...)})
		upstream.mu.Unlock()
		w.Header().Set("Content-Type", upstream.responseContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(upstream.responseBody)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}
func (u *routeMatrixUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *routeMatrixUpstream) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	requests := u.requestsSnapshot()
	if len(requests) == 0 {
		t.Fatal("expected at least one route-matrix upstream request")
	}
	return requests[len(requests)-1]
}

func (u *routeMatrixUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}
func (test runtimeOperationRouteMatrixCase) routeMatrixResponseBody(t *testing.T) []byte {
	t.Helper()
	if len(test.responsePayload) != 0 {
		raw, err := json.Marshal(test.responsePayload)
		if err != nil {
			t.Fatalf("marshal route-matrix response payload: %v", err)
		}
		return raw
	}
	return []byte(test.responseBody)
}

func routeMatrixStaticRequestPath(path string) func(seededRuntimeRoute) string {
	return func(seededRuntimeRoute) string { return path }
}

func routeMatrixStaticUpstreamPath(path string) func(string, seededRuntimeRoute) string {
	return func(endpointPrefix string, _ seededRuntimeRoute) string { return endpointPrefix + path }
}

func routeMatrixGeminiUpstreamPath(operationSuffix string) func(string, seededRuntimeRoute) string {
	return func(endpointPrefix string, route seededRuntimeRoute) string {
		return fmt.Sprintf("%s/v1beta/models/%s%s", endpointPrefix, route.TargetModelID, operationSuffix)
	}
}

func routeMatrixGeminiBody(model string, prompt string, temperature float64) map[string]any {
	return map[string]any{
		"model": model,
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": prompt}},
		}},
		"generationConfig": map[string]any{"temperature": temperature},
	}
}

func routeMatrixSlug(operationName string) string {
	replacer := strings.NewReplacer(".", "-", "_", "-")
	return replacer.Replace(strings.ToLower(operationName))
}
func routeMatrixInt64(value int64) *int64 {
	pointer := new(int64)
	*pointer = value
	return pointer
}

func assertRouteMatrixGoldenUpstreamRequest(t *testing.T, operationName string, request upstreamRequestSnapshot, route seededRuntimeRoute) {
	t.Helper()
	headers := map[string]string{}
	for _, header := range []string{"Authorization", "anthropic-version", "x-api-key", "X-Route-Matrix"} {
		if value := request.Headers.Get(header); value != "" {
			headers[strings.ToLower(header)] = strings.ReplaceAll(value, route.EndpointAPIKey, "<api_key>")
		}
	}
	snapshot := map[string]any{
		"method":  request.Method,
		"path":    strings.ReplaceAll(request.Path, route.TargetModelID, "<target_model>"),
		"headers": headers,
	}
	contentType := request.Headers.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil && strings.TrimSpace(contentType) != "" {
		t.Fatalf("parse route-matrix content type %q: %v", contentType, err)
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		snapshot["content_type"] = mediaType
		snapshot["multipart_fields"] = normalizeRouteMatrixGoldenValue(map[string]any{
			"model":  string(routeMatrixMultipartValue(t, request, "model")),
			"prompt": string(routeMatrixMultipartValue(t, request, "prompt")),
			"image":  fmt.Sprintf("<binary:%d>", len(routeMatrixMultipartValue(t, request, "image"))),
		}, route)
	} else {
		snapshot["content_type"] = mediaType
		var body any
		if err := json.Unmarshal(request.Body, &body); err != nil {
			t.Fatalf("decode route-matrix upstream JSON for %s: %v", operationName, err)
		}
		snapshot["json_body"] = normalizeRouteMatrixGoldenValue(body, route)
	}

	fixturePath := filepath.Join("testdata", "route_matrix_upstream", routeMatrixSlug(operationName)+".json")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read route-matrix golden %s: %v", fixturePath, err)
	}
	var expected map[string]any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatalf("decode route-matrix golden %s: %v", fixturePath, err)
	}
	if !jsonBytesEqual(t, snapshot, expected) {
		actual, _ := json.MarshalIndent(snapshot, "", "  ")
		want, _ := json.MarshalIndent(expected, "", "  ")
		t.Fatalf("route-matrix golden %s mismatch\nexpected: %s\nactual:   %s", fixturePath, want, actual)
	}
}

func normalizeRouteMatrixGoldenValue(value any, route seededRuntimeRoute) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized[key] = normalizeRouteMatrixGoldenValue(child, route)
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for index, child := range typed {
			normalized[index] = normalizeRouteMatrixGoldenValue(child, route)
		}
		return normalized
	case string:
		replaced := strings.ReplaceAll(typed, route.TargetModelID, "<target_model>")
		replaced = strings.ReplaceAll(replaced, route.PublicModelID, "<public_model>")
		return replaced
	default:
		return value
	}
}

func assertRouteMatrixSharedCoreForwarding(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute, apiFamily string, wantPath string, wantHeaderValue string) {
	t.Helper()
	if request.Method != http.MethodPost {
		t.Fatalf("expected POST forwarded upstream, got %s", request.Method)
	}
	if request.Path != wantPath {
		t.Fatalf("expected upstream path %q, got %q", wantPath, request.Path)
	}
	if request.Headers.Get("X-Route-Matrix") != wantHeaderValue {
		t.Fatalf("expected shared header shaping to forward X-Route-Matrix=%q, got %q", wantHeaderValue, request.Headers.Get("X-Route-Matrix"))
	}
	if apiFamily == "anthropic" {
		if request.Headers.Get("x-api-key") != route.EndpointAPIKey {
			t.Fatalf("expected Anthropic x-api-key %q, got %q", route.EndpointAPIKey, request.Headers.Get("x-api-key"))
		}
		if request.Headers.Get("anthropic-version") != "2023-06-01" {
			t.Fatalf("expected Anthropic version header, got %q", request.Headers.Get("anthropic-version"))
		}
		return
	}
	if request.Headers.Get("Authorization") != "Bearer "+route.EndpointAPIKey {
		t.Fatalf("expected bearer auth %q, got %q", "Bearer "+route.EndpointAPIKey, request.Headers.Get("Authorization"))
	}
}

func assertRouteMatrixBodyModelBinding(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute, _ string) {
	t.Helper()
	if got := requestModelID(t, request.Body); got != route.TargetModelID {
		t.Fatalf("expected body-bound operation to rewrite model to %q, got %q in %s", route.TargetModelID, got, string(request.Body))
	}
}

func assertRouteMatrixPathModelBinding(t *testing.T, request upstreamRequestSnapshot, _ seededRuntimeRoute, ignoredBodyModel string) {
	t.Helper()
	if got := requestModelID(t, request.Body); got != ignoredBodyModel {
		t.Fatalf("expected path-bound operation to leave body model %q untouched, got %q in %s", ignoredBodyModel, got, string(request.Body))
	}
}

func assertRouteMatrixMultipartImageEditBinding(t *testing.T, request upstreamRequestSnapshot, route seededRuntimeRoute, _ string) {
	t.Helper()
	if got := string(routeMatrixMultipartValue(t, request, "model")); got != route.TargetModelID {
		t.Fatalf("expected multipart image edit model %q, got %q", route.TargetModelID, got)
	}
	if got := string(routeMatrixMultipartValue(t, request, "prompt")); got != "make the image brighter" {
		t.Fatalf("expected multipart prompt to survive forwarding, got %q", got)
	}
	if got := string(routeMatrixMultipartValue(t, request, "image")); got != "fake-png-bytes" {
		t.Fatalf("expected multipart image bytes to survive forwarding, got %q", got)
	}
}

func routeMatrixMultipartValue(t *testing.T, request upstreamRequestSnapshot, fieldName string) []byte {
	t.Helper()
	_, params, err := mime.ParseMediaType(request.Headers.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse multipart content type %q: %v", request.Headers.Get("Content-Type"), err)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		t.Fatalf("expected multipart boundary in %q", request.Headers.Get("Content-Type"))
	}
	reader := multipart.NewReader(bytes.NewReader(request.Body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read multipart field %q: %v", fieldName, err)
		}
		value, readErr := io.ReadAll(part)
		_ = part.Close()
		if readErr != nil {
			t.Fatalf("read multipart field %q body: %v", fieldName, readErr)
		}
		if part.FormName() == fieldName {
			return value
		}
	}
	t.Fatalf("expected multipart field %q in upstream request", fieldName)
	return nil
}

func assertRouteMatrixSharedCorePersistence(t *testing.T, harness *runtimeHarness, profileID int, route seededRuntimeRoute, operationName string, requestPath string) {
	t.Helper()
	assertLatestRuntimeOperationName(t, harness.conn, profileID, operationName)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, route.TargetModelID)
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)

	var logRequestPath string
	var logEndpointBaseURL string
	var logConnectionID int
	var logRouteReason string
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT request_path, endpoint_base_url, connection_id, COALESCE(context_routing->>'route_reason', '') FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&logRequestPath, &logEndpointBaseURL, &logConnectionID, &logRouteReason); err != nil {
		t.Fatalf("load route-matrix request_log shared-core fields: %v", err)
	}
	if logRequestPath != requestPath || logEndpointBaseURL != route.EndpointBaseURL || logConnectionID != route.ConnectionID || logRouteReason != "model_redirect" {
		t.Fatalf("expected request_log path/base/connection/reason %q/%q/%d/model_redirect, got %q/%q/%d/%q", requestPath, route.EndpointBaseURL, route.ConnectionID, logRequestPath, logEndpointBaseURL, logConnectionID, logRouteReason)
	}

	var eventRequestPath string
	var eventConnectionID int
	var eventRouteReason string
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT request_path, connection_id, COALESCE(context_routing->>'route_reason', '') FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&eventRequestPath, &eventConnectionID, &eventRouteReason); err != nil {
		t.Fatalf("load route-matrix usage_event shared-core fields: %v", err)
	}
	if eventRequestPath != requestPath || eventConnectionID != route.ConnectionID || eventRouteReason != "model_redirect" {
		t.Fatalf("expected usage_event path/connection/reason %q/%d/model_redirect, got %q/%d/%q", requestPath, route.ConnectionID, eventRequestPath, eventConnectionID, eventRouteReason)
	}
}

func assertRouteMatrixPersistedAttribution(t *testing.T, harness *runtimeHarness, profileID int, wantOperationName string, want routeMatrixPersistedAttributionExpectation) {
	t.Helper()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)

	assertAttributionRow := func(label string, query string) {
		t.Helper()
		var operationName sql.NullString
		var upstreamOperationName sql.NullString
		var translationMode sql.NullString
		var upstreamRequestPath sql.NullString
		if err := harness.conn.QueryRow(context.Background(), query, profileID, ingressRequestID).Scan(&operationName, &upstreamOperationName, &translationMode, &upstreamRequestPath); err != nil {
			t.Fatalf("load %s attribution row: %v", label, err)
		}
		if !operationName.Valid || operationName.String != wantOperationName {
			t.Fatalf("expected %s operation_name %q, got %+v", label, wantOperationName, operationName)
		}
		if !upstreamOperationName.Valid || upstreamOperationName.String != want.upstreamOperationName {
			t.Fatalf("expected %s upstream_operation_name %q, got %+v", label, want.upstreamOperationName, upstreamOperationName)
		}
		if !translationMode.Valid || translationMode.String != want.translationMode {
			t.Fatalf("expected %s operation_translation_mode %q, got %+v", label, want.translationMode, translationMode)
		}
		if !upstreamRequestPath.Valid || upstreamRequestPath.String != want.upstreamRequestPath {
			t.Fatalf("expected %s upstream_request_path %q, got %+v", label, want.upstreamRequestPath, upstreamRequestPath)
		}
	}

	assertAttributionRow(
		"request_log",
		`SELECT operation_name, upstream_operation_name, operation_translation_mode, upstream_request_path FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
	)
	assertAttributionRow(
		"usage_event",
		`SELECT operation_name, upstream_operation_name, operation_translation_mode, upstream_request_path FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
	)
}

func assertRouteMatrixGenerationParams(t *testing.T, harness *runtimeHarness, profileID int, want routeMatrixGenerationParamsExpectation) {
	t.Helper()
	switch want.status {
	case "complete":
		assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", want.params)
	case "missing":
		assertLatestRequestGenerationParamsMissing(t, harness.conn, profileID)
	default:
		t.Fatalf("unsupported route-matrix generation params status %q", want.status)
	}
}

func assertRouteMatrixUsage(t *testing.T, harness *runtimeHarness, profileID int, want routeMatrixUsageExpectation) {
	t.Helper()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)
	logRow := loadRouteMatrixRequestLogUsage(t, harness, profileID, ingressRequestID)
	if logRow.isStream != want.isStream || logRow.streamOutcome != want.streamOutcome {
		t.Fatalf("expected request_log stream=%t outcome=%q, got stream=%t outcome=%q", want.isStream, want.streamOutcome, logRow.isStream, logRow.streamOutcome)
	}
	assertRouteMatrixUsageTokens(t, "request_log", logRow, want)

	eventRow := loadRouteMatrixUsageEventUsage(t, harness, profileID, ingressRequestID)
	if eventRow.streamOutcome != want.streamOutcome {
		t.Fatalf("expected usage_event outcome=%q, got %q", want.streamOutcome, eventRow.streamOutcome)
	}
	assertRouteMatrixUsageTokens(t, "usage_event", eventRow, want)
}

type routeMatrixUsageRow struct {
	isStream                 bool
	streamOutcome            string
	inputTokens              sql.NullInt64
	outputTokens             sql.NullInt64
	totalTokens              sql.NullInt64
	cacheReadInputTokens     sql.NullInt64
	cacheCreationInputTokens sql.NullInt64
	reasoningTokens          sql.NullInt64
}

func loadRouteMatrixRequestLogUsage(t *testing.T, harness *runtimeHarness, profileID int, ingressRequestID string) routeMatrixUsageRow {
	t.Helper()
	var row routeMatrixUsageRow
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT is_stream, stream_outcome, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&row.isStream, &row.streamOutcome, &row.inputTokens, &row.outputTokens, &row.totalTokens, &row.cacheReadInputTokens, &row.cacheCreationInputTokens, &row.reasoningTokens); err != nil {
		t.Fatalf("load route-matrix request_log usage: %v", err)
	}
	return row
}
func loadRouteMatrixUsageEventUsage(t *testing.T, harness *runtimeHarness, profileID int, ingressRequestID string) routeMatrixUsageRow {
	t.Helper()
	var row routeMatrixUsageRow
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT stream_outcome, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&row.streamOutcome, &row.inputTokens, &row.outputTokens, &row.totalTokens, &row.cacheReadInputTokens, &row.cacheCreationInputTokens, &row.reasoningTokens); err != nil {
		t.Fatalf("load route-matrix usage_event usage: %v", err)
	}
	return row
}

func assertRouteMatrixUsageTokens(t *testing.T, label string, got routeMatrixUsageRow, want routeMatrixUsageExpectation) {
	t.Helper()
	assertRouteMatrixNullInt64(t, label+" input_tokens", got.inputTokens, want.inputTokens)
	assertRouteMatrixNullInt64(t, label+" output_tokens", got.outputTokens, want.outputTokens)
	assertRouteMatrixNullInt64(t, label+" total_tokens", got.totalTokens, want.totalTokens)
	assertRouteMatrixNullInt64(t, label+" cache_read_input_tokens", got.cacheReadInputTokens, want.cacheReadInputTokens)
	assertRouteMatrixNullInt64(t, label+" cache_creation_input_tokens", got.cacheCreationInputTokens, want.cacheCreationInputTokens)
	assertRouteMatrixNullInt64(t, label+" reasoning_tokens", got.reasoningTokens, want.reasoningTokens)
}
func assertRouteMatrixNullInt64(t *testing.T, label string, got sql.NullInt64, want *int64) {
	t.Helper()
	if want == nil {
		if got.Valid {
			t.Fatalf("expected %s to be NULL, got %d", label, got.Int64)
		}
		return
	}
	if !got.Valid || got.Int64 != *want {
		t.Fatalf("expected %s=%d, got %+v", label, *want, got)
	}
}
