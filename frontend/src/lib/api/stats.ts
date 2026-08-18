import type { ChainResponse, RequestLogDetailV2 } from "../types/request-logs-v2";
import type {
  ConnectionSuccessRate,
  ConnectionSuccessRateParams,
  DashboardRecentActivityParams,
  DashboardRecentActivityResponse,
  DashboardSnapshot,
  EndpointModelStatisticsParams,
  ModelMetricsBatchParams,
  ModelMetricsBatchResponse,
  ProxyApiKeyFilterOptionsResponse,
  RequestLogListResponse,
  SpendingReportParams,
  SpendingReportResponse,
  StatsRequestParams,
  StatsSummary,
  StatsSummaryParams,
  ThroughputStatsParams,
  ThroughputStatsResponse,
  UsageModelStatistic,
  UsageSnapshotPreset,
  UsageSnapshotResponse,
} from "../types";
import type { QueryCoverage } from "@/lib/types/config-audit-settings";
import { buildQuery, request } from "./core";

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
  expected_request_log_row_count: number | null;
  retained_upstream_attempt_count: number;
  retained_request_log_row_count: number;
  legacy_unknown_row_count: number;
  chain_complete: boolean | null;
  retained_rows_loaded_count: number;
  retained_rows_page_complete: boolean;
  retained_row_count: number;
  matched_row_count: number;
  next_row_cursor: string | null;
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
  requestDetail: (requestId: string) => request<RequestLogDetailV2>(`/api/stats/requests/${requestId}`),
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
