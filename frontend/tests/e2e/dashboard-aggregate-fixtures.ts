import type {
  DashboardMetricSnapshot,
  DashboardRoutingHealthMap,
  DashboardSnapshot,
  DashboardStrategyFamilySummary,
  RequestLogListItem,
  SpendingTopModel,
  StatGroup,
} from "../../src/lib/types";

export const dashboardAggregateTimestamp = "2026-04-11T00:00:00Z";

export const legacyOverviewFanOutPaths = [
  "/api/stats/summary",
  "/api/stats/spending",
  "/api/stats/throughput",
  "/api/stats/requests",
  "/api/stats/connection-success-rates",
  "/api/models/connections/batch",
];

type DashboardSnapshotOptions = {
  apiFamilyRows?: StatGroup[];
  metricSnapshot?: Partial<DashboardMetricSnapshot>;
  recentRequests?: RequestLogListItem[];
  routingHealthMap?: DashboardRoutingHealthMap;
  strategyFamilySummary?: Partial<DashboardStrategyFamilySummary>;
  topSpendingModels?: SpendingTopModel[];
};
export function createRoutingHealthMap(): DashboardRoutingHealthMap {
  return {
    nodes: [
      {
        id: "endpoint-201",
        name: "Endpoint A",
        kind: "endpoint",
        label: "Endpoint A",
        sublabel: "https://endpoint-a.example",
        endpointId: 201,
        modelId: null,
        modelConfigId: null,
        activeConnectionCount: 1,
        trafficRequestCount24h: 42,
        requestCount24h: 42,
        successCount24h: 41,
        errorCount24h: 1,
        successRate24h: 97.6,
      },
      {
        id: "model-101",
        name: "Model A",
        kind: "model",
        label: "Model A",
        sublabel: "model-a",
        endpointId: null,
        modelId: "model-a",
        modelConfigId: 101,
        activeConnectionCount: 1,
        trafficRequestCount24h: 42,
        requestCount24h: 42,
        successCount24h: 41,
        errorCount24h: 1,
        successRate24h: 97.6,
      },
    ],
    links: [
      {
        id: "endpoint-201:model-101",
        sourceNodeId: "endpoint-201",
        targetNodeId: "model-101",
        modelId: "model-a",
        modelLabel: "Model A",
        modelConfigId: 101,
        endpointId: 201,
        endpointLabel: "Endpoint A",
        activeConnectionCount: 1,
        trafficRequestCount24h: 42,
        requestCount24h: 42,
        successCount24h: 41,
        errorCount24h: 1,
        successRate24h: 97.6,
      },
    ],
    endpointCount: 1,
    modelCount: 1,
    activeConnectionTotal: 1,
    trafficRequestTotal24h: 42,
  };
}

export function createEmptyRoutingHealthMap(): DashboardRoutingHealthMap {
  return {
    nodes: [],
    links: [],
    endpointCount: 0,
    modelCount: 0,
    activeConnectionTotal: 0,
    trafficRequestTotal24h: 0,
  };
}
export function createDashboardSnapshot(
  options: DashboardSnapshotOptions = {},
): DashboardSnapshot {
  return {
    generated_at: dashboardAggregateTimestamp,
    coverage_24h: {
      from: "2026-04-10T00:00:00Z",
      to: dashboardAggregateTimestamp,
    },
    coverage_30d: {
      from: "2026-03-12T00:00:00Z",
      to: dashboardAggregateTimestamp,
    },
    health: {
      lag_seconds: 0,
      stale: false,
      stale_after_seconds: 300,
    },
    metric_snapshot: {
      active_models: 1,
      average_rpm: 1.2,
      average_rpm_request_total: 42,
      avg_latency: 123,
      error_rate: 2.4,
      p95_latency: 180,
      priced_request_count: 41,
      stream_share: 0,
      success_rate: 97.6,
      total_cost: 250000,
      total_models: 1,
      total_requests: 42,
      unpriced_request_count: 0,
      ...options.metricSnapshot,
    },
    api_family_rows: options.apiFamilyRows ?? [],
    strategy_family_summary: {
      adaptive_count: 0,
      legacy_count: 0,
      unassigned_count: 0,
      ...options.strategyFamilySummary,
    },
    recent_requests: options.recentRequests ?? [],
    top_spending_models: options.topSpendingModels ?? [
      {
        model_id: "model-a",
        total_cost_micros: 250000,
      },
    ],
    routing_health_map: options.routingHealthMap ?? createRoutingHealthMap(),
  };
}
export function createEmptyDashboardSnapshot(): DashboardSnapshot {
  return createDashboardSnapshot({
    metricSnapshot: {
      active_models: 0,
      average_rpm: 0,
      average_rpm_request_total: 0,
      avg_latency: 0,
      error_rate: 0,
      p95_latency: 0,
      priced_request_count: 0,
      stream_share: 0,
      success_rate: 0,
      total_cost: 0,
      total_models: 0,
      total_requests: 0,
      unpriced_request_count: 0,
    },
    recentRequests: [],
    routingHealthMap: createEmptyRoutingHealthMap(),
    strategyFamilySummary: {
      adaptive_count: 0,
      legacy_count: 0,
      unassigned_count: 0,
    },
    topSpendingModels: [],
  });
}
