package configrules

import (
	"net/http"
	"regexp"
	"strings"
)

var headerTokenRE = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)

func normalizeAndValidateHeaderBlocklistCreate(requestBody *headerBlocklistRuleCreateRequest) error {
	return normalizeAndValidateHeaderRuleShape(&requestBody.MatchType, &requestBody.Pattern)
}

func normalizeAndValidateHeaderBlocklistUpdate(requestBody *headerBlocklistRuleUpdateRequest) error {
	if requestBody.MatchType.Set && requestBody.MatchType.Value != nil {
		normalized := strings.ToLower(strings.TrimSpace(*requestBody.MatchType.Value))
		requestBody.MatchType.Value = &normalized
		if normalized != "exact" && normalized != "prefix" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "match_type must be 'exact' or 'prefix'"}
		}
	}
	if requestBody.Pattern.Set && requestBody.Pattern.Value != nil {
		normalized := strings.ToLower(strings.TrimSpace(*requestBody.Pattern.Value))
		requestBody.Pattern.Value = &normalized
		if normalized == "" {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
		}
		if !headerTokenRE.MatchString(normalized) {
			return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must contain only lowercase alphanumeric characters and hyphens, and must start with an alphanumeric character"}
		}
	}
	return nil
}

func normalizeAndValidateHeaderRuleShape(matchType *string, pattern *string) error {
	*matchType = strings.ToLower(strings.TrimSpace(*matchType))
	if *matchType != "exact" && *matchType != "prefix" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "match_type must be 'exact' or 'prefix'"}
	}
	*pattern = strings.ToLower(strings.TrimSpace(*pattern))
	if *pattern == "" {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must not be empty"}
	}
	if !headerTokenRE.MatchString(*pattern) {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "pattern must contain only lowercase alphanumeric characters and hyphens, and must start with an alphanumeric character"}
	}
	if *matchType == "prefix" && !strings.HasSuffix(*pattern, "-") {
		return &domainError{StatusCode: http.StatusBadRequest, Detail: "prefix pattern must end with '-'"}
	}
	return nil
}
