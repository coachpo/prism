package runtime

import (
	"errors"
	"net/http"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

type TranslationMode string

const TranslationModeNone TranslationMode = providerauth.OpenAITextTranslationModeNone

// Stable OpenAI text planning rejection codes. These three codes are
// mutually distinguishable and are rejected before provider transport,
// provider attempt, reservation, loadbalance event, usage/billing and audit
// body capture side effects. Dynamic unavailability (Ban/admission/transport)
// keeps the ordinary 503 / admission_exhausted error family instead.
const (
	openAIOperationNotSupportedErrorCode      = "openai_operation_not_supported"
	openAINoCompatibleTerminalTargetErrorCode = "openai_no_compatible_terminal_target"
	openAINoEligibleTerminalTargetErrorCode   = "openai_no_eligible_terminal_target"
)

const (
	openAIOperationNotSupportedDetail      = "The requested model does not accept this OpenAI operation."
	openAINoCompatibleTerminalTargetDetail = "No configured terminal target can natively serve this OpenAI operation for the requested model."
	openAINoEligibleTerminalTargetDetail   = "No terminal target is currently eligible to serve this OpenAI operation for the requested model."
)

func resolveTranslationMode(operation RuntimeOperation, openAIAcceptedFormat *string, openAITextCapability *string) (TranslationMode, bool) {
	if openAIAcceptedFormat == nil || openAITextCapability == nil {
		return TranslationModeNone, false
	}
	// Native compatibility is directional at operation level. A dual-native
	// model may use a single-operation target for the operation both sides
	// support; Chat Completions and Responses are never translated.
	accepted := providerauth.OpenAITextCapabilitySupportsNativeOperation(*openAIAcceptedFormat, operation.Name)
	supported := providerauth.OpenAITextCapabilitySupportsNativeOperation(*openAITextCapability, operation.Name)
	return TranslationModeNone, accepted && supported
}

func validateOpenAIModelAcceptedFormat(operation RuntimeOperation, requestedModel runtimeModelRecord) error {
	if !providerauth.IsOpenAI(requestedModel.APIFamily) {
		return nil
	}
	if _, ok := providerauth.OpenAICallerWireFormat(operation.Name); !ok {
		return nil
	}
	if requestedModel.OpenAIAcceptedFormat == nil || !providerauth.OpenAITextCapabilitySupportsNativeOperation(*requestedModel.OpenAIAcceptedFormat, operation.Name) {
		return openAIOperationNotSupportedDomainError()
	}
	return nil
}

func openAIOperationNotSupportedDomainError() *domainError {
	return &domainError{
		StatusCode: http.StatusBadRequest,
		ErrorCode:  openAIOperationNotSupportedErrorCode,
		Detail:     openAIOperationNotSupportedDetail,
		Fields: map[string]any{
			"translation_mode": string(TranslationModeNone),
		},
	}
}

func openAINoCompatibleTerminalTargetDomainError() *domainError {
	return &domainError{
		StatusCode: http.StatusServiceUnavailable,
		ErrorCode:  openAINoCompatibleTerminalTargetErrorCode,
		Detail:     openAINoCompatibleTerminalTargetDetail,
	}
}

func openAINoEligibleTerminalTargetDomainError() *domainError {
	return &domainError{
		StatusCode: http.StatusServiceUnavailable,
		ErrorCode:  openAINoEligibleTerminalTargetErrorCode,
		Detail:     openAINoEligibleTerminalTargetDetail,
	}
}

func isOpenAIPlanningRejectionError(err error) (*domainError, bool) {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil {
		return nil, false
	}
	switch domainErr.ErrorCode {
	case openAIOperationNotSupportedErrorCode, openAINoCompatibleTerminalTargetErrorCode, openAINoEligibleTerminalTargetErrorCode:
		return domainErr, true
	default:
		return nil, false
	}
}
