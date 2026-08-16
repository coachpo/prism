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
	openAIRequestTranslationUnsupportedErrorCode = "openai_request_translation_unsupported"
	openAIRequestTranslationUnsupportedReason   = "operation_translation_unsupported"
)

const (
	openAIOperationNotSupportedDetail      = "The requested model does not accept this OpenAI operation."
	openAINoCompatibleTerminalTargetDetail = "No configured terminal target can natively serve this OpenAI operation for the requested model."
	openAINoEligibleTerminalTargetDetail   = "No terminal target is currently eligible to serve this OpenAI operation for the requested model."
	openAIRequestTranslationUnsupportedDetail = "Prism cannot translate this OpenAI request shape for the selected target."
)

// resolveTranslationMode reports whether a model and Terminal Target pair can
// natively serve the ingress operation. Prism never translates between wire
// shapes, so the mode is always None and the boolean is the whole answer; the
// per-dimension rules live in the shared capability gate.
func resolveTranslationMode(operation RuntimeOperation, model runtimeOpenAICapabilityDimensions, target runtimeOpenAICapabilityDimensions) (TranslationMode, bool) {
	if !runtimeModelAcceptsOpenAIOperation(operation, model) {
		return TranslationModeNone, false
	}
	return TranslationModeNone, runtimeOpenAICapabilitySatisfied(operation, model, target)
}

func validateOpenAIModelAcceptedFormat(operation RuntimeOperation, requestedModel runtimeModelRecord) error {
	if !providerauth.IsOpenAI(requestedModel.APIFamily) {
		return nil
	}
	if !runtimeOperationIsOpenAICapabilityGated(operation) {
		return nil
	}
	if !runtimeModelAcceptsOpenAIOperation(operation, runtimeModelCapabilityDimensions(requestedModel)) {
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
	case openAIOperationNotSupportedErrorCode, openAINoCompatibleTerminalTargetErrorCode, openAINoEligibleTerminalTargetErrorCode, openAIRequestTranslationUnsupportedErrorCode:
		return domainErr, true
	default:
		return nil, false
	}
}

// The routing planner uses a neutral name for the same typed rejection so
// operation compatibility failures do not imply that any wire translation
// occurred. Keep the error identity shared with the established OpenAI
// planning codes.
func isRequestTranslationUnsupportedError(err error) (*domainError, bool) {
	return isOpenAIPlanningRejectionError(err)
}

func openAIRequestTranslationUnsupportedDomainError() *domainError {
	return &domainError{
		StatusCode: http.StatusBadRequest,
		ErrorCode:  openAIRequestTranslationUnsupportedErrorCode,
		Detail:     openAIRequestTranslationUnsupportedDetail,
		Fields: map[string]any{
			"translation_mode":   string(TranslationModeNone),
			"unsupported_reason": openAIRequestTranslationUnsupportedReason,
		},
	}
}
