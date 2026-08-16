export interface AuthStatus {
  state?: "enabled" | "disabled" | "transition_fail_closed";
  transition_state?: "disabling_enforced" | "enabling_fail_closed" | "rollback_required" | null;
  effective_generation?: string;
  login_available?: boolean;
  retry_after_seconds?: number | null;
  /** @deprecated Legacy status shape. */
  auth_enabled?: boolean;
}

export type AuthAccessState =
  | "enabled"
  | "disabled"
  | "enabling_fail_closed"
  | "disabling_enforced"
  | "account_transition_enabled"
  | "account_transition_disabled"
  | "rollback_required";

export interface ProxyKeyReadiness {
  state: "ready" | "unavailable";
  readiness_generation?: string;
  counted_at?: string;
  active?: string;
  expired?: string;
  disabled?: string;
  activation_guard?: {
    safety_horizon_seconds: number;
    safe_active: string;
  };
  reason_code?: string;
  retry_after_seconds?: number | null;
  last_ready_generation?: string | null;
}

export interface AuthOperatorAccount {
  state: "unconfigured" | "ready" | "repair_required";
  username: string | null;
  has_password: boolean;
  session_version: string;
  updated_at: string | null;
}

// Tagged PublicAuthStatus union (Auth/Session/Landing SPEC §4.1): the only
// legal branches are enabled+null|disabling_enforced, disabled+null and
// transition_fail_closed+enabling_fail_closed|rollback_required. Every
// branch carries the canonical positive decimal effective_generation.
export type PublicAuthStatus =
  | { state: "enabled"; transition_state: null | "disabling_enforced"; login_available: true; effective_generation: string; retry_after_seconds: number | null }
  | { state: "disabled"; transition_state: null; login_available: false; effective_generation: string; retry_after_seconds: number | null }
  | { state: "transition_fail_closed"; transition_state: "enabling_fail_closed" | "rollback_required"; login_available: boolean; effective_generation: string; retry_after_seconds: number | null };

export interface AuthSettings {
  revision?: string;
  auth_mode?: {
    desired: "enabled" | "disabled";
    effective: "enabled" | "disabled";
    access_state: AuthAccessState;
    desired_generation: string;
    effective_generation: string;
  };
  operator_account?: {
    effective: AuthOperatorAccount;
    desired: AuthOperatorAccount | null;
  };
  transition?: {
    operation_id: string;
    kind: "enable" | "disable" | "account_update" | "account_and_mode_update";
    state: "staged" | "publishing" | "retrying" | "rollback_required";
    retryable: boolean;
    last_safe_error: { code: string; retry_after_seconds: number | null } | null;
  } | null;
  proxy_key_readiness?: ProxyKeyReadiness;
  attribution_mode_when_disabled?: "permissive";
  updated_at?: string;

  /** @deprecated Runtime compatibility for consumers that have not migrated their view yet. */
  auth_enabled?: boolean;
  /** @deprecated Runtime compatibility for consumers that have not migrated their view yet. */
  username?: string | null;
  /** @deprecated Runtime compatibility for consumers that have not migrated their view yet. */
  has_password?: boolean;
  /** @deprecated Runtime compatibility for consumers that have not migrated their view yet. */
  proxy_key_limit?: number;
}

export interface AuthSettingsUpdate {
	operation_id: string;
  expected_revision: string;
  expected_proxy_key_readiness_generation?: string | null;
  desired_auth_enabled: boolean;
  account_change:
    | { kind: "preserve" }
    | { kind: "update"; username: string; new_password: string | null };
  acknowledgements: {
    enable_without_active_proxy_keys?: true;
    disable_to_permissive_access?: true;
    invalidate_operator_sessions?: true;
  };
}

export interface AuthSettingsMutationResponse {
  operation_id: string;
  replayed: boolean;
  effect_state: "effective" | "transitioning" | "retrying" | "rollback_required" | "rolled_back" | "failed";
  settings: AuthSettings;
  session_action: "none" | "clear_and_login" | "clear_and_continue";
  operation_status_url: string;
}

export interface AuthOperationResult {
  operation_id: string;
  state: "transitioning" | "effective" | "retrying" | "rollback_required" | "rolled_back" | "failed";
  desired_generation: string;
  effective_generation: string;
  retryable: boolean;
  safe_error: { code: string; retry_after_seconds: number | null } | null;
  readiness_conflict: {
    code: "auth_readiness_changed" | "auth_acknowledgement_required";
    current_proxy_key_readiness: ProxyKeyReadiness;
    required_acknowledgements: string[];
    new_operation_id_required: true;
  } | null;
  session_action: "none" | "clear_and_login" | "clear_and_continue";
	settings: AuthSettings;
}

export type LoginSessionDuration = "session" | "7_days" | "30_days";

export interface LoginRequest {
  username: string;
  password: string;
  session_duration?: LoginSessionDuration;
}

// Strict AuthenticatedSession payload: authenticated login/session/refresh
// responses carry the server-authored canonical subject_key; anonymous and
// disabled payloads never include it.
export interface AuthenticatedSession {
  authenticated: true;
  auth_enabled: true;
  username: string | null;
  subject_key: string;
}

export interface AnonymousSession {
  authenticated: false;
  auth_enabled: boolean;
  username: null;
  subject_key?: never;
}

export type SessionResponse = AuthenticatedSession | AnonymousSession;

// auth_login_locked details (SPEC §7.1): authoritative UTC retry instant plus
// the same-source delta seconds.
export interface AuthLoginLockedDetails {
  retry_at: string;
  retry_after_seconds: number;
}

// auth_transition_* details (SPEC §7.1).
export interface AuthTransitionProblemDetails {
  transition_state: "enabling_fail_closed" | "rollback_required";
  effective_generation: string;
  recovery: "confirm_public_status";
  retry_after_seconds: number | null;
}

// Bounded public auth-operation status (Settings SPEC §8.2): fixed fields,
// fixed enums, never the operation id itself.
export interface PublicAuthOperationStatus {
  state: "transitioning" | "retrying" | "rollback_required" | "effective" | "rolled_back" | "failed";
  access_state:
    | "enabled"
    | "disabled"
    | "enabling_fail_closed"
    | "disabling_enforced"
    | "account_transition_enabled"
    | "account_transition_disabled"
    | "rollback_required";
  effective_generation: string;
  retry_after_seconds: number | null;
}

export interface ProxyApiKey {
  id: number;
  name: string;
  key_prefix: string;
  key_preview: string;
  is_active: boolean;
  expires_at: string | null;
  last_used_at: string | null;
  last_used_ip: string | null;
  notes: string | null;
  rotated_at: string | null;
  rotation_count: number;
  created_at: string;
  updated_at: string;
}

export interface ProxyKeyCapacity {
  limit: number;
  used: number;
  remaining: number;
  counted_at: string;
}

export interface ProxyApiKeyListResponse {
  items: ProxyApiKey[];
  capacity: ProxyKeyCapacity;
}

export interface ProxyApiKeyFilterOption {
  proxy_api_key_id: number;
  proxy_api_key_name: string;
  key_preview: string | null;
  configured: boolean;
}

export interface ProxyApiKeyFilterOptionsResponse {
  items: ProxyApiKeyFilterOption[];
  selected: ProxyApiKeyFilterOption | null;
  next_cursor: string | null;
  resolved_from_time: string | null;
  resolved_to_time: string | null;
}

export interface ProxyApiKeyCreateResponse {
  key: string;
  item: ProxyApiKey;
  capacity: ProxyKeyCapacity;
}

export interface ProxyApiKeyRotateResponse {
  key: string;
  item: ProxyApiKey;
  capacity: ProxyKeyCapacity;
}

export interface ProxyApiKeyUpdateResponse {
  item: ProxyApiKey;
  capacity: ProxyKeyCapacity;
}

export interface ProxyApiKeyDeleteResponse {
  deleted_id: number;
  capacity: ProxyKeyCapacity;
}

/**
 * Presence-aware expiry update: omitted preserves the current value, explicit
 * null clears it, and an RFC3339 string sets a new future instant. The UI
 * never relies on undefined/null serialization accidents.
 */
export interface ProxyApiKeyUpdate {
  name: string;
  notes: string | null;
  is_active: boolean;
  expires_at?: string | null;
}
