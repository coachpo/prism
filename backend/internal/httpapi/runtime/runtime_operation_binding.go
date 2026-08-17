package runtime

import (
	"fmt"
	"net/http"
	"strings"
)

func validateResolvedRuntimeOperation(operationMatch RuntimeOperationMatch, requestMethod string, requestPath string) (RuntimeOperationMatch, error) {
	operation := operationMatch.Operation
	if strings.TrimSpace(operation.Name) == "" {
		return RuntimeOperationMatch{}, &domainError{StatusCode: http.StatusNotFound, Detail: runtimeOperationNotFoundDetail}
	}
	if operation.Method != requestMethod {
		return RuntimeOperationMatch{}, &domainError{StatusCode: http.StatusMethodNotAllowed, Detail: runtimeOperationMethodNotAllowedDetail}
	}
	pathParams, ok := operation.PathMatcher.Match(requestPath)
	if !ok {
		return RuntimeOperationMatch{}, &domainError{StatusCode: http.StatusNotFound, Detail: runtimeOperationNotFoundDetail}
	}
	return RuntimeOperationMatch{Operation: operation, PathParams: cloneStringMap(pathParams)}, nil
}

func resolveModelIDForOperation(rawBody []byte, contentType string, operationMatch RuntimeOperationMatch) (string, error) {
	switch operationMatch.Operation.ModelBindingSource {
	case RuntimeOperationModelBindingBody:
		if modelID := extractModelFromBody(rawBody); modelID != "" {
			return modelID, nil
		}
	case RuntimeOperationModelBindingPath:
		if modelID := strings.TrimSpace(operationMatch.PathParams["model"]); modelID != "" {
			return modelID, nil
		}
	default:
		return "", unsupportedOperationModelBindingError(operationMatch.Operation)
	}
	return "", &domainError{
		StatusCode: http.StatusBadRequest,
		Detail:     fmt.Sprintf("Cannot determine model for routing. Operation '%s' binds models from the %s.", operationMatch.Operation.Name, operationMatch.Operation.ModelBindingSource),
	}
}

func validateOperationAPIFamily(operation RuntimeOperation, targetModel runtimeModelRecord) error {
	operationAPIFamily := strings.ToLower(strings.TrimSpace(operation.APIFamily))
	targetAPIFamily := strings.ToLower(strings.TrimSpace(targetModel.APIFamily))
	if operationAPIFamily == targetAPIFamily && operationAPIFamily != "" {
		return nil
	}
	return &domainError{
		StatusCode: http.StatusBadRequest,
		Detail:     fmt.Sprintf("Operation '%s' is incompatible with api_family '%s'. Use an operation that matches the resolved model api_family.", operation.Name, targetModel.APIFamily),
	}
}

func unsupportedOperationModelBindingError(operation RuntimeOperation) error {
	return &domainError{
		StatusCode: http.StatusBadRequest,
		Detail:     fmt.Sprintf("Operation '%s' has unsupported model binding source '%s'.", operation.Name, operation.ModelBindingSource),
	}
}
