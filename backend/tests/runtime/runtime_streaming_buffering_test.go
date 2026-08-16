package runtimetest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

const runtimeStreamingAssertionDeadline = 500 * time.Millisecond

func TestRuntimeResponseStreamingUsageCoverage(t *testing.T) {
	tests := []struct {
		name          string
		apiFamily     string
		operation     string
		prompt        string
		rawResponse   string
		responseField string
		responseValue string
		wantInput     int64
		wantOutput    int64
		wantTotal     int64
	}{
		{
			name:          "OpenAIChat",
			apiFamily:     "openai",
			operation:     "chat",
			prompt:        "preserve usage during response streaming",
			rawResponse:   fmt.Sprintf(`{"id":"chatcmpl-phase-2-streaming-usage","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"%s"}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`, strings.Repeat("phase-2-stream-usage-", 32768)),
			responseField: "id",
			responseValue: "chatcmpl-phase-2-streaming-usage",
			wantInput:     7,
			wantOutput:    13,
			wantTotal:     20,
		},
		{
			name:          "OpenAINestedUsageSpoof",
			apiFamily:     "openai",
			operation:     "chat",
			prompt:        "ignore nested spoofed usage during response streaming",
			rawResponse:   `{"id":"chatcmpl-phase-2-streaming-usage-security","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_json","value":{"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}}]}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`,
			responseField: "id",
			responseValue: "chatcmpl-phase-2-streaming-usage-security",
			wantInput:     7,
			wantOutput:    13,
			wantTotal:     20,
		},
		{
			name:          "OpenAIResponses",
			apiFamily:     "openai",
			operation:     "responses",
			prompt:        "streaming responses usage",
			rawResponse:   fmt.Sprintf(`{"id":"resp_streaming_openai_responses","object":"response","model":"streaming-responses-target","output":[{"id":"msg_streaming_openai_responses","type":"message","role":"assistant","content":[{"type":"output_text","text":"%s"}]}],"usage":{"input_tokens":19,"output_tokens":23,"total_tokens":42}}`, strings.Repeat("streaming-openai-responses-usage-", 2048)),
			responseField: "id",
			responseValue: "resp_streaming_openai_responses",
			wantInput:     19,
			wantOutput:    23,
			wantTotal:     42,
		},
		{
			name:          "Anthropic",
			apiFamily:     "anthropic",
			operation:     "messages",
			prompt:        "streaming anthropic usage",
			rawResponse:   fmt.Sprintf(`{"id":"msg-streaming-anthropic","type":"message","content":[{"type":"text","text":"%s"}],"usage":{"input_tokens":5,"output_tokens":8}}`, strings.Repeat("streaming-anthropic-usage-", 2048)),
			responseField: "id",
			responseValue: "msg-streaming-anthropic",
			wantInput:     5,
			wantOutput:    8,
			wantTotal:     13,
		},
		{
			name:          "Gemini",
			apiFamily:     "gemini",
			operation:     "generate",
			prompt:        "streaming gemini usage",
			rawResponse:   fmt.Sprintf(`{"responseId":"gemini-streaming-usage","candidates":[{"content":{"parts":[{"text":"%s"}]}}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":17,"totalTokenCount":28}}`, strings.Repeat("streaming-gemini-usage-", 2048)),
			responseField: "responseId",
			responseValue: "gemini-streaming-usage",
			wantInput:     11,
			wantOutput:    17,
			wantTotal:     28,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			endpointBaseURL := newRuntimeStreamingUsageBaseURL(t, test.rawResponse)

			route := harness.seedProxyRoute(t, runtimeRouteSeed{
				ProfileID:       profileID,
				APIFamily:       test.apiFamily,
				PublicModelID:   "streaming-usage-public-" + randomSuffix(),
				TargetModelID:   "streaming-usage-target-" + randomSuffix(),
				EndpointBaseURL: endpointBaseURL,
				EndpointAPIKey:  "streaming-usage-key",
			})

			requestPath, requestBody := runtimeStreamingUsageRequest(t, test.operation, route.PublicModelID, test.prompt)
			response := harness.requestJSON(t, http.MethodPost, requestPath, requestBody, nil)
			assertStatus(t, response, http.StatusOK)
			assertResponseField(t, response, test.responseField, test.responseValue)
			assertLatestRequestLogUsage(t, harness.conn, profileID, false, test.wantInput, test.wantOutput, test.wantTotal)
			assertLatestUsageEventUsage(t, harness.conn, profileID, test.wantInput, test.wantOutput, test.wantTotal)
		})
	}
}

func newRuntimeStreamingUsageBaseURL(t *testing.T, rawResponse string) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, rawResponse)
	}))
	t.Cleanup(upstream.Close)
	return upstream.URL
}

func runtimeStreamingUsageRequest(t *testing.T, operation string, publicModelID string, prompt string) (string, any) {
	t.Helper()
	switch operation {
	case "chat":
		return "/v1/chat/completions", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": prompt}},
			"model":    publicModelID,
		}
	case "responses":
		return "/v1/responses", map[string]any{"model": publicModelID, "input": prompt}
	case "messages":
		return "/v1/messages", map[string]any{
			"model":      publicModelID,
			"max_tokens": 32,
			"messages":   []map[string]any{{"role": "user", "content": prompt}},
		}
	case "generate":
		return fmt.Sprintf("/v1beta/models/%s:generateContent", publicModelID), runtimePhase0GeminiRequest(prompt)
	default:
		t.Fatalf("unsupported streaming usage operation %q", operation)
		return "", nil
	}
}

func TestRuntimeStreamingPassthroughBecomesReadableBeforeUpstreamCompletion(t *testing.T) {
	harness, profileID, upstream, publicModelID := newRuntimeLargeResponseRoute(t, "streaming-passthrough-public-", "streaming-passthrough-target-", "/streaming/passthrough", "streaming-passthrough-key")
	defer upstream.close()

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "streaming passthrough becomes readable before upstream completion"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	upstream.waitUntilFirstChunk(t, 5*time.Second)

	readCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		buffer := make([]byte, 64)
		n, err := response.Body.Read(buffer)
		if err != nil && err != io.EOF {
			errCh <- err
			return
		}
		readCh <- append([]byte(nil), buffer[:n]...)
	}()

	var firstChunk []byte
	select {
	case err := <-errCh:
		t.Fatalf("expected streamed response bytes before upstream completion, got read error: %v", err)
	case firstChunk = <-readCh:
		if len(firstChunk) == 0 {
			t.Fatal("expected at least one streamed response byte before upstream completion")
		}
	case <-time.After(runtimeStreamingAssertionDeadline):
		t.Fatal("expected response body to become readable before upstream finished; non-SSE response is still fully buffered")
	}

	upstream.releaseResponse()
	rest, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read streaming passthrough response tail: %v", err)
	}
	payload := string(append(firstChunk, rest...))
	if !strings.Contains(payload, `"id":"chatcmpl-phase-2-large-response"`) {
		t.Fatalf("expected streamed response payload to pass through the upstream id, got %q", payload)
	}
	if !strings.Contains(payload, `"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}`) {
		t.Fatalf("expected streamed response payload to pass through upstream usage, got %q", payload)
	}

	assertLatestRequestLogUsage(t, harness.conn, profileID, false, 7, 13, 20)
	assertLatestUsageEventUsage(t, harness.conn, profileID, 7, 13, 20)
}

func TestRuntimeLargeNonStreamResponseWaitsForDurableHandoffBeforeCommit(t *testing.T) {
	harness, profileID, upstream, publicModelID := newRuntimeLargeResponseRoute(t, "phase-2-large-response-public-", "phase-2-large-response-target-", "/phase-2/large-response", "phase-2-large-response-key")
	defer upstream.close()

	resultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "wait for durable handoff before non-sse response commit"}},
		"model":    publicModelID,
	}, nil)
	upstream.waitUntilFirstChunk(t, 5*time.Second)

	select {
	case result := <-resultCh:
		t.Fatalf("expected non-stream response to wait for upstream completion and durable handoff before commit, got %+v", result)
	case <-time.After(runtimeStreamingAssertionDeadline):
	}

	upstream.releaseResponse()
	result := awaitAsyncRequest(t, resultCh, 5*time.Second)
	if result.Err != nil {
		t.Fatalf("expected durable handoff response to complete after upstream release, got %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected durable handoff response status 200, got %d with body %s", result.StatusCode, result.Body)
	}
	if !strings.Contains(result.Body, "chatcmpl-phase-2-large-response") {
		t.Fatalf("expected provider body after durable handoff, got %s", result.Body)
	}
	assertLatestRequestLogUsage(t, harness.conn, profileID, false, 7, 13, 20)
}

func newRuntimeLargeResponseRoute(t *testing.T, publicPrefix string, targetPrefix string, upstreamPath string, apiKey string) (*runtimeHarness, int, *chunkedLargeResponseUpstream, string) {
	t.Helper()
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newChunkedLargeResponseUpstream(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   publicPrefix + randomSuffix(),
		TargetModelID:   targetPrefix + randomSuffix(),
		EndpointBaseURL: upstream.baseURL(upstreamPath),
		EndpointAPIKey:  apiKey,
	})
	return harness, profileID, upstream, route.PublicModelID
}

func TestRuntimeRequestCancellationStopsUpstream(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newCancellationAwareUpstream(t)
	defer upstream.close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "phase-2-cancel-public-" + randomSuffix(),
		TargetModelID:   "phase-2-cancel-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/phase-2/cancel"),
		EndpointAPIKey:  "phase-2-cancel-key",
	})

	pipeReader, pipeWriter := io.Pipe()
	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent", harness.url, route.PublicModelID), pipeReader)
	if err != nil {
		t.Fatalf("build cancellable runtime request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	resultCh := make(chan concurrentRuntimeRequestResult, 1)
	go func() {
		response, requestErr := harness.client.Do(request)
		if requestErr != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: requestErr}
			return
		}
		defer func() { _ = response.Body.Close() }()
		_, _ = io.Copy(io.Discard, response.Body)
		resultCh <- concurrentRuntimeRequestResult{StatusCode: response.StatusCode}
	}()

	_, err = io.WriteString(pipeWriter, `{"contents":[{"role":"user","parts":[{"text":"`+strings.Repeat("phase-2-cancel-", 4096))
	if err != nil {
		t.Fatalf("write partial cancellable request body: %v", err)
	}
	upstream.waitUntilReadStarts(t, 5*time.Second)

	cancel()
	_ = pipeWriter.CloseWithError(context.Canceled)
	upstream.waitUntilCanceled(t, 5*time.Second)
	upstream.waitUntilDone(t, 5*time.Second)

	result := awaitAsyncRequest(t, resultCh, 5*time.Second)
	if result.Err == nil {
		t.Fatalf("expected client-side cancellation to abort the runtime request, got status %d", result.StatusCode)
	}
	state := loadRuntimeState(t, harness, profileID, route.ConnectionID)
	if state.CycleRetryAttempts != 0 || state.CumulativeRetryAttempts != 0 || state.NextRetryAt.Valid || state.InFlightStream != 0 {
		t.Fatalf("expected caller cancellation to leave retry/Ban state clean and release its lease, got %+v", state)
	}
	if events := queryLoadbalanceEvents(t, harness.conn, profileID, route.ConnectionID); len(events) != 0 {
		t.Fatalf("expected caller cancellation not to persist load-balance failure events, got %+v", events)
	}
}

func TestRuntimeBufferedFallbacks(t *testing.T) {
	t.Run("RewritePathKeepsBytesBufferedUntilModelRewriteIsSafe", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newArrivalRecordingUpstream(t)
		defer upstream.close()

		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:       profileID,
			APIFamily:       "openai",
			PublicModelID:   "buffered-fallback-public-" + randomSuffix(),
			TargetModelID:   "buffered-fallback-target-" + randomSuffix(),
			EndpointBaseURL: upstream.baseURL("/buffered-fallback"),
			EndpointAPIKey:  "buffered-fallback-key",
		})

		rawBody := mustMarshalBenchmarkJSON(t, map[string]any{
			"messages": []map[string]any{{"role": "user", "content": strings.Repeat("buffered-fallback-", 4096)}},
			"model":    route.PublicModelID,
		})
		result := performSplitRuntimeRequest(t, harness.client, harness.url+"/v1/chat/completions", rawBody, upstream.assertNotStartedWithin)
		if result.Err != nil {
			t.Fatalf("expected buffered-fallback request to succeed, got error: %v", result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected buffered-fallback request status 200, got %d with body %s", result.StatusCode, result.Body)
		}
		if got := requestModelID(t, upstream.waitForRequestBody(t, 5*time.Second)); got != route.TargetModelID {
			t.Fatalf("expected buffered fallback to preserve model rewrite to %q, got %q", route.TargetModelID, got)
		}
	})

	t.Run("ReplayableGeminiBytesStayBufferedWhenMultipleConnectionsExist", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		primaryUpstream := newArrivalRecordingUpstream(t)
		defer primaryUpstream.close()
		secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
			"responseId":    "gemini-streaming-secondary",
			"usageMetadata": map[string]any{"promptTokenCount": 7, "candidatesTokenCount": 13, "totalTokenCount": 20},
		})

		suffix := randomSuffix()
		publicModelID := "buffered-gemini-public-" + suffix
		targetModelID := "buffered-gemini-target-" + suffix
		strategyID := harness.seedLegacyStrategy(t, profileID, "buffered-gemini-"+suffix, "fill-first")
		targetModelConfigID := harness.seedModel(t, profileID, "gemini", targetModelID, "native", &strategyID)
		publicModelConfigID := harness.seedModel(t, profileID, "gemini", publicModelID, "proxy", nil)
		harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
		primaryEndpointID := harness.seedEndpoint(t, profileID, "buffered-gemini-primary-"+suffix, primaryUpstream.baseURL("/buffered/gemini/primary"), "buffered-gemini-primary-key")
		secondaryEndpointID := harness.seedEndpoint(t, profileID, "buffered-gemini-secondary-"+suffix, secondaryUpstream.baseURL("/buffered/gemini/secondary"), "buffered-gemini-secondary-key")
		harness.seedConnection(t, profileID, targetModelConfigID, primaryEndpointID, "buffered-gemini-primary-connection-"+suffix, nil, nil, 0)
		harness.seedConnection(t, profileID, targetModelConfigID, secondaryEndpointID, "buffered-gemini-secondary-connection-"+suffix, nil, nil, 1)

		rawBody := mustMarshalBenchmarkJSON(t, runtimePhase0GeminiRequest(strings.Repeat("buffered-gemini-", 4096)))
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
		if got := string(primaryUpstream.waitForRequestBody(t, 5*time.Second)); !strings.Contains(got, "buffered-gemini-") {
			t.Fatalf("expected buffered gemini request body to reach the primary upstream intact, got %q", got)
		}
	})
}

func assertLatestUsageEventUsage(t *testing.T, conn *pgx.Conn, profileID int, wantInput int64, wantOutput int64, wantTotal int64) {
	t.Helper()
	var inputTokens sql.NullInt64
	var outputTokens sql.NullInt64
	var totalTokens sql.NullInt64
	if err := conn.QueryRow(
		context.Background(),
		`SELECT input_tokens, output_tokens, total_tokens FROM usage_request_events WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&inputTokens, &outputTokens, &totalTokens); err != nil {
		t.Fatalf("load latest runtime usage event usage: %v", err)
	}
	if !inputTokens.Valid || inputTokens.Int64 != wantInput || !outputTokens.Valid || outputTokens.Int64 != wantOutput || !totalTokens.Valid || totalTokens.Int64 != wantTotal {
		t.Fatalf("expected usage_request_events usage %d/%d/%d, got input=%+v output=%+v total=%+v", wantInput, wantOutput, wantTotal, inputTokens, outputTokens, totalTokens)
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

type readChunkResult struct {
	BytesRead int
	Err       error
}

type chunkedLargeResponseUpstream struct {
	server      *httptest.Server
	firstChunk  chan struct{}
	release     chan struct{}
	firstOnce   sync.Once
	releaseOnce sync.Once
}

func newChunkedLargeResponseUpstream(t *testing.T) *chunkedLargeResponseUpstream {
	t.Helper()
	upstream := &chunkedLargeResponseUpstream{
		firstChunk: make(chan struct{}),
		release:    make(chan struct{}),
	}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("chunked large-response upstream writer does not support flushing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"chatcmpl-phase-2-large-response","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"`+strings.Repeat("phase-2-large-response-", 16384))
		flusher.Flush()
		upstream.firstOnce.Do(func() { close(upstream.firstChunk) })
		<-upstream.release
		_, _ = io.WriteString(w, `"}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`)
	}))
	return upstream
}

func (u *chunkedLargeResponseUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *chunkedLargeResponseUpstream) waitUntilFirstChunk(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.firstChunk:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for the first large response chunk")
	}
}

func (u *chunkedLargeResponseUpstream) releaseResponse() {
	u.releaseOnce.Do(func() {
		close(u.release)
	})
}

func (u *chunkedLargeResponseUpstream) close() {
	u.releaseResponse()
	u.server.Close()
}

type cancellationAwareUpstream struct {
	server      *httptest.Server
	readStarted chan struct{}
	canceled    chan struct{}
	done        chan struct{}
	startOnce   sync.Once
	cancelOnce  sync.Once
	doneOnce    sync.Once
}

func newCancellationAwareUpstream(t *testing.T) *cancellationAwareUpstream {
	t.Helper()
	upstream := &cancellationAwareUpstream{
		readStarted: make(chan struct{}),
		canceled:    make(chan struct{}),
		done:        make(chan struct{}),
	}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		go func() {
			<-r.Context().Done()
			upstream.cancelOnce.Do(func() { close(upstream.canceled) })
		}()
		defer upstream.doneOnce.Do(func() { close(upstream.done) })
		buffer := make([]byte, 32*1024)
		for {
			n, err := r.Body.Read(buffer)
			if n > 0 {
				upstream.startOnce.Do(func() { close(upstream.readStarted) })
			}
			if err != nil {
				return
			}
		}
	}))
	return upstream
}

func (u *cancellationAwareUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *cancellationAwareUpstream) waitUntilReadStarts(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.readStarted:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for upstream request streaming to start")
	}
}

func (u *cancellationAwareUpstream) waitUntilCanceled(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.canceled:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for upstream cancellation")
	}
}

func (u *cancellationAwareUpstream) waitUntilDone(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.done:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for canceled upstream handler to exit")
	}
}

func (u *cancellationAwareUpstream) close() {
	u.server.Close()
}

type arrivalRecordingUpstream struct {
	server      *httptest.Server
	started     chan struct{}
	requestBody chan []byte
	startOnce   sync.Once
}

func newArrivalRecordingUpstream(t *testing.T) *arrivalRecordingUpstream {
	t.Helper()
	upstream := &arrivalRecordingUpstream{
		started:     make(chan struct{}),
		requestBody: make(chan []byte, 1),
	}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.startOnce.Do(func() { close(upstream.started) })
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read arrival-recording upstream body: %v", err)
		}
		_ = r.Body.Close()
		upstream.requestBody <- append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-phase-2-buffered-fallback"})
	}))
	return upstream
}

func (u *arrivalRecordingUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *arrivalRecordingUpstream) assertNotStartedWithin(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.started:
		t.Fatal("expected unsafe rewrite path to keep buffering before contacting upstream")
	case <-time.After(timeout):
	}
}

func (u *arrivalRecordingUpstream) waitForRequestBody(t *testing.T, timeout time.Duration) []byte {
	t.Helper()
	select {
	case body := <-u.requestBody:
		return body
	case <-time.After(timeout):
		t.Fatal("timed out waiting for buffered-fallback upstream request")
		return nil
	}
}

func (u *arrivalRecordingUpstream) close() {
	u.server.Close()
}

func TestRuntimeStreamingDoesNotRetryAfterFirstEvent(t *testing.T) {
	tests := []struct {
		name              string
		modelIDPrefix     string
		apiFamily         string
		operation         string
		prompt            string
		primaryStreamBody string
		primaryContains   string
		secondaryResponse map[string]any
	}{
		{
			name:              "OpenAIChat",
			modelIDPrefix:     "no-retry-stream-public-",
			apiFamily:         "openai",
			operation:         "chat",
			prompt:            "stream must not retry after first event",
			primaryStreamBody: "data: {\"id\":\"chatcmpl-no-retry-stream\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n",
			primaryContains:   "partial",
			secondaryResponse: map[string]any{"id": "chatcmpl-stream-secondary"},
		},
		{
			name:              "Anthropic",
			modelIDPrefix:     "no-retry-anthropic-stream-public-",
			apiFamily:         "anthropic",
			operation:         "messages",
			prompt:            "stream must not retry after Anthropic first event",
			primaryStreamBody: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial anthropic\"}}\n\n",
			primaryContains:   "partial anthropic",
			secondaryResponse: map[string]any{"id": "msg-stream-secondary", "type": "message"},
		},
		{
			name:              "GeminiPartialReadFailure",
			modelIDPrefix:     "no-retry-gemini-stream-public-",
			apiFamily:         "gemini",
			operation:         "streamGenerate",
			prompt:            "stream must not retry after Gemini partial event",
			primaryStreamBody: "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial gemini\"}]}}]}\n\n",
			primaryContains:   "partial gemini",
			secondaryResponse: map[string]any{"responseId": "gemini-stream-secondary"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			modelID := test.modelIDPrefix + randomSuffix()
			var primaryMu sync.Mutex
			primaryRequests := 0
			primaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				_ = r.Body.Close()
				primaryMu.Lock()
				primaryRequests++
				primaryMu.Unlock()
				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Fatal("streaming upstream writer does not support flushing")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, test.primaryStreamBody)
				flusher.Flush()
			}))
			defer primaryUpstream.Close()
			secondaryUpstream := newScriptedUpstream(t, http.StatusOK, test.secondaryResponse)

			switch test.apiFamily {
			case "openai":
				seedRetryPolicyNativeRoute(t, harness, profileID, modelID, primaryUpstream.URL+"/retry/stream/primary", secondaryUpstream.baseURL("/retry/stream/secondary"))
			default:
				seedRetryPolicyRoute(t, harness, profileID, test.apiFamily, modelID, primaryUpstream.URL+"/retry/"+strings.ToLower(test.name)+"/primary", secondaryUpstream.baseURL("/retry/"+strings.ToLower(test.name)+"/secondary"))
			}

			requestPath, requestBody := runtimeStreamingRetryRequest(t, test.operation, modelID, test.prompt)
			response := harness.requestJSON(t, http.MethodPost, requestPath, requestBody, nil)
			assertStatus(t, response, http.StatusOK)
			if body := readResponseBody(t, response); !strings.Contains(body, test.primaryContains) {
				t.Fatalf("expected streaming payload containing %q, got %q", test.primaryContains, body)
			}
			primaryMu.Lock()
			gotPrimary := primaryRequests
			primaryMu.Unlock()
			if gotPrimary != 1 {
				t.Fatalf("expected primary stream to receive one request, got %d", gotPrimary)
			}
			assertNoScriptedUpstreamRequests(t, secondaryUpstream, "post-commit stream retry candidate")
		})
	}
}

func runtimeStreamingRetryRequest(t *testing.T, operation string, modelID string, prompt string) (string, any) {
	t.Helper()
	switch operation {
	case "chat":
		return "/v1/chat/completions", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": prompt}},
			"model":    modelID,
			"stream":   true,
		}
	case "messages":
		return "/v1/messages", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": prompt}},
			"model":    modelID,
			"stream":   true,
		}
	case "streamGenerate":
		return "/v1beta/models/" + modelID + ":streamGenerateContent", map[string]any{
			"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": prompt}}}},
		}
	default:
		t.Fatalf("unsupported streaming retry operation %q", operation)
		return "", nil
	}
}

func seedRetryPolicyRoute(t *testing.T, harness *runtimeHarness, profileID int, apiFamily string, modelID string, primaryBaseURL string, secondaryBaseURL string) {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "retry-policy-"+apiFamily+"-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, apiFamily, modelID, "native", &strategyID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-"+apiFamily+"-primary-"+randomSuffix(), primaryBaseURL, "retry-policy-"+apiFamily+"-primary-key")
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-"+apiFamily+"-secondary-"+randomSuffix(), secondaryBaseURL, "retry-policy-"+apiFamily+"-secondary-key")
	harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "retry-policy-"+apiFamily+"-primary-connection-"+randomSuffix(), nil, nil, 0)
	harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "retry-policy-"+apiFamily+"-secondary-connection-"+randomSuffix(), nil, nil, 1)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
}
