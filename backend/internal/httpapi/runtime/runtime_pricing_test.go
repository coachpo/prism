package runtime

import (
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"reflect"
	"testing"
	"time"
)

func TestBuildRuntimePricingResultUsesStreamUsageUnavailableOnlyForInterruptedStreams(t *testing.T) {
	pricingTemplateSnapshot := runtimePricingTemplateForTest(nil)
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	inputTokens := 7
	outputTokens := 13

	priced := buildRuntimePricingResultForTest(reportCurrencySnapshot, pricingTemplateSnapshot, nil, responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens}, runtimeStreamOutcomeCompleted)
	if !priced.Billable || !priced.Priced || priced.UnpricedReason != nil || priced.TotalCostUserCurrencyMicros == nil || *priced.TotalCostUserCurrencyMicros != 79 {
		t.Fatalf("expected completed stream with observed usage to price normally, got %+v", priced)
	}

	tests := []struct {
		name       string
		outcome    string
		wantReason string
	}{
		{name: "completed", outcome: runtimeStreamOutcomeCompleted, wantReason: runtimeUnpricedReasonMissingUsage},
		{name: "not streaming", outcome: runtimeStreamOutcomeNotStreaming, wantReason: runtimeUnpricedReasonMissingUsage},
		{name: "provider incomplete", outcome: runtimeStreamOutcomeProviderIncomplete, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "client disconnected", outcome: runtimeStreamOutcomeClientDisconnected, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "upstream read error", outcome: runtimeStreamOutcomeUpstreamReadError, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "missing terminal", outcome: runtimeStreamOutcomeUpstreamEndedWithoutTerminal, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
		{name: "unknown", outcome: runtimeStreamOutcomeUnknown, wantReason: runtimeUnpricedReasonStreamUsageUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResultForTest(reportCurrencySnapshot, pricingTemplateSnapshot, nil, responseUsage{}, test.outcome)
			if got.UnpricedReason == nil || *got.UnpricedReason != test.wantReason {
				t.Fatalf("expected missing usage with outcome %q to use %q, got %+v", test.outcome, test.wantReason, got)
			}
		})
	}
}

func TestBuildRuntimePricingResultRequiresUsageBeforePriceData(t *testing.T) {
	tests := []struct {
		name                    string
		reportCurrencySnapshot  runtimeReportCurrencySnapshot
		pricingTemplateSnapshot *runtimePricingTemplateSnapshot
		endpointFXSnapshot      *runtimeEndpointFXSnapshot
		streamOutcome           string
		wantReason              string
	}{
		{
			name:                   "pricing disabled beats interrupted missing usage",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1},
			streamOutcome:          runtimeStreamOutcomeUpstreamReadError,
			wantReason:             runtimeUnpricedReasonPricingOff,
		},
		{
			name:                   "interrupted missing usage beats invalid input price",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
				card := snapshot.Cards[pricingkind.RoleStandard]
				card.InputPrice = "not-a-decimal"
				snapshot.Cards[pricingkind.RoleStandard] = card
			}),
			streamOutcome: runtimeStreamOutcomeUpstreamReadError,
			wantReason:    runtimeUnpricedReasonStreamUsageUnavailable,
		},
		{
			name:                   "completed missing usage beats invalid output price",
			reportCurrencySnapshot: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
				card := snapshot.Cards[pricingkind.RoleStandard]
				card.OutputPrice = "not-a-decimal"
				snapshot.Cards[pricingkind.RoleStandard] = card
			}),
			streamOutcome: runtimeStreamOutcomeCompleted,
			wantReason:    runtimeUnpricedReasonMissingUsage,
		},
		{
			name:                    "interrupted missing usage beats missing fx",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(nil),
			streamOutcome:           runtimeStreamOutcomeUpstreamEndedWithoutTerminal,
			wantReason:              runtimeUnpricedReasonStreamUsageUnavailable,
		},
		{
			name:                    "completed missing usage beats invalid fx",
			reportCurrencySnapshot:  runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "EUR", Epoch: 1},
			pricingTemplateSnapshot: runtimePricingTemplateForTest(nil),
			endpointFXSnapshot:      &runtimeEndpointFXSnapshot{FXRate: "not-a-decimal"},
			streamOutcome:           runtimeStreamOutcomeCompleted,
			wantReason:              runtimeUnpricedReasonMissingUsage,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResultForTest(test.reportCurrencySnapshot, test.pricingTemplateSnapshot, test.endpointFXSnapshot, responseUsage{}, test.streamOutcome)
			if got.UnpricedReason == nil || *got.UnpricedReason != test.wantReason {
				t.Fatalf("expected reason %q, got %+v", test.wantReason, got)
			}
		})
	}
}

func TestBuildRuntimePricingResultValidatesPricingOwnerCoherence(t *testing.T) {
	inputTokens, outputTokens := 1, 1
	completeUsage := responseUsage{InputTokens: &inputTokens, OutputTokens: &outputTokens}
	tests := []struct {
		name           string
		report         runtimeReportCurrencySnapshot
		mutate         func(*runtimePricingTemplateSnapshot)
		usage          responseUsage
		wantStatus     string
		wantResolution string
	}{
		{name: "coherent snapshot prices", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, usage: completeUsage, wantStatus: runtimePricingStatusPriced},
		{name: "missing revision beats missing usage", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, mutate: func(snapshot *runtimePricingTemplateSnapshot) { snapshot.RevisionID = 0 }, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionCurrencyMigrationRequired},
		{name: "missing template epoch beats missing usage", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, mutate: func(snapshot *runtimePricingTemplateSnapshot) { snapshot.ReportingCurrencyEpoch = nil }, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionCurrencyMigrationRequired},
		{name: "report epoch mismatch fails closed", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 2}, usage: completeUsage, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionSnapshotIncoherent},
		{name: "currency mismatch fails closed", report: runtimeReportCurrencySnapshot{Code: "EUR", Symbol: "€", Epoch: 1}, usage: completeUsage, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionSnapshotIncoherent},
		{name: "missing report epoch fails closed", report: runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$"}, usage: completeUsage, wantStatus: runtimePricingStatusUnpriced, wantResolution: runtimePricingResolutionSnapshotIncoherent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := buildRuntimePricingResultForTest(test.report, runtimePricingTemplateForTest(test.mutate), nil, test.usage, runtimeStreamOutcomeCompleted)
			if got.PricingStatus != test.wantStatus || got.Priced != (test.wantStatus == runtimePricingStatusPriced) || dereferenceString(got.PricingResolutionKind) != test.wantResolution {
				t.Fatalf("expected status=%q resolution=%q, got %+v", test.wantStatus, test.wantResolution, got)
			}
			if test.report.Epoch > 0 && (got.ReportingCurrencyEpoch == nil || *got.ReportingCurrencyEpoch != test.report.Epoch) {
				t.Fatalf("expected capture-time reporting epoch %d to survive classification, got %+v", test.report.Epoch, got)
			}
			if test.wantResolution != "" && (got.UnpricedReason == nil || *got.UnpricedReason != runtimeUnpricedReasonMissingData) {
				t.Fatalf("expected owner coherence failure to use missing-data reason, got %+v", got)
			}
		})
	}
}

func TestBuildRuntimePricingResult(t *testing.T) {
	pricingTemplateSnapshot := runtimePricingTemplateForTest(nil)
	reportCurrencySnapshot := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	zero := 0
	positiveCacheRead := 4
	positiveCacheCreation := 5
	positiveReasoning := 6
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20

	tests := []struct {
		name     string
		template *runtimePricingTemplateSnapshot
		usage    responseUsage
		want     runtimePricingResult
	}{
		{
			name: "prices base usage when optional counters are omitted",
			usage: responseUsage{
				InputTokens:  &inputTokens,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
			},
			want: basePricedResult(nil),
		},
		{
			name: "keeps missing token usage for missing required base usage",
			usage: responseUsage{
				OutputTokens: &outputTokens,
				TotalTokens:  &outputTokens,
			},
			want: runtimePricingResult{
				Billable:                      true,
				UnpricedReason:                stringPtr(runtimeUnpricedReasonMissingUsage),
				PricingStatus:                 runtimePricingStatusUnpriced,
				PricingEvidenceTrust:          runtimePricingEvidenceTrust,
				PricingTemplateIDUsed:         intPtr(42),
				PricingTemplateRevisionIDUsed: int64Ptr(7),
				ReportingCurrencyEpoch:        intPtr(1),
				PricingTemplateKind:           stringPtr(string(pricingkind.Standard)),
				PricingSelectionState:         stringPtr(pricingkind.SelectionSelected),
				PricingCardRole:               stringPtr(pricingkind.RoleStandard),
			},
		},
		{
			name: "prices optional counters explicitly set to zero",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &zero,
				CacheCreationInputTokens: &zero,
				ReasoningTokens:          &zero,
			},
			want: basePricedResult(nil),
		},
		{
			name: "prices positive optional counters independently",
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &positiveCacheRead,
				CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens:          &positiveReasoning,
			},
			want: basePricedResult(func(want *runtimePricingResult) {
				want.CacheReadInputCostMicros = int64Ptr(4)
				want.CacheCreationInputCostMicros = int64Ptr(10)
				want.ReasoningCostMicros = int64Ptr(18)
				want.TotalCostOriginalMicros = int64Ptr(102)
				want.TotalCostUserCurrencyMicros = int64Ptr(102)
			}),
		},
		{
			name: "prices positive component counters with concrete zero prices as free",
			template: runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
				card := snapshot.Cards[pricingkind.RoleStandard]
				card.CachedInputPrice, card.CacheCreationPrice, card.ReasoningPrice = "0", "0", "0"
				snapshot.Cards[pricingkind.RoleStandard] = card
			}),
			usage: responseUsage{
				InputTokens:              &inputTokens,
				OutputTokens:             &outputTokens,
				TotalTokens:              &totalTokens,
				CacheReadInputTokens:     &positiveCacheRead,
				CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens:          &positiveReasoning,
			},
			want: basePricedResult(func(want *runtimePricingResult) {
				want.PricingSnapshotCacheReadInput = stringPtr("0")
				want.PricingSnapshotCacheCreationInput = stringPtr("0")
				want.PricingSnapshotReasoning = stringPtr("0")
			}),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			template := pricingTemplateSnapshot
			if test.template != nil {
				template = test.template
			}
			got := buildRuntimePricingResultForTest(reportCurrencySnapshot, template, nil, test.usage, runtimeStreamOutcomeCompleted)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected pricing result %+v, got %+v", test.want, got)
			}
		})
	}
}

func TestEnforceRuntimeSpendCoherence(t *testing.T) {
	success := true
	cost := int64(1250)

	got := enforceRuntimeSpendCoherence(success, runtimePricingResult{
		Billable:                    true,
		Priced:                      true,
		TotalCostUserCurrencyMicros: nil,
	})
	if !got.Billable || got.Priced || got.UnpricedReason == nil || *got.UnpricedReason != runtimeUnpricedReasonMissingData {
		t.Fatalf("expected priced result without cost to degrade to missing price data, got %+v", got)
	}

	got = enforceRuntimeSpendCoherence(success, runtimePricingResult{
		Billable:                    true,
		Priced:                      false,
		TotalCostUserCurrencyMicros: &cost,
		CurrencyCodeOriginal:        stringPtr("USD"),
		ReportCurrencyCode:          stringPtr("USD"),
	})
	if !got.Priced || got.UnpricedReason != nil || got.FXRateUsed == nil || *got.FXRateUsed != "1" || got.FXRateSource == nil || *got.FXRateSource != runtimeFXSourceDefaultOneToOne {
		t.Fatalf("expected cost-bearing result to become priced with same-currency FX defaults, got %+v", got)
	}

	reason := "  MISSING_TOKEN_USAGE  "
	got = enforceRuntimeSpendCoherence(success, runtimePricingResult{
		Billable:       true,
		Priced:         true,
		UnpricedReason: &reason,
	})
	if got.Priced || got.UnpricedReason == nil || *got.UnpricedReason != runtimeUnpricedReasonMissingUsage {
		t.Fatalf("expected explicit unpriced reason to win and trim, got %+v", got)
	}
}

func TestBuildRuntimePricingResultRejectsInvalidConcretePriceWhenComponentIsUsed(t *testing.T) {
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20
	reasoningTokens := 3
	pricingTemplateSnapshot := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		card := snapshot.Cards[pricingkind.RoleStandard]
		card.CachedInputPrice = "0"
		card.CacheCreationPrice = "0"
		card.ReasoningPrice = "not-a-decimal"
		snapshot.Cards[pricingkind.RoleStandard] = card
	})

	got := buildRuntimePricingResultForTest(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, pricingTemplateSnapshot, nil, responseUsage{
		InputTokens:     &inputTokens,
		OutputTokens:    &outputTokens,
		TotalTokens:     &totalTokens,
		ReasoningTokens: &reasoningTokens,
	}, runtimeStreamOutcomeCompleted)

	want := runtimePricingResult{
		Billable:                      true,
		UnpricedReason:                stringPtr(runtimeUnpricedReasonMissingData),
		PricingStatus:                 runtimePricingStatusUnpriced,
		PricingResolutionKind:         stringPtr(runtimePricingResolutionMissingComponent),
		MissingPriceComponents:        []string{"reasoning_price"},
		PricingEvidenceTrust:          runtimePricingEvidenceTrust,
		PricingTemplateIDUsed:         intPtr(42),
		PricingTemplateRevisionIDUsed: int64Ptr(7),
		ReportingCurrencyEpoch:        intPtr(1),
		PricingTemplateKind:           stringPtr(string(pricingkind.Standard)),
		PricingSelectionState:         stringPtr(pricingkind.SelectionSelected),
		PricingCardRole:               stringPtr(pricingkind.RoleStandard),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected invalid used concrete component price to degrade pricing: want %+v got %+v", want, got)
	}
}

func runtimePricingTemplateForTest(mutate func(*runtimePricingTemplateSnapshot)) *runtimePricingTemplateSnapshot {
	snapshot := &runtimePricingTemplateSnapshot{
		ID:                     42,
		RevisionID:             7,
		PricingUnit:            runtimePricingUnitPerMillion,
		PricingCurrencyCode:    "USD",
		ReportingCurrencyEpoch: intPtr(1),
		TemplateKind:           string(pricingkind.Standard),
		Cards: map[string]runtimePricingCard{
			pricingkind.RoleStandard: {
				InputPrice: "2", OutputPrice: "5", CachedInputPrice: "1",
				CacheCreationPrice: "2", ReasoningPrice: "3",
			},
		},
		Version: 7,
	}
	if mutate != nil {
		mutate(snapshot)
	}
	return snapshot
}

func buildRuntimePricingResultForTest(report runtimeReportCurrencySnapshot, snapshot *runtimePricingTemplateSnapshot, fx *runtimeEndpointFXSnapshot, usage responseUsage, streamOutcome string) runtimePricingResult {
	return buildRuntimePricingResultForOperationAt(report, snapshot, fx, usage, streamOutcome, "openai.chat_completions", time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))
}

func basePricedResult(mutate func(*runtimePricingResult)) runtimePricingResult {
	result := runtimePricingResult{
		Billable:                          true,
		Priced:                            true,
		PricingStatus:                     runtimePricingStatusPriced,
		PricingEvidenceTrust:              runtimePricingEvidenceTrust,
		PricingTemplateIDUsed:             intPtr(42),
		PricingTemplateRevisionIDUsed:     int64Ptr(7),
		ReportingCurrencyEpoch:            intPtr(1),
		PricingTemplateKind:               stringPtr(string(pricingkind.Standard)),
		PricingSelectionState:             stringPtr(pricingkind.SelectionSelected),
		PricingCardRole:                   stringPtr(pricingkind.RoleStandard),
		InputCostMicros:                   int64Ptr(20),
		OutputCostMicros:                  int64Ptr(50),
		CacheReadInputCostMicros:          int64Ptr(0),
		CacheCreationInputCostMicros:      int64Ptr(0),
		ReasoningCostMicros:               int64Ptr(0),
		TotalCostOriginalMicros:           int64Ptr(70),
		TotalCostUserCurrencyMicros:       int64Ptr(70),
		CurrencyCodeOriginal:              stringPtr("USD"),
		ReportCurrencyCode:                stringPtr("USD"),
		ReportCurrencySymbol:              stringPtr("$"),
		FXRateUsed:                        stringPtr("1"),
		FXRateSource:                      stringPtr(runtimeFXSourceDefaultOneToOne),
		PricingSnapshotUnit:               stringPtr(runtimePricingUnitPerMillion),
		PricingSnapshotInput:              stringPtr("2"),
		PricingSnapshotOutput:             stringPtr("5"),
		PricingSnapshotCacheReadInput:     stringPtr("1"),
		PricingSnapshotCacheCreationInput: stringPtr("2"),
		PricingSnapshotReasoning:          stringPtr("3"),
		PricingConfigVersionUsed:          intPtr(7),
	}
	if mutate != nil {
		mutate(&result)
	}
	return result
}
