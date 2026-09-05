import { Fragment, useState } from "react";
import { ChevronDown, ChevronRight, Loader2, RefreshCw } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type {
  GlobalRetentionJobDetail,
  GlobalRetentionJobSummary,
} from "@/lib/types";
import {
  OperatorEmptyState,
  OperatorLoadingState,
  OperatorMissingValue,
  OperatorSectionCard,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
  type OperatorStatusTier,
} from "@/shared/design-system";
import { OperationalTableSkeletonRows } from "@/shared/table/operationalTable";
import { LoadMoreControl } from "@/shared/table/paginationControls";

/** One evidence lane's read state inside the job detail dialog. */
export type RetentionEvidenceLane = { loading: boolean; error: string | null };

interface RetentionJobsSectionProps {
  jobs: GlobalRetentionJobSummary[];
  jobsHasMore: boolean;
  jobsLoading: boolean;
  jobsStale: boolean;
  /** The failed snapshot read's message; rows stay on screen behind it. */
  jobsError: string | null;
  jobsLoadedAt: string | null;
  /** Manual refresh: serially re-reads the loaded depth and swaps atomically. */
  onRefreshJobs: () => void;
  jobOriginFilter: "all" | "manual" | "automatic";
  jobStateFilter: string;
  setJobOriginFilter: (value: "all" | "manual" | "automatic") => void;
  setJobStateFilter: (value: string) => void;
  loadMoreJobs: () => void;
  handleCancelJob: (job: GlobalRetentionJobSummary) => Promise<void>;
  openJobDetail: (job: GlobalRetentionJobSummary) => Promise<void>;
  selectedJob: GlobalRetentionJobSummary | null;
  jobDetail: GlobalRetentionJobDetail | null;
  jobDetailBaseLoading: boolean;
  jobDetailBaseError: string | null;
  checkpointsLane: RetentionEvidenceLane;
  partitionsLane: RetentionEvidenceLane;
  retryJobDetail: () => void;
  setSelectedJob: (job: GlobalRetentionJobSummary | null) => void;
  loadMoreJobCheckpoints: () => void;
  loadMoreJobPartitions: () => void;
}

const JOB_STATES = [
  "queued",
  "running",
  "cancel_requested",
  "cancelled",
  "succeeded",
  "failed",
  "superseded",
] as const;
const JOB_COLUMN_COUNT = 8;

type JobCopy = ReturnType<
  typeof useLocale
>["messages"]["settingsRetentionDeletion"];

function jobStateTier(
  state: GlobalRetentionJobSummary["state"],
): OperatorStatusTier {
  if (state === "failed") return "failing";
  if (state === "running" || state === "cancel_requested") return "degraded";
  if (state === "succeeded") return "healthy";
  return "idle";
}

/**
 * The retention job center is a static browser-side snapshot over the durable
 * server queue: it loads once per scope/filter, extends by explicit
 * 加载更多, refreshes only on demand or after a mutation calibrates it, and
 * never polls in the background.
 */
export function RetentionJobsSection({
  jobs,
  jobsHasMore,
  jobsLoading,
  jobsStale,
  jobsError,
  jobsLoadedAt,
  onRefreshJobs,
  jobOriginFilter,
  jobStateFilter,
  setJobOriginFilter,
  setJobStateFilter,
  loadMoreJobs,
  handleCancelJob,
  openJobDetail,
  selectedJob,
  jobDetail,
  jobDetailBaseLoading,
  jobDetailBaseError,
  checkpointsLane,
  partitionsLane,
  retryJobDetail,
  setSelectedJob,
  loadMoreJobCheckpoints,
  loadMoreJobPartitions,
}: RetentionJobsSectionProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.settingsRetentionDeletion;
  const tableCopy = messages.operationalTable;
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  const toggleRow = (id: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <section id="retention-jobs" tabIndex={-1} className="scroll-mt-24">
      <OperatorSectionCard
        title={copy.retentionJobsTitle}
        description={copy.retentionJobsDescription}
        actions={
          <div className="flex items-center gap-2">
            {/* A failed calibration keeps the previous snapshot; the badge
                names when it was actually from. */}
            {jobsStale && jobsLoadedAt ? (
              <OperatorStalenessBadge
                label={copy.jobsStaleBadge}
                reason={jobsError ?? undefined}
              />
            ) : (
              <span className="text-xs text-muted-foreground">
                {copy.jobsSummary(formatNumber(jobs.length))}
              </span>
            )}
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onRefreshJobs}
              disabled={jobsLoading}
              aria-busy={jobsLoading}
            >
              {jobsLoading ? (
                <Loader2 data-icon="inline-start" className="animate-spin" />
              ) : (
                <RefreshCw data-icon="inline-start" />
              )}
              {copy.jobsRefresh}
            </Button>
          </div>
        }
        contentClassName="flex flex-col gap-3"
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <Field>
            <FieldLabel>{copy.jobOrigin}</FieldLabel>
            <Select
              value={jobOriginFilter}
              onValueChange={(value) =>
                setJobOriginFilter(value as typeof jobOriginFilter)
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{copy.allJobs}</SelectItem>
                <SelectItem value="automatic">{copy.automaticJobs}</SelectItem>
                <SelectItem value="manual">{copy.manualJobs}</SelectItem>
              </SelectContent>
            </Select>
          </Field>
          <Field>
            <FieldLabel>{copy.jobState}</FieldLabel>
            <Select value={jobStateFilter} onValueChange={setJobStateFilter}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">{copy.allStates}</SelectItem>
                {JOB_STATES.map((state) => (
                  <SelectItem key={state} value={state}>
                    {jobStateLabel(state, copy)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        </div>

        {jobsLoading && jobs.length === 0 ? (
          <OperationalTableSkeletonRows columns={JOB_COLUMN_COUNT} rows={4} />
        ) : jobs.length === 0 && !jobsLoading ? (
          jobsStale ? (
            // A first load that failed is a failure surface, never an empty list.
            <p role="alert" className="text-sm text-failing">
              {jobsError ?? copy.jobsLoadFailed}
            </p>
          ) : (
            <OperatorEmptyState
              title={copy.jobsEmptyTitle}
              description={copy.jobsEmptyDescription}
            />
          )
        ) : (
          <>
            <div aria-busy={jobsLoading || undefined}>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-8" />
                      <TableHead>{copy.jobsColumnDataset}</TableHead>
                      <TableHead>{copy.jobOrigin}</TableHead>
                      <TableHead>{copy.jobState}</TableHead>
                      <TableHead>{copy.jobMode}</TableHead>
                      <TableHead>{copy.jobStage}</TableHead>
                      <TableHead>{copy.requestedAt}</TableHead>
                      <TableHead className="text-right">
                        {copy.jobsColumnActions}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {jobs.map((job) => (
                      <RetentionJobRows
                        key={job.id}
                        copy={copy}
                        expanded={expanded.has(job.id)}
                        job={job}
                        onCancel={handleCancelJob}
                        onOpen={openJobDetail}
                        onToggle={() => toggleRow(job.id)}
                      />
                    ))}
                  </TableBody>
                </Table>
              </div>
            </div>
            {jobsHasMore ? (
              <LoadMoreControl
                testId="retention-jobs-load-more"
                pending={jobsLoading}
                error={null}
                hasMore
                labels={{
                  loadMore: copy.loadMoreJobs,
                  loading: tableCopy.loadingMore,
                  retry: tableCopy.retryLoadMore,
                }}
                onLoadMore={loadMoreJobs}
              />
            ) : null}
          </>
        )}
      </OperatorSectionCard>
      <RetentionJobDetailDialog
        detail={jobDetail}
        fallbackJob={selectedJob}
        baseLoading={jobDetailBaseLoading}
        baseError={jobDetailBaseError}
        checkpointsLane={checkpointsLane}
        partitionsLane={partitionsLane}
        onRetryBase={retryJobDetail}
        onOpenChange={(open) => {
          if (!open) setSelectedJob(null);
        }}
        onLoadMoreCheckpoints={loadMoreJobCheckpoints}
        onLoadMorePartitions={loadMoreJobPartitions}
      />
    </section>
  );
}

/**
 * One dense row plus an expansion holding the rest. The flat 14-item
 * definition list gave `请求时间` and `物理阶段` the same weight as the state,
 * and `物理阶段` rendered the exact same value as `阶段` — one fact printed
 * twice under two names.
 */
function RetentionJobRows({
  copy,
  expanded,
  job,
  onCancel,
  onOpen,
  onToggle,
}: {
  copy: JobCopy;
  expanded: boolean;
  job: GlobalRetentionJobSummary;
  onCancel: (job: GlobalRetentionJobSummary) => Promise<void>;
  onOpen: (job: GlobalRetentionJobSummary) => Promise<void>;
  onToggle: () => void;
}) {
  const { format } = useTimezone();
  const date = (value: string | null) =>
    value ? (
      <span className="font-mono text-xs tabular-nums">{format(value)}</span>
    ) : (
      <OperatorMissingValue className="text-xs" />
    );

  return (
    <Fragment>
      <TableRow className="group/row">
        <TableCell className="align-top">
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-expanded={expanded}
            aria-label={
              expanded ? copy.collapseJobRow(job.id) : copy.expandJobRow(job.id)
            }
            onClick={onToggle}
          >
            {expanded ? <ChevronDown /> : <ChevronRight />}
          </Button>
        </TableCell>
        <TableCell className="align-top">
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="font-medium">
              {datasetLabel(job.dataset, copy)}
            </span>
            <span
              className="truncate font-mono text-xs text-muted-foreground"
              title={job.id}
            >
              {job.id}
            </span>
          </div>
        </TableCell>
        <TableCell className="align-top">
          <OperatorTypeBadge
            intent="muted"
            preserveLabel
            label={
              job.origin === "manual" ? copy.manualJobs : copy.automaticJobs
            }
          />
        </TableCell>
        <TableCell className="align-top">
          <OperatorStatusBadge
            intent={jobStateTier(job.state)}
            preserveLabel
            label={jobStateLabel(job.state, copy)}
          />
        </TableCell>
        <TableCell className="align-top text-xs">
          {job.mode === "delete_all"
            ? copy.allData
            : copy.cutoffLabel(job.cutoff ?? copy.notAvailable)}
        </TableCell>
        <TableCell className="align-top text-xs">
          {jobStateLabel(job.progress.stage, copy)}
        </TableCell>
        <TableCell className="align-top">{date(job.requested_at)}</TableCell>
        <TableCell className="align-top text-right">
          <div className="flex justify-end gap-1">
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => void onOpen(job)}
            >
              {copy.viewJobDetails}
            </Button>
            {job.cancel_allowed ? (
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => void onCancel(job)}
              >
                {copy.cancelJob}
              </Button>
            ) : null}
          </div>
        </TableCell>
      </TableRow>

      {expanded ? (
        <TableRow>
          <TableCell colSpan={JOB_COLUMN_COUNT} className="bg-inset">
            <dl className="grid gap-3 text-xs sm:grid-cols-2 xl:grid-cols-4">
              <JobFact
                label={copy.boundaryRows}
                value={job.progress.boundary_rows_deleted}
              />
              <JobFact
                label={copy.droppedPartitions}
                value={job.progress.dropped_partition_count}
              />
              <JobFact
                label={copy.protection}
                value={protectionLabel(job.progress.protection, copy, format)}
              />
              <JobFact
                label={copy.purgeToTime}
                value={job.purge_to_time ? format(job.purge_to_time) : null}
              />
              <JobFact label={copy.attemptCount} value={job.attempt_count} />
              <JobFact
                label={copy.visibilityState}
                value={visibilityStateLabel(
                  job.progress.visibility_state,
                  copy,
                )}
              />
              <JobFact
                label={copy.purgeState}
                value={purgeStateLabel(job.progress.purge_state, copy)}
              />
              <JobFact
                label={copy.startedAt}
                value={job.started_at ? format(job.started_at) : null}
              />
              <JobFact
                label={copy.finishedAt}
                value={job.finished_at ? format(job.finished_at) : null}
              />
              <JobFact
                label={copy.lastHeartbeat}
                value={
                  job.last_heartbeat_at ? format(job.last_heartbeat_at) : null
                }
              />
            </dl>
            {job.error ? (
              <p className="mt-3 text-xs text-destructive">
                {job.error.code}: {job.error.message}
              </p>
            ) : null}
          </TableCell>
        </TableRow>
      ) : null}
    </Fragment>
  );
}

function JobFact({
  label,
  value,
}: {
  label: string;
  value: string | number | null | undefined;
}) {
  const mono =
    typeof value === "number" ||
    (typeof value === "string" && /^[\d.:+\-T\sZ]+$/.test(value));
  return (
    <div className="min-w-0">
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className={cn("break-words", mono && "font-mono tabular-nums")}>
        {value === null || value === undefined ? (
          <OperatorMissingValue />
        ) : (
          value
        )}
      </dd>
    </div>
  );
}

function protectionLabel(
  protection: GlobalRetentionJobSummary["progress"]["protection"],
  copy: JobCopy,
  format: (value: string) => string,
) {
  if (!protection) {
    return copy.protectionUnavailable;
  }
  switch (protection.kind) {
    case "observe_query_token":
      return `${copy.observeProtection} · ${format(protection.deadline)}`;
    case "audit_retention_fence":
      return `${copy.auditProtection} · ${protection.reader_fence_state}/${protection.materializer_state}`;
    case "none":
      return copy.noProtection;
    default:
      return copy.protectionUnavailable;
  }
}

/**
 * Detail dialog over two independent evidence lanes: each lane owns its
 * pending/error/retry surface, so a checkpoint failure never stops partition
 * evidence from loading more, and vice versa.
 */
function RetentionJobDetailDialog({
  detail,
  fallbackJob,
  baseLoading,
  baseError,
  checkpointsLane,
  partitionsLane,
  onRetryBase,
  onOpenChange,
  onLoadMoreCheckpoints,
  onLoadMorePartitions,
}: {
  detail: GlobalRetentionJobDetail | null;
  fallbackJob: GlobalRetentionJobSummary | null;
  baseLoading: boolean;
  baseError: string | null;
  checkpointsLane: RetentionEvidenceLane;
  partitionsLane: RetentionEvidenceLane;
  onRetryBase: () => void;
  onOpenChange: (open: boolean) => void;
  onLoadMoreCheckpoints: () => void;
  onLoadMorePartitions: () => void;
}) {
  const { messages } = useLocale();
  const tableCopy = messages.operationalTable;
  const copy = messages.settingsRetentionDeletion;
  const job = detail?.job ?? fallbackJob;
  return (
    <Dialog open={Boolean(fallbackJob)} onOpenChange={onOpenChange}>
      <DialogContent size="lg">
        <DialogHeader>
          <DialogTitle>{copy.jobDetails}</DialogTitle>
          <DialogDescription>
            {job
              ? `${job.id} · ${jobStateLabel(job.state, copy)}`
              : copy.notAvailable}
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {baseLoading ? (
            <OperatorLoadingState title={copy.jobDetailLoading} />
          ) : baseError ? (
            <div
              className="flex flex-col gap-2"
              role="alert"
              data-testid="retention-job-detail-error"
            >
              <p className="text-sm text-failing">{baseError}</p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={onRetryBase}
              >
                {tableCopy.retryLoadMore}
              </Button>
            </div>
          ) : detail ? (
            <div className="flex flex-col gap-4">
              {detail.terminal_result ? (
                <OperatorCalloutTerminal
                  kind={detail.terminal_result.kind}
                  label={copy.terminalResult}
                />
              ) : null}
              <EvidenceLane
                title={copy.checkpoints}
                count={detail.checkpoints.items.length}
                hasMore={Boolean(detail.checkpoints.has_more)}
                lane={checkpointsLane}
                retryLabel={tableCopy.retryLoadMore}
                loadingLabel={tableCopy.loadingMore}
                onLoadMore={onLoadMoreCheckpoints}
              >
                {detail.checkpoints.items.map((item) => (
                  <p
                    key={item.sequence}
                    className="font-mono text-xs text-muted-foreground"
                  >
                    #{item.sequence} · {item.stage} · {item.kind}
                  </p>
                ))}
              </EvidenceLane>
              <EvidenceLane
                title={copy.partitionEvidence}
                count={detail.partitions.items.length}
                hasMore={Boolean(detail.partitions.has_more)}
                lane={partitionsLane}
                retryLabel={tableCopy.retryLoadMore}
                loadingLabel={tableCopy.loadingMore}
                onLoadMore={onLoadMorePartitions}
              >
                {detail.partitions.items.map((item) => (
                  <p
                    key={item.sequence}
                    className="font-mono text-xs text-muted-foreground"
                  >
                    #{item.sequence} · {item.partition_name} · {item.action}
                  </p>
                ))}
              </EvidenceLane>
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {messages.settingsDialogs.cancel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** One evidence list with its own pending/error/retry lane. */
function EvidenceLane({
  title,
  count,
  hasMore,
  lane,
  retryLabel,
  loadingLabel,
  onLoadMore,
  children,
}: {
  title: string;
  count: number;
  hasMore: boolean;
  lane: RetentionEvidenceLane;
  retryLabel: string;
  loadingLabel: string;
  onLoadMore: () => void;
  children: React.ReactNode;
}) {
  if (count === 0 && !lane.error) return null;
  return (
    <div className="flex flex-col gap-2" data-testid={`evidence-lane-${title}`}>
      <h3 className="text-sm font-semibold">
        {title}: {count}
      </h3>
      <div className="flex flex-col gap-1">{children}</div>
      {lane.error ? (
        <p
          role="alert"
          className="text-xs text-failing"
          data-testid="evidence-lane-error"
        >
          {lane.error}
        </p>
      ) : null}
      {hasMore || lane.loading ? (
        <LoadMoreControl
          testId={`evidence-lane-more-${title}`}
          pending={lane.loading}
          error={lane.error}
          hasMore={hasMore}
          labels={{ loadMore: title, loading: loadingLabel, retry: retryLabel }}
          onLoadMore={onLoadMore}
        />
      ) : null}
    </div>
  );
}

function OperatorCalloutTerminal({
  kind,
  label,
}: {
  kind: string;
  label: string;
}) {
  return (
    <div
      className={cn(
        "rounded-md border px-3 py-2 text-xs",
        kind === "failed"
          ? "border-destructive/25 bg-destructive/10 text-destructive"
          : "border-healthy/25 bg-healthy/10 text-healthy",
      )}
    >
      {label}: {kind}
    </div>
  );
}

function datasetLabel(dataset: string, copy: JobCopy) {
  switch (dataset) {
    case "request_logs":
      return copy.requestLogsPolicy;
    case "usage_request_events":
      return copy.statisticsPolicy;
    case "audit_logs":
      return copy.auditLogsPolicy;
    case "loadbalance_events":
      return copy.loadbalanceEventsPolicy;
    default:
      return dataset;
  }
}

function jobStateLabel(state: string, copy: JobCopy) {
  const labels: Record<string, string> = {
    queued: copy.jobQueued,
    running: copy.jobRunning,
    cancel_requested: copy.jobCancelRequested,
    cancelled: copy.jobCancelled,
    succeeded: copy.jobSucceeded,
    failed: copy.jobFailed,
    superseded: copy.jobSuperseded,
    waiting_for_resource: copy.jobWaitingForResource,
    waiting_for_protection: copy.jobWaitingForProtection,
    acquiring_purge_fence: copy.jobAcquiringPurgeFence,
    dropping_partitions: copy.jobDroppingPartitions,
    deleting_boundary_rows: copy.jobDeletingBoundaryRows,
    cleaning_rollup_and_staging: copy.jobCleaningRollup,
    purge_running: copy.jobPurgeRunning,
    publishing_epoch_coverage: copy.jobPublishing,
    finished: copy.jobFinished,
  };
  return labels[state] ?? state;
}

function visibilityStateLabel(state: string, copy: JobCopy) {
  const labels: Record<string, string> = {
    scheduled_cutoff_active: copy.visibilityScheduledCutoffActive,
    purge_unavailable: copy.visibilityPurgeUnavailable,
    revoked: copy.visibilityRevoked,
    legacy_unknown: copy.visibilityLegacyUnknown,
  };
  return labels[state] ?? state;
}

function purgeStateLabel(state: string, copy: JobCopy) {
  const labels: Record<string, string> = {
    idle: copy.purgeIdle,
    running: copy.purgeRunning,
    recovery_required: copy.purgeRecoveryRequired,
    published: copy.purgePublished,
    rolled_back: copy.purgeRolledBack,
  };
  return labels[state] ?? state;
}
