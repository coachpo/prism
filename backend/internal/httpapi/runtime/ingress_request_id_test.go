package runtime

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

func TestRuntimeIngressRequestIDMiddlewareGeneratesAndExposesID(t *testing.T) {
	var capturedID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := requestcontext.RuntimeIngressRequestIDFromContext(r.Context())
		if !ok || id == "" {
			t.Fatalf("expected ingress ID in context, got %q ok=%v", id, ok)
		}
		capturedID = id
		w.WriteHeader(http.StatusCreated)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()
	RuntimeIngressRequestIDMiddleware(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	headerID := recorder.Header().Get(IngressRequestIDHeader)
	if headerID == "" || headerID != capturedID {
		t.Fatalf("response header %q = %q, want %q", IngressRequestIDHeader, headerID, capturedID)
	}
	if len(headerID) != 32 {
		t.Fatalf("expected 32-char hex ingress ID, got %q", headerID)
	}
}

func TestRuntimeIngressRequestIDIgnoresCallerValues(t *testing.T) {
	for _, callerID := range []string{"caller-supplied-1", "X-Prism-Ingress-Request-Id: forged"} {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := requestcontext.RuntimeIngressRequestIDFromContext(r.Context())
			if strings.HasPrefix(id, "caller") || id == "" {
				t.Fatalf("ingress ID %q must be server-generated and ignore caller values", id)
			}
			w.WriteHeader(http.StatusOK)
		})
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		request.Header.Set("X-Request-ID", "caller-trace-1")
		if callerID != "" {
			request.Header.Set(IngressRequestIDHeader, callerID)
		}
		recorder := httptest.NewRecorder()
		RuntimeIngressRequestIDMiddleware(next).ServeHTTP(recorder, request)
		headerID := recorder.Header().Get(IngressRequestIDHeader)
		if strings.HasPrefix(headerID, "caller") || headerID == "" {
			t.Fatalf("response header must not echo caller value, got %q", headerID)
		}
	}
}

func TestRuntimeIngressRequestIDNotDerivedFromChiRequestID(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := requestcontext.RuntimeIngressRequestIDFromContext(r.Context())
		chiID := r.Header.Get("X-Request-ID")
		if id == chiID {
			t.Fatalf("ingress ID %q must not equal caller X-Request-ID %q", id, chiID)
		}
		w.WriteHeader(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini:generateContent", nil)
	request.Header.Set("X-Request-ID", "chi-request-id-123")
	recorder := httptest.NewRecorder()
	RuntimeIngressRequestIDMiddleware(next).ServeHTTP(recorder, request)
	if recorder.Header().Get(IngressRequestIDHeader) == "chi-request-id-123" {
		t.Fatal("response must not echo caller X-Request-ID")
	}
}

func TestRuntimeIngressRequestIDWriterOverwritesProviderAttempts(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(IngressRequestIDHeader, "provider-forged")
		w.WriteHeader(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()
	RuntimeIngressRequestIDMiddleware(next).ServeHTTP(recorder, request)
	headerID := recorder.Header().Get(IngressRequestIDHeader)
	if headerID == "" || headerID == "provider-forged" || len(headerID) != 32 {
		t.Fatalf("provider must not overwrite ingress ID, got %q", headerID)
	}
}

func TestRuntimeIngressRequestIDWriterSupportsFlusherAndHijacker(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("chunk"))
			flusher.Flush()
		}
		if hijacker, ok := w.(http.Hijacker); ok {
			conn, _, err := hijacker.Hijack()
			if err == nil {
				_ = conn.Close()
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()
	RuntimeIngressRequestIDMiddleware(next).ServeHTTP(recorder, request)
	if recorder.Header().Get(IngressRequestIDHeader) == "" {
		t.Fatal("streamed response must still carry ingress ID")
	}
}

// staticResponseWriter enables the optional-interface checks without a full
// HTTP server.
type staticResponseWriter struct {
	header http.Header
}

func (w *staticResponseWriter) Header() http.Header         { return w.header }
func (w *staticResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *staticResponseWriter) WriteHeader(int)             {}

var _ http.Flusher = (*staticResponseWriter)(nil)

func (w *staticResponseWriter) Flush() {}

var _ http.Hijacker = (*staticResponseWriter)(nil)

func (w *staticResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}

func TestRuntimeIngressRequestIDUnwrap(t *testing.T) {
	base := &staticResponseWriter{header: http.Header{}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unwrapped := w.(interface{ Unwrap() http.ResponseWriter }); unwrapped.Unwrap() != base {
			t.Fatal("expected Unwrap to expose the underlying writer")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	RuntimeIngressRequestIDMiddleware(next).ServeHTTP(base, request)
	_ = context.Background()
}
