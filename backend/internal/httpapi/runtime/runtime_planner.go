package runtime

import (
	"errors"
	"net/http"
)

func (s *Service) buildRequestPlanFromSnapshotCore(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
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
	}
	operation, err := resolveRequestOperation(input)
	if err != nil {
		return requestPlan{}, err
	}
	requestedModel, err := resolveRequestedModel(input, operation)
	if err != nil {
		return requestPlan{}, err
	}
	contextEstimation, contextEstimationErr := estimatePreflightRequestContext(operation.Match.Operation, input.RawBody, requestedModel)
	input.AllowMissingContextEstimation = allowContextEstimationUnavailablePassThrough(operation.Match.Operation, contextEstimationErr)
	target, err := s.resolveRequestPlanTarget(input, operation, requestedModel, contextEstimation)
	if err != nil {
		var runtimeErr *domainError
		if contextEstimationErr != nil && !input.AllowMissingContextEstimation && (!errors.As(err, &runtimeErr) || runtimeErr == nil || runtimeErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode) {
			return requestPlan{}, contextEstimationErr
		}
		return requestPlan{}, attachRuntimePlanningFailureTelemetry(err, input, operation, requestedModel)
	}
	if contextEstimationErr != nil && !input.AllowMissingContextEstimation {
		return requestPlan{}, contextEstimationErr
	}
	return assembleRequestPlan(input, operation, target, contextEstimation)
}
