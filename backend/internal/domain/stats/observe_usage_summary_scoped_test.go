package stats

import "testing"

func TestProjectScopedUsageEvidencePreservesSamplesAndMissingValues(t *testing.T) {
	rate := 2.5
	result := UsageSummaryResult{}
	projectScopedUsageEvidence(&result, []scopedStatObservation{
		{
			InputTokens: 10, HasInputTokens: true,
			OutputTokens: 4, HasOutputTokens: true,
			TotalTokens: 20, HasTotalTokens: true,
			CacheReadInputTokens: 3, HasCacheReadInputTokens: true,
			ReasoningTokens: 0, HasReasoningTokens: true,
			CacheBasisEligible: true,
			OutputRateTPS:      &rate,
		},
		{
			OutputTokens: 0, HasOutputTokens: true,
			CacheCreationInputTokens: 5, HasCacheCreationInputTokens: true,
		},
	})

	if result.InputTokenSampleCount != 1 || result.InputTokens == nil || *result.InputTokens != 10 {
		t.Fatalf("unexpected input-token projection: %+v", result)
	}
	if result.OutputTokenSampleCount != 2 || result.OutputTokens == nil || *result.OutputTokens != 4 {
		t.Fatalf("unexpected output-token projection: %+v", result)
	}
	if result.CacheReadInputTokenSampleCount != 1 || result.CacheReadInputTokens == nil || *result.CacheReadInputTokens != 3 {
		t.Fatalf("unexpected cache-read projection: %+v", result)
	}
	if result.CacheCreationInputTokenSampleCount != 1 || result.CacheCreationInputTokens == nil || *result.CacheCreationInputTokens != 5 {
		t.Fatalf("unexpected cache-creation projection: %+v", result)
	}
	if result.ReasoningTokenSampleCount != 1 || result.ReasoningTokens == nil || *result.ReasoningTokens != 0 {
		t.Fatalf("measured zero reasoning tokens were not preserved: %+v", result)
	}
	if result.TotalTokenSampleCount != 1 || result.TotalTokens == nil || *result.TotalTokens != 20 {
		t.Fatalf("unexpected total-token projection: %+v", result)
	}
	if result.OutputRateSampleCount != 1 || result.AvgOutputRateTPS == nil || *result.AvgOutputRateTPS != rate {
		t.Fatalf("unexpected output-rate projection: %+v", result)
	}
	if result.CacheBasisRequestCount != 1 || result.CacheBasisInputTokens == nil || *result.CacheBasisInputTokens != 10 || result.CacheBasisCacheReadTokens == nil || *result.CacheBasisCacheReadTokens != 3 {
		t.Fatalf("unexpected cache-basis projection: %+v", result)
	}
	if result.CacheBasisCacheCreationTokens == nil || *result.CacheBasisCacheCreationTokens != 0 {
		t.Fatalf("structurally absent cache creation must be a measured basis zero: %+v", result)
	}
}

func TestProjectScopedUsageEvidenceLeavesNoSamplesNull(t *testing.T) {
	result := UsageSummaryResult{}
	projectScopedUsageEvidence(&result, []scopedStatObservation{{}})

	if result.InputTokens != nil || result.OutputTokens != nil || result.CacheReadInputTokens != nil || result.CacheCreationInputTokens != nil || result.ReasoningTokens != nil || result.TotalTokens != nil {
		t.Fatalf("missing token components must stay null: %+v", result)
	}
	if result.AvgOutputRateTPS != nil || result.OutputRateSampleCount != 0 {
		t.Fatalf("missing output-rate evidence must stay null with zero samples: %+v", result)
	}
	if result.CacheBasisRequestCount != 0 || result.CacheBasisInputTokens != nil || result.CacheBasisCacheReadTokens != nil || result.CacheBasisCacheCreationTokens != nil {
		t.Fatalf("missing cache-basis evidence must stay null with zero samples: %+v", result)
	}

	zeroRate := 0.0
	measuredZero := UsageSummaryResult{}
	projectScopedUsageEvidence(&measuredZero, []scopedStatObservation{{
		HasInputTokens: true, HasOutputTokens: true, HasTotalTokens: true,
		HasCacheReadInputTokens: true, HasCacheCreationInputTokens: true, HasReasoningTokens: true,
		CacheBasisEligible: true, OutputRateTPS: &zeroRate,
	}})
	if measuredZero.InputTokens == nil || *measuredZero.InputTokens != 0 || measuredZero.OutputTokens == nil || *measuredZero.OutputTokens != 0 || measuredZero.TotalTokens == nil || *measuredZero.TotalTokens != 0 {
		t.Fatalf("measured zero token totals must stay non-null: %+v", measuredZero)
	}
	if measuredZero.CacheReadInputTokens == nil || measuredZero.CacheCreationInputTokens == nil || measuredZero.ReasoningTokens == nil {
		t.Fatalf("measured zero cache/reasoning components must stay non-null: %+v", measuredZero)
	}
	if measuredZero.AvgOutputRateTPS == nil || *measuredZero.AvgOutputRateTPS != 0 || measuredZero.OutputRateSampleCount != 1 {
		t.Fatalf("measured zero output rate must stay distinguishable from missing: %+v", measuredZero)
	}
	if measuredZero.CacheBasisRequestCount != 1 || measuredZero.CacheBasisInputTokens == nil || measuredZero.CacheBasisCacheReadTokens == nil || measuredZero.CacheBasisCacheCreationTokens == nil {
		t.Fatalf("measured zero cache basis must stay distinguishable from missing: %+v", measuredZero)
	}
}
