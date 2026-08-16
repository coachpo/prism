export type LoadbalanceEventType =
  | "retry_scheduled"
  | "retry_exhausted"
  | "banned"
  | "unbanned"
  | "recovered"
  | "admission_rejected";

export type LegacyLoadbalanceStrategyType =
  | "single"
  | "fill-first"
  | "round-robin";
export type LoadbalanceBanMode = "off" | "temporary" | "until_reset";

export type LoadbalanceFailureKind =
  | "transient_http"
  | "connect_error"
  | "timeout";

export type LoadbalanceAdmissionReason =
  | "qps_limit"
  | "max_in_flight_stream"
  | "max_in_flight_non_stream";

export interface LoadbalanceBanPolicyFields {
  legacy_strategy_type: LegacyLoadbalanceStrategyType;
  failure_status_codes: number[];
  ban_mode: LoadbalanceBanMode;
  retry_base_delay_ms: number;
  retry_backoff_multiplier: number;
  retry_jitter_ratio: number;
  retry_max_delay_ms: number;
  cycle_retry_attempt_limit: number;
  ban_cumulative_retry_attempt_threshold: number;
  ban_duration_seconds: number;
}

export interface LoadbalanceStrategySummary extends LoadbalanceBanPolicyFields {
  id: number;
  name: string;
}

export interface LoadbalanceStrategy extends LoadbalanceStrategySummary {
  profile_id: number;
  is_default: boolean;
  attached_model_count: number;
  created_at: string;
  updated_at: string;
}

export type LoadbalanceStrategyCreate = {
  name: string;
} & LoadbalanceBanPolicyFields;

export type LoadbalanceStrategyUpdate = LoadbalanceStrategyCreate;

export interface LoadbalanceStrategyCanonicalResult {
  canonical_name: string;
  strategy_id: number;
}

export interface LoadbalanceStrategyDefaultsResponse {
  created: LoadbalanceStrategyCanonicalResult[];
  existing: LoadbalanceStrategyCanonicalResult[];
  default_strategy_id: number | null;
  default_changed: boolean;
  complete: boolean;
}

export interface StrategySetDefaultRequest {
  expected_default_strategy_id: number | null;
}

export interface StrategySetDefaultResponse {
  default_strategy_id: number;
  previous_default_strategy_id: number | null;
  changed: boolean;
}

export interface StrategyImpactModelItem {
  model_config_id: number;
  model_id: string;
  display_name: string;
  is_enabled: boolean;
}

export interface StrategyImpactListResponse {
  strategy_id: number;
  attached_model_count: number;
  items: StrategyImpactModelItem[];
  has_more: boolean;
  next_cursor: string | null;
}

export interface StrategyPreviewStep {
  failure_ordinal: number;
  cycle_retry_attempt: number;
  cumulative_retry_attempt: number;
  nominal_delay_ms: number;
  jitter_min_delay_ms: number;
  jitter_max_delay_ms: number;
  cycle_exhausted: boolean;
  ban_transition: { mode: "temporary" | "until_reset"; duration_seconds: number } | null;
}

export interface StrategyPreviewBanProjection {
  mode: LoadbalanceBanMode;
  cumulative_retry_attempt_threshold: number;
  transition_at_cumulative_failure: number | null;
  duration_seconds: number;
}

export type StrategyPreviewDraft = Omit<LoadbalanceStrategyCreate, "name"> & { name?: string }

export interface StrategyPreviewResponse {
  normalized_policy: LoadbalanceStrategyCreate;
  steps: StrategyPreviewStep[];
  shown_step_count: number;
  has_more: boolean;
  termination_reason: "cycle_exhausted" | "ban_transition" | "five_step_limit";
  cycle_exhaustion_after_attempt: number;
  ban_projection: StrategyPreviewBanProjection;
}

export type LoadbalanceCurrentStateValue = "available" | "retry_wait" | "banned";

export interface LoadbalanceCurrentStateItem {
  connection_id: number;
  window_started_at: string | null;
  window_request_count: number;
  in_flight_non_stream: number;
  in_flight_stream: number;
  cycle_retry_attempts: number;
  cumulative_retry_attempts: number;
  next_retry_at: string | null;
  last_retry_delay_ms: number;
  ban_mode: LoadbalanceBanMode;
  banned_until_at: string | null;
  last_failure_kind: LoadbalanceFailureKind | null;
  last_success_at: string | null;
  last_success_response_headers_latency_ms: number | null;
  state: LoadbalanceCurrentStateValue;
  created_at: string;
  updated_at: string;
}

export interface LoadbalanceCurrentStateListResponse {
  items: LoadbalanceCurrentStateItem[];
}

export interface LoadbalanceCurrentStateResetResponse {
  connection_id: number;
  cleared: boolean;
  /** Full post-reset CurrentStateItem snapshot (null when no process state). */
  state: LoadbalanceCurrentStateItem | null;
}

// Global current state read model (SPEC §6).
export type CurrentStateCompletenessState = "ready" | "no_config" | "partial" | "unobserved";

export interface GlobalCurrentStateIdentity {
  id: number;
  label: string;
  configured: boolean;
}

export interface GlobalCurrentStateModelIdentity {
  model_config_id: number;
  id: string;
  label: string;
  configured: boolean;
}

export interface GlobalCurrentStateItem {
  model: GlobalCurrentStateModelIdentity;
  endpoint: GlobalCurrentStateIdentity;
  terminal_target: GlobalCurrentStateIdentity;
  observation_state: "observed" | "unobserved";
  state: LoadbalanceCurrentStateValue | null;
  available: boolean | null;
  cycle_retry_attempts: number | null;
  cumulative_retry_attempts: number | null;
  next_retry_at: string | null;
  last_retry_delay_ms: number | null;
  ban_mode: LoadbalanceBanMode | null;
  banned_until_at: string | null;
  last_failure_kind: LoadbalanceFailureKind | null;
  last_success_at: string | null;
  last_success_response_headers_latency_ms: number | null;
  in_flight_stream: number | null;
  in_flight_non_stream: number | null;
  qps_window_started_at: string | null;
  qps_window_request_count: number | null;
  created_at: string | null;
  updated_at: string | null;
}

export interface GlobalCurrentStateCompleteness {
  state: CurrentStateCompletenessState;
  complete: boolean;
  configured_target_count: number;
  observed_target_count: number;
  unobserved_target_count: number;
  observed_subset_counts: Record<string, number> | null;
}

export interface GlobalCurrentStateResponse {
  generated_at: string;
  scope: "process";
  instance_id: string;
  configuration_revision: string;
  completeness: GlobalCurrentStateCompleteness;
  items: GlobalCurrentStateItem[];
  has_more: boolean;
  next_cursor: string | null;
}

// Events timeline V1 (SPEC §7.2/§7.3).
export type EventSummaryEvidenceState = "complete" | "legacy_incomplete";

export interface EventSummaryV1Params {
  evidence_state: EventSummaryEvidenceState;
  failure_kind?: LoadbalanceFailureKind | null;
  admission_reason?: LoadbalanceAdmissionReason | null;
  cycle_retry_attempts?: number;
  cumulative_retry_attempts?: number;
  last_retry_delay_ms?: number;
  next_retry_at?: string | null;
  policy_cycle_retry_attempt_limit?: number | null;
  ban_mode?: LoadbalanceBanMode | null;
  policy_ban_cumulative_retry_attempt_threshold?: number | null;
  banned_until_at?: string | null;
  last_success_at?: string | null;
}

export interface EventSummaryV1 {
  version: 1;
  code:
    | "loadbalance.retry_scheduled"
    | "loadbalance.retry_exhausted"
    | "loadbalance.banned"
    | "loadbalance.unbanned"
    | "loadbalance.recovered"
    | "loadbalance.admission_rejected"
    | (string & {});
  params: EventSummaryV1Params;
}

export interface EventEntityProjection {
  id: number | null;
  label: string;
  configured: boolean | null;
  attribution: "identified" | "unattributed";
}

export interface EventModelProjection extends EventEntityProjection {
  model_config_id: number | null;
  model_id: string | null;
}

export interface EventTerminalTargetProjection extends EventEntityProjection {
  owner_model_config_id: number | null;
}

export interface EventRequestContextFilters {
  schema_version: 1;
  kind: "contextual_window";
  correlation: "not_exact";
  from_time: string;
  to_time: string;
  model_id: string | null;
  endpoint_id: number | null;
  terminal_target_id: number | null;
}

export interface LoadbalanceEventListItem {
  event_id: string;
  created_at: string;
  event_type: LoadbalanceEventType;
  summary: EventSummaryV1;
  failure_kind: LoadbalanceFailureKind | null;
  admission_reason: LoadbalanceAdmissionReason | null;
  model: EventModelProjection;
  endpoint: EventEntityProjection;
  terminal_target: EventTerminalTargetProjection;
  cycle_retry_attempts: number;
  cumulative_retry_attempts: number;
  next_retry_at: string | null;
  last_retry_delay_ms: number;
  ban_mode: LoadbalanceBanMode | null;
  policy_cycle_retry_attempt_limit: number | null;
  policy_ban_cumulative_retry_attempt_threshold: number | null;
  banned_until_at: string | null;
  last_success_at: string | null;
  request_context_filters: EventRequestContextFilters | null;
  request_context_unavailable_reason: string | null;
}

export interface EventCoverageGap {
  from_time: string;
  to_time: string;
  reason: string;
}

export interface EventCoverage {
  complete: boolean;
  gaps: EventCoverageGap[];
  retention_epoch?: string;
  retention_generation?: string;
  purge_state?: string;
  source_revision?: string;
}

export interface EventSourceStatus {
  delivery: string;
  transition_ledger_complete: boolean;
  dropped_event_count: number | null;
}

export interface LoadbalanceEventListResponse {
  generated_at: string;
  coverage: EventCoverage;
  source_status: EventSourceStatus;
  items: LoadbalanceEventListItem[];
  has_more: boolean;
  next_cursor: string | null;
}

export type LoadbalanceEventDetail = LoadbalanceEventListItem;

export interface EventsQueryContextResponse {
  query_context: string;
  requested_preset: string;
  event_bounds: { from_time: string | null; to_time: string | null };
  coverage: EventCoverage;
  source_status: EventSourceStatus;
  generated_at: string;
}

export interface LoadbalanceIncidentListResponse {
  active_bans: LoadbalanceCurrentStateItem[];
  recent_events: LoadbalanceEvent[];
  generated_at: string;
}

/** Legacy compact event shape used by incidents/Overview only. */
export interface LoadbalanceEvent {
  id: number;
  profile_id: number;
  connection_id: number;
  event_type: LoadbalanceEventType;
  failure_kind: LoadbalanceFailureKind | null;
  cycle_retry_attempts: number;
  cumulative_retry_attempts: number;
  next_retry_at: string | null;
  last_retry_delay_ms: number;
  model_id: string | null;
  endpoint_id: number | null;
  ban_mode: LoadbalanceBanMode | null;
  cycle_retry_attempt_limit: number | null;
  ban_cumulative_retry_attempt_threshold: number | null;
  banned_until_at: string | null;
  last_success_at: string | null;
  summary: { event: string; reason: string; operation: string; cooldown: string };
  created_at: string;
}

export interface LoadbalanceEventKeyset {
	created_at: string;
	id: number;
}
