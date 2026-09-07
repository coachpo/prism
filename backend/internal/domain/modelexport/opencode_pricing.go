package modelexport

import "github.com/coachpo/prism/backend/internal/domain/pricingkind"

// DecideOpenCodePriceExport adds the pinned client's representability gate to
// Prism's shared price truth. OpenCode 1.18.27 loads four flat rates only.
func DecideOpenCodePriceExport(targets []TargetPriceSnapshot) PriceExportDecision {
	decision := DecidePriceExport(targets)
	for _, target := range targets {
		if target.Kind == pricingkind.Tiered {
			decision.WarningCodes = append(decision.WarningCodes, WarningPriceTierUnrepresentable)
		}
	}
	decision.WarningCodes = sortWarningCodes(decision.WarningCodes)
	decision.Exportable = len(decision.WarningCodes) == 0
	return decision
}

func openCodeCostGroup(card *PriceCardSnapshot) map[string]any {
	return map[string]any{
		"input": decimal(card.InputPrice), "output": decimal(card.OutputPrice),
		"cache_read": decimal(*card.CachedInputPrice), "cache_write": decimal(*card.CacheCreationPrice),
	}
}
