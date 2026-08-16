package connections

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

// redactCustomHeaders masks values whose header name matches the fixed
// safediag sensitive-name bottom line, and returns the sorted list of masked
// names so the UI can render them as write-only fields.
func redactCustomHeaders(headers map[string]string) (map[string]string, []string) {
	return safediag.RedactSensitiveHeaderValues(headers)
}

// CustomHeaderRedactedValue is the sentinel management read APIs return in
// place of stored sensitive custom header values.
const CustomHeaderRedactedValue = safediag.CustomHeaderRedactedValue

// resolveCustomHeadersWrite substitutes the redaction sentinel back with the
// stored value. A sentinel for a header that does not exist yet is rejected:
// it can only come from a stale or hand-made payload.
func resolveCustomHeadersWrite(current map[string]string, next map[string]string) (map[string]string, error) {
	if len(next) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(next))
	for name, value := range next {
		if value != CustomHeaderRedactedValue {
			resolved[name] = value
			continue
		}
		stored, ok := current[name]
		if !ok {
			return nil, &DomainError{StatusCode: http.StatusUnprocessableEntity, Detail: "custom_headers contains a redaction placeholder for an unknown header name"}
		}
		resolved[name] = stored
	}
	return resolved, nil
}

func maskConnectionsForWire(items []connectionResponse) []connectionResponse {
	masked := make([]connectionResponse, len(items))
	for index, item := range items {
		masked[index] = item.maskedForWire()
	}
	return masked
}

// mustResolveCustomHeadersWrite applies the create-path sentinel rule: there
// is no stored value to substitute, so any redaction placeholder is rejected.
// The returned error is a *DomainError that callers propagate as a 422.
func mustResolveCustomHeadersWrite(headers map[string]string) (map[string]string, error) {
	return resolveCustomHeadersWrite(nil, headers)
}
