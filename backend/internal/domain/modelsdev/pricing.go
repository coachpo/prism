package modelsdev

import (
	"fmt"
	"sort"
	"strings"
)

// Stable fail-closed incompatibility reasons. The management API and UI key
// off these codes; message text never carries the semantics.
const (
	ReasonReportingCurrencyNotUSD = "reporting_currency_not_usd"
	ReasonCostMissing             = "cost_missing"
	ReasonPriceNotRepresentable   = "price_not_representable"
	ReasonAudioCostPresent        = "audio_cost_present"
	ReasonMultipleTiers           = "multiple_tiers"
	ReasonTierNotSupported        = "tier_not_supported"
	ReasonLegacyTierShape         = "legacy_tier_shape"
	ReasonTierEvidenceConflict    = "tier_evidence_conflict"
	ReasonSpecialtyShapeMismatch  = "specialty_shape_mismatch"
)

const maxPrismPriceLength = 20

// Pricing card roles mirrored from the pricingkind domain so this package
// stays dependency-free.
const (
	RoleStandard  = "standard"
	RoleTierBase  = "tier_base"
	RoleTierAbove = "tier_above"
)

// PriceCard is one five-component price card in canonical decimal form.
// Missing specialty components stay nil; explicit catalog zeros are "0".
type PriceCard struct {
	InputPrice         string
	OutputPrice        string
	CachedInputPrice   *string
	CacheCreationPrice *string
	ReasoningPrice     *string
}

// Incompatibility explains why a catalog offering cannot be mapped into a
// Prism pricing template. Nothing is written when any incompatibility exists.
type Incompatibility struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

func (item Incompatibility) String() string {
	return fmt.Sprintf("%s: %s", item.Field, item.Reason)
}

// PricePlan is the fail-closed mapping result for one offering.
type PricePlan struct {
	// Kind is "standard" or "tiered"; peak_valley is never produced here.
	Kind string
	// Cards holds role-keyed cards for the planned kind.
	Cards map[string]PriceCard
	// TierThreshold is tier.size verbatim for openai single context tiers
	// and nil otherwise; it lands in input_tokens_above unchanged.
	TierThreshold *int64
	// Incompatibilities is non-empty exactly when the plan must not commit.
	Incompatibilities []Incompatibility
}

// Committable reports whether the plan can be written at all.
func (plan PricePlan) Committable() bool {
	return len(plan.Incompatibilities) == 0
}

// BuildPricePlan maps one catalog offering into a pricing template shape.
//
// Fail-closed contract: non-USD reporting currency, missing cost rows, prices
// outside Prism's storage representation, audio cost components, multiple
// tiers, tiers that are not an OpenAI single context tier with a whole-number
// size, legacy context_over_200k without explicit tiers, conflicting duplicate
// tier evidence, and base/tier specialty shape mismatches all produce a stable
// reason and zero writes.
func BuildPricePlan(offering Offering, model *Model, reportingCurrencyCode string) PricePlan {
	plan := PricePlan{Kind: "standard", Cards: map[string]PriceCard{}}
	addIncompatibility := func(field, reason string) {
		plan.Incompatibilities = append(plan.Incompatibilities, Incompatibility{Field: field, Reason: reason})
	}
	if strings.TrimSpace(strings.ToUpper(reportingCurrencyCode)) != "USD" {
		addIncompatibility("pricing_currency_code", ReasonReportingCurrencyNotUSD)
	}
	if model == nil || model.Cost == nil {
		addIncompatibility("cost", ReasonCostMissing)
		return plan
	}
	cost := model.Cost
	if cost.Base.Input == "" || cost.Base.Output == "" {
		addIncompatibility("cost", ReasonCostMissing)
		return plan
	}
	if cost.Base.AudioEvidence {
		addIncompatibility("cost.input_audio", ReasonAudioCostPresent)
	}
	baseCard := cardFromPrices(cost.Base)
	addPriceRepresentationIncompatibilities("cost", baseCard, addIncompatibility)

	switch tierCount := len(cost.Tiers); {
	case tierCount > 1:
		addIncompatibility("cost.tiers", ReasonMultipleTiers)
		plan.Cards[RoleStandard] = baseCard
		return plan
	case tierCount == 1:
		tier := cost.Tiers[0]
		if offering.ProviderID != "openai" || tier.Type != "context" || tier.Size < 1 {
			addIncompatibility("cost.tiers", ReasonTierNotSupported)
			plan.Cards[RoleStandard] = baseCard
			return plan
		}
		if tier.Prices.AudioEvidence {
			addIncompatibility("cost.tiers.audio", ReasonAudioCostPresent)
		}
		if tier.Prices.Input == "" || tier.Prices.Output == "" {
			addIncompatibility("cost.tiers", ReasonTierNotSupported)
			plan.Cards[RoleStandard] = baseCard
			return plan
		}
		if cost.HasLegacyContextOver200k && !legacyMatchesTier(cost.LegacyContextOver200k, tier.Prices) {
			addIncompatibility("cost.context_over_200k", ReasonTierEvidenceConflict)
			plan.Cards[RoleStandard] = baseCard
			return plan
		}
		tierCard := cardFromPrices(tier.Prices)
		addPriceRepresentationIncompatibilities("cost.tiers[0]", tierCard, addIncompatibility)
		if err := specialtyParity(baseCard, tierCard); err != nil {
			addIncompatibility("cost.tiers", ReasonSpecialtyShapeMismatch)
			plan.Cards[RoleStandard] = baseCard
			return plan
		}
		threshold := tier.Size
		plan.Kind = "tiered"
		plan.TierThreshold = &threshold
		plan.Cards[RoleTierBase] = baseCard
		plan.Cards[RoleTierAbove] = tierCard
	default:
		if cost.HasLegacyContextOver200k {
			// A bare legacy long-context row has no explicit strict-> size
			// evidence, so it can never prove the input_tokens_above mapping.
			addIncompatibility("cost.context_over_200k", ReasonLegacyTierShape)
		}
		plan.Cards[RoleStandard] = baseCard
	}
	sortIncompatibilities(plan.Incompatibilities)
	return plan
}

func addPriceRepresentationIncompatibilities(prefix string, card PriceCard, add func(string, string)) {
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "input", value: &card.InputPrice},
		{name: "output", value: &card.OutputPrice},
		{name: "cache_read", value: card.CachedInputPrice},
		{name: "cache_write", value: card.CacheCreationPrice},
		{name: "reasoning", value: card.ReasoningPrice},
	} {
		if field.value != nil && len(*field.value) > maxPrismPriceLength {
			add(prefix+"."+field.name, ReasonPriceNotRepresentable)
		}
	}
}

func cardFromPrices(prices TierPrices) PriceCard {
	card := PriceCard{InputPrice: prices.Input, OutputPrice: prices.Output}
	card.CachedInputPrice = cloneStringPointer(prices.CachedInput)
	card.CacheCreationPrice = cloneStringPointer(prices.CacheCreation)
	card.ReasoningPrice = cloneStringPointer(prices.Reasoning)
	return card
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// legacyMatchesTier accepts the duplicate context_over_200k evidence only
// when its input/output prices repeat the explicit tier verbatim.
func legacyMatchesTier(legacy *TierPrices, tier TierPrices) bool {
	if legacy == nil {
		return false
	}
	return legacy.Input == tier.Input && legacy.Output == tier.Output &&
		stringPointerEqual(legacy.CachedInput, tier.CachedInput) &&
		stringPointerEqual(legacy.CacheCreation, tier.CacheCreation) &&
		stringPointerEqual(legacy.Reasoning, tier.Reasoning)
}

func stringPointerEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// specialtyParity enforces the revision-level constraint that every card of
// one template shares the same configured/NULL specialty shape.
func specialtyParity(base, tier PriceCard) error {
	for _, pair := range [][2]*string{
		{base.CachedInputPrice, tier.CachedInputPrice},
		{base.CacheCreationPrice, tier.CacheCreationPrice},
		{base.ReasoningPrice, tier.ReasoningPrice},
	} {
		if (pair[0] == nil) != (pair[1] == nil) {
			return fmt.Errorf("specialty shape mismatch between base and tier cards")
		}
	}
	return nil
}

var incompatibilityOrder = map[string]int{
	ReasonReportingCurrencyNotUSD: 0,
	ReasonCostMissing:             1,
	ReasonPriceNotRepresentable:   2,
	ReasonAudioCostPresent:        3,
	ReasonMultipleTiers:           4,
	ReasonLegacyTierShape:         5,
	ReasonTierEvidenceConflict:    6,
	ReasonTierNotSupported:        7,
	ReasonSpecialtyShapeMismatch:  8,
}

func sortIncompatibilities(items []Incompatibility) {
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if leftRank, rightRank := incompatibilityOrder[left.Reason], incompatibilityOrder[right.Reason]; leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		return left.Reason < right.Reason
	})
}
