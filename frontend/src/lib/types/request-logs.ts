// Requests/Audit request-log DTOs (Requests SPEC §3.4/§6.4/§6.5).
// Backend emits the request-log BIGINT/micros fields as JSON numbers.

import type { ApiFamily } from "./vendor";
import type { QueryCoverage } from "./config-audit-settings";
import type { PricingTemplateKind } from "./routing";

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

export interface RequestLogFilterModelOption {
  ingress_model_id: string;
  model_label: string;
}

export interface RequestLogFilterClientOption {
  client_rule_id: number;
  client_label: string;
}

export interface RequestLogFilterResolvedTargetModelOption {
  attempt_target_model_id: string;
  model_label: string;
}

export interface RequestLogListItem {
  request_log_id: string;
  row_kind: RowKind;
  ingress_request_id: string | null;
  attempt_number: number | null;
  attempt_trigger: string | null;
  attempt_result: string | null;
  is_winner: boolean | null;
  created_at: string;
  ingress_model_id: string;
  model_label: string;
  attempt_target_model_id: string | null;
  attempt_target_model_label: string | null;
  caller_client_display: string | null;
  upstream_client_display: string | null;
  user_agent_overridden: boolean;
  api_family: ApiFamily;
  endpoint_id: number | null;
  endpoint_label: string;
  terminal_target_id: number | null;
  terminal_target_label: string | null;
  terminal_target_configured: boolean;
  terminal_target_owner_model_id: string | null;
  ttft_ms: number | null;
  completion_duration_ms: number | null;
  output_rate_tps: number | null;
  output_rate_state: "measured" | "unmeasurable" | "not_applicable" | "unknown";
  output_rate_reason: string | null;
  upstream_status_code: number | null;
  gateway_status_code: number | null;
  legacy_status_code: number | null;
  attempt_duration_ms: number | null;
  legacy_duration_ms: number | null;
  is_stream: boolean;
  stream_outcome: StreamOutcome;
  stream_error_kind: StreamErrorKind | null;
  error_source: string | null;
  error_code: string | null;
  failure_stage: string | null;
  failure_detail_preview: string | null;
  failure_detail_source: "error_detail" | "stream_error_detail";
  failure_detail_preview_truncated: boolean;
  failure_detail_redacted: boolean;
  reasoning_effort: string | null;
  output_tokens: number | null;
  total_tokens: number | null;
  total_cost_user_currency_micros: number | null;
  pricing_status: "priced" | "unpriced" | "ineligible" | "unknown";
  pricing_resolution_kind: PricingResolutionKind | null;
  pricing_evidence_trust: "trusted" | "legacy_untrusted";
  unpriced_reason: string | null;
  pricing_template_kind: PricingTemplateKind | null;
  pricing_selection_state: PricingSelectionState | null;
  pricing_card_role: PricingCardRole | null;
  pricing_selector_threshold_tokens: number | null;
  pricing_selector_basis_tokens: number | null;
  report_currency_symbol: string | null;
  proxy_api_key_id: number | null;
  proxy_api_key_name_snapshot: string | null;
  proxy_api_key_attribution_state: "identified" | "none" | "unknown";
  proxy_api_key_auth_enforced_at_request: boolean | null;
  id?: number;
  connection_id?: number | null;
  status_code?: number | null;
  response_time_ms?: number | null;
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

export interface RequestLogFilterEndpointOption {
  endpoint_id: number;
  endpoint_label: string;
}

export interface RequestLogListResponse {
  items: RequestLogListItem[];
  total: number;
  total_is_exact: boolean;
  has_more: boolean;
  limit: number;
  offset: number;
  filter_options: {
    endpoints: RequestLogFilterEndpointOption[];
    ingress_models: RequestLogFilterModelOption[];
    clients: RequestLogFilterClientOption[];
    attempt_target_models: RequestLogFilterResolvedTargetModelOption[];
  };
  coverage: QueryCoverage;
  caliber: Record<string, unknown>;
  dataset_coverage: Record<string, unknown>;
  samples: Record<string, number>;
}

export type RequestStatusFamily = "2xx" | "4xx" | "5xx";

export const STATS_FROM_TIME_PARAM = "from_time" as const;
export const STATS_TO_TIME_PARAM = "to_time" as const;

export type RepeatableRequestFilter<T extends string | number> = T | T[];

export interface StatsRequestParams {
  time_range?: "1h" | "6h" | "24h" | "7d" | "30d" | "all" | "custom";
  ingress_request_id?: string;
  proxy_api_key_id?: number | string;
  ingress_model_id?: string;
  client_rule_id?: number | string;
  attempt_target_model_id?: RepeatableRequestFilter<string>;
  api_family?: RepeatableRequestFilter<string>;
  row_kind?: RepeatableRequestFilter<RowKind>;
  attempt_trigger?: RepeatableRequestFilter<AttemptTrigger | "__null__">;
  attempt_result?: RepeatableRequestFilter<AttemptResult | "__null__">;
  status_family?: RequestStatusFamily;
  status_code?: RepeatableRequestFilter<number | string>;
  stream_outcome?: RepeatableRequestFilter<string>;
  stream_error_kind?: RepeatableRequestFilter<string>;
  error_text?: string;
  pricing_status?: "priced" | "unpriced" | "ineligible" | "unknown";
  unpriced_reason?: RepeatableRequestFilter<string>;
  pricing_card_role?:
    | "standard"
    | "tier_base"
    | "tier_above"
    | "peak"
    | "offpeak";
  pricing_selection_state?:
    | "not_evaluated"
    | "not_applicable"
    | "selected"
    | "unresolved";
  from_time?: string;
  to_time?: string;
  endpoint_id?: RepeatableRequestFilter<number | string>;
  terminal_target_id?: RepeatableRequestFilter<number | string>;
  limit?: number;
  offset?: number;
  view?: "attempts" | "ingress_chains";
  chain_cursor?: string;
  row_cursor?: string;
  chain_limit?: number;
  chain_row_limit?: number;
  anchor_request_log_id?: string;
  ingress_final_result?: "completed" | "failed" | "client_disconnected";
  query_context?: string;
  final_result?: RepeatableRequestFilter<
    "completed" | "failed" | "client_disconnected"
  >;
  outcome_detail?: RepeatableRequestFilter<string>;
  final_status_code?: RepeatableRequestFilter<number | string>;
  final_stream_outcome?: RepeatableRequestFilter<string>;
  final_stream_error_kind?: RepeatableRequestFilter<string>;
  final_exclude?: RepeatableRequestFilter<string>;
  final_target_model_id?: RepeatableRequestFilter<string>;
  final_endpoint_id?: RepeatableRequestFilter<number | string>;
  final_terminal_target_id?: RepeatableRequestFilter<number | string>;
  final_pricing_status?: "priced" | "unpriced" | "ineligible" | "unknown";
  final_unpriced_reason?: RepeatableRequestFilter<string>;
  confirmed_failover?: string;
  is_stream?: boolean;
  currency_code?: string;
  reporting_currency_epoch?: number | string;
  cost_segment_key?: string;
  sort_by?: string;
  sort_order?: "asc" | "desc";
}

export type RowKind = "planning" | "admission" | "upstream" | "legacy_unknown";

export type AttemptTrigger =
  | "initial"
  | "retry_same_target"
  | "hedge"
  | "failover";

export type AttemptResult =
  | "completed"
  | "http_error"
  | "stream_error"
  | "transport_error"
  | "cancelled"
  | "client_disconnected"
  | "unknown";

export type ErrorSource =
  | "prism"
  | "upstream"
  | "transport"
  | "client"
  | "unknown";

export type FailureStage =
  | "routing"
  | "admission"
  | "upstream_connect"
  | "upstream_response"
  | "stream"
  | "unknown";

export type PricingStatus = "priced" | "unpriced" | "ineligible" | "unknown";

export type PricingEvidenceTrust = "trusted" | "legacy_untrusted";

export type UnpricedReason =
  | "PRICING_DISABLED"
  | "MISSING_TOKEN_USAGE"
  | "STREAM_USAGE_UNAVAILABLE"
  | "MISSING_PRICE_DATA";

export type PricingResolutionKind =
  | "missing_component"
  | "currency_migration_required"
  | "unsupported_unit"
  | "snapshot_incoherent"
  | "schedule_unresolved";

export type PricingSelectionState =
  | "not_evaluated"
  | "not_applicable"
  | "selected"
  | "unresolved";
export type PricingCardRole =
  | "standard"
  | "tier_base"
  | "tier_above"
  | "peak"
  | "offpeak";

export type FinalResult = "completed" | "failed" | "client_disconnected";

// Request-log chain row (Requests SPEC §6.4).
export interface RequestLogChainRow {
  request_log_id: string;
  row_kind: RowKind;
  ingress_request_id: string | null;
  attempt_number: number | null;
  attempt_trigger: AttemptTrigger | null;
  attempt_result: AttemptResult | null;
  is_winner: boolean | null;
  attempt_duration_ms: number | null;
  legacy_duration_ms: number | null;
  upstream_status_code: number | null;
  gateway_status_code: number | null;
  legacy_status_code: number | null;
  error_source: ErrorSource | null;
  error_code: string | null;
  failure_stage: FailureStage | null;
  failure_detail_preview: string | null;
  failure_detail_source: "error_detail" | "stream_error_detail";
  failure_detail_preview_truncated: boolean;
  failure_detail_redacted: boolean;
  failure_detail_persistence_truncated: boolean;
  stream_outcome: string;
  stream_error_kind: string | null;
  ingress_model_id: string;
  attempt_target_model_id: string | null;
  attempt_target_model_label?: string | null;
  endpoint_id: number | null;
  endpoint_label?: string | null;
  terminal_target_id: number | null;
  selected_terminal_target_id?: number | null;
  terminal_target_label: string | null;
  terminal_target_configured: boolean;
  terminal_target_owner_model_id: string | null;
  total_tokens: number | null;
  total_cost_user_currency_micros: number | null;
  pricing_status: PricingStatus;
  unpriced_reason: UnpricedReason | null;
  pricing_resolution_kind: PricingResolutionKind | null;
  pricing_evidence_trust: PricingEvidenceTrust;
  pricing_template_kind: PricingTemplateKind | null;
  pricing_selection_state: PricingSelectionState | null;
  pricing_card_role: PricingCardRole | null;
  pricing_selector_threshold_tokens: number | null;
  pricing_selector_basis_tokens: number | null;
  created_at: string;
  is_current?: boolean;
}

export interface FinalizedSummary {
  request_log_id?: string | null;
  final_status_code: number;
  final_result: FinalResult;
  final_error_code: string | null;
  ingress_model: { id: string; label: string } | null;
  final_target_model: { id: string; label: string } | null;
  terminal_target: {
    id: number;
    label: string;
    configured: boolean;
    owner_model_id: string | null;
  } | null;
  endpoint: { id: number; label: string } | null;
  ttft_ms: number | null;
  output_rate_tps: number | null;
  output_rate_state: "measured" | "unmeasurable" | "not_applicable" | "unknown";
  output_rate_reason: string | null;
  total_tokens: number | null;
  total_cost_user_currency_micros: number | null;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
  reporting_currency_epoch: number | null;
  currency_attribution: "identified" | "legacy_unknown";
  cost_segment_key: string | null;
  final_pricing_status: PricingStatus;
  final_unpriced_reason: UnpricedReason | null;
  final_pricing_resolution_kind: PricingResolutionKind | null;
  missing_price_components: string[] | null;
  final_pricing_evidence_trust: PricingEvidenceTrust;
  pricing_template_id_used: number | null;
  pricing_template_name_snapshot: string | null;
  pricing_template_revision_id_used: number | null;
  pricing_config_version_used: number | null;
  pricing_version_effective_at: string | null;
  pricing_snapshot_unit: string | null;
  pricing_snapshot_input: string | null;
  pricing_snapshot_output: string | null;
  pricing_snapshot_cache_read_input: string | null;
  pricing_snapshot_cache_creation_input: string | null;
  pricing_snapshot_reasoning: string | null;
  attempt_count: number;
  final_attempt_number: number | null;
  final_attempt_trigger: AttemptTrigger | null;
  final_target_entry_trigger:
    | "initial"
    | "failover"
    | "hedge"
    | "unknown"
    | null;
}

export interface ChainIngressItem {
  ingress_request_id: string;
  started_at: string | null;
  completed_at: string | null;
  elapsed_ms: number | null;
  elapsed_evidence_state: "authoritative" | "unavailable";
  finalized_evidence_state: "authoritative" | "unavailable";
  finalized_summary: FinalizedSummary | null;
  expected_attempt_count: number | null;
  expected_request_log_row_count: number | null;
  retained_upstream_attempt_count: number;
  retained_request_log_row_count: number;
  legacy_unknown_row_count: number;
  chain_complete: boolean | null;
  same_target_retry_occurred: boolean;
  hedge_occurred: boolean;
  failover_occurred: boolean;
  routing_evidence_complete: boolean | null;
  retained_rows_loaded_count: number;
  retained_rows_page_complete: boolean;
  retained_row_count: number;
  matched_row_count: number;
  next_row_cursor: string | null;
  retained_rows: RequestLogChainRow[];
  order_evidence_state?: string;
}

export interface ChainResponse {
  view: "ingress_chains";
  query_context: string | null;
  source_ingress_total: number | null;
  retained_ingress_total: number;
  retained_upstream_attempt_total: number;
  retained_request_log_row_total: number;
  legacy_unknown_row_total: number;
  page_ingress_count: number;
  page_upstream_attempt_count: number;
  page_request_log_row_count: number;
  items: ChainIngressItem[];
  filter_options: {
    endpoints: Array<{ endpoint_id: number; endpoint_label: string }>;
    ingress_models: Array<{ ingress_model_id: string; model_label: string }>;
    clients: Array<{ client_rule_id: number; client_label: string }>;
    attempt_target_models: Array<{
      attempt_target_model_id: string;
      model_label: string;
    }>;
  };
  has_more_chains: boolean;
  next_chain_cursor: string | null;
  source_coverage?:
    | (QueryCoverage & {
        domain?: string;
        actual_coverage?: Record<string, unknown>;
      })
    | null;
  raw_finalized_coverage?:
    | (QueryCoverage & {
        domain?: string;
        actual_coverage?: Record<string, unknown>;
      })
    | null;
  attempt_coverage?:
    | (QueryCoverage & {
        domain?: string;
        actual_coverage?: Record<string, unknown>;
      })
    | null;
  drilldown_coverage?:
    | (QueryCoverage & {
        domain?: string;
        actual_coverage?: Record<string, unknown>;
      })
    | null;
  order_evidence_state?: string;
  caliber: Record<string, unknown>;
  dataset_coverage: Record<string, unknown>;
  samples: Record<string, number>;
}

// Unified failure projection (Requests SPEC §6.4 exact detail).
export interface FailureProjection {
  category:
    | "planning"
    | "admission"
    | "upstream_http"
    | "transport"
    | "provider_stream"
    | "client_disconnect"
    | "unknown"
    | null;
  source: ErrorSource | null;
  stage: FailureStage | null;
  code: string | null;
  detail: string | null;
  detail_redacted: boolean;
  detail_truncated: boolean;
  detail_source: "error_detail" | "stream_error_detail";
  evidence_state: "authoritative" | "unavailable";
  upstream_request_started: boolean | null;
  response_headers_received: boolean | null;
  first_body_or_stream_event_seen: boolean | null;
  stream_outcome: string | null;
  stream_error_kind: string | null;
  stream_error_detail: string | null;
}

export interface PricingProjection {
  pricing_status: PricingStatus;
  unpriced_reason: UnpricedReason | null;
  pricing_resolution_kind: PricingResolutionKind | null;
  missing_price_components: string[] | null;
  pricing_evidence_trust: PricingEvidenceTrust;
  total_cost_user_currency_micros: number | null;
  total_cost_original_micros: string | null;
  currency_code_original: string | null;
  fx_rate_used: string | null;
  fx_rate_source: string | null;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
  reporting_currency_epoch: number | null;
  currency_attribution: "identified" | "legacy_unknown";
  cost_segment_key: string | null;
  pricing_template_id_used: number | null;
  pricing_template_name_snapshot: string | null;
  pricing_template_revision_id_used: number | null;
  pricing_config_version_used: number | null;
  pricing_version_effective_at: string | null;
  pricing_snapshot_unit: string | null;
  pricing_snapshot_input: string | null;
  pricing_snapshot_output: string | null;
  pricing_snapshot_cache_read_input: string | null;
  pricing_snapshot_cache_creation_input: string | null;
  pricing_snapshot_reasoning: string | null;
  pricing_template_kind: PricingTemplateKind | null;
  pricing_selection_state: PricingSelectionState | null;
  pricing_card_role: PricingCardRole | null;
  pricing_selector_threshold_tokens: number | null;
  pricing_selector_basis_tokens: number | null;
  pricing_schedule_decided_at: string | null;
  pricing_schedule_timezone: string | null;
  pricing_schedule_local_weekday: number | null;
  pricing_schedule_local_minute: number | null;
  pricing_schedule_digest: string | null;
  evidence_state: "authoritative" | "unavailable";
}

export interface LegacyPricingEvidence {
  raw_total_cost_original_micros: string | null;
  raw_total_cost_report_micros: string | null;
  raw_component_cost_micros: Record<string, string> | null;
  raw_price_snapshots: Record<string, string> | null;
  original_currency_code: string | null;
  report_currency_code: string | null;
  warning_code: "historical_unverified";
}

export interface RequestLogDetail {
  summary: {
    request_log_id: string;
    created_at: string;
    ingress_model_id: string;
    model_label: string;
    attempt_target_model_id: string | null;
    attempt_target_model_label: string | null;
    api_family: string;
    row_kind: RowKind;
    upstream_status_code: number | null;
    gateway_status_code: number | null;
    legacy_status_code: number | null;
    attempt_duration_ms: number | null;
    legacy_duration_ms: number | null;
    is_stream: boolean;
    stream_outcome: string;
    stream_error_kind: string | null;
    ttft_ms: number | null;
    completion_duration_ms: number | null;
    output_rate_tps: number | null;
    output_rate_state:
      | "measured"
      | "unmeasurable"
      | "not_applicable"
      | "unknown";
    output_rate_reason: string | null;
    attempt_number: number | null;
    attempt_trigger: AttemptTrigger | null;
    attempt_result: AttemptResult | null;
    is_winner: boolean | null;
  };
  request: {
    operation_name: string | null;
    upstream_operation_name: string | null;
    operation_translation_mode: string | null;
    request_path: string;
    upstream_request_path: string | null;
    ingress_request_id: string | null;
    provider_correlation_id: string | null;
    proxy_api_key_id: number | null;
    proxy_api_key_name_snapshot: string | null;
    proxy_api_key_attribution_state?: "identified" | "none" | "unknown";
    proxy_api_key_auth_enforced_at_request?: boolean | null;
    caller_user_agent: string | null;
    upstream_user_agent: string | null;
    caller_client_display: string | null;
    upstream_client_display: string | null;
    user_agent_overridden: boolean;
    request_generation_params: RequestGenerationParams | null;
    request_generation_params_status: string | null;
    metadata_redacted_fields: string[];
    metadata_truncated_fields: string[];
    url_scrub_provenance: string;
  };
  routing: {
    profile_id: number;
    endpoint_label: string;
    endpoint_id: number | null;
    terminal_target_id: number | null;
    selected_terminal_target_id: number | null;
    endpoint_base_url: string | null;
    endpoint_description: string | null;
    audit_enabled_at_request: boolean;
    audit_capture_bodies_at_request: boolean;
  };
  usage: {
    input_tokens: number | null;
    output_tokens: number | null;
    total_tokens: number | null;
    success_flag: boolean | null;
    cache_read_input_tokens: number | null;
    cache_creation_input_tokens: number | null;
    reasoning_tokens: number | null;
  };
  failure: FailureProjection | null;
  terminal_target: {
    kind: "terminal_target";
    terminal_target_id: string;
    owner_model_config_id: string | null;
    name: string | null;
    name_source: string;
    deleted: boolean;
    configured: boolean;
  } | null;
  endpoint: {
    kind: "endpoint";
    id: string;
    name: string | null;
    name_source: string;
    deleted: boolean;
    configured: boolean;
  } | null;
  routing_provenance: {
    initial_terminal_target: {
      kind: "terminal_target";
      terminal_target_id: string;
      owner_model_config_id: string | null;
      name: string | null;
      name_source: string;
      deleted: boolean;
      configured: boolean;
    } | null;
    differs_from_actual: boolean;
  };
  pricing: PricingProjection;
  legacy_pricing_evidence: LegacyPricingEvidence | null;
  current_pricing_template: {
    template_id: number;
    deleted: boolean;
    current_revision_id: string;
    current_version: number;
    current_effective_at: string | null;
    matches_request_revision: boolean;
  } | null;
  caliber: Record<string, unknown>;
  dataset_coverage: Record<string, unknown>;
  samples: Record<string, number>;
}
