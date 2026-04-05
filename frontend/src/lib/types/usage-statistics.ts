export const USAGE_SNAPSHOT_PRESETS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type UsageSnapshotPreset = (typeof USAGE_SNAPSHOT_PRESETS)[number];

export const USAGE_CHART_GRANULARITIES = ["hourly", "daily"] as const;
export type UsageChartGranularity = (typeof USAGE_CHART_GRANULARITIES)[number];

export interface UsageStatisticsChartGranularityState {
  requestTrends: UsageChartGranularity;
  tokenUsageTrends: UsageChartGranularity;
  tokenTypeBreakdown: UsageChartGranularity;
  costOverview: UsageChartGranularity;
}

export type UsageStatisticsChartKey = keyof UsageStatisticsChartGranularityState;

export interface UsageStatisticsPageState {
  selectedTimeRange: UsageSnapshotPreset;
  selectedModelLines: string[];
  chartGranularity: UsageStatisticsChartGranularityState;
}

export interface UsageSnapshotTimeRange {
  preset: UsageSnapshotPreset;
  start_at: string | null;
  end_at: string;
}

export interface UsageSnapshotCurrency {
  code: string;
  symbol: string;
}

export interface UsageSnapshotOverview {
  total_requests: number;
  success_requests: number;
  failed_requests: number;
  success_rate: number;
  total_tokens: number;
  input_tokens: number;
  output_tokens: number;
  cached_tokens: number;
  reasoning_tokens: number;
  average_rpm: number;
  average_tpm: number;
  total_cost_micros: number;
  rolling_window_minutes?: number;
  rolling_request_count?: number;
  rolling_token_count?: number;
  rolling_rpm?: number;
  rolling_tpm?: number;
}

export type UsageServiceHealthCellStatus = "ok" | "degraded" | "down" | "empty";

export interface UsageServiceHealthCell {
  bucket_start: string;
  request_count: number;
  success_count: number;
  failed_count: number;
  availability_percentage: number | null;
  status: UsageServiceHealthCellStatus;
}

export interface UsageServiceHealth {
  availability_percentage: number | null;
  request_count: number;
  success_count: number;
  failed_count: number;
  interval_minutes: number;
  cells: UsageServiceHealthCell[];
}

export interface UsageRequestTrendPoint {
  bucket_start: string;
  request_count: number;
  success_count: number;
  failed_count: number;
  rpm: number;
}

export interface UsageRequestTrendSeries {
  key: string;
  label: string;
  total_requests: number;
  points: UsageRequestTrendPoint[];
}

export interface UsageRequestTrends {
  hourly: UsageRequestTrendSeries[];
  daily: UsageRequestTrendSeries[];
}

export interface UsageTokenTrendPoint {
  bucket_start: string;
  total_tokens: number;
  input_tokens: number;
  output_tokens: number;
  cached_tokens: number;
  reasoning_tokens: number;
  tpm: number;
}

export interface UsageTokenTrendSeries {
  key: string;
  label: string;
  total_tokens: number;
  points: UsageTokenTrendPoint[];
}

export interface UsageTokenUsageTrends {
  hourly: UsageTokenTrendSeries[];
  daily: UsageTokenTrendSeries[];
}

export interface UsageTokenTypeBreakdownPoint {
  bucket_start: string;
  input_tokens: number;
  output_tokens: number;
  cached_tokens: number;
  reasoning_tokens: number;
}

export interface UsageTokenTypeBreakdown {
  hourly: UsageTokenTypeBreakdownPoint[];
  daily: UsageTokenTypeBreakdownPoint[];
}

export interface UsageCostOverviewPoint {
  bucket_start: string;
  total_cost_micros: number;
}

export interface UsageCostOverview {
  total_cost_micros: number;
  priced_request_count: number;
  unpriced_request_count: number;
  hourly: UsageCostOverviewPoint[];
  daily: UsageCostOverviewPoint[];
}

export interface UsageEndpointStatistic {
  endpoint_id: number | null;
  endpoint_label: string;
  request_count: number;
  success_rate: number;
  total_tokens: number;
  total_cost_micros: number;
}

export interface UsageModelStatistic {
  model_id: string;
  model_label: string;
  request_count: number;
  success_rate: number;
  total_tokens: number;
  total_cost_micros: number;
}

export interface UsageProxyApiKeyStatistic {
  proxy_api_key_id: number | null;
  proxy_api_key_label: string;
  request_count: number;
  success_rate: number;
  total_tokens: number;
  total_cost_micros: number;
}

export interface UsageSnapshotResponse {
  generated_at: string;
  time_range: UsageSnapshotTimeRange;
  currency: UsageSnapshotCurrency;
  overview: UsageSnapshotOverview;
  service_health: UsageServiceHealth;
  request_trends: UsageRequestTrends;
  token_usage_trends: UsageTokenUsageTrends;
  token_type_breakdown: UsageTokenTypeBreakdown;
  cost_overview: UsageCostOverview;
  endpoint_statistics: UsageEndpointStatistic[];
  model_statistics: UsageModelStatistic[];
  proxy_api_key_statistics: UsageProxyApiKeyStatistic[];
}
