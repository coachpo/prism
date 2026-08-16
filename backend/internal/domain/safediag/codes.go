package safediag

import "fmt"

// Stable failure code builders from Requests/Audit SPEC §4.2. Local typed
// enums are preferred; provider codes are adopted only after trim + scrub +
// grammar validation, otherwise these fixed fallbacks are used.
const (
	// CodeTransportError is the fallback for transport failures.
	CodeTransportError = "transport_error"
	// CodeClientDisconnected is the fallback for client disconnects.
	CodeClientDisconnected = "client_disconnected"
	// CodeStreamUpstreamReadFailed is the stable stream read failure code.
	CodeStreamUpstreamReadFailed = "stream_upstream_read_failed"
	// CodeAttemptBudgetExhausted is the launch safety cap terminal code.
	CodeAttemptBudgetExhausted = "attempt_budget_exhausted"
)

// HTTPFallbackCode builds the stable upstream HTTP fallback code.
func HTTPFallbackCode(statusCode int) string {
	return fmt.Sprintf("upstream_http_%d", statusCode)
}

// StreamKindFallbackCode builds the stable stream error code from a
// non-empty stream error kind.
func StreamKindFallbackCode(kind string) string {
	return "stream_" + kind
}

// StreamOutcomeFallbackCode builds the stable stream error code from an
// abnormal stream outcome.
func StreamOutcomeFallbackCode(outcome string) string {
	return "stream_" + outcome
}

// PrismStageFallbackCode builds the planning/admission fallback code.
func PrismStageFallbackCode(stage string) string {
	return "prism_" + stage + "_failure"
}

// AdoptProviderCode validates a provider code/type for persistence: it must
// survive trim + scrub and match the stable grammar. The empty string means
// the provider code must be dropped in favor of a fallback.
func AdoptProviderCode(providerCode string) string {
	trimmed := providerCode
	scrubbed, _ := scrubValueInner(trimmed)
	scrubbed = trimSensitiveCode(scrubbed)
	if ValidErrorCode(scrubbed) {
		return scrubbed
	}
	return ""
}

func trimSensitiveCode(code string) string {
	// A code must be a single token; strip whitespace and control characters.
	cleaned := stripControlCharacters(code)
	cleaned = foldWhitespace(cleaned)
	return cleaned
}
