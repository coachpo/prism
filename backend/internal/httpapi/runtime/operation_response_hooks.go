package runtime

import (
	"io"
	"time"
)

type operationResponseKind string

const (
	operationResponseKindTextGeneration  operationResponseKind = "text_generation"
	operationResponseKindTokenCount      operationResponseKind = "token_count"
	operationResponseKindImageGeneration operationResponseKind = "image_generation"
)

type operationNonStreamResponseParser func(operationResponseHooks, io.Writer, io.Reader, string, func() time.Time, bool) (runtimeResponseCapture, error)

type operationResponseHooks struct {
	Provider               string
	Kind                   operationResponseKind
	UsageRule              runtimeUsageNormalizationRule
	ParseNonStreamResponse operationNonStreamResponseParser
}

var operationResponseHooksByCollectionID = map[string]operationResponseHooks{
	"openai.chat_completions": {
		Provider:               "openai",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleOpenAIChatCompletions,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	"openai.responses": {
		Provider:               "openai",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleOpenAIResponses,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	runtimeHookCollectionOpenAIResponsesInputTokens: {
		Provider:               "openai",
		Kind:                   operationResponseKindTokenCount,
		ParseNonStreamResponse: proxyNonEventTokenCountResponseAndCaptureUsage,
	},
	runtimeHookCollectionOpenAIResponsesCompact: {
		Provider:               "openai",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleOpenAIResponses,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	// ImagesResponse carries a root `usage` object for the GPT image models, so
	// image responses go through the ordinary usage-capturing parser rather than
	// the usage-less one. Models that return no usage (the DALL-E family) simply
	// produce no usage and are recorded unpriced.
	runtimeHookCollectionOpenAIImagesGeneration: {
		Provider:               "openai",
		Kind:                   operationResponseKindImageGeneration,
		UsageRule:              runtimeUsageRuleOpenAIImages,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	runtimeHookCollectionOpenAIImagesEdit: {
		Provider:               "openai",
		Kind:                   operationResponseKindImageGeneration,
		UsageRule:              runtimeUsageRuleOpenAIImages,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	"anthropic.messages": {
		Provider:               "anthropic",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleAnthropicMessages,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	runtimeHookCollectionAnthropicCountTokens: {
		Provider:               "anthropic",
		Kind:                   operationResponseKindTokenCount,
		ParseNonStreamResponse: proxyNonEventTokenCountResponseAndCaptureUsage,
	},
	"gemini.generate_content": {
		Provider:               "gemini",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleGeminiGenerateContent,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	"gemini.stream_generate_content": {
		Provider:               "gemini",
		Kind:                   operationResponseKindTextGeneration,
		UsageRule:              runtimeUsageRuleGeminiStreamGenerateContent,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureUsage,
	},
	runtimeHookCollectionGeminiCountTokens: {
		Provider:               "gemini",
		Kind:                   operationResponseKindTokenCount,
		ParseNonStreamResponse: proxyNonEventTokenCountResponseAndCaptureUsage,
	},
}

func responseHooksForOperation(operation RuntimeOperation) (operationResponseHooks, bool) {
	hookCollectionID := operation.HookCollectionID
	if hookCollectionID == "" {
		hookCollectionID = operation.Name
	}
	hooks, ok := operationResponseHooksByCollectionID[hookCollectionID]
	return hooks, ok
}

func proxyNonEventResponseAndCaptureByOperation(operation RuntimeOperation, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	hooks, ok := responseHooksForOperation(operation)
	if !ok || hooks.ParseNonStreamResponse == nil {
		return proxyNonEventResponseAndCaptureWithoutUsage(operationResponseHooks{}, dst, src, contentType, now, captureAuditBody)
	}
	return hooks.ParseNonStreamResponse(hooks, dst, src, contentType, now, captureAuditBody)
}
