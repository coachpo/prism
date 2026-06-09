package runtime

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/gateway/registry"
)

type RuntimeOperationModelBindingSource string

const (
	RuntimeOperationModelBindingBody RuntimeOperationModelBindingSource = "body"
	RuntimeOperationModelBindingPath RuntimeOperationModelBindingSource = "path"
)

const (
	runtimeHookCollectionOpenAIResponsesInputTokens = "token_count.openai_responses_input_tokens"
	runtimeHookCollectionOpenAIResponsesCompact     = "openai.responses.compact"
	runtimeHookCollectionAnthropicCountTokens       = "token_count.anthropic_messages"
	runtimeHookCollectionGeminiCountTokens          = "token_count.gemini"
)

type RuntimeOperation struct {
	Name               string
	Method             string
	APIFamily          string
	PathTemplate       string
	PathMatcher        RuntimeOperationPathMatcher
	Streaming          bool
	ModelBindingSource RuntimeOperationModelBindingSource
	HookCollectionID   string
}

type RuntimeOperationPathMatcher struct {
	matcher registry.OperationPathMatcher
}

type RuntimeOperationMatch struct {
	Operation  RuntimeOperation
	PathParams map[string]string
}

var runtimeOperationCatalog = []RuntimeOperation{
	newRuntimeOperation("openai.chat_completions", "openai", "/v1/chat/completions", staticRuntimeOperationPath("/v1/chat/completions"), false, RuntimeOperationModelBindingBody),
	newRuntimeOperation("openai.responses", "openai", "/v1/responses", staticRuntimeOperationPath("/v1/responses"), false, RuntimeOperationModelBindingBody),
	newRuntimeOperationWithHookCollection("openai.responses.input_tokens", "openai", "/v1/responses/input_tokens", staticRuntimeOperationPath("/v1/responses/input_tokens"), false, RuntimeOperationModelBindingBody, runtimeHookCollectionOpenAIResponsesInputTokens),
	newRuntimeOperationWithHookCollection("openai.responses.compact", "openai", "/v1/responses/compact", staticRuntimeOperationPath("/v1/responses/compact"), false, RuntimeOperationModelBindingBody, runtimeHookCollectionOpenAIResponsesCompact),
	newRuntimeOperationWithHookCollection("openai.images.generations", "openai", "/v1/images/generations", staticRuntimeOperationPath("/v1/images/generations"), false, RuntimeOperationModelBindingBody, runtimeHookCollectionOpenAIImagesGeneration),
	newRuntimeOperationWithHookCollection("openai.images.edits", "openai", "/v1/images/edits", staticRuntimeOperationPath("/v1/images/edits"), false, RuntimeOperationModelBindingBody, runtimeHookCollectionOpenAIImagesEdit),
	newRuntimeOperation("anthropic.messages", "anthropic", "/v1/messages", staticRuntimeOperationPath("/v1/messages"), false, RuntimeOperationModelBindingBody),
	newRuntimeOperationWithHookCollection("anthropic.count_tokens", "anthropic", "/v1/messages/count_tokens", staticRuntimeOperationPath("/v1/messages/count_tokens"), false, RuntimeOperationModelBindingBody, runtimeHookCollectionAnthropicCountTokens),
	newRuntimeOperation("gemini.generate_content", "gemini", "/v1beta/models/{model}:generateContent", geminiRuntimeOperationPath(":generateContent"), false, RuntimeOperationModelBindingPath),
	newRuntimeOperation("gemini.stream_generate_content", "gemini", "/v1beta/models/{model}:streamGenerateContent", geminiRuntimeOperationPath(":streamGenerateContent"), true, RuntimeOperationModelBindingPath),
	newRuntimeOperationWithHookCollection("gemini.count_tokens", "gemini", "/v1beta/models/{model}:countTokens", geminiRuntimeOperationPath(":countTokens"), false, RuntimeOperationModelBindingPath, runtimeHookCollectionGeminiCountTokens),
}

var runtimeOperationRegistry = registry.MustNewOperationRegistry(runtimeOperationDefinitions(runtimeOperationCatalog))

func RuntimeOperationCatalog() []RuntimeOperation {
	catalog := make([]RuntimeOperation, len(runtimeOperationCatalog))
	copy(catalog, runtimeOperationCatalog)
	return catalog
}

func RuntimeOperationRegistry() *registry.InMemoryOperationRegistry {
	return runtimeOperationRegistry
}

func ResolveRuntimeOperation(method string, requestPath string) (RuntimeOperationMatch, bool) {
	match, _, ok := runtimeOperationRegistry.ResolveMatch(method, requestPath)
	if !ok {
		return RuntimeOperationMatch{}, false
	}
	operation, ok := runtimeOperationByName(match.Operation.Name)
	if !ok {
		return RuntimeOperationMatch{}, false
	}
	return RuntimeOperationMatch{Operation: operation, PathParams: match.PathParams}, true
}

func newRuntimeOperation(name string, apiFamily string, pathTemplate string, matcher RuntimeOperationPathMatcher, streaming bool, modelBindingSource RuntimeOperationModelBindingSource) RuntimeOperation {
	return RuntimeOperation{
		Name:               name,
		Method:             http.MethodPost,
		APIFamily:          apiFamily,
		PathTemplate:       pathTemplate,
		PathMatcher:        matcher,
		Streaming:          streaming,
		ModelBindingSource: modelBindingSource,
		HookCollectionID:   name,
	}
}

func newRuntimeOperationWithHookCollection(name string, apiFamily string, pathTemplate string, matcher RuntimeOperationPathMatcher, streaming bool, modelBindingSource RuntimeOperationModelBindingSource, hookCollectionID string) RuntimeOperation {
	operation := newRuntimeOperation(name, apiFamily, pathTemplate, matcher, streaming, modelBindingSource)
	operation.HookCollectionID = hookCollectionID
	return operation
}

func staticRuntimeOperationPath(path string) RuntimeOperationPathMatcher {
	return RuntimeOperationPathMatcher{matcher: registry.StaticOperationPath(path)}
}

func geminiRuntimeOperationPath(suffix string) RuntimeOperationPathMatcher {
	return RuntimeOperationPathMatcher{matcher: registry.ParameterizedOperationPath("/v1beta/models/", suffix, "model")}
}

func (matcher RuntimeOperationPathMatcher) Match(requestPath string) (map[string]string, bool) {
	return matcher.matcher.Match(requestPath)
}

func runtimeOperationDefinitions(operations []RuntimeOperation) []registry.OperationDefinition {
	definitions := make([]registry.OperationDefinition, 0, len(operations))
	for _, operation := range operations {
		definitions = append(definitions, registry.OperationDefinition{
			Operation: registry.Operation{
				Name:               operation.Name,
				Method:             operation.Method,
				APIFamily:          operation.APIFamily,
				PathTemplate:       operation.PathTemplate,
				Streaming:          operation.Streaming,
				ModelBindingSource: registry.OperationModelBindingSource(operation.ModelBindingSource),
				HookCollectionID:   operation.HookCollectionID,
			},
			PathMatcher: operation.PathMatcher.matcher,
		})
	}
	return definitions
}

func runtimeOperationByName(name string) (RuntimeOperation, bool) {
	for _, operation := range runtimeOperationCatalog {
		if operation.Name == name {
			return operation, true
		}
	}
	return RuntimeOperation{}, false
}
