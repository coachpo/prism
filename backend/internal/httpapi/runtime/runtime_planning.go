package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/platform/bodylimits"
)

func (s *Service) buildRequestPlan(ctx context.Context, request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch) (requestPlan, error) {
	operationMatch, err := validateResolvedRuntimeOperation(operationMatch, request.Method, request.URL.Path)
	if err != nil {
		return requestPlan{}, err
	}
	if s.cache == nil {
		return requestPlan{}, runtimeSnapshotDomainError(ErrPublishedRuntimeSnapshotUnavailable)
	}
	defaultProfile, snapshot, err := s.cache.LoadFreshDefaultRuntimePlan(ctx)
	if err != nil {
		return requestPlan{}, runtimeSnapshotDomainError(err)
	}
	plan, err := s.buildRequestPlanFromSnapshot(request.WithContext(ctx), rawBody, runtimeConfig, operationMatch, defaultProfile.ID, snapshot)
	if err != nil {
		return requestPlan{}, err
	}
	return plan, nil
}

func (s *Service) buildRequestPlanFromSnapshot(request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	plan, err := s.buildRequestPlanFromSnapshotCore(request, rawBody, runtimeConfig, operationMatch, activeProfileID, snapshot)
	if err != nil {
		return requestPlan{}, err
	}
	return plan, nil
}

func (s *Service) buildProbeRequestPlanFromSnapshot(request *http.Request, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot) (requestPlan, error) {
	return s.buildRequestPlanFromSnapshotCoreWithProbe(request, nil, runtimeConfig, operationMatch, activeProfileID, snapshot, true)
}

func resolveRequestOperation(input requestPlanningInput) (resolvedRequestOperation, error) {
	operationMatch, err := validateResolvedRuntimeOperation(input.OperationMatch, input.Request.Method, input.Request.URL.Path)
	if err != nil {
		return resolvedRequestOperation{}, err
	}
	requestContentType := input.Request.Header.Get("Content-Type")
	requestedModelID, err := resolveModelIDForOperation(input.RawBody, requestContentType, operationMatch)
	if err != nil {
		return resolvedRequestOperation{}, err
	}
	return resolvedRequestOperation{Match: operationMatch, ContentType: requestContentType, RequestedModelID: requestedModelID}, nil
}

func resolveRequestedModel(input requestPlanningInput, operation resolvedRequestOperation) (runtimeModelRecord, error) {
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return runtimeModelRecord{}, err
	}
	requestedModel, found := routingPlan.requestedModelByID(operation.RequestedModelID)
	if !found {
		return runtimeModelRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", operation.RequestedModelID)}
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, requestedModel); err != nil {
		return runtimeModelRecord{}, err
	}
	return requestedModel, nil
}

func resolveRequestedModelByID(input requestPlanningInput, operation resolvedRequestOperation, requestedModelID string) (runtimeModelRecord, error) {
	trimmedRequestedModelID := strings.TrimSpace(requestedModelID)
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return runtimeModelRecord{}, err
	}
	requestedModel, found := routingPlan.requestedModelByID(trimmedRequestedModelID)
	if !found {
		return runtimeModelRecord{}, &domainError{StatusCode: http.StatusNotFound, Detail: fmt.Sprintf("Model '%s' not configured or disabled", trimmedRequestedModelID)}
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, requestedModel); err != nil {
		return runtimeModelRecord{}, err
	}
	return requestedModel, nil
}

func attachRuntimePlanningFailureTelemetry(err error, input requestPlanningInput, operation resolvedRequestOperation, requestedModel runtimeModelRecord) error {
	var runtimeErr *domainError
	if !errors.As(err, &runtimeErr) || runtimeErr == nil {
		return err
	}
	_, unsupportedWire := isRequestTranslationUnsupportedError(runtimeErr)
	if !unsupportedWire && runtimeErr.StatusCode != http.StatusServiceUnavailable {
		return err
	}
	generationParams := extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody)
	selectedTerminalTargetID := cloneRuntimeIntPointer(runtimeErr.SelectedTerminalTargetID)
	var upstreamOperationName *string
	var upstreamRequestPath *string
	var operationTranslationMode *string
	if unsupportedWire {
		upstreamOperationName = stringPtr(runtimeUpstreamOperationName(operation.Match.Operation, TranslationModeNone))
		upstreamRequestPath = runtimeUpstreamRequestPath(operation.Match.Operation, TranslationModeNone, "")
		operationTranslationMode = runtimeTranslationModePointer(TranslationModeNone)
	}
	var resolvedTargetModelID *string
	if runtimeErr.ResolvedTargetModelID != nil && strings.TrimSpace(*runtimeErr.ResolvedTargetModelID) != "" {
		resolvedTargetModelID = cloneRuntimeStringPointer(runtimeErr.ResolvedTargetModelID)
	}
	runtimeErr.PlanningFailure = &runtimePlanningFailureTelemetry{
		ProfileID:                   input.ActiveProfileID,
		RequestedModelID:            requestedModel.ModelID,
		RequestedVendorID:           requestedModel.VendorID,
		RequestedVendorKey:          requestedModel.VendorKey,
		RequestedVendorName:         requestedModel.VendorName,
		APIFamily:                   requestedModel.APIFamily,
		RuntimeOperation:            operation.Match.Operation,
		UpstreamOperationName:       upstreamOperationName,
		RequestPath:                 input.Request.URL.Path,
		UpstreamRequestPath:         upstreamRequestPath,
		OperationTranslationMode:    operationTranslationMode,
		IsStreamingRequest:          requestWantsStreamForOperation(operation.Match.Operation, input.RawBody, input.Request.URL.Path),
		AuditEnabledAtRequest:       requestedModel.AuditEnabled,
		AuditCaptureBodiesAtRequest: requestedModel.AuditEnabled && requestedModel.AuditCaptureBodies,
		ReportCurrencySnapshot:      input.Snapshot.ReportCurrency,
		RequestGenerationParams:     generationParams,
		SelectedTerminalTargetID:    selectedTerminalTargetID,
	}
	runtimeErr.ResolvedTargetModelID = resolvedTargetModelID
	return err
}

func (s *Service) resolveRequestPlanTarget(input requestPlanningInput, operation resolvedRequestOperation, requestedModel runtimeModelRecord) (resolvedExecutionTarget, error) {
	routingPlan, err := input.compiledRoutingPlan()
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	resolved, err := s.resolveExecutionTargetFromRoutingPlanWithOptions(input.ActiveProfileID, routingPlan, requestedModel, operation.Match.Operation, input.ReferenceNow)
	if err != nil {
		return resolvedExecutionTarget{}, err
	}
	if err := validateOperationAPIFamily(operation.Match.Operation, resolved.TargetModel); err != nil {
		return resolvedExecutionTarget{}, err
	}
	if len(resolved.TerminalAttempts) == 0 {
		return resolvedExecutionTarget{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No eligible targets available for model '%s'.", operation.RequestedModelID)}
	}

	selectedTerminalTargetID := intPtr(resolved.TerminalAttempts[0].Connection.ID)
	return resolvedExecutionTarget{
		RequestedModel:           requestedModel,
		TargetModel:              resolved.TargetModel,
		SelectedTerminalTargetID: selectedTerminalTargetID,
		Connections:              resolved.Connections,
		TerminalAttempts:         resolved.TerminalAttempts,
		RuntimeStates:            resolved.RuntimeStates,
		Strategy:                 resolved.Strategy,
	}, nil
}

func buildPlannedUpstreamRequest(input requestPlanningInput, operation resolvedRequestOperation, attempt runtimeTerminalAttempt) (plannedUpstreamRequest, error) {
	if upstreamRequest, ok, err := buildOpenAITextPlannedUpstreamRequest(input, operation, attempt); ok || err != nil {
		return upstreamRequest, err
	}
	if upstreamRequest, ok, err := buildAnthropicPlannedUpstreamRequest(input, operation, attempt); ok || err != nil {
		return upstreamRequest, err
	}
	if upstreamRequest, ok, err := buildGeminiPlannedUpstreamRequest(input, operation, attempt); ok || err != nil {
		return upstreamRequest, err
	}
	effectiveRequestPath := input.Request.URL.Path
	upstreamBody := input.RawBody
	switch operation.Match.Operation.ModelBindingSource {
	case RuntimeOperationModelBindingPath:
		pathModelID := strings.TrimSpace(operation.Match.PathParams["model"])
		if pathModelID != "" && pathModelID != attempt.TargetModel.ModelID {
			effectiveRequestPath = rewriteModelInPath(input.Request.URL.Path, pathModelID, attempt.TargetModel.ModelID)
		}
	case RuntimeOperationModelBindingBody:
		if bodyModelID := extractModelFromBody(input.RawBody); bodyModelID != "" && bodyModelID != attempt.TargetModel.ModelID {
			upstreamBody = rewriteModelInBody(input.RawBody, attempt.TargetModel.ModelID)
		}
	default:
		return plannedUpstreamRequest{}, unsupportedOperationModelBindingError(operation.Match.Operation)
	}

	return plannedUpstreamRequest{
		EffectiveRequestPath:    effectiveRequestPath,
		RawRequestBody:          input.RawBody,
		UpstreamBody:            upstreamBody,
		IsStreamingRequest:      requestWantsStreamForOperation(operation.Match.Operation, input.RawBody, effectiveRequestPath),
		ClientHeaders:           flattenHeaders(input.Request.Header),
		RequestGenerationParams: extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody),
	}, nil
}

func assembleRequestPlan(input requestPlanningInput, operation resolvedRequestOperation, target resolvedExecutionTarget) (requestPlan, error) {
	terminalAttempts, upstreamRequest, err := buildPlannedTerminalAttempts(input, operation, target.TerminalAttempts)
	if err != nil {
		return requestPlan{}, err
	}
	firstAttempt := terminalAttempts[0]
	connections := connectionsFromTerminalAttempts(terminalAttempts)
	return requestPlan{
		ReferenceNow:                input.ReferenceNow.UTC(),
		RequestedModelID:            operation.RequestedModelID,
		ResolvedTargetModelID:       stringPointerIfNotEmpty(firstAttempt.TargetModel.ModelID),
		ResolvedPricingModelID:      strings.TrimSpace(firstAttempt.TargetModel.ModelID),
		RequestedVendorID:           target.RequestedModel.VendorID,
		RequestedVendorKey:          target.RequestedModel.VendorKey,
		RequestedVendorName:         target.RequestedModel.VendorName,
		ProfileID:                   input.ActiveProfileID,
		APIFamily:                   firstAttempt.TargetModel.APIFamily,
		RuntimeOperation:            operation.Match.Operation,
		RuntimeOperationPathParams:  cloneStringMap(operation.Match.PathParams),
		AuditEnabledAtRequest:       firstAttempt.AuditEnabledAtRequest,
		AuditCaptureBodiesAtRequest: firstAttempt.AuditCaptureBodiesRequest,
		ReportCurrencySnapshot:      input.Snapshot.ReportCurrency,
		EffectiveRequestPath:        upstreamRequest.EffectiveRequestPath,
		RawRequestBody:              upstreamRequest.RawRequestBody,
		UpstreamBody:                upstreamRequest.UpstreamBody,
		IsStreamingRequest:          upstreamRequest.IsStreamingRequest,
		SelectedTerminalTargetID:    cloneRuntimeIntPointer(target.SelectedTerminalTargetID),
		TerminalAttempts:            terminalAttempts,
		Connections:                 connections,
		RuntimeStates:               target.RuntimeStates,
		BlocklistRules:              input.Snapshot.BlocklistRules,
		ClientHeaders:               upstreamRequest.ClientHeaders,
		FailoverStatusCodes:         firstAttempt.Strategy.FailoverStatusCodes(),
		Strategy:                    firstAttempt.Strategy,
		RequestGenerationParams:     upstreamRequest.RequestGenerationParams,
		HTTPClient:                  input.RuntimeConfig.HTTPClient,
	}, nil
}

func buildPlannedTerminalAttempts(input requestPlanningInput, operation resolvedRequestOperation, attempts []runtimeTerminalAttempt) ([]runtimeTerminalAttempt, plannedUpstreamRequest, error) {
	if len(attempts) == 0 {
		return nil, plannedUpstreamRequest{}, &domainError{StatusCode: http.StatusServiceUnavailable, Detail: fmt.Sprintf("No eligible targets available for model '%s'.", operation.RequestedModelID)}
	}
	plannedAttempts := make([]runtimeTerminalAttempt, 0, len(attempts))
	var firstUpstream plannedUpstreamRequest
	for index, attempt := range attempts {
		upstreamRequest, err := buildPlannedUpstreamRequest(input, operation, attempt)
		if err != nil {
			return nil, plannedUpstreamRequest{}, err
		}
		upstreamRequest, err = applyCustomRequestParametersOverlay(input, operation, upstreamRequest, attempt)
		if err != nil {
			return nil, plannedUpstreamRequest{}, err
		}
		planned := attempt
		planned.EffectiveRequestPath = upstreamRequest.EffectiveRequestPath
		planned.UpstreamBody = upstreamRequest.UpstreamBody
		planned.RequestGenerationParams = upstreamRequest.RequestGenerationParams
		planned.AuditEnabledAtRequest = attempt.TargetModel.AuditEnabled
		planned.AuditCaptureBodiesRequest = attempt.TargetModel.AuditEnabled && attempt.TargetModel.AuditCaptureBodies
		plannedAttempts = append(plannedAttempts, planned)
		if index == 0 {
			firstUpstream = upstreamRequest
		}
	}
	return plannedAttempts, firstUpstream, nil
}

// applyCustomRequestParametersOverlay applies the attempt Connection's custom
// request parameters as a top-level shallow overlay on the provider-native
// upstream body (after model/path rewrite). It is a no-op for unconfigured
// Connections and for the rawBody == nil Gemini probe phase. When a
// configuration exists, the ingress body must be a valid JSON object, the
// merged body must stay within the runtime JSON body limit, and the
// generation-parameter snapshot is re-extracted from the final effective
// body. All failures happen before admission, Ban Policy attempt counting,
// and provider transport.
func applyCustomRequestParametersOverlay(input requestPlanningInput, operation resolvedRequestOperation, upstreamRequest plannedUpstreamRequest, attempt runtimeTerminalAttempt) (plannedUpstreamRequest, error) {
	config := attempt.Connection.CustomRequestParameters
	if config == nil || config.IsEmpty() {
		return upstreamRequest, nil
	}
	if input.ProbePlanning {
		return upstreamRequest, nil
	}
	if !isJSONObjectBody(input.RawBody) {
		return plannedUpstreamRequest{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Request body must be a JSON object when custom request parameters are configured"}
	}
	merged, err := config.OverlayRequestBody(upstreamRequest.UpstreamBody)
	if err != nil {
		return plannedUpstreamRequest{}, &domainError{StatusCode: http.StatusBadRequest, Detail: "Request body must be a JSON object when custom request parameters are configured"}
	}
	if int64(len(merged)) > bodylimits.RuntimeJSONRequestBodyLimitBytes {
		return plannedUpstreamRequest{}, &domainError{
			StatusCode: http.StatusRequestEntityTooLarge,
			ErrorCode:  "request_body_too_large",
			Detail:     "Request body is too large after applying custom request parameters",
			Fields:     map[string]any{"limit_bytes": bodylimits.RuntimeJSONRequestBodyLimitBytes},
		}
	}
	upstreamRequest.UpstreamBody = merged
	upstreamRequest.RequestGenerationParams = extractBufferedRequestGenerationParams(operation.Match.Operation, merged)
	return upstreamRequest, nil
}

func isJSONObjectBody(raw []byte) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, ok := token.(json.Delim)
	return ok && delim == '{'
}

func connectionsFromTerminalAttempts(attempts []runtimeTerminalAttempt) []runtimeConnection {
	connections := make([]runtimeConnection, 0, len(attempts))
	for _, attempt := range attempts {
		connections = append(connections, attempt.Connection)
	}
	return connections
}
