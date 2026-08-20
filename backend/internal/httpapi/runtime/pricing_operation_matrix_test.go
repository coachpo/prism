package runtime

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

// TestPricingClassifierProviderOperationMatrix covers the SPEC 12.2 LLM
// upstream operation matrix: OpenAI Chat Completions, OpenAI Responses,
// Anthropic Messages and Gemini generateContent/streamGenerateContent across
// stream and non-stream shapes with 2xx priced, abnormal stream, missing
// usage and the five token components.
func TestPricingClassifierProviderOperationMatrix(t *testing.T) {
	reportCurrency := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	template := runtimePricingTemplateForTest(nil)
	inputTokens := 10
	outputTokens := 10
	totalTokens := 20
	positiveCacheRead := 4
	positiveCacheCreation := 5
	positiveReasoning := 6

	operations := []struct {
		name      string
		method    string
		path      string
		streaming bool
	}{
		{name: "openai chat completions non-stream", method: http.MethodPost, path: "/v1/chat/completions", streaming: false},
		{name: "openai chat completions stream", method: http.MethodPost, path: "/v1/chat/completions", streaming: true},
		{name: "openai responses non-stream", method: http.MethodPost, path: "/v1/responses", streaming: false},
		{name: "openai responses stream", method: http.MethodPost, path: "/v1/responses", streaming: true},
		{name: "anthropic messages non-stream", method: http.MethodPost, path: "/v1/messages", streaming: false},
		{name: "anthropic messages stream", method: http.MethodPost, path: "/v1/messages", streaming: true},
		{name: "gemini generateContent non-stream", method: http.MethodPost, path: "/v1beta/models/gemini-test:generateContent", streaming: false},
		{name: "gemini streamGenerateContent stream", method: http.MethodPost, path: "/v1beta/models/gemini-test:streamGenerateContent", streaming: true},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			mustResolveRuntimeOperation(t, operation.method, operation.path)
			streamOutcome := runtimeStreamOutcomeNotStreaming
			if operation.streaming {
				streamOutcome = runtimeStreamOutcomeCompleted
			}

			// 2xx with full usage prices normally with all five components.
			priced := classifyRuntimePricing(200, reportCurrency, template, responseUsage{
				InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens,
				CacheReadInputTokens: &positiveCacheRead, CacheCreationInputTokens: &positiveCacheCreation,
				ReasoningTokens: &positiveReasoning,
			}, streamOutcome)
			if priced.PricingStatus != runtimePricingStatusPriced {
				t.Fatalf("expected priced outcome, got %+v", priced)
			}
			if priced.InputCostMicros == nil || priced.OutputCostMicros == nil ||
				priced.CacheReadInputCostMicros == nil || priced.CacheCreationInputCostMicros == nil ||
				priced.ReasoningCostMicros == nil || priced.TotalCostUserCurrencyMicros == nil {
				t.Fatalf("expected all five component costs plus total, got %+v", priced)
			}
			if priced.FXRateUsed == nil || *priced.FXRateUsed != "1" || priced.FXRateSource == nil || *priced.FXRateSource != runtimeFXSourceDefaultOneToOne {
				t.Fatalf("expected steady-state single-currency FX snapshot, got %+v", priced)
			}

			// Abnormal stream without usage -> STREAM_USAGE_UNAVAILABLE.
			abnormal := classifyRuntimePricing(200, reportCurrency, template, responseUsage{}, runtimeStreamOutcomeProviderIncomplete)
			if abnormal.PricingStatus != runtimePricingStatusUnpriced || abnormal.UnpricedReason == nil || *abnormal.UnpricedReason != runtimeUnpricedReasonStreamUsageUnavailable {
				t.Fatalf("expected abnormal stream usage-unavailable outcome, got %+v", abnormal)
			}

			// Completed without usage -> MISSING_TOKEN_USAGE.
			missing := classifyRuntimePricing(200, reportCurrency, template, responseUsage{}, runtimeStreamOutcomeCompleted)
			if missing.PricingStatus != runtimePricingStatusUnpriced || missing.UnpricedReason == nil || *missing.UnpricedReason != runtimeUnpricedReasonMissingUsage {
				t.Fatalf("expected missing usage outcome, got %+v", missing)
			}

			// Non-2xx with tokens -> ineligible, never priced or unpriced.
			ineligible := classifyRuntimePricing(503, reportCurrency, template, responseUsage{
				InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens,
			}, streamOutcome)
			if ineligible.PricingStatus != runtimePricingStatusIneligible || ineligible.UnpricedReason != nil {
				t.Fatalf("expected non-2xx to be ineligible with no reason, got %+v", ineligible)
			}
		})
	}
}

// TestPricingClassifierOperationalComponentIgnoring pins the
// operation-required vs observed component rule (SPEC 3.4 step 5): an
// input-only operation is not blocked by an unusable output price when no
// output tokens were observed.
func TestPricingClassifierOperationalComponentIgnoring(t *testing.T) {
	reportCurrency := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	inputTokens := 10
	outputTokens := 0
	template := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.OutputPrice = "not-a-decimal"
	})
	result := classifyRuntimePricing(200, reportCurrency, template, responseUsage{
		InputTokens: &inputTokens, OutputTokens: &outputTokens,
	}, runtimeStreamOutcomeCompleted)
	if result.PricingStatus != runtimePricingStatusPriced {
		t.Fatalf("input-only usage with invalid unused output price must still price, got %+v", result)
	}
	if result.OutputCostMicros == nil || *result.OutputCostMicros != 0 {
		t.Fatalf("expected zero output cost for zero output tokens, got %+v", result)
	}
}

func tieredRuntimePricingTemplate(t *testing.T) *runtimePricingTemplateSnapshot {
	t.Helper()
	threshold := 272000
	return runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.TierInputTokensAbove = &threshold
		snapshot.TierInputPrice = "4"
		snapshot.TierOutputPrice = "18"
		snapshot.TierCachedInputPrice = "2"
		snapshot.TierCacheCreationPrice = "5"
		snapshot.TierReasoningPrice = "20"
	})
}

func TestPricingTierSelectionBoundariesUseWholeCard(t *testing.T) {
	cases := []struct {
		name           string
		input          int
		cacheRead      int
		output         int
		reasoning      int
		wantTier       string
		wantInputPrice string
		wantOutputCost int64
		wantReasonCost int64
	}{
		{name: "threshold stays base", input: 272000, output: 10, reasoning: 2, wantTier: runtimePricingTierBase, wantInputPrice: "2", wantOutputCost: 50, wantReasonCost: 6},
		{name: "one token over switches all components", input: 272001, output: 10, reasoning: 2, wantTier: runtimePricingTierAbove, wantInputPrice: "4", wantOutputCost: 180, wantReasonCost: 40},
		{name: "cache read contributes to basis", input: 272000, cacheRead: 1, output: 10, reasoning: 2, wantTier: runtimePricingTierAbove, wantInputPrice: "4", wantOutputCost: 180, wantReasonCost: 40},
		{name: "large output does not affect tier basis", input: 1, output: 1000000, reasoning: 2, wantTier: runtimePricingTierBase, wantInputPrice: "2", wantOutputCost: 5000000, wantReasonCost: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			template := tieredRuntimePricingTemplate(t)
			usage := responseUsage{InputTokens: &tc.input, OutputTokens: &tc.output, ReasoningTokens: &tc.reasoning}
			if tc.cacheRead != 0 {
				usage.CacheReadInputTokens = &tc.cacheRead
			}
			selection := selectRuntimePricingTier(template, usage, "openai.chat_completions")
			if selection.Kind != tc.wantTier || selection.Snapshot == nil || selection.Snapshot.InputPrice != tc.wantInputPrice {
				t.Fatalf("expected tier=%s input_price=%s, got %+v", tc.wantTier, tc.wantInputPrice, selection)
			}
			result := buildRuntimePricingResultForOperation(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, template, nil, usage, runtimeStreamOutcomeCompleted, "openai.chat_completions")
			if result.PricingStatus != runtimePricingStatusPriced || result.OutputCostMicros == nil || *result.OutputCostMicros != tc.wantOutputCost || result.ReasoningCostMicros == nil || *result.ReasoningCostMicros != tc.wantReasonCost {
				t.Fatalf("expected whole-card priced result, got %+v", result)
			}
		})
	}
}

func TestTypedCountTokenPricingUsesBaseCardWithoutGenerationOutput(t *testing.T) {
	threshold := 1
	snapshot := runtimePricingTemplateForTest(func(snapshot *runtimePricingTemplateSnapshot) {
		snapshot.TemplateKind = string(pricingkind.Tiered)
		snapshot.TierInputTokensAbove = &threshold
		snapshot.Cards = map[string]runtimePricingCard{
			pricingkind.RoleTierBase:  {InputPrice: "2", OutputPrice: "5", CachedInputPrice: "1", CacheCreationPrice: "3", ReasoningPrice: "4"},
			pricingkind.RoleTierAbove: {InputPrice: "8", OutputPrice: "20", CachedInputPrice: "6", CacheCreationPrice: "7", ReasoningPrice: "9"},
		}
	})
	input, total := 10, 10
	usage := responseUsage{InputTokens: &input, TotalTokens: &total}
	for _, operation := range []string{"openai.responses.input_tokens", "anthropic.count_tokens", "gemini.count_tokens"} {
		t.Run(operation, func(t *testing.T) {
			result := buildRuntimePricingResultForOperation(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, snapshot, nil, usage, runtimeStreamOutcomeNotStreaming, operation)
			if result.PricingStatus != runtimePricingStatusPriced || result.Priced != true {
				t.Fatalf("expected token-count operation to be priced from input usage, got %+v", result)
			}
			if result.PricingSelectionState == nil || *result.PricingSelectionState != pricingkind.SelectionNotApplicable || result.PricingCardRole == nil || *result.PricingCardRole != pricingkind.RoleTierBase {
				t.Fatalf("expected not_applicable tier_base evidence, got %+v", result)
			}
			if result.PricingSelectorThresholdTokens != nil || result.PricingSelectorBasisTokens != nil {
				t.Fatalf("count-token selection must not persist generation threshold/basis, got %+v", result)
			}
			if result.OutputCostMicros == nil || *result.OutputCostMicros != 0 || result.PricingSnapshotInput == nil || *result.PricingSnapshotInput != "2" {
				t.Fatalf("expected base card input-only cost and snapshot, got %+v", result)
			}
		})
	}
}

func TestPricingTierStatesAndFailClosedBoundaries(t *testing.T) {
	fullUsage := responseUsage{InputTokens: intPtr(272001), OutputTokens: intPtr(1), ReasoningTokens: intPtr(1)}
	noTier := runtimePricingTemplateForTest(nil)
	selection := selectRuntimePricingTier(noTier, fullUsage, "openai.chat_completions")
	if selection.Kind != runtimePricingTierNotApplicable || selection.Threshold != nil || selection.Basis != nil {
		t.Fatalf("expected no-tier not_applicable without evidence, got %+v", selection)
	}

	missingUsage := selectRuntimePricingTier(tieredRuntimePricingTemplate(t), responseUsage{}, "openai.chat_completions")
	if missingUsage.Kind != runtimePricingTierNotEvaluated || missingUsage.Threshold != nil || missingUsage.Basis != nil {
		t.Fatalf("expected missing usage not_evaluated without evidence, got %+v", missingUsage)
	}
	missingResult := buildRuntimePricingResultForOperation(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, tieredRuntimePricingTemplate(t), nil, responseUsage{InputTokens: intPtr(1)}, runtimeStreamOutcomeCompleted, "openai.chat_completions")
	if missingResult.PricingStatus != runtimePricingStatusUnpriced {
		t.Fatalf("expected 2xx missing usage to remain unpriced, got %+v", missingResult)
	}

	countTokens := selectRuntimePricingTier(tieredRuntimePricingTemplate(t), fullUsage, "anthropic.count_tokens")
	if countTokens.Kind != runtimePricingTierNotApplicable || countTokens.Basis != nil {
		t.Fatalf("expected count_tokens not_applicable even with tier, got %+v", countTokens)
	}

	missingTierPrice := tieredRuntimePricingTemplate(t)
	missingTierPrice.TierReasoningPrice = ""
	missingPrice := buildRuntimePricingResultForOperation(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, missingTierPrice, nil, fullUsage, runtimeStreamOutcomeCompleted, "openai.chat_completions")
	if missingPrice.PricingStatus != runtimePricingStatusUnpriced || missingPrice.PricingResolutionKind == nil || *missingPrice.PricingResolutionKind != runtimePricingResolutionMissingComponent || len(missingPrice.MissingPriceComponents) != 1 || missingPrice.MissingPriceComponents[0] != "reasoning_price" {
		t.Fatalf("expected tier evidence with missing tier price, got %+v", missingPrice)
	}

	maxInt := int(^uint(0) >> 1)
	overflow := responseUsage{InputTokens: &maxInt, CacheReadInputTokens: intPtr(1), OutputTokens: intPtr(1)}
	overflowResult := buildRuntimePricingResultForOperation(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, tieredRuntimePricingTemplate(t), nil, overflow, runtimeStreamOutcomeCompleted, "openai.chat_completions")
	if overflowResult.PricingStatus != runtimePricingStatusUnpriced || overflowResult.PricingResolutionKind == nil || *overflowResult.PricingResolutionKind != runtimePricingResolutionSnapshotIncoherent {
		t.Fatalf("expected tier basis overflow to fail closed as snapshot_incoherent, got %+v", overflowResult)
	}

	non2xx := selectRuntimePricingTier(tieredRuntimePricingTemplate(t), fullUsage, "openai.chat_completions")
	if non2xx.Kind != runtimePricingTierAbove {
		t.Fatalf("selector itself should still classify full usage; status gating belongs to pricing entrypoint, got %+v", non2xx)
	}
	ineligible := classifyRuntimePricingForOperation(503, runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, tieredRuntimePricingTemplate(t), fullUsage, runtimeStreamOutcomeNotStreaming, "openai.chat_completions")
	if ineligible.PricingStatus != runtimePricingStatusIneligible || ineligible.PricingSelectionState == nil || *ineligible.PricingSelectionState != pricingkind.SelectionNotEvaluated {
		t.Fatalf("expected non-2xx typed evidence to remain unevaluated, got %+v", ineligible)
	}
}

func typedPeakValleySnapshot(t *testing.T, timezone string, windows []terminaltarget.Window) *runtimePricingTemplateSnapshot {
	t.Helper()
	return &runtimePricingTemplateSnapshot{
		ID: 77, RevisionID: 88, PricingUnit: runtimePricingUnitPerMillion, PricingCurrencyCode: "USD", ReportingCurrencyEpoch: intPtr(1), Version: 4,
		TemplateKind: string(pricingkind.PeakValley),
		Cards: map[string]runtimePricingCard{
			pricingkind.RolePeak:    {InputPrice: "10", OutputPrice: "20", CachedInputPrice: "3", CacheCreationPrice: "4", ReasoningPrice: "5"},
			pricingkind.RoleOffpeak: {InputPrice: "1", OutputPrice: "2", CachedInputPrice: "0", CacheCreationPrice: "0", ReasoningPrice: "0"},
		},
		PricingScheduleTimezone: stringPtr(timezone), PricingScheduleDigest: runtimePricingWindowsDigest(windows),
		PricingSchedule: compileRuntimePricingSchedule(timezone, windows),
	}
}

func TestTypedPeakValleySelectionUsesFrozenReferenceClock(t *testing.T) {
	mondayWindow := []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 600, EndMinute: 720}}
	snapshot := typedPeakValleySnapshot(t, "UTC", mondayWindow)
	input, output, total := 10, 5, 15
	usage := responseUsage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total}
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}

	peakAt := time.Date(2026, time.August, 10, 10, 0, 0, 0, time.UTC) // Monday, window start.
	peak := buildRuntimePricingResultForOperationAt(report, snapshot, nil, usage, runtimeStreamOutcomeCompleted, "openai.chat_completions", peakAt)
	if peak.PricingStatus != runtimePricingStatusPriced || peak.PricingSelectionState == nil || *peak.PricingSelectionState != pricingkind.SelectionSelected || peak.PricingCardRole == nil || *peak.PricingCardRole != pricingkind.RolePeak {
		t.Fatalf("expected peak card at inclusive start, got %+v", peak)
	}
	if peak.PricingSnapshotInput == nil || *peak.PricingSnapshotInput != "10" || peak.InputCostMicros == nil || *peak.InputCostMicros != 100 {
		t.Fatalf("expected complete peak price card and cost, got %+v", peak)
	}
	if peak.PricingScheduleLocalWeekday == nil || *peak.PricingScheduleLocalWeekday != 1 || peak.PricingScheduleLocalMinute == nil || *peak.PricingScheduleLocalMinute != 600 {
		t.Fatalf("expected ISO local schedule evidence, got %+v", peak)
	}

	offpeakAt := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC) // exclusive end.
	offpeak := buildRuntimePricingResultForOperationAt(report, snapshot, nil, usage, runtimeStreamOutcomeCompleted, "openai.chat_completions", offpeakAt)
	if offpeak.PricingStatus != runtimePricingStatusPriced || offpeak.PricingCardRole == nil || *offpeak.PricingCardRole != pricingkind.RoleOffpeak || offpeak.PricingSnapshotInput == nil || *offpeak.PricingSnapshotInput != "1" {
		t.Fatalf("expected offpeak card at exclusive end, got %+v", offpeak)
	}
	if peak.PricingScheduleDigest == nil || *peak.PricingScheduleDigest != runtimePricingWindowsDigest(mondayWindow) {
		t.Fatalf("expected canonical schedule digest evidence, got %+v", peak)
	}
}

func TestTypedPeakValleySelectionCrossMidnightAndFailClosed(t *testing.T) {
	windows := []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 1380, EndMinute: 1500}}
	snapshot := typedPeakValleySnapshot(t, "UTC", windows)
	input, output, total := 1, 1, 2
	usage := responseUsage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total}
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	crossMidnight := time.Date(2026, time.August, 11, 0, 30, 0, 0, time.UTC) // Tuesday, carried from Monday.
	selected := buildRuntimePricingResultForOperationAt(report, snapshot, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", crossMidnight)
	if selected.PricingStatus != runtimePricingStatusPriced || selected.PricingCardRole == nil || *selected.PricingCardRole != pricingkind.RolePeak {
		t.Fatalf("expected cross-midnight peak selection, got %+v", selected)
	}

	invalid := typedPeakValleySnapshot(t, "Not/AZone", windows)
	failed := buildRuntimePricingResultForOperationAt(report, invalid, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", crossMidnight)
	if failed.PricingStatus != runtimePricingStatusUnpriced || failed.PricingResolutionKind == nil || *failed.PricingResolutionKind != runtimePricingResolutionScheduleUnresolved || failed.PricingCardRole != nil || failed.TotalCostUserCurrencyMicros != nil {
		t.Fatalf("invalid timezone must fail closed without a fallback card, got %+v", failed)
	}

	missing := typedPeakValleySnapshot(t, "UTC", windows)
	missing.PricingScheduleDigest = "wrong-digest"
	failedSnapshot := buildRuntimePricingResultForOperationAt(report, missing, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", crossMidnight)
	if failedSnapshot.PricingStatus != runtimePricingStatusUnpriced || failedSnapshot.PricingResolutionKind == nil || *failedSnapshot.PricingResolutionKind != runtimePricingResolutionScheduleUnresolved || failedSnapshot.PricingSnapshotInput != nil {
		t.Fatalf("snapshot digest mismatch must fail closed before price snapshots, got %+v", failedSnapshot)
	}
}

func TestTypedPeakValleySelectionUsesIANA_DSTWallClock(t *testing.T) {
	windows := []terminaltarget.Window{{WeekdayMask: 64, StartMinute: 60, EndMinute: 120}}
	snapshot := typedPeakValleySnapshot(t, "America/New_York", windows)
	input, output, total := 1, 1, 2
	usage := responseUsage{InputTokens: &input, OutputTokens: &output, TotalTokens: &total}
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}
	// During fall-back, the second 01:30 instant is still Sunday local wall
	// time. The selector must use the authored IANA location, not UTC or the
	// browser display timezone.
	fallBack := time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC)
	result := buildRuntimePricingResultForOperationAt(report, snapshot, nil, usage, runtimeStreamOutcomeCompleted, "openai.responses", fallBack)
	if result.PricingStatus != runtimePricingStatusPriced || result.PricingCardRole == nil || *result.PricingCardRole != pricingkind.RolePeak || result.PricingScheduleLocalWeekday == nil || *result.PricingScheduleLocalWeekday != 7 || result.PricingScheduleLocalMinute == nil || *result.PricingScheduleLocalMinute != 90 {
		t.Fatalf("expected DST fall-back local Sunday 01:30 peak evidence, got %+v", result)
	}
}

func TestTypedPeakValleySelectionIsRecordedBeforeMissingUsage(t *testing.T) {
	windows := []terminaltarget.Window{{WeekdayMask: 2, StartMinute: 0, EndMinute: 1440}}
	snapshot := typedPeakValleySnapshot(t, "UTC", windows)
	reference := time.Date(2026, time.August, 11, 3, 0, 0, 0, time.UTC) // Tuesday.
	result := buildRuntimePricingResultForOperationAt(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, snapshot, nil, responseUsage{}, runtimeStreamOutcomeCompleted, "anthropic.messages", reference)
	if result.PricingStatus != runtimePricingStatusUnpriced || result.UnpricedReason == nil || *result.UnpricedReason != runtimeUnpricedReasonMissingUsage || result.PricingSelectionState == nil || *result.PricingSelectionState != pricingkind.SelectionSelected || result.PricingCardRole == nil || *result.PricingCardRole != pricingkind.RolePeak {
		t.Fatalf("peak selection must remain auditable even when usage is missing, got %+v", result)
	}
	if result.PricingSnapshotInput != nil || result.TotalCostUserCurrencyMicros != nil {
		t.Fatalf("missing usage must not copy or calculate a card snapshot, got %+v", result)
	}
}

var _ = strings.TrimSpace
