export type LoadbalanceEventType =
  | "retry_scheduled"
  | "retry_exhausted"
  | "banned"
  | "unbanned"
  | "recovered"
  | "admission_rejected";

export type LegacyLoadbalanceStrategyType = "single" | "fill-first" | "round-robin";
export type LoadbalanceBanMode = "off" | "temporary" | "until_reset";

export type LoadbalanceFailureKind =
  | "transient_http"
  | "connect_error"
  | "timeout";

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
  attached_model_count: number;
  created_at: string;
  updated_at: string;
}

export type LoadbalanceStrategyCreate = {
  name: string;
} & LoadbalanceBanPolicyFields;

export type LoadbalanceStrategyUpdate = LoadbalanceStrategyCreate;

export interface LoadbalanceStrategyDefaultsResponse {
  items: LoadbalanceStrategy[];
  created_count: number;
  created_names: string[];
  existing_names: string[];
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
  live_p95_latency_ms: number | null;
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
}

export interface LoadbalanceEventSummary {
  event: string;
  reason: string;
  operation: string;
  cooldown: string;
}

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
  vendor_id: number | null;
  ban_mode: LoadbalanceBanMode | null;
  cycle_retry_attempt_limit: number | null;
  ban_cumulative_retry_attempt_threshold: number | null;
  banned_until_at: string | null;
  last_success_at: string | null;
  summary: LoadbalanceEventSummary;
  created_at: string;
}

export type LoadbalanceEventDetail = LoadbalanceEvent;

export interface LoadbalanceEventListResponse {
  items: LoadbalanceEvent[];
  total: number;
  limit: number;
  offset: number;
}
