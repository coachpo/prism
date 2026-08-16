export const USAGE_SNAPSHOT_PRESETS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type UsageSnapshotPreset = (typeof USAGE_SNAPSHOT_PRESETS)[number];

export const USAGE_CHART_GRANULARITIES = ["hourly", "daily"] as const;
export type UsageChartGranularity = (typeof USAGE_CHART_GRANULARITIES)[number];

export interface UsageStatisticsChartGranularityState {
  requestTrends: UsageChartGranularity;
  latencyTrends: UsageChartGranularity;
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
  /** Base input tokens only; cache-read and cache-creation input are excluded. */
  input_tokens: number;
  /** Base output tokens only; reasoning output is excluded. */
  output_tokens: number;
  /** Derived aggregate: cache-read input plus cache-creation input tokens. */
  cached_tokens: number;
  reasoning_tokens: number;
  /** Caliber of the component fields above; currently always "disjoint". */
  token_component_basis: string;
  /** total_tokens minus the sum of the disjoint components, clamped at zero. */
  uncategorized_tokens: number;
  average_rpm: number;
  average_tpm: number;
  total_cost_micros: number;
  rolling_window_minutes?: number;
  rolling_request_count?: number;
  rolling_token_count?: number;
  rolling_rpm?: number;
  rolling_tpm?: number;
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

export interface UsageLatencyTrendPoint {
  bucket_start: string;
  p50_ms: number | null;
  p95_ms: number | null;
}

export interface UsageLatencyTrendSeries {
  key: string;
  label: string;
  points: UsageLatencyTrendPoint[];
}

export interface UsageLatencyTrends {
  hourly: UsageLatencyTrendSeries[];
  daily: UsageLatencyTrendSeries[];
}

export interface UsageTokenTrendPoint {
  bucket_start: string;
  total_tokens: number;
  /** Base input tokens only; cache-read and cache-creation input are excluded. */
  input_tokens: number;
  /** Base output tokens only; reasoning output is excluded. */
  output_tokens: number;
  /** Derived aggregate: cache-read input plus cache-creation input tokens. */
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
  p50_ttft_ms: number | null;
  p95_ttft_ms: number | null;
  request_count: number;
  success_rate: number;
  total_tokens: number;
  avg_output_rate_tps: number | null;
  total_cost_micros: number;
}

export interface UsageModelStatistic {
  model_id: string;
  model_label: string;
  p50_ttft_ms: number | null;
  p95_ttft_ms: number | null;
  success_count: number | null;
  failed_count: number | null;
   priced_request_count: number | null;
   unpriced_request_count: number | null;
  request_count: number;
  success_rate: number;
  input_tokens: number | null;
  output_tokens: number | null;
  cached_tokens: number | null;
  reasoning_tokens: number | null;
  total_tokens: number;
  avg_output_rate_tps: number | null;
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
  request_trends: UsageRequestTrends;
  latency_trends: UsageLatencyTrends;
  token_usage_trends: UsageTokenUsageTrends;
  token_type_breakdown: UsageTokenTypeBreakdown;
  cost_overview: UsageCostOverview;
  endpoint_statistics: UsageEndpointStatistic[];
  model_statistics: UsageModelStatistic[];
  proxy_api_key_statistics: UsageProxyApiKeyStatistic[];
}
