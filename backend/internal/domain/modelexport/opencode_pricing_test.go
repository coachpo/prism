package modelexport

import (
	"slices"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

func TestOpenCodePriceGatesOmitWholeGroup(t *testing.T) {
	tests := []struct {
		name, warning string
		mutate        func(*TargetPriceSnapshot)
	}{
		{"missing template", WarningPriceNoTemplate, func(p *TargetPriceSnapshot) { *p = TargetPriceSnapshot{} }},
		{"missing input", WarningPricingComponentMissing, func(p *TargetPriceSnapshot) { p.Card.InputPrice = "" }},
		{"missing output", WarningPricingComponentMissing, func(p *TargetPriceSnapshot) { p.Card.OutputPrice = "" }},
		{"missing cached input", WarningPricingComponentMissing, func(p *TargetPriceSnapshot) { p.Card.CachedInputPrice = nil }},
		{"missing cache creation", WarningPricingComponentMissing, func(p *TargetPriceSnapshot) { p.Card.CacheCreationPrice = nil }},
		{"missing reasoning", WarningPricingComponentMissing, func(p *TargetPriceSnapshot) { p.Card.ReasoningPrice = nil }},
		{"reasoning differs", WarningPriceReasoningMismatch, func(p *TargetPriceSnapshot) { p.Card.ReasoningPrice = ptrString("16") }},
		{"currency", WarningPriceCurrencyNotUSD, func(p *TargetPriceSnapshot) { p.CurrencyCode = "EUR" }},
		{"unit", WarningPriceUnitNotPerMillion, func(p *TargetPriceSnapshot) { p.PricingUnit = "PER_1K" }},
		{"tier 200k", WarningPriceTierUnrepresentable, func(p *TargetPriceSnapshot) {
			p.Kind = pricingkind.Tiered
			p.BaseCard = p.Card
			p.AboveCard = completeCardPtr()
			p.TierThreshold = ptrInt(200000)
		}},
		{"tier arbitrary", WarningPriceTierUnrepresentable, func(p *TargetPriceSnapshot) {
			p.Kind = pricingkind.Tiered
			p.BaseCard = p.Card
			p.AboveCard = completeCardPtr()
			p.TierThreshold = ptrInt(100)
		}},
		{"peak valley", WarningPricePeakValleyUnrepresentable, func(p *TargetPriceSnapshot) { p.Kind = pricingkind.PeakValley }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := openCodeInput()
			input.Selection = []int{3}
			test.mutate(input.Facts.Models[0].Targets[0].Pricing)
			result, err := RenderOpenCode(input)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.ModelResults) != 1 || result.ModelResults[0].CostExported || strings.Contains(result.Content, `"cost"`) || !slices.Contains(result.Warnings, test.warning) {
				t.Fatalf("cost must be omitted while retaining model and reason: %+v", result)
			}
		})
	}
}

func TestOpenCodePricesRequireAllReachableTargetsToAgree(t *testing.T) {
	input := openCodeInput()
	input.Selection = []int{3}
	first, second := standardTarget(completeCard()), standardTarget(completeCard())
	second.Card.InputPrice = "7"
	input.Facts.Models[0].Targets = []TargetFact{{TerminalTargetID: 1, Pricing: &first}, {TerminalTargetID: 2, Pricing: &second}}
	result, err := RenderOpenCode(input)
	if err != nil || result.ModelResults[0].CostExported || !slices.Contains(result.Warnings, WarningPriceTargetConflict) {
		t.Fatalf("conflicting target price accepted: %v %+v", err, result)
	}
	zero := PriceCardSnapshot{InputPrice: "0", OutputPrice: "0", CachedInputPrice: ptrString("0"), CacheCreationPrice: ptrString("0"), ReasoningPrice: ptrString("0")}
	first.Card, second.Card = &zero, &zero
	result, err = RenderOpenCode(input)
	if err != nil || !result.ModelResults[0].CostExported || !strings.Contains(result.Content, `"cache_write": 0`) {
		t.Fatalf("explicit zero must export: %v %+v", err, result)
	}
}
