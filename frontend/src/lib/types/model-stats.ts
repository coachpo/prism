import type { ApiFamily } from "./vendor";
import type {
  Connection,
  ContextCapabilityFields,
  OpenAIAcceptedFormat,
} from "./routing";
import type { LoadbalanceStrategySummary } from "./loadbalance";
import type { UsageSnapshotPreset } from "./usage-statistics";
import type {
  CompatibilityTerminalTargetKind,
  PersistedTerminalTargetType,
  TerminalTargetProductKind,
} from "./target-compatibility";

export type StreamOutcome =
  | "not_streaming"
  | "completed"
  | "provider_incomplete"
  | "client_disconnected"
  | "upstream_read_error"
  | "upstream_ended_without_terminal"
  | "unknown";

export type StreamErrorKind =
  | "client_write_failed"
  | "request_context_canceled"
  | "upstream_read_failed"
  | "missing_terminal_event";

export type ModelAccessTargetType = "model" | PersistedTerminalTargetType;

export interface ModelAccessTargetModelSummary {
  id: number;
  profile_id: number;
  api_family: ApiFamily;
  model_id: string;
  display_name: string | null;
  openai_accepted_format: OpenAIAcceptedFormat | null;
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

export interface ModelConfig extends ContextCapabilityFields {
  id: number;
  profile_id: number;
  api_family: ApiFamily;
  model_id: string;
  display_name: string | null;
  openai_accepted_format: OpenAIAcceptedFormat | null;
  loadbalance_strategy_id: number | null;
  loadbalance_strategy: LoadbalanceStrategySummary | null;
  access_targets: ModelAccessTarget[];
  is_enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface ModelConfigListItem extends ContextCapabilityFields {
  id: number;
  profile_id: number;
  api_family: ApiFamily;
  model_id: string;
  display_name: string | null;
  openai_accepted_format: OpenAIAcceptedFormat | null;
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
  loadbalance_strategy_id?: number;
  context_window_tokens?: number | null;
  default_output_token_reserve?: number | null;
  max_context_utilization?: number | null;
  preferred_context_utilization_threshold?: number | null;
  access_targets?: ModelAccessTargetModelMutation[];
  is_enabled?: boolean;
}

export interface ModelConfigCreate extends ModelConfigMutationBase {
  api_family: ApiFamily;
  model_id: string;
  loadbalance_strategy_id: number;
  access_targets: ModelAccessTargetModelMutation[];
}

export type ModelConfigUpdate = ModelConfigMutationBase;

export interface RequestLogEntry {
  id: number;
  model_id: string;
  model_label: string;
  resolved_target_model_id: string | null;
  resolved_target_model_label: string | null;
  profile_id: number;
  api_family?: ApiFamily;
  endpoint_id: number | null;
  connection_id: number | null;
  terminal_target_id?: number | null;
  ingress_request_id: string | null;
  attempt_number: number | null;
  provider_correlation_id: string | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  ttft_ms: number | null;
  completion_duration_ms: number | null;
  status_code: number;
  response_time_ms: number;
  is_stream: boolean;
  stream_outcome: StreamOutcome;
  stream_error_kind: StreamErrorKind | null;
  input_tokens: number | null;
  output_tokens: number | null;
  total_tokens: number | null;
  success_flag: boolean | null;
  billable_flag: boolean | null;
  priced_flag: boolean | null;
  unpriced_reason: string | null;
  cache_read_input_tokens: number | null;
  cache_creation_input_tokens: number | null;
  reasoning_tokens: number | null;
  input_cost_micros: number | null;
  output_cost_micros: number | null;
  cache_read_input_cost_micros: number | null;
  cache_creation_input_cost_micros: number | null;
  reasoning_cost_micros: number | null;
  total_cost_original_micros: number | null;
  total_cost_user_currency_micros: number | null;
  currency_code_original: string | null;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
  fx_rate_used: string | null;
  fx_rate_source: string | null;
  pricing_snapshot_unit: string | null;
  pricing_snapshot_input: string | null;
  pricing_snapshot_output: string | null;
  pricing_snapshot_cache_read_input: string | null;
  pricing_snapshot_cache_creation_input: string | null;
  pricing_snapshot_reasoning: string | null;
  pricing_config_version_used: number | null;
  request_path: string;
  error_detail: string | null;
  created_at: string;
}

export interface RequestLogFilterModelOption {
  model_id: string;
  model_label: string;
}

export interface RequestLogFilterClientOption {
  client_rule_id: number;
  client_label: string;
}

export interface RequestLogFilterResolvedTargetModelOption {
  resolved_target_model_id: string;
  model_label: string;
}

export interface RequestLogListItem {
  id: number;
  created_at: string;
  model_id: string;
  model_label: string;
  resolved_target_model_id: string | null;
  resolved_target_model_label: string | null;
  caller_client_display: string | null;
  upstream_client_display: string | null;
  user_agent_overridden: boolean;
  api_family: ApiFamily;
  endpoint_id: number | null;
  endpoint_label: string;
  connection_id: number | null;
  terminal_target_id?: number | null;
  ttft_ms: number | null;
  completion_duration_ms: number | null;
  status_code: number;
  response_time_ms: number;
  is_stream: boolean;
  stream_outcome: StreamOutcome;
  stream_error_kind: StreamErrorKind | null;
  reasoning_effort: string | null;
  output_tokens: number | null;
  total_tokens: number | null;
  total_cost_user_currency_micros: number | null;
  priced_flag: boolean | null;
  unpriced_reason: string | null;
  report_currency_symbol: string | null;
}

export interface RequestLogDetailSummary {
  id: number;
  created_at: string;
  model_id: string;
  model_label: string;
  resolved_target_model_id: string | null;
  resolved_target_model_label: string | null;
  api_family: ApiFamily;
  status_code: number;
  response_time_ms: number;
  ttft_ms: number | null;
  completion_duration_ms: number | null;
  is_stream: boolean;
  stream_outcome: StreamOutcome;
  stream_error_kind: StreamErrorKind | null;
  stream_error_detail: string | null;
}

export interface RequestGenerationParamsReasoning {
  effort?: string | null;
  mode?: string | null;
  budget_tokens?: number | null;
  include_thoughts?: boolean | null;
  source_field?: string | null;
}

export interface RequestGenerationParams {
  provider?: string | null;
  temperature?: number | null;
  top_p?: number | null;
  top_k?: number | null;
  max_output_tokens?: number | null;
  max_output_tokens_source?: string | null;
  reasoning?: RequestGenerationParamsReasoning | null;
}

export interface RequestLogDetailRequest {
  request_path: string;
  ingress_request_id: string | null;
  attempt_number: number | null;
  provider_correlation_id: string | null;
  proxy_api_key_id: number | null;
  proxy_api_key_name_snapshot: string | null;
  caller_user_agent: string | null;
  upstream_user_agent: string | null;
  caller_client_display: string | null;
  upstream_client_display: string | null;
  user_agent_overridden: boolean;
  request_generation_params: RequestGenerationParams | null;
  request_generation_params_status: string | null;
  error_detail: string | null;
}

export interface RequestLogContextRoutingSkippedTerminalTarget {
  terminal_target_id?: number | null;
  endpoint_id?: number | null;
  reason: string;
  usable_context_window_tokens?: number | null;
  estimated_total_context_tokens?: number | null;
}

export interface RequestLogContextRouting {
  policy: string;
  selected_terminal_target_id?: number | null;
  estimation_method?: string | null;
  estimated_input_tokens?: number | null;
  reserved_output_tokens?: number | null;
  estimated_total_context_tokens?: number | null;
  usable_context_window_tokens?: number | null;
  cost_ranking_method?: string | null;
  selected_estimated_blended_cost_micros?: number | null;
  skipped_terminal_targets?: RequestLogContextRoutingSkippedTerminalTarget[];
}

export interface RequestLogDetailRouting {
  profile_id: number;
  endpoint_label: string;
  endpoint_id: number | null;
  terminal_target_id?: number | null;
  selected_terminal_target_id?: number | null;
  context_routing?: RequestLogContextRouting | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
}

export interface RequestLogDetailUsage {
  /** Base input tokens only; cache-read and cache-creation input are excluded. */
  input_tokens: number | null;
  /** Base output tokens only; reasoning output is excluded. */
  output_tokens: number | null;
  total_tokens: number | null;
  success_flag: boolean | null;
  billable_flag: boolean | null;
  priced_flag: boolean | null;
  unpriced_reason: string | null;
  cache_read_input_tokens: number | null;
  cache_creation_input_tokens: number | null;
  reasoning_tokens: number | null;
}

export interface RequestLogDetailCosting {
  input_cost_micros: number | null;
  output_cost_micros: number | null;
  cache_read_input_cost_micros: number | null;
  cache_creation_input_cost_micros: number | null;
  reasoning_cost_micros: number | null;
  total_cost_original_micros: number | null;
  total_cost_user_currency_micros: number | null;
  currency_code_original: string | null;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
  fx_rate_used: string | null;
  fx_rate_source: string | null;
}

export interface RequestLogDetailPricing {
  pricing_snapshot_unit: string | null;
  pricing_snapshot_input: string | null;
  pricing_snapshot_output: string | null;
  pricing_snapshot_cache_read_input: string | null;
  pricing_snapshot_cache_creation_input: string | null;
  pricing_snapshot_reasoning: string | null;
  pricing_config_version_used: number | null;
}

export interface RequestLogDetail {
  summary: RequestLogDetailSummary;
  request: RequestLogDetailRequest;
  routing: RequestLogDetailRouting;
  usage: RequestLogDetailUsage;
  costing: RequestLogDetailCosting;
  pricing: RequestLogDetailPricing;
}

export interface RequestLogFilterEndpointOption {
  endpoint_id: number;
  endpoint_label: string;
}

export interface RequestLogListResponse {
  items: RequestLogListItem[];
  total: number;
  limit: number;
  offset: number;
  filter_options: {
    endpoints: RequestLogFilterEndpointOption[];
    models: RequestLogFilterModelOption[];
    clients: RequestLogFilterClientOption[];
    resolved_target_models: RequestLogFilterResolvedTargetModelOption[];
  };
}

export interface StatGroup {
  key: string;
  total_requests: number;
  success_count: number;
  error_count: number;
  avg_response_time_ms: number;
  total_tokens: number;
}

export interface StatsSummary {
  total_requests: number;
  success_count: number;
  error_count: number;
  success_rate: number;
  avg_response_time_ms: number;
  p95_response_time_ms: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_tokens: number;
  groups: StatGroup[];
}

export type RequestStatusFamily = "4xx" | "5xx";

export const STATS_FROM_TIME_PARAM = "from_time" as const;
export const STATS_TO_TIME_PARAM = "to_time" as const;

export interface StatsRequestParams {
  ingress_request_id?: string;
  model_id?: string;
  client_rule_id?: number;
  resolved_target_model_id?: string;
  status_family?: RequestStatusFamily;
  from_time?: string;
  endpoint_id?: number;
  limit?: number;
  offset?: number;
}

export interface StatsSummaryParams {
  from_time?: string;
  to_time?: string;
  group_by?: "model" | "api_family" | "endpoint";
  model_id?: string;
  api_family?: ApiFamily;
  endpoint_id?: number;
  connection_id?: number;
}

export interface EndpointModelStatisticsParams {
  preset?: UsageSnapshotPreset;
  from_time?: string;
  to_time?: string;
}

export interface ModelMetricsBatchParams {
  model_ids: string[];
  summary_window_hours?: number;
  spending_preset?: "today" | "last_7_days" | "last_30_days" | "custom" | "all";
}

export interface ModelMetricsBatchItem {
  model_id: string;
  success_rate: number;
  request_count_24h: number;
  p95_latency_ms: number;
  spend_30d_micros: number;
}

export interface ModelMetricsBatchResponse {
  items: ModelMetricsBatchItem[];
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
  | "model"
  | "endpoint"
  | "model_endpoint";

export interface SpendingReportParams {
  preset?: "today" | "last_7_days" | "last_30_days" | "custom" | "all";
  from_time?: string;
  to_time?: string;
  api_family?: ApiFamily;
  model_id?: string;
  connection_id?: number;
  group_by?: SpendingGroupBy;
  limit?: number;
  offset?: number;
  top_n?: number;
}

export interface SpendingSummary {
  total_cost_micros: number;
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
  total_cost_micros: number;
  total_requests: number;
  priced_requests: number;
  unpriced_requests: number;
  total_tokens: number;
}

export interface SpendingTopModel {
  model_id: string;
  model_label: string;
  total_cost_micros: number;
}

export interface SpendingTopEndpoint {
  endpoint_id: number | null;
  endpoint_label: string;
  total_cost_micros: number;
}

export interface SpendingReportResponse {
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
}

export interface ThroughputStatsParams {
  from_time?: string;
  to_time?: string;
  model_id?: string;
  api_family?: string;
  endpoint_id?: number;
  connection_id?: number;
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
  avg_latency: number;
  error_rate: number;
  p95_latency: number;
  priced_request_count: number;
  stream_share: number;
  success_rate: number;
  total_cost: number;
  total_models: number;
  total_requests: number;
  unpriced_request_count: number;
}

export type DashboardTopologyNodeKind =
  | "endpoint"
  | "model"
  | CompatibilityTerminalTargetKind;

export type DashboardTopologyNodeProductKind =
  | "endpoint"
  | "model"
  | TerminalTargetProductKind;

export type DashboardTopologyEdgeKind =
  | "model_to_model"
  | "model_to_endpoint"
  | "model_to_connection"
  | "model_to_terminal_target"
  | "connection_to_endpoint"
  | "terminal_target_to_endpoint";

export type DashboardTopologyEdgeProductKind =
  | "model_to_model"
  | "model_to_terminal_target"
  | "terminal_target_to_endpoint";

export interface DashboardTopologyStats {
  model_count: number;
  active_model_count: number;
  disabled_model_count: number;
  terminal_target_count: number;
  active_terminal_target_count: number;
  inactive_terminal_target_count: number;
  endpoint_count: number;
  edge_count: number;
}

export interface DashboardTopologyNode {
  id: string;
  kind: DashboardTopologyNodeKind;
  product_kind?: DashboardTopologyNodeProductKind;
  label: string;
  sublabel?: string | null;
  status: string;
  model_config_id?: number | null;
  model_id?: string | null;
  terminal_target_id?: number | null;
  connection_id?: number | null;
  endpoint_id?: number | null;
  active?: boolean | null;
  health_status?: string | null;
  recent_request_count?: number | null;
  recent_success_rate?: number | null;
  last_request_at?: string | null;
}

export interface DashboardTopologyEdge {
  id: string;
  kind: DashboardTopologyEdgeKind;
  product_kind?: DashboardTopologyEdgeProductKind;
  source_node_id: string;
  target_node_id: string;
  position?: number | null;
  enabled?: boolean | null;
  source_model_config_id?: number | null;
  source_model_id?: string | null;
  target_model_config_id?: number | null;
  target_model_id?: string | null;
  terminal_target_id?: number | null;
  connection_id?: number | null;
  endpoint_id?: number | null;
}

export interface DashboardTopologyGraph {
  nodes: DashboardTopologyNode[];
  edges: DashboardTopologyEdge[];
  stats: DashboardTopologyStats;
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
  topology_graph?: DashboardTopologyGraph;
}

export interface DashboardRecentActivityItem {
  request_log_id: number;
  created_at: string;
  model_id: string;
  model_label: string;
  resolved_target_model_id: string | null;
  resolved_target_model_label: string | null;
  endpoint_id: number | null;
  endpoint_label: string;
  status_code: number;
  response_time_ms: number;
  ttft_ms: number | null;
  completion_duration_ms: number | null;
  is_stream: boolean;
  stream_outcome: StreamOutcome;
  total_tokens: number | null;
  total_cost_user_currency_micros: number | null;
  priced_flag: boolean | null;
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

export interface DashboardRealtimeSnapshotPayload {
  type: "dashboard.snapshot";
  profile_id: number;
  snapshot: DashboardSnapshot;
}

export interface DashboardRealtimeActivityPayload {
  type: "dashboard.activity";
  profile_id: number;
  activity_watermark: DashboardRecentActivityWatermark;
  activity: DashboardRecentActivityItem;
}

export type DashboardRealtimePayload =
  | DashboardRealtimeSnapshotPayload
  | DashboardRealtimeActivityPayload;

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
