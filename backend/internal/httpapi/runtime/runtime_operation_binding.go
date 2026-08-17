package runtime

// Runtime operation binding validates the registry match against the incoming
// method/path and resolves the operation's declared model-binding source.
// It owns family compatibility errors but not model graph routing or body/path
// rewriting.
//
// The operation registry remains the sole route allowlist. This module only
// validates a match already returned by that registry.
//
// Wrong methods and missing path parameters remain typed ingress failures before
// body buffering, planning, admission, provider transport, or telemetry.
// Body model extraction is intentionally minimal; provider-native adapters own
// richer payload parsing after the operation has been accepted.
//
// The error details remain provider-neutral at this layer. Native operation
// compatibility diagnostics are attached later by the planning classifiers.
//
// This keeps ingress errors auditable without widening the route registry.
// The source-of-truth catalog remains operations.go.
// Binding never broadens the supported route set.
//
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
