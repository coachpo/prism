package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

// IngressRequestIDHeader is the response header carrying the server-generated
// runtime ingress correlation ID. It is never accepted from callers and never
// contains secret material.
const IngressRequestIDHeader = "X-Prism-Ingress-Request-Id"

// RuntimeIngressRequestIDMiddleware generates an opaque, server-entropy ingress
// ID for every runtime-branch request before auth runs, stores it in its own
// request-context field, and guarantees the response carries it even when the
// handler or an upstream provider attempts to overwrite it.
//
// The ID deliberately does not derive from chi middleware.RequestID, because
// that transport trace may honor a caller-supplied X-Request-ID. Caller
// X-Request-ID and X-Prism-* values are ignored for ingress correlation.
func RuntimeIngressRequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRuntimeIngressRequestID()
		ctx := requestcontext.WithRuntimeIngressRequestID(r.Context(), id)
		next.ServeHTTP(&runtimeIngressResponseWriter{ResponseWriter: w, id: id}, r.WithContext(ctx))
	})
}

func newRuntimeIngressRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		// crypto/rand failure is catastrophic for correlation; fall back to a
		// time-based opaque value so the response still carries a stable ID.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

// runtimeIngressResponseWriter forces the ingress ID header onto every
// response before any status/body write, so neither the runtime handler nor a
// provider adapter can overwrite or omit it. It is set exactly once per
// response.
type runtimeIngressResponseWriter struct {
	http.ResponseWriter
	id          string
	wroteHeader bool
}

func (w *runtimeIngressResponseWriter) ensureHeader() {
	if w.wroteHeader {
		return
	}
	w.Header().Set(IngressRequestIDHeader, w.id)
	w.wroteHeader = true
}

func (w *runtimeIngressResponseWriter) WriteHeader(statusCode int) {
	w.ensureHeader()
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *runtimeIngressResponseWriter) Write(body []byte) (int, error) {
	w.ensureHeader()
	return w.ResponseWriter.Write(body)
}

func (w *runtimeIngressResponseWriter) Flush() {
	w.ensureHeader()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *runtimeIngressResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

