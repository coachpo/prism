// Package modelexport is the HTTP-neutral domain for exporting Prism-managed
// model configuration into Pi (models.json) and OpenCode (config JSON) client
// files. It owns the three-layer leaf merge (Prism truth, models.dev
// enrichment, manual enhancement), the fail-closed price export gates, the
// clock-free source digest, and the deterministic per-platform renderers.
//
// Hard contracts:
//
//   - Prism truth (model_id, protocol mapping, base URL, provider slot,
//     credential slots, prices, routing) can never be overridden by the
//     manual layer; locked paths and sensitive recursive keys fail closed.
//   - models.dev enrichment only fills leaves that Prism metadata left
//     absent, never real-time prices, provider bodies/headers, experimental
//     flags, or request shapes.
//   - A cost group is emitted only when the current price of every actually
//     reachable Terminal Target of every accepted operation resolves to one
//     identical normalized shape under USD/PER_1M with all five components
//     configured, reasoning equal to output, and the target format able to
//     express it losslessly. Everything else keeps the model, omits the whole
//     cost group, and records a stable warning code.
//   - Unknown values are never disguised as valid configuration: absent cost,
//     absent metadata, and explicit zeros stay visibly distinct.
package modelexport

import "fmt"

// Platform identifies one supported client export target.
type Platform string

const (
	// PlatformPi renders a Pi models.json document (Pi 0.84.3 schema).
	PlatformPi Platform = "pi"
	// PlatformOpenCode renders an OpenCode config JSON document
	// (OpenCode 1.18.x config schema).
	PlatformOpenCode Platform = "opencode"
	// PiTargetVersion is the exact client schema/version this renderer pins.
	PiTargetVersion = "0.84.3"
	// OpenCodeTargetVersion is the exact client schema/version this renderer pins.
	OpenCodeTargetVersion = "1.18.23"
)

// TargetVersion returns the exact client version pinned by a platform.
func TargetVersion(platform Platform) string {
	switch platform {
	case PlatformPi:
		return PiTargetVersion
	case PlatformOpenCode:
		return OpenCodeTargetVersion
	default:
		return ""
	}
}

// Valid reports whether the platform is one of the supported targets.
func (p Platform) Valid() bool {
	switch p {
	case PlatformPi, PlatformOpenCode:
		return true
	default:
		return false
	}
}

// ParsePlatform normalizes and validates a raw platform path value.
func ParsePlatform(raw string) (Platform, error) {
	switch raw {
	case string(PlatformPi):
		return PlatformPi, nil
	case string(PlatformOpenCode):
		return PlatformOpenCode, nil
	default:
		return "", fmt.Errorf("unsupported export platform %q", raw)
	}
}

// MergeWarningCodes combines, deduplicates, and sorts warning collections for
// stable source and render wire order.
func MergeWarningCodes(groups ...[]string) []string {
	merged := []string{}
	for _, group := range groups {
		merged = append(merged, group...)
	}
	return sortWarningCodes(merged)
}

// Stable warning codes carried on source and render responses. Message text
// lives in the UI dictionary; these codes carry the semantics.
const (
	// WarningPriceNoTemplate: a reachable Terminal Target has no current
	// pricing template revision.
	WarningPriceNoTemplate = "price_no_template"
	// WarningPriceCurrencyNotUSD: the current revision's currency is not USD.
	WarningPriceCurrencyNotUSD = "price_currency_not_usd"
	// WarningPriceUnitNotPerMillion: the current revision's unit is not PER_1M.
	WarningPriceUnitNotPerMillion = "price_unit_not_per_1m"
	// WarningPriceIncompleteComponents: at least one of the five price
	// components is unconfigured (NULL) somewhere in the reachable shape.
	WarningPricingComponentMissing = "pricing_component_missing"
	// WarningPriceIncompleteComponents is kept as a source-compatible alias;
	// the stable wire value is pricing_component_missing.
	WarningPriceIncompleteComponents = WarningPricingComponentMissing
	// WarningPriceReasoningMismatch: reasoning_price is configured but differs
	// from output_price, so the four-component client shape would be lossy.
	WarningPriceReasoningMismatch = "price_reasoning_mismatch"
	// WarningPriceTargetConflict: reachable Terminal Targets disagree on the
	// normalized price shape.
	WarningPriceTargetConflict = "price_target_conflict"
	// WarningPricePeakValleyUnrepresentable: peak/valley time-based pricing
	// cannot be expressed by any client file.
	WarningPricePeakValleyUnrepresentable = "price_peak_valley_unrepresentable"
	// WarningPriceTierUnrepresentable: tiered pricing cannot be expressed by
	// this platform's file format losslessly.
	WarningPriceTierUnrepresentable = "price_tier_unrepresentable"
	// WarningEnrichmentUnavailable: models.dev data was unavailable or the
	// offering vanished; only stored Prism metadata backs this model.
	WarningEnrichmentUnavailable = "enrichment_unavailable"
	// WarningMetadataIncomplete reports that at least one metadata leaf the
	// target client can represent remains unknown. It never supplies a default.
	WarningMetadataIncomplete = "metadata_incomplete"
	// WarningVariantsUnrepresentable: reasoning options exist but this
	// platform's variant payload cannot be derived safely.
	WarningVariantsUnrepresentable = "variants_unrepresentable"
	// WarningThinkingMapUnrepresentable: reasoning options exist but cannot be
	// projected onto Pi's thinkingLevelMap without guessing.
	WarningThinkingMapUnrepresentable = "thinking_level_map_unrepresentable"
	// WarningPiCompatMayRequireManualOverride reports a catalog-bound OpenAI
	// compatible offering not owned by the official openai provider. Prism does
	// not guess Pi compat flags from model names.
	WarningPiCompatMayRequireManualOverride = "pi_compat_may_require_manual_override"
	// WarningUnsupportedInputModality reports Pi input modalities outside its
	// text/image schema. Supported values remain exported; nothing is guessed.
	WarningUnsupportedInputModality = "unsupported_input_modality"
	// WarningMixedCredentials: the operator asked to embed keys but the
	// selected models resolve to more than one distinct credential, which the
	// single provider slot cannot express honestly.
	WarningMixedCredentials = "mixed_credentials"
	// WarningMixedBaseURLs: selected models resolve to more than one endpoint
	// base URL, so per-model overrides carry the URLs and no provider-level
	// default exists.
	WarningMixedBaseURLs = "mixed_base_urls"
)
