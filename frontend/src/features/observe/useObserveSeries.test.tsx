import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  ObserveCoverage,
  DashboardNowResponse,
  QueryContextResponse,
  UsageSummaryResponse,
  UsageSeriesResponse,
} from "@/lib/api/observability";
import type {
  ObserveMetric,
  ObserveScope,
} from "@/features/observe/observeSearch";
import {
  useObserveAnalysisContext,
  useObserveFragments,
} from "./useObserveFragments";
import { useUsageSeriesFragment } from "./useObserveSeries";

const mocks = vi.hoisted(() => ({
  queryContext: vi.fn(),
  usageSummary: vi.fn(),
  usageSeries: vi.fn(),
  dashboardNow: vi.fn(),
}));

vi.mock("@/lib/api/observability", () => ({
  observe: {
    queryContext: mocks.queryContext,
    usageSummary: mocks.usageSummary,
    usageSeries: mocks.usageSeries,
    dashboardNow: mocks.dashboardNow,
  },
}));

vi.mock("@/i18n/staticMessages", () => ({
  getStaticMessages: () => ({
    observe: { queryContextUnavailable: "query context unavailable" },
  }),
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

function coverage(): ObserveCoverage {
  return {
    requested_preset: "24h",
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-09T00:00:00Z",
    retention_from_time: null,
    source: "raw",
    complete: true,
    gaps: [],
  };
}

function contextResponse(
  scope: ObserveScope,
  queryContext: string,
): QueryContextResponse {
  const bounds = {
    from_time: "2026-08-08T00:00:00Z",
    to_time: "2026-08-09T00:00:00Z",
  };
  return {
    query_context: queryContext,
    scope,
    requested_bounds: bounds,
    usage_bounds: bounds,
    usage_coverage: coverage(),
    event_bounds: bounds,
    event_coverage: coverage(),
    request_bounds: bounds,
    request_coverage: coverage(),
    generated_at: bounds.to_time,
    caliber: {
      scope,
      grain: scope,
      identity_basis: scope,
      outcome_basis: scope,
      latency_basis: scope,
      cost_basis: scope === "route_attempt" ? "none" : "trusted",
      datasets: scope === "route_attempt" ? ["request_logs"] : ["usage_request_events"],
    },
  };
}

function seriesResponse(
  scope: ObserveScope,
  metric: ObserveMetric,
): UsageSeriesResponse {
  return {
    generated_at: "2026-08-09T00:00:00Z",
    coverage: coverage(),
    metric,
    group_by: "none",
    selection_basis: "request_count",
    interval: "1h",
    series_limit: 6,
    truncated: false,
    caliber: contextResponse(scope, "series-fixture").caliber,
    dataset_coverage:
      scope === "route_attempt"
        ? { request_logs: coverage() }
        : { usage_request_events: coverage() },
    samples: {
      observation_count: 0,
      latency_sample_count: 0,
      latency_missing_count: 0,
      cost_sample_count: 0,
      cost_missing_count: 0,
    },
    series: [],
  };
}

describe("Observe scope-bound series", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("withdraws the old context synchronously and never sends it with the new scope metric", async () => {
    const routeContext = deferred<QueryContextResponse>();
    mocks.queryContext.mockImplementation(
      ({ scope }: { scope: ObserveScope }) =>
        scope === "ingress"
          ? Promise.resolve(contextResponse("ingress", "token-ingress"))
          : routeContext.promise,
    );
    mocks.usageSeries.mockImplementation(
      (_token: string, params: { metric: ObserveMetric }) =>
        Promise.resolve(
          seriesResponse(
            params.metric === "attempts" ? "route_attempt" : "ingress",
            params.metric,
          ),
        ),
    );

    const { result, rerender } = renderHook(
      ({ metric, scope }: { metric: ObserveMetric; scope: ObserveScope }) => {
        const context = useObserveAnalysisContext("24h", scope);
        const series = useUsageSeriesFragment(
          context.phase === "ready"
            ? (context.data?.query_context ?? null)
            : null,
          { metric, groupBy: "none", interval: "auto", scope },
          context.phase,
        );
        return { context, series };
      },
      { initialProps: { metric: "requests", scope: "ingress" } },
    );

    await waitFor(() => expect(result.current.series.phase).toBe("ready"));
    expect(mocks.usageSeries).toHaveBeenCalledWith(
      "token-ingress",
      { metric: "requests", group_by: "none", interval: "auto" },
      expect.any(AbortSignal),
    );

    rerender({ metric: "attempts", scope: "route_attempt" });
    expect(result.current.context.phase).toBe("loading");
    expect(result.current.context.data).toBeNull();
    expect(result.current.series.phase).toBe("loading");
    expect(result.current.series.data).toBeNull();
    expect(mocks.usageSeries).toHaveBeenCalledTimes(1);

    await act(async () => {
      routeContext.resolve(contextResponse("route_attempt", "token-attempt"));
      await routeContext.promise;
    });
    await waitFor(() => expect(result.current.series.phase).toBe("ready"));
    expect(mocks.usageSeries).toHaveBeenLastCalledWith(
      "token-attempt",
      { metric: "attempts", group_by: "none", interval: "auto" },
      expect.any(AbortSignal),
    );
  });

  it("rejects a series whose returned scope does not match its request key", async () => {
    mocks.usageSeries.mockResolvedValue(
      seriesResponse("ingress", "attempts"),
    );
    const { result } = renderHook(() =>
      useUsageSeriesFragment(
        "token-attempt",
        {
          metric: "attempts",
          groupBy: "none",
          interval: "auto",
          scope: "route_attempt",
        },
        "ready",
      ),
    );

    await waitFor(() => expect(result.current.phase).toBe("error"));
    expect(result.current.data).toBeNull();
  });

  it("withdraws ingress fragments synchronously when the preset changes", async () => {
    const nextContext = deferred<QueryContextResponse>();
    mocks.queryContext.mockImplementation(
      ({ preset }: { preset: string }) =>
        preset === "24h"
          ? Promise.resolve(contextResponse("ingress", "token-24h"))
          : nextContext.promise,
    );
    mocks.usageSummary.mockResolvedValue({} as UsageSummaryResponse);
    mocks.dashboardNow.mockResolvedValue({} as DashboardNowResponse);

    const { result, rerender } = renderHook(
      ({ preset }: { preset: string }) => useObserveFragments(preset),
      { initialProps: { preset: "24h" } },
    );
    await waitFor(() =>
      expect(result.current.queryContext.phase).toBe("ready"),
    );

    rerender({ preset: "7d" });
    expect(result.current.queryContext.phase).toBe("loading");
    expect(result.current.queryContext.data).toBeNull();
    expect(result.current.summary.phase).toBe("loading");
    expect(result.current.summary.data).toBeNull();
    expect(result.current.now.phase).toBe("loading");
    expect(result.current.now.data).toBeNull();
  });
});
