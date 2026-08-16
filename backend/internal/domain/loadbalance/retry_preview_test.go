package loadbalance

import (
	"testing"
)

func feedbackPolicyForStrategy(t *testing.T, strategy RuntimeStrategy) runtimeFeedbackPolicy {
	t.Helper()
	return strategy.FeedbackPolicy()
}

func TestRetryDelayBoundsBalancedGolden(t *testing.T) {
	strategy := DefaultStrategyPayload(CanonicalStrategySpec{Name: "Default fill-first routing", LegacyStrategyType: "fill-first"})
	policy := feedbackPolicyForStrategy(t, strategy)

	first := retryDelayBounds(policy, 1)
	if first.NominalDelayMS != 60000 || first.JitterMinDelayMS != 48000 || first.JitterMaxDelayMS != 72000 {
		t.Fatalf("expected balanced attempt 1 bounds 60000/48000/72000, got %+v", first)
	}
	second := retryDelayBounds(policy, 2)
	if second.NominalDelayMS != 120000 || second.JitterMinDelayMS != 96000 || second.JitterMaxDelayMS != 144000 {
		t.Fatalf("expected balanced attempt 2 bounds 120000/96000/144000, got %+v", second)
	}
	third := retryDelayBounds(policy, 3)
	if third.NominalDelayMS != 240000 || third.JitterMinDelayMS != 192000 || third.JitterMaxDelayMS != 288000 {
		t.Fatalf("expected balanced attempt 3 bounds 240000/192000/288000, got %+v", third)
	}
}

func TestRetryDelayBoundsOverflowSaturation(t *testing.T) {
	policy := runtimeFeedbackPolicy{
		Enabled:                true,
		CycleRetryAttemptLimit: 50,
		BaseDelayMS:            86400000,
		BackoffMultiplier:      10.0,
		JitterRatio:            0.2,
		MaxDelayMS:             900000,
		BanMode:                "off",
	}
	bounds := retryDelayBounds(policy, 50)
	if bounds.NominalDelayMS != 900000 {
		t.Fatalf("expected saturation cap at retry_max_delay_ms 900000, got %d", bounds.NominalDelayMS)
	}
	if bounds.JitterMaxDelayMS != 900000 {
		t.Fatalf("expected jitter max capped at retry_max_delay_ms, got %d", bounds.JitterMaxDelayMS)
	}
	if bounds.JitterMinDelayMS != 720000 {
		t.Fatalf("expected jitter min 720000, got %d", bounds.JitterMinDelayMS)
	}
}

func TestRetryDelayBoundsZeroBaseAndDisabled(t *testing.T) {
	disabled := runtimeFeedbackPolicy{Enabled: false, BaseDelayMS: 1000, MaxDelayMS: 900000}
	if bounds := retryDelayBounds(disabled, 1); bounds.NominalDelayMS != 0 || bounds.JitterMinDelayMS != 0 || bounds.JitterMaxDelayMS != 0 {
		t.Fatalf("expected disabled policy to yield zero bounds, got %+v", bounds)
	}
	zeroBase := runtimeFeedbackPolicy{Enabled: true, BaseDelayMS: 0, MaxDelayMS: 900000, JitterRatio: 0.2}
	if bounds := retryDelayBounds(zeroBase, 1); bounds.NominalDelayMS != 0 {
		t.Fatalf("expected zero base delay to yield zero nominal, got %+v", bounds)
	}
}

func TestPreviewRetryCycleBalancedStopsAtCycleExhaustion(t *testing.T) {
	strategy := DefaultStrategyPayload(CanonicalStrategySpec{Name: "Default fill-first routing", LegacyStrategyType: "fill-first"})
	result := PreviewRetryCycle(feedbackPolicyForStrategy(t, strategy))

	if result.TerminationReason != PreviewTerminationCycleExhausted || result.HasMore {
		t.Fatalf("expected balanced preview to terminate with cycle exhaustion and no more steps, got %+v", result)
	}
	if result.ShownStepCount != 3 || len(result.Steps) != 3 {
		t.Fatalf("expected 3 shown steps, got %d", result.ShownStepCount)
	}
	if result.CycleExhaustionAfterAttempt != 3 {
		t.Fatalf("expected cycle exhaustion after attempt 3, got %d", result.CycleExhaustionAfterAttempt)
	}
	for index, step := range result.Steps {
		if step.FailureOrdinal != index+1 || step.CycleRetryAttempt != index+1 || step.CumulativeRetryAttempt != index+1 {
			t.Fatalf("unexpected step ordinals at %d: %+v", index, step)
		}
		if step.CycleExhausted != (index == 2) {
			t.Fatalf("expected cycle exhausted only on final step, got %+v", step)
		}
		if step.BanTransition != nil {
			t.Fatalf("expected no ban transition for balanced policy, got %+v", step.BanTransition)
		}
	}
	if result.BanProjection.Mode != "off" || result.BanProjection.TransitionAtCumulativeFailure != nil {
		t.Fatalf("expected off ban projection with null transition, got %+v", result.BanProjection)
	}
}

func TestPreviewRetryCycleConservativeProjectsBanBeyondCycle(t *testing.T) {
	strategy := RuntimeStrategy{
		LegacyStrategyType:                 stringPointer("fill-first"),
		FailureStatusCodes:                 []int{403, 422, 429, 500, 502, 503, 504, 529},
		BanMode:                            "temporary",
		RetryBaseDelayMS:                   120000,
		RetryBackoffMultiplier:             2.0,
		RetryJitterRatio:                   0.2,
		RetryMaxDelayMS:                    1800000,
		CycleRetryAttemptLimit:             2,
		BanCumulativeRetryAttemptThreshold: 4,
		BanDurationSeconds:                 3600,
	}
	result := PreviewRetryCycle(feedbackPolicyForStrategy(t, strategy))

	if result.TerminationReason != PreviewTerminationCycleExhausted || result.HasMore || result.ShownStepCount != 2 {
		t.Fatalf("expected conservative preview to stop at cycle 2, got %+v", result)
	}
	for _, step := range result.Steps {
		if step.BanTransition != nil {
			t.Fatalf("expected no ban transition inside the two-step cycle, got %+v", step)
		}
	}
	if result.BanProjection.Mode != "temporary" ||
		result.BanProjection.CumulativeRetryAttemptThreshold != 4 ||
		result.BanProjection.TransitionAtCumulativeFailure == nil ||
		*result.BanProjection.TransitionAtCumulativeFailure != 4 ||
		result.BanProjection.DurationSeconds != 3600 {
		t.Fatalf("expected temporary ban projection at cumulative 4, got %+v", result.BanProjection)
	}
}

func TestPreviewRetryCycleAggressiveStopsAtFiveWithCycleExhaustion(t *testing.T) {
	strategy := RuntimeStrategy{
		LegacyStrategyType:     stringPointer("fill-first"),
		FailureStatusCodes:     []int{403, 422, 429, 500, 502, 503, 504, 529},
		BanMode:                "off",
		RetryBaseDelayMS:       10000,
		RetryBackoffMultiplier: 1.5,
		RetryJitterRatio:       0.2,
		RetryMaxDelayMS:        120000,
		CycleRetryAttemptLimit: 5,
	}
	result := PreviewRetryCycle(feedbackPolicyForStrategy(t, strategy))
	if result.TerminationReason != PreviewTerminationCycleExhausted || result.HasMore || result.ShownStepCount != 5 {
		t.Fatalf("expected aggressive preview to stop at cycle 5 with cycle exhaustion, got %+v", result)
	}
	if !result.Steps[4].CycleExhausted {
		t.Fatalf("expected final step cycle exhausted, got %+v", result.Steps[4])
	}
}

func TestPreviewRetryCycleFiveStepLimitTruncatesLongCycle(t *testing.T) {
	strategy := RuntimeStrategy{
		LegacyStrategyType:                 stringPointer("fill-first"),
		FailureStatusCodes:                 []int{403, 422, 429, 500, 502, 503, 504, 529},
		BanMode:                            "temporary",
		RetryBaseDelayMS:                   10000,
		RetryBackoffMultiplier:             1.5,
		RetryJitterRatio:                   0.2,
		RetryMaxDelayMS:                    120000,
		CycleRetryAttemptLimit:             8,
		BanCumulativeRetryAttemptThreshold: 12,
		BanDurationSeconds:                 3600,
	}
	result := PreviewRetryCycle(feedbackPolicyForStrategy(t, strategy))
	if result.TerminationReason != PreviewTerminationFiveStepLimit || !result.HasMore || result.ShownStepCount != 5 {
		t.Fatalf("expected five-step cap with has_more, got %+v", result)
	}
	for _, step := range result.Steps {
		if step.CycleExhausted || step.BanTransition != nil {
			t.Fatalf("expected no termination inside truncated steps, got %+v", step)
		}
	}
	if result.CycleExhaustionAfterAttempt != 8 {
		t.Fatalf("expected cycle exhaustion after attempt 8, got %d", result.CycleExhaustionAfterAttempt)
	}
}

func TestPreviewRetryCycleBanTransitionAtCycleLimit(t *testing.T) {
	strategy := RuntimeStrategy{
		LegacyStrategyType:                 stringPointer("fill-first"),
		FailureStatusCodes:                 []int{403, 422, 429, 500, 502, 503, 504, 529},
		BanMode:                            "temporary",
		RetryBaseDelayMS:                   60000,
		RetryBackoffMultiplier:             2.0,
		RetryJitterRatio:                   0.2,
		RetryMaxDelayMS:                    900000,
		CycleRetryAttemptLimit:             3,
		BanCumulativeRetryAttemptThreshold: 3,
		BanDurationSeconds:                 900,
	}
	result := PreviewRetryCycle(feedbackPolicyForStrategy(t, strategy))
	if result.TerminationReason != PreviewTerminationBanTransition || result.HasMore || result.ShownStepCount != 3 {
		t.Fatalf("expected ban transition at cumulative 3 to terminate, got %+v", result)
	}
	last := result.Steps[2]
	if !last.CycleExhausted || last.BanTransition == nil || last.BanTransition.Mode != "temporary" || last.BanTransition.DurationSeconds != 900 {
		t.Fatalf("expected final step with cycle exhaustion and ban transition, got %+v", last)
	}
}

func TestPreviewRetryCycleUntilResetProjectsZeroDuration(t *testing.T) {
	strategy := RuntimeStrategy{
		LegacyStrategyType:                 stringPointer("fill-first"),
		FailureStatusCodes:                 []int{403, 422, 429, 500, 502, 503, 504, 529},
		BanMode:                            "until_reset",
		RetryBaseDelayMS:                   60000,
		RetryBackoffMultiplier:             2.0,
		RetryJitterRatio:                   0.2,
		RetryMaxDelayMS:                    900000,
		CycleRetryAttemptLimit:             2,
		BanCumulativeRetryAttemptThreshold: 2,
		BanDurationSeconds:                 0,
	}
	result := PreviewRetryCycle(feedbackPolicyForStrategy(t, strategy))
	if result.BanProjection.Mode != "until_reset" || result.BanProjection.DurationSeconds != 0 {
		t.Fatalf("expected until_reset projection with zero duration, got %+v", result.BanProjection)
	}
	if result.Steps[1].BanTransition == nil || result.Steps[1].BanTransition.DurationSeconds != 0 {
		t.Fatalf("expected until_reset transition with zero duration, got %+v", result.Steps[1].BanTransition)
	}
}

func TestRuntimeRetryDelayMillisecondsUsesSharedBounds(t *testing.T) {
	restore := setRuntimeRetryJitterOffsetForTest(func(maxOffsetMS int) int {
		if maxOffsetMS != 100 {
			t.Fatalf("expected max jitter offset 100ms, got %d", maxOffsetMS)
		}
		return -25
	})
	defer restore()

	policy := RuntimeStrategy{RetryBaseDelayMS: 1000, RetryJitterRatio: 0.1, CycleRetryAttemptLimit: 3}.FeedbackPolicy()
	if got := retryDelayMilliseconds(policy, 1); got != 975 {
		t.Fatalf("expected deterministic jittered retry delay 975ms, got %d", got)
	}
}

func TestCanonicalStrategyPayloadAndSpecsReconcile(t *testing.T) {
	specs := CanonicalDefaultStrategySpecs()
	if len(specs) != 3 {
		t.Fatalf("expected 3 canonical specs, got %d", len(specs))
	}
	for _, spec := range specs {
		payload := DefaultStrategyPayload(spec)
		if payload.Name != spec.Name {
			t.Fatalf("expected canonical name %q, got %q", spec.Name, payload.Name)
		}
		if payload.LegacyStrategyType == nil || *payload.LegacyStrategyType != spec.LegacyStrategyType {
			t.Fatalf("expected canonical type %q, got %v", spec.LegacyStrategyType, payload.LegacyStrategyType)
		}
		if !StrategyMatchesCanonical(payload, spec) {
			t.Fatalf("expected canonical payload to match its spec %q", spec.Name)
		}
	}
	edited := DefaultStrategyPayload(specs[1])
	edited.RetryBaseDelayMS = 5000
	if StrategyMatchesCanonical(edited, specs[1]) {
		t.Fatalf("expected edited payload to stop matching the canonical spec")
	}
	wrongType := DefaultStrategyPayload(specs[1])
	wrongType.LegacyStrategyType = stringPointer("single")
	if StrategyMatchesCanonical(wrongType, specs[1]) {
		t.Fatalf("expected wrong subtype to stop matching the canonical spec")
	}
}
