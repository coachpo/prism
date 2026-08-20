package runtime

import (
	"net/http"
	"time"
)

func (s *Service) buildRequestPlanFromSnapshotCore(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	return s.buildRequestPlanFromSnapshotCoreWithProbe(request, rawBody, runtimeConfig, operationMatch, activeProfileID, snapshot, false)
}

func (s *Service) buildRequestPlanFromSnapshotCoreWithProbe(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot, probePlanning bool) (requestPlan, error) {
	routingPlan, err := snapshot.compiledRoutingPlan()
	if err != nil {
		return requestPlan{}, err
	}
	input := requestPlanningInput{
		Request:         request,
		RawBody:         rawBody,
		RuntimeConfig:   runtimeConfig,
		OperationMatch:  operationMatch,
		ActiveProfileID: activeProfileID,
		Snapshot:        snapshot,
		RoutingPlan:     routingPlan,
		ReferenceNow:    s.planningReferenceNow(request),
		ProbePlanning:   probePlanning,
	}
	operation, err := resolveRequestOperation(input)
	if err != nil {
		return requestPlan{}, err
	}
	requestedModel, err := resolveRequestedModel(input, operation)
	if err != nil {
		return requestPlan{}, err
	}
	target, err := s.resolveRequestPlanTarget(input, operation, requestedModel)
	if err != nil {
		return requestPlan{}, attachRuntimePlanningFailureTelemetry(err, input, operation, requestedModel)
	}
	return assembleRequestPlan(input, operation, target)
}

// planningReferenceNow returns only the clock captured by the runtime
// ingress context. A missing or zero value remains zero so schedule/card
// selection fails closed; it must never silently substitute a later live clock.
func (s *Service) planningReferenceNow(request *http.Request) time.Time {
	if request == nil {
		return time.Time{}
	}
	referenceNow, _ := runtimePlanningReferenceNowFromContext(request.Context())
	return referenceNow
}
