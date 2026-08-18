import type { RetentionSettingsPolicies } from "./management-settings";

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
