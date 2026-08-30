// Pure presentation helpers for the models.dev source-linked pricing import.
// Both the /route/pricing catalog import and the model-detail action render a
// preview through these, so the two surfaces cannot drift on how a price, a
// missing component, or a stable incompatibility reason reaches the screen.
//
// Every helper takes the active locale bundle explicitly; nothing here caches
// messages, so a language switch can never render stale copy.

import type { Messages } from "@/i18n/messages";
import type {
  CatalogIncompatibility,
  CatalogPriceCard,
  CatalogPricingPreviewResponse,
} from "@/lib/types";

export type CatalogCopy = Messages["modelCatalog"];

/** Card roles in a stable display order; unknown roles sort last by name. */
const CARD_ROLE_ORDER: string[] = [
  "standard",
  "tier_base",
  "tier_above",
  "peak",
  "offpeak",
];

export function orderedPlanRoles(
  preview: CatalogPricingPreviewResponse,
): string[] {
  return Object.keys(preview.plan.cards).sort((left, right) => {
    const leftRank = CARD_ROLE_ORDER.indexOf(left);
    const rightRank = CARD_ROLE_ORDER.indexOf(right);
    if (leftRank === -1 && rightRank === -1) return left.localeCompare(right);
    if (leftRank === -1) return 1;
    if (rightRank === -1) return -1;
    return leftRank - rightRank;
  });
}

export function catalogCardRoleLabel(copy: CatalogCopy, role: string): string {
  switch (role) {
    case "standard":
      return copy.pricingPlanKindStandard;
    case "tier_base":
      return copy.pricingRoleTierBase;
    case "tier_above":
      return copy.pricingRoleTierAbove;
    case "peak":
      return copy.pricingRolePeak;
    case "offpeak":
      return copy.pricingRoleOffpeak;
    default:
      return role;
  }
}

export type PriceComponentKey = keyof CatalogPriceCard;

/**
 * The five price components in a fixed order. `optional` marks the three
 * specialty components, where null means "not configured" rather than zero.
 */
export const PRICE_COMPONENTS: Array<{
  key: PriceComponentKey;
  label: (copy: CatalogCopy) => string;
  optional: boolean;
}> = [
  {
    key: "input_price",
    label: (copy) => copy.pricingColumnInput,
    optional: false,
  },
  {
    key: "output_price",
    label: (copy) => copy.pricingColumnOutput,
    optional: false,
  },
  {
    key: "cached_input_price",
    label: (copy) => copy.pricingColumnCacheRead,
    optional: true,
  },
  {
    key: "cache_creation_price",
    label: (copy) => copy.pricingColumnCacheWrite,
    optional: true,
  },
  {
    key: "reasoning_price",
    label: (copy) => copy.pricingColumnReasoning,
    optional: true,
  },
];

/**
 * A configured price always renders verbatim, including the literal "0", so a
 * free component stays visibly zero. An absent component returns null so the
 * caller can render the shared OperatorMissingValue marker instead of text
 * that could be mistaken for a configured value.
 */
export function renderPriceComponent(
  _copy: CatalogCopy,
  value: string | null | undefined,
): string | null {
  if (value === null || value === undefined || value === "") {
    return null;
  }
  return value;
}

/**
 * Stable fail-closed reasons get operator-facing labels. An unrecognised reason
 * still surfaces, carrying its code, so a newer backend reason is visible rather
 * than silently dropped.
 */
export function catalogIncompatibilityLabel(
  copy: CatalogCopy,
  item: CatalogIncompatibility,
): string {
  switch (item.reason) {
    case "reporting_currency_not_usd":
      return copy.incompatReportingCurrencyNotUsd;
    case "cost_missing":
      return copy.incompatCostMissing;
    case "price_not_representable":
      return copy.incompatPriceNotRepresentable;
    case "audio_cost_present":
      return copy.incompatAudioCostPresent;
    case "multiple_tiers":
      return copy.incompatMultipleTiers;
    case "tier_not_supported":
      return copy.incompatTierNotSupported;
    case "legacy_tier_shape":
      return copy.incompatLegacyTierShape;
    case "tier_evidence_conflict":
      return copy.incompatTierEvidenceConflict;
    case "specialty_shape_mismatch":
      return copy.incompatSpecialtyShapeMismatch;
    default:
      return copy.incompatUnknown(item.reason);
  }
}

export function catalogOfferingCoordinate(
  preview: CatalogPricingPreviewResponse,
): string {
  return `${preview.offering.provider_id}/${preview.offering.catalog_model_id}`;
}

/** The template an import will name, before any linked template exists. */
export function catalogProjectedTemplateName(
  preview: CatalogPricingPreviewResponse,
): string {
  return preview.template?.name ?? catalogOfferingCoordinate(preview);
}

/**
 * The commit gate, as explicit reasons rather than a bare disabled flag. Each
 * entry is something the operator has not done yet, and the dialog lists them
 * so a disabled action always explains itself.
 */
export function catalogCommitBlockers(
  copy: CatalogCopy,
  preview: CatalogPricingPreviewResponse | null,
  options: { confirmDrift: boolean },
): string[] {
  if (!preview) return [copy.pricingBlockedNoPreview];
  const blockers: string[] = [];
  if (!preview.committable) blockers.push(copy.pricingBlockedIncompatible);
  if (!preview.preview_hash) blockers.push(copy.pricingBlockedNoPreview);
  if (preview.drift && !options.confirmDrift)
    blockers.push(copy.pricingBlockedDrift);
  return blockers;
}

/** Whether a preview is safe to send to the commit endpoint at all. */
export function catalogPreviewCommittable(
  copy: CatalogCopy,
  preview: CatalogPricingPreviewResponse | null,
  options: { confirmDrift: boolean },
): boolean {
  return catalogCommitBlockers(copy, preview, options).length === 0;
}

/** A catalog fetch stamp rendered in the operator's locale, or the absent
 *  marker when the backend did not supply one. */
export function formatCatalogFetchedAt(
  copy: CatalogCopy,
  value: string | null | undefined,
  formatDateTime: (raw: string) => string,
): string {
  const trimmed = typeof value === "string" ? value.trim() : "";
  if (!trimmed) return copy.valueAbsent;
  return formatDateTime(trimmed);
}
