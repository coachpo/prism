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
  quota_exceeded?: boolean;
  quota_reason?: string;
  quota_next_recover_at?: string;
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

export interface SidecarWatchdogPolicy {
  id: number;
  sidecar_id: number;
  enabled: boolean;
  failure_threshold: number;
  failure_window_seconds: number;
  fallback_cooldown_seconds: number;
  deprioritized_priority: number;
  manual_override_pause_seconds: number;
  created_at: string;
  updated_at: string;
}

export interface SidecarWatchdogPolicyUpdate {
  enabled?: boolean;
  failure_threshold?: number;
  failure_window_seconds?: number;
  fallback_cooldown_seconds?: number;
  deprioritized_priority?: number;
  manual_override_pause_seconds?: number;
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
  target_priority?: number;
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
