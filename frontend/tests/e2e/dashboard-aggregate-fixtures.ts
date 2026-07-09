import type {
  DashboardMetricSnapshot,
  DashboardRecentActivityItem,
  DashboardRecentActivityResponse,
  DashboardRoutingHealthMap,
  DashboardSnapshot,
  DashboardTopologyGraph,
  SpendingTopModel,
  StatGroup,
  UsageSnapshotResponse,
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

export function createDashboardModel(modelId: string, displayName: string, id: number) {
  return {
    id,
    api_family: "openai",
    model_id: modelId,
    display_name: displayName,
    model_type: "proxy",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: dashboardAggregateTimestamp,
    updated_at: dashboardAggregateTimestamp,
  };
}

type UsageSnapshotOptions = {
  empty?: boolean;
  requestBreakdowns?: boolean;
};

export function createUsageSnapshot(
  options: UsageSnapshotOptions = {},
): UsageSnapshotResponse {
  const empty = options.empty ?? false;
  const modelId = "gpt-5.4";
  const modelLabel = "Primary canonical model";
  const snapshot: UsageSnapshotResponse = {
    generated_at: "2026-04-12T00:00:00Z",
    time_range: {
      preset: "30d",
      start_at: "2026-03-13T00:00:00Z",
      end_at: "2026-04-12T00:00:00Z",
    },
    currency: { code: "USD", symbol: "$" },
    overview: {
      total_requests: empty ? 0 : 6,
      success_requests: empty ? 0 : 5,
      failed_requests: empty ? 0 : 1,
      success_rate: empty ? 0 : 83.3,
      total_tokens: empty ? 0 : 2400,
      input_tokens: empty ? 0 : 1400,
      output_tokens: empty ? 0 : 900,
      cached_tokens: empty ? 0 : 50,
      reasoning_tokens: empty ? 0 : 50,
      average_rpm: empty ? 0 : 0.2,
      average_tpm: empty ? 0 : 80,
      total_cost_micros: empty ? 0 : 620000,
      rolling_window_minutes: 30,
      rolling_request_count: empty ? 0 : 2,
      rolling_token_count: empty ? 0 : 800,
      rolling_rpm: empty ? 0 : 0.07,
      rolling_tpm: empty ? 0 : 26.67,
    },
    request_trends: {
      hourly: buildSeries(
        modelId,
        modelLabel,
        empty,
        [
          ["2026-04-09T00:00:00Z", 2, 2],
          ["2026-04-09T01:00:00Z", 4, 3],
        ],
        (bucket_start, request_count, success_count) => ({
          bucket_start,
          request_count,
          success_count,
          failed_count: request_count - success_count,
          rpm: request_count,
        }),
      ),
      daily: buildSeries(
        modelId,
        modelLabel,
        empty,
        [
          ["2026-04-08T00:00:00Z", 2, 2],
          ["2026-04-09T00:00:00Z", 4, 3],
        ],
        (bucket_start, request_count, success_count) => ({
          bucket_start,
          request_count,
          success_count,
          failed_count: request_count - success_count,
          rpm: Number((request_count / 24).toFixed(2)),
        }),
      ),
    },
    latency_trends: {
      hourly: buildSeries(
        modelId,
        modelLabel,
        empty,
        [
          ["2026-04-09T00:00:00Z", 180, 320],
          ["2026-04-09T01:00:00Z", 240, 480],
        ],
        (bucket_start, p50_ms, p95_ms) => ({ bucket_start, p50_ms, p95_ms }),
        "All models",
      ),
      daily: buildSeries(
        modelId,
        modelLabel,
        empty,
        [
          ["2026-04-08T00:00:00Z", 180, 320],
          ["2026-04-09T00:00:00Z", 240, 480],
        ],
        (bucket_start, p50_ms, p95_ms) => ({ bucket_start, p50_ms, p95_ms }),
        "All models",
      ),
    },
    token_usage_trends: {
      hourly: buildSeries(
        modelId,
        modelLabel,
        empty,
        [
          ["2026-04-09T00:00:00Z", 900, 500, 350, 25, 25, 900],
          ["2026-04-09T01:00:00Z", 1500, 900, 550, 25, 25, 1500],
        ],
        (bucket_start, total_tokens, input_tokens, output_tokens, cached_tokens, reasoning_tokens, tpm) => ({
          bucket_start,
          total_tokens,
          input_tokens,
          output_tokens,
          cached_tokens,
          reasoning_tokens,
          tpm,
        }),
        "All models",
        (points) => points.reduce((sum, point) => sum + point.total_tokens, 0),
      ),
      daily: buildSeries(
        modelId,
        modelLabel,
        empty,
        [
          ["2026-04-08T00:00:00Z", 900, 500, 350, 25, 25, 37.5],
          ["2026-04-09T00:00:00Z", 1500, 900, 550, 25, 25, 62.5],
        ],
        (bucket_start, total_tokens, input_tokens, output_tokens, cached_tokens, reasoning_tokens, tpm) => ({
          bucket_start,
          total_tokens,
          input_tokens,
          output_tokens,
          cached_tokens,
          reasoning_tokens,
          tpm,
        }),
        "All models",
        (points) => points.reduce((sum, point) => sum + point.total_tokens, 0),
      ),
    },
    token_type_breakdown: {
      hourly: empty
        ? []
        : [
            {
              bucket_start: "2026-04-09T00:00:00Z",
              input_tokens: 500,
              output_tokens: 350,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
            {
              bucket_start: "2026-04-09T01:00:00Z",
              input_tokens: 900,
              output_tokens: 550,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
          ],
      daily: empty
        ? []
        : [
            {
              bucket_start: "2026-04-08T00:00:00Z",
              input_tokens: 500,
              output_tokens: 350,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
            {
              bucket_start: "2026-04-09T00:00:00Z",
              input_tokens: 900,
              output_tokens: 550,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
          ],
    },
    cost_overview: {
      total_cost_micros: empty ? 0 : 620000,
      priced_request_count: empty ? 0 : 2,
      unpriced_request_count: 0,
      hourly: empty
        ? []
        : [
            { bucket_start: "2026-04-09T00:00:00Z", total_cost_micros: 170000 },
            { bucket_start: "2026-04-09T01:00:00Z", total_cost_micros: 450000 },
          ],
      daily: empty
        ? []
        : [
            { bucket_start: "2026-04-08T00:00:00Z", total_cost_micros: 170000 },
            { bucket_start: "2026-04-09T00:00:00Z", total_cost_micros: 450000 },
          ],
    },
    endpoint_statistics: empty
      ? []
      : [
          {
            endpoint_id: 10,
            endpoint_label: "Primary canonical endpoint",
            p50_ttft_ms: 120,
            p95_ttft_ms: 220,
            request_count: 6,
            success_rate: 83.3,
            total_tokens: 2400,
            avg_output_rate_tps: 81.63,
            total_cost_micros: 620000,
          },
        ],
    model_statistics: [
      {
        model_id: modelId,
        model_label: modelLabel,
        p50_ttft_ms: empty ? null : 120,
        p95_ttft_ms: empty ? null : 220,
        success_count: empty ? 0 : 5,
        failed_count: empty ? 0 : 1,
        priced_request_count: empty ? 0 : 2,
        unpriced_request_count: 0,
        request_count: empty ? 0 : 6,
        success_rate: empty ? 0 : 83.3,
        input_tokens: empty ? 0 : 1400,
        output_tokens: empty ? 0 : 900,
        cached_tokens: empty ? 0 : 50,
        reasoning_tokens: empty ? 0 : 50,
        total_tokens: empty ? 0 : 2400,
        avg_output_rate_tps: empty ? null : 81.63,
        total_cost_micros: empty ? 0 : 620000,
      },
    ],
    proxy_api_key_statistics: [],
  };

  if (options.requestBreakdowns) {
    snapshot.endpoint_statistics = [
      {
        endpoint_id: 2,
        endpoint_label: "Sub-CPA-B",
        p50_ttft_ms: 120,
        p95_ttft_ms: 220,
        request_count: 4,
        success_rate: 100,
        total_tokens: 1600,
        avg_output_rate_tps: 81.63,
        total_cost_micros: 420000,
      },
      {
        endpoint_id: 3,
        endpoint_label: "DeepSeek",
        p50_ttft_ms: 180,
        p95_ttft_ms: 280,
        request_count: 2,
        success_rate: 50,
        total_tokens: 800,
        avg_output_rate_tps: 42.5,
        total_cost_micros: 200000,
      },
    ];
    snapshot.model_statistics = [
      { ...snapshot.model_statistics[0], request_count: 4 },
      {
        ...snapshot.model_statistics[0],
        model_id: "claude-3.7-sonnet",
        model_label: "Secondary global-only model",
        request_count: 2,
        total_cost_micros: 200000,
      },
    ];
  }

  return snapshot;
}

function buildSeries<TPoint extends { bucket_start: string }>(
  modelId: string,
  modelLabel: string,
  empty: boolean,
  rows: ReadonlyArray<readonly unknown[]>,
  mapPoint: (...values: unknown[]) => TPoint,
  allLabel = "All requests",
  totalFromPoints?: (points: TPoint[]) => number,
) {
  const points = empty ? [] : rows.map((row) => mapPoint(...row));
  const total = empty
    ? 0
    : totalFromPoints
      ? totalFromPoints(points)
      : points.reduce(
          (sum, point) =>
            sum + ("request_count" in point && typeof point.request_count === "number" ? point.request_count : 0),
          0,
        );

  return [
    {
      key: "all",
      label: allLabel,
      total_requests: total,
      total_tokens: total,
      points,
    },
    {
      key: modelId,
      label: modelLabel,
      total_requests: total,
      total_tokens: total,
      points,
    },
  ];
}
