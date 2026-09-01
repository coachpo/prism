package terminaltarget

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// UpstreamModelIDMaxLength matches connections.upstream_model_id and
	// model_configs.model_id. The bound is Unicode scalar values, not bytes.
	UpstreamModelIDMaxLength = 200

	UpstreamModelIDReasonRequired = "required"
	UpstreamModelIDReasonTooLong  = "too_long"
)

// UpstreamModelIDValidationError is the HTTP-neutral rejection contract for
// one provided upstream model identity. Limit is populated only for the
// too_long reason.
type UpstreamModelIDValidationError struct {
	Reason string
	Limit  int
}

func (err *UpstreamModelIDValidationError) Error() string {
	if err == nil {
		return ""
	}
	message := fmt.Sprintf("invalid upstream model id: %s", err.Reason)
	if err.Limit > 0 {
		message += fmt.Sprintf(" (limit %d)", err.Limit)
	}
	return message
}

// NormalizeUpstreamModelID trims only leading and trailing Unicode whitespace,
// preserves case, slashes, and interior characters, then enforces the stored
// non-blank and 200-rune contract.
func NormalizeUpstreamModelID(value string) (string, *UpstreamModelIDValidationError) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return "", &UpstreamModelIDValidationError{Reason: UpstreamModelIDReasonRequired}
	}
	if utf8.RuneCountInString(normalized) > UpstreamModelIDMaxLength {
		return "", &UpstreamModelIDValidationError{Reason: UpstreamModelIDReasonTooLong, Limit: UpstreamModelIDMaxLength}
	}
	return normalized, nil
}
