import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { GlobalRetentionJobSummary } from "@/lib/types";
import { toast } from "sonner";
import { newRetentionOperationId } from "./retentionProtocol";

interface UseRetentionJobListInput {
  enabled: boolean;
}

export function useRetentionJobList({ enabled }: UseRetentionJobListInput) {
  const messages = getStaticMessages();
  const jobTerminalNotice =
    messages.settingsRetentionDeletion.jobTerminalNotice;
  const [jobs, setJobs] = useState<GlobalRetentionJobSummary[]>([]);
  const [jobsHasMore, setJobsHasMore] = useState(false);
  const [jobsLoading, setJobsLoading] = useState(false);
  const [jobsStale, setJobsStale] = useState(false);
  /** The failed read's message; the snapshot stays on screen behind it. */
  const [jobsError, setJobsError] = useState<string | null>(null);
  // Last successful jobs read, so the freshness bar can answer "when is this
  // from" even after a failed refresh keeps the previous rows on screen.
  const [jobsLoadedAt, setJobsLoadedAt] = useState<string | null>(null);
  const [jobOriginFilter, setJobOriginFilter] = useState<
    "all" | "manual" | "automatic"
  >("all");
  const [jobStateFilter, setJobStateFilter] = useState("all");
  const jobsRequestIdRef = useRef(0);
  const jobsCursorRef = useRef<string | null>(null);
  /** How many pages the static snapshot currently holds. */
  const jobsPagesLoadedRef = useRef(1);
  const terminalJobStateRef = useRef(new Map<string, string>());
  const cancelOperationIdsRef = useRef(new Map<string, string>());
  const jobsSnapshotInitializedRef = useRef(false);

  /** Announce terminal transitions observed against the previous snapshot. */
  const announceTerminalTransitions = useCallback(
    (items: GlobalRetentionJobSummary[]) => {
      if (!jobsSnapshotInitializedRef.current) return;
      for (const job of items) {
        const previousState = terminalJobStateRef.current.get(job.id);
        if (
          previousState &&
          previousState !== job.state &&
          ["cancelled", "succeeded", "failed", "superseded"].includes(job.state)
        ) {
          toast.info(jobTerminalNotice(job.dataset, job.state, job.id));
        }
      }
    },
    [jobTerminalNotice],
  );

  /** Record every read's states so a later snapshot can diff transitions. */
  const rememberJobStates = useCallback(
    (items: GlobalRetentionJobSummary[]) => {
      for (const job of items)
        terminalJobStateRef.current.set(job.id, job.state);
      jobsSnapshotInitializedRef.current = true;
    },
    [],
  );

  const fetchJobs = useCallback(
    async (append = false) => {
      const requestId = ++jobsRequestIdRef.current;
      setJobsLoading(true);
      if (!append) {
        setJobsError(null);
        jobsPagesLoadedRef.current = 1;
      }
      try {
        const response = await api.settings.retention.jobs.list({
          origin: jobOriginFilter === "all" ? undefined : jobOriginFilter,
          state: jobStateFilter === "all" ? undefined : [jobStateFilter],
          cursor: append ? (jobsCursorRef.current ?? undefined) : undefined,
        });
        if (requestId !== jobsRequestIdRef.current) return;
        announceTerminalTransitions(response.items);
        rememberJobStates(response.items);
        // Appends extend the static snapshot; fresh loads replace it wholesale.
        setJobs((current) =>
          append ? [...current, ...response.items] : response.items,
        );
        jobsCursorRef.current = response.next_cursor;
        if (append) jobsPagesLoadedRef.current += 1;
        setJobsHasMore(response.has_more);
        setJobsStale(false);
        setJobsError(null);
        setJobsLoadedAt(new Date().toISOString());
      } catch (error) {
        if (requestId === jobsRequestIdRef.current) {
          // The snapshot stays exactly as it was — rows and cursor both. Only
          // the honest-state surface changes.
          setJobsStale(true);
          setJobsError(
            error instanceof Error
              ? error.message
              : messages.settingsRetentionDeletion.jobsLoadFailed,
          );
        }
      } finally {
        if (requestId === jobsRequestIdRef.current) setJobsLoading(false);
      }
    },
    [
      announceTerminalTransitions,
      jobOriginFilter,
      jobStateFilter,
      messages.settingsRetentionDeletion.jobsLoadFailed,
      rememberJobStates,
    ],
  );

  /**
   * Post-mutation / manual calibration over the static snapshot: serially
   * re-fetch up to the currently loaded number of pages with fresh cursors,
   * then swap the whole list in one commit. Any page failure keeps the old
   * snapshot untouched behind the staleness badge (全成全败).
   */
  const calibrateJobs = useCallback(async () => {
    const requestId = ++jobsRequestIdRef.current;
    setJobsLoading(true);
    try {
      const pagesToLoad = Math.max(1, jobsPagesLoadedRef.current);
      const collected: GlobalRetentionJobSummary[] = [];
      let cursor: string | undefined = undefined;
      let nextCursor: string | null = null;
      let hasMore = false;
      for (let page = 0; page < pagesToLoad; page += 1) {
        const response = await api.settings.retention.jobs.list({
          origin: jobOriginFilter === "all" ? undefined : jobOriginFilter,
          state: jobStateFilter === "all" ? undefined : [jobStateFilter],
          cursor,
        });
        if (requestId !== jobsRequestIdRef.current) return;
        collected.push(...response.items);
        nextCursor = response.next_cursor;
        hasMore = response.has_more;
        if (!response.has_more || !response.next_cursor) break;
        cursor = response.next_cursor;
      }
      announceTerminalTransitions(collected);
      rememberJobStates(collected);
      // All pages succeeded: swap atomically.
      setJobs(collected);
      jobsCursorRef.current = nextCursor;
      setJobsHasMore(hasMore);
      setJobsStale(false);
      setJobsError(null);
      setJobsLoadedAt(new Date().toISOString());
    } catch (error) {
      if (requestId === jobsRequestIdRef.current) {
        setJobsStale(true);
        setJobsError(
          error instanceof Error
            ? error.message
            : messages.settingsRetentionDeletion.jobDetailFailed,
        );
      }
    } finally {
      if (requestId === jobsRequestIdRef.current) setJobsLoading(false);
    }
  }, [
    announceTerminalTransitions,
    jobOriginFilter,
    jobStateFilter,
    messages.settingsRetentionDeletion.jobDetailFailed,
    rememberJobStates,
  ]);

  useEffect(() => {
    if (!enabled) return;
    jobsCursorRef.current = null;
    void fetchJobs(false);
  }, [enabled, fetchJobs, jobOriginFilter, jobStateFilter]);

  // The browser never polls: the job center is a static snapshot with an
  // explicit refresh control plus post-mutation calibration. Server-side job
  // progress stays owned by the management-jobs worker.

  const handleCancelJob = useCallback(
    async (job: GlobalRetentionJobSummary) => {
      // Keep the same operation identity across a response-loss retry.  A new
      // cancellation intent is created only after the server has durably
      // returned the previous result.
      const operationId =
        cancelOperationIdsRef.current.get(job.id) ??
        newRetentionOperationId("retention-cancel");
      cancelOperationIdsRef.current.set(job.id, operationId);
      try {
        const response = await api.settings.retention.jobs.cancel(
          job.id,
          operationId,
        );
        cancelOperationIdsRef.current.delete(job.id);
        // Optimistic row patch for instant feedback, then the post-mutation
        // calibration re-reads every loaded page with fresh cursors and swaps
        // atomically.
        setJobs((current) =>
          current.map((item) =>
            item.id === response.job.id ? response.job : item,
          ),
        );
        void calibrateJobs();
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : messages.settingsRetentionDeletion.jobCancelFailed,
        );
      }
    },
    [calibrateJobs, messages.settingsRetentionDeletion.jobCancelFailed],
  );

  return {
    calibrateJobs,
    jobs,
    jobsHasMore,
    jobsLoading,
    jobsLoadedAt,
    jobsStale,
    jobsError,
    refreshJobs: () => {
      void calibrateJobs();
    },
    jobOriginFilter,
    jobStateFilter,
    setJobOriginFilter,
    setJobStateFilter,
    loadMoreJobs: () => {
      void fetchJobs(true);
    },
    handleCancelJob,
  };
}
