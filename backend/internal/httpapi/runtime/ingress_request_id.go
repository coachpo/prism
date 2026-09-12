package runtime

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/coachpo/prism/backend/internal/httpapi/requestcontext"
)

// IngressRequestIDHeader is the response header carrying the server-generated
// canonical accepted-operation ID, or a branch correlation token when the
// request has not reached that boundary. It is never accepted from callers.
const IngressRequestIDHeader = "X-Prism-Ingress-Request-Id"

// RuntimeIngressRequestIDMiddleware generates an opaque, server-entropy ingress
// token for every runtime-branch request before auth runs. Accepted proxy
// operations bind the writer to their canonical UUID before any response write;
// early rejections and the local models list retain the branch token. Neither
// caller nor provider headers can overwrite the server-owned response value.
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

// bindRuntimeIngressResponseID projects the accepted-operation owner's UUID to
// the outer response writer before body handling or provider execution. The
// branch token stays separate so early rejection creates no accepted identity.
func bindRuntimeIngressResponseID(w http.ResponseWriter, id string) {
	for {
		switch writer := w.(type) {
		case *runtimeIngressResponseWriter:
			if !writer.wroteHeader {
				writer.id = id
			}
			return
		case interface{ Unwrap() http.ResponseWriter }:
			w = writer.Unwrap()
		default:
			return
		}
	}
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
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack preserves the optional HTTP/1 connection interface while retaining
// the server-owned correlation header. Runtime operations do not normally
// hijack connections, but middleware must not silently remove an interface
// exposed by the underlying writer.
func (w *runtimeIngressResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.ensureHeader()
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *runtimeIngressResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
