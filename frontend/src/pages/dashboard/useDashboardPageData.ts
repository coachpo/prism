import { useEffect, useMemo } from "react";
import type {
  DashboardRecentActivityItem,
  DashboardRecentActivityResponse,
  DashboardSnapshot,
  LoadbalanceIncidentListResponse,
  SpendingTopModel,
  StatGroup,
} from "@/lib/types";
import { useDashboardBootstrapData } from "./useDashboardBootstrapData";
import { useDashboardPolling } from "./useDashboardPolling";

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
  incidents: LoadbalanceIncidentListResponse | null;
  recentActivity: DashboardRecentActivityResponse | null;
  recentActivityItems: DashboardRecentActivityItem[];
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

function buildModelDisplayNames(
  snapshot: DashboardSnapshot | null,
  recentActivityItems: DashboardRecentActivityItem[],
) {
  const displayNames = new Map<string, string>();

  for (const activity of recentActivityItems) {
    displayNames.set(activity.model_id, activity.model_label || activity.model_id);
    if (activity.resolved_target_model_id) {
      displayNames.set(
        activity.resolved_target_model_id,
        activity.resolved_target_model_label || activity.resolved_target_model_id,
      );
    }
  }

  if (!snapshot) {
    return displayNames;
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
  incidents: LoadbalanceIncidentListResponse | null,
  recentActivity: DashboardRecentActivityResponse | null,
  recentActivityItems: DashboardRecentActivityItem[],
): DashboardOverviewData {
  return {
    apiFamilyRows: [...(snapshot?.api_family_rows ?? [])].sort(
      (left, right) => right.total_requests - left.total_requests,
    ),
    metricSnapshot: toDashboardMetricSnapshot(snapshot),
    modelDisplayNames: buildModelDisplayNames(snapshot, recentActivityItems),
    incidents,
    recentActivity,
    recentActivityItems,
    topSpendingModels: snapshot?.top_spending_models ?? [],
  };
}

export function useDashboardPageData({
  revision,
  selectedProfileId,
}: UseDashboardPageDataInput) {
  const {
    dashboardRecentActivity,
    dashboardRecentActivityItems,
    dashboardIncidents,
    dashboardSnapshot,
    fetchDashboardData,
    loading,
  } = useDashboardBootstrapData({
    revision,
    selectedProfileId,
  });
  const {
    clearRecentRequestHighlight,
    isRefreshing,
    metricsHighlighted,
    recentNewIds,
    refreshDashboard,
  } = useDashboardPolling({
    fetchDashboardData,
    selectedProfileId,
  });

  useEffect(() => {
    void fetchDashboardData({ reuseInFlight: true });
  }, [fetchDashboardData]);

  const overviewData = useMemo<DashboardOverviewData>(() => {
    return toDashboardOverviewData(
      dashboardSnapshot,
      dashboardIncidents,
      dashboardRecentActivity,
      dashboardRecentActivityItems,
    );
  }, [
    dashboardRecentActivity,
    dashboardRecentActivityItems,
    dashboardIncidents,
    dashboardSnapshot,
  ]);

  return {
    clearRecentRequestHighlight,
    isRefreshing,
    loading,
    metricsHighlighted,
    overviewData,
    recentNewIds,
    refreshDashboard,
  };
}
