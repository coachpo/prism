import { useEffect, useState } from "react";

import { observe, type UsageSeriesResponse } from "@/lib/api/observability";
import type { ObserveGroupBy, ObserveMetric } from "@/features/observe/observeSearch";
import { fragmentErrorFrom, type FragmentState } from "@/features/observe/useObserveFragments";
import { getStaticMessages } from "@/i18n/staticMessages";

export interface MainChartState {
  metric: ObserveMetric;
  groupBy: ObserveGroupBy;
  interval: string;
  scope?: string;
}

export function useUsageSeriesFragment(
  queryContext: string | null,
  state: MainChartState,
  queryContextPhase: "loading" | "ready" | "error" = "ready",
): FragmentState<UsageSeriesResponse> {
  const { metric, groupBy, interval, scope } = state;
  const [fragment, setFragment] = useState<FragmentState<UsageSeriesResponse>>({ phase: "loading", data: null, stale: false, error: null, retryAfterMs: null });
  useEffect(() => {
    if (!queryContext) {
      let active = true
      queueMicrotask(() => {
        if (!active) return
        if (queryContextPhase === "error") {
          setFragment({ phase: "error", data: null, stale: false, error: getStaticMessages().observe.queryContextUnavailable, retryAfterMs: null })
        } else {
          setFragment({ phase: "loading", data: null, stale: false, error: null, retryAfterMs: null })
        }
      })
      return () => { active = false }
    }
    // Aborting, not just ignoring: the chart is re-requested on every metric
    // and grouping change, and each abandoned read would otherwise keep its
    // server admission slot until it finished.
    const controller = new AbortController();
    void observe
      .usageSeries(queryContext, { metric, group_by: groupBy, interval }, controller.signal)
      .then((series) => {
        if (controller.signal.aborted) return;
        if (
          (scope && series.caliber.scope !== scope) ||
          series.metric !== metric
        ) {
          setFragment({
            phase: "error",
            data: null,
            stale: false,
            error: getStaticMessages().observe.queryContextUnavailable,
            retryAfterMs: null,
          });
          return;
        }
        setFragment({ phase: "ready", data: series, stale: false, error: null, retryAfterMs: null });
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          const mapped = fragmentErrorFrom(err);
          setFragment({ phase: "error", data: null, stale: false, error: mapped.error, retryAfterMs: mapped.retryAfterMs });
        }
      });
    return () => {
      controller.abort();
    };
  }, [queryContext, queryContextPhase, metric, groupBy, interval, scope]);
  return fragment;
}
