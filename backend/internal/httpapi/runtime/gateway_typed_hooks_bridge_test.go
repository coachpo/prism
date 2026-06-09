package runtime

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

func TestRuntimeTypedHookBridgeBuildsSafePayload(t *testing.T) {
	operationMatch, ok := ResolveRuntimeOperation(http.MethodPost, "/v1/images/generations")
	if !ok {
		t.Fatal("expected image generation operation")
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/images/generations?trace=true", nil)
	request.Header.Set("Authorization", "Bearer provider-secret")
	request.Header.Set("X-Api-Key", "provider-key")
	request.Header.Set("X-Visible", "safe")
	input := NewRuntimeTypedHookPayloadInput(request, operationMatch, []byte(`{"model":"gpt-image-1","prompt":"hidden"}`), gatewaycore.HookPhaseOnIngress)

	if input.Envelope.Operation.Shape != gatewaycore.EndpointShapeImageGeneration {
		t.Fatalf("expected image generation shape, got %q", input.Envelope.Operation.Shape)
	}
	if input.AdditionalMetadata["hook_collection_id"] != runtimeHookCollectionOpenAIImagesGeneration {
		t.Fatalf("expected hook collection metadata, got %+v", input.AdditionalMetadata)
	}
	defaultPayload := gatewaycore.NewHookPayload(input)
	if len(defaultPayload.Headers) != 0 || len(defaultPayload.RequestBody) != 0 {
		t.Fatalf("default typed hook payload exposed restricted data: %+v", defaultPayload)
	}

	input.Permissions = []gatewaycore.HookPermission{gatewaycore.HookPermissionReadHeaders, gatewaycore.HookPermissionReadRequestBody}
	allowedPayload := gatewaycore.NewHookPayload(input)
	if allowedPayload.Headers["Authorization"] != nil || allowedPayload.Headers["X-Api-Key"] != nil {
		t.Fatalf("provider credential headers leaked to typed hook payload: %+v", allowedPayload.Headers)
	}
	if got := allowedPayload.Headers["X-Visible"]; !reflect.DeepEqual(got, []string{"safe"}) {
		t.Fatalf("expected visible header, got %+v", allowedPayload.Headers)
	}
	if string(allowedPayload.RequestBody) != string(input.Envelope.Body) {
		t.Fatalf("explicit request-body permission did not expose body")
	}
}

func TestRuntimeGatewayCoreDescriptorMapsHookShapes(t *testing.T) {
	tests := []struct {
		path string
		want gatewaycore.EndpointShape
	}{
		{path: "/v1/chat/completions", want: gatewaycore.EndpointShapeTextGeneration},
		{path: "/v1/responses/input_tokens", want: gatewaycore.EndpointShapeTokenCount},
		{path: "/v1/images/edits", want: gatewaycore.EndpointShapeImageEdit},
		{path: "/v1beta/models/gemini-2.5-pro:countTokens", want: gatewaycore.EndpointShapeTokenCount},
	}
	for _, test := range tests {
		operationMatch, ok := ResolveRuntimeOperation(http.MethodPost, test.path)
		if !ok {
			t.Fatalf("resolve %s", test.path)
		}
		if got := gatewayCoreOperationDescriptor(operationMatch.Operation).Shape; got != test.want {
			t.Fatalf("expected %s shape %q, got %q", test.path, test.want, got)
		}
	}
}
