import type { ReactNode } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type {
  UsageModelStatistic,
  UsageSnapshotResponse,
  UsageStatisticsPageState,
} from "@/lib/types";
import { useUsageStatisticsPageData } from "../useUsageStatisticsPageData";

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((innerResolve) => {
    resolve = innerResolve;
  });
  return { promise, resolve };
}

const api = vi.hoisted(() => ({
  stats: {
    endpointModelStatistics: vi.fn(),
    usageSnapshot: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));

function createState(
  overrides: Partial<UsageStatisticsPageState> = {},
): UsageStatisticsPageState {
  return {
    chartGranularity: {
      costOverview: "hourly",
      requestTrends: "hourly",
      tokenTypeBreakdown: "hourly",
      tokenUsageTrends: "hourly",
    },
    selectedModelLines: [],
    selectedTimeRange: "1h",
    ...overrides,
  };
}

function createSnapshot(overrides?: Partial<UsageSnapshotResponse>): UsageSnapshotResponse {
  return {
    cost_overview: {
      daily: [{ bucket_start: "2026-03-27T00:00:00Z", total_cost_micros: 4200 }],
      hourly: [{ bucket_start: "2026-03-27T11:00:00Z", total_cost_micros: 4200 }],
      priced_request_count: 1,
      total_cost_micros: 4200,
      unpriced_request_count: 1,
    },
    currency: { code: "USD", symbol: "$" },
    endpoint_statistics: [
      {
        endpoint_id: 10,
        endpoint_label: "Primary Endpoint",
        request_count: 1,
        success_rate: 100,
        total_cost_micros: 4200,
        total_tokens: 185,
      },
      {
        endpoint_id: 11,
        endpoint_label: "Unknown Endpoint",
        request_count: 1,
        success_rate: 0,
        total_cost_micros: 0,
        total_tokens: 60,
      },
    ],
    generated_at: "2026-03-27T12:00:00Z",
    model_statistics: [
      {
        model_id: "gpt-5.4",
        model_label: "GPT-5.4",
        request_count: 1,
        success_rate: 100,
        total_cost_micros: 4200,
        total_tokens: 185,
      },
      {
        model_id: "claude-sonnet-4-6",
        model_label: "Claude Sonnet 4.6",
        request_count: 1,
        success_rate: 0,
        total_cost_micros: 0,
        total_tokens: 60,
      },
    ],
    overview: {
      average_rpm: 0.005,
      average_tpm: 0.583,
      failed_requests: 1,
      input_tokens: 140,
      output_tokens: 70,
      cached_tokens: 25,
      reasoning_tokens: 10,
      rolling_request_count: 1,
      rolling_rpm: 0.033,
      rolling_token_count: 185,
      rolling_tpm: 6.167,
      rolling_window_minutes: 30,
      success_rate: 50,
      success_requests: 1,
      total_cost_micros: 4200,
      total_requests: 2,
      total_tokens: 245,
    },
    proxy_api_key_statistics: [
      {
        proxy_api_key_id: 77,
        proxy_api_key_label: "Primary runtime key",
        request_count: 2,
        success_rate: 50,
        total_cost_micros: 4200,
        total_tokens: 245,
      },
      {
        proxy_api_key_id: null,
        proxy_api_key_label: "Unknown Proxy API Key",
        request_count: 1,
        success_rate: 0,
        total_cost_micros: 0,
        total_tokens: 60,
      },
    ],
    request_trends: {
      daily: [
        {
          key: "all",
          label: "All Models",
          points: [
            {
              bucket_start: "2026-03-27T00:00:00Z",
              failed_count: 1,
              request_count: 2,
              rpm: 0.0014,
              success_count: 1,
            },
          ],
          total_requests: 2,
        },
        {
          key: "gpt-5.4",
          label: "GPT-5.4",
          points: [
            {
              bucket_start: "2026-03-27T00:00:00Z",
              failed_count: 0,
              request_count: 1,
              rpm: 0.0007,
              success_count: 1,
            },
          ],
          total_requests: 1,
        },
      ],
      hourly: [
        {
          key: "all",
          label: "All Models",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              failed_count: 1,
              request_count: 2,
              rpm: 2,
              success_count: 1,
            },
          ],
          total_requests: 2,
        },
        {
          key: "gpt-5.4",
          label: "GPT-5.4",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              failed_count: 0,
              request_count: 1,
              rpm: 1,
              success_count: 1,
            },
          ],
          total_requests: 1,
        },
        {
          key: "claude-sonnet-4-6",
          label: "Claude Sonnet 4.6",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              failed_count: 1,
              request_count: 1,
              rpm: 1,
              success_count: 0,
            },
          ],
          total_requests: 1,
        },
      ],
    },
    service_health: {
      availability_percentage: 50,
      cells: [
        {
          availability_percentage: 100,
          bucket_start: "2026-03-27T11:00:00Z",
          failed_count: 0,
          request_count: 1,
          status: "ok",
          success_count: 1,
        },
      ],
      failed_count: 1,
      interval_minutes: 15,
      request_count: 2,
      success_count: 1,
    },
    time_range: {
      end_at: "2026-03-27T12:00:00Z",
      preset: "1h",
      start_at: "2026-03-27T11:00:00Z",
      ...overrides?.time_range,
    },
    token_type_breakdown: {
      daily: [
        {
          bucket_start: "2026-03-27T00:00:00Z",
          cached_tokens: 25,
          input_tokens: 140,
          output_tokens: 70,
          reasoning_tokens: 10,
        },
      ],
      hourly: [
        {
          bucket_start: "2026-03-27T11:00:00Z",
          cached_tokens: 25,
          input_tokens: 140,
          output_tokens: 70,
          reasoning_tokens: 10,
        },
      ],
    },
    token_usage_trends: {
      daily: [
        {
          key: "all",
          label: "All requests",
          points: [
            {
              bucket_start: "2026-03-27T00:00:00Z",
              cached_tokens: 25,
              input_tokens: 140,
              output_tokens: 70,
              reasoning_tokens: 10,
              total_tokens: 245,
              tpm: 0.17,
            },
          ],
          total_tokens: 245,
        },
      ],
      hourly: [
        {
          key: "all",
          label: "All requests",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              cached_tokens: 25,
              input_tokens: 140,
              output_tokens: 70,
              reasoning_tokens: 10,
              total_tokens: 245,
              tpm: 245,
            },
          ],
          total_tokens: 245,
        },
        {
          key: "gpt-5.4",
          label: "GPT-5.4",
          points: [
            {
              bucket_start: "2026-03-27T11:00:00Z",
              cached_tokens: 25,
              input_tokens: 100,
              output_tokens: 50,
              reasoning_tokens: 10,
              total_tokens: 185,
              tpm: 185,
            },
          ],
          total_tokens: 185,
        },
      ],
    },
    ...overrides,
  };
}

describe("useUsageStatisticsPageData", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.stats.usageSnapshot.mockResolvedValue(createSnapshot());
    api.stats.endpointModelStatistics.mockResolvedValue([
      {
        model_id: "claude-sonnet-4-6",
        model_label: "Claude Sonnet 4.6",
        request_count: 1,
        success_rate: 100,
        total_cost_micros: 1200,
        total_tokens: 60,
      },
    ]);
  });

  function wrapper({ children }: { children: ReactNode }) {
    return <LocaleProvider>{children}</LocaleProvider>;
  }

  it("filters visible model lines and localizes the surviving statistics payload", async () => {
    const { result } = renderHook(
      () =>
        useUsageStatisticsPageData({
          revision: 3,
          selectedProfileId: 1,
          state: createState({
            chartGranularity: {
              costOverview: "daily",
              requestTrends: "hourly",
              tokenTypeBreakdown: "hourly",
              tokenUsageTrends: "hourly",
            },
            selectedModelLines: ["gpt-5.4"],
            selectedTimeRange: "1h",
          }),
        }),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(api.stats.usageSnapshot).toHaveBeenCalledWith({ preset: "1h" });
    expect(result.current.snapshot?.overview.total_requests).toBe(2);
    expect(result.current.availableModelLineIds).toEqual(["claude-sonnet-4-6", "gpt-5.4"]);
    expect(result.current.requestTrendSeries.map((series) => series.key)).toEqual([
      "all",
      "gpt-5.4",
    ]);
    expect(result.current.requestTrendSeries[0]?.label).toBe("All Models");
    expect(result.current.tokenUsageTrendSeries.map((series) => series.key)).toEqual([
      "all",
      "gpt-5.4",
    ]);
    expect(result.current.snapshot?.overview.rolling_window_minutes).toBe(30);
    expect(result.current.snapshot?.service_health.interval_minutes).toBe(15);
    expect(result.current.snapshot?.endpoint_statistics[1]?.endpoint_label).toBe("Unknown endpoint");
    expect(result.current.snapshot?.proxy_api_key_statistics[1]?.proxy_api_key_label).toBe(
      "Unknown proxy API key",
    );
    expect(result.current).not.toHaveProperty("requestEvents");
    expect(result.current).not.toHaveProperty("requestEventsTotal");
  });

  it("lazy-loads endpoint model statistics against the current snapshot window and clears the cache on refresh", async () => {
    const { result } = renderHook(
      () =>
        useUsageStatisticsPageData({
          revision: 3,
          selectedProfileId: 1,
          state: createState({ selectedTimeRange: "1h" }),
        }),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(api.stats.endpointModelStatistics).not.toHaveBeenCalled();

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10);
    });

    await waitFor(() => {
      expect(result.current.endpointModelStatisticsByEndpointId[10]).toEqual([
        {
          model_id: "claude-sonnet-4-6",
          model_label: "Claude Sonnet 4.6",
          request_count: 1,
          success_rate: 100,
          total_cost_micros: 1200,
          total_tokens: 60,
        },
      ]);
    });

    expect(api.stats.endpointModelStatistics).toHaveBeenCalledWith(10, {
      from_time: "2026-03-27T11:00:00Z",
      to_time: "2026-03-27T12:00:00Z",
    });

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10);
    });
    expect(api.stats.endpointModelStatistics).toHaveBeenCalledTimes(1);

    await act(async () => {
      await result.current.refresh();
    });

    await waitFor(() => {
      expect(api.stats.usageSnapshot).toHaveBeenCalledTimes(2);
      expect(result.current.endpointModelStatisticsByEndpointId).toEqual({});
    });

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10);
    });

    await waitFor(() => {
      expect(api.stats.endpointModelStatistics).toHaveBeenNthCalledWith(2, 10, {
        from_time: "2026-03-27T11:00:00Z",
        to_time: "2026-03-27T12:00:00Z",
      });
    });
  });

  it("clears endpoint model detail cache when the selected time range changes", async () => {
    const { result, rerender } = renderHook(
      ({ selectedProfileId, state }) =>
        useUsageStatisticsPageData({
          revision: 3,
          selectedProfileId,
          state,
        }),
      {
        initialProps: { selectedProfileId: 1, state: createState({ selectedTimeRange: "1h" }) },
        wrapper,
      },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10);
    });

    await waitFor(() => {
      expect(api.stats.endpointModelStatistics).toHaveBeenCalledWith(10, {
        from_time: "2026-03-27T11:00:00Z",
        to_time: "2026-03-27T12:00:00Z",
      });
    });

    rerender({ selectedProfileId: 1, state: createState({ selectedTimeRange: "7d" }) });

    await waitFor(() => {
      expect(api.stats.usageSnapshot).toHaveBeenLastCalledWith({ preset: "7d" });
      expect(result.current.endpointModelStatisticsByEndpointId).toEqual({});
    });

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10);
    });

    await waitFor(() => {
      expect(api.stats.endpointModelStatistics).toHaveBeenLastCalledWith(10, {
        from_time: "2026-03-27T11:00:00Z",
        to_time: "2026-03-27T12:00:00Z",
      });
    });
  });

  it("clears endpoint model detail cache when the selected profile changes", async () => {
    const { result, rerender } = renderHook(
      ({ selectedProfileId }) =>
        useUsageStatisticsPageData({
          revision: 3,
          selectedProfileId,
          state: createState({ selectedTimeRange: "1h" }),
        }),
      {
        initialProps: { selectedProfileId: 1 },
        wrapper,
      },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10);
    });

    await waitFor(() => {
      expect(result.current.endpointModelStatisticsByEndpointId[10]).toBeDefined();
    });

    rerender({ selectedProfileId: 2 });

    await waitFor(() => {
      expect(api.stats.usageSnapshot).toHaveBeenCalledTimes(2);
      expect(result.current.endpointModelStatisticsByEndpointId).toEqual({});
    });
  });

  it("ignores stale endpoint model detail responses after the scope changes", async () => {
    const deferred = createDeferred<UsageModelStatistic[]>();
    api.stats.endpointModelStatistics.mockReset();
    api.stats.endpointModelStatistics.mockImplementationOnce(() => deferred.promise);

    const { result, rerender } = renderHook(
      ({ state }) =>
        useUsageStatisticsPageData({
          revision: 3,
          selectedProfileId: 1,
          state,
        }),
      {
        initialProps: { state: createState({ selectedTimeRange: "1h" }) },
        wrapper,
      },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    await act(async () => {
      void result.current.loadEndpointModelStatistics(10);
      await Promise.resolve();
    });

    rerender({ state: createState({ selectedTimeRange: "7d" }) });

    await waitFor(() => {
      expect(result.current.endpointModelStatisticsByEndpointId).toEqual({});
    });

    await act(async () => {
      deferred.resolve([
        {
          model_id: "stale-model",
          model_label: "Stale Model",
          request_count: 99,
          success_rate: 100,
          total_cost_micros: 9999,
          total_tokens: 999,
        },
      ]);
      await deferred.promise;
    });

    await waitFor(() => {
      expect(result.current.endpointModelStatisticsByEndpointId).toEqual({});
    });
  });

  it("ignores endpoint detail responses that started during refresh after the new snapshot is accepted", async () => {
    const deferredSnapshot = createDeferred<UsageSnapshotResponse>();
    const deferredDetails = createDeferred<UsageModelStatistic[]>();

    api.stats.usageSnapshot.mockReset();
    api.stats.usageSnapshot
      .mockResolvedValueOnce(createSnapshot())
      .mockImplementationOnce(() => deferredSnapshot.promise);
    api.stats.endpointModelStatistics.mockReset();
    api.stats.endpointModelStatistics.mockImplementationOnce(() => deferredDetails.promise);

    const { result } = renderHook(
      () =>
        useUsageStatisticsPageData({
          revision: 3,
          selectedProfileId: 1,
          state: createState({ selectedTimeRange: "1h" }),
        }),
      { wrapper },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    await act(async () => {
      void result.current.refresh();
      await Promise.resolve();
    });

    await waitFor(() => {
      expect(result.current.loading).toBe(true);
    });

    await act(async () => {
      void result.current.loadEndpointModelStatistics(10);
      await Promise.resolve();
    });

    expect(api.stats.endpointModelStatistics).toHaveBeenCalledWith(10, {
      from_time: "2026-03-27T11:00:00Z",
      to_time: "2026-03-27T12:00:00Z",
    });

    await act(async () => {
      deferredSnapshot.resolve(
        createSnapshot({
          generated_at: "2026-03-27T12:05:00Z",
          overview: {
            ...createSnapshot().overview,
            total_requests: 9,
          },
          time_range: {
            end_at: "2026-03-27T12:05:00Z",
            preset: "1h",
            start_at: "2026-03-27T11:05:00Z",
          },
        }),
      );
      await deferredSnapshot.promise;
    });

    await waitFor(() => {
      expect(result.current.snapshot?.generated_at).toBe("2026-03-27T12:05:00Z");
      expect(result.current.endpointModelStatisticsByEndpointId).toEqual({});
    });

    await act(async () => {
      deferredDetails.resolve([
        {
          model_id: "stale-model",
          model_label: "Stale Model",
          request_count: 99,
          success_rate: 100,
          total_cost_micros: 9999,
          total_tokens: 999,
        },
      ]);
      await deferredDetails.promise;
    });

    await waitFor(() => {
      expect(result.current.snapshot?.generated_at).toBe("2026-03-27T12:05:00Z");
      expect(result.current.endpointModelStatisticsByEndpointId).toEqual({});
    });
  });
});
