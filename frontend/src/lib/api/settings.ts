import type {
  AuditSettingsResponse,
  AuditSettingsUpdate,
  AuditStorageSummary,
  CancelRetentionJobResponse,
  CostingSettingsMutation,
  CostingSettingsResponse,
  CreateManualRetentionJobRequest,
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
  GlobalRetentionJobDetail,
  GlobalRetentionJobList,
  GlobalRetentionJobSummary,
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
  HeaderBlocklistRuleUpdate,
  ManualCleanupPreflightRequest,
  PolicyChangePreflightRequest,
  PricingMigrationInventoryTemplatePage,
  RetentionPreflightResponse,
  RetentionSettingsResponse,
  RetentionSettingsUpdate,
  UserAgentClientRule,
  UserAgentClientRuleCreate,
  UserAgentClientRuleUpdate,
} from "../types";
import { request } from "./core";

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

export const settingsAudit = {
  get: () => request<AuditSettingsResponse>("/api/settings/audit"),
  update: (data: AuditSettingsUpdate) =>
    request<{ operation_id: string; replayed: boolean; settings: AuditSettingsResponse }>("/api/settings/audit", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  storageSummary: () => request<AuditStorageSummary>("/api/settings/audit/storage-summary"),
};

export const settingsRetention = {
  get: () => request<RetentionSettingsResponse>("/api/settings/log-retention"),
  update: (data: RetentionSettingsUpdate) =>
    request<{
      settings: RetentionSettingsResponse;
      changes: Array<Record<string, unknown>>;
      scheduled_work: Array<Record<string, unknown>>;
      operation_id: string;
      replayed: boolean;
    }>("/api/settings/log-retention", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  preflight: (data: PolicyChangePreflightRequest | ManualCleanupPreflightRequest) =>
    request<RetentionPreflightResponse>("/api/maintenance/log-retention/preflights", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  createJob: (data: CreateManualRetentionJobRequest) =>
    request<{ operation_id: string; replayed: boolean; job: GlobalRetentionJobSummary }>("/api/maintenance/log-retention/jobs", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  jobs: {
    list: (params?: { origin?: "manual" | "automatic"; state?: string[]; cursor?: string }) => {
      const query = new URLSearchParams({ scope: "global", type: "log_retention" });
      if (params?.origin) query.set("origin", params.origin);
      if (params?.state?.length) query.set("state", params.state.join(","));
      if (params?.cursor) query.set("cursor", params.cursor);
      return request<GlobalRetentionJobList>(`/api/management/jobs?${query.toString()}`);
    },
    get: (id: string) =>
      request<GlobalRetentionJobDetail>(`/api/management/jobs/${encodeURIComponent(id)}?scope=global&type=log_retention`),
    checkpoints: (id: string, params?: { limit?: number; cursor?: string }) => {
      const query = new URLSearchParams({ scope: "global", type: "log_retention" });
      if (params?.limit) query.set("limit", String(params.limit));
      if (params?.cursor) query.set("cursor", params.cursor);
      return request<GlobalRetentionJobDetail["checkpoints"]>(`/api/management/jobs/${encodeURIComponent(id)}/checkpoints?${query.toString()}`);
    },
    partitions: (id: string, params?: { limit?: number; cursor?: string }) => {
      const query = new URLSearchParams({ scope: "global", type: "log_retention" });
      if (params?.limit) query.set("limit", String(params.limit));
      if (params?.cursor) query.set("cursor", params.cursor);
      return request<GlobalRetentionJobDetail["partitions"]>(`/api/management/jobs/${encodeURIComponent(id)}/partitions?${query.toString()}`);
    },
    cancel: (id: string, operationId: string) =>
      request<CancelRetentionJobResponse>(
        `/api/management/jobs/${encodeURIComponent(id)}/cancel?scope=global&type=log_retention`,
        { method: "POST", body: JSON.stringify({ operation_id: operationId }) },
      ),
  },
};

export const config = {
  headerBlocklistRules: {
    list: (includeDisabled = true) =>
      request<HeaderBlocklistRule[]>(
        `/api/config/header-blocklist-rules?include_disabled=${includeDisabled}`
      ),
    get: (id: number) => request<HeaderBlocklistRule>(`/api/config/header-blocklist-rules/${id}`),
    create: (data: HeaderBlocklistRuleCreate) =>
      request<HeaderBlocklistRule>("/api/config/header-blocklist-rules", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: number, data: HeaderBlocklistRuleUpdate) =>
      request<HeaderBlocklistRule>(`/api/config/header-blocklist-rules/${id}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
    delete: (id: number) =>
      request<void>(`/api/config/header-blocklist-rules/${id}`, {
        method: "DELETE",
      }),
  },
  userAgentClientRules: {
    list: (includeDisabled = true) =>
      request<UserAgentClientRule[]>(
        `/api/config/user-agent-client-rules?include_disabled=${includeDisabled}`
      ),
    get: (id: number) => request<UserAgentClientRule>(`/api/config/user-agent-client-rules/${id}`),
    create: (data: UserAgentClientRuleCreate) =>
      request<UserAgentClientRule>("/api/config/user-agent-client-rules", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: number, data: UserAgentClientRuleUpdate) =>
      request<UserAgentClientRule>(`/api/config/user-agent-client-rules/${id}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
    delete: (id: number) =>
      request<void>(`/api/config/user-agent-client-rules/${id}`, {
        method: "DELETE",
      }),
  },
};
