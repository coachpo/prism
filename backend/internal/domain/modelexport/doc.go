// Package modelexport is the HTTP-neutral domain for exporting Prism-managed
// model configuration into Pi 0.84.3 models.json. It owns the clock-free
// source digest, the full-ID + Pi-API candidate matching contract, the
// safe metadata projection (name/reasoning/input/contextWindow/maxTokens/
// thinkingLevelMap/compat), the fail-closed price gates, and the deterministic
// Pi renderer.
//
// Hard contracts:
//
//   - Prism truth (model_id, protocol mapping, base URL, provider slot,
//     credential slots, prices, routing) can never be overridden by the Pi
//     catalog. Locked paths fail closed.
//   - pi.dev entries are matched by complete model_id case-sensitive exact
//     equality plus final Pi API compatibility. No name/slug/contains/fuzzy.
//     Single candidate auto-selects; multiple candidates require explicit
//     operator selection with no auto-merge/first/lex/provider preference.
//   - pi.dev provider/api/baseUrl/cost/headers/samplingParams/fallback/routing
//     never override Prism values; pricing stays fail-closed across all
//     actually reachable Terminal Targets.
//   - A cost group is emitted only when the current price of every actually
//     reachable Terminal Target resolves to one identical normalized shape
//     under USD/PER_1M with all five components and reasoning==output and
//     the Pi tier shape is representable losslessly.
//   - Unknown values are never disguised: absent cost or metadata stays
//     visible warnings, explicit zeros stay "0".
package modelexport

import "github.com/coachpo/prism/backend/internal/domain/pidev"

const PiTargetVersion = pidev.PiTargetVersion

// MergeWarningCodes combines, deduplicates, and sorts warning collections for
// stable source and render wire order.
func MergeWarningCodes(groups ...[]string) []string {
	merged := []string{}
	for _, group := range groups {
		merged = append(merged, group...)
	}
	return sortWarningCodes(merged)
}

// Stable warning codes carried on source and render responses.
const (
	WarningPriceNoTemplate                = "price_no_template"
	WarningPriceCurrencyNotUSD            = "price_currency_not_usd"
	WarningPriceUnitNotPerMillion         = "price_unit_not_per_1m"
	WarningPricingComponentMissing        = "pricing_component_missing"
	WarningPriceIncompleteComponents      = WarningPricingComponentMissing
	WarningPriceReasoningMismatch         = "price_reasoning_mismatch"
	WarningPriceTargetConflict            = "price_target_conflict"
	WarningPricePeakValleyUnrepresentable = "price_peak_valley_unrepresentable"
	WarningPriceTierUnrepresentable       = "price_tier_unrepresentable"
	WarningMetadataIncomplete             = "metadata_incomplete"
	WarningPiSourceFieldsDropped          = "pi_source_fields_dropped"
	WarningUnsupportedInputModality       = "unsupported_input_modality"
	WarningMixedBaseURLs                  = "mixed_base_urls"
)
