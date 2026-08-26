package connections

import (
	"fmt"
	"net/http"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

func validateLimiter(fieldName string, value *int) error {
	if value != nil && *value < 1 {
		return &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("%s must be >= 1 when provided", fieldName)}
	}
	return nil
}

func validateAPIFamily(value string, required bool) (string, error) {
	normalized := providerauth.NormalizeAPIFamily(value)
	if normalized == "" {
		if required {
			return "", &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family is required"}
		}
		return "", nil
	}
	if !providerauth.IsSupportedAPIFamily(normalized) {
		return "", &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family must be one of 'openai', 'anthropic', or 'gemini'"}
	}
	return normalized, nil
}

func validateOwnerScopedAPIFamily(value string, ownerAPIFamily string) error {
	apiFamily, err := validateAPIFamily(value, false)
	if err != nil {
		return err
	}
	if apiFamily != "" && !providerauth.SameAPIFamily(apiFamily, ownerAPIFamily) {
		return &DomainError{StatusCode: http.StatusBadRequest, Detail: "api_family must match owner model api_family"}
	}
	return nil
}

func validateAuthType(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized := providerauth.NormalizeAPIFamily(*value)
	if normalized == "" || !providerauth.IsSupportedAuthType(normalized) {
		return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "auth_type must be one of 'openai', 'anthropic', 'gemini', or 'gemini_api_key'"}
	}
	return &normalized, nil
}

func normalizeHeaders(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	return value
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func resolvedBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
