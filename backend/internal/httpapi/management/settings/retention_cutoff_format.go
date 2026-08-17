package settings

import (
	"fmt"
	"strings"
	"time"
)

func parseRetentionCutoff(value string, now time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.HasSuffix(trimmed, "Z") {
		return time.Time{}, fmt.Errorf("retention cutoff must be UTC")
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil || !parsed.UTC().Equal(parsed) || parsed.After(now.UTC()) {
		return time.Time{}, fmt.Errorf("invalid retention cutoff")
	}
	// The wire contract canonicalizes accepted custom cutoffs to UTC
	// microseconds before hashing and before the sealed job scope is created.
	return parsed.UTC().Truncate(time.Microsecond), nil
}

func formatRetentionCutoff(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format("2006-01-02T15:04:05.000000Z")
	return &formatted
}
