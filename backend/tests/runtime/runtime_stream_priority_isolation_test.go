package runtime_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type priorityStreamingUpstream struct {
	server      *httptest.Server
	mu          sync.Mutex
	requests    []upstreamRequestSnapshot
	ready       chan struct{}
	release     chan struct{}
	readyOnce   sync.Once
	releaseOnce sync.Once
}

func TestRuntimeStreamPriorityIsolation(t *testing.T) {
	t.Run("StreamTTFT", runtimePriorityIsolationStreamTTFT)
}

func runtimePriorityIsolationStreamTTFT(t *testing.T) {
	harness := newRuntimeStatsHarness(t)
	profileID := harness.activeProfileID(t)
	harness.seedStatsPressureHistory(t, profileID, "priority-stream")

	statsLock := holdRuntimeStatsTablesLock(t, harness.databaseName)
	defer statsLock.release(t)

	pressureResults := []<-chan concurrentRuntimeRequestResult{
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/requests?limit=50&offset=0", nil, runtimeModelHeader(profileID)),
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/summary", nil, runtimeModelHeader(profileID)),
		startAsyncPriorityRequest(t, harness.client, http.MethodGet, harness.url+"/api/stats/throughput", nil, runtimeModelHeader(profileID)),
	}
	expectedPendingPressure := expectedPendingFirstShedRequests(len(pressureResults), harness.settings)
	waitForStatsLockWaiters(t, harness.conn, expectedPendingPressure, 5*time.Second)

	streamingUpstream := newPriorityStreamingUpstream(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "priority-stream-public-" + randomSuffix(),
		TargetModelID:   "priority-stream-target-" + randomSuffix(),
		EndpointBaseURL: streamingUpstream.baseURL("/priority/stream"),
		EndpointAPIKey:  "priority-stream-key",
	})

	startedAt := time.Now()
	runtimeResultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/responses", map[string]any{
		"model":  route.PublicModelID,
		"stream": true,
		"input": []map[string]any{{
			"type": "message",
			"role": "user",
			"content": []map[string]any{{
				"type": "input_text",
				"text": "stream priority isolation",
			}},
		}},
	}, nil)

	streamingUpstream.waitUntilFirstPayload(t, 5*time.Second)
	ttftElapsed := time.Since(startedAt)
	if ttftElapsed > 2*time.Second {
		t.Fatalf("expected stream upstream TTFT to stay below 2s under stats pressure, got %s", ttftElapsed)
	}

	pendingPressureResults := assertAsyncRequestsPendingOrRejected(t, pressureResults, expectedPendingPressure, len(pressureResults)-expectedPendingPressure)

	requests := streamingUpstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected stream request to reach the upstream once under stats pressure, got %d requests", len(requests))
	}
	if requests[0].Path != "/priority/stream/v1/responses" {
		t.Fatalf("expected stream request path %q, got %q", "/priority/stream/v1/responses", requests[0].Path)
	}
	if got := requestModelID(t, requests[0].Body); got != route.TargetModelID {
		t.Fatalf("expected stream request model %q, got %q", route.TargetModelID, got)
	}

	statsLock.release(t)
	assertAsyncRequestsStatus(t, pendingPressureResults, http.StatusOK)

	streamingUpstream.releaseResponse()
	runtimeResult := awaitAsyncRequest(t, runtimeResultCh, 5*time.Second)
	if runtimeResult.Err != nil {
		t.Fatalf("expected stream runtime request to succeed under mixed load, got error: %v", runtimeResult.Err)
	}
	if runtimeResult.StatusCode != http.StatusOK {
		t.Fatalf("expected stream runtime request status 200, got %d with body %s", runtimeResult.StatusCode, runtimeResult.Body)
	}
	if !strings.Contains(runtimeResult.Body, `"type":"response.completed"`) {
		t.Fatalf("expected completed stream payload after releasing upstream, got %q", runtimeResult.Body)
	}

	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, true)
}

func newPriorityStreamingUpstream(t *testing.T) *priorityStreamingUpstream {

	t.Helper()
	upstream := &priorityStreamingUpstream{ready: make(chan struct{}), release: make(chan struct{})}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read priority streaming upstream request body: %v", err)
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

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("priority streaming upstream response writer does not support flushing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		select {
		case <-r.Context().Done():
			return
		case <-time.After(25 * time.Millisecond):
		}
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"priority hello\"}\n\n")
		flusher.Flush()

		upstream.readyOnce.Do(func() {
			close(upstream.ready)
		})

		select {
		case <-r.Context().Done():
			return
		case <-upstream.release:
		}
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}}}\n\n")
		flusher.Flush()
	}))
	t.Cleanup(func() {
		upstream.releaseResponse()
		upstream.server.Close()
	})
	return upstream
}

func (u *priorityStreamingUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *priorityStreamingUpstream) waitUntilFirstPayload(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.ready:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for the first streaming payload")
	}
}

func (u *priorityStreamingUpstream) releaseResponse() {
	u.releaseOnce.Do(func() {
		close(u.release)
	})
}

func (u *priorityStreamingUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}
