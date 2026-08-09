package runtimetest

import (
	"net"
	"net/http"
	"testing"
	"time"
)

// TestRuntimeLatencyIsResponseHeadersLatencyNotBodyConsumption pins the
// last_success_response_headers_latency_ms semantics: the value measures the
// last successful upstream attempt from request start to http.Client.Do
// response-headers receipt. Body/SSE consumption time must not change it, so
// it is neither total attempt time nor TTFT nor a percentile.
func TestRuntimeLatencyIsResponseHeadersLatencyNotBodyConsumption(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	headersDelay := 150 * time.Millisecond
	bodyDelay := 900 * time.Millisecond
	upstream := newSlowBodyUpstream(t, headersDelay, bodyDelay)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "latency-headers-semantics",
		suffix:    suffix,
		endpoints: []selectorEndpointSeed{{label: "slow-body", baseURL: upstream.baseURL("/latency/headers"), position: 0}},
	})

	startedAt := time.Now()
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "headers latency"), nil)
	totalElapsed := time.Since(startedAt)
	assertStatus(t, response, http.StatusOK)

	successState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if !successState.LastSuccessResponseHeadersLatencyMS.Valid {
		t.Fatalf("expected success to record last_success_response_headers_latency_ms, got %+v", successState)
	}
	recorded := time.Duration(successState.LastSuccessResponseHeadersLatencyMS.Int32) * time.Millisecond
	if recorded > bodyDelay/2 {
		t.Fatalf("expected recorded headers latency %v to exclude the %v body consumption delay (total elapsed %v)", recorded, bodyDelay, totalElapsed)
	}
	if recorded < headersDelay-50*time.Millisecond {
		t.Fatalf("expected recorded headers latency %v to include the %v headers delay", recorded, headersDelay)
	}
}

type slowBodyUpstream struct {
	server       *http.Server
	headersDelay time.Duration
	bodyDelay    time.Duration
}

func newSlowBodyUpstream(t *testing.T, headersDelay time.Duration, bodyDelay time.Duration) *slowBodyUpstream {
	t.Helper()
	mux := http.NewServeMux()
	upstream := &slowBodyUpstream{headersDelay: headersDelay, bodyDelay: bodyDelay}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = r.Body.Close()
		time.Sleep(headersDelay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(bodyDelay)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-latency-headers","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"slow body"},"finish_reason":"stop"}]}`))
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen slow-body upstream: %v", err)
	}
	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	upstream.server = server
	upstream.server.Addr = listener.Addr().String()
	return upstream
}

func (upstream *slowBodyUpstream) baseURL(path string) string {
	return "http://" + upstream.server.Addr + path
}
