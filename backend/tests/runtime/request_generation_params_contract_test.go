package runtimetest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/jackc/pgx/v5"
)

func TestRuntimeRequestGenerationParamsPersistProviderMatrix(t *testing.T) {
	t.Run("OpenAINonStreaming", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "generation-openai-chat"})
		route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "generation-openai-chat-public-" + randomSuffix(), TargetModelID: "generation-openai-chat-target-" + randomSuffix(), EndpointBaseURL: upstream.baseURL("/generation/openai/chat"), EndpointAPIKey: "generation-openai-chat-key"})

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "hidden openai chat prompt"}}, "tools": []map[string]any{{"type": "function", "function": map[string]any{"name": "hidden_tool"}}}, "temperature": 0.7, "top_p": 0.9, "max_completion_tokens": 1024, "reasoning_effort": "low"}, nil)
		assertStatus(t, response, http.StatusOK)
		assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", map[string]any{"provider": "openai", "temperature": 0.7, "top_p": 0.9, "max_output_tokens": float64(1024), "max_output_tokens_source": "max_completion_tokens", "reasoning": map[string]any{"effort": "low", "source_field": "reasoning_effort"}})
	})

	t.Run("OpenAIStreamingResponses", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newGenerationParamsOpenAIResponsesStreamUpstream(t)
		defer upstream.Close()
		route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "generation-openai-responses-public-" + randomSuffix(), TargetModelID: "generation-openai-responses-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "generation-openai-responses-key"})

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": []map[string]any{{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "hidden responses prompt"}}}}, "stream": true, "temperature": 0.4, "top_p": 0.8, "max_output_tokens": 256, "reasoning": map[string]any{"effort": "medium"}}, nil)
		assertStatus(t, response, http.StatusOK)
		assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", map[string]any{"provider": "openai", "temperature": 0.4, "top_p": 0.8, "max_output_tokens": float64(256), "max_output_tokens_source": "max_output_tokens", "reasoning": map[string]any{"effort": "medium", "source_field": "reasoning.effort"}})
	})

	t.Run("AnthropicNonStreaming", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "generation-anthropic"})
		route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "anthropic", PublicModelID: "generation-anthropic-public-" + randomSuffix(), TargetModelID: "generation-anthropic-target-" + randomSuffix(), EndpointBaseURL: upstream.baseURL("/generation/anthropic"), EndpointAPIKey: "generation-anthropic-key"})

		response := harness.requestJSON(t, http.MethodPost, "/v1/messages", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "hidden anthropic prompt"}}, "temperature": 0.6, "top_p": 0.95, "max_tokens": 512, "thinking": map[string]any{"type": "enabled", "budget_tokens": 2048}, "output_config": map[string]any{"effort": "high"}}, nil)
		assertStatus(t, response, http.StatusOK)
		assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", map[string]any{"provider": "anthropic", "temperature": 0.6, "top_p": 0.95, "max_output_tokens": float64(512), "max_output_tokens_source": "max_tokens", "reasoning": map[string]any{"effort": "high", "mode": "enabled", "budget_tokens": float64(2048), "source_field": "output_config.effort"}})
	})

	t.Run("AnthropicStreaming", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newGenerationParamsAnthropicStreamUpstream(t)
		defer upstream.Close()
		route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "anthropic", PublicModelID: "generation-anthropic-stream-public-" + randomSuffix(), TargetModelID: "generation-anthropic-stream-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "generation-anthropic-stream-key"})

		response := harness.requestJSON(t, http.MethodPost, "/v1/messages", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "hidden anthropic stream prompt"}}, "stream": true, "temperature": 0.2, "top_p": 0.75, "max_tokens": 128, "thinking": map[string]any{"type": "enabled", "budget_tokens": 512}}, nil)
		assertStatus(t, response, http.StatusOK)
		assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", map[string]any{"provider": "anthropic", "temperature": 0.2, "top_p": 0.75, "max_output_tokens": float64(128), "max_output_tokens_source": "max_tokens", "reasoning": map[string]any{"mode": "enabled", "budget_tokens": float64(512), "source_field": "thinking"}})
	})

	t.Run("GeminiBufferedFallback", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		primary := newScriptedUpstream(t, http.StatusOK, map[string]any{"responseId": "generation-gemini-buffered"})
		secondary := newScriptedUpstream(t, http.StatusOK, map[string]any{"responseId": "generation-gemini-buffered-secondary"})
		route := seedGeminiMultiConnectionRoute(t, harness, profileID, primary.baseURL("/generation/gemini/buffered/primary"), secondary.baseURL("/generation/gemini/buffered/secondary"))

		response := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/v1beta/models/%s:generateContent", route.PublicModelID), geminiGenerationParamsBody("hidden gemini buffered prompt"), nil)
		assertStatus(t, response, http.StatusOK)
		if got := len(secondary.requestsSnapshot()); got != 0 {
			t.Fatalf("expected buffered Gemini request to complete on primary, got %d secondary requests", got)
		}
		assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", expectedGeminiGenerationParams())
	})
}

func TestRuntimeOperationNamePersistsForTextAndTokenCount(t *testing.T) {
	tests := []struct {
		name          string
		apiFamily     string
		operationName string
		perform       func(t *testing.T, harness *runtimeHarness, route seededRuntimeRoute) *http.Response
	}{
		{
			name:          "OpenAIText",
			apiFamily:     "openai",
			operationName: "openai.chat_completions",
			perform: func(t *testing.T, harness *runtimeHarness, route seededRuntimeRoute) *http.Response {
				return harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "persist text operation name"}}}, nil)
			},
		},
		{
			name:          "AnthropicTokenCount",
			apiFamily:     "anthropic",
			operationName: "anthropic.count_tokens",
			perform: func(t *testing.T, harness *runtimeHarness, route seededRuntimeRoute) *http.Response {
				return harness.requestJSON(t, http.MethodPost, "/v1/messages/count_tokens", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "persist token-count operation name"}}}, nil)
			},
		},
		{
			name:          "GeminiTokenCount",
			apiFamily:     "gemini",
			operationName: "gemini.count_tokens",
			perform: func(t *testing.T, harness *runtimeHarness, route seededRuntimeRoute) *http.Response {
				return harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/v1beta/models/%s:countTokens", route.PublicModelID), map[string]any{"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "persist gemini token-count operation name"}}}}}, nil)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "operation-name-" + strings.ToLower(test.name)})
			route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: test.apiFamily, PublicModelID: "operation-name-public-" + strings.ToLower(test.name) + "-" + randomSuffix(), TargetModelID: "operation-name-target-" + strings.ToLower(test.name) + "-" + randomSuffix(), EndpointBaseURL: upstream.baseURL("/operation-name/" + strings.ToLower(test.name)), EndpointAPIKey: "operation-name-key-" + strings.ToLower(test.name)})

			response := test.perform(t, harness, route)
			assertStatus(t, response, http.StatusOK)
			assertLatestRuntimeOperationName(t, harness.conn, profileID, test.operationName)
		})
	}
}

func TestRuntimeRequestGenerationParamsPersistGeminiDirectStreaming(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newArrivalRecordingUpstream(t)
	defer upstream.close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "gemini", PublicModelID: "generation-gemini-direct-public-" + randomSuffix(), TargetModelID: "generation-gemini-direct-target-" + randomSuffix(), EndpointBaseURL: upstream.baseURL("/generation/gemini/direct"), EndpointAPIKey: "generation-gemini-direct-key"})

	rawBody := mustMarshalBenchmarkJSON(t, geminiGenerationParamsBody(strings.Repeat("hidden direct gemini prompt ", 4096)))
	result := performSplitRuntimeRequestExpectingUpstreamStart(t, harness.client, fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent", harness.url, route.PublicModelID), rawBody, upstream.started)
	if result.Err != nil {
		t.Fatalf("expected direct streaming Gemini request to succeed, got error: %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected direct streaming Gemini request status 200, got %d with body %s", result.StatusCode, result.Body)
	}
	assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", expectedGeminiGenerationParams())
}

func TestRuntimeRequestGenerationParamsPersistWithAuditDisabled(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "generation-audit-disabled"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "generation-audit-disabled-public-" + randomSuffix(), TargetModelID: "generation-audit-disabled-target-" + randomSuffix(), EndpointBaseURL: upstream.baseURL("/generation/audit-disabled"), EndpointAPIKey: "generation-audit-disabled-key"})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "hidden audit disabled prompt"}}, "temperature": 0.33}, nil)
	assertStatus(t, response, http.StatusOK)
	assertLatestRequestGenerationParams(t, harness.conn, profileID, "complete", map[string]any{"provider": "openai", "temperature": 0.33})
	var auditEnabled bool
	if err := harness.conn.QueryRow(context.Background(), `SELECT audit_enabled_at_request FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&auditEnabled); err != nil {
		t.Fatalf("load audit-disabled request log snapshot: %v", err)
	}
	if auditEnabled {
		t.Fatal("expected audit disabled snapshot while generation params still persisted")
	}
}

func TestRuntimeRequestGenerationParamsMalformedAndFailoverClone(t *testing.T) {
	t.Run("MalformedGeminiPersistsNullParams", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "malformed-generation"})
		route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "gemini", PublicModelID: "malformed-generation-public-" + randomSuffix(), TargetModelID: "malformed-generation-target-" + randomSuffix(), EndpointBaseURL: upstream.baseURL("/malformed"), EndpointAPIKey: "malformed-key"})
		request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/v1beta/models/%s:generateContent", harness.url, route.PublicModelID), strings.NewReader(`{"generationConfig":`))
		if err != nil {
			t.Fatalf("build malformed request: %v", err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := harness.client.Do(request)
		if err != nil {
			t.Fatalf("perform malformed request: %v", err)
		}
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(io.Discard, response.Body)
		assertStatus(t, response, http.StatusOK)
		params, status := loadLatestRequestGenerationParams(t, harness.conn, profileID)
		if params != nil || !status.Valid || status.String != "malformed" {
			t.Fatalf("expected malformed null params, got status=%+v params=%+v", status, params)
		}
	})

	t.Run("FailoverAttemptsCarryEquivalentParams", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		primary := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "try next"})
		secondary := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "failover-generation"})
		strategyID := harness.seedLegacyStrategy(t, profileID, "generation-failover-"+randomSuffix(), "fill-first")
		targetID := harness.seedModel(t, profileID, "openai", "generation-failover-target-"+randomSuffix(), "native", &strategyID)
		publicID := "generation-failover-public-" + randomSuffix()
		publicConfigID := harness.seedModel(t, profileID, "openai", publicID, "proxy", nil)
		harness.seedProxyTarget(t, publicConfigID, targetID)
		primaryEndpointID := harness.seedEndpoint(t, profileID, "generation-primary", primary.baseURL("/primary"), "primary-key", 0)
		secondaryEndpointID := harness.seedEndpoint(t, profileID, "generation-secondary", secondary.baseURL("/secondary"), "secondary-key", 1)
		harness.seedConnection(t, profileID, targetID, primaryEndpointID, "generation-primary", nil, nil, 0)
		harness.seedConnection(t, profileID, targetID, secondaryEndpointID, "generation-secondary", nil, nil, 1)
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": publicID, "messages": []map[string]any{{"role": "user", "content": "hidden failover prompt"}}, "temperature": 0.6, "top_p": 0.8}, nil)
		assertStatus(t, response, http.StatusOK)
		waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
		assertLatestGenerationParamsClonedForAttempts(t, harness.conn, profileID, map[string]any{"provider": "openai", "temperature": 0.6, "top_p": 0.8})
	})
}

func newGenerationParamsOpenAIResponsesStreamUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}}}\n\n")
	}))
}

func newGenerationParamsAnthropicStreamUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":1}}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":13}}\n\n")
	}))
}

func seedGeminiMultiConnectionRoute(t *testing.T, harness *runtimeHarness, profileID int, primaryURL string, secondaryURL string) seededRuntimeRoute {
	t.Helper()
	suffix := randomSuffix()
	publicID := "generation-gemini-buffered-public-" + suffix
	targetID := "generation-gemini-buffered-target-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "generation-gemini-buffered-"+suffix, "fill-first")
	targetConfigID := harness.seedModel(t, profileID, "gemini", targetID, "native", &strategyID)
	publicConfigID := harness.seedModel(t, profileID, "gemini", publicID, "proxy", nil)
	harness.seedProxyTarget(t, publicConfigID, targetConfigID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "generation-gemini-buffered-primary-"+suffix, primaryURL, "generation-gemini-buffered-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "generation-gemini-buffered-secondary-"+suffix, secondaryURL, "generation-gemini-buffered-secondary-key", 1)
	connectionID := harness.seedConnection(t, profileID, targetConfigID, primaryEndpointID, "generation-gemini-buffered-primary-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, targetConfigID, secondaryEndpointID, "generation-gemini-buffered-secondary-connection-"+suffix, nil, nil, 1)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return seededRuntimeRoute{PublicModelID: publicID, TargetModelID: targetID, ConnectionID: connectionID}
}

func geminiGenerationParamsBody(prompt string) map[string]any {
	return map[string]any{"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": prompt}}}}, "tools": []map[string]any{{"functionDeclarations": []map[string]any{{"name": "hidden_tool"}}}}, "generationConfig": map[string]any{"temperature": 0.3, "topP": 0.7, "topK": 40, "maxOutputTokens": 777, "thinkingConfig": map[string]any{"thinkingBudget": 123, "thinkingLevel": "high", "includeThoughts": true}}}
}

func expectedGeminiGenerationParams() map[string]any {
	return map[string]any{"provider": "gemini", "temperature": 0.3, "top_p": 0.7, "top_k": float64(40), "max_output_tokens": float64(777), "max_output_tokens_source": "generationConfig.maxOutputTokens", "reasoning": map[string]any{"effort": "high", "budget_tokens": float64(123), "include_thoughts": true, "source_field": "generationConfig.thinkingConfig"}}
}

func performSplitRuntimeRequestExpectingUpstreamStart(t *testing.T, client *http.Client, url string, rawBody []byte, started <-chan struct{}) concurrentRuntimeRequestResult {
	t.Helper()
	splitAt := len(rawBody) / 2
	pipeReader, pipeWriter := io.Pipe()
	request, err := http.NewRequest(http.MethodPost, url, pipeReader)
	if err != nil {
		t.Fatalf("build split streaming generation request %s: %v", url, err)
	}
	request.Header.Set("Content-Type", "application/json")
	resultCh := make(chan concurrentRuntimeRequestResult, 1)
	go func() {
		response, requestErr := client.Do(request)
		if requestErr != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: requestErr}
			return
		}
		defer func() { _ = response.Body.Close() }()
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: readErr}
			return
		}
		resultCh <- concurrentRuntimeRequestResult{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(body))}
	}()
	if _, err := pipeWriter.Write(rawBody[:splitAt]); err != nil {
		t.Fatalf("write first split streaming generation chunk: %v", err)
	}
	select {
	case <-started:
	case <-time.After(runtimeStreamingAssertionDeadline):
		t.Fatal("expected direct Gemini request body to reach upstream before the full client body was read")
	}
	if _, err := pipeWriter.Write(rawBody[splitAt:]); err != nil {
		t.Fatalf("write second split streaming generation chunk: %v", err)
	}
	if err := pipeWriter.Close(); err != nil {
		t.Fatalf("close split streaming generation writer: %v", err)
	}
	return awaitAsyncRequest(t, resultCh, 5*time.Second)
}

func performRuntimeRawRequest(t *testing.T, harness *runtimeHarness, method string, path string, body []byte, contentType string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, harness.url+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build raw runtime request %s %s: %v", method, path, err)
	}
	if strings.TrimSpace(contentType) != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := harness.client.Do(request)
	if err != nil {
		t.Fatalf("perform raw runtime request %s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func seedTranslatedOpenAIProxyRoute(t *testing.T, harness *runtimeHarness, profileID int, publicModelPrefix string, targetModelPrefix string, endpointBaseURL string, endpointAPIKey string, openAITextCapability string) seededRuntimeRoute {
	t.Helper()
	suffix := randomSuffix()
	strategyID := harness.seedLegacyStrategy(t, profileID, "translated-openai-"+suffix, "fill-first")
	publicModelID := publicModelPrefix + "-" + suffix
	targetModelID := targetModelPrefix + "-" + suffix
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, profileID, publicModelPrefix+"-endpoint-"+suffix, endpointBaseURL, endpointAPIKey, 0)
	connectionID := harness.seedConnectionWithOpenAITextCapability(t, profileID, targetModelConfigID, endpointID, publicModelPrefix+"-connection-"+suffix, nil, nil, 0, &openAITextCapability)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return seededRuntimeRoute{PublicModelID: publicModelID, TargetModelID: targetModelID, ConnectionID: connectionID}
}

func assertLatestRuntimeOperationName(t *testing.T, conn *pgx.Conn, profileID int, want string) {
	t.Helper()
	waitForRuntimeTelemetryCounts(t, conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	var requestLogOperationName sql.NullString
	if err := conn.QueryRow(context.Background(), `SELECT operation_name FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`, profileID, ingressRequestID).Scan(&requestLogOperationName); err != nil {
		t.Fatalf("load request_logs operation_name: %v", err)
	}
	if !requestLogOperationName.Valid || requestLogOperationName.String != want {
		t.Fatalf("expected request_logs operation_name %q, got %+v", want, requestLogOperationName)
	}

	var usageEventOperationName sql.NullString
	if err := conn.QueryRow(context.Background(), `SELECT operation_name FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`, profileID, ingressRequestID).Scan(&usageEventOperationName); err != nil {
		t.Fatalf("load usage_request_events operation_name: %v", err)
	}
	if !usageEventOperationName.Valid || usageEventOperationName.String != want {
		t.Fatalf("expected usage_request_events operation_name %q, got %+v", want, usageEventOperationName)
	}
}

func assertLatestRequestGenerationParamsMissing(t *testing.T, conn *pgx.Conn, profileID int) {
	t.Helper()
	params, status := loadLatestRequestGenerationParams(t, conn, profileID)
	if params != nil || !status.Valid || status.String != "missing" {
		t.Fatalf("expected null request_generation_params with missing status, got status=%+v params=%+v", status, params)
	}
}

func assertLatestRequestGenerationParams(t *testing.T, conn *pgx.Conn, profileID int, wantStatus string, want map[string]any) {
	t.Helper()
	params, status := loadLatestRequestGenerationParams(t, conn, profileID)
	if !status.Valid || status.String != wantStatus {
		t.Fatalf("expected request generation params status %q, got %+v with params %+v", wantStatus, status, params)
	}
	if !reflect.DeepEqual(params, want) {
		gotJSON, _ := json.Marshal(params)
		wantJSON, _ := json.Marshal(want)
		t.Fatalf("expected request generation params %s, got %s", wantJSON, gotJSON)
	}
	assertNoForbiddenGenerationPayload(t, params)
}

func assertNoForbiddenGenerationPayload(t *testing.T, params map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(params)
	for _, forbidden := range []string{"hidden", "messages", "contents", "parts", "tools", "functionDeclarations", "input_text", "prompt"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("persisted forbidden request payload %q in %s", forbidden, raw)
		}
	}
}

func loadLatestRequestGenerationParams(t *testing.T, conn *pgx.Conn, profileID int) (map[string]any, sql.NullString) {
	t.Helper()
	var raw []byte
	var status sql.NullString
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := conn.QueryRow(context.Background(), `SELECT request_generation_params, request_generation_params_status FROM request_logs WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`, profileID).Scan(&raw, &status)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("load request generation params: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(raw) == 0 {
		return nil, status
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("decode request generation params: %v", err)
	}
	return params, status
}

func assertLatestGenerationParamsClonedForAttempts(t *testing.T, conn *pgx.Conn, profileID int, want map[string]any) {
	t.Helper()
	ingressID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	rows, err := conn.Query(context.Background(), `SELECT request_generation_params, request_generation_params_status FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number ASC`, profileID, ingressID)
	if err != nil {
		t.Fatalf("query generation params attempts: %v", err)
	}
	defer rows.Close()
	var first string
	count := 0
	for rows.Next() {
		var raw []byte
		var status sql.NullString
		if err := rows.Scan(&raw, &status); err != nil {
			t.Fatalf("scan generation params attempt: %v", err)
		}
		if !status.Valid || status.String != "complete" {
			t.Fatalf("expected complete status, got %+v", status)
		}
		var params map[string]any
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Fatalf("decode generation params attempt: %v", err)
		}
		if !reflect.DeepEqual(params, want) {
			t.Fatalf("expected attempt params %+v, got %+v", want, params)
		}
		if count == 0 {
			first = string(raw)
		} else if string(raw) != first {
			t.Fatalf("expected cloned equivalent params, got %q and %q", first, raw)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate generation params attempts: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected two failover attempts, got %d", count)
	}
}
