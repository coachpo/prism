package configrules

import (
	"net/http"
	"regexp"
	"strings"
)

func normalizeAndValidateUserAgentCreate(requestBody *userAgentClientRuleCreateRequest) error {
	requestBody.Name = strings.TrimSpace(requestBody.Name)
	if requestBody.Name == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "name must not be empty"}
	}
	requestBody.Pattern = strings.TrimSpace(requestBody.Pattern)
	if requestBody.Pattern == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
	}
	if _, err := regexp.Compile(requestBody.Pattern); err != nil {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must be a valid regular expression"}
	}
	return nil
}

func normalizeAndValidateUserAgentUpdate(requestBody *userAgentClientRuleUpdateRequest) error {
	if requestBody.Name.Set && requestBody.Name.Value != nil {
		normalized := strings.TrimSpace(*requestBody.Name.Value)
		requestBody.Name.Value = &normalized
		if normalized == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "name must not be empty"}
		}
	}
	if requestBody.Pattern.Set && requestBody.Pattern.Value != nil {
		normalized := strings.TrimSpace(*requestBody.Pattern.Value)
		requestBody.Pattern.Value = &normalized
		if normalized == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
		}
		if _, err := regexp.Compile(normalized); err != nil {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must be a valid regular expression"}
		}
	}
	return nil
}
