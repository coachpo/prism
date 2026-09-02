package models

import (
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

const (
	openAIAcceptedFormatResponsesOnly       = "responses_only"
	openAIAcceptedFormatChatCompletionsOnly = "chat_completions_only"
	openAIAcceptedFormatDualNative          = "dual_native"
)

func normalizeCreateRequest(requestBody *modelCreateRequest) {
	requestBody.APIFamily = strings.ToLower(strings.TrimSpace(requestBody.APIFamily))
	requestBody.ModelID = strings.TrimSpace(requestBody.ModelID)
	requestBody.DisplayName = normalizeOptionalString(requestBody.DisplayName, false, true)
	requestBody.OpenAIAcceptedFormat = optionalString{Set: requestBody.OpenAIAcceptedFormat.Set, Value: normalizeOptionalString(requestBody.OpenAIAcceptedFormat.Value, true, true)}
	requestBody.OpenAIImageOperations = optionalString{Set: requestBody.OpenAIImageOperations.Set, Value: normalizeOptionalString(requestBody.OpenAIImageOperations.Value, true, true)}
}

func normalizeUpdateRequest(requestBody *modelUpdateRequest) {
	requestBody.APIFamily = optionalString{Set: requestBody.APIFamily.Set, Value: normalizeOptionalString(requestBody.APIFamily.Value, true, false)}
	requestBody.ModelID = optionalString{Set: requestBody.ModelID.Set, Value: normalizeOptionalString(requestBody.ModelID.Value, false, false)}
	requestBody.DisplayName = optionalString{Set: requestBody.DisplayName.Set, Value: normalizeOptionalString(requestBody.DisplayName.Value, false, true)}
	requestBody.OpenAIAcceptedFormat = optionalString{Set: requestBody.OpenAIAcceptedFormat.Set, Value: normalizeOptionalString(requestBody.OpenAIAcceptedFormat.Value, true, true)}
	requestBody.OpenAIImageOperations = optionalString{Set: requestBody.OpenAIImageOperations.Set, Value: normalizeOptionalString(requestBody.OpenAIImageOperations.Value, true, true)}
}

func normalizeAccessTargets(values []modelAccessTargetRequest) []modelAccessTargetRequest {
	if values == nil {
		return nil
	}
	normalized := make([]modelAccessTargetRequest, 0, len(values))
	for _, value := range values {
		normalizedTarget := value
		normalizedTarget.TargetType = modelrouting.NormalizeTargetType(value.TargetType)
		normalizedTarget.TargetModelID = normalizeOptionalString(value.TargetModelID, false, false)
		normalized = append(normalized, normalizedTarget)
	}
	return normalized
}

func validateCreateRequest(requestBody modelCreateRequest) error {
	if requestBody.DirectRequestEnabled.Null || requestBody.DirectRequestEnabled.Invalid {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "direct_request_enabled must be a boolean when provided"}
	}
	if strings.TrimSpace(requestBody.APIFamily) == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family is required"}
	}
	if !isValidAPIFamily(requestBody.APIFamily) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	if strings.TrimSpace(requestBody.ModelID) == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "model_id is required"}
	}
	if requestBody.LoadbalanceStrategyID == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id is required"}
	}
	if err := validateOpenAIDimensionsForModel(requestBody.APIFamily, requestBody.OpenAIAcceptedFormat.Value, requestBody.OpenAIAcceptedFormat.Set, requestBody.OpenAIImageOperations.Value, requestBody.OpenAIImageOperations.Set); err != nil {
		return err
	}
	return nil
}

// validateOpenAIDimensionsForModel enforces the two independent OpenAI
// dimensions. Each one is optional on its own but must be a supported value
// when present, and an OpenAI model that declares neither dimension can serve
// no operation at all. This mirrors ck_model_configs_openai_dimensions.
func validateOpenAIDimensionsForModel(apiFamily string, acceptedFormat *string, acceptedProvided bool, imageOperations *string, imageProvided bool) error {
	if apiFamily != "openai" {
		if acceptedProvided {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "openai_accepted_format is only allowed when api_family is 'openai'"}
		}
		if imageProvided {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "openai_image_operations is only allowed when api_family is 'openai'"}
		}
		return nil
	}
	if acceptedFormat != nil && !isValidOpenAIAcceptedFormat(strings.TrimSpace(*acceptedFormat)) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "openai_accepted_format must be one of 'responses_only', 'chat_completions_only', or 'dual_native'"}
	}
	if imageOperations != nil && !providerauth.IsSupportedOpenAIImageCapability(*imageOperations) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "openai_image_operations must be one of 'generations', 'edits', or 'generations_and_edits'"}
	}
	if acceptedFormat == nil && imageOperations == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "at least one of openai_accepted_format or openai_image_operations is required when api_family is 'openai'"}
	}
	return nil
}

func isValidOpenAIAcceptedFormat(value string) bool {
	switch value {
	case openAIAcceptedFormatResponsesOnly, openAIAcceptedFormatChatCompletionsOnly, openAIAcceptedFormatDualNative:
		return true
	default:
		return false
	}
}

func validateUpdateRequest(requestBody modelUpdateRequest) error {
	if requestBody.DirectRequestEnabled.Null || requestBody.DirectRequestEnabled.Invalid {
		return &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "direct_request_enabled must be a boolean when provided"}
	}
	if requestBody.APIFamily.Set {
		if requestBody.APIFamily.Value == nil || !isValidAPIFamily(*requestBody.APIFamily.Value) {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "api_family must be one of 'openai', 'anthropic', or 'gemini'"}
		}
	}
	if requestBody.ModelID.Set && (requestBody.ModelID.Value == nil || strings.TrimSpace(*requestBody.ModelID.Value) == "") {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "model_id is required"}
	}
	if requestBody.LoadbalanceStrategyID.Set && requestBody.LoadbalanceStrategyID.Value == nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "loadbalance_strategy_id is required"}
	}
	return nil
}
