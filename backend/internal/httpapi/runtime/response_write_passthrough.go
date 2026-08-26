package runtime

import (
	"net/http"
	"strings"
	"time"
)

func (s *Service) writePassthroughProxyResponse(proxyWriter *runtimeDeferredCommitWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	captureAuditBody := execution.AuditEnabledAtRequest && execution.AuditCaptureBodiesAtRequest
	copyUpstreamResponseHeaders(proxyWriter.Header(), execution.Response.Header)
	proxyWriter.WriteHeader(execution.Response.StatusCode)
	acceptedRowID := int64(0)
	if s.runtimeResponseRequiresDurableHandoff(execution) {
		rowID, err := s.enqueueStreamingRuntimeActivityAcceptedBeforeResponse(plan, execution, r, startedAt)
		if err != nil {
			writeRuntimeObservabilityHandoffError(proxyWriter.dst)
			return
		}
		acceptedRowID = rowID
		proxyWriter.Flush()
	}
	passthroughCapture, passthroughErr := proxyNonEventResponseAndCaptureByOperation(plan.RuntimeOperation, proxyWriter, execution.Response.Body, contentType, s.nowUTC, captureAuditBody)
	if passthroughErr != nil {
		if !proxyWriter.Committed() {
			writeError(proxyWriter.dst, http.StatusBadGateway, "", "Failed to read upstream response", nil)
			return
		}
	}
	if acceptedRowID > 0 {
		if err := s.finalizeStreamingRuntimeActivityBeforeCompletion(acceptedRowID, plan, execution, r, startedAt, passthroughCapture); err != nil {
			return
		}
	} else {
		s.recordRuntimeActivity(plan, execution, r, startedAt, passthroughCapture)
	}
	proxyWriter.Commit()
}
