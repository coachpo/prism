package runtime_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestRuntimeStreamingFirstProxyPassthrough(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newChunkedLargeResponseUpstream(t)
	defer upstream.close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase3-streaming-first-passthrough-public-" + randomSuffix(),
		TargetModelID:   "phase3-streaming-first-passthrough-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/phase3/streaming-first/passthrough"),
		EndpointAPIKey:  "phase3-streaming-first-passthrough-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-3 streaming-first passthrough"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	upstream.waitUntilFirstChunk(t, 5*time.Second)

	type firstChunkResult struct {
		payload []byte
		err     error
	}
	readCh := make(chan firstChunkResult, 1)
	go func() {
		buffer := make([]byte, 64)
		n, err := response.Body.Read(buffer)
		readCh <- firstChunkResult{payload: append([]byte(nil), buffer[:n]...), err: err}
	}()

	var firstChunk firstChunkResult
	select {
	case firstChunk = <-readCh:
		if firstChunk.err != nil && firstChunk.err != io.EOF {
			t.Fatalf("expected streamed response bytes before upstream completion, got read error: %v", firstChunk.err)
		}
		if len(firstChunk.payload) < 1 {
			t.Fatal("expected at least one streamed response byte before upstream completion")
		}
	case <-time.After(runtimeStreamingAssertionDeadline):
		t.Fatal("expected response body to become readable before upstream finished; non-SSE response is still fully buffered")
	}

	upstream.releaseResponse()
	rest, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read streaming-first passthrough response tail: %v", err)
	}
	payload := string(append(firstChunk.payload, rest...))
	if !strings.Contains(payload, `"id":"chatcmpl-phase-2-large-response"`) {
		t.Fatalf("expected streamed response payload to pass through the upstream id, got %q", payload)
	}
	if !strings.Contains(payload, `"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}`) {
		t.Fatalf("expected streamed response payload to pass through upstream usage, got %q", payload)
	}

	assertLatestRequestLogUsage(t, harness.conn, profileID, false, 7, 13, 20)
	assertLatestUsageEventUsage(t, harness.conn, profileID, 7, 13, 20)
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
	assertLatestRuntimeUsageEventTiming(t, harness.conn, profileID)
}

func TestRuntimeStreamingFirstUsageExtraction(t *testing.T) {
	tests := []struct {
		name          string
		apiFamily     string
		requestPath   func(publicModelID string) string
		requestBody   func(publicModelID string) any
		responseBody  map[string]any
		responseField string
		responseValue string
		wantInput     int64
		wantOutput    int64
		wantTotal     int64
	}{
		{
			name:      "OpenAI",
			apiFamily: "openai",
			requestPath: func(string) string {
				return "/v1/chat/completions"
			},
			requestBody: func(publicModelID string) any {
				return map[string]any{
					"messages": []map[string]any{{"role": "user", "content": "phase-3 streaming-first openai usage"}},
					"model":    publicModelID,
				}
			},
			responseBody: map[string]any{
				"id":     "chatcmpl-phase3-streaming-first-openai",
				"object": "chat.completion",
				"choices": []map[string]any{{
					"index":   0,
					"message": map[string]any{"role": "assistant", "content": strings.Repeat("phase3-openai-usage-", 4096)},
				}},
				"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 20},
			},
			responseField: "id",
			responseValue: "chatcmpl-phase3-streaming-first-openai",
			wantInput:     7,
			wantOutput:    13,
			wantTotal:     20,
		},
		{
			name:      "OpenAIResponses",
			apiFamily: "openai",
			requestPath: func(string) string {
				return "/v1/responses"
			},
			requestBody: func(publicModelID string) any {
				return map[string]any{
					"model": publicModelID,
					"input": "phase-3 streaming-first responses usage",
				}
			},
			responseBody: map[string]any{
				"id":     "resp_phase3_streaming_first_openai_responses",
				"object": "response",
				"model":  "phase3-streaming-first-responses-target",
				"output": []map[string]any{{
					"id":   "msg_phase3_streaming_first_openai_responses",
					"type": "message",
					"role": "assistant",
					"content": []map[string]any{{
						"type": "output_text",
						"text": strings.Repeat("phase3-openai-responses-usage-", 2048),
					}},
				}},
				"usage": map[string]any{"input_tokens": 19, "output_tokens": 23, "total_tokens": 42},
			},
			responseField: "id",
			responseValue: "resp_phase3_streaming_first_openai_responses",
			wantInput:     19,
			wantOutput:    23,
			wantTotal:     42,
		},
		{
			name:      "Anthropic",
			apiFamily: "anthropic",
			requestPath: func(string) string {
				return "/v1/messages"
			},
			requestBody: func(publicModelID string) any {
				return map[string]any{
					"model":      publicModelID,
					"max_tokens": 32,
					"messages":   []map[string]any{{"role": "user", "content": "phase-3 streaming-first anthropic usage"}},
				}
			},
			responseBody: map[string]any{
				"id":   "msg-phase3-streaming-first-anthropic",
				"type": "message",
				"content": []map[string]any{{
					"type": "text",
					"text": strings.Repeat("phase3-anthropic-usage-", 2048),
				}},
				"usage": map[string]any{"input_tokens": 5, "output_tokens": 8},
			},
			responseField: "id",
			responseValue: "msg-phase3-streaming-first-anthropic",
			wantInput:     5,
			wantOutput:    8,
			wantTotal:     13,
		},
		{
			name:      "Gemini",
			apiFamily: "gemini",
			requestPath: func(publicModelID string) string {
				return fmt.Sprintf("/v1beta/models/%s:generateContent", publicModelID)
			},
			requestBody: func(string) any {
				return runtimePhase0GeminiRequest("phase-3 streaming-first gemini usage")
			},
			responseBody: map[string]any{
				"responseId": "gemini-phase3-streaming-first",
				"candidates": []map[string]any{{
					"content": map[string]any{"parts": []map[string]any{{"text": strings.Repeat("phase3-gemini-usage-", 2048)}}},
				}},
				"usageMetadata": map[string]any{"promptTokenCount": 11, "candidatesTokenCount": 17, "totalTokenCount": 28},
			},
			responseField: "responseId",
			responseValue: "gemini-phase3-streaming-first",
			wantInput:     11,
			wantOutput:    17,
			wantTotal:     28,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			upstream := newScriptedUpstream(t, http.StatusOK, test.responseBody)

			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:       profileID,
				APIFamily:       test.apiFamily,
				PublicModelID:   "phase3-streaming-first-usage-public-" + randomSuffix(),
				TargetModelID:   "phase3-streaming-first-usage-target-" + randomSuffix(),
				EndpointBaseURL: upstream.baseURL("/phase3/streaming-first/usage/" + strings.ToLower(test.name)),
				EndpointAPIKey:  "phase3-streaming-first-usage-key",
			})

			response := harness.requestJSON(t, http.MethodPost, test.requestPath(route.PublicModelID), test.requestBody(route.PublicModelID), nil)
			assertStatus(t, response, http.StatusOK)
			assertResponseField(t, response, test.responseField, test.responseValue)

			assertLatestRequestLogUsage(t, harness.conn, profileID, false, test.wantInput, test.wantOutput, test.wantTotal)
			assertLatestUsageEventUsage(t, harness.conn, profileID, test.wantInput, test.wantOutput, test.wantTotal)
			assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
			assertLatestRuntimeUsageEventTiming(t, harness.conn, profileID)
		})
	}
}

func TestRuntimeStreamingFirstBufferedFallbacks(t *testing.T) {
	t.Run("BodyRewriteRequiresBufferedBytes", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newArrivalRecordingUpstream(t)
		defer upstream.close()

		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:       profileID,
			APIFamily:       "openai",
			PublicModelID:   "phase3-streaming-first-buffered-rewrite-public-" + randomSuffix(),
			TargetModelID:   "phase3-streaming-first-buffered-rewrite-target-" + randomSuffix(),
			EndpointBaseURL: upstream.baseURL("/phase3/streaming-first/buffered/rewrite"),
			EndpointAPIKey:  "phase3-streaming-first-buffered-rewrite-key",
		})

		rawBody := mustMarshalBenchmarkJSON(t, map[string]any{
			"messages": []map[string]any{{"role": "user", "content": strings.Repeat("phase3-streaming-first-buffered-rewrite-", 4096)}},
			"model":    route.PublicModelID,
		})
		result := performSplitRuntimeRequest(t, harness.client, harness.url+"/v1/chat/completions", rawBody, upstream.assertNotStartedWithin)
		if result.Err != nil {
			t.Fatalf("expected buffered rewrite fallback request to succeed, got error: %v", result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected buffered rewrite fallback status 200, got %d with body %s", result.StatusCode, result.Body)
		}
		if got := requestModelID(t, upstream.waitForRequestBody(t, 5*time.Second)); got != route.TargetModelID {
			t.Fatalf("expected buffered rewrite fallback to preserve model rewrite to %q, got %q", route.TargetModelID, got)
		}
	})

	t.Run("ReplayableGeminiBytesStayBufferedWhenMultipleConnectionsExist", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		primaryUpstream := newArrivalRecordingUpstream(t)
		defer primaryUpstream.close()
		secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"responseId": "gemini-phase3-secondary", "usageMetadata": map[string]any{"promptTokenCount": 7, "candidatesTokenCount": 13, "totalTokenCount": 20}})

		suffix := randomSuffix()
		publicModelID := "phase3-streaming-first-buffered-gemini-public-" + suffix
		targetModelID := "phase3-streaming-first-buffered-gemini-target-" + suffix
		strategyID := harness.seedLegacyStrategy(t, profileID, "phase3-streaming-first-buffered-gemini-"+suffix, "fill-first")
		targetModelConfigID := harness.seedModel(t, profileID, "gemini", targetModelID, "native", &strategyID)
		publicModelConfigID := harness.seedModel(t, profileID, "gemini", publicModelID, "proxy", nil)
		harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
		primaryEndpointID := harness.seedEndpoint(t, profileID, "phase3-streaming-first-buffered-gemini-primary-"+suffix, primaryUpstream.baseURL("/phase3/streaming-first/buffered/gemini/primary"), "phase3-streaming-first-buffered-gemini-primary-key", 0)
		secondaryEndpointID := harness.seedEndpoint(t, profileID, "phase3-streaming-first-buffered-gemini-secondary-"+suffix, secondaryUpstream.baseURL("/phase3/streaming-first/buffered/gemini/secondary"), "phase3-streaming-first-buffered-gemini-secondary-key", 1)
		harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "phase3-streaming-first-buffered-gemini-primary-connection-"+suffix, nil, nil, 0)
		harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "phase3-streaming-first-buffered-gemini-secondary-connection-"+suffix, nil, nil, 1)

		rawBody := mustMarshalBenchmarkJSON(t, runtimePhase0GeminiRequest(strings.Repeat("phase3-streaming-first-buffered-gemini-", 4096)))
		requestPath := fmt.Sprintf("%s/v1beta/models/%s:generateContent", harness.url, publicModelID)
		result := performSplitRuntimeRequest(t, harness.client, requestPath, rawBody, primaryUpstream.assertNotStartedWithin)
		if result.Err != nil {
			t.Fatalf("expected replayable gemini buffered request to succeed, got error: %v", result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected replayable gemini buffered request status 200, got %d with body %s", result.StatusCode, result.Body)
		}
		if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected replayable gemini request to complete on the primary connection without failover, got %d secondary requests", got)
		}
		if got := string(primaryUpstream.waitForRequestBody(t, 5*time.Second)); !strings.Contains(got, "phase3-streaming-first-buffered-gemini-") {
			t.Fatalf("expected buffered gemini request body to reach the primary upstream intact, got %q", got)
		}
	})
}

func assertLatestRuntimeUsageEventTiming(t *testing.T, conn *pgx.Conn, profileID int) {
	t.Helper()
	waitForRuntimeTelemetryCounts(t, conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	var completionDurationMS sql.NullInt64
	if err := conn.QueryRow(
		context.Background(),
		`SELECT completion_duration_ms FROM usage_request_events WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&completionDurationMS); err != nil {
		t.Fatalf("load latest runtime usage-event timing: %v", err)
	}
	if !completionDurationMS.Valid || completionDurationMS.Int64 < 1 {
		t.Fatalf("expected latest usage_request_events row to persist positive completion_duration_ms, got %+v", completionDurationMS)
	}
}

func performSplitRuntimeRequest(t *testing.T, client *http.Client, url string, rawBody []byte, assertNotStarted func(t *testing.T, timeout time.Duration)) concurrentRuntimeRequestResult {
	t.Helper()
	splitAt := len(rawBody) / 2
	pipeReader, pipeWriter := io.Pipe()
	request, err := http.NewRequest(http.MethodPost, url, pipeReader)
	if err != nil {
		t.Fatalf("build split runtime request %s: %v", url, err)
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
		t.Fatalf("write first split runtime request body chunk: %v", err)
	}
	assertNotStarted(t, runtimeStreamingAssertionDeadline)
	if _, err := pipeWriter.Write(rawBody[splitAt:]); err != nil {
		t.Fatalf("write second split runtime request body chunk: %v", err)
	}
	if err := pipeWriter.Close(); err != nil {
		t.Fatalf("close split runtime request body writer: %v", err)
	}
	return awaitAsyncRequest(t, resultCh, 5*time.Second)
}
