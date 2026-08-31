package runtime

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	anthropicprovider "github.com/coachpo/prism/backend/internal/gateway/provider/anthropic"
	geminiprovider "github.com/coachpo/prism/backend/internal/gateway/provider/gemini"
)

func proxyNonEventTokenCountResponseAndCaptureUsage(hooks operationResponseHooks, dst io.Writer, src io.Reader, contentType string, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if !responseMayContainJSONUsage(contentType) {
		return proxyNonEventResponseAndCaptureWithoutUsage(hooks, dst, src, contentType, now, captureAuditBody)
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
		OutputDelivery: classifyOutputDelivery(
			hooks.Kind,
			runtimeStreamOutcomeNotStreaming,
			usage,
			outputDeliveryEvidence{},
		),
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
