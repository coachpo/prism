package runtime

import (
	"context"
	"io"
	"time"

	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/providercompat"
)

type CodingAgentFormatBridge struct{}

type codingAgentFormatBridgePlan struct {
	TranslationMode     TranslationMode
	UpstreamRequestPath string
	UpstreamBody        []byte
	ToolContext         *openai.ToolContext
	TranslationLoss     *runtimeTranslationLossDecision
}

func NewCodingAgentFormatBridge() CodingAgentFormatBridge {
	return CodingAgentFormatBridge{}
}

func defaultCodingAgentFormatBridge() CodingAgentFormatBridge {
	return NewCodingAgentFormatBridge()
}

func (s *Service) codingAgentFormatBridge() CodingAgentFormatBridge {
	return defaultCodingAgentFormatBridge()
}

func (bridge CodingAgentFormatBridge) PlanRequest(operation RuntimeOperation, rawBody []byte, targetModelID string, connection runtimeConnection) (codingAgentFormatBridgePlan, bool, error) {
	acceptedFormat := providercompat.OpenAITextCapabilityDualNative
	mode, supported := resolveTranslationMode(operation, &acceptedFormat, connection.OpenAITextCapability)
	if !supported || mode == TranslationModeNone {
		return codingAgentFormatBridgePlan{}, false, nil
	}
	capability := classifyOpenAITranslationCapability(operation, rawBody, mode)
	if rejection := capability.rejection(); rejection != nil {
		return codingAgentFormatBridgePlan{}, true, rejection
	}
	path, body, loss, err := bridge.TranslateRequestWithLoss(rawBody, mode, targetModelID)
	if err != nil {
		return codingAgentFormatBridgePlan{}, true, err
	}
	return codingAgentFormatBridgePlan{TranslationMode: mode, UpstreamRequestPath: path, UpstreamBody: body, ToolContext: bridge.ResponseToolContext(rawBody, mode), TranslationLoss: loss}, true, nil
}

func (bridge CodingAgentFormatBridge) TranslateRequest(rawBody []byte, mode TranslationMode, targetModelID string) (string, []byte, error) {
	return translateOpenAIRequest(rawBody, mode, targetModelID)
}

func (bridge CodingAgentFormatBridge) TranslateRequestWithLoss(rawBody []byte, mode TranslationMode, targetModelID string) (string, []byte, *runtimeTranslationLossDecision, error) {
	return translateOpenAIRequestWithLoss(rawBody, mode, targetModelID)
}

func (bridge CodingAgentFormatBridge) TranslateResponse(rawBody []byte, mode TranslationMode, requestedModelID string) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	return translateOpenAIResponse(rawBody, mode, requestedModelID)
}

func (bridge CodingAgentFormatBridge) TranslateResponseWithToolContext(rawBody []byte, mode TranslationMode, requestedModelID string, toolContext *openai.ToolContext) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	return translateOpenAIResponseWithToolContext(rawBody, mode, requestedModelID, toolContext)
}

func (bridge CodingAgentFormatBridge) ResponseToolContext(rawRequestBody []byte, mode TranslationMode) *openai.ToolContext {
	if mode != TranslationModeOpenAIResponsesToChatCompletions {
		return nil
	}
	return openai.BuildToolContextFromResponsesRawBody(rawRequestBody)
}

func (bridge CodingAgentFormatBridge) ProxyNonEventResponseAndCapture(mode TranslationMode, requestedModelID string, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return bridge.ProxyNonEventResponseAndCaptureWithToolContext(mode, requestedModelID, nil, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyNonEventResponseAndCaptureWithToolContext(mode TranslationMode, requestedModelID string, toolContext *openai.ToolContext, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	rawBody, err := readBoundedResponseBody(src, openAITranslatedNonStreamResponseBodyLimit)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	translatedBody, usage, usageRule, err := bridge.TranslateResponseWithToolContext(rawBody, mode, requestedModelID, toolContext)
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

func (bridge CodingAgentFormatBridge) ProxyNonEventResponseAndCaptureForFinalAttempt(metadata runtimeFinalResponseTranslationMetadata, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return bridge.ProxyNonEventResponseAndCaptureForFinalAttemptWithRequestBody(metadata, nil, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyNonEventResponseAndCaptureForFinalAttemptWithRequestBody(metadata runtimeFinalResponseTranslationMetadata, rawRequestBody []byte, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	translationMode, err := runtimeTranslationModeForFinalResponseDirection(metadata.ResponseTranslationDirection)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	return bridge.ProxyNonEventResponseAndCaptureWithToolContext(translationMode, metadata.RequestedModelID, bridge.ResponseToolContext(rawRequestBody, translationMode), dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponse(operation RuntimeOperation, mode TranslationMode, requestedModelID string, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return proxyEventStreamAndCaptureCompletedResponseByOperation(operation, mode, requestedModelID, ctx, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponseWithToolContext(operation RuntimeOperation, mode TranslationMode, requestedModelID string, toolContext *openai.ToolContext, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return proxyEventStreamAndCaptureCompletedResponseByOperationWithToolContext(operation, mode, requestedModelID, toolContext, ctx, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponseForFinalAttempt(operation RuntimeOperation, metadata runtimeFinalResponseTranslationMetadata, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return bridge.ProxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBody(operation, metadata, nil, ctx, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBody(operation RuntimeOperation, metadata runtimeFinalResponseTranslationMetadata, rawRequestBody []byte, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return bridge.ProxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBodies(operation, metadata, rawRequestBody, nil, ctx, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponseForFinalAttemptWithRequestBodies(operation RuntimeOperation, metadata runtimeFinalResponseTranslationMetadata, rawRequestBody []byte, upstreamRequestBody []byte, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	translationMode, err := runtimeTranslationModeForFinalResponseDirection(metadata.ResponseTranslationDirection)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	toolContext := bridge.ResponseToolContext(rawRequestBody, translationMode)
	if toolContext == nil || len(toolContext.ChatTools()) == 0 {
		toolContext = bridge.ResponseToolContext(upstreamRequestBody, translationMode)
	}
	return bridge.ProxyEventStreamAndCaptureCompletedResponseWithToolContext(operation, translationMode, metadata.RequestedModelID, toolContext, ctx, dst, src, now, captureAuditBody)
}
