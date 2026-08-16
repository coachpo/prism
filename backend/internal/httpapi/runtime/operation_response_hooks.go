package runtime

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	anthropicprovider "github.com/coachpo/prism/backend/internal/gateway/provider/anthropic"
	geminiprovider "github.com/coachpo/prism/backend/internal/gateway/provider/gemini"
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

type cliProxyAPIOverflowClassification struct {
	Promotable bool
	ErrorCode  string
	Classifier string
}

type cliProxyAPIOverflowEvidence struct {
	code    *string
	message *string
}

const (
	cliProxyAPIOverflowClassifierErrorCode   = "error_code"
	cliProxyAPIOverflowClassifierMessageText = "message_text"
)

func classifyCLIProxyAPIOverflowResponse(statusCode int, rawBody []byte) cliProxyAPIOverflowClassification {
	if !cliProxyAPIOverflowStatusAllowed(statusCode) {
		return cliProxyAPIOverflowClassification{}
	}
	payload, ok := decodeCLIProxyAPIOverflowPayload(rawBody)
	if !ok {
		return cliProxyAPIOverflowClassification{}
	}
	evidence, ok := extractCLIProxyAPIOverflowEvidence(payload)
	if !ok {
		return cliProxyAPIOverflowClassification{}
	}
	if evidence.code != nil {
		code := strings.ToLower(strings.TrimSpace(*evidence.code))
		if code == "context_length_exceeded" || code == "context_too_large" {
			return cliProxyAPIOverflowClassification{Promotable: true, ErrorCode: code, Classifier: cliProxyAPIOverflowClassifierErrorCode}
		}
		return cliProxyAPIOverflowClassification{}
	}
	message := normalizedCLIProxyAPIOverflowMessage(evidence.message)
	if message == "" || cliProxyAPIOverflowMessageRejected(message) {
		return cliProxyAPIOverflowClassification{}
	}
	if cliProxyAPIOverflowMessageSignalsOverflow(message) {
		return cliProxyAPIOverflowClassification{Promotable: true, Classifier: cliProxyAPIOverflowClassifierMessageText}
	}
	return cliProxyAPIOverflowClassification{}
}

func cliProxyAPIOverflowStatusAllowed(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func decodeCLIProxyAPIOverflowPayload(rawBody []byte) (map[string]any, bool) {
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return nil, false
	}
	payload := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, false
	}
	return payload, true
}

func extractCLIProxyAPIOverflowEvidence(payload map[string]any) (cliProxyAPIOverflowEvidence, bool) {
	if payload == nil {
		return cliProxyAPIOverflowEvidence{}, false
	}
	errorPayload, hasErrorObject := payload["error"].(map[string]any)
	if hasErrorObject {
		return extractCLIProxyAPIOverflowEvidenceFromErrorObject(errorPayload, payload)
	}
	return extractCLIProxyAPIOverflowEvidenceFromFlatPayload(payload)
}

func extractCLIProxyAPIOverflowEvidenceFromErrorObject(errorPayload map[string]any, payload map[string]any) (cliProxyAPIOverflowEvidence, bool) {
	if errorPayload == nil {
		return cliProxyAPIOverflowEvidence{}, false
	}
	evidence := cliProxyAPIOverflowEvidence{code: trimmedStringFromAny(errorPayload["code"])}
	if message := trimmedStringFromAny(firstValue(errorPayload, "message", "detail")); message != nil {
		evidence.message = message
	} else {
		evidence.message = trimmedStringFromAny(firstValue(payload, "message", "detail"))
	}
	return evidence, evidence.code != nil || evidence.message != nil
}

func extractCLIProxyAPIOverflowEvidenceFromFlatPayload(payload map[string]any) (cliProxyAPIOverflowEvidence, bool) {
	evidence := cliProxyAPIOverflowEvidence{
		code:    trimmedStringFromAny(payload["code"]),
		message: trimmedStringFromAny(firstValue(payload, "detail", "message", "error")),
	}
	return evidence, evidence.code != nil || evidence.message != nil
}

func normalizedCLIProxyAPIOverflowMessage(message *string) string {
	if message == nil {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(*message), " "))
}

func cliProxyAPIOverflowMessageSignalsOverflow(message string) bool {
	return containsCLIProxyAPIOverflowFragment(message,
		"maximum context length",
		"max context length",
		"context length",
		"context window",
		"too many tokens",
	) && containsCLIProxyAPIOverflowFragment(message,
		"exceeded",
		"exceeds",
		"too large",
		"too long",
		"over limit",
		"over the limit",
	)
}

func cliProxyAPIOverflowMessageRejected(message string) bool {
	return containsCLIProxyAPIOverflowFragment(message,
		"model_not_found",
		"model not found",
		"unknown model",
		"unknown provider",
		"does not exist",
		"invalid_api_key",
		"invalid api key",
		"incorrect api key",
		"authentication",
		"unauthorized",
		"permission denied",
		"forbidden",
		"insufficient_quota",
		"insufficient quota",
		"quota exceeded",
		"credit balance",
		"balance",
		"billing",
		"hard limit",
		"rate_limit",
		"rate limit",
		"too many requests",
		"tokens per minute",
		"per minute",
		"retry after",
		"server overloaded",
		"overloaded",
		"capacity exceeded",
		"capacity exhausted",
		"temporarily unavailable",
		"try again later",
		"moderation",
		"safety",
	)
}

func containsCLIProxyAPIOverflowFragment(message string, fragments ...string) bool {
	for _, fragment := range fragments {
		if fragment != "" && strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func proxyNonEventResponseAndCaptureWithoutUsage(hooks operationResponseHooks, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	auditBuffer, auditWriter := newBoundedAuditWriter(captureAuditBody)
	writers := []io.Writer{dst}
	if auditWriter != nil {
		writers = append(writers, auditWriter)
	}
	_, err := io.Copy(io.MultiWriter(writers...), src)
	completedAt := now()
	capture := runtimeResponseCapture{CompletedAt: &completedAt, StreamOutcome: runtimeStreamOutcomeNotStreaming}
	if captureAuditBody {
		capture.AuditBody, capture.AuditBodyObserved, capture.AuditBodyStored, capture.AuditBodyTruncated = auditBuffer.snapshot()
	}
	return capture, err
}

func proxyNonEventTokenCountResponseAndCaptureUsage(hooks operationResponseHooks, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if !responseMayContainJSONUsage(contentType) {
		return proxyNonEventResponseAndCaptureWithoutUsage(operationResponseHooks{}, dst, src, contentType, now, captureAuditBody)
	}
	bodyBuffer := &bytes.Buffer{}
	auditBuffer, auditWriter := newBoundedAuditWriter(captureAuditBody)
	writers := []io.Writer{dst, bodyBuffer}
	if auditWriter != nil {
		writers = append(writers, auditWriter)
	}
	_, copyErr := io.Copy(io.MultiWriter(writers...), src)
	completedAt := now()
	usage := extractTokenCountResponseUsageByProvider(hooks, bodyBuffer.Bytes())
	capture := runtimeResponseCapture{
		Body:          buildUsageBodyFromResponseUsage(usage),
		Usage:         usage,
		CompletedAt:   &completedAt,
		StreamOutcome: runtimeStreamOutcomeNotStreaming,
	}
	if captureAuditBody {
		capture.AuditBody, capture.AuditBodyObserved, capture.AuditBodyStored, capture.AuditBodyTruncated = auditBuffer.snapshot()
	}
	return capture, copyErr
}

func extractTokenCountResponseUsageByProvider(hooks operationResponseHooks, body []byte) responseUsage {
	switch hooks.Provider {
	case "anthropic":
		return responseUsageFromProviderUsageEnvelope(anthropicprovider.ExtractTokenCountUsage(body))
	case "gemini":
		return responseUsageFromProviderUsageEnvelope(geminiprovider.ExtractCountTokensUsage(body))
	default:
		return extractTokenCountResponseUsage(body)
	}
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
