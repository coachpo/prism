package runtime

import "net/http"

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
