package config

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

func missingBootstrapFieldError(path string) error {
	return fmt.Errorf("bootstrap config field %s is required", path)
}

func requiredTrimmedString(path string, value *string, minLength int, maxLength int) (string, error) {
	if value == nil {
		return "", missingBootstrapFieldError(path)
	}
	trimmed := strings.TrimSpace(*value)
	if len(trimmed) < minLength {
		return "", fmt.Errorf("bootstrap config field %s must be at least %d characters", path, minLength)
	}
	if maxLength > 0 && len(trimmed) > maxLength {
		return "", fmt.Errorf("bootstrap config field %s must be at most %d characters", path, maxLength)
	}
	return trimmed, nil
}

func requiredEnumString(path string, value *string, allowed []string) (string, error) {
	resolved, err := requiredTrimmedString(path, value, 1, 0)
	if err != nil {
		return "", err
	}
	if slices.Contains(allowed, resolved) {
		return resolved, nil
	}
	return "", fmt.Errorf("bootstrap config field %s must be one of %q", path, allowed)
}

func optionalTrimmedString(path string, value *string, maxLength int) (string, error) {
	if value == nil {
		return "", nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return "", nil
	}
	if maxLength > 0 && len(trimmed) > maxLength {
		return "", fmt.Errorf("bootstrap config field %s must be at most %d characters", path, maxLength)
	}
	return trimmed, nil
}

func requiredDateTime(path string, value *string) (time.Time, error) {
	resolved, err := requiredTrimmedString(path, value, 1, 0)
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339, resolved)
	if err != nil {
		return time.Time{}, fmt.Errorf("bootstrap config field %s must be a valid RFC3339 date-time", path)
	}
	return parsed, nil
}

func requiredIntConst(path string, value *int, expected int) (int, error) {
	resolved, err := requiredIntRange(path, value, expected, expected)
	if err != nil {
		return 0, err
	}
	return resolved, nil
}

func requiredIntMin(path string, value *int, minimum int) (int, error) {
	if value == nil {
		return 0, missingBootstrapFieldError(path)
	}
	if *value < minimum {
		return 0, fmt.Errorf("bootstrap config field %s must be greater than or equal to %d", path, minimum)
	}
	return *value, nil
}

func requiredIntRange(path string, value *int, minimum int, maximum int) (int, error) {
	if value == nil {
		return 0, missingBootstrapFieldError(path)
	}
	if *value < minimum || *value > maximum {
		return 0, fmt.Errorf("bootstrap config field %s must be between %d and %d", path, minimum, maximum)
	}
	return *value, nil
}

func requiredFloat64Range(path string, value *float64, minimum float64, maximum float64) (float64, error) {
	if value == nil {
		return 0, missingBootstrapFieldError(path)
	}
	if *value < minimum || *value > maximum {
		return 0, fmt.Errorf("bootstrap config field %s must be between %g and %g", path, minimum, maximum)
	}
	return *value, nil
}

func requiredBool(path string, value *bool) (bool, error) {
	if value == nil {
		return false, missingBootstrapFieldError(path)
	}
	return *value, nil
}

func requiredAbsoluteURIs(path string, value *[]string) ([]string, error) {
	if value == nil {
		return nil, missingBootstrapFieldError(path)
	}
	resolved := make([]string, 0, len(*value))
	seen := make(map[string]struct{}, len(*value))
	for index, rawValue := range *value {
		trimmed := strings.TrimSpace(rawValue)
		if trimmed == "" {
			return nil, fmt.Errorf("bootstrap config field %s[%d] must be a non-empty absolute URI", path, index)
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
			return nil, fmt.Errorf("bootstrap config field %s[%d] must be a non-empty absolute URI", path, index)
		}
		if _, exists := seen[trimmed]; exists {
			return nil, fmt.Errorf("bootstrap config field %s contains duplicate URI %q", path, trimmed)
		}
		seen[trimmed] = struct{}{}
		resolved = append(resolved, trimmed)
	}
	return resolved, nil
}

func parseDurationField(path string, value *string) (time.Duration, error) {
	resolved, err := requiredTrimmedString(path, value, 1, 0)
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(resolved)
	if err != nil {
		return 0, fmt.Errorf("bootstrap config field %s must parse as a Go duration", path)
	}
	return parsed, nil
}
