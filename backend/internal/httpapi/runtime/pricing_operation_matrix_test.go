package runtime

import (
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

func standardRuntimePricingTemplate(mutate func(*runtimePricingTemplateSnapshot)) *runtimePricingTemplateSnapshot {
	snapshot := &runtimePricingTemplateSnapshot{
		ID:                     42,
		Name:                   "typed standard",
		RevisionID:             7,
		PricingUnit:            runtimePricingUnitPerMillion,
		PricingCurrencyCode:    "USD",
		ReportingCurrencyEpoch: intPtr(1),
		TemplateKind:           string(pricingkind.Standard),
		Cards: map[string]runtimePricingCard{
			pricingkind.RoleStandard: {
				InputPrice: "2", OutputPrice: "5", CachedInputPrice: "1",
				CacheCreationPrice: "3", ReasoningPrice: "4",
			},
		},
		Version: 1,
	}
	if mutate != nil {
		mutate(snapshot)
	}
	return snapshot
}

func tieredRuntimePricingTemplate(t *testing.T) *runtimePricingTemplateSnapshot {
	t.Helper()
	threshold := 5
	snapshot := standardRuntimePricingTemplate(nil)
	snapshot.Name = "typed tiered"
	snapshot.TemplateKind = string(pricingkind.Tiered)
	snapshot.TierInputTokensAbove = &threshold
	snapshot.Cards = map[string]runtimePricingCard{
		pricingkind.RoleTierBase: {
			InputPrice: "2", OutputPrice: "5", CachedInputPrice: "1",
			CacheCreationPrice: "3", ReasoningPrice: "4",
		},
		pricingkind.RoleTierAbove: {
			InputPrice: "8", OutputPrice: "20", CachedInputPrice: "6",
			CacheCreationPrice: "7", ReasoningPrice: "9",
		},
	}
	return snapshot
}

func typedPeakValleySnapshot(t *testing.T, timezone string, windows []terminaltarget.Window) *runtimePricingTemplateSnapshot {
	t.Helper()
	return &runtimePricingTemplateSnapshot{
		ID: 77, Name: "typed peak valley", RevisionID: 88,
		PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD",
		ReportingCurrencyEpoch: intPtr(1), Version: 4,
		TemplateKind: string(pricingkind.PeakValley),
		Cards: map[string]runtimePricingCard{
			pricingkind.RolePeak: {
				InputPrice: "10", OutputPrice: "20", CachedInputPrice: "3",
				CacheCreationPrice: "4", ReasoningPrice: "5",
			},
			pricingkind.RoleOffpeak: {
				InputPrice: "1", OutputPrice: "2", CachedInputPrice: "0",
				CacheCreationPrice: "0", ReasoningPrice: "0",
			},
		},
		PricingSchedule:       terminaltarget.CompilePricingSchedule(timezone, windows),
		PricingScheduleDigest: terminaltarget.PricingWindowsDigest(windows),
	}
}

func TestTypedPricingOperationCatalogUsesOneCardPipeline(t *testing.T) {
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	template := tieredRuntimePricingTemplate(t)
	for _, operation := range RuntimeOperationCatalog() {
		if operation.Name == runtimeOperationOpenAIModels {
			continue
		}
		t.Run(operation.Name, func(t *testing.T) {
			input, output, total := 10, 4, 14
			usage := responseUsage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total}
			if runtimePricingTierOperationIsTokenCount(operation.HookCollectionID) {
				usage.OutputTokens = nil
				usage.TotalTokens = &input
			}
			result := buildRuntimePricingResultForOperationAt(
				report, template, nil, usage, runtimeStreamOutcomeCompleted,
				operation.HookCollectionID, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC),
			)
			if result.PricingStatus != runtimePricingStatusPriced {
				t.Fatalf("expected typed pricing for %s, got %+v", operation.Name, result)
			}
			if runtimePricingTierOperationIsTokenCount(operation.HookCollectionID) {
				if result.PricingSelectionState == nil || *result.PricingSelectionState != pricingkind.SelectionNotApplicable || result.PricingCardRole == nil || *result.PricingCardRole != pricingkind.RoleTierBase {
					t.Fatalf("expected token-count base-card evidence, got %+v", result)
				}
				if result.OutputCostMicros == nil || *result.OutputCostMicros != 0 {
					t.Fatalf("expected input-only token-count cost, got %+v", result)
				}
			} else if result.PricingSelectionState == nil || *result.PricingSelectionState != pricingkind.SelectionSelected || result.PricingCardRole == nil || *result.PricingCardRole != pricingkind.RoleTierAbove {
				t.Fatalf("expected generation operation to select tier_above, got %+v", result)
			}
		})
	}
}

func TestTypedPricingCardNullZeroAndComponentSemantics(t *testing.T) {
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	input, output, cacheRead := 10, 0, 1
	template := standardRuntimePricingTemplate(func(snapshot *runtimePricingTemplateSnapshot) {
		card := snapshot.Cards[pricingkind.RoleStandard]
		card.OutputPrice = "not-a-decimal"
		card.CachedInputPrice = ""
		card.CacheCreationPrice = "0"
		snapshot.Cards[pricingkind.RoleStandard] = card
	})
	result := buildRuntimePricingResultForOperationAt(report, template, nil, responseUsage{
		InputTokens: &input, OutputTokens: &output, CacheReadInputTokens: &cacheRead,
	}, runtimeStreamOutcomeCompleted, "openai.chat_completions", time.Now().UTC())
	if result.PricingStatus != runtimePricingStatusUnpriced || result.PricingResolutionKind == nil || *result.PricingResolutionKind != runtimePricingResolutionMissingComponent {
		t.Fatalf("expected only the observed null specialty component to fail, got %+v", result)
	}
	if len(result.MissingPriceComponents) != 1 || result.MissingPriceComponents[0] != "cached_input_price" {
		t.Fatalf("unexpected missing components: %+v", result.MissingPriceComponents)
	}
}

func TestTypedPricingTierBoundariesUseWholeCard(t *testing.T) {
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	template := tieredRuntimePricingTemplate(t)
	for _, tc := range []struct {
		name     string
		input    int
		cache    int
		wantRole string
		wantRate string
	}{
		{name: "threshold stays base", input: 5, wantRole: pricingkind.RoleTierBase, wantRate: "2"},
		{name: "one over uses above", input: 6, wantRole: pricingkind.RoleTierAbove, wantRate: "8"},
		{name: "cache contributes to basis", input: 5, cache: 1, wantRole: pricingkind.RoleTierAbove, wantRate: "8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := 2
			usage := responseUsage{InputTokens: &tc.input, OutputTokens: &output}
			if tc.cache > 0 {
				usage.CacheReadInputTokens = &tc.cache
			}
			result := buildRuntimePricingResultForOperationAt(report, template, nil, usage, runtimeStreamOutcomeCompleted, "openai.chat_completions", time.Now().UTC())
			if result.PricingStatus != runtimePricingStatusPriced || result.PricingCardRole == nil || *result.PricingCardRole != tc.wantRole || result.PricingSnapshotInput == nil || *result.PricingSnapshotInput != tc.wantRate {
				t.Fatalf("unexpected tier result: %+v", result)
			}
		})
	}
}

func TestTypedPricingMissingRoleAndZeroClockFailClosed(t *testing.T) {
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	input, output := 1, 1
	usage := responseUsage{InputTokens: &input, OutputTokens: &output}
	missingRole := standardRuntimePricingTemplate(func(snapshot *runtimePricingTemplateSnapshot) {
		delete(snapshot.Cards, pricingkind.RoleStandard)
	})
	missing := buildRuntimePricingResultForOperationAt(report, missingRole, nil, usage, runtimeStreamOutcomeCompleted, "openai.chat_completions", time.Now().UTC())
	if missing.PricingStatus != runtimePricingStatusUnpriced || missing.PricingResolutionKind == nil || *missing.PricingResolutionKind != runtimePricingResolutionSnapshotIncoherent || missing.PricingSelectionState == nil || *missing.PricingSelectionState != pricingkind.SelectionUnresolved || missing.PricingCardRole != nil {
		t.Fatalf("missing card role must be unresolved snapshot evidence, got %+v", missing)
	}

	peak := typedPeakValleySnapshot(t, "UTC", []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 600, EndMinute: 720}})
	zeroClock := buildRuntimePricingResultForOperationAt(report, peak, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", time.Time{})
	if zeroClock.PricingStatus != runtimePricingStatusUnpriced || zeroClock.PricingResolutionKind == nil || *zeroClock.PricingResolutionKind != runtimePricingResolutionScheduleUnresolved || zeroClock.PricingCardRole != nil {
		t.Fatalf("zero planning clock must fail closed, got %+v", zeroClock)
	}
}

func TestTypedPeakValleySelectionBoundariesAndDST(t *testing.T) {
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	input, output := 10, 5
	usage := responseUsage{InputTokens: &input, OutputTokens: &output}
	monday := []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 600, EndMinute: 720}}
	snapshot := typedPeakValleySnapshot(t, "UTC", monday)
	for _, tc := range []struct {
		name string
		at   time.Time
		role string
	}{
		{name: "inclusive start", at: time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC), role: pricingkind.RolePeak},
		{name: "exclusive end", at: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), role: pricingkind.RoleOffpeak},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := buildRuntimePricingResultForOperationAt(report, snapshot, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", tc.at)
			if result.PricingStatus != runtimePricingStatusPriced || result.PricingCardRole == nil || *result.PricingCardRole != tc.role {
				t.Fatalf("unexpected peak/valley result: %+v", result)
			}
		})
	}

	crossMidnight := typedPeakValleySnapshot(t, "UTC", []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 1380, EndMinute: 1500}})
	carried := buildRuntimePricingResultForOperationAt(report, crossMidnight, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", time.Date(2026, 8, 11, 0, 30, 0, 0, time.UTC))
	if carried.PricingCardRole == nil || *carried.PricingCardRole != pricingkind.RolePeak {
		t.Fatalf("expected cross-midnight peak card, got %+v", carried)
	}

	dst := typedPeakValleySnapshot(t, "America/New_York", []terminaltarget.Window{{WeekdayMask: 64, StartMinute: 60, EndMinute: 120}})
	fallBack := buildRuntimePricingResultForOperationAt(report, dst, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC))
	if fallBack.PricingCardRole == nil || *fallBack.PricingCardRole != pricingkind.RolePeak || fallBack.PricingScheduleLocalWeekday == nil || *fallBack.PricingScheduleLocalWeekday != 7 || fallBack.PricingScheduleLocalMinute == nil || *fallBack.PricingScheduleLocalMinute != 90 {
		t.Fatalf("expected repeated local 01:30 to remain in-window, got %+v", fallBack)
	}
}

func TestTypedPeakValleyInvalidTimezoneAndDigestAreUnresolved(t *testing.T) {
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	input, output := 1, 1
	usage := responseUsage{InputTokens: &input, OutputTokens: &output}
	windows := []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 60}}
	for _, snapshot := range []*runtimePricingTemplateSnapshot{
		typedPeakValleySnapshot(t, "Not/AZone", windows),
		typedPeakValleySnapshot(t, "UTC", windows),
	} {
		if snapshot.PricingSchedule.Timezone == "UTC" {
			snapshot.PricingScheduleDigest = "wrong"
		}
		result := buildRuntimePricingResultForOperationAt(report, snapshot, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", time.Date(2026, 8, 10, 0, 30, 0, 0, time.UTC))
		if result.PricingStatus != runtimePricingStatusUnpriced || result.PricingResolutionKind == nil || *result.PricingResolutionKind != runtimePricingResolutionScheduleUnresolved || result.PricingCardRole != nil || result.PricingSnapshotInput != nil {
			t.Fatalf("invalid schedule must remain unresolved without fallback card, got %+v", result)
		}
	}
}
