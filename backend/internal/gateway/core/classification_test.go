package core

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestEndpointClassification(t *testing.T) {
	tests := []struct {
		name       string
		descriptor OperationDescriptor
		want       EndpointClassification
	}{
		{
			name:       "body-bound text generation",
			descriptor: OperationDescriptor{Name: "openai.chat_completions", Method: http.MethodPost, APIFamily: APIFamilyOpenAI, PathTemplate: "/v1/chat/completions", Shape: EndpointShapeTextGeneration, ModelBindingSource: ModelBindingSourceBody},
			want:       EndpointClassification{OperationName: "openai.chat_completions", APIFamily: APIFamilyOpenAI, Shape: EndpointShapeTextGeneration, ModelBindingSource: ModelBindingSourceBody, RequestPathTemplate: "/v1/chat/completions"},
		},
		{
			name:       "path-bound streaming generation",
			descriptor: OperationDescriptor{Name: "gemini.stream_generate_content", Method: http.MethodPost, APIFamily: APIFamilyGemini, PathTemplate: "/v1beta/models/{model}:streamGenerateContent", Shape: EndpointShapeTextGeneration, Streaming: true, ModelBindingSource: ModelBindingSourcePath},
			want:       EndpointClassification{OperationName: "gemini.stream_generate_content", APIFamily: APIFamilyGemini, Shape: EndpointShapeTextGeneration, Streaming: true, ModelBindingSource: ModelBindingSourcePath, RequestPathTemplate: "/v1beta/models/{model}:streamGenerateContent"},
		},
		{
			name:       "token count operation",
			descriptor: OperationDescriptor{Name: "anthropic.count_tokens", Method: http.MethodPost, APIFamily: APIFamilyAnthropic, PathTemplate: "/v1/messages/count_tokens", Shape: EndpointShapeTokenCount, ModelBindingSource: ModelBindingSourceBody},
			want:       EndpointClassification{OperationName: "anthropic.count_tokens", APIFamily: APIFamilyAnthropic, Shape: EndpointShapeTokenCount, ModelBindingSource: ModelBindingSourceBody, RequestPathTemplate: "/v1/messages/count_tokens"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ClassifyEndpoint(test.descriptor)
			if err != nil {
				t.Fatalf("classify endpoint: %v", err)
			}
			if got != test.want {
				t.Fatalf("expected %+v, got %+v", test.want, got)
			}
		})
	}
}

func TestEndpointClassificationInvalidDescriptorUsesTypedError(t *testing.T) {
	_, err := ClassifyEndpoint(OperationDescriptor{Method: http.MethodPost, Shape: "unknown"})
	var gatewayErr *GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("expected GatewayError, got %T", err)
	}
	if gatewayErr.Type != ErrorTypeValidation || gatewayErr.Code != "invalid_endpoint_descriptor" {
		t.Fatalf("expected validation error, got %+v", gatewayErr)
	}
	payload, err := json.Marshal(gatewayErr)
	if err != nil {
		t.Fatalf("marshal typed error: %v", err)
	}
	want := `{"type":"validation","code":"invalid_endpoint_descriptor","detail":"Invalid endpoint descriptor","fields":[{"field":"api_family","code":"required","detail":"API family is required"},{"field":"model_binding_source","code":"unsupported","detail":"model binding source is unsupported"},{"field":"name","code":"required","detail":"operation name is required"},{"field":"path_template","code":"required","detail":"path template is required"},{"field":"shape","code":"unsupported","detail":"endpoint shape is unsupported"}]}`
	if string(payload) != want {
		t.Fatalf("expected deterministic typed error JSON %s, got %s", want, string(payload))
	}
}
