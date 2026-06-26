package runtime

import (
	"context"
	"io"
	"time"

	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/providercompat"
)

type codingAgentFormatPlan struct {
	TranslationMode     TranslationMode
	UpstreamRequestPath string
	UpstreamBody        []byte
	ToolContext         *openai.ToolContext
	TranslationLoss     *runtimeTranslationLossDecision
}

func planCodingAgentFormatRequest(operation RuntimeOperation, rawBody []byte, targetModelID string, connection runtimeConnection) (codingAgentFormatPlan, bool, error) {
	acceptedFormat := providercompat.OpenAITextCapabilityDualNative
	mode, supported := resolveTranslationMode(operation, &acceptedFormat, connection.OpenAITextCapability)
	if !supported || mode == TranslationModeNone {
		return codingAgentFormatPlan{}, false, nil
	}
	capability := classifyOpenAITranslationCapability(operation, rawBody, mode)
	if rejection := capability.rejection(); rejection != nil {
		return codingAgentFormatPlan{}, true, rejection
	}
	path, body, loss, err := translateOpenAIRequestWithLoss(rawBody, mode, targetModelID)
	if err != nil {
		return codingAgentFormatPlan{}, true, err
	}
	return codingAgentFormatPlan{TranslationMode: mode, UpstreamRequestPath: path, UpstreamBody: body, ToolContext: responseToolContext(rawBody, mode), TranslationLoss: loss}, true, nil
}

func responseToolContext(rawRequestBody []byte, mode TranslationMode) *openai.ToolContext {
	if mode != TranslationModeOpenAIResponsesToChatCompletions {
		return nil
	}
	return openai.BuildToolContextFromResponsesRawBody(rawRequestBody)
}

func proxyNonEventResponseAndCaptureWithToolContext(mode TranslationMode, requestedModelID string, toolContext *openai.ToolContext, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	rawBody, err := readBoundedResponseBody(src, openAITranslatedNonStreamResponseBodyLimit)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	translatedBody, usage, usageRule, err := translateOpenAIResponseWithToolContext(rawBody, mode, requestedModelID, toolContext)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	if _, err := dst.Write(translatedBody); err != nil {
		return runtimeResponseCapture{}, err
	}
	completedAt := now()
	capture := runtimeResponseCapture{
		Body:          append([]byte(nil), rawBody...),
		Usage:         usage,
		UsageRule:     usageRule,
		UsageSource:   runtimeUsageSourceFromUsage(usage, runtimeStreamOutcomeNotStreaming),
		CompletedAt:   &completedAt,
		StreamOutcome: runtimeStreamOutcomeNotStreaming,
	}
	if captureAuditBody {
		capture.AuditBody = append([]byte(nil), rawBody...)
	}
	return capture, nil
}

func proxyNonEventResponseAndCaptureForFinalAttemptWithRequestBody(metadata runtimeFinalResponseTranslationMetadata, rawRequestBody []byte, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	translationMode, err := runtimeTranslationModeForFinalResponseDirection(metadata.ResponseTranslationDirection)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	return proxyNonEventResponseAndCaptureWithToolContext(translationMode, metadata.RequestedModelID, responseToolContext(rawRequestBody, translationMode), dst, src, now, captureAuditBody)
}

func proxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBody(operation RuntimeOperation, metadata runtimeFinalResponseTranslationMetadata, rawRequestBody []byte, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return proxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBodies(operation, metadata, rawRequestBody, nil, ctx, dst, src, now, captureAuditBody)
}

func proxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBodies(operation RuntimeOperation, metadata runtimeFinalResponseTranslationMetadata, rawRequestBody []byte, upstreamRequestBody []byte, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	translationMode, err := runtimeTranslationModeForFinalResponseDirection(metadata.ResponseTranslationDirection)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	toolContext := responseToolContext(rawRequestBody, translationMode)
	if toolContext == nil || len(toolContext.ChatTools()) == 0 {
		toolContext = responseToolContext(upstreamRequestBody, translationMode)
	}
	return proxyEventStreamAndCaptureCompletedResponseByOperationWithToolContext(operation, translationMode, metadata.RequestedModelID, toolContext, ctx, dst, src, now, captureAuditBody)
}
