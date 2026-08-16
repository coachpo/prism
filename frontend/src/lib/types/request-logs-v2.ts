// Requests/Audit v2 discriminated DTOs (Requests SPEC §3.4/§6.4/§6.5).
// BIGINT/micros fields stay decimal strings; never convert to JS number.

import type { RequestGenerationParams } from "./model-stats";
import type { QueryCoverage } from "./config-audit-settings";

export type RowKind = "planning" | "admission" | "upstream" | "legacy_unknown";

export type AttemptTrigger = "initial" | "retry_same_target" | "hedge" | "failover";

export type AttemptResult =
  | "completed"
  | "http_error"
  | "stream_error"
  | "transport_error"
  | "cancelled"
  | "client_disconnected"
  | "unknown";

export type ErrorSource = "prism" | "upstream" | "transport" | "client" | "unknown";

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
  | "snapshot_incoherent";

export type FinalResult = "completed" | "failed" | "client_disconnected";

// v2 slim row (Requests SPEC §6.4).
export interface RequestLogRowV2 {
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
  model_id: string;
  resolved_target_model_id: string | null;
  endpoint_id: number | null;
  terminal_target_id: number | null;
  terminal_target_label: string | null;
  terminal_target_configured: boolean;
  terminal_target_owner_model_id: string | null;
  total_tokens: number | null;
  total_cost_user_currency_micros: string | null;
  pricing_status: PricingStatus;
  unpriced_reason: UnpricedReason | null;
  pricing_evidence_trust: PricingEvidenceTrust;
  created_at: string;
  is_current?: boolean;
}

export interface FinalizedSummary {
  request_log_id?: string | null;
  final_status_code: number;
  final_result: FinalResult;
  final_error_code: string | null;
  requested_model: { id: string; label: string } | null;
  resolved_model: { id: string; label: string } | null;
  terminal_target: {
    id: number;
    label: string;
    configured: boolean;
    owner_model_id: string | null;
  } | null;
  endpoint: { id: number; label: string } | null;
  ttft_ms: number | null;
  output_rate_tps: number | null;
  total_tokens: number | null;
  total_cost_user_currency_micros: string | null;
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
  pricing_template_revision_id_used: string | null;
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
  final_target_entry_trigger: "initial" | "failover" | "hedge" | "unknown" | null;
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
  retained_rows: RequestLogRowV2[];
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
  filter_options?: {
    endpoints: Array<{ endpoint_id: number; endpoint_label: string }>;
    models: Array<{ model_id: string; model_label: string }>;
    clients: Array<{ client_rule_id: number; client_label: string }>;
    resolved_target_models: Array<{ resolved_target_model_id: string; model_label: string }>;
  };
  has_more_chains: boolean;
  next_chain_cursor: string | null;
  source_coverage?: QueryCoverage & { domain?: string; actual_coverage?: Record<string, unknown> } | null;
  raw_finalized_coverage?: QueryCoverage & { domain?: string; actual_coverage?: Record<string, unknown> } | null;
  attempt_coverage?: QueryCoverage & { domain?: string; actual_coverage?: Record<string, unknown> } | null;
  drilldown_coverage?: QueryCoverage & { domain?: string; actual_coverage?: Record<string, unknown> } | null;
  order_evidence_state?: string;
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

export interface TerminalTargetProjection {
  id: number;
  label: string;
  owner_model_id: string | null;
  endpoint_id: number | null;
  endpoint_label: string | null;
}

export interface PricingProjection {
  pricing_status: PricingStatus;
  unpriced_reason: UnpricedReason | null;
  pricing_resolution_kind: PricingResolutionKind | null;
  missing_price_components: string[] | null;
  pricing_evidence_trust: PricingEvidenceTrust;
  total_cost_user_currency_micros: string | null;
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
  pricing_template_revision_id_used: string | null;
  pricing_config_version_used: number | null;
  pricing_version_effective_at: string | null;
  pricing_snapshot_unit: string | null;
  pricing_snapshot_input: string | null;
  pricing_snapshot_output: string | null;
  pricing_snapshot_cache_read_input: string | null;
  pricing_snapshot_cache_creation_input: string | null;
  pricing_snapshot_reasoning: string | null;
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

export interface RequestLogDetailV2 {
  summary: {
    request_log_id: string;
    created_at: string;
    model_id: string;
    model_label: string;
    resolved_target_model_id: string | null;
    resolved_target_model_label: string | null;
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
}
