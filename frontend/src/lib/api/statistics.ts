import type {
  ConnectionSuccessRate,
  ConnectionSuccessRateParams,
  DashboardRecentActivityParams,
  DashboardRecentActivityResponse,
  DashboardSnapshot,
  EndpointModelStatisticsParams,
  ModelMetricsBatchParams,
  ModelMetricsBatchResponse,
  SpendingReportParams,
  SpendingReportResponse,
  StatsSummary,
  StatsSummaryParams,
  ThroughputStatsParams,
  ThroughputStatsResponse,
  UsageModelStatistic,
  UsageSnapshotPreset,
  UsageSnapshotResponse,
} from "../types";
import type { QueryCoverage } from "@/lib/types/config-audit-settings";
import { buildQuery, request } from "./request";

export interface TerminalTargetStatistic {
  connection_id: number;
  connection_label: string;
  request_count: number;
  http_success_count: number;
  http_failed_count: number;
  final_failed_count: number;
  client_disconnected_count: number;
  p50_latency_ms: number | null;
  p95_latency_ms: number | null;
  avg_output_rate_tps: number | null;
  total_tokens: number;
  known_cost_micros: number | null;
  total_cost_micros?: number;
  pricing_status_counts: {
    priced: number;
    unpriced: number;
    ineligible: number;
    unknown: number;
  };
  unpriced_reason_counts: Record<string, number>;
  coverage: QueryCoverage;
  ban_event_count: number;
  admission_rejection_count: number;
  event_coverage_complete: boolean;
  samples?: {
    observation_count: number;
    latency_sample_count: number;
    latency_missing_count: number;
    cost_sample_count: number;
    cost_missing_count: number;
  };
}

export interface TerminalTargetStatisticsResponse {
  items: TerminalTargetStatistic[];
  total: number;
  limit: number;
  offset: number;
  coverage: QueryCoverage;
  generated_at: string;
  scope: "final_execution" | "route_attempt";
  caliber?: Record<string, unknown>;
  dataset_coverage?: Record<string, unknown>;
}

export const statistics = {
  dashboard: () => request<DashboardSnapshot>("/api/stats/dashboard"),
  dashboardRecentActivity: (params?: DashboardRecentActivityParams) => {
    const query = buildQuery(
      params as
        | Record<string, string | number | boolean | null | undefined>
        | undefined,
    );
    return request<DashboardRecentActivityResponse>(
      `/api/stats/dashboard/recent-activity${query ? `?${query}` : ""}`,
    );
  },
  endpointTerminalTargets: (
    endpointId: number,
    params?: {
      preset?: string;
      from_time?: string;
      to_time?: string;
      limit?: number;
      offset?: number;
      cost_segment_key?: string;
      scope?: "final_execution" | "route_attempt";
    },
  ) => {
    const query = buildQuery(
      params as Record<string, string | number | null | undefined> | undefined,
    );
    return request<TerminalTargetStatisticsResponse>(
      `/api/stats/endpoints/${endpointId}/terminal-targets${query ? `?${query}` : ""}`,
    );
  },
  usageSnapshot: (params?: {
    preset?: UsageSnapshotPreset;
    scope?: "ingress" | "final_execution" | "route_attempt";
  }) => {
    const query = buildQuery(params);
    return request<UsageSnapshotResponse>(
      `/api/stats/usage-snapshot${query ? `?${query}` : ""}`,
    );
  },
  endpointModelStatistics: (
    endpointId: number,
    params?: EndpointModelStatisticsParams,
  ) => {
    const query = buildQuery(
      params as
        | Record<string, string | number | boolean | null | undefined>
        | undefined,
    );
    return request<UsageModelStatistic[]>(
      `/api/stats/endpoints/${endpointId}/models${query ? `?${query}` : ""}`,
    );
  },
  summary: (params?: StatsSummaryParams) => {
    const query = buildQuery(
      params as
        | Record<string, string | number | boolean | null | undefined>
        | undefined,
    );
    return request<StatsSummary>(
      `/api/stats/summary${query ? `?${query}` : ""}`,
    );
  },
  modelMetrics: (params: ModelMetricsBatchParams) =>
    request<ModelMetricsBatchResponse>("/api/stats/models/metrics", {
      method: "POST",
      body: JSON.stringify(params),
    }),
  connectionSuccessRates: (params?: ConnectionSuccessRateParams) => {
    const query = buildQuery(
      params as
        | Record<string, string | number | boolean | null | undefined>
        | undefined,
    );
    return request<ConnectionSuccessRate[]>(
      `/api/stats/connection-success-rates${query ? `?${query}` : ""}`,
    );
  },
  spending: (params?: SpendingReportParams) => {
    const query = buildQuery(
      params as
        | Record<string, string | number | boolean | null | undefined>
        | undefined,
    );
    return request<SpendingReportResponse>(
      `/api/stats/spending${query ? `?${query}` : ""}`,
    );
  },
  throughput: (params?: ThroughputStatsParams) => {
    const query = buildQuery(
      params as
        | Record<string, string | number | boolean | null | undefined>
        | undefined,
    );
    return request<ThroughputStatsResponse>(
      `/api/stats/throughput${query ? `?${query}` : ""}`,
    );
  },
};
