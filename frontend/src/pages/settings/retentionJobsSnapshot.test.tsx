// C5 regression: the retention job center is a static browser-side snapshot —
// no background polling, explicit refresh, and post-mutation calibration that
// serially re-reads every loaded page with fresh cursors before swapping
// atomically (all-or-nothing).
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useRetentionDeletionData } from "./useRetentionDeletionData";

const mocks = vi.hoisted(() => ({
  jobsList: vi.fn(),
  settingsGet: vi.fn(),
  toastInfo: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  ApiError: class MockApiError extends Error {
    status: number;
    constructor(message: string, status = 0) {
      super(message);
      this.status = status;
    }
  },
  api: {
    settings: {
      costing: {
        get: vi.fn().mockResolvedValue({ timezone_preference: "UTC" }),
      },
      retention: {
        get: mocks.settingsGet,
        jobs: { list: mocks.jobsList },
      },
    },
  },
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    info: mocks.toastInfo,
  },
}));

function jobPage(ids: string[], nextCursor: string | null, hasMore: boolean) {
  return {
    items: ids.map((id) => ({
      id,
      dataset: "request_logs",
      origin: "manual",
      state: "queued",
      mode: "keep_days",
      cutoff: null,
      requested_at: "2026-08-13T12:00:00Z",
      attempt_count: 1,
      cancel_allowed: false,
      progress: {
        stage: "queued",
        boundary_rows_deleted: "0",
        dropped_partition_count: 0,
        visibility_state: "scheduled_cutoff_active",
        purge_state: "idle",
        protection: null,
      },
      error: null,
    })),
    has_more: hasMore,
    next_cursor: nextCursor,
    generated_at: "2026-08-13T12:00:00Z",
  };
}

function renderJobsHook() {
  return renderHook(() =>
    useRetentionDeletionData({
      enabled: true,
      setRecentlySavedSection: undefined,
    }),
  );
}

describe("retention job center static snapshot (C5)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.settingsGet.mockResolvedValue({
      revision: 1,
      policies: {
        request_logs_retention_days: 30,
        statistics_retention_days: 30,
        audit_logs_retention_days: 30,
        loadbalance_events_retention_days: 30,
      },
      recommendations: [],
    });
  });

  it("registers no background poll: the browser owns no timer for jobs", async () => {
    // The old implementation polled every five seconds via window.setInterval.
    // Pin its absence directly instead of racing wall-clock timers.
    const setIntervalSpy = vi.spyOn(window, "setInterval");
    mocks.jobsList.mockResolvedValue(jobPage(["j1"], null, false));
    const { result } = renderJobsHook();
    await waitFor(() => expect(result.current.jobsLoading).toBe(false));
    expect(result.current.jobs).toHaveLength(1);
    expect(mocks.jobsList).toHaveBeenCalledTimes(1);
    // waitFor itself registers a 50ms checker; anything at second-scale
    // would be the old five-second poll.
    const secondScaleIntervals = setIntervalSpy.mock.calls.filter(
      ([, delay]) => Number(delay) >= 1000,
    );
    expect(secondScaleIntervals).toEqual([]);
    setIntervalSpy.mockRestore();
  });

  it("calibrates by serially re-reading loaded depth with fresh cursors", async () => {
    mocks.jobsList.mockImplementation(
      async ({ cursor }: { cursor?: string }) => {
        if (!cursor) return jobPage(["j1", "j2"], "c2", true);
        if (cursor === "c2") return jobPage(["j3"], null, false);
        throw new Error(`unexpected cursor ${cursor}`);
      },
    );
    const { result } = renderJobsHook();
    await waitFor(() => expect(result.current.jobs).toHaveLength(2));

    await act(async () => {
      result.current.loadMoreJobs();
    });
    await waitFor(() => expect(result.current.jobs).toHaveLength(3));
    expect(mocks.jobsList).toHaveBeenCalledTimes(2);

    // Manual refresh re-walks both loaded pages serially with fresh cursors
    // and swaps atomically only after every page arrived.
    await act(async () => {
      result.current.refreshJobs();
    });
    await waitFor(() => expect(mocks.jobsList).toHaveBeenCalledTimes(4));
    expect(mocks.jobsList).toHaveBeenNthCalledWith(3, {
      origin: undefined,
      state: undefined,
      cursor: undefined,
    });
    expect(mocks.jobsList).toHaveBeenNthCalledWith(4, {
      origin: undefined,
      state: undefined,
      cursor: "c2",
    });
    expect(result.current.jobs.map((job) => job.id)).toEqual([
      "j1",
      "j2",
      "j3",
    ]);
    expect(result.current.jobsStale).toBe(false);
  });

  it("keeps the old snapshot untouched when a calibration page fails (全成全败)", async () => {
    mocks.jobsList.mockImplementation(
      async ({ cursor }: { cursor?: string }) => {
        if (!cursor) return jobPage(["j1"], "c2", true);
        if (cursor === "c2") return jobPage(["j2"], null, false);
        throw new Error(`unexpected cursor ${cursor}`);
      },
    );
    const { result } = renderJobsHook();
    await waitFor(() => expect(result.current.jobs).toHaveLength(1));
    await act(async () => {
      result.current.loadMoreJobs();
    });
    await waitFor(() => expect(result.current.jobs).toHaveLength(2));

    // Every read now fails mid-calibration: page two of the walk never
    // arrives, so page one's fresh rows must not be committed either.
    mocks.jobsList.mockRejectedValue(new Error("page read failed"));
    await act(async () => {
      result.current.refreshJobs();
    });
    await waitFor(() => expect(result.current.jobsStale).toBe(true));
    // Not one row of the old snapshot was swapped out.
    expect(result.current.jobs.map((job) => job.id)).toEqual(["j1", "j2"]);
    expect(result.current.jobsError).toContain("page read failed");
    // And the operator can retry the whole calibration explicitly.
    mocks.jobsList.mockImplementation(
      async ({ cursor }: { cursor?: string }) => {
        if (!cursor) return jobPage(["j1"], "c2", true);
        if (cursor === "c2") return jobPage(["j2"], null, false);
        throw new Error(`unexpected cursor ${cursor}`);
      },
    );
    await act(async () => {
      result.current.refreshJobs();
    });
    await waitFor(() => expect(result.current.jobsStale).toBe(false));
    expect(result.current.jobs.map((job) => job.id)).toEqual(["j1", "j2"]);
  });
});
