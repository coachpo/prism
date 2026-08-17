package settings

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullableNonEmptyString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intPtr(value int) *int { return &value }
