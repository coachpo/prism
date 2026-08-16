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

var _ = strings.TrimSpace
