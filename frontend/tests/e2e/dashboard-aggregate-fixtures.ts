import type {
  DashboardMetricSnapshot,
  DashboardRecentActivityItem,
  DashboardRecentActivityResponse,
  DashboardRoutingHealthMap,
  DashboardSnapshot,
  SpendingTopModel,
  StatGroup,
} from "../../src/lib/types";

export const dashboardAggregateTimestamp = "2026-04-11T00:00:00Z";

type DashboardSnapshotOptions = {
  apiFamilyRows?: StatGroup[];
  metricSnapshot?: Partial<DashboardMetricSnapshot>;
  routingHealthMap?: DashboardRoutingHealthMap;
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

export function createDashboardRecentActivityItem(
  overrides: Partial<DashboardRecentActivityItem> = {},
): DashboardRecentActivityItem {
  return {
    request_log_id: 301,
    created_at: dashboardAggregateTimestamp,
    ingress_model_id: "model-a",
    ingress_model_label: "Model A",
    attempt_target_model_id: null,
    attempt_target_model_label: null,
    endpoint_id: 201,
    endpoint_label: "Endpoint A",
    status_code: 200,
    response_time_ms: 640,
    ttft_ms: 80,
    completion_duration_ms: 240,
    is_stream: false,
    stream_outcome: "not_streaming",
    total_tokens: 120,
    total_cost_user_currency_micros: 250000,
    pricing_status: "priced",
    unpriced_reason: null,
    report_currency_symbol: "$",
    ...overrides,
  };
}

export function createDashboardRecentActivityResponse(
  items: DashboardRecentActivityItem[] = [createDashboardRecentActivityItem()],
): DashboardRecentActivityResponse {
  return {
    generated_at: dashboardAggregateTimestamp,
    activity_watermark: {
      latest_request_log_created_at: items[0]?.created_at ?? null,
      latest_request_log_id: items[0]?.request_log_id ?? null,
    },
    items,
  };
}

export function createDashboardSnapshot(
  options: DashboardSnapshotOptions = {},
): DashboardSnapshot {
  return {
    generated_at: dashboardAggregateTimestamp,
    snapshot_revision: "01J2DASHBOARD000000000000",
    source_watermark: {
      latest_usage_event_created_at: dashboardAggregateTimestamp,
      latest_usage_event_id: 42,
    },
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
    top_spending_models: options.topSpendingModels ?? [
      {
        model_id: "model-a",
        model_label: "Model A Spend Label",
        known_cost_micros: 250000,
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
    routingHealthMap: createEmptyRoutingHealthMap(),
    topSpendingModels: [],
  });
}
