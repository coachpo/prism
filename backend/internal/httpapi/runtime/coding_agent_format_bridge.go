package runtime

import (
	"context"
	"io"
	"time"
)

type CodingAgentFormatBridge struct{}

type codingAgentFormatBridgePlan struct {
	TranslationMode     TranslationMode
	UpstreamRequestPath string
	UpstreamBody        []byte
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
	mode, supported := resolveTranslationMode(operation, connection.OpenAITextCapability)
	if !supported || mode == TranslationModeNone {
		return codingAgentFormatBridgePlan{}, false, nil
	}
	capability := classifyOpenAITranslationCapability(operation, rawBody, mode)
	if rejection := capability.rejection(); rejection != nil {
		return codingAgentFormatBridgePlan{}, true, rejection
	}
	path, body, err := bridge.TranslateRequest(rawBody, mode, targetModelID)
	if err != nil {
		return codingAgentFormatBridgePlan{}, true, err
	}
	return codingAgentFormatBridgePlan{TranslationMode: mode, UpstreamRequestPath: path, UpstreamBody: body}, true, nil
}

func (bridge CodingAgentFormatBridge) TranslateRequest(rawBody []byte, mode TranslationMode, targetModelID string) (string, []byte, error) {
	return translateOpenAIRequest(rawBody, mode, targetModelID)
}

func (bridge CodingAgentFormatBridge) TranslateResponse(rawBody []byte, mode TranslationMode, requestedModelID string) ([]byte, responseUsage, runtimeUsageNormalizationRule, error) {
	return translateOpenAIResponse(rawBody, mode, requestedModelID)
}

func (bridge CodingAgentFormatBridge) ProxyNonEventResponseAndCapture(mode TranslationMode, requestedModelID string, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	rawBody, err := readBoundedResponseBody(src, openAITranslatedNonStreamResponseBodyLimit)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	translatedBody, usage, usageRule, err := bridge.TranslateResponse(rawBody, mode, requestedModelID)
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
	return bridge.ProxyNonEventResponseAndCapture(metadata.TranslationMode, metadata.RequestedModelID, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponse(operation RuntimeOperation, mode TranslationMode, requestedModelID string, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return proxyEventStreamAndCaptureCompletedResponseByOperation(operation, mode, requestedModelID, ctx, dst, src, now, captureAuditBody)
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponseForFinalAttempt(operation RuntimeOperation, metadata runtimeFinalResponseTranslationMetadata, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return bridge.ProxyEventStreamAndCaptureCompletedResponse(operation, metadata.TranslationMode, metadata.RequestedModelID, ctx, dst, src, now, captureAuditBody)
}
