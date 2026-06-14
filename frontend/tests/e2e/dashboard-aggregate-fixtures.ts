import type {
  DashboardMetricSnapshot,
  DashboardRecentActivityItem,
  DashboardRecentActivityResponse,
  DashboardRoutingHealthMap,
  DashboardSnapshot,
  DashboardTopologyGraph,
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
  routingHealthMap?: DashboardRoutingHealthMap;
  topologyGraph?: DashboardTopologyGraph;
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

export function createTopologyGraph(): DashboardTopologyGraph {
  return {
    nodes: [
      {
        id: "model-101",
        kind: "model",
        label: "Model A",
        sublabel: "model-a",
        status: "enabled",
        model_config_id: 101,
        model_id: "model-a",
      },
      {
        id: "model-102",
        kind: "model",
        label: "Disabled Model",
        sublabel: "disabled-model",
        status: "disabled",
        model_config_id: 102,
        model_id: "disabled-model",
      },
      {
        id: "terminal-target-501",
        kind: "connection",
        product_kind: "terminal_target",
        label: "Primary Target",
        sublabel: "Endpoint A",
        status: "active",
        terminal_target_id: 501,
        connection_id: 501,
        active: true,
        health_status: "healthy",
        recent_request_count: 42,
        recent_success_rate: 97.6,
        last_request_at: dashboardAggregateTimestamp,
      },
      {
        id: "terminal-target-502",
        kind: "connection",
        product_kind: "terminal_target",
        label: "Backup Target",
        sublabel: "Endpoint A",
        status: "inactive",
        terminal_target_id: 502,
        connection_id: 502,
        active: false,
        health_status: "unknown",
        recent_request_count: 0,
        recent_success_rate: null,
        last_request_at: null,
      },
      {
        id: "endpoint-201",
        kind: "endpoint",
        label: "Endpoint A",
        sublabel: "Endpoint 201",
        status: "configured",
        endpoint_id: 201,
      },
    ],
    edges: [
      {
        id: "access-target-1001",
        kind: "model_to_model",
        source_node_id: "model-101",
        target_node_id: "model-102",
        position: 0,
        enabled: true,
        source_model_config_id: 101,
        source_model_id: "model-a",
        target_model_config_id: 102,
        target_model_id: "disabled-model",
      },
      {
        id: "access-target-1002",
        kind: "model_to_connection",
        product_kind: "model_to_terminal_target",
        source_node_id: "model-101",
        target_node_id: "terminal-target-501",
        position: 1,
        enabled: true,
        source_model_config_id: 101,
        source_model_id: "model-a",
        terminal_target_id: 501,
        connection_id: 501,
      },
      {
        id: "access-target-1003",
        kind: "model_to_connection",
        product_kind: "model_to_terminal_target",
        source_node_id: "model-101",
        target_node_id: "terminal-target-502",
        position: 2,
        enabled: true,
        source_model_config_id: 101,
        source_model_id: "model-a",
        terminal_target_id: 502,
        connection_id: 502,
      },
      {
        id: "terminal-target-binding-501",
        kind: "connection_to_endpoint",
        product_kind: "terminal_target_to_endpoint",
        source_node_id: "terminal-target-501",
        target_node_id: "endpoint-201",
        terminal_target_id: 501,
        connection_id: 501,
        endpoint_id: 201,
      },
      {
        id: "terminal-target-binding-502",
        kind: "connection_to_endpoint",
        product_kind: "terminal_target_to_endpoint",
        source_node_id: "terminal-target-502",
        target_node_id: "endpoint-201",
        terminal_target_id: 502,
        connection_id: 502,
        endpoint_id: 201,
      },
    ],
    stats: {
      model_count: 2,
      active_model_count: 1,
      disabled_model_count: 1,
      terminal_target_count: 2,
      active_terminal_target_count: 1,
      inactive_terminal_target_count: 1,
      endpoint_count: 1,
      edge_count: 5,
    },
  };
}

export function createEmptyTopologyGraph(): DashboardTopologyGraph {
  return {
    nodes: [],
    edges: [],
    stats: {
      model_count: 0,
      active_model_count: 0,
      disabled_model_count: 0,
      terminal_target_count: 0,
      active_terminal_target_count: 0,
      inactive_terminal_target_count: 0,
      endpoint_count: 0,
      edge_count: 0,
    },
  };
}

export function createDashboardRecentActivityItem(
  overrides: Partial<DashboardRecentActivityItem> = {},
): DashboardRecentActivityItem {
  return {
    request_log_id: 301,
    created_at: dashboardAggregateTimestamp,
    model_id: "model-a",
    model_label: "Model A",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
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
    priced_flag: true,
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
        total_cost_micros: 250000,
      },
    ],
    routing_health_map: options.routingHealthMap ?? createRoutingHealthMap(),
    topology_graph: options.topologyGraph ?? createTopologyGraph(),
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
    topologyGraph: createEmptyTopologyGraph(),
    topSpendingModels: [],
  });
}
