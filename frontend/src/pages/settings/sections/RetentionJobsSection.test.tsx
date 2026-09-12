import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import type { GlobalRetentionJobSummary, GlobalRetentionJobDetail } from "@/lib/types"
import { RetentionJobsSection } from "./RetentionJobsSection"

vi.mock("@/hooks/useTimezone", () => ({ useTimezone: () => ({ format: (value: string) => value }) }))

const time = "2026-09-12T10:00:00Z"
const job: GlobalRetentionJobSummary = {
  id: "job-test", contract_version: 2, type: "log_retention", job_scope: "instance", origin: "manual",
  legacy_origin_provenance: null, legacy_execution_provenance: null, dataset: "request_logs", state: "failed",
  terminal_disposition: null, legacy_original_state: null, mode: "cutoff", cutoff: time, purge_to_time: time,
  policy_revision: null, preflight_id: null, operation_id: null, requested_at: time, started_at: time,
  finished_at: time, last_heartbeat_at: time, attempt_count: 1, cancel_allowed: false,
  progress: {
    accounting_provenance: "v2_exact", stage: "finished", visibility_state: "scheduled_cutoff_active", purge_state: "recovery_required",
    protection: { kind: "none" }, rows_matched_estimate: null, rows_matched_accuracy: "unavailable",
    boundary_rows_deleted: "2", boundary_batches_completed: "1", dropped_partition_count: "1", dropped_partition_count_accuracy: "exact",
    dropped_partition_names_preview: [], dropped_partition_names_total_count: "1", dropped_partition_names_truncated: false,
    dropped_rows_estimate: "10", dropped_rows_accuracy: "estimated", staged_items_tombstoned: null,
    sensitive_artifact_bytes_deleted: null, last_checkpoint_at: time,
  },
  error: { code: "sql_failed", message: "internal diagnostic /private/table.sql" },
}
const detail: GlobalRetentionJobDetail = {
  job,
  terminal_result: { kind: "failed", finished_at: time, accounting_provenance: "v2_exact" },
  checkpoints: { items: [{ sequence: "1", recorded_at: time, stage: "deleting_boundary_rows", kind: "internal_kind", boundary_rows_delta: "2", dropped_partition_delta: "0", safe_detail_code: null }], has_more: false, next_cursor: null, generated_at: time },
  partitions: { items: [{ sequence: "1", partition_name: "request_logs_private_202609", action: "dropped", evidence_at: time, boundary_rows_deleted: "0", dropped_rows_estimate: "10", dropped_rows_accuracy: "estimated" }], has_more: false, next_cursor: null, generated_at: time },
}

describe("retention task details", () => {
  it("shows the outcome, progress and estimated counts without diagnostic identifiers", () => {
    render(<LocaleProvider><RetentionJobsSection
      jobs={[job]} jobsHasMore={false} jobsLoading={false} jobsStale={false} jobsError={null} jobsLoadedAt={time}
      onRefreshJobs={vi.fn()} jobOriginFilter="all" jobStateFilter="all" setJobOriginFilter={vi.fn()} setJobStateFilter={vi.fn()}
      loadMoreJobs={vi.fn()} handleCancelJob={vi.fn()} openJobDetail={vi.fn()} selectedJob={job} jobDetail={detail}
      jobDetailBaseLoading={false} jobDetailBaseError={null} checkpointsLane={{ loading: false, error: null }} partitionsLane={{ loading: false, error: null }}
      retryJobDetail={vi.fn()} setSelectedJob={vi.fn()} loadMoreJobCheckpoints={vi.fn()} loadMoreJobPartitions={vi.fn()}
    /></LocaleProvider>)
    const dialog = screen.getByRole("dialog")
    expect(dialog).toHaveTextContent("清理结果: 失败")
    expect(dialog).toHaveTextContent("清理剩余记录")
    expect(dialog).toHaveTextContent("约 10 条")
    expect(dialog).not.toHaveTextContent(/request_logs_private|internal_kind|deleting_boundary_rows|sql_failed|private\/table/)
  })

  it("keeps initial loading rows inside a labelled table body", () => {
    const consoleError = vi.spyOn(console, "error");
    render(<LocaleProvider><RetentionJobsSection
      jobs={[]} jobsHasMore={false} jobsLoading jobsStale={false} jobsError={null} jobsLoadedAt={null}
      onRefreshJobs={vi.fn()} jobOriginFilter="all" jobStateFilter="all" setJobOriginFilter={vi.fn()} setJobStateFilter={vi.fn()}
      loadMoreJobs={vi.fn()} handleCancelJob={vi.fn()} openJobDetail={vi.fn()} selectedJob={null} jobDetail={null}
      jobDetailBaseLoading={false} jobDetailBaseError={null} checkpointsLane={{ loading: false, error: null }} partitionsLane={{ loading: false, error: null }}
      retryJobDetail={vi.fn()} setSelectedJob={vi.fn()} loadMoreJobCheckpoints={vi.fn()} loadMoreJobPartitions={vi.fn()}
    /></LocaleProvider>)
    const table = screen.getByRole("table");
    expect(table.querySelectorAll("thead > tr > th")).toHaveLength(8);
    expect(table.querySelectorAll("tbody > tr")).toHaveLength(5);
    expect(screen.getByRole("status")).toHaveTextContent("正在读取清理任务");
    expect(consoleError).not.toHaveBeenCalled();
  })

})
