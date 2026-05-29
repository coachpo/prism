package loadbalance

import (
	"fmt"
	"strings"
)

const (
	defaultImportedCycleRetryAttemptLimit             = 3
	defaultImportedBanCumulativeRetryAttemptThreshold = 0
)

type ImportedStrategyDocument struct {
	Name                               string
	LegacyStrategyType                 *string
	FailureStatusCodes                 []int
	BanMode                            *string
	RetryBaseDelayMS                   *int
	RetryBackoffMultiplier             *float64
	RetryJitterRatio                   *float64
	RetryMaxDelayMS                    *int
	CycleRetryAttemptLimit             *int
	BanCumulativeRetryAttemptThreshold *int
	BanDurationSeconds                 *int
}

type CanonicalImportedStrategy struct {
	Name                               string
	LegacyStrategyType                 *string
	FailureStatusCodes                 []int
	BanMode                            string
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
	LegacyStrategyPtr                  *string
}

type importedStrategyPolicyPayload struct {
	RetryBaseDelayMS                   int
	RetryBackoffMultiplier             float64
	RetryJitterRatio                   float64
	RetryMaxDelayMS                    int
	CycleRetryAttemptLimit             int
	BanCumulativeRetryAttemptThreshold int
	BanDurationSeconds                 int
	BanMode                            string
}

func CanonicalizeImportedStrategyDocument(document ImportedStrategyDocument) (CanonicalImportedStrategy, error) {
	name := strings.TrimSpace(document.Name)
	if name == "" {
		return CanonicalImportedStrategy{}, &domainError{StatusCode: 400, Detail: "name must not be empty"}
	}
	if document.LegacyStrategyType == nil {
		return CanonicalImportedStrategy{}, &domainError{StatusCode: 400, Detail: "legacy_strategy_type is required"}
	}
	legacyStrategyType, err := normalizeLegacyStrategyType(*document.LegacyStrategyType)
	if err != nil {
		return CanonicalImportedStrategy{}, err
	}
	failureStatusCodes, err := normalizeStatusCodes(document.FailureStatusCodes, defaultFailureStatusCodes)
	if err != nil {
		return CanonicalImportedStrategy{}, err
	}

	policy := importedStrategyPolicyPayload{
		BanMode:                            resolvedString(document.BanMode, defaultBanMode),
		RetryBaseDelayMS:                   resolvedInt(document.RetryBaseDelayMS, defaultRetryBaseDelayMS),
		RetryBackoffMultiplier:             resolvedFloat(document.RetryBackoffMultiplier, defaultRetryBackoffMultiplier),
		RetryJitterRatio:                   resolvedFloat(document.RetryJitterRatio, defaultRetryJitterRatio),
		RetryMaxDelayMS:                    resolvedInt(document.RetryMaxDelayMS, defaultRetryMaxDelayMS),
		CycleRetryAttemptLimit:             resolvedInt(document.CycleRetryAttemptLimit, defaultImportedCycleRetryAttemptLimit),
		BanCumulativeRetryAttemptThreshold: resolvedInt(document.BanCumulativeRetryAttemptThreshold, defaultImportedBanCumulativeRetryAttemptThreshold),
		BanDurationSeconds:                 resolvedInt(document.BanDurationSeconds, defaultBanDurationSeconds),
	}
	if err := validateImportedBanPolicy(policy); err != nil {
		return CanonicalImportedStrategy{}, err
	}

	return CanonicalImportedStrategy{
		Name:                               name,
		LegacyStrategyType:                 &legacyStrategyType,
		FailureStatusCodes:                 append([]int(nil), failureStatusCodes...),
		BanMode:                            policy.BanMode,
		RetryBaseDelayMS:                   policy.RetryBaseDelayMS,
		RetryBackoffMultiplier:             policy.RetryBackoffMultiplier,
		RetryJitterRatio:                   policy.RetryJitterRatio,
		RetryMaxDelayMS:                    policy.RetryMaxDelayMS,
		CycleRetryAttemptLimit:             policy.CycleRetryAttemptLimit,
		BanCumulativeRetryAttemptThreshold: policy.BanCumulativeRetryAttemptThreshold,
		BanDurationSeconds:                 policy.BanDurationSeconds,
		LegacyStrategyPtr:                  &legacyStrategyType,
	}, nil
}

func validateImportedBanPolicy(policy importedStrategyPolicyPayload) error {
	if policy.RetryBaseDelayMS < 0 || policy.RetryBaseDelayMS > 86400000 {
		return &domainError{StatusCode: 400, Detail: "retry_base_delay_ms must be between 0 and 86400000"}
	}
	if policy.RetryBackoffMultiplier < 1.0 || policy.RetryBackoffMultiplier > 10.0 {
		return &domainError{StatusCode: 400, Detail: "retry_backoff_multiplier must be between 1.0 and 10.0"}
	}
	if policy.RetryJitterRatio < 0.0 || policy.RetryJitterRatio > 1.0 {
		return &domainError{StatusCode: 400, Detail: "retry_jitter_ratio must be between 0.0 and 1.0"}
	}
	if policy.RetryMaxDelayMS < 1 || policy.RetryMaxDelayMS > 86400000 {
		return &domainError{StatusCode: 400, Detail: "retry_max_delay_ms must be between 1 and 86400000"}
	}
	if policy.CycleRetryAttemptLimit < 1 || policy.CycleRetryAttemptLimit > 50 {
		return &domainError{StatusCode: 400, Detail: "cycle_retry_attempt_limit must be between 1 and 50"}
	}
	if policy.BanDurationSeconds < 0 || policy.BanDurationSeconds > 86400 {
		return &domainError{StatusCode: 400, Detail: "ban_duration_seconds must be between 0 and 86400"}
	}

	switch policy.BanMode {
	case "off":
		if policy.BanCumulativeRetryAttemptThreshold != 0 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='off' requires ban_cumulative_retry_attempt_threshold=0"}
		}
		if policy.BanDurationSeconds != 0 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='off' requires ban_duration_seconds=0"}
		}
	case "temporary", "until_reset":
		if policy.BanCumulativeRetryAttemptThreshold < 1 || policy.BanCumulativeRetryAttemptThreshold > 500 {
			return &domainError{StatusCode: 400, Detail: fmt.Sprintf("ban_mode='%s' requires ban_cumulative_retry_attempt_threshold between 1 and 500", policy.BanMode)}
		}
		if policy.BanCumulativeRetryAttemptThreshold < policy.CycleRetryAttemptLimit {
			return &domainError{StatusCode: 400, Detail: "ban_cumulative_retry_attempt_threshold must be greater than or equal to cycle_retry_attempt_limit"}
		}
		if policy.BanMode == "temporary" {
			if policy.BanDurationSeconds < 1 {
				return &domainError{StatusCode: 400, Detail: "ban_mode='temporary' requires ban_duration_seconds >= 1"}
			}
		} else if policy.BanDurationSeconds != 0 {
			return &domainError{StatusCode: 400, Detail: "ban_mode='until_reset' requires ban_duration_seconds=0"}
		}
	default:
		return &domainError{StatusCode: 400, Detail: "ban_mode must be one of 'off', 'temporary', or 'until_reset'"}
	}
	return nil
}
