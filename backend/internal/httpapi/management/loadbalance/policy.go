package loadbalance

import (
	"fmt"
	"sort"
	"strings"
)

var defaultFailureStatusCodes = []int{403, 422, 429, 500, 502, 503, 504, 529}

const (
	defaultBanMode                = "off"
	defaultRetryBaseDelayMS       = 60000
	defaultRetryBackoffMultiplier = 2.0
	defaultRetryJitterRatio       = 0.2
	defaultRetryMaxDelayMS        = 900000
	defaultRetryMaxAttempts       = 3
	defaultBanDurationSeconds     = 0
)

func canonicalDefaultStrategySpecs() []canonicalDefaultStrategySpec {
	return []canonicalDefaultStrategySpec{
		{Name: "Default single routing", LegacyStrategyType: "single"},
		{Name: "Default fill-first routing", LegacyStrategyType: "fill-first"},
		{Name: "Default round-robin routing", LegacyStrategyType: "round-robin"},
	}
}

func canonicalizeStrategyRequest(requestBody loadbalanceStrategyRequest) (strategyPersistedPayload, error) {
	name := strings.TrimSpace(requestBody.Name)
	if name == "" {
		return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "name must not be empty"}
	}
	if requestBody.LegacyStrategyType == nil {
		return strategyPersistedPayload{}, &domainError{StatusCode: 400, Detail: "legacy_strategy_type is required"}
	}
	legacyStrategyType, err := normalizeLegacyStrategyType(*requestBody.LegacyStrategyType)
	if err != nil {
		return strategyPersistedPayload{}, err
	}
	failureStatusCodes, err := normalizeStatusCodes(requestBody.FailureStatusCodes, defaultFailureStatusCodes)
	if err != nil {
		return strategyPersistedPayload{}, err
	}
	banMode := resolvedString(requestBody.BanMode, defaultBanMode)
	payload := strategyPersistedPayload{
		Name:                   name,
		LegacyStrategyType:     legacyStrategyType,
		FailureStatusCodes:     failureStatusCodes,
		BanMode:                banMode,
		RetryBaseDelayMS:       resolvedInt(requestBody.RetryBaseDelayMS, defaultRetryBaseDelayMS),
		RetryBackoffMultiplier: resolvedFloat(requestBody.RetryBackoffMultiplier, defaultRetryBackoffMultiplier),
		RetryJitterRatio:       resolvedFloat(requestBody.RetryJitterRatio, defaultRetryJitterRatio),
		RetryMaxDelayMS:        resolvedInt(requestBody.RetryMaxDelayMS, defaultRetryMaxDelayMS),
		RetryMaxAttempts:       resolvedInt(requestBody.RetryMaxAttempts, defaultRetryMaxAttempts),
		BanDurationSeconds:     resolvedInt(requestBody.BanDurationSeconds, defaultBanDurationSeconds),
	}
	if err := validateBanPolicy(payload); err != nil {
		return strategyPersistedPayload{}, err
	}
	return payload, nil
}

func defaultStrategyPayload(spec canonicalDefaultStrategySpec) strategyPersistedPayload {
	return strategyPersistedPayload{
		Name:                   spec.Name,
		LegacyStrategyType:     spec.LegacyStrategyType,
		FailureStatusCodes:     append([]int(nil), defaultFailureStatusCodes...),
		BanMode:                defaultBanMode,
		RetryBaseDelayMS:       defaultRetryBaseDelayMS,
		RetryBackoffMultiplier: defaultRetryBackoffMultiplier,
		RetryJitterRatio:       defaultRetryJitterRatio,
		RetryMaxDelayMS:        defaultRetryMaxDelayMS,
		RetryMaxAttempts:       defaultRetryMaxAttempts,
		BanDurationSeconds:     defaultBanDurationSeconds,
	}
}

func normalizeLegacyStrategyType(value string) (string, error) {
	resolved := strings.ToLower(strings.TrimSpace(value))
	switch resolved {
	case "single", "fill-first", "round-robin":
		return resolved, nil
	default:
		return "", &domainError{StatusCode: 400, Detail: "legacy_strategy_type must be one of 'single', 'fill-first', or 'round-robin'"}
	}
}

func normalizeStatusCodes(values []int, defaults []int) ([]int, error) {
	if len(values) == 0 {
		return append([]int(nil), defaults...), nil
	}
	seen := map[int]struct{}{}
	items := make([]int, 0, len(values))
	for _, value := range values {
		if value < 100 || value > 599 {
			return nil, &domainError{StatusCode: 400, Detail: "failure_status_codes must contain valid HTTP status codes"}
		}
		if _, ok := seen[value]; ok {
			return nil, &domainError{StatusCode: 400, Detail: "failure_status_codes must not contain duplicates"}
		}
		seen[value] = struct{}{}
		items = append(items, value)
	}
	sort.Ints(items)
	return items, nil
}

func validateBanPolicy(payload strategyPersistedPayload) error {
	if payload.RetryBaseDelayMS < 0 || payload.RetryBaseDelayMS > 86400000 {
		return &domainError{StatusCode: 400, Detail: "retry_base_delay_ms must be between 0 and 86400000"}
	}
	if payload.RetryBackoffMultiplier < 1.0 || payload.RetryBackoffMultiplier > 10.0 {
		return &domainError{StatusCode: 400, Detail: "retry_backoff_multiplier must be between 1.0 and 10.0"}
	}
	if payload.RetryJitterRatio < 0.0 || payload.RetryJitterRatio > 1.0 {
		return &domainError{StatusCode: 400, Detail: "retry_jitter_ratio must be between 0.0 and 1.0"}
	}
	if payload.RetryMaxDelayMS < 1 || payload.RetryMaxDelayMS > 86400000 {
		return &domainError{StatusCode: 400, Detail: "retry_max_delay_ms must be between 1 and 86400000"}
	}
	if payload.RetryMaxAttempts < 1 || payload.RetryMaxAttempts > 50 {
		return &domainError{StatusCode: 400, Detail: "retry_max_attempts must be between 1 and 50"}
	}
	if payload.BanDurationSeconds < 0 || payload.BanDurationSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "ban_duration_seconds must be between 0 and 86400"}
	}
	switch payload.BanMode {
	case "off", "manual":
		if payload.BanDurationSeconds != 0 {
			return &domainError{StatusCode: 400, Detail: fmt.Sprintf("ban_mode='%s' requires ban_duration_seconds=0", payload.BanMode)}
		}
	case "temporary":
		if payload.BanDurationSeconds < 1 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='temporary' requires ban_duration_seconds >= 1"}
		}
	default:
		return &domainError{StatusCode: 400, Detail: "ban_mode must be one of 'off', 'manual', or 'temporary'"}
	}
	return nil
}

func resolvedInt(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func resolvedFloat(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func resolvedString(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	resolved := strings.ToLower(strings.TrimSpace(*value))
	if resolved == "" {
		return fallback
	}
	return resolved
}

func strategyResponseFromRow(row strategyRow) (loadbalanceStrategyResponse, error) {
	legacyStrategyType, err := normalizeLegacyStrategyType(row.LegacyStrategyType)
	if err != nil {
		return loadbalanceStrategyResponse{}, err
	}
	failureStatusCodes, err := normalizeStatusCodes(row.FailureStatusCodes, defaultFailureStatusCodes)
	if err != nil {
		return loadbalanceStrategyResponse{}, err
	}
	payload := strategyPersistedPayload{
		Name:                   row.Name,
		LegacyStrategyType:     legacyStrategyType,
		FailureStatusCodes:     failureStatusCodes,
		BanMode:                strings.ToLower(strings.TrimSpace(row.BanMode)),
		RetryBaseDelayMS:       row.RetryBaseDelayMS,
		RetryBackoffMultiplier: row.RetryBackoffMultiplier,
		RetryJitterRatio:       row.RetryJitterRatio,
		RetryMaxDelayMS:        row.RetryMaxDelayMS,
		RetryMaxAttempts:       row.RetryMaxAttempts,
		BanDurationSeconds:     row.BanDurationSeconds,
	}
	if err := validateBanPolicy(payload); err != nil {
		return loadbalanceStrategyResponse{}, err
	}
	return loadbalanceStrategyResponse{
		ID:                     row.ID,
		ProfileID:              row.ProfileID,
		Name:                   row.Name,
		LegacyStrategyType:     payload.LegacyStrategyType,
		FailureStatusCodes:     append([]int(nil), payload.FailureStatusCodes...),
		BanMode:                payload.BanMode,
		RetryBaseDelayMS:       payload.RetryBaseDelayMS,
		RetryBackoffMultiplier: payload.RetryBackoffMultiplier,
		RetryJitterRatio:       payload.RetryJitterRatio,
		RetryMaxDelayMS:        payload.RetryMaxDelayMS,
		RetryMaxAttempts:       payload.RetryMaxAttempts,
		BanDurationSeconds:     payload.BanDurationSeconds,
		AttachedModelCount:     row.AttachedModelCount,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}, nil
}

func strategyMatchesCanonicalDefault(response loadbalanceStrategyResponse, expected canonicalDefaultStrategySpec) bool {
	payload := defaultStrategyPayload(expected)
	return response.Name == payload.Name &&
		response.LegacyStrategyType == payload.LegacyStrategyType &&
		response.BanMode == payload.BanMode &&
		response.RetryBaseDelayMS == payload.RetryBaseDelayMS &&
		response.RetryBackoffMultiplier == payload.RetryBackoffMultiplier &&
		response.RetryJitterRatio == payload.RetryJitterRatio &&
		response.RetryMaxDelayMS == payload.RetryMaxDelayMS &&
		response.RetryMaxAttempts == payload.RetryMaxAttempts &&
		response.BanDurationSeconds == payload.BanDurationSeconds &&
		equalIntSlices(response.FailureStatusCodes, payload.FailureStatusCodes)
}

func equalIntSlices(left []int, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
