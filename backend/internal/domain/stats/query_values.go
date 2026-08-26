package stats

import "database/sql"

// PostgreSQL scan and scalar projection conversions shared by stats read models.
// These conversions are intentionally limited to null/value representation;
// read-model policy stays with the caller that projects each row.
func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func nullableBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	resolved := value.Bool
	return &resolved
}

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	resolved := value.Int64
	return &resolved
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
