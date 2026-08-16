package safediag

import "unicode/utf8"

// Fixed bounds from Requests/Audit SPEC §3.1. These MUST remain code-fixed
// constants; changing any value requires syncing PRODUCT, API metadata,
// tests, and canonical docs.
const (
	// MaxErrorDetailBytes is the maximum persisted error detail length.
	MaxErrorDetailBytes = 4 * 1024

	// MaxStreamErrorDetailBytes mirrors MaxErrorDetailBytes for the
	// independent stream_error_detail field.
	MaxStreamErrorDetailBytes = 4 * 1024

	// MaxErrorCodeLength is the maximum stable error code length.
	MaxErrorCodeLength = 120

	// MaxUpstreamErrorSampleBytes is the in-memory raw upstream error sample
	// bound (32 KiB per attempt); it never enters the outbox.
	MaxUpstreamErrorSampleBytes = 32 * 1024

	// MaxRequestURLBytes is the scrubbed request URL cap (4 KiB).
	MaxRequestURLBytes = 4 * 1024

	// MaxAuditHeaderValueBytes is the per-value cap applied while scrubbing
	// non-sensitive audit header values (the persisted header block is
	// bounded by the capture budget; this only prevents a single huge value
	// from dominating the block).
	MaxAuditHeaderValueBytes = 16 * 1024

	// MaxEndpointBaseURLBytes is the scrubbed endpoint base URL cap (4 KiB).
	MaxEndpointBaseURLBytes = 4 * 1024

	// MaxRequestPathBytes is the request path cap (1 KiB).
	MaxRequestPathBytes = 1024

	// MaxCorrelationValueBytes is the correlation/request ID cap (1 KiB).
	MaxCorrelationValueBytes = 1024

	// MaxCorrelationCodePoints is the correlation/request ID codepoint cap.
	MaxCorrelationCodePoints = 255

	// MaxUserAgentBytes is the User-Agent cap (2 KiB).
	MaxUserAgentBytes = 2 * 1024

	// MaxLabelBytes is the generic external label/path cap (1 KiB).
	MaxLabelBytes = 1024

	// MaxOperationNameCodePoints is the operation_name column codepoint cap.
	MaxOperationNameCodePoints = 120

	// MaxRequestPathCodePoints is the request_path column codepoint cap.
	MaxRequestPathCodePoints = 500

	// MaxEndpointBaseURLCodePoints is the endpoint_base_url column codepoint cap.
	MaxEndpointBaseURLCodePoints = 500

	// MaxRequestURLCodePoints is the audit request_url column codepoint cap.
	MaxRequestURLCodePoints = 2000
)

// ErrorCodePattern is the stable error code grammar. New failed rows MUST use
// codes matching this pattern.
const ErrorCodePattern = `^[A-Za-z0-9][A-Za-z0-9._:-]{0,119}$`

// ValidErrorCode reports whether code is a stable code candidate.
func ValidErrorCode(code string) bool {
	if code == "" {
		return false
	}
	if len(code) > MaxErrorCodeLength {
		return false
	}
	first := code[0]
	if !isAlphaNumericASCII(first) {
		return false
	}
	for i := 1; i < len(code); i++ {
		c := code[i]
		if !isAlphaNumericASCII(c) && c != '.' && c != '_' && c != ':' && c != '-' {
			return false
		}
	}
	return true
}

func isAlphaNumericASCII(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// TruncateUTF8 truncates s to at most maxBytes while preserving UTF-8
// code-point boundaries. It returns the safe prefix and whether truncation
// occurred.
func TruncateUTF8(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	limit := maxBytes
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	if limit == 0 {
		return "", true
	}
	return s[:limit], true
}

// TruncateCodePoints truncates s to at most maxCodePoints UTF-8 code points.
// It returns the safe prefix and whether truncation occurred.
func TruncateCodePoints(s string, maxCodePoints int) (string, bool) {
	if utf8.RuneCountInString(s) <= maxCodePoints {
		return s, false
	}
	count := 0
	for index := range s {
		if count == maxCodePoints {
			return s[:index], true
		}
		count++
	}
	return s, true
}

// ByteLenUnderCap reports whether the UTF-8 byte length of s is within cap.
func ByteLenUnderCap(s string, capBytes int) bool {
	return len(s) <= capBytes
}
