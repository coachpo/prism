import { useEffect, useMemo, useRef } from "react";
import type {
  DashboardSnapshot,
  RequestLogListItem,
  SpendingTopModel,
  StatGroup,
} from "@/lib/types";
import type { RoutingDiagramData } from "./routingDiagram";
import { useDashboardBootstrapData } from "./useDashboardBootstrapData";
import { useDashboardRealtime } from "./useDashboardRealtime";

interface UseDashboardPageDataInput {
  revision: number;
  selectedProfileId: number | null;
}

export interface DashboardMetricSnapshot {
  activeModels: number;
  averageRpm: number;
  averageRpmRequestTotal: number;
  avgLatency: number;
  errorRate: number;
  p95Latency: number;
  pricedRequestCount: number;
  streamShare: number;
  successRate: number;
  totalCost: number;
  totalModels: number;
  totalRequests: number;
  unpricedRequestCount: number;
}

export interface DashboardOverviewData {
  apiFamilyRows: StatGroup[];
  metricSnapshot: DashboardMetricSnapshot;
  modelDisplayNames: Map<string, string>;
  recentRequests: RequestLogListItem[];
  routingDiagramData: RoutingDiagramData | null;
  routingDiagramError: string | null;
  routingDiagramLoading: boolean;
  topSpendingModels: SpendingTopModel[];
}

const EMPTY_METRIC_SNAPSHOT: DashboardMetricSnapshot = {
  activeModels: 0,
  averageRpm: 0,
  averageRpmRequestTotal: 0,
  avgLatency: 0,
  errorRate: 0,
  p95Latency: 0,
  pricedRequestCount: 0,
  streamShare: 0,
  successRate: 0,
  totalCost: 0,
  totalModels: 0,
  totalRequests: 0,
  unpricedRequestCount: 0,
};

function toDashboardMetricSnapshot(snapshot: DashboardSnapshot | null): DashboardMetricSnapshot {
  if (!snapshot) {
    return EMPTY_METRIC_SNAPSHOT;
  }

  const metric = snapshot.metric_snapshot;
  return {
    activeModels: metric.active_models,
    averageRpm: metric.average_rpm,
    averageRpmRequestTotal: metric.average_rpm_request_total,
    avgLatency: metric.avg_latency,
    errorRate: metric.error_rate,
    p95Latency: metric.p95_latency,
    pricedRequestCount: metric.priced_request_count,
    streamShare: metric.stream_share,
    successRate: metric.success_rate,
    totalCost: metric.total_cost,
    totalModels: metric.total_models,
    totalRequests: metric.total_requests,
    unpricedRequestCount: metric.unpriced_request_count,
  };
}

function buildModelDisplayNames(snapshot: DashboardSnapshot | null) {
  const displayNames = new Map<string, string>();

  if (!snapshot) {
    return displayNames;
  }

  for (const request of snapshot.recent_requests) {
    displayNames.set(request.model_id, request.model_label || request.model_id);
    if (request.resolved_target_model_id) {
      displayNames.set(
        request.resolved_target_model_id,
        request.resolved_target_model_label || request.resolved_target_model_id,
      );
    }
  }

  for (const model of snapshot.top_spending_models) {
    if (!displayNames.has(model.model_id)) {
      displayNames.set(model.model_id, model.model_label || model.model_id);
    }
  }

  return displayNames;
}

function toDashboardOverviewData(
  snapshot: DashboardSnapshot | null,
  routingDiagramError: string | null,
  routingDiagramLoading: boolean,
): DashboardOverviewData {
  return {
    apiFamilyRows: [...(snapshot?.api_family_rows ?? [])].sort(
      (left, right) => right.total_requests - left.total_requests,
    ),
    metricSnapshot: toDashboardMetricSnapshot(snapshot),
    modelDisplayNames: buildModelDisplayNames(snapshot),
    recentRequests: snapshot?.recent_requests ?? [],
    routingDiagramData: snapshot?.routing_health_map ?? null,
    routingDiagramError,
    routingDiagramLoading,
    topSpendingModels: snapshot?.top_spending_models ?? [],
  };
}

export function useDashboardPageData({
  revision,
  selectedProfileId,
}: UseDashboardPageDataInput) {
  const latestDashboardRequestIdRef = useRef(0);
  const {
    dashboardSnapshot,
    fetchDashboardData,
    loading,
    reconcileDashboardSnapshot,
    routingDiagramError,
    routingDiagramLoading,
    setRoutingDiagramError,
  } = useDashboardBootstrapData({
    latestDashboardRequestIdRef,
    revision,
    selectedProfileId,
  });
  const {
    clearRecentRequestHighlight,
    connectionState,
    isRefreshing,
    isSyncing,
    metricsHighlighted,
    recentNewIds,
    refreshDashboard,
  } = useDashboardRealtime({
    fetchDashboardData,
    reconcileDashboardSnapshot,
    selectedProfileId,
    setRoutingDiagramError,
  });

  useEffect(() => {
    latestDashboardRequestIdRef.current = 0;
  }, [latestDashboardRequestIdRef, selectedProfileId]);

  useEffect(() => {
    void fetchDashboardData({ reuseInFlight: true });
  }, [fetchDashboardData]);

  const overviewData = useMemo<DashboardOverviewData>(() => {
    return toDashboardOverviewData(
      dashboardSnapshot,
      routingDiagramError,
      routingDiagramLoading,
    );
  }, [dashboardSnapshot, routingDiagramError, routingDiagramLoading]);

  return {
    clearRecentRequestHighlight,
    connectionState,
    isRefreshing,
    isSyncing,
    loading,
    metricsHighlighted,
    overviewData,
    recentNewIds,
    refreshDashboard,
  };
}
