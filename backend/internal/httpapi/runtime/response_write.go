package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s *Service) writeProxyResponse(w http.ResponseWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	proxyWriter := newRuntimeDeferredCommitWriter(w)

	var responseCapture runtimeResponseCapture
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	captureAuditBody := execution.AuditEnabledAtRequest && execution.AuditCaptureBodiesAtRequest
	if strings.Contains(contentType, "text/event-stream") {
		if _, ok := streamHooksForProxyResponse(plan.RuntimeOperation, plan.IsStreamingRequest); ok {
			copyUpstreamResponseHeaders(proxyWriter.Header(), execution.Response.Header)
			proxyWriter.WriteHeader(execution.Response.StatusCode)
			acceptedRowID := int64(0)
			if s.runtimeResponseRequiresDurableHandoff(execution) {
				rowID, err := s.enqueueStreamingRuntimeActivityAcceptedBeforeResponse(plan, execution, r, startedAt)
				if err != nil {
					slog.Error("runtime streaming handoff enqueue failed", "error", err)
					writeRuntimeObservabilityHandoffError(w)
					return
				}
				acceptedRowID = rowID
				proxyWriter.Flush()
			}
			responseCapture, streamErr := proxyEventStreamAndCaptureCompletedResponse(plan.RuntimeOperation, r.Context(), proxyWriter, execution.Response.Body, s.nowUTC, captureAuditBody)
			if streamErr != nil {
				slog.Debug("runtime stream proxy ended with classified error", "error", streamErr, "stream_outcome", responseCapture.StreamOutcome)
			}
			if acceptedRowID > 0 {
				if err := s.finalizeStreamingRuntimeActivityBeforeCompletion(acceptedRowID, plan, execution, r, startedAt, responseCapture); err != nil {
					writeRuntimeObservabilityHandoffStreamError(proxyWriter)
					return
				}
			} else {
				s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
			}
			// Emitted after the handoff so a finalize failure reports itself once
			// rather than stacking two error frames on the same stream.
			if reason, aborted := runtimeStreamAbortReasonFor(responseCapture.StreamOutcome); aborted {
				writeRuntimeStreamAbortFrame(proxyWriter, plan.RuntimeOperation, reason)
			}
			proxyWriter.Commit()
			return
		}
	}
	if !nonStreamResponseRequiresBufferedInspection(execution.Response.StatusCode) {
		copyUpstreamResponseHeaders(proxyWriter.Header(), execution.Response.Header)
		proxyWriter.WriteHeader(execution.Response.StatusCode)
		acceptedRowID := int64(0)
		if s.runtimeResponseRequiresDurableHandoff(execution) {
			rowID, err := s.enqueueStreamingRuntimeActivityAcceptedBeforeResponse(plan, execution, r, startedAt)
			if err != nil {
				writeRuntimeObservabilityHandoffError(w)
				return
			}
			acceptedRowID = rowID
			proxyWriter.Flush()
		}
		passthroughCapture, passthroughErr := proxyNonEventResponseAndCaptureByOperation(plan.RuntimeOperation, proxyWriter, execution.Response.Body, contentType, s.nowUTC, captureAuditBody)
		responseCapture = passthroughCapture
		if passthroughErr != nil {
			if !proxyWriter.Committed() {
				writeError(w, http.StatusBadGateway, "", "Failed to read upstream response", nil)
				return
			}
		}
		if acceptedRowID > 0 {
			if err := s.finalizeStreamingRuntimeActivityBeforeCompletion(acceptedRowID, plan, execution, r, startedAt, responseCapture); err != nil {
				return
			}
		} else {
			s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
		}
		proxyWriter.Commit()
		return
	}
	sourceRawBody, err := readAndCloseRuntimeResponseBody(execution.Response)
	if err != nil {
		writeError(w, http.StatusBadGateway, "", "Failed to read upstream response", nil)
		return
	}
	responseCapture, err = s.writeBufferedNonStreamResponse(proxyWriter, plan, execution, sourceRawBody)
	if err != nil {
		if !proxyWriter.Committed() {
			writeError(w, http.StatusBadGateway, "", "Failed to read upstream response", nil)
		}
		return
	}
	if s.runtimeResponseRequiresDurableHandoff(execution) {
		if err := s.enqueueRuntimeActivityBeforeResponse(plan, execution, r, startedAt, responseCapture); err != nil {
			slog.Error("runtime observability handoff enqueue failed", "error", err)
			writeRuntimeObservabilityHandoffError(w)
			return
		}
	} else {
		s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
	}
	proxyWriter.Commit()
}

func nonStreamResponseRequiresBufferedInspection(statusCode int) bool {
	return cliProxyAPIOverflowStatusAllowed(statusCode)
}

func (s *Service) runtimeResponseRequiresDurableHandoff(execution executionResult) bool {
	return s != nil && s.requireDurableSuccessHandoff && execution.Response != nil && execution.Response.StatusCode >= http.StatusOK && execution.Response.StatusCode <= 299
}

func writeRuntimeObservabilityHandoffError(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "runtime_observability_handoff_failed", "Runtime observability handoff failed", nil)
}

func readAndCloseRuntimeResponseBody(response *http.Response) ([]byte, error) {
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("runtime response body unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (s *Service) writeBufferedNonStreamResponse(proxyWriter *runtimeDeferredCommitWriter, plan requestPlan, execution executionResult, rawBody []byte) (runtimeResponseCapture, error) {
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	captureAuditBody := execution.AuditEnabledAtRequest && execution.AuditCaptureBodiesAtRequest
	copyUpstreamResponseHeaders(proxyWriter.Header(), execution.Response.Header)
	proxyWriter.WriteHeader(execution.Response.StatusCode)
	return proxyNonEventResponseAndCaptureByOperation(plan.RuntimeOperation, proxyWriter, bytes.NewReader(rawBody), contentType, s.nowUTC, captureAuditBody)
}

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

func writeDomainError(w http.ResponseWriter, err error) {
	var runtimeErr *domainError
	if errors.As(err, &runtimeErr) {
		writeError(w, runtimeErr.StatusCode, runtimeErr.ErrorCode, runtimeErr.Detail, runtimeErr.Fields)
		return
	}
	writeError(w, http.StatusInternalServerError, "", "Internal server error", nil)
}

func writeError(w http.ResponseWriter, statusCode int, errorCode string, detail string, fields map[string]any) {
	payload := map[string]any{"detail": detail}
	if strings.TrimSpace(errorCode) != "" {
		payload["error"] = strings.TrimSpace(errorCode)
	}
	for key, value := range fields {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		payload[key] = value
	}
	writeJSON(w, statusCode, payload)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
