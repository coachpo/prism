import type { ApiFamily } from "./vendor";
import type { LogRetentionTable } from "./retention-jobs";

export interface AuditAPIFamilySetting {
  api_family: ApiFamily;
  audit_enabled: boolean;
  audit_capture_bodies: boolean;
}

export interface AuditAPIFamilySettingsResponse {
  profile_id: number;
  settings: AuditAPIFamilySetting[];
}

export type AuditPolicyMode = "disabled" | "metadata_only" | "body_capture";

export interface AuditPolicyRow {
  family: ApiFamily;
  mode: AuditPolicyMode;
}

export interface AuditSettingsResponse {
  revision: string;
  updated_at: string;
  policies: AuditPolicyRow[];
  fixed_capture_limits: {
    per_request_body_bytes: number;
    aggregate_request_body_bytes: number;
    final_response_body_bytes: number;
    aggregate_raw_body_bytes_per_ingress: number;
  };
}

export interface AuditSettingsUpdate {
  operation_id: string;
  expected_revision: string;
  policies: AuditPolicyRow[];
}

export interface AuditStorageSummary {
  source_revision: string;
  storage_fact_evidence:
    | { state: "bound"; generation: string }
    | { state: "unavailable"; reason_code: string };
  generated_at: string;
  retention_source: Record<string, unknown>;
  audit_protection: Record<string, unknown>;
  retained_rows: string | null;
  logical_header_bytes: string | null;
  logical_body_bytes: string | null;
  last_7d_logical_bytes_added: string | null;
  sampled_days: number;
  daily_average_logical_bytes: string | null;
  precision: "exact" | "estimated" | "unavailable";
  freshness: "fresh" | "partial" | "stale";
}

export type RetentionPolicyDays = number | null;

export interface RetentionSettingsPolicies {
  request_logs_retention_days: RetentionPolicyDays;
  statistics_retention_days: RetentionPolicyDays;
  audit_logs_retention_days: RetentionPolicyDays;
  loadbalance_events_retention_days: RetentionPolicyDays;
}

export interface RetentionPolicyReadValue {
  state: "valid" | "repair_required";
  value?: number | null;
  raw_integer?: string;
  issue?: string;
}

export interface RetentionSettingsRepairPolicies {
  request_logs_retention_days: RetentionPolicyReadValue;
  statistics_retention_days: RetentionPolicyReadValue;
  audit_logs_retention_days: RetentionPolicyReadValue;
  loadbalance_events_retention_days: RetentionPolicyReadValue;
}

export type RetentionSettingsResponsePolicies = RetentionSettingsPolicies | RetentionSettingsRepairPolicies;

export interface RetentionRecommendation {
  id: "balanced-v1";
  policies: RetentionSettingsPolicies;
  rationale_codes: string[];
}

export interface ObserveTokenProtection {
  kind: "observe_query_token";
  token_ttl_seconds: number;
  extra_grace_seconds: number;
  physical_reclaim_not_before: string | null;
  source_revision: string;
  retention_epoch: string;
  retention_generation: string;
  purge_state: string;
}

export interface AuditFenceProtection {
  kind: "audit_retention_fence";
  contract_version: 3;
  retention_source: Record<string, unknown> | null;
  audit_protection: AuditFenceMaterializerProjection | null;
  fixed_token_ttl_seconds: null;
  fixed_extra_grace_seconds: null;
  physical_reclaim_not_before: null;
}

/** Exact Requests/Audit owner projection; Settings must not flatten it. */
export interface AuditFenceMaterializerProjection {
  contract_version: 1;
  fence_generation: string;
  reader_fence_state: "clear" | "waiting_for_readers";
  materializer_generation: string;
  materializer_state: "ready" | "draining" | "blocked";
  generated_at: string;
}

export interface RetentionCoverageSummary {
  from_time: string | null;
  to_time: string | null;
  source: string;
  precision: string;
  gaps: Array<Record<string, unknown>>;
  complete: boolean;
  freshness: "fresh" | "partial" | "stale" | string;
  source_revision: string;
  retention_epoch: string;
  retention_generation: string;
  purge_state: string;
}

export interface RetentionSettingsResponse {
  state: "ready" | "repair_required";
  scope: "instance";
  revision: string;
  updated_at: string;
  server_now: string;
  policies: RetentionSettingsResponsePolicies;
  recommendations: RetentionRecommendation[];
  policy_generation: Record<LogRetentionTable, string>;
  configured_logical_cutoffs: Record<LogRetentionTable, string | null>;
  published_retention_floors: Record<LogRetentionTable, string | null>;
  retention_source_revision: Partial<Record<LogRetentionTable, string>>;
  actual_coverage: Partial<Record<LogRetentionTable, RetentionCoverageSummary>>;
  protection: Partial<Record<LogRetentionTable, ObserveTokenProtection | AuditFenceProtection>>;
  owner_drift_inventory?: {
    inventory_generation: string;
    state: "action_required" | "resolved";
    current_heads: Array<Record<string, unknown>>;
    generated_at: string;
  } | null;
  repair_preflight_url?: string;

  /** @deprecated Read only for callers not yet migrated to `policies`. */
  request_logs_retention_days?: number | null;
  /** @deprecated Read only for callers not yet migrated to `policies`. */
  statistics_retention_days?: number | null;
  /** @deprecated Read only for callers not yet migrated to `policies`. */
  audit_logs_retention_days?: number | null;
  /** @deprecated Read only for callers not yet migrated to `policies`. */
  loadbalance_events_retention_days?: number | null;
}

export interface RetentionSettingsUpdate {
  operation_id: string;
  expected_revision: string;
  policies: RetentionSettingsPolicies;
  preflight_token?: string;
  confirmation?: { keyword: string };
}

export interface HeaderBlocklistRule {
  id: number;
  name: string;
  match_type: "exact" | "prefix";
  pattern: string;
  enabled: boolean;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

export interface HeaderBlocklistRuleCreate {
  name: string;
  match_type: "exact" | "prefix";
  pattern: string;
  enabled?: boolean;
}

export interface HeaderBlocklistRuleUpdate {
  name?: string;
  match_type?: "exact" | "prefix";
  pattern?: string;
  enabled?: boolean;
}

export interface UserAgentClientRule {
  id: number;
  name: string;
  pattern: string;
  enabled: boolean;
  is_system: boolean;
  created_at: string;
  updated_at: string;
}

export interface UserAgentClientRuleCreate {
  name: string;
  pattern: string;
  enabled?: boolean;
}

export interface UserAgentClientRuleUpdate {
  name?: string;
  pattern?: string;
  enabled?: boolean;
}
