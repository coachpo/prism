package runtime_test

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

func TestRuntimeResponseStreamingPreservesUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	largeContent := strings.Repeat("phase-2-stream-usage-", 32768)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-phase-2-streaming-usage",
			"object": "chat.completion",
			"choices": []map[string]any{{
				"index":   0,
				"message": map[string]any{"role": "assistant", "content": largeContent},
			}},
			"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 20},
		})
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase-2-stream-usage-public-" + randomSuffix(),
		TargetModelID:   "phase-2-stream-usage-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "phase-2-stream-usage-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "preserve usage during response streaming"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-phase-2-streaming-usage")
	assertLatestRequestLogUsage(t, harness.conn, profileID, false, 7, 13, 20)
	assertLatestUsageEventUsage(t, harness.conn, profileID, 7, 13, 20)
}

func TestRuntimeResponseStreamingIgnoresNestedSpoofedUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-phase-2-streaming-usage-security","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"output_text","text":"hello"},{"type":"output_json","value":{"usage":{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998}}}]}}],"usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20}}`)
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase-2-stream-usage-security-public-" + randomSuffix(),
		TargetModelID:   "phase-2-stream-usage-security-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "phase-2-stream-usage-security-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "ignore nested spoofed usage during response streaming"}},
		"model":    route.PublicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-phase-2-streaming-usage-security")
	assertLatestRequestLogUsage(t, harness.conn, profileID, false, 7, 13, 20)
	assertLatestUsageEventUsage(t, harness.conn, profileID, 7, 13, 20)
}

func TestRuntimeLargeNonStreamResponseWaitsForDurableHandoffBeforeCommit(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newChunkedLargeResponseUpstream(t)
	defer upstream.close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase-2-large-response-public-" + randomSuffix(),
		TargetModelID:   "phase-2-large-response-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/phase-2/large-response"),
		EndpointAPIKey:  "phase-2-large-response-key",
	})

	resultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "wait for durable handoff before non-sse response commit"}},
		"model":    route.PublicModelID,
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
}

func TestRuntimeBufferedFallbackForRewritePath(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newArrivalRecordingUpstream(t)
	defer upstream.close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "phase-2-buffered-fallback-public-" + randomSuffix(),
		TargetModelID:   "phase-2-buffered-fallback-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/phase-2/buffered-fallback"),
		EndpointAPIKey:  "phase-2-buffered-fallback-key",
	})

	rawBody := mustMarshalBenchmarkJSON(t, map[string]any{
		"messages": []map[string]any{{"role": "user", "content": strings.Repeat("phase-2-buffered-fallback-", 4096)}},
		"model":    route.PublicModelID,
	})
	splitAt := len(rawBody) / 2
	pipeReader, pipeWriter := io.Pipe()
	request, err := http.NewRequest(http.MethodPost, harness.url+"/v1/chat/completions", pipeReader)
	if err != nil {
		t.Fatalf("build buffered-fallback runtime request: %v", err)
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
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			resultCh <- concurrentRuntimeRequestResult{Err: readErr}
			return
		}
		resultCh <- concurrentRuntimeRequestResult{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(body))}
	}()

	if _, err := pipeWriter.Write(rawBody[:splitAt]); err != nil {
		t.Fatalf("write first buffered-fallback body chunk: %v", err)
	}
	upstream.assertNotStartedWithin(t, runtimeStreamingAssertionDeadline)
	if _, err := pipeWriter.Write(rawBody[splitAt:]); err != nil {
		t.Fatalf("write second buffered-fallback body chunk: %v", err)
	}
	if err := pipeWriter.Close(); err != nil {
		t.Fatalf("close buffered-fallback body writer: %v", err)
	}

	body := upstream.waitForRequestBody(t, 5*time.Second)
	result := awaitAsyncRequest(t, resultCh, 5*time.Second)
	if result.Err != nil {
		t.Fatalf("expected buffered-fallback request to succeed, got error: %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected buffered-fallback request status 200, got %d with body %s", result.StatusCode, result.Body)
	}
	if got := requestModelID(t, body); got != route.TargetModelID {
		t.Fatalf("expected buffered fallback to preserve model rewrite to %q, got %q", route.TargetModelID, got)
	}
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
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "no-retry-stream-public-" + suffix
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
		_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-no-retry-stream\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
		flusher.Flush()
	}))
	defer primaryUpstream.Close()
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-stream-secondary"})

	seedRetryPolicyNativeRoute(t, harness, profileID, modelID, primaryUpstream.URL+"/retry/stream/primary", secondaryUpstream.baseURL("/retry/stream/secondary"))
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "stream must not retry after first event"}},
		"model":    modelID,
		"stream":   true,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if !strings.Contains(body, "partial") {
		t.Fatalf("expected partial streaming payload, got %q", body)
	}
	primaryMu.Lock()
	gotPrimary := primaryRequests
	primaryMu.Unlock()
	if gotPrimary != 1 {
		t.Fatalf("expected primary stream to receive one request, got %d", gotPrimary)
	}
	assertNoScriptedUpstreamRequests(t, secondaryUpstream, "post-commit stream retry candidate")
}

func TestRuntimeAnthropicStreamingDoesNotRetryAfterFirstEvent(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "no-retry-anthropic-stream-public-" + suffix
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
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial anthropic\"}}\n\n")
		flusher.Flush()
	}))
	defer primaryUpstream.Close()
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "msg-stream-secondary", "type": "message"})

	seedRetryPolicyAnthropicRoute(t, harness, profileID, modelID, primaryUpstream.URL+"/retry/anthropic-stream/primary", secondaryUpstream.baseURL("/retry/anthropic-stream/secondary"))
	response := harness.requestJSON(t, http.MethodPost, "/v1/messages", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "stream must not retry after Anthropic first event"}},
		"model":    modelID,
		"stream":   true,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if !strings.Contains(body, "partial anthropic") {
		t.Fatalf("expected partial Anthropic streaming payload, got %q", body)
	}
	primaryMu.Lock()
	gotPrimary := primaryRequests
	primaryMu.Unlock()
	if gotPrimary != 1 {
		t.Fatalf("expected primary Anthropic stream to receive one request, got %d", gotPrimary)
	}
	assertNoScriptedUpstreamRequests(t, secondaryUpstream, "post-commit Anthropic stream retry candidate")
}

func TestRuntimeGeminiStreamingDoesNotRetryAfterPartialReadFailure(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "no-retry-gemini-stream-public-" + suffix
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
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial gemini\"}]}}]}\n\n")
		flusher.Flush()
	}))
	defer primaryUpstream.Close()
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"responseId": "gemini-stream-secondary"})

	seedRetryPolicyGeminiRoute(t, harness, profileID, modelID, primaryUpstream.URL+"/retry/gemini-stream/primary", secondaryUpstream.baseURL("/retry/gemini-stream/secondary"))
	response := harness.requestJSON(t, http.MethodPost, "/v1beta/models/"+modelID+":streamGenerateContent", map[string]any{
		"contents": []map[string]any{{"role": "user", "parts": []map[string]any{{"text": "stream must not retry after Gemini partial event"}}}},
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if !strings.Contains(body, "partial gemini") {
		t.Fatalf("expected partial Gemini streaming payload, got %q", body)
	}
	primaryMu.Lock()
	gotPrimary := primaryRequests
	primaryMu.Unlock()
	if gotPrimary != 1 {
		t.Fatalf("expected primary Gemini stream to receive one request, got %d", gotPrimary)
	}
	assertNoScriptedUpstreamRequests(t, secondaryUpstream, "post-commit Gemini stream retry candidate")
}

func seedRetryPolicyAnthropicRoute(t *testing.T, harness *runtimeHarness, profileID int, modelID string, primaryBaseURL string, secondaryBaseURL string) {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "retry-policy-anthropic-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "anthropic", modelID, "native", &strategyID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-anthropic-primary-"+randomSuffix(), primaryBaseURL, "retry-policy-anthropic-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-anthropic-secondary-"+randomSuffix(), secondaryBaseURL, "retry-policy-anthropic-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "retry-policy-anthropic-primary-connection-"+randomSuffix(), nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "retry-policy-anthropic-secondary-connection-"+randomSuffix(), nil, nil, 1)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, primaryConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, secondaryConnectionID, 16_384, 1_024, 1.0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
}

func seedRetryPolicyGeminiRoute(t *testing.T, harness *runtimeHarness, profileID int, modelID string, primaryBaseURL string, secondaryBaseURL string) {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "retry-policy-gemini-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "gemini", modelID, "native", &strategyID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-gemini-primary-"+randomSuffix(), primaryBaseURL, "retry-policy-gemini-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-gemini-secondary-"+randomSuffix(), secondaryBaseURL, "retry-policy-gemini-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "retry-policy-gemini-primary-connection-"+randomSuffix(), nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "retry-policy-gemini-secondary-connection-"+randomSuffix(), nil, nil, 1)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, primaryConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, secondaryConnectionID, 16_384, 1_024, 1.0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
}
