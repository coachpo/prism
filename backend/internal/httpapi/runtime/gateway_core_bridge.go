package runtime

import (
	"net/http"
	"strings"
	"time"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
	"github.com/go-chi/chi/v5/middleware"
)

func newGatewayCoreRequestEnvelope(request *http.Request, operationMatch RuntimeOperationMatch) gatewaycore.RequestEnvelope {
	if request == nil {
		return gatewaycore.RequestEnvelope{}
	}
	return gatewaycore.NewRequestEnvelope(gatewaycore.RequestEnvelopeInput{
		Context:    gatewaycore.NewRequestContext(request.Context(), middleware.GetReqID(request.Context()), time.Time{}),
		Operation:  gatewayCoreOperationDescriptor(operationMatch.Operation),
		Method:     request.Method,
		Path:       request.URL.Path,
		RawQuery:   request.URL.RawQuery,
		PathParams: operationMatch.PathParams,
		Headers:    map[string][]string(request.Header),
	})
}

func gatewayCoreOperationDescriptor(operation RuntimeOperation) gatewaycore.OperationDescriptor {
	return gatewaycore.OperationDescriptor{
		Name:               strings.TrimSpace(operation.Name),
		Method:             strings.TrimSpace(operation.Method),
		APIFamily:          gatewaycore.APIFamily(strings.TrimSpace(operation.APIFamily)),
		PathTemplate:       strings.TrimSpace(operation.PathTemplate),
		Shape:              gatewayCoreEndpointShape(operation),
		Streaming:          operation.Streaming,
		ModelBindingSource: gatewayCoreModelBindingSource(operation.ModelBindingSource),
	}
}

func gatewayCoreEndpointShape(operation RuntimeOperation) gatewaycore.EndpointShape {
	operationName := strings.TrimSpace(operation.Name)
	switch {
	case strings.Contains(operationName, "count_tokens") || strings.Contains(operationName, "input_tokens"):
		return gatewaycore.EndpointShapeTokenCount
	default:
		return gatewaycore.EndpointShapeTextGeneration
	}
}

func gatewayCoreModelBindingSource(source RuntimeOperationModelBindingSource) gatewaycore.ModelBindingSource {
	switch source {
	case RuntimeOperationModelBindingPath:
		return gatewaycore.ModelBindingSourcePath
	default:
		return gatewaycore.ModelBindingSourceBody
	}
}
