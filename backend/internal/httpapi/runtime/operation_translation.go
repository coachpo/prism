package runtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/providercompat"
)

type TranslationMode string

const (
	TranslationModeNone                             TranslationMode = providercompat.OpenAITextTranslationModeNone
	TranslationModeOpenAIResponsesToChatCompletions TranslationMode = providercompat.OpenAITextTranslationModeResponsesToChat
	TranslationModeOpenAIChatCompletionsToResponses TranslationMode = providercompat.OpenAITextTranslationModeChatToResponses
)

const (
	openAIRequestTranslationUnsupportedErrorCode        = "openai_request_translation_unsupported"
	openAIRequestTranslationUnsupportedDetail           = "Prism cannot translate this OpenAI request shape for the selected target."
	openAIResponseTranslationUnsupportedErrorCode       = "openai_response_translation_unsupported"
	openAIStreamTranslationUnsupportedErrorCode         = "openai_stream_translation_unsupported"
	openAIStreamTranslationUnsupportedDetail            = "Prism cannot translate this OpenAI stream shape for the selected target."
	openAITranslatedUpstreamResponseReadFailedErrorCode = "openai_translated_upstream_response_read_failed"
	openAITranslatedUpstreamResponseReadFailedDetail    = "Failed to read upstream response"
	openAITranslatedUpstreamResponseReadFailedHint      = "OpenAI text translation was active; verify the selected target openai_text_capability matches the upstream API surface."
)

type openAITranslationCapabilityClass string

const (
	openAITranslationCapabilitySafe   openAITranslationCapabilityClass = "safe"
	openAITranslationCapabilityReject openAITranslationCapabilityClass = "reject"
	openAITranslationCapabilityDefer  openAITranslationCapabilityClass = "defer"
)

type runtimeOpenAITranslationCapability struct {
	Mode              TranslationMode
	RequestClass      openAITranslationCapabilityClass
	ResponseClass     openAITranslationCapabilityClass
	StreamClass       openAITranslationCapabilityClass
	UnsupportedReason string
	HTTPStatus        int
}

func newOpenAITranslationCapability(mode TranslationMode) runtimeOpenAITranslationCapability {
	return runtimeOpenAITranslationCapability{
		Mode:          mode,
		RequestClass:  openAITranslationCapabilitySafe,
		ResponseClass: openAITranslationCapabilitySafe,
		StreamClass:   openAITranslationCapabilitySafe,
		HTTPStatus:    http.StatusBadRequest,
	}
}

func classifyOpenAITranslationCapability(operation RuntimeOperation, rawBody []byte, mode TranslationMode) runtimeOpenAITranslationCapability {
	capability := newOpenAITranslationCapability(mode)
	if mode == TranslationModeNone {
		return capability
	}
	adapter := openai.New()
	providerCapability, err := adapter.ConversionCapability(context.Background(), provider.ConversionRequest{Operation: providerOperationFromRuntime(operation), RawBody: rawBody, Mode: providerTranslationMode(mode), TargetModelID: "translation-preview-model"})
	if err != nil {
		return capability.withRequestRejection("invalid_translation_payload")
	}
	if !providerCapability.RequestSupported {
		return capability.withRequestRejection(providerCapability.UnsupportedReason)
	}
	if !providerCapability.StreamSupported {
		return capability.withStreamRejection(providerCapability.UnsupportedReason)
	}
	if !providerCapability.ResponseSupported {
		capability.ResponseClass = openAITranslationCapabilityReject
		capability.UnsupportedReason = normalizedTranslationUnsupportedReason(providerCapability.UnsupportedReason)
		return capability
	}
	return capability
}

func (capability runtimeOpenAITranslationCapability) supported() bool {
	if capability.RequestClass == openAITranslationCapabilityDefer || capability.ResponseClass == openAITranslationCapabilityDefer || capability.StreamClass == openAITranslationCapabilityDefer {
		return false
	}
	return capability.RequestClass == openAITranslationCapabilitySafe && capability.ResponseClass == openAITranslationCapabilitySafe && capability.StreamClass == openAITranslationCapabilitySafe
}

func (capability runtimeOpenAITranslationCapability) rejection() *domainError {
	if capability.supported() {
		return nil
	}
	reason := strings.TrimSpace(capability.UnsupportedReason)
	if reason == "" {
		reason = "unsupported_request_shape"
	}
	rejection := openAIRequestTranslationUnsupportedDomainError(capability.Mode, reason)
	if capability.HTTPStatus > 0 {
		rejection.StatusCode = capability.HTTPStatus
	}
	return rejection
}

func (capability runtimeOpenAITranslationCapability) withRequestRejection(reason string) runtimeOpenAITranslationCapability {
	capability.RequestClass = openAITranslationCapabilityReject
	capability.UnsupportedReason = normalizedTranslationUnsupportedReason(reason)
	return capability
}

func (capability runtimeOpenAITranslationCapability) withStreamRejection(reason string) runtimeOpenAITranslationCapability {
	capability.StreamClass = openAITranslationCapabilityReject
	capability.UnsupportedReason = normalizedTranslationUnsupportedReason(reason)
	return capability
}

func normalizedTranslationUnsupportedReason(reason string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return "unsupported_request_shape"
}

func resolveTranslationMode(operation RuntimeOperation, openAIAcceptedFormat *string, openAITextCapability *string) (TranslationMode, bool) {
	if openAIAcceptedFormat == nil || openAITextCapability == nil {
		return TranslationModeNone, false
	}
	switch providercompat.OpenAITextWireCompatibility(operation.Name, *openAIAcceptedFormat, *openAITextCapability) {
	case providercompat.OpenAIWireCompatibilityNative:
		return TranslationModeNone, true
	case providercompat.OpenAIWireCompatibilityTranslateToChat:
		return TranslationModeOpenAIResponsesToChatCompletions, true
	case providercompat.OpenAIWireCompatibilityTranslateToResponses:
		return TranslationModeOpenAIChatCompletionsToResponses, true
	default:
		return TranslationModeNone, false
	}
}

func isRequestTranslationUnsupportedError(err error) (*domainError, bool) {
	return translationUnsupportedDomainError(err, openAIRequestTranslationUnsupportedErrorCode)
}

func translationUnsupportedDomainError(err error, errorCode string) (*domainError, bool) {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil {
		return nil, false
	}
	if strings.TrimSpace(domainErr.ErrorCode) != errorCode {
		return nil, false
	}
	return domainErr, true
}

func translationUnsupportedReasonFromError(err error, errorCode string, fallback string) string {
	if domainErr, ok := translationUnsupportedDomainError(err, errorCode); ok && domainErr != nil {
		if reason := stringValue(domainErr.Fields["unsupported_reason"]); strings.TrimSpace(reason) != "" {
			return reason
		}
	}
	return normalizedTranslationUnsupportedReason(fallback)
}

func openAIRequestTranslationUnsupportedDomainError(mode TranslationMode, reason string) *domainError {
	return openAITranslationUnsupportedDomainError(http.StatusBadRequest, openAIRequestTranslationUnsupportedErrorCode, openAIRequestTranslationUnsupportedDetail, mode, reason)
}

func openAITranslatedUpstreamResponseReadFailedDomainError(operation RuntimeOperation, mode TranslationMode) *domainError {
	fields := map[string]any{
		"operation_translation_mode": string(normalizedRuntimeTranslationMode(mode)),
		"upstream_operation_name":    runtimeUpstreamOperationName(operation, mode),
		"upstream_request_path":      runtimeUpstreamRequestPathTemplate(operation, mode),
		"diagnostic_hint":            openAITranslatedUpstreamResponseReadFailedHint,
	}
	return &domainError{
		StatusCode: http.StatusBadGateway,
		ErrorCode:  openAITranslatedUpstreamResponseReadFailedErrorCode,
		Detail:     openAITranslatedUpstreamResponseReadFailedDetail,
		Fields:     fields,
	}
}

func openAIStreamTranslationUnsupportedDomainError(mode TranslationMode, reason string) *domainError {
	return openAITranslationUnsupportedDomainError(http.StatusBadGateway, openAIStreamTranslationUnsupportedErrorCode, openAIStreamTranslationUnsupportedDetail, mode, reason)
}

func openAITranslationUnsupportedDomainError(statusCode int, errorCode string, detail string, mode TranslationMode, reason string) *domainError {
	fields := map[string]any{}
	if strings.TrimSpace(string(mode)) != "" {
		fields["translation_mode"] = string(mode)
	}
	if strings.TrimSpace(reason) != "" {
		fields["unsupported_reason"] = strings.TrimSpace(reason)
	}
	return &domainError{
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Detail:     detail,
		Fields:     fields,
	}
}

func unsupportedTranslationModeError(mode TranslationMode) error {
	return fmt.Errorf("unsupported translation mode %q", mode)
}
