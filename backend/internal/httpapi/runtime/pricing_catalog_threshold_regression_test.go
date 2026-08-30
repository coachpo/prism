package runtime

import (
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

// TestCatalogImportStrictThresholdContractRegression pins the strict
// input-basis > input_tokens_above semantics that models.dev long-context
// imports rely on. The OpenAI official rule and the OpenCode consumption
// implementation both compare strictly; Prism's equals-threshold behavior
// stays authoritative:
//
//	basis == threshold      -> tier_base card
//	basis == threshold + 1  -> tier_above card
//
// The persisted selector column must remain pricing_template_revisions.
// tier_input_tokens_above (wire name input_tokens_above); the import maps the
// catalog tier size verbatim into it without renaming or rescaling.
func TestCatalogImportStrictThresholdContractRegression(t *testing.T) {
	// A threshold shaped like an OpenAI single context tier import
	// (gpt-*-pro style: context tier at 272000 tokens).
	catalogTierSize := int(272000)
	template := tieredRuntimePricingTemplate(t)
	template.TierInputTokensAbove = &catalogTierSize
	report := runtimeReportCurrencySnapshot{Code: "USD", Symbol: "$", Epoch: 1}

	output := 1
	for _, tc := range []struct {
		name      string
		input     int
		wantRole  string
		wantState string
	}{
		{name: "basis equals threshold keeps tier_base", input: 272000, wantRole: pricingkind.RoleTierBase, wantState: pricingkind.SelectionSelected},
		{name: "basis one over threshold uses tier_above", input: 272001, wantRole: pricingkind.RoleTierAbove, wantState: pricingkind.SelectionSelected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage := responseUsage{InputTokens: &tc.input, OutputTokens: &output}
			result := buildRuntimePricingResultForOperationAt(
				report, template, nil, usage, runtimeStreamOutcomeCompleted,
				"openai.chat_completions", time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
			)
			if result.PricingStatus != runtimePricingStatusPriced {
				t.Fatalf("expected priced result, got %+v", result)
			}
			if result.PricingCardRole == nil || *result.PricingCardRole != tc.wantRole {
				t.Fatalf("card role = %v, want %s", result.PricingCardRole, tc.wantRole)
			}
			if result.PricingSelectionState == nil || *result.PricingSelectionState != tc.wantState {
				t.Fatalf("selection state = %v, want %s", result.PricingSelectionState, tc.wantState)
			}
			if result.PricingSelectorThresholdTokens == nil || *result.PricingSelectorThresholdTokens != 272000 {
				t.Fatalf("selector threshold evidence must carry the verbatim catalog size, got %v", result.PricingSelectorThresholdTokens)
			}
			if result.PricingSelectorBasisTokens == nil || *result.PricingSelectorBasisTokens != int64(tc.input) {
				t.Fatalf("selector basis evidence mismatch: %v", result.PricingSelectorBasisTokens)
			}
		})
	}
}
