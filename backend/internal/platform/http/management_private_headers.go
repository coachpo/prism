package platformhttp

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

// managementPrivateNoStoreMiddleware sits outside every management rejection
// layer so a private route keeps its cache policy even when its handler is not
// reached. The route registry keeps the match limited to exact method/path
// pairs instead of applying private response headers to the whole /api branch.
func managementPrivateNoStoreMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spec, ok := matchManagementRouteSpec(r.Method, r.URL.Path)
		if !ok || !spec.privateNoStore {
			next.ServeHTTP(w, r)
			return
		}

		writer := &privateNoStoreResponseWriter{ResponseWriter: w}
		responseutil.SetPrivateNoStoreHeaders(writer)
		next.ServeHTTP(writer, r)
	})
}

// privateNoStoreResponseWriter reapplies and normalizes the headers immediately
// before commit. This matters when an inner buffering middleware copies its
// response headers onto headers already installed by this outer middleware.
type privateNoStoreResponseWriter struct {
	http.ResponseWriter
}

func (w *privateNoStoreResponseWriter) WriteHeader(statusCode int) {
	responseutil.SetPrivateNoStoreHeaders(w.ResponseWriter)
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *privateNoStoreResponseWriter) Write(payload []byte) (int, error) {
	responseutil.SetPrivateNoStoreHeaders(w.ResponseWriter)
	return w.ResponseWriter.Write(payload)
}

func (w *privateNoStoreResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
