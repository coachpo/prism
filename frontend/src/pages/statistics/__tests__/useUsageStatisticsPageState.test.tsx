import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import {
  USAGE_STATISTICS_STORAGE_KEY,
  useUsageStatisticsPageState,
} from "../useUsageStatisticsPageState";

describe("useUsageStatisticsPageState", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it("persists the local usage statistics presentation state", async () => {
    const { result, unmount } = renderHook(() => useUsageStatisticsPageState());

    expect(result.current.state.selectedTimeRange).toBe("1h");
    expect(result.current.state.selectedModelLines).toEqual([]);
    expect(result.current.state.chartGranularity).toEqual({
      costOverview: "hourly",
      requestTrends: "hourly",
      tokenTypeBreakdown: "hourly",
      tokenUsageTrends: "hourly",
    });

    act(() => {
      result.current.setSelectedTimeRange("7d");
      result.current.setSelectedModelLines(["gpt-5.4", "claude-sonnet-4-6"]);
      result.current.setChartGranularity("requestTrends", "daily");
      result.current.setChartGranularity("costOverview", "daily");
    });

    await waitFor(() => {
      expect(localStorage.getItem(USAGE_STATISTICS_STORAGE_KEY)).not.toBeNull();
    });

    expect(JSON.parse(localStorage.getItem(USAGE_STATISTICS_STORAGE_KEY) ?? "null")).toEqual({
      state: {
        chartGranularity: {
          costOverview: "daily",
          requestTrends: "daily",
          tokenTypeBreakdown: "hourly",
          tokenUsageTrends: "hourly",
        },
        selectedModelLines: ["gpt-5.4", "claude-sonnet-4-6"],
        selectedTimeRange: "7d",
      },
      version: 1,
    });

    unmount();

    const { result: nextResult } = renderHook(() => useUsageStatisticsPageState());

    expect(nextResult.current.state.selectedTimeRange).toBe("7d");
    expect(nextResult.current.state.selectedModelLines).toEqual(["gpt-5.4", "claude-sonnet-4-6"]);
    expect(nextResult.current.state.chartGranularity).toEqual({
      costOverview: "daily",
      requestTrends: "daily",
      tokenTypeBreakdown: "hourly",
      tokenUsageTrends: "hourly",
    });
  });

  it("resets to defaults when the stored shape is unsupported", () => {
    localStorage.setItem(
      USAGE_STATISTICS_STORAGE_KEY,
      JSON.stringify({
        chartGranularity: {
          costOverview: "daily",
          requestTrends: "daily",
          tokenTypeBreakdown: "daily",
          tokenUsageTrends: "daily",
        },
        selectedModelLines: ["stale-model"],
        selectedTimeRange: "7d",
      }),
    );

    const { result } = renderHook(() => useUsageStatisticsPageState());

    expect(result.current.state.selectedTimeRange).toBe("1h");
    expect(result.current.state.selectedModelLines).toEqual([]);
    expect(result.current.state.chartGranularity).toEqual({
      costOverview: "hourly",
      requestTrends: "hourly",
      tokenTypeBreakdown: "hourly",
      tokenUsageTrends: "hourly",
    });
  });
});
