package runtimetest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type upstreamRequestSnapshot struct {
	Method  string
	URL     string
	Path    string
	Query   string
	Headers http.Header
	Body    []byte
}

type upstreamRecorder struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []upstreamRequestSnapshot
}

type scriptedUpstream struct {
	server       *httptest.Server
	mu           sync.Mutex
	requests     []upstreamRequestSnapshot
	statusCode   int
	responseBody map[string]any
}

type blockingScriptedUpstream struct {
	server       *httptest.Server
	mu           sync.Mutex
	requests     []upstreamRequestSnapshot
	statusCode   int
	responseBody map[string]any
	waitFor      int
	arrived      int
	ready        chan struct{}
	release      chan struct{}
	headersSent  chan struct{}
	headersFirst bool
	readyOnce    sync.Once
	releaseOnce  sync.Once
	headersOnce  sync.Once
}

func newUpstreamRecorder(tb testing.TB) *upstreamRecorder {
	tb.Helper()
	recorder := &upstreamRecorder{}
	recorder.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			tb.Fatalf("read upstream request body: %v", err)
		}
		_ = r.Body.Close()
		recorder.mu.Lock()
		recorder.requests = append(recorder.requests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		recorder.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/chat/completions"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-smoke"})
		case strings.HasSuffix(r.URL.Path, "/v1/messages") || strings.HasSuffix(r.URL.Path, "/v1/messages/count_tokens"):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg-smoke", "type": "message"})
		case strings.Contains(r.URL.Path, ":generateContent") || strings.Contains(r.URL.Path, ":streamGenerateContent"):
			_ = json.NewEncoder(w).Encode(map[string]any{"responseId": "gemini-smoke"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	tb.Cleanup(recorder.server.Close)
	return recorder
}

func (u *upstreamRecorder) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *upstreamRecorder) clear() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requests = nil
}

func (u *upstreamRecorder) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	requests := u.requestsSnapshot()
	if len(requests) == 0 {
		t.Fatal("expected at least one upstream request")
	}
	return requests[len(requests)-1]
}

func (u *upstreamRecorder) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}

func newScriptedUpstream(t *testing.T, statusCode int, responseBody map[string]any) *scriptedUpstream {
	t.Helper()
	upstream := &scriptedUpstream{statusCode: statusCode, responseBody: responseBody}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read scripted upstream request body: %v", err)
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstream.statusCode)
		payload := upstream.responseBody
		if payload == nil {
			payload = map[string]any{"ok": upstream.statusCode < 400}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func newBlockingScriptedUpstream(t *testing.T, waitFor int, statusCode int, responseBody map[string]any) *blockingScriptedUpstream {
	return newBlockingScriptedUpstreamMode(t, waitFor, statusCode, responseBody, false)
}

func newHeaderFirstBlockingScriptedUpstream(t *testing.T, statusCode int, responseBody map[string]any) *blockingScriptedUpstream {
	return newBlockingScriptedUpstreamMode(t, 1, statusCode, responseBody, true)
}

func newBlockingScriptedUpstreamMode(t *testing.T, waitFor int, statusCode int, responseBody map[string]any, headersFirst bool) *blockingScriptedUpstream {
	t.Helper()
	if waitFor < 1 {
		t.Fatalf("blocking upstream waitFor must be >= 1, got %d", waitFor)
	}
	upstream := &blockingScriptedUpstream{
		statusCode:   statusCode,
		responseBody: responseBody,
		waitFor:      waitFor,
		ready:        make(chan struct{}),
		release:      make(chan struct{}),
		headersSent:  make(chan struct{}),
		headersFirst: headersFirst,
	}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read blocking scripted upstream request body: %v", err)
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
		upstream.arrived++
		if upstream.arrived >= upstream.waitFor {
			upstream.readyOnce.Do(func() {
				close(upstream.ready)
			})
		}
		release := upstream.release
		upstream.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if upstream.headersFirst {
			w.WriteHeader(upstream.statusCode)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			upstream.headersOnce.Do(func() { close(upstream.headersSent) })
		}
		<-release
		if !upstream.headersFirst {
			w.WriteHeader(upstream.statusCode)
		}
		payload := upstream.responseBody
		if payload == nil {
			payload = map[string]any{"ok": upstream.statusCode < 400}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(func() {
		upstream.releaseRequests()
		upstream.server.Close()
	})
	return upstream
}

func (u *scriptedUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *scriptedUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}

func (u *scriptedUpstream) lastRequest(t *testing.T) upstreamRequestSnapshot {
	t.Helper()
	requests := u.requestsSnapshot()
	if len(requests) == 0 {
		t.Fatal("expected at least one scripted upstream request")
	}
	return requests[len(requests)-1]
}

func assertNoScriptedUpstreamRequests(t *testing.T, upstream *scriptedUpstream, name string) {
	t.Helper()
	if got := len(upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected %s to stay unattempted, got %d requests", name, got)
	}
}

func (u *blockingScriptedUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *blockingScriptedUpstream) waitUntilReady(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.ready:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %d blocking upstream requests", u.waitFor)
	}
}

func (u *blockingScriptedUpstream) waitUntilHeadersSent(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-u.headersSent:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for blocking upstream response headers")
	}
}

func (u *blockingScriptedUpstream) releaseRequests() {
	u.releaseOnce.Do(func() {
		close(u.release)
	})
}

func (u *blockingScriptedUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}
