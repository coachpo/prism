import { useCallback, useEffect, useRef, useState } from "react";
import { useRealtimeData } from "@/hooks/useRealtimeData";
import type {
  DashboardRealtimeUpdatePayload,
  RequestLogEntry,
  RequestLogListItem,
  SpendingReportResponse,
  StatsSummary,
  ThroughputStatsResponse,
} from "@/lib/types";
import { applyRoutingDiagramRealtimeUpdate, type RoutingDiagramData } from "./routingDiagram";

function toRequestLogListItem(entry: RequestLogEntry): RequestLogListItem {
  return {
    id: entry.id,
    created_at: entry.created_at,
    model_id: entry.model_id,
    resolved_target_model_id: entry.resolved_target_model_id,
    api_family: entry.api_family ?? "openai",
    vendor_id: entry.vendor_id,
    vendor_key: entry.vendor_key,
    vendor_name: entry.vendor_name,
    endpoint_id: entry.endpoint_id,
    connection_id: entry.connection_id,
    status_code: entry.status_code,
    response_time_ms: entry.response_time_ms,
    is_stream: entry.is_stream,
    total_tokens: entry.total_tokens,
    total_cost_user_currency_micros: entry.total_cost_user_currency_micros,
    report_currency_symbol: entry.report_currency_symbol,
  };
}

type Params = {
  fetchDashboardData: (args?: { forceRefresh?: boolean; silent?: boolean }) => Promise<void>;
  latestDashboardRequestIdRef: React.MutableRefObject<number>;
  selectedProfileId: number | null;
  setApiFamilyStats: React.Dispatch<React.SetStateAction<StatsSummary | null>>;
  setRecentRequests: React.Dispatch<React.SetStateAction<RequestLogListItem[]>>;
  setRoutingDiagramData: React.Dispatch<React.SetStateAction<RoutingDiagramData | null>>;
  setRoutingDiagramError: React.Dispatch<React.SetStateAction<string | null>>;
  setSpending: React.Dispatch<React.SetStateAction<SpendingReportResponse | null>>;
  setStats: React.Dispatch<React.SetStateAction<StatsSummary | null>>;
  setThroughput: React.Dispatch<React.SetStateAction<ThroughputStatsResponse | null>>;
};

export function useDashboardRealtime({
  fetchDashboardData,
  latestDashboardRequestIdRef,
  selectedProfileId,
  setApiFamilyStats,
  setRecentRequests,
  setRoutingDiagramData,
  setRoutingDiagramError,
  setSpending,
  setStats,
  setThroughput,
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
      const summary = toRequestLogListItem(entry);

      if (entry.id <= latestDashboardRequestIdRef.current) {
        return;
      }

      latestDashboardRequestIdRef.current = entry.id;

      setRecentRequests((prev) => [summary, ...prev].slice(0, 12));
      setRecentNewIds((prev) => new Set(prev).add(entry.id));

      setStats(update.stats_summary_24h);
      setApiFamilyStats(update.api_family_summary_24h);
      setSpending(update.spending_summary_30d);
      setThroughput(update.throughput_24h);
      setRoutingDiagramError(null);
      setRoutingDiagramData((prev) =>
        applyRoutingDiagramRealtimeUpdate(prev, update.routing_route_24h)
      );

      triggerMetricHighlight();
    },
    [
      latestDashboardRequestIdRef,
      setApiFamilyStats,
      setRecentRequests,
      setRoutingDiagramData,
      setRoutingDiagramError,
      setSpending,
      setStats,
      setThroughput,
      triggerMetricHighlight,
    ]
  );

  const handleReconnect = useCallback(() => {
    queueMicrotask(() => {
      markSyncCompleteRef.current();
    });
  }, []);

  const refreshDashboard = useCallback(async () => {
    setIsRefreshing(true);

    try {
      await fetchDashboardData({ forceRefresh: true, silent: true });
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
