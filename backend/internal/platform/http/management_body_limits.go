package platformhttp

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

func managementBodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limitBytes, ok := managementRequestBodyLimit(r.Method, r.URL.Path)
		if !ok || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}

		LimitRequestBody(w, r, limitBytes)
		exceeded := false
		r.Body = &requestBodyLimitObserver{source: r.Body, exceeded: &exceeded}

		buffered := newBodyLimitBufferedResponseWriter(w)
		next.ServeHTTP(buffered, r)
		if exceeded {
			WriteRequestBodyTooLarge(w, limitBytes)
			return
		}
		buffered.Flush()
	})
}

func managementRequestBodyLimit(method string, rawPath string) (int64, bool) {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	if normalizedMethod != http.MethodPost && normalizedMethod != http.MethodPut && normalizedMethod != http.MethodPatch {
		return 0, false
	}
	segments := managementRouteSegments(normalizeManagementRoutePath(rawPath))
	if len(segments) == 0 {
		return 0, false
	}

	if segments[0] == "auth" {
		return AuthRequestBodyLimitBytes, true
	}
	if matchesSegments(segments, "config", "bootstrap") || matchesSegments(segments, "config", "bootstrap", "validate") {
		return BootstrapRequestBodyLimitBytes, true
	}
	if matchesSegments(segments, "config", "profile", "import") || matchesSegments(segments, "config", "profile", "import", "preview") {
		return ConfigBundleRequestBodyLimitBytes, true
	}
	if matchesSegments(segments, "config", "profile", "export", "with-secrets") {
		return 0, false
	}
	return ManagementJSONRequestBodyLimitBytes, true
}

type requestBodyLimitObserver struct {
	source   io.ReadCloser
	exceeded *bool
}

func (r *requestBodyLimitObserver) Read(payload []byte) (int, error) {
	n, err := r.source.Read(payload)
	if IsRequestBodyTooLarge(err) && r.exceeded != nil {
		*r.exceeded = true
	}
	return n, err
}

func (r *requestBodyLimitObserver) Close() error {
	return r.source.Close()
}

type bodyLimitBufferedResponseWriter struct {
	target     http.ResponseWriter
	header     http.Header
	body       bytes.Buffer
	statusCode int
}

func newBodyLimitBufferedResponseWriter(target http.ResponseWriter) *bodyLimitBufferedResponseWriter {
	return &bodyLimitBufferedResponseWriter{target: target, header: make(http.Header)}
}

func (w *bodyLimitBufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bodyLimitBufferedResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
}

func (w *bodyLimitBufferedResponseWriter) Write(payload []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.body.Write(payload)
}

func (w *bodyLimitBufferedResponseWriter) Flush() {
	for key, values := range w.header {
		for _, value := range values {
			w.target.Header().Add(key, value)
		}
	}
	statusCode := w.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.target.WriteHeader(statusCode)
	_, _ = w.target.Write(w.body.Bytes())
}
