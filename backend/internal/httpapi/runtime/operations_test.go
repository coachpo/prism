package runtime

import (
	"net/http"
	"reflect"
	"testing"
)

type runtimeOperationExpectation struct {
	name               string
	method             string
	apiFamily          string
	pathTemplate       string
	samplePath         string
	streaming          bool
	modelBindingSource RuntimeOperationModelBindingSource
	pathParams         map[string]string
	hookCollectionID   string
}

func TestResolveRuntimeOperation(t *testing.T) {
	for _, want := range runtimeOperationExpectations() {
		t.Run(want.name, func(t *testing.T) {
			match, ok := ResolveRuntimeOperation(want.method, want.samplePath)
			if !ok {
				t.Fatalf("expected %s %s to resolve", want.method, want.samplePath)
			}
			assertRuntimeOperation(t, match.Operation, want)
			if !reflect.DeepEqual(match.PathParams, want.pathParams) {
				t.Fatalf("expected path params %+v, got %+v", want.pathParams, match.PathParams)
			}
		})
	}

	for _, want := range runtimeOperationExpectations() {
		t.Run("wrong method "+want.name, func(t *testing.T) {
			wrongMethod := http.MethodGet
			if want.method == http.MethodGet {
				wrongMethod = http.MethodPost
			}
			if match, ok := ResolveRuntimeOperation(wrongMethod, want.samplePath); ok {
				t.Fatalf("expected %s %s to be unsupported, got %+v", wrongMethod, want.samplePath, match)
			}
		})
	}

	unsupported := []struct {
		name   string
		method string
		path   string
	}{
		{name: "lowercase method", method: "post", path: "/v1/responses"},
		{name: "unknown openai path", method: http.MethodPost, path: "/v1/completions"},
		{name: "openai models wrong method", method: http.MethodPost, path: "/v1/models"},
		{name: "responses input tokens trailing slash", method: http.MethodPost, path: "/v1/responses/input_tokens/"},
		{name: "responses compact nested path", method: http.MethodPost, path: "/v1/responses/compact/extra"},
		{name: "chat completions trailing slash", method: http.MethodPost, path: "/v1/chat/completions/"},
		{name: "anthropic trailing slash", method: http.MethodPost, path: "/v1/messages/"},
		{name: "gemini missing action", method: http.MethodPost, path: "/v1beta/models/gemini-2.5-pro"},
		{name: "gemini unsupported action", method: http.MethodPost, path: "/v1beta/models/gemini-2.5-pro:embedContent"},
		{name: "gemini empty model", method: http.MethodPost, path: "/v1beta/models/:generateContent"},
		{name: "gemini nested model path", method: http.MethodPost, path: "/v1beta/models/publishers/google/models/gemini-2.5-pro:generateContent"},
		{name: "gemini trailing slash", method: http.MethodPost, path: "/v1beta/models/gemini-2.5-pro:streamGenerateContent/"},
	}
	for _, test := range unsupported {
		t.Run(test.name, func(t *testing.T) {
			if match, ok := ResolveRuntimeOperation(test.method, test.path); ok {
				t.Fatalf("expected %s %s to be unsupported, got %+v", test.method, test.path, match)
			}
		})
	}
}

func TestResolveRuntimeOperationIncludesCountTokens(t *testing.T) {
	tests := []struct {
		name             string
		path             string
		apiFamily        string
		hookCollectionID string
		modelBinding     RuntimeOperationModelBindingSource
		pathParams       map[string]string
	}{
		{
			name:             "openai.responses.input_tokens",
			path:             "/v1/responses/input_tokens",
			apiFamily:        "openai",
			hookCollectionID: runtimeHookCollectionOpenAIResponsesInputTokens,
			modelBinding:     RuntimeOperationModelBindingBody,
		},
		{
			name:             "anthropic.count_tokens",
			path:             "/v1/messages/count_tokens",
			apiFamily:        "anthropic",
			hookCollectionID: runtimeHookCollectionAnthropicCountTokens,
			modelBinding:     RuntimeOperationModelBindingBody,
		},
		{
			name:             "gemini.count_tokens",
			path:             "/v1beta/models/gemini-2.5-pro:countTokens",
			apiFamily:        "gemini",
			hookCollectionID: runtimeHookCollectionGeminiCountTokens,
			modelBinding:     RuntimeOperationModelBindingPath,
			pathParams:       map[string]string{"model": "gemini-2.5-pro"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match, ok := ResolveRuntimeOperation(http.MethodPost, test.path)
			if !ok {
				t.Fatalf("expected count-token route %s to resolve", test.path)
			}
			operation := match.Operation
			if operation.Name != test.name || operation.APIFamily != test.apiFamily {
				t.Fatalf("expected %s/%s, got %+v", test.name, test.apiFamily, operation)
			}
			if operation.HookCollectionID != test.hookCollectionID {
				t.Fatalf("expected hook collection %q, got %q", test.hookCollectionID, operation.HookCollectionID)
			}
			if operation.Streaming {
				t.Fatal("expected count-token operation to be non-streaming")
			}
			if operation.ModelBindingSource != test.modelBinding {
				t.Fatalf("expected model binding %q, got %q", test.modelBinding, operation.ModelBindingSource)
			}
			if !reflect.DeepEqual(match.PathParams, test.pathParams) {
				t.Fatalf("expected path params %+v, got %+v", test.pathParams, match.PathParams)
			}
		})
	}
	if match, ok := ResolveRuntimeOperation(http.MethodPost, "/v1beta/models/gemini-2.5-pro:count_tokens"); ok {
		t.Fatalf("expected non-contract Gemini count_tokens spelling to be unsupported, got %+v", match)
	}
}

func TestRuntimeOperationCatalog(t *testing.T) {
	want := runtimeOperationExpectations()
	catalog := RuntimeOperationCatalog()
	if len(catalog) != len(want) {
		t.Fatalf("expected %d runtime operations, got %d", len(want), len(catalog))
	}
	seen := map[string]struct{}{}
	for i, operation := range catalog {
		assertRuntimeOperation(t, operation, want[i])
		if _, ok := seen[operation.Name]; ok {
			t.Fatalf("duplicate runtime operation %q", operation.Name)
		}
		seen[operation.Name] = struct{}{}
		params, ok := operation.PathMatcher.Match(want[i].samplePath)
		if !ok {
			t.Fatalf("expected catalog matcher for %q to match %s", operation.Name, want[i].samplePath)
		}
		if !reflect.DeepEqual(params, want[i].pathParams) {
			t.Fatalf("expected catalog matcher params %+v, got %+v", want[i].pathParams, params)
		}
	}
	catalog[0].Name = "mutated"
	if got := RuntimeOperationCatalog()[0].Name; got == "mutated" {
		t.Fatal("expected RuntimeOperationCatalog to return a defensive copy")
	}
}

func assertRuntimeOperation(t *testing.T, operation RuntimeOperation, want runtimeOperationExpectation) {
	t.Helper()
	if operation.Name != want.name {
		t.Fatalf("expected operation name %q, got %q", want.name, operation.Name)
	}
	if operation.Method != want.method {
		t.Fatalf("expected method %q, got %q", want.method, operation.Method)
	}
	if operation.APIFamily != want.apiFamily {
		t.Fatalf("expected api_family %q, got %q", want.apiFamily, operation.APIFamily)
	}
	if operation.PathTemplate != want.pathTemplate {
		t.Fatalf("expected path template %q, got %q", want.pathTemplate, operation.PathTemplate)
	}
	if operation.Streaming != want.streaming {
		t.Fatalf("expected streaming=%v, got %v", want.streaming, operation.Streaming)
	}
	if operation.ModelBindingSource != want.modelBindingSource {
		t.Fatalf("expected model binding source %q, got %q", want.modelBindingSource, operation.ModelBindingSource)
	}
	wantHookCollectionID := want.name
	if want.hookCollectionID != "" {
		wantHookCollectionID = want.hookCollectionID
	}
	if operation.HookCollectionID != wantHookCollectionID {
		t.Fatalf("expected hook collection id %q, got %q", wantHookCollectionID, operation.HookCollectionID)
	}
}

func runtimeOperationExpectations() []runtimeOperationExpectation {
	return []runtimeOperationExpectation{
		{
			name:               "openai.chat_completions",
			method:             http.MethodPost,
			apiFamily:          "openai",
			pathTemplate:       "/v1/chat/completions",
			samplePath:         "/v1/chat/completions",
			modelBindingSource: RuntimeOperationModelBindingBody,
		},
		{
			name:               "openai.responses",
			method:             http.MethodPost,
			apiFamily:          "openai",
			pathTemplate:       "/v1/responses",
			samplePath:         "/v1/responses",
			modelBindingSource: RuntimeOperationModelBindingBody,
		},
		{
			name:               "openai.responses.input_tokens",
			method:             http.MethodPost,
			apiFamily:          "openai",
			pathTemplate:       "/v1/responses/input_tokens",
			samplePath:         "/v1/responses/input_tokens",
			modelBindingSource: RuntimeOperationModelBindingBody,
			hookCollectionID:   runtimeHookCollectionOpenAIResponsesInputTokens,
		},
		{
			name:               "openai.responses.compact",
			method:             http.MethodPost,
			apiFamily:          "openai",
			pathTemplate:       "/v1/responses/compact",
			samplePath:         "/v1/responses/compact",
			modelBindingSource: RuntimeOperationModelBindingBody,
			hookCollectionID:   runtimeHookCollectionOpenAIResponsesCompact,
		},
		{
			name:               "openai.images.generations",
			method:             http.MethodPost,
			apiFamily:          "openai",
			pathTemplate:       "/v1/images/generations",
			samplePath:         "/v1/images/generations",
			modelBindingSource: RuntimeOperationModelBindingBody,
			hookCollectionID:   runtimeHookCollectionOpenAIImagesGeneration,
		},
		{
			name:               "openai.images.edits",
			method:             http.MethodPost,
			apiFamily:          "openai",
			pathTemplate:       "/v1/images/edits",
			samplePath:         "/v1/images/edits",
			modelBindingSource: RuntimeOperationModelBindingBody,
			hookCollectionID:   runtimeHookCollectionOpenAIImagesEdit,
		},
		{
			name:               "openai.models",
			method:             http.MethodGet,
			apiFamily:          "openai",
			pathTemplate:       "/v1/models",
			samplePath:         "/v1/models",
			modelBindingSource: RuntimeOperationModelBindingSource("none"),
		},
		{
			name:               "anthropic.messages",
			method:             http.MethodPost,
			apiFamily:          "anthropic",
			pathTemplate:       "/v1/messages",
			samplePath:         "/v1/messages",
			modelBindingSource: RuntimeOperationModelBindingBody,
		},
		{
			name:               "anthropic.count_tokens",
			method:             http.MethodPost,
			apiFamily:          "anthropic",
			pathTemplate:       "/v1/messages/count_tokens",
			samplePath:         "/v1/messages/count_tokens",
			modelBindingSource: RuntimeOperationModelBindingBody,
			hookCollectionID:   runtimeHookCollectionAnthropicCountTokens,
		},
		{
			name:               "gemini.generate_content",
			method:             http.MethodPost,
			apiFamily:          "gemini",
			pathTemplate:       "/v1beta/models/{model}:generateContent",
			samplePath:         "/v1beta/models/gemini-2.5-pro:generateContent",
			modelBindingSource: RuntimeOperationModelBindingPath,
			pathParams:         map[string]string{"model": "gemini-2.5-pro"},
		},
		{
			name:               "gemini.stream_generate_content",
			method:             http.MethodPost,
			apiFamily:          "gemini",
			pathTemplate:       "/v1beta/models/{model}:streamGenerateContent",
			samplePath:         "/v1beta/models/gemini-2.5-pro:streamGenerateContent",
			streaming:          true,
			modelBindingSource: RuntimeOperationModelBindingPath,
			pathParams:         map[string]string{"model": "gemini-2.5-pro"},
		},
		{
			name:               "gemini.count_tokens",
			method:             http.MethodPost,
			apiFamily:          "gemini",
			pathTemplate:       "/v1beta/models/{model}:countTokens",
			samplePath:         "/v1beta/models/gemini-2.5-pro:countTokens",
			modelBindingSource: RuntimeOperationModelBindingPath,
			pathParams:         map[string]string{"model": "gemini-2.5-pro"},
			hookCollectionID:   runtimeHookCollectionGeminiCountTokens,
		},
	}
}
