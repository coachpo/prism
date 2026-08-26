import { api } from "@/lib/api";
import type {
  CostingSettingsUpdate,
  CurrencyMigrationDraftChunkItem,
  CurrencyMigrationDraftHeader,
  CurrencyMigrationPreview,
} from "@/lib/types";
import {
  currencyMigrationCardsForRevision,
  currencyMigrationInventoryRowToDraftItem,
} from "../sections/billing-currency/currencyMigrationCards";

export type PreparedMigration = {
  rows: CurrencyMigrationDraftChunkItem[];
  names: Record<number, string>;
  inventoryId: string | null;
  inventoryHash: string | null;
  inventoryGeneration: number | null;
  operationKind: "currency_cutover" | "repair_same_currency";
};

async function loadAllPricingTemplatePages(): Promise<PreparedMigration["rows"]> {
  const rows: CurrencyMigrationDraftChunkItem[] = [];
  let cursor: string | undefined;
  const seen = new Set<string>();
  while (true) {
    const page = await api.pricingTemplates.listPage({ limit: 100, cursor });
    for (const item of page.items) {
      rows.push({
        template_id: Number(item.id),
        expected_version: item.current_revision.version,
        expected_updated_at: item.updated_at,
        template_kind: item.current_revision.template_kind,
        cards: currencyMigrationCardsForRevision(item.current_revision),
      });
    }
    if (!page.next_cursor) break;
    if (seen.has(page.next_cursor))
      throw new Error(
        "Pricing template page cursor repeated; reload Settings before retrying.",
      );
    seen.add(page.next_cursor);
    cursor = page.next_cursor;
  }
  return rows;
}

async function loadAllInventoryTemplatePages(inventoryId: string): Promise<{
  rows: CurrencyMigrationDraftChunkItem[];
  names: Record<number, string>;
}> {
  const rows: CurrencyMigrationDraftChunkItem[] = [];
  const names: Record<number, string> = {};
  let cursor: string | undefined;
  const seen = new Set<string>();
  while (true) {
    const page = await api.settings.costing.currencyMigrationInventoryTemplates(
      inventoryId,
      { limit: 100, cursor },
    );
    for (const item of page.items) {
      rows.push(currencyMigrationInventoryRowToDraftItem(item));
      names[item.template_id] = item.name;
    }
    if (!page.next_cursor) {
      break;
    }
    if (seen.has(page.next_cursor)) {
      throw new Error(
        "Pricing migration inventory cursor repeated; reload Settings before retrying.",
      );
    }
    seen.add(page.next_cursor);
    cursor = page.next_cursor;
  }
  return { rows, names };
}

export async function prepareCurrencyMigration(
  currentCosting: CostingSettingsUpdate,
  targetCurrencyCode: string,
): Promise<PreparedMigration> {
  const inventory = currentCosting.pricing_migration_inventory;
  const currentCode = currentCosting.report_currency_code.trim().toUpperCase();
  const hasEpoch = Boolean(
    String(currentCosting.reporting_currency_epoch ?? "").trim(),
  );
  const useInventory = Boolean(
    inventory &&
      (!hasEpoch ||
        (inventory.template_issue_count > 0 &&
          targetCurrencyCode.trim().toUpperCase() === currentCode)),
  );
  let rows: CurrencyMigrationDraftChunkItem[];
  let names: Record<number, string> = {};
  let inventoryId: string | null = null;
  let inventoryHash: string | null = null;
  let inventoryGeneration: number | null = null;
  if (useInventory && inventory) {
    const loaded = await loadAllInventoryTemplatePages(inventory.inventory_id);
    rows = loaded.rows;
    names = loaded.names;
    inventoryId = inventory.inventory_id;
    inventoryHash = inventory.inventory_hash;
    inventoryGeneration = inventory.generation;
  } else {
    rows = await loadAllPricingTemplatePages();
  }
  const operationKind: PreparedMigration["operationKind"] =
    useInventory &&
    hasEpoch &&
    currentCode &&
    targetCurrencyCode.trim().toUpperCase() === currentCode
      ? "repair_same_currency"
      : "currency_cutover";
  return {
    rows,
    names,
    inventoryId,
    inventoryHash,
    inventoryGeneration,
    operationKind,
  };
}

export async function submitCurrencyMigrationPreview(
  migration: PreparedMigration,
  currentCosting: CostingSettingsUpdate,
  targetCurrencyCode: string,
  targetCurrencySymbol: string,
  previewFailedMessage: string,
): Promise<{
  draft: CurrencyMigrationDraftHeader;
  preview: CurrencyMigrationPreview;
}> {
  const expectedUpdatedAt = currentCosting.expected_updated_at?.trim();
  const expectedEpochText = String(
    currentCosting.reporting_currency_epoch ?? "",
  ).trim();
  const expectedEpoch = expectedEpochText ? Number(expectedEpochText) : null;
  if (
    !expectedUpdatedAt ||
    (expectedEpoch !== null &&
      (!Number.isSafeInteger(expectedEpoch) || expectedEpoch < 1))
  ) {
    throw new Error(previewFailedMessage);
  }
  const draftId = crypto.randomUUID();
  const migrationOperationId = crypto.randomUUID();
  const created = await api.settings.costing.currencyMigrationDraftCreate({
    draft_id: draftId,
    migration_operation_id: migrationOperationId,
    operation_kind: migration.operationKind,
    target_currency_code: targetCurrencyCode.trim().toUpperCase(),
    target_currency_symbol: targetCurrencySymbol.trim(),
    expected_inventory_id: migration.inventoryId,
    expected_inventory_hash: migration.inventoryHash,
    expected_inventory_generation: migration.inventoryGeneration,
    expected_reporting_currency_epoch: expectedEpoch,
    expected_settings_updated_at: expectedUpdatedAt,
  });
  for (
    let offset = 0, ordinal = 1;
    offset < migration.rows.length;
    offset += 100, ordinal += 1
  ) {
    await api.settings.costing.currencyMigrationDraftChunk(
      draftId,
      ordinal,
      migration.rows.slice(offset, offset + 100),
    );
  }
  const draft = await api.settings.costing.currencyMigrationDraftSeal(draftId);
  const preview = await api.settings.costing.currencyMigrationPreview({
    operation_kind: migration.operationKind,
    migration_operation_id: created.migration_operation_id,
    draft_id: draft.draft_id,
    draft_hash: draft.draft_hash ?? "",
  });
  return { draft, preview };
}

export async function commitCurrencyMigration(
  preview: CurrencyMigrationPreview,
): Promise<void> {
  await api.settings.costing.currencyMigrationCommit({
    operation_kind: preview.operation_kind,
    migration_operation_id: preview.migration_operation_id,
    draft_id: preview.draft_id,
    draft_hash: preview.draft_hash,
    preview_hash: preview.preview_hash,
  });
}
