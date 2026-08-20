package runtime

import (
	"net/http"
)

// buildTestRequestPlanFromSnapshot makes the otherwise mandatory ingress
// planning clock explicit for direct unit helpers. Production callers must
// arrive through ingress and never use this method.
func (s *Service) buildTestRequestPlanFromSnapshot(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	request = request.WithContext(withRuntimeIngressContext(request.Context(), newRuntimeIngressContext(s.nowUTC())))
	return s.buildRequestPlanFromSnapshot(request, rawBody, runtimeConfig, operationMatch, activeProfileID, snapshot)
}
