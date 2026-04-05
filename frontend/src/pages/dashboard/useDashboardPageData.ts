import { useEffect, useMemo, useRef } from "react";
import type {
  SpendingTopModel,
  StatGroup,
} from "@/lib/types";
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
  streamShare: number;
  successRate: number;
  totalCost: number;
  totalModels: number;
  totalRequests: number;
}

export interface DashboardStrategyFamilySummary {
  adaptiveCount: number;
  legacyCount: number;
  unassignedCount: number;
}

export function useDashboardPageData({
  revision,
  selectedProfileId,
}: UseDashboardPageDataInput) {
  const latestDashboardRequestIdRef = useRef(0);
  const {
    fetchDashboardData,
    loading,
    models,
    apiFamilyStats,
    recentRequests,
    routingDiagramData,
    routingDiagramError,
    routingDiagramLoading,
    setApiFamilyStats,
    setRecentRequests,
    setRoutingDiagramData,
    setRoutingDiagramError,
    setSpending,
    setStats,
    setThroughput,
    spending,
    stats,
    throughput,
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
    latestDashboardRequestIdRef,
    selectedProfileId,
    setApiFamilyStats,
    setRecentRequests,
    setRoutingDiagramData,
    setRoutingDiagramError,
    setSpending,
    setStats,
    setThroughput,
  });

  const modelDisplayNames = useMemo(() => {
    return new Map(models.map((model) => [model.model_id, model.display_name || model.model_id]));
  }, [models]);

  useEffect(() => {
    void fetchDashboardData({ reuseInFlight: true });
  }, [fetchDashboardData]);

  const metricSnapshot = useMemo<DashboardMetricSnapshot>(() => {
    const activeModels = models.filter((model) => model.is_enabled).length;
    const totalRequests = stats?.total_requests ?? 0;
    const successRate = stats?.success_rate ?? 0;
    const avgLatency = stats?.avg_response_time_ms ?? 0;
    const p95Latency = stats?.p95_response_time_ms ?? 0;
    const totalCost = spending?.summary.total_cost_micros ?? 0;
    const streamShare =
      recentRequests.length === 0
        ? 0
        : (recentRequests.filter((request) => request.is_stream).length / recentRequests.length) * 100;

    return {
      activeModels,
      averageRpm: throughput?.average_rpm ?? 0,
      averageRpmRequestTotal: throughput?.total_requests ?? 0,
      avgLatency,
      errorRate: Math.max(0, 100 - successRate),
      p95Latency,
      streamShare,
      successRate,
      totalCost,
      totalModels: models.length,
      totalRequests,
    };
  }, [models, recentRequests, spending, stats, throughput]);

  const apiFamilyRows = useMemo<StatGroup[]>(() => {
    return [...(apiFamilyStats?.groups ?? [])].sort((left, right) => right.total_requests - left.total_requests);
  }, [apiFamilyStats]);

  const topSpendingModels = useMemo<SpendingTopModel[]>(() => {
    return spending?.top_spending_models ?? [];
  }, [spending]);

  const strategyFamilySummary = useMemo<DashboardStrategyFamilySummary>(() => {
    return models.reduce(
      (summary, model) => {
        if (!model.loadbalance_strategy) {
          summary.unassignedCount += 1;
          return summary;
        }

        if (model.loadbalance_strategy.strategy_type === "adaptive") {
          summary.adaptiveCount += 1;
        } else {
          summary.legacyCount += 1;
        }

        return summary;
      },
      {
        adaptiveCount: 0,
        legacyCount: 0,
        unassignedCount: 0,
      },
    );
  }, [models]);

  return {
    clearRecentRequestHighlight,
    connectionState,
    isRefreshing,
    isSyncing,
    loading,
    metricSnapshot,
    metricsHighlighted,
    modelDisplayNames,
    apiFamilyRows,
    recentNewIds,
    recentRequests,
    refreshDashboard,
    routingDiagramData,
    routingDiagramError,
    routingDiagramLoading,
    strategyFamilySummary,
    topSpendingModels,
  };
}
