import type {
  CostingSettingsMutation,
  CostingSettingsResponse,
  CurrencyMigrationCommitRequest,
  CurrencyMigrationCommitResponse,
  CurrencyMigrationDraftChunkItem,
  CurrencyMigrationDraftChunkPage,
  CurrencyMigrationDraftHeader,
  CurrencyMigrationDraftItemPage,
  CurrencyMigrationDraftOperationKind,
  CurrencyMigrationFXEvidencePage,
  CurrencyMigrationOperationKind,
  CurrencyMigrationPreview,
  CurrencyMigrationPreviewItemsResponse,
  PricingMigrationInventoryTemplatePage,
} from "../types";
import { request } from "./request";

export const settingsCosting = {
  get: () => request<CostingSettingsResponse>("/api/settings/costing"),
  update: (data: CostingSettingsMutation) =>
    request<CostingSettingsResponse>("/api/settings/costing", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  currencyMigrationDraftCreate: (data: {
    draft_id: string;
    migration_operation_id: string;
    operation_kind: CurrencyMigrationDraftOperationKind;
    target_currency_code: string;
    target_currency_symbol: string;
    expected_inventory_id: string | null;
    expected_inventory_hash: string | null;
    expected_inventory_generation: number | null;
    expected_reporting_currency_epoch: number | null;
    expected_settings_updated_at: string;
  }) => request<CurrencyMigrationDraftHeader>("/api/settings/costing/currency-migration-drafts", {
    method: "POST",
    body: JSON.stringify(data),
  }),
  currencyMigrationInventoryTemplates: (inventoryId: string, params?: { limit?: number; cursor?: string }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set("limit", String(params.limit));
    if (params?.cursor) query.set("cursor", params.cursor);
    const suffix = query.toString() ? `?${query.toString()}` : "";
    return request<PricingMigrationInventoryTemplatePage>(`/api/settings/costing/pricing-migration-inventories/${encodeURIComponent(inventoryId)}/templates${suffix}`);
  },
  currencyMigrationInventoryFXEvidence: (inventoryId: string, params?: { limit?: number; cursor?: string }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set("limit", String(params.limit));
    if (params?.cursor) query.set("cursor", params.cursor);
    const suffix = query.toString() ? `?${query.toString()}` : "";
    return request<CurrencyMigrationFXEvidencePage>(`/api/settings/costing/pricing-migration-inventories/${encodeURIComponent(inventoryId)}/fx-evidence${suffix}`);
  },
  currencyMigrationDraft: (draftId: string) =>
    request<CurrencyMigrationDraftHeader>(`/api/settings/costing/currency-migration-drafts/${encodeURIComponent(draftId)}`),
  currencyMigrationDraftChunks: (draftId: string, params?: { limit?: number; cursor?: string }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set("limit", String(params.limit));
    if (params?.cursor) query.set("cursor", params.cursor);
    const suffix = query.toString() ? `?${query.toString()}` : "";
    return request<CurrencyMigrationDraftChunkPage>(`/api/settings/costing/currency-migration-drafts/${encodeURIComponent(draftId)}/chunks${suffix}`);
  },
  currencyMigrationDraftChunk: (draftId: string, ordinal: number, items: CurrencyMigrationDraftChunkItem[]) =>
    request<CurrencyMigrationDraftHeader>(`/api/settings/costing/currency-migration-drafts/${encodeURIComponent(draftId)}/chunks/${ordinal}`, {
      method: "PUT",
      body: JSON.stringify({ items }),
    }),
  currencyMigrationDraftSeal: (draftId: string) =>
    request<CurrencyMigrationDraftHeader>(`/api/settings/costing/currency-migration-drafts/${encodeURIComponent(draftId)}/seal`, {
      method: "POST",
    }),
  currencyMigrationDraftItems: (draftId: string, params?: { limit?: number; cursor?: string }) => {
    const query = new URLSearchParams();
    if (params?.limit) query.set("limit", String(params.limit));
    if (params?.cursor) query.set("cursor", params.cursor);
    const suffix = query.toString() ? `?${query.toString()}` : "";
    return request<CurrencyMigrationDraftItemPage>(`/api/settings/costing/currency-migration-drafts/${encodeURIComponent(draftId)}/items${suffix}`);
  },
  currencyMigrationPreview: (data: {
    operation_kind: CurrencyMigrationOperationKind;
    migration_operation_id: string;
    draft_id: string;
    draft_hash: string;
    expected_inventory_id?: string;
    expected_inventory_hash?: string;
    expected_inventory_generation?: number;
    expected_reporting_currency_epoch?: number;
    expected_settings_updated_at?: string;
  }) =>
    request<CurrencyMigrationPreview>("/api/settings/costing/currency-migrations/preview", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  currencyMigrationPreviewItems: (draftId: string, previewHash: string, params?: { limit?: number; cursor?: string }) => {
    const query = new URLSearchParams({ preview_hash: previewHash });
    if (params?.limit) query.set("limit", String(params.limit));
    if (params?.cursor) query.set("cursor", params.cursor);
    return request<CurrencyMigrationPreviewItemsResponse>(`/api/settings/costing/currency-migration-drafts/${encodeURIComponent(draftId)}/preview-items?${query.toString()}`);
  },
  currencyMigrationCommit: (data: CurrencyMigrationCommitRequest) =>
    request<CurrencyMigrationCommitResponse>(
      "/api/settings/costing/currency-migrations/commit",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
    ),
};
