package registry_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/registry"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimeOperationRegistryBridgePreservesCurrentAllowlist(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		pathParams   map[string]string
		streaming    bool
		modelBinding registry.OperationModelBindingSource
	}{
		{"openai.chat_completions", "/v1/chat/completions", nil, false, registry.OperationModelBindingBody},
		{"openai.responses", "/v1/responses", nil, false, registry.OperationModelBindingBody},
		{"openai.responses.input_tokens", "/v1/responses/input_tokens", nil, false, registry.OperationModelBindingBody},
		{"openai.responses.compact", "/v1/responses/compact", nil, false, registry.OperationModelBindingBody},
		{"openai.images.generations", "/v1/images/generations", nil, false, registry.OperationModelBindingBody},
		{"openai.images.edits", "/v1/images/edits", nil, false, registry.OperationModelBindingBody},
		{"anthropic.messages", "/v1/messages", nil, false, registry.OperationModelBindingBody},
		{"anthropic.count_tokens", "/v1/messages/count_tokens", nil, false, registry.OperationModelBindingBody},
		{"gemini.generate_content", "/v1beta/models/gemini-2.5-pro:generateContent", map[string]string{"model": "gemini-2.5-pro"}, false, registry.OperationModelBindingPath},
		{"gemini.stream_generate_content", "/v1beta/models/gemini-2.5-pro:streamGenerateContent", map[string]string{"model": "gemini-2.5-pro"}, true, registry.OperationModelBindingPath},
		{"gemini.count_tokens", "/v1beta/models/gemini-2.5-pro:countTokens", map[string]string{"model": "gemini-2.5-pro"}, false, registry.OperationModelBindingPath},
	}

	operationRegistry := runtimeapi.RuntimeOperationRegistry()
	if got := len(operationRegistry.Operations()); got != len(tests) {
		t.Fatalf("expected exactly %d bridged operations, got %d", len(tests), got)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match, allowedMethods, ok := operationRegistry.ResolveMatch(http.MethodPost, test.path)
			if !ok {
				t.Fatalf("expected registry to resolve POST %s; allowed=%v", test.path, allowedMethods)
			}
			if match.Operation.Name != test.name {
				t.Fatalf("expected operation %q, got %+v", test.name, match.Operation)
			}
			if match.Operation.Streaming != test.streaming {
				t.Fatalf("expected streaming=%v, got %v", test.streaming, match.Operation.Streaming)
			}
			if match.Operation.ModelBindingSource != test.modelBinding {
				t.Fatalf("expected model binding %q, got %q", test.modelBinding, match.Operation.ModelBindingSource)
			}
			if !reflect.DeepEqual(match.PathParams, test.pathParams) {
				t.Fatalf("expected path params %+v, got %+v", test.pathParams, match.PathParams)
			}

			runtimeMatch, ok := runtimeapi.ResolveRuntimeOperation(http.MethodPost, test.path)
			if !ok {
				t.Fatalf("expected runtime bridge to resolve POST %s", test.path)
			}
			if runtimeMatch.Operation.Name != match.Operation.Name || runtimeMatch.Operation.PathTemplate != match.Operation.PathTemplate {
				t.Fatalf("runtime and registry operation drift: runtime=%+v registry=%+v", runtimeMatch.Operation, match.Operation)
			}
			if !reflect.DeepEqual(runtimeMatch.PathParams, match.PathParams) {
				t.Fatalf("runtime and registry params drift: runtime=%+v registry=%+v", runtimeMatch.PathParams, match.PathParams)
			}
		})
	}
}

func TestRuntimeOperationRegistryBridgeRejectsUnsupportedAndWrongMethodRoutes(t *testing.T) {
	operationRegistry := runtimeapi.RuntimeOperationRegistry()
	for _, path := range []string{
		"/v1/responses/input_tokens/extra",
		"/v1/responses/compact/extra",
		"/v1/models",
		"/v1beta/models/gemini-2.5-pro:embedContent",
	} {
		if match, allowedMethods, ok := operationRegistry.ResolveMatch(http.MethodPost, path); ok || len(allowedMethods) != 0 {
			t.Fatalf("expected unsupported POST %s to reject without allowed methods, got ok=%v match=%+v allowed=%v", path, ok, match, allowedMethods)
		}
	}

	_, allowedMethods, ok := operationRegistry.Resolve(http.MethodGet, "/v1/chat/completions")
	if ok {
		t.Fatal("expected wrong method not to resolve")
	}
	if !reflect.DeepEqual([]string(allowedMethods), []string{http.MethodPost}) {
		t.Fatalf("expected wrong method to expose POST only, got %v", allowedMethods)
	}
}
