import { useCallback, useEffect, useRef, useState } from "react";
import { useRealtimeData } from "@/hooks/useRealtimeData";
import type { DashboardRealtimeUpdatePayload } from "@/lib/types";
import type { DashboardSnapshotReconciler } from "./useDashboardBootstrapData";

type Params = {
  fetchDashboardData: (args?: { silent?: boolean }) => Promise<void>;
  reconcileDashboardSnapshot: DashboardSnapshotReconciler;
  selectedProfileId: number | null;
  setRoutingDiagramError: React.Dispatch<React.SetStateAction<string | null>>;
};

export function useDashboardRealtime({
  fetchDashboardData,
  reconcileDashboardSnapshot,
  selectedProfileId,
  setRoutingDiagramError,
}: Params) {
  const [recentNewIds, setRecentNewIds] = useState<Set<number>>(() => new Set());
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [metricsHighlighted, setMetricsHighlighted] = useState(false);
  const markSyncCompleteRef = useRef<() => void>(() => undefined);
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

  const applyDashboardUpdate = useCallback(
    (update: DashboardRealtimeUpdatePayload) => {
      const entry = update.request_log;

      const didApply = reconcileDashboardSnapshot(update.snapshot, {
        requestId: entry.id,
      });

      if (!didApply) {
        return;
      }

      setRecentNewIds((prev) => new Set(prev).add(entry.id));
      setRoutingDiagramError(null);
      triggerMetricHighlight();
    },
    [reconcileDashboardSnapshot, setRoutingDiagramError, triggerMetricHighlight]
  );

  const handleReconnect = useCallback(() => {
    void fetchDashboardData({ silent: true }).finally(() => {
      markSyncCompleteRef.current();
    });
  }, [fetchDashboardData]);

  const refreshDashboard = useCallback(async () => {
    setIsRefreshing(true);

    try {
      await fetchDashboardData({ silent: true });
      triggerMetricHighlight();
    } finally {
      setIsRefreshing(false);
    }
  }, [fetchDashboardData, triggerMetricHighlight]);

  const { connectionState, isSyncing, markSyncComplete } = useRealtimeData({
    profileId: selectedProfileId,
    channel: "dashboard",
    onData: applyDashboardUpdate,
    onReconnect: handleReconnect,
  });

  useEffect(() => {
    markSyncCompleteRef.current = markSyncComplete;
  }, [markSyncComplete]);

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
    connectionState,
    isRefreshing,
    isSyncing,
    metricsHighlighted,
    recentNewIds,
    refreshDashboard,
  };
}
