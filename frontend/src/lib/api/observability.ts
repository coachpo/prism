import type {
  AuditLogDeleteResponse,
  AuditLogDetail,
  AuditLogListResponse,
  AuditLogParams,
  BatchDeleteResponse,
  ConfigExportResponse,
  ConfigImportPreviewResponse,
  ConfigImportRequest,
  ConfigImportResponse,
  ConnectionSuccessRate,
  ConnectionSuccessRateParams,
  CostingSettingsResponse,
  CostingSettingsUpdate,
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
  HeaderBlocklistRuleUpdate,
  LoadbalanceCurrentStateListResponse,
  LoadbalanceCurrentStateResetResponse,
  LoadbalanceEventDeleteResponse,
  LoadbalanceEventDetail,
  LoadbalanceEventListResponse,
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
  TimezonePreferenceResponse,
  TimezonePreferenceUpdate,
  ThroughputStatsResponse,
  RequestLogDetail,
} from "../types";
import { buildQuery, request } from "./core";

function buildStatsQuery(params?: StatsRequestParams) {
  return buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
}

export const stats = {
  requests: (params?: StatsRequestParams) => {
    const query = buildStatsQuery(params);
    return request<RequestLogListResponse>(`/api/stats/requests${query ? `?${query}` : ""}`);
  },
  requestDetail: (requestId: number) => request<RequestLogDetail>(`/api/stats/requests/${requestId}`),
  usageSnapshot: (params?: { preset?: UsageSnapshotPreset }) => {
    const query = buildQuery(params);
    return request<UsageSnapshotResponse>(`/api/stats/usage-snapshot${query ? `?${query}` : ""}`);
  },
  endpointModelStatistics: (
    endpointId: number,
    params?: { preset?: UsageSnapshotPreset; from_time?: string; to_time?: string },
  ) => {
    const query = buildQuery(params);
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
  throughput: (params?: {
    from_time?: string;
    to_time?: string;
    model_id?: string;
    api_family?: string;
    endpoint_id?: number;
    connection_id?: number;
  }) => {
    const query = buildQuery(params);
    return request<ThroughputStatsResponse>(`/api/stats/throughput${query ? `?${query}` : ""}`);
  },
  delete: (params: { older_than_days?: number; delete_all?: boolean }) => {
    const query = buildQuery(params);
    return request<BatchDeleteResponse>(`/api/stats/requests?${query}`, { method: "DELETE" });
  },
  deleteStatistics: (params: { older_than_days?: number; delete_all?: boolean }) => {
    const query = buildQuery(params);
    return request<BatchDeleteResponse>(`/api/stats/statistics?${query}`, { method: "DELETE" });
  },
};

export const settingsCosting = {
  get: () => request<CostingSettingsResponse>("/api/settings/costing"),
  update: (data: CostingSettingsUpdate) =>
    request<CostingSettingsResponse>("/api/settings/costing", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
};

export const settingsTimezone = {
  get: () => request<TimezonePreferenceResponse>("/api/settings/timezone"),
  update: (data: TimezonePreferenceUpdate) =>
    request<TimezonePreferenceResponse>("/api/settings/timezone", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
};

export const config = {
  export: () => request<ConfigExportResponse>("/api/config/profile/export"),
  previewImport: (data: ConfigImportRequest) =>
    request<ConfigImportPreviewResponse>("/api/config/profile/import/preview", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  import: (data: ConfigImportRequest) =>
    request<ConfigImportResponse>("/api/config/profile/import", {
      method: "POST",
      body: JSON.stringify(data),
    }),
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
};

export const audit = {
  list: (params?: AuditLogParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<AuditLogListResponse>(`/api/audit/logs${query ? `?${query}` : ""}`);
  },
  get: (id: number) => request<AuditLogDetail>(`/api/audit/logs/${id}`),
  delete: (params: { before?: string; older_than_days?: number; delete_all?: boolean }) => {
    const query = buildQuery(params);
    return request<AuditLogDeleteResponse>(`/api/audit/logs?${query}`, { method: "DELETE" });
  },
};

export const loadbalance = {
  listCurrentState: (params: { model_config_id: number }) => {
    const query = buildQuery(params);
    return request<LoadbalanceCurrentStateListResponse>(
      `/api/loadbalance/current-state${query ? `?${query}` : ""}`
    );
  },
  resetCurrentState: (connectionId: number) =>
    request<LoadbalanceCurrentStateResetResponse>(
      `/api/loadbalance/current-state/${connectionId}/reset`,
      { method: "POST" }
    ),
  listEvents: (params: {
    model_id: string;
    limit?: number;
    offset?: number;
  }) => {
    const query = buildQuery(params);
    return request<LoadbalanceEventListResponse>(`/api/loadbalance/events${query ? `?${query}` : ""}`);
  },
  getEvent: (eventId: number) => request<LoadbalanceEventDetail>(`/api/loadbalance/events/${eventId}`),
  deleteEvents: (params: { before?: string; older_than_days?: number; delete_all?: boolean }) => {
    const query = buildQuery(params);
    return request<LoadbalanceEventDeleteResponse>(`/api/loadbalance/events?${query}`, { method: "DELETE" });
  },
};
