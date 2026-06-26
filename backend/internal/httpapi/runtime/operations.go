package runtime

import (
	"net/http"
	"strings"
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
	staticPath string
	prefix     string
	suffix     string
	paramName  string
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

func RuntimeOperationCatalog() []RuntimeOperation {
	catalog := make([]RuntimeOperation, len(runtimeOperationCatalog))
	copy(catalog, runtimeOperationCatalog)
	return catalog
}

func ResolveRuntimeOperation(method string, requestPath string) (RuntimeOperationMatch, bool) {
	for _, operation := range runtimeOperationCatalog {
		params, matchedPath := operation.PathMatcher.Match(requestPath)
		if !matchedPath || method != operation.Method {
			continue
		}
		return RuntimeOperationMatch{Operation: operation, PathParams: params}, true
	}
	return RuntimeOperationMatch{}, false
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
	return RuntimeOperationPathMatcher{staticPath: strings.TrimSpace(path)}
}

func geminiRuntimeOperationPath(suffix string) RuntimeOperationPathMatcher {
	return RuntimeOperationPathMatcher{prefix: "/v1beta/models/", suffix: strings.TrimSpace(suffix), paramName: "model"}
}

func (matcher RuntimeOperationPathMatcher) Match(requestPath string) (map[string]string, bool) {
	if matcher.staticPath != "" {
		return nil, requestPath == matcher.staticPath
	}
	if matcher.prefix == "" || matcher.suffix == "" || matcher.paramName == "" {
		return nil, false
	}
	if !strings.HasPrefix(requestPath, matcher.prefix) || !strings.HasSuffix(requestPath, matcher.suffix) {
		return nil, false
	}
	value := strings.TrimSuffix(strings.TrimPrefix(requestPath, matcher.prefix), matcher.suffix)
	if value == "" || strings.ContainsAny(value, "/:") {
		return nil, false
	}
	return map[string]string{matcher.paramName: value}, true
}
