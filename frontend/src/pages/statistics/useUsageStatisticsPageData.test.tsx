import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type {
  UsageModelStatistic,
  UsageSnapshotResponse,
  UsageStatisticsPageState,
} from "@/lib/types"
import { useUsageStatisticsPageData } from "./useUsageStatisticsPageData"

const mocks = vi.hoisted(() => ({
  endpointModelStatistics: vi.fn(),
  getSharedModels: vi.fn(),
  usageSnapshot: vi.fn(),
}))

vi.mock("@/lib/api", () => ({
  api: {
    stats: {
      endpointModelStatistics: mocks.endpointModelStatistics,
      usageSnapshot: mocks.usageSnapshot,
    },
  },
}))

vi.mock("@/lib/referenceData", () => ({
  getSharedModels: mocks.getSharedModels,
}))

vi.mock("@/i18n/useLocale", () => ({
  useLocale: () => ({
    messages: {
      modelDetail: {
        unknownEndpoint: "Unknown endpoint",
      },
      statistics: {
        allModels: "All models",
        failedToLoadEndpointModelStatistics: "Failed to load endpoint model statistics",
        failedToLoadUsageStatistics: "Failed to load usage statistics",
        unknownProxyApiKey: "Unknown proxy API key",
      },
    },
  }),
}))

const pageState: UsageStatisticsPageState = {
  chartGranularity: {
    costOverview: "hourly",
    requestTrends: "hourly",
    tokenTypeBreakdown: "hourly",
    tokenUsageTrends: "hourly",
  },
  selectedModelLines: [],
  selectedTimeRange: "24h",
}

describe("useUsageStatisticsPageData", () => {
  beforeEach(() => {
    mocks.endpointModelStatistics.mockReset()
    mocks.getSharedModels.mockReset()
    mocks.usageSnapshot.mockReset()
    mocks.getSharedModels.mockResolvedValue([])
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("reloads endpoint model drilldowns after polling accepts a fresh snapshot", async () => {
    const pollUsageSnapshotRef = captureUsagePolling()
    mocks.usageSnapshot
      .mockResolvedValueOnce(makeSnapshot("2026-07-08T10:00:00.000Z"))
      .mockResolvedValueOnce(makeSnapshot("2026-07-08T10:00:30.000Z"))
    mocks.endpointModelStatistics
      .mockResolvedValueOnce([makeModelStatistic("stale model", 1)])
      .mockResolvedValueOnce([makeModelStatistic("fresh model", 2)])

    const { result } = renderHook(() =>
      useUsageStatisticsPageData({
        revision: 1,
        selectedProfileId: 1,
        state: pageState,
      }),
    )

    await waitFor(() => {
      expect(result.current.snapshot?.generated_at).toBe("2026-07-08T10:00:00.000Z")
    })
    const initialDrilldownScopeKey = result.current.endpointModelStatisticsScopeKey

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10)
    })
    await waitFor(() => {
      expect(result.current.endpointModelStatisticsByEndpointId[10]?.[0]?.model_label).toBe(
        "stale model",
      )
    })

    const runPoll = pollUsageSnapshotRef.current
    if (!runPoll) {
      throw new Error("usage statistics polling was not registered")
    }

    await act(async () => {
      runPoll()
    })
    await waitFor(() => {
      expect(result.current.snapshot?.generated_at).toBe("2026-07-08T10:00:30.000Z")
    })

    expect(result.current.endpointModelStatisticsByEndpointId[10]).toBeUndefined()
    expect(result.current.endpointModelStatisticsScopeKey).not.toBe(initialDrilldownScopeKey)

    await act(async () => {
      await result.current.loadEndpointModelStatistics(10)
    })

    await waitFor(() => {
      expect(result.current.endpointModelStatisticsByEndpointId[10]?.[0]?.model_label).toBe(
        "fresh model",
      )
    })
    expect(mocks.endpointModelStatistics).toHaveBeenCalledTimes(2)
  })

  it("ignores endpoint model drilldowns that resolve after polling accepts a newer snapshot", async () => {
    const pollUsageSnapshotRef = captureUsagePolling()
    let resolveStaleDrilldown: (items: UsageModelStatistic[]) => void = () => {}
    const staleDrilldownRequest = new Promise<UsageModelStatistic[]>((resolve) => {
      resolveStaleDrilldown = resolve
    })

    mocks.usageSnapshot
      .mockResolvedValueOnce(makeSnapshot("2026-07-08T10:00:00.000Z"))
      .mockResolvedValueOnce(makeSnapshot("2026-07-08T10:00:30.000Z"))
    mocks.endpointModelStatistics.mockReturnValueOnce(staleDrilldownRequest)

    const { result } = renderHook(() =>
      useUsageStatisticsPageData({
        revision: 1,
        selectedProfileId: 1,
        state: pageState,
      }),
    )

    await waitFor(() => {
      expect(result.current.snapshot?.generated_at).toBe("2026-07-08T10:00:00.000Z")
    })

    let staleLoadPromise: Promise<void>
    act(() => {
      staleLoadPromise = result.current.loadEndpointModelStatistics(10)
    })
    await waitFor(() => {
      expect(mocks.endpointModelStatistics).toHaveBeenCalledTimes(1)
    })

    const runPoll = pollUsageSnapshotRef.current
    if (!runPoll) {
      throw new Error("usage statistics polling was not registered")
    }

    await act(async () => {
      runPoll()
    })
    await waitFor(() => {
      expect(result.current.snapshot?.generated_at).toBe("2026-07-08T10:00:30.000Z")
    })

    await act(async () => {
      resolveStaleDrilldown([makeModelStatistic("stale model", 1)])
      await staleLoadPromise
    })

    expect(result.current.endpointModelStatisticsByEndpointId[10]).toBeUndefined()
  })

  it("keeps an initial load error visible after a failing silent poll", async () => {
    const pollUsageSnapshotRef = captureUsagePolling()
    mocks.usageSnapshot
      .mockRejectedValueOnce(new Error("initial failed"))
      .mockRejectedValueOnce(new Error("poll failed"))

    const { result } = renderHook(() =>
      useUsageStatisticsPageData({
        revision: 1,
        selectedProfileId: 1,
        state: pageState,
      }),
    )

    await waitFor(() => {
      expect(result.current.error).toBe("initial failed")
    })
    expect(result.current.loading).toBe(false)

    const runPoll = pollUsageSnapshotRef.current
    if (!runPoll) {
      throw new Error("usage statistics polling was not registered")
    }

    await act(async () => {
      runPoll()
    })
    await waitFor(() => {
      expect(mocks.usageSnapshot).toHaveBeenCalledTimes(2)
    })

    expect(result.current.error).toBe("initial failed")
    expect(result.current.loading).toBe(false)
  })
})

function captureUsagePolling(): { current?: () => void } {
  const pollUsageSnapshotRef: { current?: () => void } = {}
  const realSetInterval = window.setInterval.bind(window)
  vi.spyOn(window, "setInterval").mockImplementation((handler, timeout, ...args) => {
    if (timeout === 30_000) {
      pollUsageSnapshotRef.current = typeof handler === "function" ? () => handler() : undefined
      return 1
    }
    return realSetInterval(handler, timeout, ...args)
  })
  vi.spyOn(window, "clearInterval").mockImplementation(() => {})
  return pollUsageSnapshotRef
}

function makeSnapshot(generatedAt: string): UsageSnapshotResponse {
  return {
    cost_overview: {
      daily: [],
      hourly: [],
      priced_request_count: 1,
      total_cost_micros: 100,
      unpriced_request_count: 0,
    },
    currency: {
      code: "USD",
      symbol: "$",
    },
    endpoint_statistics: [
      {
        avg_output_rate_tps: null,
        endpoint_id: 10,
        endpoint_label: "endpoint 10",
        p50_ttft_ms: null,
        p95_ttft_ms: null,
        request_count: 1,
        success_rate: 100,
        total_cost_micros: 100,
        total_tokens: 10,
      },
    ],
    generated_at: generatedAt,
    model_statistics: [],
    overview: {
      average_rpm: 1,
      average_tpm: 10,
      cached_tokens: 0,
      failed_requests: 0,
      input_tokens: 5,
      output_tokens: 5,
      reasoning_tokens: 0,
      success_rate: 100,
      success_requests: 1,
      total_cost_micros: 100,
      total_requests: 1,
      total_tokens: 10,
    },
    proxy_api_key_statistics: [],
    request_trends: {
      daily: [],
      hourly: [],
    },
    time_range: {
      end_at: generatedAt,
      preset: "24h",
      start_at: null,
    },
    token_type_breakdown: {
      daily: [],
      hourly: [],
    },
    token_usage_trends: {
      daily: [],
      hourly: [],
    },
  }
}

function makeModelStatistic(modelLabel: string, requestCount: number): UsageModelStatistic {
  return {
    avg_output_rate_tps: null,
    cached_tokens: 0,
    failed_count: 0,
    input_tokens: 5,
    model_id: modelLabel,
    model_label: modelLabel,
    output_tokens: 5,
    p50_ttft_ms: null,
    p95_ttft_ms: null,
    priced_request_count: requestCount,
    reasoning_tokens: 0,
    request_count: requestCount,
    success_count: requestCount,
    success_rate: 100,
    total_cost_micros: 100,
    total_tokens: 10,
    unpriced_request_count: 0,
  }
}
