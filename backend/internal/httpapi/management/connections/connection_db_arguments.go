package connections

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableJSONString(value map[string]string) any {
	if len(value) == 0 {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(raw)
}

func nullableCustomRequestParametersArg(value *terminaltarget.CustomRequestParameters) any {
	if value == nil || value.IsEmpty() {
		return nil
	}
	return string(value.RawObject())
}

func nullableTimeValue(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	resolved := value.Time.UTC()
	return &resolved
}

func nullableInt64Value(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func int32ArrayArg(values []int) []int32 {
	items := make([]int32, 0, len(values))
	for _, value := range values {
		items = append(items, int32(value))
	}
	return items
}

// int16ArrayArg feeds the smallint columns of connection_routing_windows.
// Narrowing is safe here because every value has already passed the domain
// range validation (mask 1..127, minutes 0..2880); validating first and
// narrowing second is the required order, since narrowing first would turn an
// out-of-range 384 into a plausible 128.
func int16ArrayArg(values []int) []int16 {
	items := make([]int16, 0, len(values))
	for _, value := range values {
		items = append(items, int16(value))
	}
	return items
}
