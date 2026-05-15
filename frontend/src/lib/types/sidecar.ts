export type SidecarManagementAuthState = "unknown" | "valid" | "invalid_management_auth";

export type SidecarProviderKey =
  | "gemini"
  | "claude"
  | "codex"
  | "vertex"
  | "openai-compatibility";

export interface SidecarCredentialState {
  management_password_configured: boolean;
}

export interface SidecarPauseMetadata {
  reason: string;
  paused_until?: string;
}

export interface SidecarInstance {
  id: number;
  name: string;
  base_url: string;
  base_url_canonical: string;
  enabled: boolean;
  environment_label?: string;
  allow_private_network: boolean;
  allow_insecure_http: boolean;
  skip_tls_verify: boolean;
  sync_interval_seconds: number;
  request_timeout_seconds: number;
  credential_state: SidecarCredentialState;
  management_auth_state: SidecarManagementAuthState;
  pause_metadata?: SidecarPauseMetadata;
  last_sync_at?: string;
  last_successful_sync_at?: string;
  snapshot_stale_after?: string;
  last_sync_error?: string;
  created_at: string;
  updated_at: string;
}

export interface SidecarListResponse {
  items: SidecarInstance[];
}

export interface SidecarAuthSnapshot {
  id: number;
  sidecar_id: number;
  auth_id: string;
  auth_index?: string;
  name: string;
  provider?: string;
  label?: string;
  status?: string;
  status_message?: string;
  disabled?: boolean;
  unavailable?: boolean;
  priority?: number;
  next_retry_after?: string;
  success_count?: number;
  failed_count?: number;
  recent_requests?: unknown;
  model_states?: unknown;
  observed_at: string;
  snapshot?: unknown;
}

export interface SidecarAuthSnapshotListResponse {
  items: SidecarAuthSnapshot[];
}

export interface SidecarProviderSnapshot {
  id: number;
  sidecar_id: number;
  provider_key: SidecarProviderKey | string;
  provider_item_key: string;
  name?: string;
  label?: string;
  status?: string;
  disabled?: boolean;
  observed_at: string;
  snapshot?: unknown;
}

export interface SidecarProviderSnapshotListResponse {
  items: SidecarProviderSnapshot[];
}

export interface SidecarWatchdogPolicyRevision {
  id: number;
  policy_id: number;
  sidecar_id: number;
  enabled: boolean;
  watchdog_sweep_interval_seconds: number;
  failure_threshold: number;
  failure_window_seconds: number;
  fallback_cooldown_seconds: number;
  using_priority: number;
  quota_exceeded_priority: number;
  working_priority: number;
  empty_quota_priority: number;
  initial_priority: number;
  error_priority: number;
  manual_override_pause_seconds: number;
  probe_concurrency: number;
  probe_timeout_seconds: number;
  probe_batch_cooldown_seconds: number;
  probe_jitter_min_ms: number;
  probe_jitter_max_ms: number;
  cooldown_jitter_percent: number;
  quota_inventory_enabled: boolean;
  initial_scan_enabled: boolean;
  rolling_refresh_enabled: boolean;
  rolling_refresh_after_seconds: number;
  created_at: string;
}

export interface SidecarWatchdogActiveSweep {
  sweep_id: string;
  status: string;
  policy_revision_id: number;
  started_at: string;
  next_batch_after?: string;
  restart_requested_at?: string;
  next_item_index: number;
  total_items: number;
}

export interface SidecarWatchdogPolicy extends Omit<SidecarWatchdogPolicyRevision, "id" | "policy_id" | "created_at"> {
  id: number;
  active_revision_id?: number;
  pending_revision_id?: number;
  has_pending_changes: boolean;
  active_revision?: SidecarWatchdogPolicyRevision;
  pending_revision?: SidecarWatchdogPolicyRevision;
  active_sweep?: SidecarWatchdogActiveSweep;
  created_at: string;
  updated_at: string;
}

export interface SidecarWatchdogPolicyUpdate {
  expected_revision_id: number;
  enabled?: boolean;
  watchdog_sweep_interval_seconds?: number;
  failure_threshold?: number;
  failure_window_seconds?: number;
  fallback_cooldown_seconds?: number;
  using_priority?: number;
  quota_exceeded_priority?: number;
  working_priority?: number;
  empty_quota_priority?: number;
  initial_priority?: number;
  error_priority?: number;
  manual_override_pause_seconds?: number;
  probe_concurrency?: number;
  probe_timeout_seconds?: number;
  probe_batch_cooldown_seconds?: number;
  probe_jitter_min_ms?: number;
  probe_jitter_max_ms?: number;
  cooldown_jitter_percent?: number;
  quota_inventory_enabled?: boolean;
  initial_scan_enabled?: boolean;
  rolling_refresh_enabled?: boolean;
  rolling_refresh_after_seconds?: number;
}

export interface SidecarWatchdogPolicyApplyInput {
  target_revision_id: number;
  expected_revision_id: number;
}

export type SidecarQuotaBand = "using" | "quota_exceeded" | "error";
export type SidecarPriorityState = "working" | "empty-quota" | "initial" | "error" | string;

export interface SidecarAuthQuotaState {
  sidecar_id: number;
  auth_id: string;
  auth_name?: string;
  provider?: string;
  auth_index_present: boolean;
  disabled: boolean;
  current_priority?: number;
  priority_state: SidecarPriorityState;
  quota_band: SidecarQuotaBand;
  probe_status?: string;
  reason_code?: string;
  quota_reset_at?: string;
  blocking_window?: string;
  last_snapshot_at?: string;
  last_probed_at?: string;
  last_error_code?: string;
  active_hold: boolean;
}

export interface SidecarAuthQuotaStateListResponse {
  items: SidecarAuthQuotaState[];
}

export type SidecarQuotaScanStatus = "queued" | "running" | "completed" | "cancelled" | "failed" | string;
export type SidecarQuotaScanType = "initial" | "manual" | "scheduled" | string;

export interface SidecarQuotaScanRun {
  id: number;
  sidecar_id: number;
  scan_type: SidecarQuotaScanType;
  status: SidecarQuotaScanStatus;
  requested_by?: string;
  planned_count: number;
  attempted_count: number;
  using_count: number;
  quota_exceeded_count: number;
  error_count: number;
  skipped_count: number;
  cancel_requested_at?: string;
  started_at?: string;
  completed_at?: string;
  last_error_code?: string;
  created_at: string;
  updated_at: string;
}

export interface SidecarQuotaScanRunListResponse {
  items: SidecarQuotaScanRun[];
}

export interface SidecarQuotaScanCreateInput {
  requested_by?: string;
  replace_active?: boolean;
}

export interface SidecarTestConnectionResponse {
  state: "succeeded";
  management_auth_state: SidecarManagementAuthState;
  status_code: number;
}

export interface SidecarSyncStatus {
  sidecar_id: number;
  enabled: boolean;
  sync_interval_seconds: number;
  management_auth_state: SidecarManagementAuthState;
  last_sync_at?: string;
  last_successful_sync_at?: string;
  snapshot_stale_after?: string;
  last_sync_error?: string;
  auth_failure_pause_until?: string;
  stale: boolean;
  due: boolean;
  paused: boolean;
}

export interface SidecarSyncResponse {
  state: string;
  sidecar: SidecarInstance;
  sync_status: SidecarSyncStatus;
  auth_snapshot_count: number;
  provider_snapshot_count: number;
  error_code?: string;
  error_detail?: string;
}

export interface SidecarActionHistoryItem {
  id: number;
  sidecar_id: number;
  auth_snapshot_id?: number;
  auth_id?: string;
  auth_index?: string;
  provider?: string;
  hold_id?: number;
  action_type: string;
  status: string;
  reason?: string;
  previous_priority?: number;
  previous_priority_state?: SidecarPriorityState;
  target_priority?: number;
  target_priority_state?: SidecarPriorityState;
  priority_state?: SidecarPriorityState;
  mutation_outcome?: string;
  hold_until?: string;
  error_message?: string;
  created_at: string;
  updated_at: string;
  completed_at?: string;
}

export interface SidecarAuthMutationResponse {
  state: string;
  snapshot?: SidecarAuthSnapshot;
  sync_error?: string;
}

export interface SidecarActionHistoryListResponse {
  items: SidecarActionHistoryItem[];
}
