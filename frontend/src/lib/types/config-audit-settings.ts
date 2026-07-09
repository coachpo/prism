import type { ApiFamily } from "./vendor";

export interface AuditAPIFamilySetting {
  api_family: ApiFamily;
  audit_enabled: boolean;
  audit_capture_bodies: boolean;
}

export interface AuditAPIFamilySettingsResponse {
  profile_id: number;
  settings: AuditAPIFamilySetting[];
}

export interface AuditAPIFamilySettingsUpdate {
  settings: AuditAPIFamilySetting[];
}

export interface AuditLogListItem {
  id: number;
  request_log_id: number | null;
  profile_id: number;
  vendor_id?: number;
  model_id: string;
  endpoint_id: number | null;
  connection_id: number | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  request_method: string;
  request_url: string;
  request_headers: string;
  request_body_preview: string | null;
  request_body_stored: boolean;
  response_status: number;
  response_body_stored: boolean;
  is_stream: boolean;
  duration_ms: number;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
  created_at: string;
}

export interface AuditLogDetail {
  id: number;
  request_log_id: number | null;
  profile_id: number;
  vendor_id?: number;
  model_id: string;
  endpoint_id: number | null;
  connection_id: number | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  request_method: string;
  request_url: string;
  request_headers: string;
  request_body: string | null;
  request_body_stored: boolean;
  response_status: number;
  response_headers: string | null;
  response_body: string | null;
  response_body_stored: boolean;
  is_stream: boolean;
  duration_ms: number;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
  created_at: string;
}

export interface AuditLogListWindow {
  from: string;
  to: string;
}

export interface AuditLogListResponse {
  items: AuditLogListItem[];
  next_cursor: string | null;
  has_more: boolean;
  window: AuditLogListWindow;
  limit: number;
  sort: "desc";
}

export interface AuditLogParams {
  request_log_id?: number;
  vendor_id?: number;
  model_id?: string;
  status_code?: number;
  endpoint_id?: number;
  connection_id?: number;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
  sort?: "desc";
}

export type LogRetentionTable =
  | "request_logs"
  | "audit_logs"
  | "usage_request_events"
  | "loadbalance_events";

export interface LogRetentionJobScope {
  before?: string | null;
  table: LogRetentionTable;
  cutoff?: string | null;
  delete_all?: boolean;
}

export interface LogRetentionJobRequest {
  table: LogRetentionTable;
  cutoff?: string | null;
  delete_all?: boolean;
  reason: string;
}

export interface LogRetentionJobResponse {
  job_id: string;
  state: string;
  status_url: string;
  scope: LogRetentionJobScope;
}

export interface EndpointFxMapping {
  model_id: string;
  endpoint_id: number;
  fx_rate: string;
}

export interface CostingSettingsResponse {
  report_currency_code: string;
  report_currency_symbol: string;
  endpoint_fx_mappings: EndpointFxMapping[];
  timezone_preference?: string | null;
}

export interface TimezonePreferenceResponse {
  timezone_preference?: string | null;
}

export interface CostingSettingsUpdate {
  report_currency_code: string;
  report_currency_symbol: string;
  endpoint_fx_mappings: EndpointFxMapping[];
  timezone_preference?: string | null;
}

export interface TimezonePreferenceUpdate {
  timezone_preference?: string | null;
}

export interface RetentionSettingsResponse {
  request_logs_retention_days: number | null;
  statistics_retention_days: number | null;
  audit_logs_retention_days: number | null;
  loadbalance_events_retention_days: number | null;
}

export interface RetentionSettingsUpdate {
  request_logs_retention_days?: number | null;
  statistics_retention_days?: number | null;
  audit_logs_retention_days?: number | null;
  loadbalance_events_retention_days?: number | null;
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
