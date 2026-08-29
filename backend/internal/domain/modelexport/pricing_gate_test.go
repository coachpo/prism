package modelexport

import (
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

func ptrInt(value int) *int { return &value }

func standardTarget(card PriceCardSnapshot) TargetPriceSnapshot {
	return TargetPriceSnapshot{
		TerminalTargetID: 1,
		Kind:             pricingkind.Standard,
		Card:             &card,
		CurrencyCode:     "USD",
		PricingUnit:      "PER_1M",
	}
}

func completeCard() PriceCardSnapshot {
	return PriceCardSnapshot{
		InputPrice:         "3",
		OutputPrice:        "15",
		CachedInputPrice:   ptrString("0.3"),
		CacheCreationPrice: ptrString("3.75"),
		ReasoningPrice:     ptrString("15"),
	}
}

func TestDecidePriceExportHappyPath(t *testing.T) {
	target := standardTarget(completeCard())
	decision := DecidePriceExport([]TargetPriceSnapshot{target})
	if !decision.Exportable || len(decision.WarningCodes) != 0 {
		t.Fatalf("pi must export a complete flat price: %+v", decision)
	}
}

func TestDecidePriceExportGatesKeepModelAndOmitCost(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TargetPriceSnapshot)
		want   string
	}{
		{name: "missing reasoning price", mutate: func(target *TargetPriceSnapshot) {
			target.Card.ReasoningPrice = nil
		}, want: WarningPriceIncompleteComponents},
		{name: "reasoning differs from output", mutate: func(target *TargetPriceSnapshot) {
			target.Card.ReasoningPrice = ptrString("20")
		}, want: WarningPriceReasoningMismatch},
		{name: "non USD currency", mutate: func(target *TargetPriceSnapshot) {
			target.CurrencyCode = "CNY"
		}, want: WarningPriceCurrencyNotUSD},
		{name: "wrong unit", mutate: func(target *TargetPriceSnapshot) {
			target.PricingUnit = "PER_1K"
		}, want: WarningPriceUnitNotPerMillion},
		{name: "peak valley", mutate: func(target *TargetPriceSnapshot) {
			target.Kind = pricingkind.PeakValley
			target.Card = nil
		}, want: WarningPricePeakValleyUnrepresentable},
	}
	for _, testCase := range cases {
		target := standardTarget(completeCard())
		testCase.mutate(&target)
		decision := DecidePriceExport([]TargetPriceSnapshot{target})
		if decision.Exportable {
			t.Fatalf("%s must keep the cost group omitted", testCase.name)
		}
		found := false
		for _, code := range decision.WarningCodes {
			if code == testCase.want {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s warnings = %v, want %s", testCase.name, decision.WarningCodes, testCase.want)
		}
	}
}

func TestDecidePriceExportTierRepresentability(t *testing.T) {
	base := completeCard()
	above := completeCard()
	above.InputPrice = "6"
	tiered := TargetPriceSnapshot{
		TerminalTargetID: 1,
		Kind:             pricingkind.Tiered,
		BaseCard:         &base,
		AboveCard:        &above,
		TierThreshold:    ptrInt(200000),
		CurrencyCode:     "USD",
		PricingUnit:      "PER_1M",
	}
	if decision := DecidePriceExport([]TargetPriceSnapshot{tiered}); !decision.Exportable {
		t.Fatalf("pi can express one strict tier: %+v", decision.WarningCodes)
	}
	*tiered.TierThreshold = 200001
	if decision := DecidePriceExport([]TargetPriceSnapshot{tiered}); !decision.Exportable {
		t.Fatalf("pi can express any positive tier threshold")
	}
	zeroThreshold := tiered
	zeroThreshold.TierThreshold = ptrInt(0)
	if decision := DecidePriceExport([]TargetPriceSnapshot{zeroThreshold}); decision.Exportable {
		t.Fatalf("pi cannot express zero tier threshold")
	}
}

func TestDecidePriceExportConflictingTargetsFailClosed(t *testing.T) {
	first := standardTarget(completeCard())
	second := standardTarget(completeCard())
	second.TerminalTargetID = 2
	second.Card.OutputPrice = "16"
	decision := DecidePriceExport([]TargetPriceSnapshot{first, second})
	if decision.Exportable {
		t.Fatalf("conflicting target prices must fail closed")
	}
	found := false
	for _, code := range decision.WarningCodes {
		if code == WarningPriceTargetConflict {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings %v must include target conflict", decision.WarningCodes)
	}
}

func TestDecidePriceExportKeepsMissingReachableTargets(t *testing.T) {
	configured := standardTarget(completeCard())
	missing := TargetPriceSnapshot{TerminalTargetID: 2}
	decision := DecidePriceExport([]TargetPriceSnapshot{configured, missing})
	if decision.Exportable {
		t.Fatalf("a reachable target without a current template must omit the whole cost group")
	}
	if !containsWarning(decision.WarningCodes, WarningPriceNoTemplate) || !containsWarning(decision.WarningCodes, WarningPriceTargetConflict) {
		t.Fatalf("warnings = %v, want no-template and target-conflict evidence", decision.WarningCodes)
	}
}

func TestDecidePriceExportTreatsExplicitZeroAsConfigured(t *testing.T) {
	zero := "0"
	card := PriceCardSnapshot{
		InputPrice: "0", OutputPrice: "0", CachedInputPrice: &zero,
		CacheCreationPrice: &zero, ReasoningPrice: &zero,
	}
	decision := DecidePriceExport([]TargetPriceSnapshot{standardTarget(card)})
	if !decision.Exportable || len(decision.WarningCodes) != 0 {
		t.Fatalf("explicit zero prices are configured and free: %+v", decision)
	}
}

func containsWarning(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}
