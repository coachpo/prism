package runtime

import (
	"net/http"
	"strings"
	"testing"
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
		{name: "one token over switches all components", input: 272001, output: 10, reasoning: 2, wantTier: runtimePricingTierApplied, wantInputPrice: "4", wantOutputCost: 180, wantReasonCost: 40},
		{name: "cache read contributes to basis", input: 272000, cacheRead: 1, output: 10, reasoning: 2, wantTier: runtimePricingTierApplied, wantInputPrice: "4", wantOutputCost: 180, wantReasonCost: 40},
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
			if result.PricingTierApplied == nil || *result.PricingTierApplied != tc.wantTier || result.PricingTierThresholdTokens == nil || *result.PricingTierThresholdTokens != 272000 || result.PricingTierBasisTokens == nil {
				t.Fatalf("expected persisted tier evidence, got %+v", result)
			}
			wantBasis := int64(tc.input + tc.cacheRead)
			if *result.PricingTierBasisTokens != wantBasis {
				t.Fatalf("expected basis %d, got %d", wantBasis, *result.PricingTierBasisTokens)
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
	if missingResult.PricingStatus != runtimePricingStatusUnpriced || missingResult.PricingTierApplied != nil {
		t.Fatalf("expected 2xx missing usage to keep persisted tier columns NULL, got %+v", missingResult)
	}

	countTokens := selectRuntimePricingTier(tieredRuntimePricingTemplate(t), fullUsage, "anthropic.count_tokens")
	if countTokens.Kind != runtimePricingTierNotApplicable || countTokens.Basis != nil {
		t.Fatalf("expected count_tokens not_applicable even with tier, got %+v", countTokens)
	}

	missingTierPrice := tieredRuntimePricingTemplate(t)
	missingTierPrice.TierReasoningPrice = ""
	missingPrice := buildRuntimePricingResultForOperation(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, missingTierPrice, nil, fullUsage, runtimeStreamOutcomeCompleted, "openai.chat_completions")
	if missingPrice.PricingStatus != runtimePricingStatusUnpriced || missingPrice.PricingResolutionKind == nil || *missingPrice.PricingResolutionKind != runtimePricingResolutionMissingComponent || missingPrice.PricingTierApplied == nil || *missingPrice.PricingTierApplied != runtimePricingTierApplied || len(missingPrice.MissingPriceComponents) != 1 || missingPrice.MissingPriceComponents[0] != "reasoning_price" {
		t.Fatalf("expected tier evidence with missing tier price, got %+v", missingPrice)
	}

	maxInt := int(^uint(0) >> 1)
	overflow := responseUsage{InputTokens: &maxInt, CacheReadInputTokens: intPtr(1), OutputTokens: intPtr(1)}
	overflowResult := buildRuntimePricingResultForOperation(runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, tieredRuntimePricingTemplate(t), nil, overflow, runtimeStreamOutcomeCompleted, "openai.chat_completions")
	if overflowResult.PricingStatus != runtimePricingStatusUnpriced || overflowResult.PricingResolutionKind == nil || *overflowResult.PricingResolutionKind != runtimePricingResolutionSnapshotIncoherent {
		t.Fatalf("expected tier basis overflow to fail closed as snapshot_incoherent, got %+v", overflowResult)
	}

	non2xx := selectRuntimePricingTier(tieredRuntimePricingTemplate(t), fullUsage, "openai.chat_completions")
	if non2xx.Kind != runtimePricingTierApplied {
		t.Fatalf("selector itself should still classify full usage; status gating belongs to pricing entrypoint, got %+v", non2xx)
	}
	ineligible := classifyRuntimePricingForOperation(503, runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}, tieredRuntimePricingTemplate(t), fullUsage, runtimeStreamOutcomeNotStreaming, "openai.chat_completions")
	if ineligible.PricingStatus != runtimePricingStatusIneligible || ineligible.PricingTierApplied == nil || *ineligible.PricingTierApplied != runtimePricingTierNotEvaluated || ineligible.PricingTierThresholdTokens != nil || ineligible.PricingTierBasisTokens != nil {
		t.Fatalf("expected non-2xx tier evidence to remain unevaluated, got %+v", ineligible)
	}
}

var _ = strings.TrimSpace
