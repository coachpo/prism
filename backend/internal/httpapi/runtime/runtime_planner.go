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

// planningReferenceNow returns the single planning clock of this ingress, or
// the live clock when the request carries no ingress context (tests and
// non-runtime callers). It is the only allowed second read of the clock on
// the planning path: routing eligibility must never re-read the live clock
// mid-ingress, while execution-phase admission and Ban re-checks
// deliberately keep reading it.
func (s *Service) planningReferenceNow(request *http.Request) time.Time {
	if request == nil {
		return s.nowUTC()
	}
	referenceNow, ok := runtimePlanningReferenceNowFromContext(request.Context())
	if !ok || referenceNow.IsZero() {
		return s.nowUTC()
	}
	return referenceNow
}
