import type { ApiFamily } from "./vendor";
import type {
  Connection,
  OpenAIAcceptedFormat,
  OpenAIImageCapability,
  OpenAIImageOperations,
  OpenAITextCapability,
} from "./routing";
import type { LoadbalanceStrategySummary } from "./loadbalance";
import type { UsageSnapshotPreset } from "./usage-statistics";
import type { PersistedTerminalTargetType } from "./target-compatibility";
import type { StreamOutcome } from "./request-logs";

export type ModelAccessTargetType = "model" | PersistedTerminalTargetType;

export interface ModelAccessTargetModelSummary {
  id: number;
  profile_id: number;
  api_family: ApiFamily;
  model_id: string;
  display_name: string | null;
  openai_accepted_format: OpenAIAcceptedFormat | null;
  openai_image_operations: OpenAIImageOperations | null;
  loadbalance_strategy_id: number | null;
  is_enabled: boolean;
}

export interface ModelAccessTarget {
  id: number;
  target_type: ModelAccessTargetType;
  target_model_id: string | null;
  connection_id: number | null;
  terminal_target_id?: number | null;
  position: number;
  is_enabled: boolean;
  target_model: ModelAccessTargetModelSummary | null;
  connection: Connection | null;
  terminal_target?: Connection | null;
  created_at: string;
  updated_at: string;
}

export type ModelAccessTargetModelMutation = {
  target_type: "model";
  target_model_id: string;
  connection_id?: null;
  position: number;
  is_enabled?: boolean;
};

export type ModelAccessTargetConnectionMutation = {
  target_type: PersistedTerminalTargetType;
  connection_id: number;
  target_model_id?: null;
  position: number;
  is_enabled?: boolean;
};

export type ModelAccessTargetMutation =
  | ModelAccessTargetModelMutation
  | ModelAccessTargetConnectionMutation;

export type ModelAccessTargetCreate = ModelAccessTargetModelMutation;

export interface ModelAccessTargetUpdate {
  position?: number;
  is_enabled?: boolean;
}

export interface ModelConfig {
  id: number;
  profile_id: number;
  api_family: ApiFamily;
  model_id: string;
  display_name: string | null;
  openai_accepted_format: OpenAIAcceptedFormat | null;
  openai_image_operations: OpenAIImageOperations | null;
  loadbalance_strategy_id: number | null;
  loadbalance_strategy: LoadbalanceStrategySummary | null;
  access_targets: ModelAccessTarget[];
  is_enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface ModelConfigListItem {
  id: number;
  profile_id: number;
  api_family: ApiFamily;
  model_id: string;
  display_name: string | null;
  openai_accepted_format: OpenAIAcceptedFormat | null;
  openai_image_operations: OpenAIImageOperations | null;
  loadbalance_strategy_id: number | null;
  loadbalance_strategy: LoadbalanceStrategySummary | null;
  access_targets: ModelAccessTarget[];
  is_enabled: boolean;
  connection_count: number;
  active_connection_count: number;
  health_success_rate: number | null;
  health_total_requests: number;
  created_at: string;
  updated_at: string;
}

interface ModelConfigMutationBase {
  api_family?: ApiFamily;
  model_id?: string;
  display_name?: string | null;
  openai_accepted_format?: OpenAIAcceptedFormat | null;
  openai_image_operations?: OpenAIImageOperations | null;
  loadbalance_strategy_id?: number;
  is_enabled?: boolean;
}

export interface ModelConfigCreate extends ModelConfigMutationBase {
  api_family: ApiFamily;
  model_id: string;
  loadbalance_strategy_id: number;
}

/** Composite create: model + optional first Terminal Target in one transaction. */
export interface ModelConfigCompositeCreate extends ModelConfigCreate {
  initial_terminal_target?: {
    endpoint_id?: number;
    endpoint_create?: {
      name: string;
      base_url: string;
      api_key: string;
    };
    name?: string | null;
    is_active?: boolean;
    openai_text_capability?: OpenAITextCapability | null;
    openai_image_capability?: OpenAIImageCapability | null;
    pricing_template_id?: number | null;
    qps_limit?: number | null;
    max_in_flight_non_stream?: number | null;
    max_in_flight_stream?: number | null;
    custom_headers?: Record<string, string> | null;
    custom_headers_redacted?: string[] | null;
    custom_request_parameters?: Record<string, unknown> | null;
  };
}

export type ModelConfigUpdate = ModelConfigMutationBase;

export interface StatGroup {
  key: string;
  total_requests: number;
  success_count: number;
  error_count: number;
  success_rate?: number | null;
  avg_response_time_ms: number | null;
  p95_response_time_ms?: number | null;
  total_tokens: number;
  samples?: ScopeMetricSamples;
}

export interface StatsSummary {
  total_requests: number;
  success_count: number;
  error_count: number;
  success_rate: number | null;
  avg_response_time_ms: number | null;
  p95_response_time_ms: number | null;
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  groups: StatGroup[];
  granularity: string;
  latency_basis: string;
  caliber: ScopeCaliber;
  coverage: Record<string, unknown>;
  samples: ScopeMetricSamples;
}

export interface StatsSummaryParams {
  from_time?: string;
  to_time?: string;
  group_by?:
    | "none"
    | "api_family"
    | "ingress_model"
    | "final_target_model"
    | "attempt_target_model"
    | "endpoint"
    | "terminal_target"
    | "attempt_trigger"
    | "attempt_result";
  ingress_model_id?: string;
  final_target_model_id?: string;
  attempt_target_model_id?: string;
  api_family?: ApiFamily;
  endpoint_id?: number;
  connection_id?: number;
  attempt_trigger?: string;
  attempt_result?: string;
  scope?: ObservabilityScope;
}

export interface EndpointModelStatisticsParams {
  preset?: UsageSnapshotPreset;
  from_time?: string;
  to_time?: string;
  scope?: "final_execution" | "route_attempt";
}

export interface ModelMetricsBatchParams {
  model_ids: string[];
  summary_window_hours?: number;
  spending_preset?: "today" | "last_7_days" | "last_30_days" | "custom" | "all";
}

export type ObservabilityScope =
  | "ingress"
  | "final_execution"
  | "route_attempt";

export interface ScopeCaliber {
  scope: ObservabilityScope;
  grain: string;
  identity_basis: string;
  outcome_basis: string;
  latency_basis: string;
  cost_basis: string;
  datasets: string[];
}

export interface ScopeMetricSamples {
  observation_count: number;
  latency_sample_count: number;
  latency_missing_count: number;
  cost_sample_count: number;
  cost_missing_count: number;
}

export interface ScopeMetricBlock {
  request_count: number;
  success_rate: number | null;
  p95_latency_ms: number | null;
  known_cost_micros: number | null;
  caliber: ScopeCaliber;
  samples: ScopeMetricSamples;
}

export interface ModelMetricsBatchItem {
  model_id: string;
  ingress: ScopeMetricBlock;
  final_execution: ScopeMetricBlock;
  route_attempt: ScopeMetricBlock;
}

export interface ModelMetricsBatchResponse {
  items: ModelMetricsBatchItem[];
  coverage: {
    quality: Record<string, unknown>;
    spending: Record<string, unknown>;
  };
}

export interface ConnectionSuccessRate {
  connection_id: number;
  total_requests: number;
  success_count: number;
  error_count: number;
  success_rate: number | null;
}

export interface ConnectionSuccessRateParams {
  from_time?: string;
  to_time?: string;
}

export type SpendingGroupBy =
  | "none"
  | "day"
  | "week"
  | "month"
  | "api_family"
  | "ingress_model"
  | "final_target_model"
  | "attempt_target_model"
  | "endpoint"
  | "terminal_target";

export interface SpendingReportParams {
  preset?: "today" | "last_7_days" | "last_30_days" | "custom" | "all";
  from_time?: string;
  to_time?: string;
  api_family?: ApiFamily;
  ingress_model_id?: string;
  final_target_model_id?: string;
  endpoint_id?: number;
  terminal_target_id?: number;
  group_by?: SpendingGroupBy;
  limit?: number;
  offset?: number;
  top_n?: number;
  scope?: ObservabilityScope;
}

export interface SpendingSummary {
  known_cost_micros: number | null;
  cost_sample_count: number;
  cost_missing_count: number;
  successful_request_count: number;
  priced_request_count: number;
  unpriced_request_count: number;
  /** Base input tokens only; cache-read and cache-creation input are excluded. */
  total_input_tokens: number;
  /** Base output tokens only; reasoning output is excluded. */
  total_output_tokens: number;
  total_cache_read_input_tokens: number;
  total_cache_creation_input_tokens: number;
  total_reasoning_tokens: number;
  total_tokens: number;
  avg_cost_per_successful_request_micros: number;
}

export interface SpendingGroupRow {
  key: string;
  known_cost_micros: number | null;
  cost_sample_count: number;
  total_requests: number;
  priced_requests: number;
  unpriced_requests: number;
  total_tokens: number;
}

export interface SpendingTopModel {
  model_id: string;
  model_label: string;
  known_cost_micros: number | null;
}

export interface SpendingTopEndpoint {
  endpoint_id: number | null;
  endpoint_label: string;
  known_cost_micros: number | null;
}

export interface SpendingReportResponse {
  scope: ObservabilityScope;
  caliber: ScopeCaliber;
  coverage: Record<string, unknown>;
  summary: SpendingSummary;
  groups: SpendingGroupRow[];
  groups_total: number;
  top_spending_models: SpendingTopModel[];
  top_spending_endpoints: SpendingTopEndpoint[];
  unpriced_breakdown: Record<string, number>;
  report_currency_code: string;
  report_currency_symbol: string;
}

export interface ThroughputBucket {
  timestamp: string;
  request_count: number;
  rpm: number;
}

export interface ThroughputStatsResponse {
  average_rpm: number;
  peak_rpm: number;
  current_rpm: number;
  total_requests: number;
  time_window_seconds: number;
  buckets: ThroughputBucket[];
  caliber: ScopeCaliber;
  coverage: Record<string, unknown>;
  samples: ScopeMetricSamples;
}

export interface ThroughputStatsParams {
  from_time?: string;
  to_time?: string;
  model_id?: string;
  api_family?: string;
  endpoint_id?: number;
  connection_id?: number;
  scope?: ObservabilityScope;
}

export interface DashboardSnapshotCoverage {
  from: string;
  to: string;
}

export interface DashboardSnapshotHealth {
  lag_seconds: number;
  stale: boolean;
  stale_after_seconds: number;
}

export interface DashboardMetricSnapshot {
  active_models: number;
  average_rpm: number;
  average_rpm_request_total: number;
  avg_latency: number | null;
  error_rate: number | null;
  p95_latency: number | null;
  priced_request_count: number;
  stream_share: number;
  success_rate: number | null;
  total_cost: number;
  total_models: number;
  total_requests: number;
  unpriced_request_count: number;
}

export interface DashboardRoutingNode {
  id: string;
  name: string;
  kind: "endpoint" | "model";
  label: string;
  sublabel: string | null;
  endpointId: number | null;
  modelId: string | null;
  modelConfigId: number | null;
  activeConnectionCount: number;
  activeTerminalTargetCount?: number;
  trafficRequestCount24h: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
  successRate24h: number | null;
}

export interface DashboardRoutingLink {
  id: string;
  sourceNodeId: string;
  targetNodeId: string;
  modelId: string;
  modelLabel: string;
  modelConfigId: number;
  endpointId: number;
  endpointLabel: string;
  activeConnectionCount: number;
  activeTerminalTargetCount?: number;
  trafficRequestCount24h: number;
  requestCount24h: number;
  successCount24h: number;
  errorCount24h: number;
  successRate24h: number | null;
}

export interface DashboardRoutingHealthMap {
  nodes: DashboardRoutingNode[];
  links: DashboardRoutingLink[];
  endpointCount: number;
  modelCount: number;
  activeConnectionTotal: number;
  activeTerminalTargetTotal?: number;
  trafficRequestTotal24h: number;
}

export interface DashboardSnapshotSourceWatermark {
  latest_usage_event_created_at: string | null;
  latest_usage_event_id: number | null;
}

export interface DashboardRecentActivityWatermark {
  latest_request_log_created_at: string | null;
  latest_request_log_id: number | null;
}

export interface DashboardSnapshot {
  generated_at: string;
  snapshot_revision: string;
  source_watermark: DashboardSnapshotSourceWatermark;
  coverage_24h: DashboardSnapshotCoverage;
  coverage_30d: DashboardSnapshotCoverage;
  health: DashboardSnapshotHealth;
  metric_snapshot: DashboardMetricSnapshot;
  api_family_rows: StatGroup[];
  top_spending_models: SpendingTopModel[];
  routing_health_map: DashboardRoutingHealthMap;
}

export interface DashboardRecentActivityItem {
  request_log_id: number;
  created_at: string;
  ingress_model_id: string;
  ingress_model_label: string;
  attempt_target_model_id: string | null;
  attempt_target_model_label: string | null;
  endpoint_id: number | null;
  endpoint_label: string;
  status_code: number | null;
  response_time_ms: number | null;
  ttft_ms: number | null;
  completion_duration_ms: number | null;
  is_stream: boolean;
  stream_outcome: StreamOutcome;
  total_tokens: number | null;
  total_cost_user_currency_micros: number | null;
  pricing_status: "priced" | "unpriced" | "ineligible" | "unknown";
  unpriced_reason: string | null;
  report_currency_symbol: string | null;
}

export interface DashboardRecentActivityResponse {
  generated_at: string;
  activity_watermark: DashboardRecentActivityWatermark;
  items: DashboardRecentActivityItem[];
}

export interface DashboardRecentActivityParams {
  limit?: number;
}

export interface EndpointModelsBatchParams {
  endpoint_ids: number[];
}

export interface EndpointModelsBatchItem {
  endpoint_id: number;
  models: ModelConfigListItem[];
}

export interface EndpointModelsBatchResponse {
  items: EndpointModelsBatchItem[];
}
