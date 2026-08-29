package models

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
)

// sourceTargetRows converts domain facts into wire rows without secrets.
func sourceTargetRows(targets []modelexport.TargetFact) []exportSourceTargetRow {
	rows := make([]exportSourceTargetRow, 0, len(targets))
	for _, target := range targets {
		wire := exportSourceTargetRow{
			TerminalTargetID:     target.TerminalTargetID,
			Position:             target.Position,
			EndpointID:           target.EndpointID,
			EndpointName:         target.EndpointName,
			OpenAITextCapability: target.OpenAITextCapability,
		}
		if pricing := target.Pricing; pricing != nil {
			card := func(value *modelexport.PriceCardSnapshot) *exportPriceCardWire {
				if value == nil {
					return nil
				}
				return &exportPriceCardWire{
					InputPrice:         value.InputPrice,
					OutputPrice:        value.OutputPrice,
					CachedInputPrice:   value.CachedInputPrice,
					CacheCreationPrice: value.CacheCreationPrice,
					ReasoningPrice:     value.ReasoningPrice,
				}
			}
			wire.Pricing = &exportTargetPricingWire{
				TerminalTargetID: target.TerminalTargetID,
				Kind:             string(pricing.Kind),
				CurrencyCode:     pricing.CurrencyCode,
				PricingUnit:      pricing.PricingUnit,
				TierThreshold:    pricing.TierThreshold,
				Card:             card(pricing.Card),
				BaseCard:         card(pricing.BaseCard),
				AboveCard:        card(pricing.AboveCard),
			}
		}
		rows = append(rows, wire)
	}
	return rows
}

func rawMessageMap(layer modelexport.MetadataLayer) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	for _, leaf := range layer.Leaves() {
		value, _ := layer.Get(leaf)
		values[leaf] = value
	}
	return values
}

func provenanceStrings(provenance map[string]modelexport.MetadataSource) map[string]string {
	out := map[string]string{}
	for leaf, source := range provenance {
		out[leaf] = string(source)
	}
	return out
}
