package runtime

import (
	"net/http"
	"strings"
	"time"
)

func (s *Service) writeProxyResponse(w http.ResponseWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	proxyWriter := newRuntimeDeferredCommitWriter(w)

	contentType := strings.ToLower(strings.TrimSpace(execution.Response.Header.Get("Content-Type")))
	if strings.Contains(contentType, "text/event-stream") {
		if _, ok := streamHooksForProxyResponse(plan.RuntimeOperation, plan.IsStreamingRequest); ok {
			s.writeSSEProxyResponse(proxyWriter, r, plan, execution, startedAt)
			return
		}
	}
	if !nonStreamResponseRequiresBufferedInspection(execution.Response.StatusCode) {
		s.writePassthroughProxyResponse(proxyWriter, r, plan, execution, startedAt)
		return
	}
	s.writeBufferedProxyResponse(proxyWriter, r, plan, execution, startedAt)
}
