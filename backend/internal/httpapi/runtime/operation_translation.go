package runtime

import (
	"errors"
	"net/http"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

type TranslationMode string

const TranslationModeNone TranslationMode = providerauth.OpenAITextTranslationModeNone

const (
	openAIRequestTranslationUnsupportedErrorCode = "openai_request_translation_unsupported"
	openAIRequestTranslationUnsupportedDetail    = "Prism cannot translate this OpenAI request shape for the selected target."
	openAIRequestTranslationUnsupportedReason    = "operation_translation_unsupported"
)

func resolveTranslationMode(operation RuntimeOperation, openAIAcceptedFormat *string, openAITextCapability *string) (TranslationMode, bool) {
	if openAIAcceptedFormat == nil || openAITextCapability == nil {
		return TranslationModeNone, false
	}
	// Strict mode equality: dual_native, chat_completions_only, and responses_only
	// may connect only to the identical mode. Wire-compatible but mode-different
	// targets (for example dual_native source with a single-mode connection) are
	// no longer eligible, so Chat Completions/Responses conversion is impossible.
	return TranslationModeNone, providerauth.OpenAITextModesEqual(*openAIAcceptedFormat, *openAITextCapability)
}

func validateOpenAIModelAcceptedFormat(operation RuntimeOperation, requestedModel runtimeModelRecord) error {
	if !providerauth.IsOpenAI(requestedModel.APIFamily) {
		return nil
	}
	if _, ok := providerauth.OpenAICallerWireFormat(operation.Name); !ok {
		return nil
	}
	if requestedModel.OpenAIAcceptedFormat == nil || !providerauth.OpenAITextCapabilitySupportsNativeOperation(*requestedModel.OpenAIAcceptedFormat, operation.Name) {
		return openAIRequestTranslationUnsupportedDomainError()
	}
	return nil
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

func isRequestTranslationUnsupportedError(err error) (*domainError, bool) {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil || domainErr.ErrorCode != openAIRequestTranslationUnsupportedErrorCode {
		return nil, false
	}
	return domainErr, true
}
