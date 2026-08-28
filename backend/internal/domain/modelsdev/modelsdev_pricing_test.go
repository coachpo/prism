package modelsdev

import "testing"

func pointer(value string) *string { return &value }

func TestBuildPricePlanStandardMapping(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	model, _ := catalog.Find("anthropic", "claude-test")
	plan := BuildPricePlan(Offering{ProviderID: "anthropic", ModelID: "claude-test"}, model, "USD")
	if !plan.Committable() || plan.Kind != "standard" {
		t.Fatalf("plan = %+v", plan)
	}
	card := plan.Cards[RoleStandard]
	if card.InputPrice != "3" || card.OutputPrice != "15" {
		t.Fatalf("base card = %+v", card)
	}
	if card.CachedInputPrice != nil {
		t.Fatal("absent cache_read must map to null")
	}
	if card.CacheCreationPrice == nil || *card.CacheCreationPrice != "3.75" {
		t.Fatalf("cache_write must map to cache_creation_price: %v", card.CacheCreationPrice)
	}
	if card.ReasoningPrice != nil {
		t.Fatal("absent reasoning must map to null")
	}
}

func TestBuildPricePlanOpenAISingleContextTierMapsSizeVerbatim(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	model, _ := catalog.Find("openai", "gpt-long")
	plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "gpt-long"}, model, "USD")
	if !plan.Committable() || plan.Kind != "tiered" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.TierThreshold == nil || *plan.TierThreshold != 272000 {
		t.Fatalf("tier size must land verbatim in the threshold: %v", plan.TierThreshold)
	}
	base, above := plan.Cards[RoleTierBase], plan.Cards[RoleTierAbove]
	if base.InputPrice != "30" || above.InputPrice != "60" || above.OutputPrice != "270" {
		t.Fatalf("cards = %+v / %+v", base, above)
	}
	if base.CachedInputPrice != nil || above.CachedInputPrice != nil {
		t.Fatal("both cards omit cache_read so both must stay null")
	}

	cachedModel, _ := catalog.Find("openai", "gpt-tiered-cache")
	cachedPlan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "gpt-tiered-cache"}, cachedModel, "USD")
	if !cachedPlan.Committable() {
		t.Fatalf("configured specialty shape must stay committable: %+v", cachedPlan.Incompatibilities)
	}
	cachedBase, cachedAbove := cachedPlan.Cards[RoleTierBase], cachedPlan.Cards[RoleTierAbove]
	if cachedBase.CacheCreationPrice == nil || *cachedBase.CacheCreationPrice != "5" {
		t.Fatalf("base cache_write = %v", cachedBase.CacheCreationPrice)
	}
	if cachedAbove.CachedInputPrice == nil || *cachedAbove.CachedInputPrice != "0.8" {
		t.Fatalf("tier cache_read = %v", cachedAbove.CachedInputPrice)
	}
}

func TestBuildPricePlanFailClosedReasons(t *testing.T) {
	catalog := loadFixtureCatalog(t)
	cases := []struct {
		name             string
		provider         string
		modelID          string
		currency         string
		wantIncompatible bool
		wantReason       string
	}{
		{name: "audio cost", provider: "openai", modelID: "gpt-audio", currency: "USD", wantIncompatible: true, wantReason: ReasonAudioCostPresent},
		{name: "non-USD reporting currency", provider: "openai", modelID: "gpt-test", currency: "CNY", wantIncompatible: true, wantReason: ReasonReportingCurrencyNotUSD},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			model, found := catalog.Find(testCase.provider, testCase.modelID)
			if !found {
				t.Fatal("model missing from fixture")
			}
			plan := BuildPricePlan(Offering{ProviderID: testCase.provider, ModelID: testCase.modelID}, model, testCase.currency)
			if plan.Committable() == testCase.wantIncompatible {
				t.Fatalf("committable=%v but wantIncompatible=%v (%+v)", plan.Committable(), testCase.wantIncompatible, plan.Incompatibilities)
			}
			foundReason := false
			for _, item := range plan.Incompatibilities {
				if item.Reason == testCase.wantReason {
					foundReason = true
				}
			}
			if !foundReason {
				t.Fatalf("reason %s missing from %+v", testCase.wantReason, plan.Incompatibilities)
			}
		})
	}
	t.Run("missing cost", func(t *testing.T) {
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "no-cost"}, &Model{ProviderID: "openai", ModelID: "no-cost"}, "USD")
		if plan.Committable() {
			t.Fatal("cost-less model must fail closed")
		}
	})
	t.Run("nil model", func(t *testing.T) {
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "gone"}, nil, "USD")
		if plan.Committable() {
			t.Fatal("nil model must fail closed")
		}
	})
	t.Run("multiple tiers", func(t *testing.T) {
		model := &Model{Cost: &Cost{Base: TierPrices{Input: "1", Output: "2"}, Tiers: []CostTier{
			{Type: "context", Size: 100, Prices: TierPrices{Input: "2", Output: "3"}},
			{Type: "context", Size: 200, Prices: TierPrices{Input: "3", Output: "4"}},
		}}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "multi"}, model, "USD")
		if hasReason(plan, ReasonMultipleTiers) == false {
			t.Fatalf("multiple tiers reason missing: %+v", plan.Incompatibilities)
		}
	})
	t.Run("non-openai context tier", func(t *testing.T) {
		model := &Model{Cost: &Cost{Base: TierPrices{Input: "1", Output: "2"}, Tiers: []CostTier{
			{Type: "context", Size: 272000, Prices: TierPrices{Input: "2", Output: "3"}},
		}}}
		plan := BuildPricePlan(Offering{ProviderID: "anthropic", ModelID: "tiered"}, model, "USD")
		if !hasReason(plan, ReasonTierNotSupported) {
			t.Fatalf("non-openai tier must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("non-context tier type on openai", func(t *testing.T) {
		model := &Model{Cost: &Cost{Base: TierPrices{Input: "1", Output: "2"}, Tiers: []CostTier{
			{Type: "cache", Size: 1000, Prices: TierPrices{Input: "2", Output: "3"}},
		}}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "cachetier"}, model, "USD")
		if !hasReason(plan, ReasonTierNotSupported) {
			t.Fatalf("non-context tier must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("legacy shape alone", func(t *testing.T) {
		model := &Model{Cost: &Cost{
			Base:                     TierPrices{Input: "1", Output: "2"},
			LegacyContextOver200k:    &TierPrices{Input: "2", Output: "3"},
			HasLegacyContextOver200k: true,
		}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "legacy"}, model, "USD")
		if !hasReason(plan, ReasonLegacyTierShape) {
			t.Fatalf("bare legacy tier evidence must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("legacy conflicts explicit tier", func(t *testing.T) {
		model := &Model{Cost: &Cost{
			Base: TierPrices{Input: "1", Output: "2"},
			Tiers: []CostTier{
				{Type: "context", Size: 272000, Prices: TierPrices{Input: "2", Output: "3"}},
			},
			LegacyContextOver200k:    &TierPrices{Input: "9", Output: "9"},
			HasLegacyContextOver200k: true,
		}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "conflict"}, model, "USD")
		if !hasReason(plan, ReasonTierEvidenceConflict) {
			t.Fatalf("conflicting duplicate evidence must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("specialty shape mismatch between base and tier", func(t *testing.T) {
		model := &Model{Cost: &Cost{
			Base: TierPrices{Input: "1", Output: "2", CachedInput: pointer("0.5")},
			Tiers: []CostTier{
				{Type: "context", Size: 272000, Prices: TierPrices{Input: "2", Output: "3"}},
			},
		}}
		plan := BuildPricePlan(Offering{ProviderID: "openai", ModelID: "parity"}, model, "USD")
		if !hasReason(plan, ReasonSpecialtyShapeMismatch) {
			t.Fatalf("specialty mismatch must fail closed: %+v", plan.Incompatibilities)
		}
	})
	t.Run("price outside Prism storage representation", func(t *testing.T) {
		longPrice, err := CanonicalPrice("2.4499999999999995e-3")
		if err != nil || longPrice != "0.0024499999999999995" {
			t.Fatalf("canonical exponent price = %q, %v", longPrice, err)
		}
		model := &Model{Cost: &Cost{Base: TierPrices{
			Input: "0.0245", Output: "0.0978", CachedInput: &longPrice,
		}}}
		plan := BuildPricePlan(Offering{ProviderID: "chutes", ModelID: "long-price"}, model, "USD")
		if !hasReason(plan, ReasonPriceNotRepresentable) {
			t.Fatalf("unrepresentable price must fail closed: %+v", plan.Incompatibilities)
		}
		if len(plan.Incompatibilities) != 1 || plan.Incompatibilities[0].Field != "cost.cache_read" {
			t.Fatalf("unrepresentable price field drifted: %+v", plan.Incompatibilities)
		}
		if plan.Cards[RoleStandard].CachedInputPrice == nil ||
			*plan.Cards[RoleStandard].CachedInputPrice != longPrice {
			t.Fatalf("price-plan preview must preserve source evidence: %+v", plan.Cards)
		}
	})
}

func hasReason(plan PricePlan, reason string) bool {
	for _, item := range plan.Incompatibilities {
		if item.Reason == reason {
			return true
		}
	}
	return false
}
