package loadbalance

import (
	"slices"
	"strings"
)

// Canonical payload constants: the balanced preset expanded at schema level.
// They are the single shared source of truth for built-in strategy rows and the
// balanced preview preset; the one-time SQL backfill migration carries the same
// values and is covered by migration fixtures.
const (
	canonicalBanMode                            = "off"
	canonicalRetryBaseDelayMS                   = 60000
	canonicalRetryBackoffMultiplier             = 2.0
	canonicalRetryJitterRatio                   = 0.2
	canonicalRetryMaxDelayMS                    = 900000
	canonicalCycleRetryAttemptLimit             = 3
	canonicalBanCumulativeRetryAttemptThreshold = 0
	canonicalBanDurationSeconds                 = 0
)

// CanonicalStrategySpec identifies one of the three built-in routing strategy
// rows by exact name and machine strategy type.
type CanonicalStrategySpec struct {
	Name               string
	LegacyStrategyType string
}

// CanonicalDefaultStrategySpecs is the single shared source of truth for the
// built-in strategy rows. Seed, defaults action and completeness checks MUST
// use this list; the one-time SQL backfill migration carries the same payload
// constants and is covered by migration fixtures.
func CanonicalDefaultStrategySpecs() []CanonicalStrategySpec {
	return []CanonicalStrategySpec{
		{Name: "Default single routing", LegacyStrategyType: "single"},
		{Name: "Default fill-first routing", LegacyStrategyType: "fill-first"},
		{Name: "Default round-robin routing", LegacyStrategyType: "round-robin"},
	}
}

// DefaultStrategyPayload returns the exact canonical policy payload for a spec.
func DefaultStrategyPayload(spec CanonicalStrategySpec) RuntimeStrategy {
	legacyStrategyType := spec.LegacyStrategyType
	return RuntimeStrategy{
		Name:                               spec.Name,
		LegacyStrategyType:                 &legacyStrategyType,
		FailureStatusCodes:                 append([]int(nil), defaultRuntimeFailoverStatusCodes...),
		BanMode:                            canonicalBanMode,
		RetryBaseDelayMS:                   canonicalRetryBaseDelayMS,
		RetryBackoffMultiplier:             canonicalRetryBackoffMultiplier,
		RetryJitterRatio:                   canonicalRetryJitterRatio,
		RetryMaxDelayMS:                    canonicalRetryMaxDelayMS,
		CycleRetryAttemptLimit:             canonicalCycleRetryAttemptLimit,
		BanCumulativeRetryAttemptThreshold: canonicalBanCumulativeRetryAttemptThreshold,
		BanDurationSeconds:                 canonicalBanDurationSeconds,
	}
}

// StrategyMatchesCanonical reports whether the strategy row is an exact
// canonical match: name, machine strategy type and the complete policy payload
// must all be equal. ID, profile, is_default, attachment count and timestamps
// are not part of the canonical payload.
func StrategyMatchesCanonical(strategy RuntimeStrategy, spec CanonicalStrategySpec) bool {
	expected := DefaultStrategyPayload(spec)
	if strategy.Name != expected.Name {
		return false
	}
	if strategy.LegacyStrategyType == nil || !strings.EqualFold(strings.TrimSpace(*strategy.LegacyStrategyType), spec.LegacyStrategyType) {
		return false
	}
	if strategy.BanMode != expected.BanMode ||
		strategy.RetryBaseDelayMS != expected.RetryBaseDelayMS ||
		strategy.RetryBackoffMultiplier != expected.RetryBackoffMultiplier ||
		strategy.RetryJitterRatio != expected.RetryJitterRatio ||
		strategy.RetryMaxDelayMS != expected.RetryMaxDelayMS ||
		strategy.CycleRetryAttemptLimit != expected.CycleRetryAttemptLimit ||
		strategy.BanCumulativeRetryAttemptThreshold != expected.BanCumulativeRetryAttemptThreshold ||
		strategy.BanDurationSeconds != expected.BanDurationSeconds {
		return false
	}
	return slices.Equal(strategy.FailureStatusCodes, expected.FailureStatusCodes)
}

// CanonicalPayloadNames returns the canonical names in canonical order.
func CanonicalPayloadNames() []string {
	specs := CanonicalDefaultStrategySpecs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}
