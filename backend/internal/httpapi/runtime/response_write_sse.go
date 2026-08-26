package runtime

import (
	"log/slog"
	"net/http"
	"time"
)

func (s *Service) writeSSEProxyResponse(proxyWriter *runtimeDeferredCommitWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	captureAuditBody := execution.AuditEnabledAtRequest && execution.AuditCaptureBodiesAtRequest
	copyUpstreamResponseHeaders(proxyWriter.Header(), execution.Response.Header)
	proxyWriter.WriteHeader(execution.Response.StatusCode)
	acceptedRowID := int64(0)
	if s.runtimeResponseRequiresDurableHandoff(execution) {
		rowID, err := s.enqueueStreamingRuntimeActivityAcceptedBeforeResponse(plan, execution, r, startedAt)
		if err != nil {
			slog.Error("runtime streaming handoff enqueue failed", "error", err)
			writeRuntimeObservabilityHandoffError(proxyWriter.dst)
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
}
