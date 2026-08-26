package runtime

import (
	"bytes"
	"net/http"
)

// Downstream bytes become committed only when Commit or Flush runs. This keeps
// buffered non-stream success responses reversible until the durable telemetry
// handoff row is inserted.
type runtimeDeferredCommitWriter struct {
	dst        http.ResponseWriter
	header     http.Header
	statusCode int
	body       bytes.Buffer
	committed  bool
}

func newRuntimeDeferredCommitWriter(dst http.ResponseWriter) *runtimeDeferredCommitWriter {
	return &runtimeDeferredCommitWriter{
		dst:        dst,
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (writer *runtimeDeferredCommitWriter) Header() http.Header {
	if writer.committed {
		return writer.dst.Header()
	}
	return writer.header
}

func (writer *runtimeDeferredCommitWriter) WriteHeader(statusCode int) {
	if writer.committed {
		return
	}
	writer.statusCode = statusCode
}

func (writer *runtimeDeferredCommitWriter) Write(payload []byte) (int, error) {
	if writer.committed {
		written, err := writer.dst.Write(payload)
		if flusher, ok := writer.dst.(http.Flusher); ok {
			flusher.Flush()
		}
		return written, err
	}
	return writer.body.Write(payload)
}

func (writer *runtimeDeferredCommitWriter) Flush() {
	writer.Commit()
	if flusher, ok := writer.dst.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (writer *runtimeDeferredCommitWriter) Commit() {
	if writer.committed {
		return
	}
	copyResponseHeaders(writer.dst.Header(), writer.header)
	writer.dst.WriteHeader(writer.statusCode)
	writer.committed = true
	if writer.body.Len() > 0 {
		_, _ = writer.dst.Write(writer.body.Bytes())
		writer.body.Reset()
	}
}

func (writer *runtimeDeferredCommitWriter) Committed() bool {
	return writer.committed
}
