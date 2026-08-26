package modelexport

import (
	"sort"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

// pricingUnitPerMillion is the only unit the export accepts; client cost
// fields are per-million-token rates.
const pricingUnitPerMillion = "PER_1M"

// PriceCardSnapshot is the normalized view of one current price card. All
// five components use canonical decimal literals so the values can be emitted
// as exact JSON numbers without float round-trips. NULL specialty components
// stay nil: unconfigured is never rewritten to zero.
type PriceCardSnapshot struct {
	InputPrice         string
	OutputPrice        string
	CachedInputPrice   *string
	CacheCreationPrice *string
	ReasoningPrice     *string
}

// complete reports whether every one of the five components is configured.
func (c PriceCardSnapshot) complete() bool {
	return c.InputPrice != "" && c.OutputPrice != "" &&
		c.CachedInputPrice != nil && c.CacheCreationPrice != nil && c.ReasoningPrice != nil
}

// reasoningEqualsOutput reports whether the reasoning component carries the
// output rate. Client files express at most four components (input, output,
// cache read, cache write); reasoning is losslessly implied by output exactly
// when the two rates are equal.
func (c PriceCardSnapshot) reasoningEqualsOutput() bool {
	return c.ReasoningPrice != nil && c.OutputPrice != "" && *c.ReasoningPrice == c.OutputPrice
}

// equal compares two cards numerically by canonical literal. Canonical forms
// are unique per value, so string equality is numeric equality.
func (c PriceCardSnapshot) equal(other PriceCardSnapshot) bool {
	return c.InputPrice == other.InputPrice && c.OutputPrice == other.OutputPrice &&
		stringPtrEqual(c.CachedInputPrice, other.CachedInputPrice) &&
		stringPtrEqual(c.CacheCreationPrice, other.CacheCreationPrice) &&
		stringPtrEqual(c.ReasoningPrice, other.ReasoningPrice)
}

// TargetPriceSnapshot is the normalized current-price shape of one reachable
// Terminal Target.
type TargetPriceSnapshot struct {
	TerminalTargetID int
	// Kind is standard, tiered, or peak_valley (pricingkind).
	Kind pricingkind.Kind
	// Card is the single card for standard kind.
	Card *PriceCardSnapshot
	// BaseCard and AboveCard carry tiered kind; TierThreshold is the strict
	// input_tokens_above boundary of the above card.
	BaseCard      *PriceCardSnapshot
	AboveCard     *PriceCardSnapshot
	TierThreshold *int
	// CurrencyCode and PricingUnit come from the current revision row.
	CurrencyCode string
	PricingUnit  string
}

// sameShape reports whether two reachable targets resolve to one identical
// normalized price shape. Canonical decimal literals are unique per value, so
// string equality is numeric equality.
func sameShape(left, right TargetPriceSnapshot) bool {
	if left.Kind != right.Kind || left.CurrencyCode != right.CurrencyCode || left.PricingUnit != right.PricingUnit {
		return false
	}
	if (left.TierThreshold == nil) != (right.TierThreshold == nil) {
		return false
	}
	if left.TierThreshold != nil && *left.TierThreshold != *right.TierThreshold {
		return false
	}
	switch left.Kind {
	case pricingkind.Standard:
		if left.Card == nil || right.Card == nil {
			return left.Card == right.Card
		}
		return left.Card.equal(*right.Card)
	case pricingkind.Tiered:
		if left.BaseCard == nil || right.BaseCard == nil || left.AboveCard == nil || right.AboveCard == nil {
			return false
		}
		return left.BaseCard.equal(*right.BaseCard) && left.AboveCard.equal(*right.AboveCard)
	default:
		// peak_valley shapes are never exportable; treat identical kinds as
		// one failing shape so warnings stay stable.
		return true
	}
}

func stringPtrEqual(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

// PriceExportDecision explains what the renderer must do with one model's
// prices. The model itself always renders; only the cost group is gated.
type PriceExportDecision struct {
	// Exportable reports whether a cost group may be emitted.
	Exportable bool
	// WarningCodes lists every failed gate as stable codes, sorted.
	WarningCodes []string
}

// DecidePriceExport applies the fail-closed gates of C5 against the snapshot
// of every actually reachable Terminal Target:
//
// USD currency, PER_1M unit, all five components configured everywhere,
// reasoning equal to output everywhere, one identical normalized shape across
// targets, and a target-representable structure. Explicit zeros pass through;
// they are configured values, not absences.
func DecidePriceExport(platform Platform, targets []TargetPriceSnapshot) PriceExportDecision {
	decision := PriceExportDecision{}
	add := func(code string) {
		for _, existing := range decision.WarningCodes {
			if existing == code {
				return
			}
		}
		decision.WarningCodes = append(decision.WarningCodes, code)
	}
	if len(targets) == 0 {
		add(WarningPriceNoTemplate)
		decision.WarningCodes = sortWarningCodes(decision.WarningCodes)
		return decision
	}
	consistent := true
	var reference TargetPriceSnapshot
	for index, target := range targets {
		if index == 0 {
			reference = target
		} else {
			consistent = consistent && sameShape(reference, target)
		}
		if target.Kind == "" {
			add(WarningPriceNoTemplate)
			continue
		}
		switch target.Kind {
		case pricingkind.PeakValley:
			add(WarningPricePeakValleyUnrepresentable)
		case pricingkind.Standard:
			if target.Card == nil {
				add(WarningPriceNoTemplate)
				continue
			}
			if !target.Card.complete() {
				add(WarningPriceIncompleteComponents)
			} else if !target.Card.reasoningEqualsOutput() {
				add(WarningPriceReasoningMismatch)
			}
		case pricingkind.Tiered:
			cards := []*PriceCardSnapshot{target.BaseCard, target.AboveCard}
			for _, card := range cards {
				if card == nil {
					add(WarningPriceNoTemplate)
					continue
				}
				if !card.complete() {
					add(WarningPriceIncompleteComponents)
				} else if !card.reasoningEqualsOutput() {
					add(WarningPriceReasoningMismatch)
				}
			}
			if target.TierThreshold == nil || *target.TierThreshold < 1 {
				add(WarningPriceIncompleteComponents)
			}
		default:
			add(WarningPriceNoTemplate)
		}
		if target.CurrencyCode != "USD" {
			add(WarningPriceCurrencyNotUSD)
		}
		if target.PricingUnit != pricingUnitPerMillion {
			add(WarningPriceUnitNotPerMillion)
		}
	}
	if !consistent {
		add(WarningPriceTargetConflict)
	}
	// Platform representability: Pi can express flat and strict-threshold
	// tiered shapes; OpenCode can additionally express the exact 200,000-token
	// threshold through context_over_200k.
	switch platform {
	case PlatformPi:
		if reference.Kind == pricingkind.Tiered && reference.TierThreshold != nil && *reference.TierThreshold < 1 {
			add(WarningPriceTierUnrepresentable)
		}
	case PlatformOpenCode:
		if reference.Kind == pricingkind.Tiered && (reference.TierThreshold == nil || *reference.TierThreshold != 200000) {
			add(WarningPriceTierUnrepresentable)
		}
	}
	decision.Exportable = len(decision.WarningCodes) == 0
	decision.WarningCodes = sortWarningCodes(decision.WarningCodes)
	return decision
}

// sortWarningCodes deduplicates and sorts warning codes so every response
// carries one stable order.
func sortWarningCodes(codes []string) []string {
	seen := make(map[string]struct{}, len(codes))
	unique := codes[:0:0]
	for _, code := range codes {
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		unique = append(unique, code)
	}
	sort.Strings(unique)
	return unique
}
