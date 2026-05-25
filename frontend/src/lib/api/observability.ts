import type {
  AuditLogDetail,
  AuditLogListResponse,
  AuditLogParams,
  BootstrapConfigResponse,
  BootstrapConfigUpdateRequest,
  ConfigExportResponse,
  ConfigImportPreviewResponse,
  ConfigImportRequest,
  ConfigImportResponse,
  ConnectionSuccessRate,
  ConnectionSuccessRateParams,
  CostingSettingsResponse,
  CostingSettingsUpdate,
  DashboardSnapshot,
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
  HeaderBlocklistRuleUpdate,
  LoadbalanceCurrentStateListResponse,
  LoadbalanceCurrentStateResetResponse,
  LoadbalanceEventDetail,
  LoadbalanceEventListResponse,
  LogRetentionJobRequest,
  LogRetentionJobResponse,
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
  TimezonePreferenceResponse,
  TimezonePreferenceUpdate,
  ThroughputStatsResponse,
  RequestLogDetail,
  EndpointModelStatisticsParams,
  ThroughputStatsParams,
  UserAgentClientRule,
  UserAgentClientRuleCreate,
  UserAgentClientRuleUpdate,
  VendorCatalogExportResponse,
  VendorCatalogImportPreviewResponse,
  VendorCatalogImportRequest,
  VendorCatalogImportResponse,
} from "../types";
import { buildQuery, request } from "./core";

type RequestLogAuditParams = Required<Pick<AuditLogParams, "from" | "to">>
  & Pick<AuditLogParams, "limit" | "cursor">;

function buildStatsQuery(params?: StatsRequestParams) {
  return buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
}

export const stats = {
  dashboard: () => request<DashboardSnapshot>("/api/stats/dashboard"),
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

export const settingsRetention = {
  get: () => request<RetentionSettingsResponse>("/api/settings/log-retention"),
  update: (data: RetentionSettingsUpdate) =>
    request<RetentionSettingsResponse>("/api/settings/log-retention", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  createJob: (data: LogRetentionJobRequest) =>
    request<LogRetentionJobResponse>("/api/maintenance/log-retention/jobs", {
      method: "POST",
      body: JSON.stringify(data),
    }),
};

export const config = {
  bootstrap: {
    get: () => request<BootstrapConfigResponse>("/api/config/bootstrap"),
    validate: (data: BootstrapConfigUpdateRequest) =>
      request<BootstrapConfigResponse>("/api/config/bootstrap/validate", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (data: BootstrapConfigUpdateRequest) =>
      request<BootstrapConfigResponse>("/api/config/bootstrap", {
        method: "PUT",
        body: JSON.stringify(data),
      }),
  },
  export: () => request<ConfigExportResponse>("/api/config/profile/export"),
  exportWithSecrets: () =>
    request<ConfigExportResponse>("/api/config/profile/export/with-secrets", {
      method: "POST",
      headers: {
        "X-Prism-Dangerous-Confirm": "profile-export",
      },
    }),
  previewImport: (data: ConfigImportRequest) =>
    request<ConfigImportPreviewResponse>("/api/config/profile/import/preview", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  import: (data: ConfigImportRequest, previewToken: string) =>
    request<ConfigImportResponse>("/api/config/profile/import", {
      method: "POST",
      headers: {
        "X-Prism-Preview-Token": previewToken,
      },
      body: JSON.stringify(data),
    }),
  vendors: {
    export: () => request<VendorCatalogExportResponse>("/api/config/vendors/export"),
    previewImport: (data: VendorCatalogImportRequest) =>
      request<VendorCatalogImportPreviewResponse>("/api/config/vendors/import/preview", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    import: (data: VendorCatalogImportRequest, previewToken: string) =>
      request<VendorCatalogImportResponse>("/api/config/vendors/import", {
        method: "POST",
        headers: {
          "X-Prism-Preview-Token": previewToken,
        },
        body: JSON.stringify(data),
      }),
  },
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
};
