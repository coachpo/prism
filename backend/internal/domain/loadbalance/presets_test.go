package loadbalance

import (
	"testing"
)

// presetPolicyFields pins the three SPEC §5.6 presets as backend golden
// fixtures so the frontend-owned preset constants and the preview endpoint
// reconcile against one authoritative table. The balanced preset must equal the
// canonical default payload exactly.
type presetPolicyFields struct {
	name               string
	baseDelayMS        int
	backoffMultiplier  float64
	jitterRatio        float64
	maxDelayMS         int
	cycleLimit         int
	banMode            string
	banThreshold       int
	banDurationSeconds int
}

func (preset presetPolicyFields) strategy() RuntimeStrategy {
	return RuntimeStrategy{
		LegacyStrategyType:                 stringPointer("fill-first"),
		FailureStatusCodes:                 append([]int(nil), defaultRuntimeFailoverStatusCodes...),
		BanMode:                            preset.banMode,
		RetryBaseDelayMS:                   preset.baseDelayMS,
		RetryBackoffMultiplier:             preset.backoffMultiplier,
		RetryJitterRatio:                   preset.jitterRatio,
		RetryMaxDelayMS:                    preset.maxDelayMS,
		CycleRetryAttemptLimit:             preset.cycleLimit,
		BanCumulativeRetryAttemptThreshold: preset.banThreshold,
		BanDurationSeconds:                 preset.banDurationSeconds,
	}
}

var threePresets = []presetPolicyFields{
	{
		name: "conservative", baseDelayMS: 30000, backoffMultiplier: 2.0, jitterRatio: 0.2,
		maxDelayMS: 1800000, cycleLimit: 2, banMode: "temporary", banThreshold: 4, banDurationSeconds: 3600,
	},
	{
		name: "balanced", baseDelayMS: 5000, backoffMultiplier: 2.0, jitterRatio: 0.2,
		maxDelayMS: 900000, cycleLimit: 3, banMode: "off", banThreshold: 0, banDurationSeconds: 0,
	},
	{
		name: "aggressive", baseDelayMS: 2000, backoffMultiplier: 1.5, jitterRatio: 0.2,
		maxDelayMS: 120000, cycleLimit: 5, banMode: "off", banThreshold: 0, banDurationSeconds: 0,
	},
}

func TestThreePresetsGoldenPayloads(t *testing.T) {
	if len(threePresets) != 3 {
		t.Fatalf("expected exactly three presets, got %d", len(threePresets))
	}
	// All three presets share the canonical failure status codes.
	for _, preset := range threePresets {
		strategy := preset.strategy()
		if !equalIntSlicesForTest(strategy.FailureStatusCodes, defaultRuntimeFailoverStatusCodes) {
			t.Fatalf("preset %q must use the canonical failure status codes, got %v", preset.name, strategy.FailureStatusCodes)
		}
	}

	// The balanced preset must equal the canonical default payload exactly
	// (presets do not carry a name; every policy field must match).
	canonical := DefaultStrategyPayload(CanonicalStrategySpec{Name: "Default fill-first routing", LegacyStrategyType: "fill-first"})
	balanced := threePresets[1].strategy()
	if balanced.LegacyStrategyType == nil || *balanced.LegacyStrategyType != *canonical.LegacyStrategyType ||
		balanced.BanMode != canonical.BanMode ||
		balanced.RetryBaseDelayMS != canonical.RetryBaseDelayMS ||
		balanced.RetryBackoffMultiplier != canonical.RetryBackoffMultiplier ||
		balanced.RetryJitterRatio != canonical.RetryJitterRatio ||
		balanced.RetryMaxDelayMS != canonical.RetryMaxDelayMS ||
		balanced.CycleRetryAttemptLimit != canonical.CycleRetryAttemptLimit ||
		balanced.BanCumulativeRetryAttemptThreshold != canonical.BanCumulativeRetryAttemptThreshold ||
		balanced.BanDurationSeconds != canonical.BanDurationSeconds ||
		!equalIntSlicesForTest(balanced.FailureStatusCodes, canonical.FailureStatusCodes) {
		t.Fatalf("balanced preset must equal the canonical default payload exactly, got %+v vs %+v", balanced, canonical)
	}
}

func TestThreePresetsPreviewGolden(t *testing.T) {
	// Conservative: 30-second base, 2x backoff, 20% jitter, 30-minute cap,
	// cycle 2, temporary ban at cumulative 4 for 1 hour. The preview stops at
	// cycle exhaustion (2 steps) and projects the ban beyond the cycle.
	conservative := PreviewRetryCycle(threePresets[0].strategy().FeedbackPolicy())
	if conservative.TerminationReason != PreviewTerminationCycleExhausted || conservative.HasMore || conservative.ShownStepCount != 2 {
		t.Fatalf("unexpected conservative preview %+v", conservative)
	}
	if conservative.Steps[0].NominalDelayMS != 30000 || conservative.Steps[0].JitterMinDelayMS != 24000 || conservative.Steps[0].JitterMaxDelayMS != 36000 {
		t.Fatalf("unexpected conservative first step %+v", conservative.Steps[0])
	}
	if conservative.Steps[1].NominalDelayMS != 60000 || conservative.Steps[1].JitterMaxDelayMS != 72000 {
		t.Fatalf("unexpected conservative second step %+v", conservative.Steps[1])
	}
	if conservative.BanProjection.Mode != "temporary" || conservative.BanProjection.TransitionAtCumulativeFailure == nil || *conservative.BanProjection.TransitionAtCumulativeFailure != 4 || conservative.BanProjection.DurationSeconds != 3600 {
		t.Fatalf("unexpected conservative ban projection %+v", conservative.BanProjection)
	}

	// Aggressive: 2-second base, 1.5x backoff, 20% jitter, 2-minute cap,
	// cycle 5, no ban. The preview shows all five steps with cycle exhaustion
	// on the last one.
	aggressive := PreviewRetryCycle(threePresets[2].strategy().FeedbackPolicy())
	if aggressive.TerminationReason != PreviewTerminationCycleExhausted || aggressive.HasMore || aggressive.ShownStepCount != 5 {
		t.Fatalf("unexpected aggressive preview %+v", aggressive)
	}
	wantNominal := []int{2000, 3000, 4500, 6750, 10125}
	for index, step := range aggressive.Steps {
		if step.NominalDelayMS != wantNominal[index] {
			t.Fatalf("unexpected aggressive step %d nominal %d, want %d", index, step.NominalDelayMS, wantNominal[index])
		}
		if step.CycleExhausted != (index == 4) {
			t.Fatalf("unexpected aggressive step %d flags %+v", index, step)
		}
	}
	if aggressive.BanProjection.Mode != "off" || aggressive.BanProjection.TransitionAtCumulativeFailure != nil {
		t.Fatalf("unexpected aggressive ban projection %+v", aggressive.BanProjection)
	}
}

func equalIntSlicesForTest(left []int, right []int) bool {
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
