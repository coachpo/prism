package runtime

import (
	"encoding/json"
	"strings"
	"time"
)

func durationMilliseconds(duration time.Duration) int {
	milliseconds := int(duration / time.Millisecond)
	if milliseconds < 1 {
		return 1
	}
	return milliseconds
}

func trimmedStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// optionalTrimmedStringPointer returns a pointer to the trimmed value when
// non-empty (used for typed enums that may legitimately be empty, e.g. legacy
// rows with no trigger evidence).
func optionalTrimmedStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// nonNegativeIntPointer returns a pointer to value when value > 0.
func nonNegativeIntPointer(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// nullableBytesArg binds a captured audit body to its bytea column. The bytes
// must reach the driver as []byte: a Go string is sent in text format, which
// PostgreSQL then parses as a bytea input literal, so a body containing a
// backslash fails with 22P02 and one without is stored escape-interpreted
// instead of verbatim. The legacy v1 drain already writes these columns this
// way (platform/startup/runtime_telemetry_v1_drain.go legacyBodyBytes).
func nullableBytesArg(value *string) any {
	if value == nil {
		return nil
	}
	return []byte(*value)
}

// nullableAttemptNumberArg writes the attempt number only for real upstream
// rows; planning/admission diagnostic rows must keep NULL attempt fields
// (Requests SPEC §3.4: diagnostics never masquerade as attempt 1).
func nullableAttemptNumberArg(rowKind string, attemptNumber int) any {
	if rowKind != requestLogRowKindUpstream {
		return nil
	}
	return attemptNumber
}

// nullableStringSliceArg passes a string slice as a text[] column value. For
// columns that are NOT NULL DEFAULT '{}' (metadata provenance arrays) the
// empty slice maps to an empty array literal; for nullable columns (e.g.
// missing_price_components) an empty slice maps to SQL NULL so the pricing
// CHECKs that require NULL stay satisfiable.
func nullableStringSliceArg(value []string) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

// notNullStringArrayArg maps an empty slice to an empty array literal for
// NOT NULL text[] columns.
func notNullStringArrayArg(value []string) any {
	if len(value) == 0 {
		return []string{}
	}
	return value
}

func nullableIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Arg(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func nullableBoolArg(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableJSONArg(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(raw)
}

func intPtr(value int) *int {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func stringPtr(value string) *string {
	return &value
}
