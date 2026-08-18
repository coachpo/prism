// The audit API is the Requests/Audit v2 projection: scoped statuses and
// durations by row kind, bytea-captured bodies exposed as base64, and per-body
// capture metadata. Keep the field names aligned with
// backend/internal/domain/audit/service.go.
export interface AuditLogListItem {
  id: number;
  request_log_id: string | null;
  request_log_created_at: string | null;
  ingress_request_id: string | null;
  request_log_missing: boolean;
  profile_id: number;
  model_id: string;
  endpoint_id: number | null;
  connection_id: number | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  request_method: string;
  request_url: string;
  request_headers: string | null;
  request_body_preview: string | null;
  request_body_preview_truncated: boolean;
  request_body_preview_unavailable_reason: string | null;
  request_body_stored: boolean;
  request_body_encoding: string | null;
  request_body_capture_status: string;
  request_body_capture_provenance: string;
  request_body_capture_end_state: string | null;
  request_body_truncated: boolean;
  request_body_bytes_observed: number | null;
  request_body_bytes_stored: number | null;
  response_body_stored: boolean;
  row_kind: string;
  attempt_number: number | null;
  attempt_duration_ms: number | null;
  legacy_duration_ms: number | null;
  upstream_status_code: number | null;
  gateway_status_code: number | null;
  legacy_status_code: number | null;
  request_url_truncated: boolean;
  endpoint_base_url_truncated: boolean;
  is_stream: boolean;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
  created_at: string;
}

export interface AuditLogDetail {
  id: number;
  request_log_id: string | null;
  request_log_created_at: string | null;
  ingress_request_id: string | null;
  request_log_missing: boolean;
  profile_id: number;
  model_id: string;
  endpoint_id: number | null;
  connection_id: number | null;
  endpoint_base_url: string | null;
  endpoint_description: string | null;
  request_method: string;
  request_url: string;
  request_headers: string | null;
  request_body_base64: string | null;
  request_body_stored: boolean;
  request_body_encoding: string | null;
  request_body_capture_status: string;
  request_body_capture_provenance: string;
  request_body_capture_end_state: string | null;
  request_body_truncated: boolean;
  request_body_bytes_observed: number | null;
  request_body_bytes_stored: number | null;
  response_headers: string | null;
  response_body_base64: string | null;
  response_body_stored: boolean;
  response_body_encoding: string | null;
  response_body_capture_status: string;
  response_body_capture_provenance: string;
  response_body_capture_end_state: string | null;
  response_body_truncated: boolean;
  response_body_bytes_observed: number | null;
  response_body_bytes_stored: number | null;
  row_kind: string;
  attempt_number: number | null;
  attempt_duration_ms: number | null;
  legacy_duration_ms: number | null;
  upstream_status_code: number | null;
  gateway_status_code: number | null;
  legacy_status_code: number | null;
  request_url_truncated: boolean;
  endpoint_base_url_truncated: boolean;
  is_stream: boolean;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
  created_at: string;
}

export interface AuditLogListWindow {
  from: string;
  to: string;
}


export interface QueryCoverageGap {
  from_time: string;
  to_time: string | null;
  reason: string;
}

export interface QueryCoverage {
  requested_from_time: string;
  requested_to_time: string;
  effective_from_time: string;
  effective_to_time: string;
  retention_from_time?: string | null;
  complete: boolean;
  gaps: QueryCoverageGap[];
  precision?: { row_count: number } | null;
  state: "known" | "legacy_unknown";
  source_revision: string;
  retention_epoch?: string;
  retention_generation?: string;
  purge_state?: string;
}

export interface AuditLogListResponse {
  items: AuditLogListItem[];
  next_cursor: string | null;
  has_more: boolean;
  window: AuditLogListWindow;
  limit: number;
  sort: "desc";
  anchor_item?: AuditLogListItem | null;
  coverage: QueryCoverage;
}

export interface AuditLogParams {
  request_log_id?: string;
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
