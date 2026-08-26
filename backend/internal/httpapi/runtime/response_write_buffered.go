package runtime

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Service) writeBufferedProxyResponse(proxyWriter *runtimeDeferredCommitWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	sourceRawBody, err := readAndCloseRuntimeResponseBody(execution.Response)
	if err != nil {
		writeError(proxyWriter.dst, http.StatusBadGateway, "", "Failed to read upstream response", nil)
		return
	}
	responseCapture, err := s.writeBufferedNonStreamResponse(proxyWriter, plan, execution, sourceRawBody)
	if err != nil {
		if !proxyWriter.Committed() {
			writeError(proxyWriter.dst, http.StatusBadGateway, "", "Failed to read upstream response", nil)
		}
		return
	}
	if s.runtimeResponseRequiresDurableHandoff(execution) {
		if err := s.enqueueRuntimeActivityBeforeResponse(plan, execution, r, startedAt, responseCapture); err != nil {
			writeRuntimeObservabilityHandoffError(proxyWriter.dst)
			return
		}
	} else {
		s.recordRuntimeActivity(plan, execution, r, startedAt, responseCapture)
	}
	proxyWriter.Commit()
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
