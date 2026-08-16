package core

import "strings"

type EndpointClassification struct {
	OperationName       string             `json:"operation_name"`
	APIFamily           APIFamily          `json:"api_family"`
	Shape               EndpointShape      `json:"shape"`
	Streaming           bool               `json:"streaming"`
	ModelBindingSource  ModelBindingSource `json:"model_binding_source"`
	RequestPathTemplate string             `json:"request_path_template"`
}

func ClassifyEndpoint(descriptor OperationDescriptor) (EndpointClassification, error) {
	fields := validateEndpointDescriptor(descriptor)
	if len(fields) > 0 {
		return EndpointClassification{}, NewGatewayError(ErrorTypeValidation, "invalid_endpoint_descriptor", "Invalid endpoint descriptor", 0, fields...)
	}
	return EndpointClassification{
		OperationName:       strings.TrimSpace(descriptor.Name),
		APIFamily:           APIFamily(strings.TrimSpace(string(descriptor.APIFamily))),
		Shape:               descriptor.Shape,
		Streaming:           descriptor.Streaming,
		ModelBindingSource:  descriptor.ModelBindingSource,
		RequestPathTemplate: strings.TrimSpace(descriptor.PathTemplate),
	}, nil
}

func validateEndpointDescriptor(descriptor OperationDescriptor) []FieldError {
	var fields []FieldError
	if strings.TrimSpace(descriptor.Name) == "" {
		fields = append(fields, FieldError{Field: "name", Code: "required", Detail: "operation name is required"})
	}
	if strings.TrimSpace(descriptor.Method) == "" {
		fields = append(fields, FieldError{Field: "method", Code: "required", Detail: "HTTP method is required"})
	}
	if strings.TrimSpace(string(descriptor.APIFamily)) == "" {
		fields = append(fields, FieldError{Field: "api_family", Code: "required", Detail: "API family is required"})
	}
	if strings.TrimSpace(descriptor.PathTemplate) == "" {
		fields = append(fields, FieldError{Field: "path_template", Code: "required", Detail: "path template is required"})
	}
	if !validEndpointShape(descriptor.Shape) {
		fields = append(fields, FieldError{Field: "shape", Code: "unsupported", Detail: "endpoint shape is unsupported"})
	}
	if !validModelBindingSource(descriptor.ModelBindingSource) {
		fields = append(fields, FieldError{Field: "model_binding_source", Code: "unsupported", Detail: "model binding source is unsupported"})
	}
	return fields
}

func validEndpointShape(shape EndpointShape) bool {
	switch shape {
	case EndpointShapeTextGeneration, EndpointShapeTokenCount, EndpointShapeImageGeneration, EndpointShapeImageEdit:
		return true
	default:
		return false
	}
}

func validModelBindingSource(source ModelBindingSource) bool {
	switch source {
	case ModelBindingSourceBody, ModelBindingSourcePath:
		return true
	default:
		return false
	}
}
