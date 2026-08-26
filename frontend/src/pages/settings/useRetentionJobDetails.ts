import { useCallback, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  GlobalRetentionJobDetail,
  GlobalRetentionJobSummary,
} from "@/lib/types";

export function useRetentionJobDetails() {
  const messages = getStaticMessages();
  const [selectedJob, setSelectedJob] =
    useState<GlobalRetentionJobSummary | null>(null);
  const [jobDetail, setJobDetail] = useState<GlobalRetentionJobDetail | null>(
    null,
  );
  /** Detail dialog reads: base detail plus one lane per evidence list. */
  const [jobDetailBaseLoading, setJobDetailBaseLoading] = useState(false);
  const [jobDetailBaseError, setJobDetailBaseError] = useState<string | null>(
    null,
  );
  const [checkpointsLane, setCheckpointsLane] = useState<{
    loading: boolean;
    error: string | null;
  }>({ loading: false, error: null });
  const [partitionsLane, setPartitionsLane] = useState<{
    loading: boolean;
    error: string | null;
  }>({ loading: false, error: null });
  const checkpointCursorRef = useRef<string | null>(null);
  const partitionCursorRef = useRef<string | null>(null);

  const openJobDetail = useCallback(
    async (job: GlobalRetentionJobSummary) => {
      setSelectedJob(job);
      setJobDetailBaseLoading(true);
      setJobDetailBaseError(null);
      // Both evidence lanes restart with their own cursor when the dialog
      // target changes; they stay independent of each other afterwards.
      checkpointCursorRef.current = null;
      partitionCursorRef.current = null;
      setCheckpointsLane({ loading: false, error: null });
      setPartitionsLane({ loading: false, error: null });
      try {
        const detail = await api.settings.retention.jobs.get(job.id);
        checkpointCursorRef.current = detail.checkpoints.next_cursor;
        partitionCursorRef.current = detail.partitions.next_cursor;
        setJobDetail(detail);
      } catch (error) {
        setJobDetail(null);
        setJobDetailBaseError(
          error instanceof Error
            ? error.message
            : messages.settingsRetentionDeletion.jobDetailFailed,
        );
      } finally {
        setJobDetailBaseLoading(false);
      }
    },
    [messages.settingsRetentionDeletion.jobDetailFailed],
  );

  /** One evidence lane's append read; the sibling lane is never touched. */
  const loadJobEvidence = useCallback(
    async (kind: "checkpoints" | "partitions") => {
      if (!selectedJob) return;
      const cursor =
        kind === "checkpoints"
          ? checkpointCursorRef.current
          : partitionCursorRef.current;
      if (!cursor) return;
      const setLane =
        kind === "checkpoints" ? setCheckpointsLane : setPartitionsLane;
      setLane({ loading: true, error: null });
      try {
        if (kind === "checkpoints") {
          const page = await api.settings.retention.jobs.checkpoints(
            selectedJob.id,
            { cursor },
          );
          checkpointCursorRef.current = page.next_cursor;
          setJobDetail((current) =>
            current
              ? {
                  ...current,
                  checkpoints: {
                    ...page,
                    items: [...current.checkpoints.items, ...page.items],
                  },
                }
              : current,
          );
        } else {
          const page = await api.settings.retention.jobs.partitions(
            selectedJob.id,
            { cursor },
          );
          partitionCursorRef.current = page.next_cursor;
          setJobDetail((current) =>
            current
              ? {
                  ...current,
                  partitions: {
                    ...page,
                    items: [...current.partitions.items, ...page.items],
                  },
                }
              : current,
          );
        }
        setLane({ loading: false, error: null });
      } catch (error) {
        // The failure belongs to this lane only: its own rows stay, its own
        // retry re-reads the same cursor, and the other lane keeps scrolling.
        setLane({
          loading: false,
          error:
            error instanceof Error
              ? error.message
              : messages.settingsRetentionDeletion.jobDetailFailed,
        });
      }
    },
    [messages.settingsRetentionDeletion.jobDetailFailed, selectedJob],
  );

  return {
    selectedJob,
    jobDetail,
    jobDetailBaseLoading,
    jobDetailBaseError,
    checkpointsLane,
    partitionsLane,
    openJobDetail,
    setSelectedJob,
    loadMoreJobCheckpoints: () => {
      void loadJobEvidence("checkpoints");
    },
    loadMoreJobPartitions: () => {
      void loadJobEvidence("partitions");
    },
  };
}
