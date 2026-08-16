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
  request_body_binary: boolean;
  request_body_bytes_count: number;
  response_status: number;
  response_headers: string | null;
  response_body: string | null;
  response_body_stored: boolean;
  response_body_binary: boolean;
  response_body_bytes_count: number;
  is_stream: boolean;
  duration_ms: number;
  audit_enabled_at_request: boolean;
  audit_capture_bodies_at_request: boolean;
  ingress_audit_bytes_observed: number;
  ingress_audit_bytes_stored: number;
  ingress_audit_bytes_truncated: number;
  request_capture_limit_reason: string;
  response_capture_limit_reason: string;
  request_header_bytes_observed: number;
  request_header_bytes_stored: number;
  request_header_bytes_truncated: number;
  request_headers_limit_reason: string;
  response_header_bytes_observed: number;
  response_header_bytes_stored: number;
  response_header_bytes_truncated: number;
  response_headers_limit_reason: string;
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

export type RetentionCountAccuracy = "exact" | "estimated" | "unavailable";

export type RetentionJobProtection =
  | { kind: "none" }
  | { kind: "legacy_unknown" }
  | { kind: "observe_query_token"; deadline: string }
  | {
      kind: "audit_retention_fence";
      audit_retention_epoch: string;
      published_floor: string | null;
      reader_fence_state: "clear" | "waiting_for_readers";
      materializer_state: "ready" | "draining" | "blocked";
    };

export interface RetentionImpactCount {
  value: string | null;
  accuracy: RetentionCountAccuracy;
  method: string;
}

export interface RetentionImpactBytes {
  value: string | null;
  accuracy: RetentionCountAccuracy;
  basis: string;
}

export interface RetentionAffectedDomain {
  dataset: LogRetentionTable;
  owner_snapshot: Record<string, unknown>;
  impact: {
    change: Record<string, unknown>;
    resolved_cutoff: string | null;
    logical_coverage_after: {
      from_time: string | null;
      to_time: string;
      gaps: Array<Record<string, unknown>>;
      accuracy: "exact" | "estimated" | "unavailable";
      basis: string;
    };
    physical_reclaim_not_before: string | null;
    matched_rows: RetentionImpactCount;
    retained_rows: RetentionImpactCount;
    matched_logical_bytes: RetentionImpactBytes;
    reclaimable_physical_bytes: RetentionImpactBytes;
    matched_fraction: string | null;
    whole_partitions: {
      count: string;
      names_preview: string[];
      names_total_count: string;
      truncated: boolean;
    };
    boundary_partitions: Array<Record<string, unknown>>;
    storage_layers: Array<Record<string, unknown>>;
    consumers: string[];
    non_cascades: Array<{
      dataset: LogRetentionTable;
      effect: "preserved";
      retained_rows: RetentionImpactCount;
    }>;
    semantic_facts_complete: boolean;
    warnings: string[];
  };
}

export interface RetentionPreflightResponse {
  preflight_id: string;
  preflight_token: string;
  kind: "policy_change" | "manual_cleanup";
  operation_id: string;
  preflight_attempt_id: string;
  scope: "instance";
  request_hash: string;
  previewed_at: string;
  generated_at: string;
  expires_at: string;
  settings_revision: string;
  affected_domains: RetentionAffectedDomain[];
  confirmation_keyword: string;
}

export interface PolicyChangePreflightRequest {
  kind: "policy_change";
  operation_id: string;
  preflight_attempt_id: string;
  expected_settings_revision: string;
  policies: RetentionSettingsPolicies;
}

export type ManualCleanupSelection =
  | { mode: "keep_days"; days: 1 | 7 | 30 | 90 }
  | { mode: "cutoff"; cutoff: string }
  | { mode: "delete_all" };

export interface ManualCleanupPreflightRequest {
  kind: "manual_cleanup";
  operation_id: string;
  preflight_attempt_id: string;
  dataset: LogRetentionTable;
  selection: ManualCleanupSelection;
}

export interface CreateManualRetentionJobRequest {
  operation_id: string;
  preflight_token: string;
  confirmation: { keyword: string };
}

export interface RetentionJobProgress {
  accounting_provenance: "v2_exact" | "legacy_boundary_only";
  stage: string;
  visibility_state: string;
  purge_state: string;
  protection: RetentionJobProtection | null;
  rows_matched_estimate: string | null;
  rows_matched_accuracy: RetentionCountAccuracy;
  boundary_rows_deleted: string;
  boundary_batches_completed: string;
  dropped_partition_count: string | null;
  dropped_partition_count_accuracy: RetentionCountAccuracy;
  dropped_partition_names_preview: string[];
  dropped_partition_names_total_count: string | null;
  dropped_partition_names_truncated: boolean;
  dropped_rows_estimate: string | null;
  dropped_rows_accuracy: "estimated" | "unavailable";
  staged_items_tombstoned: string | null;
  sensitive_artifact_bytes_deleted: string | null;
  last_checkpoint_at: string | null;
}

export interface GlobalRetentionJobSummary {
  id: string;
  contract_version: 1 | 2;
  type: "log_retention";
  job_scope: "instance";
  origin: "automatic" | "manual";
  legacy_origin_provenance: string | null;
  legacy_execution_provenance: string | null;
  dataset: LogRetentionTable;
  state: "queued" | "running" | "cancel_requested" | "cancelled" | "succeeded" | "failed" | "superseded";
  terminal_disposition: string | null;
  legacy_original_state: string | null;
  mode: "cutoff" | "delete_all";
  cutoff: string | null;
  purge_to_time: string | null;
  policy_revision: string | null;
  preflight_id: string | null;
  operation_id: string | null;
  requested_at: string;
  started_at: string | null;
  finished_at: string | null;
  last_heartbeat_at: string | null;
  attempt_count: number;
  cancel_allowed: boolean;
  progress: RetentionJobProgress;
  error: { code: string; message: string } | null;
}

export interface GlobalRetentionJobList {
  items: GlobalRetentionJobSummary[];
  has_more: boolean;
  next_cursor: string | null;
  generated_at: string;
}

export interface RetentionJobCheckpoint {
  sequence: string;
  recorded_at: string;
  stage: string;
  kind: string;
  boundary_rows_delta: string;
  dropped_partition_delta: string;
  safe_detail_code: string | null;
}

export interface RetentionJobPartitionEvidence {
  sequence: string;
  partition_name: string;
  action: string;
  evidence_at: string;
  boundary_rows_deleted: string;
  dropped_rows_estimate: string | null;
  dropped_rows_accuracy: "estimated" | "unavailable";
}

export interface RetentionJobCheckpointPage {
  items: RetentionJobCheckpoint[];
  has_more: boolean;
  next_cursor: string | null;
  generated_at: string;
}

export interface RetentionJobPartitionPage {
  items: RetentionJobPartitionEvidence[];
  has_more: boolean;
  next_cursor: string | null;
  generated_at: string;
}

export interface RetentionJobTerminalResult {
  kind: "succeeded" | "cancelled" | "failed" | "superseded";
  finished_at: string;
  visibility_state?: string;
  published_epoch?: string | null;
  published_floor?: string | null;
  accounting_provenance: "v2_exact" | "legacy_boundary_only";
  cancellation_scope?: string;
  coherent_outcome?: string;
  safe_error?: { code: string; message: string };
  disposition?: string;
  legacy_original_state?: string;
  replacement_job_id?: string | null;
}

export interface GlobalRetentionJobDetail {
  job: GlobalRetentionJobSummary;
  terminal_result: RetentionJobTerminalResult | null;
  checkpoints: RetentionJobCheckpointPage;
  partitions: RetentionJobPartitionPage;
}

export interface CancelRetentionJobResponse {
  operation_id: string;
  replayed: boolean;
  job: GlobalRetentionJobSummary;
}

export interface CostingSettingsResponse {
  profile_id: number;
  report_currency_code: string | null;
  report_currency_symbol: string | null;
  reporting_currency_epoch: string | null;
  currency_effective_at?: string | null;
  pricing_migration_state: string;
  legacy_migration_issues: string[];
  timezone_preference?: string | null;
  pricing_template_generation: string;
  pricing_reference_generation: string;
  active_template_count: number;
  pricing_migration_inventory: PricingMigrationInventorySummary | null;
  updated_at: string;
}

export interface PricingMigrationInventorySummary {
  inventory_id: string;
  inventory_hash: string;
  generation: number;
  issue_codes: string[];
  template_issue_count: number;
  legacy_fx_row_count: number;
  live_fx_dependency_count: number;
  recommended_operation_kind: "currency_cutover" | "repair_same_currency" | "archive_unused_fx";
  archive_only_available: boolean;
  template_scaffold_url: string;
  fx_evidence_url: string;
  reporting_currency_evidence?: {
    evidence_id: string;
    raw_currency_code: string;
    raw_currency_symbol: string;
    settings_updated_at: string;
    validation_codes: string[];
  } | null;
}

export interface PricingMigrationInventoryTemplate {
  template_id: number;
  name: string;
  updated_at: string;
  base_version: number;
  current_revision_id: string | null;
  current_input_price: string | null;
  current_output_price: string | null;
  current_cached_input_price: string | null;
  current_cache_creation_price: string | null;
  current_reasoning_price: string | null;
  legacy_template_evidence_id: string | null;
  raw_pricing_unit: string | null;
  raw_currency_code: string | null;
  raw_input_price: string | null;
  raw_output_price: string | null;
  raw_cached_input_price: string | null;
  raw_cache_creation_price: string | null;
  raw_reasoning_price: string | null;
  issue_codes: string[];
  model_reference_count: number;
  endpoint_reference_count: number;
  terminal_target_reference_count: number;
}

export interface PricingMigrationInventoryTemplatePage {
  inventory_id: string;
  inventory_hash: string;
  generation: number;
  total_active_template_count: number;
  items: PricingMigrationInventoryTemplate[];
  total_count: number;
  next_cursor: string | null;
}

export interface TimezonePreferenceResponse {
  timezone_preference?: string | null;
}

export interface CostingSettingsUpdate {
  report_currency_code: string;
  report_currency_symbol: string;
  timezone_preference?: string | null;
  expected_updated_at?: string | null;
  reporting_currency_epoch?: number | string;
  pricing_migration_state?: string;
  pricing_template_generation?: number | string;
  pricing_reference_generation?: number | string;
  pricing_migration_inventory?: PricingMigrationInventorySummary | null;
  active_template_count?: number;
}

// Normal costing PUTs never author the active currency code. Code changes use
// the dedicated preview/commit migration owner; this mutation keeps symbol,
// timezone and the shared Pricing CAS as the only ordinary write fields.
export interface CostingSettingsMutation {
  report_currency_symbol?: string;
  timezone_preference?: string | null;
  expected_updated_at?: string | null;
}

export interface TimezonePreferenceUpdate {
  timezone_preference?: string | null;
}

export type CurrencyMigrationOperationKind = "currency_cutover" | "repair_same_currency" | "archive_unused_fx";
export type CurrencyMigrationDraftOperationKind = Exclude<CurrencyMigrationOperationKind, "archive_unused_fx">;

export interface CurrencyMigrationDraftChunkSummary {
  ordinal: number;
  row_count: number;
  content_hash: string;
}

export interface CurrencyMigrationDraftChunkPage {
  items: CurrencyMigrationDraftChunkSummary[];
  total_count: number;
  consumed_count: number;
  next_cursor: string | null;
}

export interface CurrencyMigrationDraftHeader {
  draft_id: string;
  migration_operation_id: string;
  operation_kind: CurrencyMigrationDraftOperationKind;
  target_currency_code: string;
  target_currency_symbol: string;
  expected_inventory_id: string | null;
  expected_inventory_hash: string | null;
  expected_inventory_generation: number | null;
  expected_reporting_currency_epoch: number | null;
  expected_settings_updated_at: string;
  status: "uploading" | "sealed" | "committed" | "expired";
  normalized_header_hash: string;
  received_chunk_count: number;
  chunk_page: CurrencyMigrationDraftChunkPage;
  draft_hash: string | null;
  template_count: number | null;
  committed_result_operation_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface CurrencyMigrationDraftChunkItem {
  template_id: number;
  expected_version: number;
  expected_updated_at: string;
  input_price: string;
  output_price: string;
  cached_input_price: string | null;
  cache_creation_price: string | null;
  reasoning_price: string | null;
}

export interface CurrencyMigrationDraftItem {
  template_id: number;
  template_name: string;
  expected_version: number;
  expected_updated_at: string;
  input_price: string;
  output_price: string;
  cached_input_price: string | null;
  cache_creation_price: string | null;
  reasoning_price: string | null;
  reference_count: number;
}

export interface CurrencyMigrationDraftItemPage {
  items: CurrencyMigrationDraftItem[];
  total_count: number;
  next_cursor: string | null;
  has_more: boolean;
  limit: number;
}

export interface CurrencyMigrationPreviewItem {
  template_id: number;
  name: string;
  current_version: number;
  next_version: number;
  current_input_price: string | null;
  current_output_price: string | null;
  current_cached_input_price: string | null;
  current_cache_creation_price: string | null;
  current_reasoning_price: string | null;
  new_input_price: string;
  new_output_price: string;
  new_cached_input_price: string | null;
  new_cache_creation_price: string | null;
  new_reasoning_price: string | null;
  reference_count: number;
}

export interface CurrencyMigrationPreviewPage {
  items: CurrencyMigrationPreviewItem[];
  total_count: number;
  next_cursor: string | null;
  has_more: boolean;
  limit: number;
}

export interface CurrencyMigrationFXEvidence {
  legacy_fx_evidence_id: string;
  source_fx_row_id: string;
  model_id: string;
  endpoint_id: string;
  fx_rate: string;
  source_created_at: string;
  source_updated_at: string;
  row_hash: string;
  attribution: "has_live" | "unused" | "unknown";
  scan_proof_code: string;
  scan_proof_hash: string;
  dependency_count: number;
}

export interface CurrencyMigrationFXEvidencePage {
  inventory_id: string;
  inventory_hash: string;
  generation: number;
  items: CurrencyMigrationFXEvidence[];
  total_count: number;
  next_cursor: string | null;
}

export interface CurrencyMigrationPreview {
  operation_kind: CurrencyMigrationOperationKind;
  migration_operation_id: string;
  draft_id: string;
  draft_hash: string;
  preview_hash: string;
  target_currency_code: string;
  target_currency_symbol: string;
  current_currency_code: string | null;
  current_epoch: number | null;
  next_epoch: number | null;
  template_count: number;
  revision_change_count: number;
  template_page: CurrencyMigrationPreviewPage;
  committable: boolean;
  validation_errors: Array<Record<string, unknown>>;
  epoch_change: boolean;
  inventory_id?: string;
  inventory_hash?: string;
  archived_fx_evidence_count?: number;
  fx_evidence_page?: CurrencyMigrationFXEvidencePage;
}

export interface CurrencyMigrationPreviewItemsResponse {
  preview_hash: string;
  page: CurrencyMigrationPreviewPage;
}

export interface CurrencyMigrationCommitRequest {
  operation_kind: CurrencyMigrationOperationKind;
  migration_operation_id: string;
  draft_id: string;
  draft_hash: string;
  preview_hash: string;
  expected_inventory_id?: string;
  expected_inventory_hash?: string;
  expected_inventory_generation?: number;
  expected_reporting_currency_epoch?: number;
  expected_settings_updated_at?: string;
}

export interface CurrencyMigrationCommitResponse {
  old_currency_code: string | null;
  new_currency_code: string;
  old_epoch: number | null;
  new_epoch: number | null;
  revision_change_count: number;
  template_count: number;
  migration_operation_id: string;
  epoch_change: boolean;
  archived_fx_evidence_count?: number;
  inventory_id?: string;
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
