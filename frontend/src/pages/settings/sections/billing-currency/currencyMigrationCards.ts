import type {
  CurrencyMigrationCard,
  CurrencyMigrationDraftChunkItem,
  PricingMigrationInventoryTemplate,
  PricingTemplateListPageRevision,
} from "@/lib/types";

function cardForRole(
  role: CurrencyMigrationCard["card_role"],
  card: Omit<CurrencyMigrationCard, "card_role"> | undefined,
): CurrencyMigrationCard {
  if (!card) throw new Error(`Pricing template is missing the ${role} card; rebuild the pricing instance before migrating currency.`);
  return { card_role: role, ...card };
}

export function currencyMigrationCardsForRevision(revision: PricingTemplateListPageRevision): CurrencyMigrationCard[] {
  switch (revision.template_kind) {
    case "standard":
      return [cardForRole("standard", revision.card)];
    case "tiered":
      return [cardForRole("tier_base", revision.base_card), cardForRole("tier_above", revision.tier?.card)];
    case "peak_valley":
      return [cardForRole("peak", revision.peak_card), cardForRole("offpeak", revision.offpeak_card)];
  }
}

export function currencyMigrationCardSetHasMissingRequiredPrice(row: CurrencyMigrationDraftChunkItem) {
  return row.cards.some((card) => !card.input_price.trim() || !card.output_price.trim());
}

export function currencyMigrationInventoryRowToDraftItem(item: PricingMigrationInventoryTemplate): CurrencyMigrationDraftChunkItem {
  if (!item.template_kind || item.current_cards.length === 0) throw new Error(`Pricing template ${item.template_id} has no complete current card set; rebuild the pricing instance before migrating currency.`);
  return {
    template_id: item.template_id,
    expected_version: item.base_version,
    expected_updated_at: item.updated_at,
    template_kind: item.template_kind,
    cards: item.current_cards,
  };
}
