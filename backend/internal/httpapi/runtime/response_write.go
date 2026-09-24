package runtime

import (
	"net/http"
	"strings"
	"time"
)

func (s *Service) writeProxyResponse(w http.ResponseWriter, r *http.Request, plan requestPlan, execution executionResult, startedAt time.Time) {
	proxyWriter := newRuntimeDeferredCommitWriter(w)

	if responseUsesSSEProxy(plan, execution.Response) {
		s.writeSSEProxyResponse(proxyWriter, r, plan, execution, startedAt)
		return
	}
	if !nonStreamResponseRequiresBufferedInspection(execution.Response.StatusCode) {
		s.writePassthroughProxyResponse(proxyWriter, r, plan, execution, startedAt)
		return
	}
	s.writeBufferedProxyResponse(proxyWriter, r, plan, execution, startedAt)
}

// responseUsesSSEProxy reports whether the response is proxied through the
// operation's SSE capture.
func responseUsesSSEProxy(plan requestPlan, response *http.Response) bool {
	contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	if !strings.Contains(contentType, "text/event-stream") {
		return false
	}
	_, ok := streamHooksForProxyResponse(plan.RuntimeOperation, plan.IsStreamingRequest)
	return ok
}
