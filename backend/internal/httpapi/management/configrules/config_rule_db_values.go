package configrules

import "database/sql"

func nullableInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func resolvedBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
