package runtime

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

type operationResponseKind string

const (
	operationResponseKindTextGeneration operationResponseKind = "text_generation"
	operationResponseKindTokenCount     operationResponseKind = "token_count"
	operationResponseKindMedia          operationResponseKind = "media"
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
	runtimeHookCollectionOpenAIImagesGeneration: {
		Provider:               "openai",
		Kind:                   operationResponseKindMedia,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureWithoutUsage,
	},
	runtimeHookCollectionOpenAIImagesEdit: {
		Provider:               "openai",
		Kind:                   operationResponseKindMedia,
		ParseNonStreamResponse: proxyNonEventResponseAndCaptureWithoutUsage,
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

// ResponseHooksForOperation resolves response-hook metadata for a runtime operation.
func ResponseHooksForOperation(operation RuntimeOperation) (operationResponseHooks, bool) {
	return responseHooksForOperation(operation)
}

func proxyNonEventResponseAndCaptureByOperation(operation RuntimeOperation, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	hooks, ok := responseHooksForOperation(operation)
	if !ok || hooks.ParseNonStreamResponse == nil {
		return proxyNonEventResponseAndCaptureWithoutUsage(operationResponseHooks{}, dst, src, contentType, now, captureAuditBody)
	}
	return hooks.ParseNonStreamResponse(hooks, dst, src, contentType, now, captureAuditBody)
}

func proxyNonEventResponseAndCaptureWithoutUsage(_ operationResponseHooks, dst io.Writer, src io.Reader, _ string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	writers := []io.Writer{dst}
	auditBuffer := &bytes.Buffer{}
	if captureAuditBody {
		writers = append(writers, auditBuffer)
	}
	_, err := io.Copy(io.MultiWriter(writers...), src)
	completedAt := now()
	capture := runtimeResponseCapture{CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming}
	if captureAuditBody {
		capture.AuditBody = append([]byte(nil), auditBuffer.Bytes()...)
	}
	return capture, err
}

func proxyNonEventTokenCountResponseAndCaptureUsage(_ operationResponseHooks, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if !responseMayContainJSONUsage(contentType) {
		return proxyNonEventResponseAndCaptureWithoutUsage(operationResponseHooks{}, dst, src, contentType, now, captureAuditBody)
	}
	bodyBuffer := &bytes.Buffer{}
	auditBuffer := &bytes.Buffer{}
	writers := []io.Writer{dst, bodyBuffer}
	if captureAuditBody {
		writers = append(writers, auditBuffer)
	}
	_, copyErr := io.Copy(io.MultiWriter(writers...), src)
	completedAt := now()
	usage := extractTokenCountResponseUsage(bodyBuffer.Bytes())
	capture := runtimeResponseCapture{
		Body:          buildUsageBodyFromResponseUsage(usage),
		Usage:         usage,
		CompletedAt:   &completedAt,
		StreamOutcome: runtimeStreamOutcomeNotStreaming,
	}
	if captureAuditBody {
		capture.AuditBody = append([]byte(nil), auditBuffer.Bytes()...)
	}
	return capture, copyErr
}

func extractTokenCountResponseUsage(body []byte) responseUsage {
	if len(body) == 0 {
		return responseUsage{}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return responseUsage{}
	}
	usage := responseUsage{}
	if inputTokens := intPointerFromAny(payload["input_tokens"]); inputTokens != nil {
		usage.InputTokens = inputTokens
	}
	if totalTokens := intPointerFromAny(firstValue(payload, "total_tokens", "totalTokens")); totalTokens != nil {
		assignTokenCountTotal(&usage, *totalTokens)
	}
	if cacheReadTokens := intPointerFromAny(firstValue(payload, "cache_read_input_tokens", "cachedContentTokenCount")); cacheReadTokens != nil {
		usage.CacheReadInputTokens = cacheReadTokens
	}
	if cacheCreationTokens := intPointerFromAny(payload["cache_creation_input_tokens"]); cacheCreationTokens != nil {
		usage.CacheCreationInputTokens = cacheCreationTokens
	}
	if !usage.validForRuntimeUsage(runtimeUsageNormalizationRule{ValidateParentSplitBounds: true}) {
		return responseUsage{}
	}
	return usage.normalized()
}

func assignTokenCountTotal(usage *responseUsage, count int) {
	if usage.InputTokens == nil {
		inputTokens := count
		usage.InputTokens = &inputTokens
	}
	if usage.TotalTokens == nil {
		totalTokens := count
		usage.TotalTokens = &totalTokens
	}
}
