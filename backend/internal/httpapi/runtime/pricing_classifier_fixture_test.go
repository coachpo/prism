package runtime

import (
	"strings"
	"testing"
)

// TestPricingClassifierResearchFixtureReconciliation is the fixed 24-row
// research fixture (SPEC 3.6): 17 non-2xx ineligible rows, 5
// STREAM_USAGE_UNAVAILABLE, 2 MISSING_TOKEN_USAGE, 0 PRICING_DISABLED,
// 0 MISSING_PRICE_DATA, 0 unknown; sum(unpriced_reason_counts) = 7.
// Dashboard, Analytics, spending, Requests filter, CSV, JSON export, raw
// rollup and hybrid merge must consume the same persisted projection.
func TestPricingClassifierResearchFixtureReconciliation(t *testing.T) {
	reportCurrency := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	template := runtimePricingTemplateForTest(nil)

	// 17 non-2xx rows: 400/403/404/429/503 with zero tokens.
	non2xxStatuses := []int{400, 403, 403, 404, 404, 404, 429, 429, 429, 429, 503, 503, 503, 503, 503, 502, 408}
	if len(non2xxStatuses) != 17 {
		t.Fatalf("fixture must contain exactly 17 non-2xx rows, got %d", len(non2xxStatuses))
	}

	counts := map[string]int{
		"ineligible":               0,
		"PRICING_DISABLED":         0,
		"MISSING_TOKEN_USAGE":      0,
		"STREAM_USAGE_UNAVAILABLE": 0,
		"MISSING_PRICE_DATA":       0,
		"unknown":                  0,
	}
	reasonSum := 0

	for _, status := range non2xxStatuses {
		result := classifyRuntimePricing(status, reportCurrency, template, responseUsage{}, runtimeStreamOutcomeNotStreaming)
		if result.PricingStatus != runtimePricingStatusIneligible {
			t.Fatalf("non-2xx status %d must be ineligible, got %+v", status, result)
		}
		if result.UnpricedReason != nil {
			t.Fatalf("non-2xx status %d must carry no reason, got %+v", status, result)
		}
		counts["ineligible"]++
	}

	// 5 2xx interrupted streams without usage -> STREAM_USAGE_UNAVAILABLE.
	interruptedOutcomes := []string{
		runtimeStreamOutcomeProviderIncomplete,
		runtimeStreamOutcomeClientDisconnected,
		runtimeStreamOutcomeUpstreamReadError,
		runtimeStreamOutcomeUpstreamEndedWithoutTerminal,
		runtimeStreamOutcomeUnknown,
	}
	if len(interruptedOutcomes) != 5 {
		t.Fatalf("fixture must contain exactly 5 interrupted streams, got %d", len(interruptedOutcomes))
	}
	for _, outcome := range interruptedOutcomes {
		result := classifyRuntimePricing(200, reportCurrency, template, responseUsage{}, outcome)
		if result.PricingStatus != runtimePricingStatusUnpriced || result.UnpricedReason == nil || *result.UnpricedReason != runtimeUnpricedReasonStreamUsageUnavailable {
			t.Fatalf("interrupted stream outcome %q must be STREAM_USAGE_UNAVAILABLE, got %+v", outcome, result)
		}
		counts["STREAM_USAGE_UNAVAILABLE"]++
		reasonSum++
	}

	// 2 2xx completed requests without usage -> MISSING_TOKEN_USAGE.
	for index := 0; index < 2; index++ {
		result := classifyRuntimePricing(200, reportCurrency, template, responseUsage{}, runtimeStreamOutcomeCompleted)
		if result.PricingStatus != runtimePricingStatusUnpriced || result.UnpricedReason == nil || *result.UnpricedReason != runtimeUnpricedReasonMissingUsage {
			t.Fatalf("completed 2xx without usage must be MISSING_TOKEN_USAGE, got %+v", result)
		}
		counts["MISSING_TOKEN_USAGE"]++
		reasonSum++
	}

	want := map[string]int{
		"ineligible":               17,
		"STREAM_USAGE_UNAVAILABLE": 5,
		"MISSING_TOKEN_USAGE":      2,
		"PRICING_DISABLED":         0,
		"MISSING_PRICE_DATA":       0,
		"unknown":                  0,
	}
	for key, value := range want {
		if counts[key] != value {
			t.Fatalf("fixture reconciliation %s: want %d got %d (all: %+v)", key, value, counts[key], counts)
		}
	}
	if reasonSum != 7 {
		t.Fatalf("fixture reason sum must be 7, got %d", reasonSum)
	}
	if counts["ineligible"]+counts["STREAM_USAGE_UNAVAILABLE"]+counts["MISSING_TOKEN_USAGE"] != 24 {
		t.Fatalf("fixture total must be 24, got %d", counts["ineligible"]+counts["STREAM_USAGE_UNAVAILABLE"]+counts["MISSING_TOKEN_USAGE"])
	}
}

// TestPricingClassifierNullZeroValueSemantics pins the NULL/0/value
// component contract (SPEC 4.3): tokens=0 with null specialty price is not a
// blocker; tokens>0 with null price is MISSING_PRICE_DATA + missing_component;
// explicit "0" with tokens>0 is priced with zero component cost.
func TestPricingClassifierNullZeroValueSemantics(t *testing.T) {
	reportCurrency := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	inputTokens := 10
	outputTokens := 10
	zero := 0
	positive := 5

	nullSpecialty := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.ReasoningPrice = ""
	})
	result := classifyRuntimePricing(200, reportCurrency, nullSpecialty, responseUsage{
		InputTokens: &inputTokens, OutputTokens: &outputTokens, ReasoningTokens: &zero,
	}, runtimeStreamOutcomeCompleted)
	if result.PricingStatus != runtimePricingStatusPriced {
		t.Fatalf("tokens=0 with null specialty price must not block pricing, got %+v", result)
	}
	if result.ReasoningCostMicros == nil || *result.ReasoningCostMicros != 0 {
		t.Fatalf("expected zero reasoning cost, got %+v", result)
	}

	missing := classifyRuntimePricing(200, reportCurrency, nullSpecialty, responseUsage{
		InputTokens: &inputTokens, OutputTokens: &outputTokens, ReasoningTokens: &positive,
	}, runtimeStreamOutcomeCompleted)
	if missing.PricingStatus != runtimePricingStatusUnpriced || missing.UnpricedReason == nil || *missing.UnpricedReason != runtimeUnpricedReasonMissingData {
		t.Fatalf("tokens>0 with null price must be MISSING_PRICE_DATA, got %+v", missing)
	}
	if missing.PricingResolutionKind == nil || *missing.PricingResolutionKind != runtimePricingResolutionMissingComponent {
		t.Fatalf("expected missing_component resolution, got %+v", missing)
	}
	if len(missing.MissingPriceComponents) != 1 || missing.MissingPriceComponents[0] != "reasoning_price" {
		t.Fatalf("expected [reasoning_price] components, got %+v", missing.MissingPriceComponents)
	}

	explicitZero := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.ReasoningPrice = "0"
	})
	priced := classifyRuntimePricing(200, reportCurrency, explicitZero, responseUsage{
		InputTokens: &inputTokens, OutputTokens: &outputTokens, ReasoningTokens: &positive,
	}, runtimeStreamOutcomeCompleted)
	if priced.PricingStatus != runtimePricingStatusPriced || priced.ReasoningCostMicros == nil || *priced.ReasoningCostMicros != 0 {
		t.Fatalf("explicit 0 with tokens>0 must price with zero component cost, got %+v", priced)
	}
}

// TestPricingClassifierResolutionPriority pins the fixed resolution priority
// (SPEC 3.4 step 5): unit first, then missing component, then incoherence.
func TestPricingClassifierResolutionPriority(t *testing.T) {
	reportCurrency := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	inputTokens := 10
	outputTokens := 10
	positive := 5
	usage := responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens, ReasoningTokens: &positive}

	unsupportedUnit := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.PricingUnit = "PER_TOKEN"
	})
	result := classifyRuntimePricing(200, reportCurrency, unsupportedUnit, usage, runtimeStreamOutcomeCompleted)
	if result.PricingResolutionKind == nil || *result.PricingResolutionKind != runtimePricingResolutionUnsupportedUnit {
		t.Fatalf("expected unsupported_unit, got %+v", result)
	}

	missingComponent := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.ReasoningPrice = ""
	})
	result = classifyRuntimePricing(200, reportCurrency, missingComponent, usage, runtimeStreamOutcomeCompleted)
	if result.PricingResolutionKind == nil || *result.PricingResolutionKind != runtimePricingResolutionMissingComponent {
		t.Fatalf("expected missing_component, got %+v", result)
	}
}

// TestPricingClassifierEpochAttributionAndMigrationBlock pins step 3: a
// legacy_foreign/pre_epoch_pending snapshot (null epoch) is not usable by the
// ready runtime and fails closed to currency_migration_required.
func TestPricingClassifierEpochAttributionAndMigrationBlock(t *testing.T) {
	reportCurrency := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	inputTokens := 10
	outputTokens := 10
	usage := responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens}

	legacyForeign := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.ReportingCurrencyEpoch = nil
		snapshot.PricingCurrencyCode = "EUR"
	})
	result := classifyRuntimePricing(200, reportCurrency, legacyForeign, usage, runtimeStreamOutcomeCompleted)
	if result.PricingStatus != runtimePricingStatusUnpriced || result.UnpricedReason == nil || *result.UnpricedReason != runtimeUnpricedReasonMissingData {
		t.Fatalf("legacy-foreign snapshot must fail closed to MISSING_PRICE_DATA, got %+v", result)
	}
	if result.PricingResolutionKind == nil || *result.PricingResolutionKind != runtimePricingResolutionCurrencyMigrationRequired {
		t.Fatalf("expected currency_migration_required, got %+v", result)
	}

	// Usage reasons are classified AFTER migration blockers (SPEC 3.4): a
	// legacy-foreign snapshot with missing usage still fails closed to
	// currency_migration_required.
	missingUsage := classifyRuntimePricing(200, reportCurrency, legacyForeign, responseUsage{}, runtimeStreamOutcomeCompleted)
	if missingUsage.UnpricedReason == nil || *missingUsage.UnpricedReason != runtimeUnpricedReasonMissingData {
		t.Fatalf("migration blocker must be classified before usage reasons, got %+v", missingUsage)
	}
	if missingUsage.PricingResolutionKind == nil || *missingUsage.PricingResolutionKind != runtimePricingResolutionCurrencyMigrationRequired {
		t.Fatalf("expected currency_migration_required, got %+v", missingUsage)
	}
}

// TestPricingClassifierExactArithmeticOverflow pins the checked-sum overflow
// fail-closed behavior (SPEC 4.5).
func TestPricingClassifierExactArithmeticOverflow(t *testing.T) {
	reportCurrency := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	large := int(1 << 30)
	template := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.InputPrice = strings.Repeat("9", 20)
		snapshot.OutputPrice = strings.Repeat("9", 20)
	})
	result := classifyRuntimePricing(200, reportCurrency, template, responseUsage{
		InputTokens: &large, OutputTokens: &large,
	}, runtimeStreamOutcomeCompleted)
	if result.PricingStatus != runtimePricingStatusUnpriced || result.UnpricedReason == nil || *result.UnpricedReason != runtimeUnpricedReasonMissingData {
		t.Fatalf("expected overflow to fail closed as MISSING_PRICE_DATA, got %+v", result)
	}
	if result.PricingResolutionKind == nil || *result.PricingResolutionKind != runtimePricingResolutionSnapshotIncoherent {
		t.Fatalf("expected snapshot_incoherent resolution for overflow, got %+v", result)
	}
}
