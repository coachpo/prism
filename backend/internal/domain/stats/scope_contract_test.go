package stats

import "testing"

func TestFinalExecutionAndRouteAttemptCalibersAreDisjoint(t *testing.T) {
	final := CaliberForScope(ScopeFinal)
	if final.Grain != "finalized_execution" || final.IdentityBasis != "final_target_model_id" || final.CostBasis != "served_final_trusted_cost" || len(final.Datasets) != 2 {
		t.Fatalf("final_execution caliber = %#v", final)
	}
	attempt := CaliberForScope(ScopeRouteAttempt)
	if attempt.Grain != "upstream_attempt" || attempt.IdentityBasis != "attempt_target_model_id" || attempt.CostBasis != "none" || len(attempt.Datasets) != 1 || attempt.Datasets[0] != "request_logs" {
		t.Fatalf("route_attempt caliber = %#v", attempt)
	}
}

func TestScopeGroupAndFilterGrammarRejectsAmbiguousModelKeys(t *testing.T) {
	for _, scope := range []string{ScopeIngress, ScopeFinal, ScopeRouteAttempt} {
		if _, err := ValidateGroupBy(scope, "model"); err == nil {
			t.Fatalf("scope %s accepted retired group_by=model", scope)
		}
	}
	valid := map[string]string{ScopeIngress: GroupIngressModel, ScopeFinal: GroupFinalTargetModel, ScopeRouteAttempt: GroupAttemptTargetModel}
	for scope, group := range valid {
		if got, err := ValidateGroupBy(scope, group); err != nil || got != group {
			t.Fatalf("ValidateGroupBy(%s, %s) = %q, %v", scope, group, got, err)
		}
	}
	if err := ValidateScopeQueryKeys(ScopeFinal, []string{"model_id"}); err == nil {
		t.Fatal("final_execution accepted retired model_id filter")
	}
	if err := ValidateScopeQueryKeys(ScopeRouteAttempt, []string{"pricing_status"}); err == nil {
		t.Fatal("route_attempt accepted a cost-cohort filter")
	}
}

func TestScopeMetricMatrixDefaultsAndRejectsCrossScopeMetrics(t *testing.T) {
	wantDefaults := map[string]string{
		ScopeIngress: MetricRequests, ScopeFinal: MetricRequests, ScopeRouteAttempt: MetricAttempts,
	}
	for scope, want := range wantDefaults {
		if got, err := NormalizeMetric(scope, ""); err != nil || got != want {
			t.Fatalf("NormalizeMetric(%q, empty) = %q, %v; want %q", scope, got, err, want)
		}
	}
	for _, item := range []struct{ scope, metric string }{
		{ScopeIngress, MetricOutputRate}, {ScopeFinal, MetricFinalAttemptLatency}, {ScopeRouteAttempt, MetricAttemptLatency},
	} {
		if got, err := NormalizeMetric(item.scope, item.metric); err != nil || got != item.metric {
			t.Fatalf("NormalizeMetric(%q, %q) = %q, %v", item.scope, item.metric, got, err)
		}
	}
	for _, item := range []struct{ scope, metric string }{
		{ScopeIngress, MetricAttempts}, {ScopeFinal, MetricTTFT}, {ScopeRouteAttempt, MetricCost},
	} {
		if _, err := NormalizeMetric(item.scope, item.metric); err == nil {
			t.Fatalf("scope %q accepted cross-scope metric %q", item.scope, item.metric)
		}
	}
}

func TestTrustedZeroAndMissingCostRemainDistinct(t *testing.T) {
	trustedZero := &modelMetricAccumulator{observations: 1, costSamples: 1, cost: 0}
	block := metricBlockFromAccumulator(ScopeFinal, trustedZero)
	if block.KnownCostMicros == nil || *block.KnownCostMicros != 0 || block.Samples.CostSampleCount != 1 {
		t.Fatalf("trusted zero block = %#v", block)
	}
	missing := metricBlockFromAccumulator(ScopeFinal, &modelMetricAccumulator{observations: 1, costMissing: 1})
	if missing.KnownCostMicros != nil || missing.Samples.CostMissingCount != 1 {
		t.Fatalf("missing cost block = %#v", missing)
	}
	attempt := metricBlockFromAccumulator(ScopeRouteAttempt, trustedZero)
	if attempt.KnownCostMicros != nil || attempt.Caliber.CostBasis != "none" {
		t.Fatalf("route_attempt claimed cost: %#v", attempt)
	}
}

func TestRouteAttemptSummaryUsesAttemptDurationAndMissingCounts(t *testing.T) {
	duration := 25
	observations := []scopedStatObservation{
		{TargetModelID: stringPointer("B"), AttemptResult: stringPointer("http_error"), LatencyMS: &duration},
		{TargetModelID: stringPointer("C"), AttemptResult: stringPointer("completed"), Success: true},
	}
	result := buildScopedStatsSummary(observations, ScopeRouteAttempt, GroupAttemptTargetModel)
	if result.TotalRequests != 2 || result.SuccessCount != 1 || result.Samples.LatencySampleCount != 1 || result.Samples.LatencyMissingCount != 1 {
		t.Fatalf("route_attempt summary = %#v", result)
	}
}
