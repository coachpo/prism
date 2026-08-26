package endpointdomain

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	// MaxEndpointNameCodePoints is the create/update contract for endpoint names
	// after trimming surrounding whitespace (counted in Unicode code points).
	MaxEndpointNameCodePoints = 128
	// MaxEndpointBaseURLCodePoints is the create/update contract for normalized
	// base URLs (counted in Unicode code points).
	MaxEndpointBaseURLCodePoints = 512
)

// Stable field error codes returned by validation helpers. The management layer
// maps them into typed 422 `fields` payloads; the frontend owns zh-CN copy.
const (
	FieldErrorNameRequired   = "name_required"
	FieldErrorNameTooLong    = "name_too_long"
	FieldErrorBaseURLInvalid = "base_url_invalid"
	FieldErrorBaseURLTooLong = "base_url_too_long"
)

// NormalizeBaseURL applies the §5.2 normalization order: trim surrounding
// whitespace, then remove trailing '/' characters while preserving a valid
// origin form. Values that fail to parse after slash removal keep their
// whitespace-trimmed form so validation can report the original problem.
func NormalizeBaseURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	withoutSlash := strings.TrimRight(trimmed, "/")
	parsed, err := url.Parse(withoutSlash)
	if err == nil && strings.TrimSpace(parsed.Scheme) != "" && strings.TrimSpace(parsed.Host) != "" {
		return withoutSlash
	}
	return trimmed
}

// ValidateBaseURL returns stable error codes for a normalized base URL.
// It requires an http/https scheme and a host, rejects query/fragment, and
// enforces the normalized 512 code-point limit.
func ValidateBaseURL(baseURL string) []string {
	var codes []string
	parsed, err := url.Parse(baseURL)
	scheme := ""
	host := ""
	if err == nil {
		scheme = strings.TrimSpace(parsed.Scheme)
		host = strings.TrimSpace(parsed.Host)
	}
	if scheme == "" || host == "" || (scheme != "http" && scheme != "https") {
		codes = append(codes, FieldErrorBaseURLInvalid)
	}
	if parsed != nil && (parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil) {
		codes = append(codes, FieldErrorBaseURLInvalid)
	}
	if utf8.RuneCountInString(baseURL) > MaxEndpointBaseURLCodePoints {
		codes = append(codes, FieldErrorBaseURLTooLong)
	}
	return codes
}

// ValidateEndpointName returns a stable error code for a trimmed endpoint name,
// or "" when valid. Names are required and limited to 128 Unicode code points.
func ValidateEndpointName(name string) string {
	if name == "" {
		return FieldErrorNameRequired
	}
	if utf8.RuneCountInString(name) > MaxEndpointNameCodePoints {
		return FieldErrorNameTooLong
	}
	return ""
}

// BuildDuplicateEndpointName derives the next free "<name> copy [N]" name.
func BuildDuplicateEndpointName(sourceName string, existingNames map[string]struct{}) string {
	baseName := strings.TrimSpace(sourceName) + " copy"
	if _, exists := existingNames[baseName]; !exists {
		return baseName
	}

	suffix := 2
	for {
		candidate := fmt.Sprintf("%s %d", baseName, suffix)
		if _, exists := existingNames[candidate]; !exists {
			return candidate
		}
		suffix++
	}
}
