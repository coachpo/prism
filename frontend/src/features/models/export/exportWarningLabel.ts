/** Localized export evidence shared by the result sheet and source tables. */
const WARNING_KEYS: Record<string, string> = {
  price_no_template: "warnNoTemplate",
  price_currency_not_usd: "warnNotUsd",
  price_unit_not_per_1m: "warnNotPerMillion",
  price_incomplete_components: "warnIncomplete",
  pricing_component_missing: "warnIncomplete",
  price_reasoning_mismatch: "warnReasoningMismatch",
  price_target_conflict: "warnTargetConflict",
  price_peak_valley_unrepresentable: "warnPeakValley",
  price_tier_unrepresentable: "warnTierUnrepresentable",
  metadata_incomplete: "warnMetadataIncomplete",
  metadata_invalid: "warnMetadataInvalid",
  pi_source_fields_dropped: "warnPiSourceFieldsDropped",
  unsupported_input_modality: "warnUnsupportedInputModality",
  mixed_base_urls: "warnMixedBaseUrls",
};
export function exportWarningLabel(copy: Record<string, string>, code: string): string {
  return copy[WARNING_KEYS[code]] ?? copy.warnGeneric;
}
