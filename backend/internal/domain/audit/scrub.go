package audit

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// Header scrub contract (Requests/Audit SPEC: headers are fixed-scrubbed
// before the ordinary backend outbox; the browser reuses the same matcher for
// legacy/deep-defense). Sensitive header NAMES and sensitive VALUE patterns
// (Bearer/Basic/JWT/token/secret/credential/password/private key) are masked
// with a fixed sentinel. The matcher is deliberately name+value dual-layer:
// name hits mask unconditionally; otherwise the value is pattern-scanned.

const scrubSentinel = "[REDACTED]"

// sensitiveHeaderNames is the exact name set shared with the frontend matcher.
var sensitiveHeaderNames = map[string]struct{}{
	"authorization":          {},
	"proxy-authorization":    {},
	"cookie":                 {},
	"set-cookie":             {},
	"x-api-key":              {},
	"x-goog-api-key":         {},
	"x-amz-security-token":   {},
	"x-auth-token":           {},
	"www-authenticate":       {},
	"proxy-authenticate":     {},
	"api-key":                {},
	"private-key":            {},
	"x-azure-key":            {},
	"x-rapidapi-key":         {},
	"x-rbl-key":              {},
	"x-ms-secret":            {},
	"x-client-secret":        {},
	"x-vercel-secret":        {},
	"x-vercel-token":         {},
	"x-vercel-api-key":       {},
	"x-token":                {},
	"access-token":           {},
	"refresh-token":          {},
	"id-token":               {},
	"session-token":          {},
	"client-secret":          {},
	"client-id-secret":       {},
	"slack-signing-secret":   {},
	"stripe-signature":       {},
	"x-paypal-client-secret": {},
}

var sensitiveNameParts = []string{
	"api-key",
	"apikey",
	"token",
	"secret",
	"credential",
	"password",
	"private-key",
	"signature",
}

// valuePatterns scan header values for credential-shaped content that would
// survive a non-sensitive header name (e.g. X-Trace with an embedded token).
var valuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{6,}`),
	regexp.MustCompile(`(?i)\bBasic\s+[A-Za-z0-9+/=]{8,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), // JWT
	regexp.MustCompile(`(?i)(?:password|passwd|pwd|secret|api[_-]?key|token|credential|private[_-]?key|access[_-]?key)\s*[=:]\s*["']?[^\s"',;]+`),
	regexp.MustCompile(`(?i)\b(?:sk|pk|rk|AKIA)[A-Za-z0-9]{16,}\b`), // sk-… / AWS access key shapes
	regexp.MustCompile(`(?i)\b(?:sk|pk|rk)-[A-Za-z0-9_-]{12,}\b`),   // sk-proj-… provider keys
}

// HeaderNameSensitive reports whether a header name must be masked.
func HeaderNameSensitive(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	if _, exact := sensitiveHeaderNames[normalized]; exact {
		return true
	}
	for _, part := range sensitiveNameParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

// HeaderValueSensitive reports whether a header value contains credential
// patterns (used only when the name itself is not sensitive).
func HeaderValueSensitive(value string) bool {
	for _, pattern := range valuePatterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}

// ScrubHeaderValue masks sensitive names unconditionally and scans non
// sensitive names for credential value patterns.
func ScrubHeaderValue(name string, value string) string {
	if HeaderNameSensitive(name) {
		return scrubSentinel
	}
	if HeaderValueSensitive(value) {
		return scrubSentinel
	}
	return value
}

// ScrubHeaderMap returns a new map with every value scrubbed (map form used
// by the request-side telemetry and audit rows).
func ScrubHeaderMap(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return headers
	}
	scrubbed := make(map[string]string, len(headers))
	for key, value := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		scrubbed[normalizedKey] = ScrubHeaderValue(normalizedKey, value)
	}
	return scrubbed
}

// ScrubHTTPHeaderMap flattens an http.Header into a scrubbed string-keyed map
// (deterministic join of multi-values; used by response-side audit rows).
func ScrubHTTPHeaderMap(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return map[string]string{}
	}
	flattened := make(map[string]string, len(headers))
	for key, values := range headers {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" {
			continue
		}
		flattened[normalizedKey] = strings.Join(values, ", ")
	}
	return ScrubHeaderMap(flattened)
}

// MarshalScrubbedHeaders serializes a scrubbed header map to JSON ({} when
// empty); used by audit request-side rows.
func MarshalScrubbedHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(ScrubHeaderMap(headers))
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// MarshalScrubbedHTTPHeaders serializes an http.Header to a scrubbed JSON
// string pointer (nil when empty); used by audit response-side rows.
func MarshalScrubbedHTTPHeaders(headers http.Header) *string {
	if len(headers) == 0 {
		return nil
	}
	encoded, err := json.Marshal(ScrubHTTPHeaderMap(headers))
	if err != nil {
		return nil
	}
	resolved := string(encoded)
	return &resolved
}
