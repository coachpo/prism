package safediag

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ErrorEnvelopeExtraction is the result of extracting a safe provider error
// summary from a bounded raw response sample.
type ErrorEnvelopeExtraction struct {
	// Code is a stable provider/local code, or "" when none could be adopted.
	Code string
	// Detail is the sanitized operator-readable message, or "".
	Detail string
	// Redacted reports whether any credential replacement occurred.
	Redacted bool
	// Truncated reports whether the detail was cut at the 4 KiB cap.
	Truncated bool
	// Recognized reports whether the sample was a recognized JSON error
	// envelope. Unrecognized content must fall back to generic status text.
	Recognized bool
}

// ErrorFieldPriority is the fixed extraction order message → detail → reason.
var ErrorFieldPriority = []string{"message", "detail", "reason"}

// allowlistedScalars are the only keys that may contribute to diagnostics.
var allowlistedScalars = map[string]struct{}{
	"code":    {},
	"type":    {},
	"status":  {},
	"message": {},
	"detail":  {},
	"reason":  {},
}

// ExtractProviderErrorEnvelope extracts a safe diagnostic from a bounded raw
// upstream error sample. The sample must be process-local and never enter
// ordinary telemetry. Only recognized provider JSON error envelopes may
// contribute allowlisted scalars; any other content type shape (plain text,
// HTML, binary, unknown) yields an unrecognized extraction that callers must
// map to a generic fallback.
func ExtractProviderErrorEnvelope(sample []byte, contentType string, extraRules ...SensitiveNameRule) ErrorEnvelopeExtraction {
	if len(sample) == 0 {
		return ErrorEnvelopeExtraction{Recognized: false}
	}
	if !looksLikeJSONContentType(contentType) {
		return ErrorEnvelopeExtraction{Recognized: false}
	}
	var payload map[string]any
	if err := json.Unmarshal(sample, &payload); err != nil || payload == nil {
		return ErrorEnvelopeExtraction{Recognized: false}
	}
	matcher := NewSensitiveNameMatcher(extraRules...)
	code, detail, redacted, recognized := extractFromPayload(payload, matcher)
	if !recognized {
		return ErrorEnvelopeExtraction{Recognized: false}
	}
	truncated := false
	if len(detail) > MaxErrorDetailBytes {
		detail, truncated = TruncateUTF8(detail, MaxErrorDetailBytes)
	}
	if code != "" && !ValidErrorCode(code) {
		code = ""
	}
	return ErrorEnvelopeExtraction{
		Code:       code,
		Detail:     detail,
		Redacted:   redacted,
		Truncated:  truncated,
		Recognized: true,
	}
}

func looksLikeJSONContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = strings.TrimSpace(mediaType[:idx])
	}
	return mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json") ||
		strings.HasPrefix(mediaType, "application/problem+json")
}

// extractFromPayload checks candidate objects in fixed specificity order:
// root.error, then the first element of root.errors[], then the root itself.
// The most specific candidate with at least one usable allowlisted scalar
// wins.
func extractFromPayload(payload map[string]any, matcher *sensitiveNameMatcher) (string, string, bool, bool) {
	var candidates []map[string]any
	if errorObj, ok := asObject(payload["error"]); ok {
		candidates = append(candidates, errorObj)
	}
	if errorsArr, ok := payload["errors"].([]any); ok && len(errorsArr) > 0 {
		for _, item := range errorsArr {
			if errorObj, ok := asObject(item); ok {
				candidates = append(candidates, errorObj)
				break
			}
		}
	}
	if root, ok := asObject(payload); ok {
		candidates = append(candidates, root)
	}
	for _, candidate := range candidates {
		code, detail, redacted, recognized := extractFromObject(candidate, matcher)
		if recognized {
			return code, detail, redacted, true
		}
	}
	return "", "", false, false
}

// extractFromObject collects allowlisted scalars from one error object. A
// candidate is recognized only if at least one allowlisted scalar holds a
// usable string (or a numeric code/status). Code preference is fixed: string
// code > string type > string status; numeric code/status is used only when
// no string code/type/status exists.
func extractFromObject(object map[string]any, matcher *sensitiveNameMatcher) (string, string, bool, bool) {
	code := ""
	status := ""
	var detailParts []string
	redacted := false
	usable := false
	stringCodeFound := false
	// Pass 1: strings in fixed key priority; detail order message → detail → reason.
	for _, key := range []string{"code", "type", "status", "message", "detail", "reason"} {
		value, exists := object[key]
		if !exists {
			continue
		}
		text, isString := value.(string)
		if !isString {
			continue
		}
		scrubbed, wasRedacted := scrubValueInner(text)
		if wasRedacted {
			redacted = true
		}
		switch key {
		case "code":
			if !stringCodeFound {
				code = scrubbed
				stringCodeFound = true
			}
		case "type":
			if !stringCodeFound && scrubbed != "" {
				code = scrubbed
				stringCodeFound = true
			}
		case "status":
			if !stringCodeFound && scrubbed != "" {
				status = scrubbed
				stringCodeFound = true
			}
		case "message", "detail", "reason":
			if scrubbed != "" {
				detailParts = append(detailParts, scrubbed)
			}
		}
		usable = true
	}
	// Pass 2: numeric code/status only when no string code/type/status exists.
	if !stringCodeFound {
		if number, isNumber := jsonNumberString(object["code"]); isNumber {
			code = number
			usable = true
		} else if number, isNumber := jsonNumberString(object["status"]); isNumber {
			status = number
			usable = true
		}
	}
	if !usable {
		return "", "", redacted, false
	}
	if code == "" {
		code = status
	}
	detail := dedupeAndJoin(detailParts, " ")
	return strings.TrimSpace(code), detail, redacted, true
}

func asObject(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func jsonNumberString(value any) (string, bool) {
	switch number := value.(type) {
	case json.Number:
		return number.String(), true
	case float64:
		return strconv.FormatFloat(number, 'f', -1, 64), true
	default:
		return "", false
	}
}

func dedupeAndJoin(parts []string, separator string) string {
	seen := map[string]struct{}{}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if _, duplicate := seen[part]; duplicate {
			continue
		}
		seen[part] = struct{}{}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, separator)
}
