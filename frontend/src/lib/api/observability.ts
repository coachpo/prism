import type { ChainResponse, RequestLogDetailV2 } from "../types/request-logs-v2";
import type {
  AuditLogDetail,
  AuditLogListResponse,
  AuditLogParams,
  AuditSettingsResponse,
  AuditSettingsUpdate,
  AuditStorageSummary,
  ProxyApiKeyFilterOptionsResponse,
  ConnectionSuccessRate,
  ConnectionSuccessRateParams,
  CostingSettingsResponse,
  CostingSettingsMutation,
  CurrencyMigrationDraftChunkItem,
  CurrencyMigrationDraftChunkPage,
  CurrencyMigrationDraftHeader,
  CurrencyMigrationDraftItemPage,
  CurrencyMigrationOperationKind,
  CurrencyMigrationDraftOperationKind,
  CurrencyMigrationCommitRequest,
  CurrencyMigrationCommitResponse,
  CurrencyMigrationPreview,
  CurrencyMigrationPreviewItemsResponse,
  PricingMigrationInventoryTemplatePage,
  CurrencyMigrationFXEvidencePage,
  DashboardRecentActivityParams,
  DashboardRecentActivityResponse,
  DashboardSnapshot,
  EventsQueryContextResponse,
  GlobalCurrentStateResponse,
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
  HeaderBlocklistRuleUpdate,
  LoadbalanceAdmissionReason,
  LoadbalanceCurrentStateResetResponse,
  LoadbalanceCurrentStateValue,
  LoadbalanceEventDetail,
  LoadbalanceEventListResponse,
  LoadbalanceEventType,
  LoadbalanceFailureKind,
  LoadbalanceIncidentListResponse,
  CancelRetentionJobResponse,
  CreateManualRetentionJobRequest,
  GlobalRetentionJobDetail,
  GlobalRetentionJobList,
  GlobalRetentionJobSummary,
  ManualCleanupPreflightRequest,
  PolicyChangePreflightRequest,
  RetentionPreflightResponse,
  RequestLogListResponse,
  SpendingReportParams,
  SpendingReportResponse,
  UsageSnapshotPreset,
  UsageSnapshotResponse,
  StatsRequestParams,
  StatsSummary,
  ModelMetricsBatchParams,
  ModelMetricsBatchResponse,
  UsageModelStatistic,
  StatsSummaryParams,
  RetentionSettingsResponse,
  RetentionSettingsUpdate,
  ThroughputStatsResponse,
  EndpointModelStatisticsParams,
  ThroughputStatsParams,
  UserAgentClientRule,
  UserAgentClientRuleCreate,
  UserAgentClientRuleUpdate,
} from "../types";
import { buildQuery, request } from "./core";
import type { QueryCoverage } from "@/lib/types/config-audit-settings";

type RequestLogAuditParams = Required<Pick<AuditLogParams, "from" | "to">> & { anchor_id?: number }
  & Pick<AuditLogParams, "limit" | "cursor">;

function buildStatsQuery(params?: StatsRequestParams) {
  return buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
}

export interface IngressChainRow {
  request_log_id: string;
  row_kind: string;
  created_at: string;
  model_id: string;
  resolved_target_model_id: string | null;
  attempt_number: number | null;
  attempt_trigger: string | null;
  attempt_result: string | null;
  is_winner: boolean | null;
  upstream_status_code: number | null;
  gateway_status_code: number | null;
  legacy_status_code: number | null;
  status_code: number | null;
  stream_outcome: string;
  stream_error_kind: string | null;
  error_code: string | null;
  error_detail: string | null;
}

export interface IngressChainItem {
  ingress_request_id: string;
  expected_attempt_count: number | null;
  retained_upstream_attempt_count: number;
  retained_request_log_row_count: number;
  legacy_unknown_row_count: number;
  chain_complete: boolean | null;
  retained_rows_loaded_count: number;
  retained_rows_page_complete: boolean;
  next_row_cursor: string;
  retained_rows: IngressChainRow[];
}

export interface IngressChainsResponse {
  items: IngressChainItem[];
  has_more_chains: boolean;
  next_chain_cursor: string;
  page_ingress_count: number;
  page_upstream_attempt_count: number;
  page_request_log_row_count: number;
}

export interface TerminalTargetStatistic {
  connection_id: number;
  connection_label: string;
  request_count: number;
  http_success_count: number;
  http_failed_count: number;
  final_failed_count: number;
  client_disconnected_count: number;
  p50_ttft_ms: number | null;
  p95_ttft_ms: number | null;
  avg_output_rate_tps: number | null;
  total_tokens: number;
  total_cost_micros: number;
  pricing_status_counts: { priced: number; unpriced: number; ineligible: number; unknown: number };
  unpriced_reason_counts: Record<string, number>;
  coverage: QueryCoverage;
  ban_event_count: number;
  admission_rejection_count: number;
  event_coverage_complete: boolean;
}

export interface TerminalTargetStatisticsResponse {
  items: TerminalTargetStatistic[];
  total: number;
  limit: number;
  offset: number;
  coverage: QueryCoverage;
  generated_at: string;
}

export const stats = {
  dashboard: () => request<DashboardSnapshot>("/api/stats/dashboard"),
  dashboardRecentActivity: (params?: DashboardRecentActivityParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<DashboardRecentActivityResponse>(`/api/stats/dashboard/recent-activity${query ? `?${query}` : ""}`);
  },
  requests: (params?: StatsRequestParams) => {
    const query = buildStatsQuery(params);
    return request<RequestLogListResponse>(`/api/stats/requests${query ? `?${query}` : ""}`);
  },
  requestIngressChains: (params?: StatsRequestParams) => {
    const query = buildStatsQuery(params);
    return request<IngressChainsResponse>(`/api/stats/requests${query ? `?${query}` : ""}`);
  },
  endpointTerminalTargets: (endpointId: number, params?: { preset?: string; from_time?: string; to_time?: string; limit?: number; offset?: number; cost_segment_key?: string }) => {
    const query = buildQuery(params as Record<string, string | number | null | undefined> | undefined);
    return request<TerminalTargetStatisticsResponse>(`/api/stats/endpoints/${endpointId}/terminal-targets${query ? `?${query}` : ""}`);
  },
  chains: (params?: StatsRequestParams & { view?: string; chain_cursor?: string; sort_by?: string; sort_order?: string }) => {
    const query = buildStatsQuery(params);
    return request<ChainResponse>(`/api/stats/requests${query ? `?${query}` : ""}`);
  },
  exportCsv: async (params?: StatsRequestParams): Promise<Blob> => {
    const query = buildStatsQuery(params);
    return request<Blob>(`/api/stats/requests/export${query ? `?${query}` : ""}`, {
      headers: { Accept: "text/csv" },
    }, { responseType: "blob" });
  },
  requestDetail: (requestId: number) => request<RequestLogDetailV2>(`/api/stats/requests/${requestId}`),
  proxyApiKeyFilterOptions: (params?: { q?: string; from_time?: string; to_time?: string; limit?: number; cursor?: string; selected_id?: number }) => {
    const query = buildQuery(params as Record<string, string | number | undefined> | undefined);
    return request<ProxyApiKeyFilterOptionsResponse>(`/api/stats/request-filter-options/proxy-api-keys${query ? `?${query}` : ""}`);
  },
  usageSnapshot: (params?: { preset?: UsageSnapshotPreset }) => {
    const query = buildQuery(params);
    return request<UsageSnapshotResponse>(`/api/stats/usage-snapshot${query ? `?${query}` : ""}`);
  },
  endpointModelStatistics: (
    endpointId: number,
    params?: EndpointModelStatisticsParams,
  ) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<UsageModelStatistic[]>(`/api/stats/endpoints/${endpointId}/models${query ? `?${query}` : ""}`);
  },
  summary: (params?: StatsSummaryParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<StatsSummary>(`/api/stats/summary${query ? `?${query}` : ""}`);
  },
  modelMetrics: (params: ModelMetricsBatchParams) =>
    request<ModelMetricsBatchResponse>("/api/stats/models/metrics", {
      method: "POST",
      body: JSON.stringify(params),
    }),
  connectionSuccessRates: (params?: ConnectionSuccessRateParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<ConnectionSuccessRate[]>(`/api/stats/connection-success-rates${query ? `?${query}` : ""}`);
  },
  spending: (params?: SpendingReportParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<SpendingReportResponse>(`/api/stats/spending${query ? `?${query}` : ""}`);
  },
  throughput: (params?: ThroughputStatsParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<ThroughputStatsResponse>(`/api/stats/throughput${query ? `?${query}` : ""}`);
  },
};

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

export const audit = {
  list: (params?: AuditLogParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<AuditLogListResponse>(`/api/audit/logs${query ? `?${query}` : ""}`);
  },
  listForRequestLog: (requestLogId: number, params: RequestLogAuditParams) => {
    const query = buildQuery({ ...params, request_log_id: requestLogId } as Record<string, string | number | boolean | null | undefined>);
    return request<AuditLogListResponse>(`/api/audit/logs${query ? `?${query}` : ""}`);
  },
  get: (id: number) => request<AuditLogDetail>(`/api/audit/logs/${id}`),
};

export type EventsQueryContextPreset = "1h" | "6h" | "24h" | "7d" | "30d" | "all" | "custom";

export interface EventsQueryContextParams {
  requested_preset: EventsQueryContextPreset;
  custom_from_time?: string;
  custom_to_time?: string;
}

export interface ListEventsParams {
  query_context: string;
  model_id?: string;
  event_type?: LoadbalanceEventType[];
  failure_kind?: LoadbalanceFailureKind[];
  admission_reason?: LoadbalanceAdmissionReason[];
  endpoint_id?: number;
  terminal_target_id?: number;
  sort_order?: "desc" | "asc";
  limit?: number;
  cursor?: string;
}

export interface ListCurrentStateParams {
  model_id?: string;
  state?: Array<LoadbalanceCurrentStateValue | "unobserved">;
  endpoint_id?: number;
  terminal_target_id?: number;
  limit?: number;
  cursor?: string;
}

export const loadbalance = {
  issueEventsQueryContext: (params: EventsQueryContextParams) =>
    request<EventsQueryContextResponse>("/api/loadbalance/events/query-context", {
      method: "POST",
      body: JSON.stringify(params),
    }),
  listCurrentState: (params: ListCurrentStateParams = {}) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<GlobalCurrentStateResponse>(
      `/api/loadbalance/current-state${query ? `?${query}` : ""}`
    );
  },
  resetCurrentState: (terminalTargetId: number) =>
    request<LoadbalanceCurrentStateResetResponse>(
      `/api/loadbalance/current-state/${terminalTargetId}/reset`,
      { method: "POST" }
    ),
  listEvents: (params: ListEventsParams) => {
    const query = buildQuery(params as unknown as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<LoadbalanceEventListResponse>(`/api/loadbalance/events${query ? `?${query}` : ""}`);
  },
  listIncidents: (params?: { limit?: number; since_hours?: number }) => {
    const query = buildQuery(params);
    return request<LoadbalanceIncidentListResponse>(`/api/loadbalance/incidents${query ? `?${query}` : ""}`);
  },
  getEvent: (eventId: string, queryContext: string) =>
    request<LoadbalanceEventDetail>(`/api/loadbalance/events/${eventId}?query_context=${encodeURIComponent(queryContext)}`),
};

// ---- Observe v2 read models ----

export type ObserveCoverage = {
  requested_preset: string;
  from_time: string;
  to_time: string;
  retention_from_time: string | null;
  source: "raw" | "rollup" | "hybrid";
  complete: boolean;
  gaps: { from_time: string; to_time: string; reason: string }[];
  retention_epoch?: string;
  retention_generation?: string;
  purge_state?: string;
  source_revision?: string;
};

export type ObserveCostSegment = {
  segment_key: string;
  reporting_currency_epoch: number | null;
  currency_attribution: "identified" | "legacy_unknown";
  currency_code: string | null;
  display_symbol: string | null;
  observed_symbols: string[];
  observed_symbol_count: number;
  observed_symbols_truncated: boolean;
  request_count: number;
  pricing_eligible_request_count: number;
  pricing_ineligible_request_count: number;
  priced_request_count: number;
  unpriced_request_count: number;
  pricing_unknown_request_count: number;
  unpriced_reason_counts: {
    PRICING_DISABLED: number;
    MISSING_TOKEN_USAGE: number;
    STREAM_USAGE_UNAVAILABLE: number;
    MISSING_PRICE_DATA: number;
  };
  pricing_coverage_state: "complete" | "partial" | "no_trusted_cost" | "no_eligible";
  known_cost_micros: string | null;
};

export type QueryContextResponse = {
  query_context: string;
  requested_bounds: { from_time: string; to_time: string } | null;
  usage_bounds: { from_time: string; to_time: string };
  usage_coverage: ObserveCoverage;
  event_bounds: { from_time: string; to_time: string };
  event_coverage: ObserveCoverage;
  request_bounds: { from_time: string; to_time: string };
  request_coverage: ObserveCoverage;
  generated_at: string;
};

export type UsageSummaryResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  cost_segments: ObserveCostSegment[];
  request_count: number;
  http_success_count: number;
  http_failed_count: number;
  http_success_rate: number | null;
  completed_count: number;
  stream_error_count: number;
  client_disconnected_count: number;
  failed_count: number;
  ttft_sample_count: number;
  p50_ttft_ms: number | null;
  p95_ttft_ms: number | null;
  output_rate_sample_count: number;
  avg_output_rate_tps: number | null;
  total_tokens: number | null;
  pricing_reconciliation: {
    pricing_eligible_request_count: number;
    pricing_ineligible_request_count: number;
    priced_request_count: number;
    unpriced_request_count: number;
    pricing_unknown_request_count: number;
    unpriced_reason_counts: {
      PRICING_DISABLED: number;
      MISSING_TOKEN_USAGE: number;
      STREAM_USAGE_UNAVAILABLE: number;
      MISSING_PRICE_DATA: number;
    };
    pricing_coverage_state: "complete" | "partial" | "no_trusted_cost" | "no_eligible";
  };
  window_average_rpm: number | null;
  window_average_tpm: number | null;
};

export type UsageSeriesResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  metric: string;
  group_by: string;
  selection_basis: string;
  interval: string;
  series_limit: number;
  truncated: boolean;
  series: {
    key: string;
    entity_id: string | null;
    label: string;
    configured: boolean | null;
    request_count: number;
    points: {
      bucket_start: string;
      request_count: number;
      http_success_count: number;
      http_failed_count: number;
      failed_count: number;
      client_disconnected_count: number;
      ttft_sample_count: number;
      p50_ttft_ms: number | null;
      p95_ttft_ms: number | null;
      total_tokens: number | null;
      known_cost_micros: string | null;
      pricing_reconciliation: UsageSummaryResponse["pricing_reconciliation"];
    }[];
  }[];
};

export type DashboardNowResponse = {
  generated_at: string;
  health: { stale: boolean; cache_lag_ms: number | null };
  rolling: {
    window_minutes: number;
    coverage: ObserveCoverage;
    request_count: number;
    token_sample_count: number;
    token_coverage_complete: boolean;
    token_count: number | null;
    rpm: number | null;
    tpm: number | null;
  };
  enabled_model_count: number;
};

export const modelRoutingDiagnostics = {
  get: (modelConfigId: number) => request<RoutingDiagnosticsResponse>(`/api/models/${modelConfigId}/routing-diagnostics`),
};

export const observe = {
  observeActivity: (queryContext: string, params: { limit?: number; before?: string }) => {
    const query = buildQuery({ ...params, query_context: queryContext });
    return request<ObserveActivityResponse>(`/api/stats/observe-activity${query ? `?${query}` : ""}`);
  },
  usageErrors: (queryContext: string, params: { group_by?: string; limit?: number }) => {
    const query = buildQuery({ ...params, query_context: queryContext });
    return request<UsageErrorsResponse>(`/api/stats/usage-errors${query ? `?${query}` : ""}`);
  },
  queryContext: (params: { preset: string; from_time?: string; to_time?: string }) => {
    const query = buildQuery(params);
    return request<QueryContextResponse>(`/api/stats/query-context${query ? `?${query}` : ""}`);
  },
  usageSummary: (queryContext: string) =>
    request<UsageSummaryResponse>(`/api/stats/usage-summary?query_context=${encodeURIComponent(queryContext)}`),
  usageSeries: (queryContext: string, params: { metric?: string; group_by?: string; interval?: string }) => {
    const query = buildQuery({ ...params, query_context: queryContext });
    return request<UsageSeriesResponse>(`/api/stats/usage-series${query ? `?${query}` : ""}`);
  },
  dashboardNow: () => request<DashboardNowResponse>("/api/stats/dashboard/now"),
};

export type UsageErrorsResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  requests_context: {
    view: string;
    query_context: string;
    final_from_time: string;
    final_to_time: string;
    base_request_filters: Record<string, string[]>;
  };
  summary: {
    request_count: number;
    http_error_count: number;
    stream_error_count: number;
    failed_count: number;
    client_disconnected_count: number;
    diagnostic_stream_anomaly_count: number;
  };
  timeline: {
    bucket_start: string;
    http_error_count: number;
    stream_error_count: number;
    failed_count: number;
    client_disconnected_count: number;
  }[];
  http_statuses: {
    status_code: number;
    count: number;
    denominator: number;
    percentage: number | null;
    last_seen_at: string;
    request_filters: Record<string, string[]>;
  }[];
  stream_outcomes: {
    stream_outcome: string;
    count: number;
    denominator: number;
    percentage: number | null;
    last_seen_at: string;
    request_filters: Record<string, string[]>;
    error_kinds: { stream_error_kind: string | null; count: number; denominator: number; percentage: number | null; request_filters: Record<string, string[]> }[];
    other_error_kinds: { count: number; denominator: number; percentage: number | null; request_filters: Record<string, string[]> | null };
  }[];
  groups: {
    entity_type: string;
    entity_id: string | null;
    label: string;
    configured: boolean | null;
    problem_count: number;
    failed_count: number;
    client_disconnected_count: number;
    denominator: number;
    percentage: number | null;
    last_seen_at: string;
    request_filters: Record<string, string[]>;
  }[];
  other: {
    http_statuses: { count: number; denominator: number; percentage: number | null; request_filters: Record<string, string[]> | null };
    stream_outcomes: { count: number; denominator: number; percentage: number | null; request_filters: Record<string, string[]> | null };
    groups: { count: number; denominator: number; percentage: number | null; request_filters: Record<string, string[]> | null };
  };
};

export type ObserveActivityItem = {
  usage_event_id: string;
  final_ingress_request_id: string;
  created_at: string;
  model_id: string;
  model_label: string;
  resolved_target_model_id: string | null;
  resolved_target_model_label: string | null;
  route_changed: boolean;
  attempt_count: number;
  routing_evidence_complete: boolean;
  endpoint_id: number | null;
  endpoint_label: string;
  terminal_target_id: number | null;
  status_code: number;
  final_result: "completed" | "failed" | "client_disconnected";
  outcome_detail: "completed" | "http_error" | "stream_error" | "client_disconnected";
  is_stream: boolean | null;
  stream_outcome: string;
  stream_error_kind: string | null;
  ttft_ms: number | null;
  total_duration_ms: number | null;
  output_tokens: number | null;
  total_tokens: number | null;
  known_cost_micros: string | null;
  final_pricing_status: string;
  final_unpriced_reason: string | null;
  reporting_currency_epoch: number | null;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
};

export type ObserveActivityResponse = {
  generated_at: string;
  coverage: ObserveCoverage;
  items: ObserveActivityItem[];
  has_more: boolean;
};

// ---- Model routing diagnostics (model connection UX pair) ----

export type RoutingDiagnosticTarget = {
  access_target_id: number;
  position: number;
  enabled_strategy_index: number | null;
  target_type: "model" | "connection";
  target_model_config_id: number | null;
  connection_id: number | null;
  target_mode: string | null;
  mode_match: boolean | null;
  operation_results: {
    operation_name: string;
    disposition: string;
    terminal_connection_ids: number[];
  }[];
};

export type RoutingDiagnosticRoute = {
  operation_name: string;
  accepted: boolean;
  configured_leaf_exists: boolean;
  statically_routable: boolean;
  access_target_ids: number[];
};

export type RoutingConfigurationWarning = {
  code: string;
  severity: "warning" | "danger";
  message: string;
  path: string;
  model_config_id: number;
  access_target_id: number | null;
  connection_id: number | null;
  operation_names: string[];
  details: Record<string, unknown>;
};

export type RoutingDiagnosticsResponse = {
  model_config_id: number;
  openai_accepted_format: string | null;
  strategy: { id: number; type: string } | null;
  accepted_operations: string[];
  targets: RoutingDiagnosticTarget[];
  operation_routes: RoutingDiagnosticRoute[];
  configuration_warnings: RoutingConfigurationWarning[];
};

// ---- Terminal Target batch copy (MC-B4) ----

export type TerminalTargetCopyResponse = {
  source_connection_id: number;
  items: {
    model_config_id: number;
    connection_summary: {
      id: number;
      name: string | null;
      endpoint_id: number;
      is_active: boolean;
      openai_text_capability: string | null;
      pricing_template: { id: number; name: string } | null;
      qps_limit: number | null;
      max_in_flight_non_stream: number | null;
      max_in_flight_stream: number | null;
      custom_header_count: number;
      custom_request_parameter_count: number;
    };
    access_target: { id: number; is_enabled: boolean; position: number };
  }[];
  configuration_warnings: RoutingConfigurationWarning[];
};

export const terminalTargetCopies = {
  create: (modelConfigId: number, connectionId: number, body: { destination_model_config_ids: number[]; enable_copies?: boolean }) =>
    request<TerminalTargetCopyResponse>(`/api/models/${modelConfigId}/connections/${connectionId}/copies`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
};
