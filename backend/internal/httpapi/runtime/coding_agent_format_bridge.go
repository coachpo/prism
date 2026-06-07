package runtime

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

type CodingAgentFormatBridge struct {
	mode config.OpenAITerminalTranslationMode
}

type codingAgentFormatBridgePlan struct {
	TranslationMode     TranslationMode
	UpstreamRequestPath string
	UpstreamBody        []byte
}

func NewCodingAgentFormatBridge(mode config.OpenAITerminalTranslationMode) CodingAgentFormatBridge {
	if strings.TrimSpace(string(mode)) == "" {
		mode = config.OpenAITerminalTranslationModeSafeOnly
	}
	return CodingAgentFormatBridge{mode: mode}
}

func defaultCodingAgentFormatBridge() CodingAgentFormatBridge {
	return NewCodingAgentFormatBridge(config.OpenAITerminalTranslationModeSafeOnly)
}

func (bridge CodingAgentFormatBridge) PlanRequest(operation RuntimeOperation, rawBody []byte, targetModelID string, connection runtimeConnection) (codingAgentFormatBridgePlan, bool, error) {
	if bridge.mode == config.OpenAITerminalTranslationModeOff {
		return codingAgentFormatBridgePlan{}, false, nil
	}
	mode := resolveTranslationMode(operation, connection.OpenAIUpstreamOperation, connection.OpenAIProbeEndpointVariant)
	if mode == TranslationModeNone {
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
		CompletedAt:   &completedAt,
		StreamOutcome: runtimeStreamOutcomeNotStreaming,
	}
	if captureAuditBody {
		capture.AuditBody = append([]byte(nil), rawBody...)
	}
	return capture, nil
}

func (bridge CodingAgentFormatBridge) ProxyEventStreamAndCaptureCompletedResponse(operation RuntimeOperation, mode TranslationMode, requestedModelID string, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	return proxyEventStreamAndCaptureCompletedResponseByOperation(operation, mode, requestedModelID, ctx, dst, src, now, captureAuditBody)
}
