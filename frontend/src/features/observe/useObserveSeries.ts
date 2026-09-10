import { useObserveReadCycle } from "./observeReadCycleContext";
import { useCallback, useEffect, useState } from "react";

import { observe, type UsageSeriesResponse } from "@/lib/api/observability";
import type {
  ObserveGroupBy,
  ObserveMetric,
} from "@/features/observe/observeSearch";
import {
  fragmentErrorFrom,
  type FragmentState,
} from "@/features/observe/useObserveFragments";
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
  basisKey?: string,
): FragmentState<UsageSeriesResponse> & { refresh: () => void } {
  const { track } = useObserveReadCycle();
  const { metric, groupBy, interval, scope } = state;
  // A failed read owns its own retry: the only recovery used to be the page's
  // freshness bar, a screen above the error it was meant to clear.
  const [reloadToken, setReloadToken] = useState(0);
  const key = `${basisKey ?? queryContext ?? ""}:${metric}:${groupBy}:${interval}:${scope ?? ""}`;
  const requestKey = `${queryContext}:${queryContextPhase}:${reloadToken}`;
  const [snapshot, setSnapshot] = useState<{
    key: string;
    requestKey: string;
    fragment: FragmentState<UsageSeriesResponse>;
  }>(() => ({ key, requestKey, fragment: loadingFragment() }));

  // React effects run after render. Bind the visible state to the complete
  // request key so a scope/metric transition cannot render the previous
  // response even for that one pre-effect frame.
  const fragment =
    snapshot.key === key
      ? snapshot.requestKey === requestKey ? snapshot.fragment : { ...snapshot.fragment, phase: "loading" as const }
      : queryContextPhase === "error" && !queryContext
        ? queryContextErrorFragment()
        : loadingFragment();
  useEffect(() => {
    if (!queryContext || queryContextPhase !== "ready") {
      return;
    }
    // Aborting, not just ignoring: the chart is re-requested on every metric
    // and grouping change, and each abandoned read would otherwise keep its
    // server admission slot until it finished.
    const controller = new AbortController();
    void track(observe
      .usageSeries(
        queryContext,
        { metric, group_by: groupBy, interval },
        controller.signal,
      ))
      .then((series) => {
        if (controller.signal.aborted) return;
        // A response belongs to exactly one scope/metric request key. Missing
        // or mismatched server attribution is an invalid fragment, not data
        // that may be painted under the current controls.
        if (scope && series.caliber?.scope !== scope) {
          setSnapshot({ key, requestKey, fragment: queryContextErrorFragment() });
          return;
        }
        if (series.metric !== metric) {
          setSnapshot({ key, requestKey, fragment: queryContextErrorFragment() });
          return;
        }
        setSnapshot({
          key,
          requestKey,
          fragment: {
            phase: "ready",
            data: series,
            stale: false,
            error: null,
            retryAfterMs: null,
          },
        });
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          const mapped = fragmentErrorFrom(err);
          setSnapshot(previous => {
            const data = previous.key === key ? previous.fragment.data : null;
            return { key, requestKey, fragment: { phase: "error", data, stale: data !== null, error: mapped.error, retryAfterMs: mapped.retryAfterMs } };
          });
        }
      });
    return () => {
      controller.abort();
    };
  }, [
    queryContext,
    queryContextPhase,
    metric,
    groupBy,
    interval,
    scope,
    key,
    reloadToken,
    requestKey,
    track,
  ]);
  const refresh = useCallback(() => setReloadToken((value) => value + 1), []);
  if (queryContextPhase === "error" && !fragment.data) return { ...queryContextErrorFragment(), refresh };
  if (queryContextPhase === "error" && fragment.data) {
    return { ...fragment, phase: "error", stale: true, error: getStaticMessages().observe.queryContextUnavailable, refresh };
  }
  return { ...fragment, refresh };
}

function loadingFragment(): FragmentState<UsageSeriesResponse> {
  return {
    phase: "loading",
    data: null,
    stale: false,
    error: null,
    retryAfterMs: null,
  };
}

function queryContextErrorFragment(): FragmentState<UsageSeriesResponse> {
  return {
    phase: "error",
    data: null,
    stale: false,
    error: getStaticMessages().observe.queryContextUnavailable,
    retryAfterMs: null,
  };
}
