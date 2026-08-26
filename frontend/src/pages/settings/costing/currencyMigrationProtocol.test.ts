import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  CostingSettingsUpdate,
  CurrencyMigrationCard,
  CurrencyMigrationDraftChunkItem,
  CurrencyMigrationPreview,
} from "@/lib/types";

const mocks = vi.hoisted(() => ({
  listPage: vi.fn(),
  inventoryTemplates: vi.fn(),
  draftCreate: vi.fn(),
  draftChunk: vi.fn(),
  draftSeal: vi.fn(),
  preview: vi.fn(),
  commit: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    pricingTemplates: {
      listPage: mocks.listPage,
    },
    settings: {
      costing: {
        currencyMigrationInventoryTemplates: mocks.inventoryTemplates,
        currencyMigrationDraftCreate: mocks.draftCreate,
        currencyMigrationDraftChunk: mocks.draftChunk,
        currencyMigrationDraftSeal: mocks.draftSeal,
        currencyMigrationPreview: mocks.preview,
        currencyMigrationCommit: mocks.commit,
      },
    },
  },
}));

import {
  commitCurrencyMigration,
  prepareCurrencyMigration,
  submitCurrencyMigrationPreview,
  type PreparedMigration,
} from "./currencyMigrationProtocol";

const card = {
  input_price: "1",
  output_price: "2",
  cached_input_price: null,
  cache_creation_price: null,
  reasoning_price: null,
};

const inventoryCard: CurrencyMigrationCard = {
  card_role: "standard",
  ...card,
};

function costing(overrides: Partial<CostingSettingsUpdate> = {}): CostingSettingsUpdate {
  return {
    report_currency_code: "USD",
    report_currency_symbol: "$",
    expected_updated_at: "2026-08-13T00:00:00Z",
    reporting_currency_epoch: "4",
    ...overrides,
  };
}

function preparedRows(count: number): CurrencyMigrationDraftChunkItem[] {
  return Array.from({ length: count }, (_, index) => ({
    template_id: index + 1,
    expected_version: 1,
    expected_updated_at: "2026-08-13T00:00:00Z",
    template_kind: "standard",
    cards: [inventoryCard],
  }));
}

describe("currency migration protocol", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("uses the immutable inventory for same-currency repair", async () => {
    mocks.inventoryTemplates.mockResolvedValue({
      items: [
        {
          template_id: 7,
          name: "Template 7",
          template_kind: "standard",
          base_version: 3,
          updated_at: "2026-08-13T00:00:00Z",
          current_cards: [inventoryCard],
        },
      ],
      next_cursor: null,
    });

    const prepared = await prepareCurrencyMigration(
      costing({
        pricing_migration_inventory: {
          inventory_id: "inventory-1",
          inventory_hash: "inventory-hash",
          generation: 9,
          template_issue_count: 1,
        } as unknown as CostingSettingsUpdate["pricing_migration_inventory"],
      }),
      " usd ",
    );

    expect(mocks.inventoryTemplates).toHaveBeenCalledWith("inventory-1", {
      limit: 100,
      cursor: undefined,
    });
    expect(mocks.listPage).not.toHaveBeenCalled();
    expect(prepared).toMatchObject({
      inventoryId: "inventory-1",
      inventoryHash: "inventory-hash",
      inventoryGeneration: 9,
      operationKind: "repair_same_currency",
      names: { 7: "Template 7" },
    });
  });

  it("chunks the draft before sealing and previewing with CAS witnesses", async () => {
    const migration: PreparedMigration = {
      rows: preparedRows(101),
      names: {},
      inventoryId: "inventory-1",
      inventoryHash: "inventory-hash",
      inventoryGeneration: 9,
      operationKind: "currency_cutover",
    };
    mocks.draftCreate.mockResolvedValue({ migration_operation_id: "migration-op" });
    mocks.draftSeal.mockResolvedValue({ draft_id: "draft-1", draft_hash: "draft-hash" });
    mocks.preview.mockResolvedValue({
      operation_kind: "currency_cutover",
      migration_operation_id: "migration-op",
      draft_id: "draft-1",
      draft_hash: "draft-hash",
      preview_hash: "preview-hash",
    } as CurrencyMigrationPreview);

    const result = await submitCurrencyMigrationPreview(
      migration,
      costing({ reporting_currency_epoch: "4" }),
      " eur ",
      " € ",
      "preview failed",
    );

    expect(mocks.draftCreate).toHaveBeenCalledWith(expect.objectContaining({
      operation_kind: "currency_cutover",
      target_currency_code: "EUR",
      target_currency_symbol: "€",
      expected_inventory_id: "inventory-1",
      expected_inventory_hash: "inventory-hash",
      expected_inventory_generation: 9,
      expected_reporting_currency_epoch: 4,
      expected_settings_updated_at: "2026-08-13T00:00:00Z",
    }));
    expect(mocks.draftChunk).toHaveBeenCalledTimes(2);
    const draftId = mocks.draftCreate.mock.calls[0][0].draft_id;
    expect(mocks.draftChunk.mock.calls[0][0]).toBe(draftId);
    expect(mocks.draftChunk.mock.calls[1][0]).toBe(draftId);
    expect(mocks.draftChunk.mock.calls[0][1]).toBe(1);
    expect(mocks.draftChunk.mock.calls[0][2]).toHaveLength(100);
    expect(mocks.draftChunk.mock.calls[1][1]).toBe(2);
    expect(mocks.draftChunk.mock.calls[1][2]).toHaveLength(1);
    expect(mocks.draftSeal).toHaveBeenCalledWith(draftId);
    expect(mocks.preview).toHaveBeenCalledWith({
      operation_kind: "currency_cutover",
      migration_operation_id: "migration-op",
      draft_id: "draft-1",
      draft_hash: "draft-hash",
    });
    expect(result).toEqual({
      draft: { draft_id: "draft-1", draft_hash: "draft-hash" },
      preview: expect.objectContaining({ preview_hash: "preview-hash" }),
    });
  });

  it("commits only the server-issued preview witnesses", async () => {
    const preview = {
      operation_kind: "repair_same_currency",
      migration_operation_id: "migration-op",
      draft_id: "draft-1",
      draft_hash: "draft-hash",
      preview_hash: "preview-hash",
    } as CurrencyMigrationPreview;

    await commitCurrencyMigration(preview);

    expect(mocks.commit).toHaveBeenCalledWith({
      operation_kind: "repair_same_currency",
      migration_operation_id: "migration-op",
      draft_id: "draft-1",
      draft_hash: "draft-hash",
      preview_hash: "preview-hash",
    });
  });
});
