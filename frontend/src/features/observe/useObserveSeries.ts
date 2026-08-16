import { useEffect, useState } from "react";

import { observe, type UsageSeriesResponse } from "@/lib/api/observability";
import type { ObserveGroupBy, ObserveMetric } from "@/features/observe/observeSearch";
import { fragmentErrorFrom, type FragmentState } from "@/features/observe/useObserveFragments";

export interface MainChartState {
  metric: ObserveMetric;
  groupBy: ObserveGroupBy;
  interval: string;
}

export function useUsageSeriesFragment(
  queryContext: string | null,
  state: MainChartState,
  queryContextPhase: "loading" | "ready" | "error" = "ready",
): FragmentState<UsageSeriesResponse> {
  const { metric, groupBy, interval } = state;
  const [fragment, setFragment] = useState<FragmentState<UsageSeriesResponse>>({ phase: "loading", data: null, stale: false, error: null, retryAfterMs: null });
  useEffect(() => {
    if (!queryContext) {
      let active = true
      queueMicrotask(() => {
        if (!active) return
        if (queryContextPhase === "error") {
          setFragment({ phase: "error", data: null, stale: false, error: "查询上下文不可用", retryAfterMs: null })
        }
      })
      return () => { active = false }
    }
    let cancelled = false;
    void observe
      .usageSeries(queryContext, { metric, group_by: groupBy, interval })
      .then((series) => {
        if (!cancelled) setFragment({ phase: "ready", data: series, stale: false, error: null, retryAfterMs: null });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          const mapped = fragmentErrorFrom(err);
          setFragment({ phase: "error", data: null, stale: false, error: mapped.error, retryAfterMs: mapped.retryAfterMs });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [queryContext, queryContextPhase, metric, groupBy, interval]);
  return fragment;
}

