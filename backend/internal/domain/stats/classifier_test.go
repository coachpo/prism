package stats

import (
	"testing"
)

func strPtr(value string) *string { return &value }

func TestClassifyOutcomeDetailTruthTable(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		outcome    *string
		wantDetail OutcomeDetail
		wantFinal  FinalResult
	}{
		{name: "2xx non-stream completed", status: 200, outcome: strPtr("not_streaming"), wantDetail: OutcomeDetailCompleted, wantFinal: FinalResultCompleted},
		{name: "2xx stream completed", status: 200, outcome: strPtr("completed"), wantDetail: OutcomeDetailCompleted, wantFinal: FinalResultCompleted},
		{name: "non-2xx + normal stream outcome", status: 503, outcome: strPtr("completed"), wantDetail: OutcomeDetailHTTPError, wantFinal: FinalResultFailed},
		{name: "2xx provider incomplete null kind", status: 200, outcome: strPtr("provider_incomplete"), wantDetail: OutcomeDetailStreamError, wantFinal: FinalResultFailed},
		{name: "2xx upstream read error", status: 200, outcome: strPtr("upstream_read_error"), wantDetail: OutcomeDetailStreamError, wantFinal: FinalResultFailed},
		{name: "2xx client disconnected", status: 200, outcome: strPtr("client_disconnected"), wantDetail: OutcomeDetailClientDisconnected, wantFinal: FinalResultClientDisconnected},
		{name: "non-2xx + abnormal stream metadata", status: 400, outcome: strPtr("provider_incomplete"), wantDetail: OutcomeDetailHTTPError, wantFinal: FinalResultFailed},
		{name: "2xx unknown outcome", status: 200, outcome: strPtr("unknown"), wantDetail: OutcomeDetailStreamError, wantFinal: FinalResultFailed},
		{name: "null outcome 2xx", status: 200, outcome: nil, wantDetail: OutcomeDetailCompleted, wantFinal: FinalResultCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detail := ClassifyOutcomeDetail(tc.status, tc.outcome)
			if detail != tc.wantDetail {
				t.Fatalf("expected outcome detail %q, got %q", tc.wantDetail, detail)
			}
			if final := ClassifyFinalResult(detail); final != tc.wantFinal {
				t.Fatalf("expected final result %q, got %q", tc.wantFinal, final)
			}
		})
	}
}

func TestOutcomeCountInvariants(t *testing.T) {
	counts := OutcomeCounts{}
	for _, detail := range []OutcomeDetail{
		OutcomeDetailCompleted,
		OutcomeDetailCompleted,
		OutcomeDetailHTTPError,
		OutcomeDetailStreamError,
		OutcomeDetailStreamError,
		OutcomeDetailClientDisconnected,
	} {
		MergeOutcomeCounts(&counts, detail)
	}
	if counts.RequestCount != 6 || counts.CompletedCount != 2 || counts.HTTPErrorCount != 1 || counts.StreamErrorCount != 2 || counts.ClientDisconnectedCount != 1 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
	if counts.HTTPSuccessCount() != 5 || counts.HTTPFailedCount() != 1 || counts.FailedCount() != 3 {
		t.Fatalf("invariant violation: success=%d failed=%d", counts.HTTPSuccessCount(), counts.FailedCount())
	}
}

func TestClassifyPricingStatus(t *testing.T) {
	cases := []struct {
		name       string
		success    bool
		priced     bool
		reason     *string
		trust      PricingEvidenceTrust
		wantStatus PricingStatus
		wantReason string
	}{
		{name: "non-2xx ineligible", success: false, priced: false, reason: nil, trust: PricingEvidenceTrustTrusted, wantStatus: PricingStatusIneligible},
		{name: "2xx priced", success: true, priced: true, reason: nil, trust: PricingEvidenceTrustTrusted, wantStatus: PricingStatusPriced},
		{name: "2xx unpriced disabled", success: true, priced: false, reason: strPtr(UnpricedReasonPricingDisabled), trust: PricingEvidenceTrustTrusted, wantStatus: PricingStatusUnpriced, wantReason: UnpricedReasonPricingDisabled},
		{name: "2xx unpriced missing usage", success: true, priced: false, reason: strPtr(UnpricedReasonMissingTokenUsage), trust: PricingEvidenceTrustTrusted, wantStatus: PricingStatusUnpriced, wantReason: UnpricedReasonMissingTokenUsage},
		{name: "2xx unpriced stream usage", success: true, priced: false, reason: strPtr(UnpricedReasonStreamUsageUnavailable), trust: PricingEvidenceTrustTrusted, wantStatus: PricingStatusUnpriced, wantReason: UnpricedReasonStreamUsageUnavailable},
		{name: "2xx unpriced missing data", success: true, priced: false, reason: strPtr(UnpricedReasonMissingPriceData), trust: PricingEvidenceTrustTrusted, wantStatus: PricingStatusUnpriced, wantReason: UnpricedReasonMissingPriceData},
		{name: "2xx legacy untrusted unknown", success: true, priced: false, reason: nil, trust: PricingEvidenceTrustLegacyUntrusted, wantStatus: PricingStatusUnknown},
		{name: "2xx no evidence unknown", success: true, priced: false, reason: nil, trust: PricingEvidenceTrustTrusted, wantStatus: PricingStatusUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, reason := ClassifyPricingStatus(tc.success, tc.priced, tc.reason, tc.trust)
			if status != tc.wantStatus || reason != tc.wantReason {
				t.Fatalf("expected %q/%q, got %q/%q", tc.wantStatus, tc.wantReason, status, reason)
			}
		})
	}
}

func TestPricingCoverageStates(t *testing.T) {
	cases := []struct {
		name      string
		eligible  int
		priced    int
		unpriced  int
		unknown   int
		wantState string
	}{
		{name: "complete", eligible: 10, priced: 10, wantState: "complete"},
		{name: "partial", eligible: 10, priced: 7, unpriced: 3, wantState: "partial"},
		{name: "no trusted cost", eligible: 10, unpriced: 10, wantState: "no_trusted_cost"},
		{name: "no eligible", wantState: "no_eligible"},
		{name: "partial with unknown", eligible: 5, priced: 2, unknown: 3, wantState: "partial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := PricingCoverageState(tc.eligible, tc.priced, tc.unpriced, tc.unknown)
			if state != tc.wantState {
				t.Fatalf("expected %q, got %q", tc.wantState, state)
			}
		})
	}
}

func TestPricingReconciliationInvariants(t *testing.T) {
	reconciliation := NewPricingReconciliation()
	// 24-row fixture: 17 ineligible, 5 STREAM_USAGE_UNAVAILABLE,
	// 2 MISSING_TOKEN_USAGE, 0 PRICING_DISABLED, 0 MISSING_PRICE_DATA,
	// 0 unknown, priced = eligible - 7.
	for range 17 {
		MergePricingReconciliation(&reconciliation, PricingStatusIneligible, "")
	}
	for range 5 {
		MergePricingReconciliation(&reconciliation, PricingStatusUnpriced, UnpricedReasonStreamUsageUnavailable)
	}
	for range 2 {
		MergePricingReconciliation(&reconciliation, PricingStatusUnpriced, UnpricedReasonMissingTokenUsage)
	}
	for range 18 {
		MergePricingReconciliation(&reconciliation, PricingStatusPriced, "")
	}
	FinalizePricingReconciliation(&reconciliation)
	if reconciliation.IneligibleRequestCount != 17 {
		t.Fatalf("expected 17 ineligible, got %d", reconciliation.IneligibleRequestCount)
	}
	if reconciliation.UnpricedReasonCounts[UnpricedReasonStreamUsageUnavailable] != 5 ||
		reconciliation.UnpricedReasonCounts[UnpricedReasonMissingTokenUsage] != 2 ||
		reconciliation.UnpricedReasonCounts[UnpricedReasonPricingDisabled] != 0 ||
		reconciliation.UnpricedReasonCounts[UnpricedReasonMissingPriceData] != 0 {
		t.Fatalf("unexpected reason counts: %+v", reconciliation.UnpricedReasonCounts)
	}
	if reconciliation.UnpricedRequestCount != 7 {
		t.Fatalf("expected unpriced sum 7, got %d", reconciliation.UnpricedRequestCount)
	}
	if reconciliation.EligibleRequestCount != 25 || reconciliation.PricedRequestCount != 18 {
		t.Fatalf("expected eligible 25 / priced 18, got %+v", reconciliation)
	}
	if reconciliation.PricingCoverageState != "partial" {
		t.Fatalf("expected partial coverage, got %q", reconciliation.PricingCoverageState)
	}
}

func TestCostSegmentKey(t *testing.T) {
	epoch := 3
	if key := CostSegmentKeyFor(&epoch, "USD", true); key != "e.3" {
		t.Fatalf("expected e.3, got %q", key)
	}
	if key := CostSegmentKeyFor(nil, "usd", true); key != "l.USD" {
		t.Fatalf("expected l.USD, got %q", key)
	}
	if key := CostSegmentKeyFor(nil, "USD", false); key != "l.__unknown__" {
		t.Fatalf("expected l.__unknown__, got %q", key)
	}
	if key := CostSegmentKeyFor(nil, "US", true); key != "l.__unknown__" {
		t.Fatalf("expected l.__unknown__ for short code, got %q", key)
	}
}

func TestDetailPricingUsesCanonicalCostSegmentKey(t *testing.T) {
	stringPointer := func(value string) *string { return &value }
	epoch := 4
	projection := buildDetailPricing(requestLogDetailRow{
		ReportingCurrencyEpoch: &epoch,
		PricingStatus:          "priced",
	})
	if projection.CostSegmentKey == nil || *projection.CostSegmentKey != "e.4" {
		t.Fatalf("expected detail epoch segment key e.4, got %#v", projection.CostSegmentKey)
	}

	legacy := buildDetailPricing(requestLogDetailRow{
		ReportCurrencyCode: stringPointer(" usd "),
		PricingStatus:      "priced",
	})
	if legacy.CostSegmentKey == nil || *legacy.CostSegmentKey != "l.USD" {
		t.Fatalf("expected detail legacy segment key l.USD, got %#v", legacy.CostSegmentKey)
	}

	invalid := buildDetailPricing(requestLogDetailRow{
		ReportCurrencyCode: stringPointer("US"),
		PricingStatus:      "priced",
	})
	if invalid.CostSegmentKey == nil || *invalid.CostSegmentKey != "l.__unknown__" {
		t.Fatalf("expected invalid detail currency to use unknown key, got %#v", invalid.CostSegmentKey)
	}
}
