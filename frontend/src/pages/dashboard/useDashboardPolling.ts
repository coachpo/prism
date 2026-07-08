import { useCallback, useEffect, useRef, useState } from "react";
import type { DashboardBootstrapFetchResult } from "./useDashboardBootstrapData";

const DASHBOARD_POLL_INTERVAL_MS = 30_000;

type Params = {
  fetchDashboardData: (args?: { silent?: boolean }) => Promise<DashboardBootstrapFetchResult>;
  selectedProfileId: number | null;
};

export function useDashboardPolling({
  fetchDashboardData,
  selectedProfileId,
}: Params) {
  const [recentNewIds, setRecentNewIds] = useState<Set<number>>(() => new Set());
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [metricsHighlighted, setMetricsHighlighted] = useState(false);
  const metricHighlightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const triggerMetricHighlight = useCallback(() => {
    setMetricsHighlighted(true);
    if (metricHighlightTimerRef.current) {
      clearTimeout(metricHighlightTimerRef.current);
    }

    metricHighlightTimerRef.current = setTimeout(() => {
      setMetricsHighlighted(false);
    }, 1500);
  }, []);

  const applyFetchResult = useCallback(
    (result: DashboardBootstrapFetchResult) => {
      if (result.newRecentActivityIds.length > 0) {
        setRecentNewIds((prev) => {
          const next = new Set(prev);
          for (const requestId of result.newRecentActivityIds) {
            next.add(requestId);
          }
          return next;
        });
      }
      if (result.snapshotApplied) {
        triggerMetricHighlight();
      }
    },
    [triggerMetricHighlight],
  );

  const refreshDashboard = useCallback(async () => {
    setIsRefreshing(true);

    try {
      const result = await fetchDashboardData({ silent: true });
      applyFetchResult(result);
    } finally {
      setIsRefreshing(false);
    }
  }, [applyFetchResult, fetchDashboardData]);

  useEffect(() => {
    if (selectedProfileId === null) {
      return undefined;
    }

    const pollDashboard = () => {
      void fetchDashboardData({ silent: true }).then(applyFetchResult);
    };
    const intervalID = window.setInterval(pollDashboard, DASHBOARD_POLL_INTERVAL_MS);
    return () => {
      window.clearInterval(intervalID);
    };
  }, [applyFetchResult, fetchDashboardData, selectedProfileId]);

  useEffect(() => {
    return () => {
      if (metricHighlightTimerRef.current) {
        clearTimeout(metricHighlightTimerRef.current);
      }
    };
  }, []);

  const clearRecentRequestHighlight = useCallback((requestId: number) => {
    setRecentNewIds((prev) => {
      if (!prev.has(requestId)) {
        return prev;
      }

      const next = new Set(prev);
      next.delete(requestId);
      return next;
    });
  }, []);

  return {
    clearRecentRequestHighlight,
    isRefreshing,
    metricsHighlighted,
    recentNewIds,
    refreshDashboard,
  };
}
