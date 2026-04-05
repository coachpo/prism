import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  DashboardRealtimeUpdatePayload,
  RequestLogEntry,
  SpendingReportResponse,
  StatsSummary,
  ThroughputStatsResponse,
} from "@/lib/types";
import { useDashboardRealtime } from "../useDashboardRealtime";
import type { RoutingDiagramData } from "../routingDiagram";

let capturedOptions: Record<string, unknown> | null = null;

vi.mock("@/hooks/useRealtimeData", () => ({
  useRealtimeData: (options: Record<string, unknown>) => {
    capturedOptions = options;

    return {
      connectionState: "connected",
      isConnected: true,
      isSubscribed: true,
      isSyncing: false,
      lastData: null,
      lastMessage: null,
      markSyncComplete: vi.fn(),
    };
  },
}));

function makeRequestLog(overrides: Partial<RequestLogEntry> = {}): RequestLogEntry {
  const request: RequestLogEntry = {
    id: 101,
    model_id: "gpt-4o-mini",
    resolved_target_model_id: null,
    profile_id: 1,
    api_family: "openai",
    endpoint_id: 2,
    connection_id: 3,
    ingress_request_id: null,
    attempt_number: null,
    provider_correlation_id: null,
    endpoint_base_url: "https://api.openai.com",
    endpoint_description: "Primary endpoint",
    status_code: 200,
    response_time_ms: 120,
    is_stream: false,
    input_tokens: 10,
    output_tokens: 15,
    total_tokens: 25,
    success_flag: true,
    billable_flag: true,
    priced_flag: true,
    unpriced_reason: null,
    cache_read_input_tokens: null,
    cache_creation_input_tokens: null,
    reasoning_tokens: null,
    input_cost_micros: 10,
    output_cost_micros: 15,
    cache_read_input_cost_micros: null,
    cache_creation_input_cost_micros: null,
    reasoning_cost_micros: null,
    total_cost_original_micros: 25,
    total_cost_user_currency_micros: 25,
    currency_code_original: "USD",
    report_currency_code: "USD",
    report_currency_symbol: "$",
    fx_rate_used: null,
    fx_rate_source: null,
    pricing_snapshot_unit: null,
    pricing_snapshot_input: null,
    pricing_snapshot_output: null,
    pricing_snapshot_cache_read_input: null,
    pricing_snapshot_cache_creation_input: null,
    pricing_snapshot_reasoning: null,
    pricing_snapshot_missing_special_token_price_policy: null,
    pricing_config_version_used: 1,
    request_path: "/v1/chat/completions",
    error_detail: null,
    created_at: "2026-03-13T12:00:00Z",
    ...overrides,
  };

  return {
    ...request,
    resolved_target_model_id: request.resolved_target_model_id ?? null,
    ingress_request_id: request.ingress_request_id ?? null,
    attempt_number: request.attempt_number ?? null,
    provider_correlation_id: request.provider_correlation_id ?? null,
  };
}

function makeUpdatePayload(): DashboardRealtimeUpdatePayload {
  return {
    request_log: makeRequestLog({ id: 202 }),
    stats_summary_24h: {
      total_requests: 40,
      success_count: 38,
      error_count: 2,
      success_rate: 95,
      avg_response_time_ms: 180,
      p95_response_time_ms: 400,
      total_input_tokens: 100,
      total_output_tokens: 200,
      total_tokens: 300,
      groups: [],
    },
    api_family_summary_24h: {
      total_requests: 40,
      success_count: 38,
      error_count: 2,
      success_rate: 95,
      avg_response_time_ms: 180,
      p95_response_time_ms: 400,
      total_input_tokens: 100,
      total_output_tokens: 200,
      total_tokens: 300,
      groups: [
        {
          key: "openai",
          total_requests: 40,
          success_count: 38,
          error_count: 2,
          avg_response_time_ms: 180,
          total_tokens: 300,
        },
      ],
    },
    spending_summary_30d: {
      summary: {
        total_cost_micros: 900,
        successful_request_count: 38,
        priced_request_count: 38,
        unpriced_request_count: 0,
        total_input_tokens: 100,
        total_output_tokens: 200,
        total_cache_read_input_tokens: 0,
        total_cache_creation_input_tokens: 0,
        total_reasoning_tokens: 0,
        total_tokens: 300,
        avg_cost_per_successful_request_micros: 24,
      },
      groups: [],
      groups_total: 0,
      top_spending_models: [{ model_id: "gpt-4o-mini", total_cost_micros: 900 }],
      top_spending_endpoints: [],
      unpriced_breakdown: {},
      report_currency_code: "USD",
      report_currency_symbol: "$",
    },
    throughput_24h: {
      average_rpm: 1.667,
      peak_rpm: 5,
      current_rpm: 2,
      total_requests: 40,
      time_window_seconds: 1440,
      buckets: [{ timestamp: "2026-03-13T12:00:00Z", request_count: 2, rpm: 2 }],
    },
    routing_route_24h: {
      model_id: "gpt-4o-mini",
      model_config_id: 12,
      model_label: "GPT-4o Mini",
      endpoint_id: 2,
      endpoint_label: "Primary endpoint",
      active_connection_count: 1,
      traffic_request_count_24h: 18,
      request_count_24h: 20,
      success_count_24h: 19,
      error_count_24h: 1,
      success_rate_24h: 95,
    },
  };
}

describe("useDashboardRealtime", () => {
  beforeEach(() => {
    capturedOptions = null;
    vi.clearAllMocks();
    vi.useFakeTimers();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("applies websocket snapshots across dashboard state", () => {
    const payload = makeUpdatePayload();
    const latestDashboardRequestIdRef = { current: 100 };

    let recentRequests = [makeRequestLog({ id: 100 })];
    let stats: StatsSummary | null = null;
    let apiFamilyStats: StatsSummary | null = null;
    let spending: SpendingReportResponse | null = null;
    let throughput: ThroughputStatsResponse | null = null;
    let routingDiagramData: RoutingDiagramData | null = null;
    let routingDiagramError = "stale error";

    const setRecentRequests = vi.fn((value) => {
      recentRequests = typeof value === "function" ? value(recentRequests) : value;
    });
    const setStats = vi.fn((value) => {
      stats = typeof value === "function" ? value(stats) : value;
    });
    const setApiFamilyStats = vi.fn((value) => {
      apiFamilyStats = typeof value === "function" ? value(apiFamilyStats) : value;
    });
    const setSpending = vi.fn((value) => {
      spending = typeof value === "function" ? value(spending) : value;
    });
    const setThroughput = vi.fn((value) => {
      throughput = typeof value === "function" ? value(throughput) : value;
    });
    const setRoutingDiagramData = vi.fn((value) => {
      routingDiagramData =
        typeof value === "function" ? value(routingDiagramData) : value;
    });
    const setRoutingDiagramError = vi.fn((value) => {
      routingDiagramError = typeof value === "function" ? value(routingDiagramError) : value;
    });

    const { result } = renderHook(() =>
      useDashboardRealtime({
        fetchDashboardData: vi.fn(),
        latestDashboardRequestIdRef,
        selectedProfileId: 1,
        setApiFamilyStats,
        setRecentRequests,
        setRoutingDiagramData,
        setRoutingDiagramError,
        setSpending,
        setStats,
        setThroughput,
      })
    );

    act(() => {
      (capturedOptions?.onData as (payload: DashboardRealtimeUpdatePayload) => void)(payload);
    });

    expect(latestDashboardRequestIdRef.current).toBe(202);
    expect(recentRequests.map((request) => request.id)).toEqual([202, 100]);
    expect(stats).toEqual(payload.stats_summary_24h);
    expect(apiFamilyStats).toEqual(payload.api_family_summary_24h);
    expect(spending).toEqual(payload.spending_summary_30d);
    expect(throughput).toEqual(payload.throughput_24h);
    expect(routingDiagramError).toBeNull();
    const appliedRoutingDiagramData: RoutingDiagramData =
      routingDiagramData ?? {
        nodes: [],
        links: [],
        endpointCount: 0,
        modelCount: 0,
        activeConnectionTotal: 0,
        trafficRequestTotal24h: 0,
      };

    expect(routingDiagramData).not.toBeNull();
    expect(appliedRoutingDiagramData.links).toHaveLength(1);
    expect(appliedRoutingDiagramData.links[0]?.requestCount24h).toBe(20);
    expect(appliedRoutingDiagramData.trafficRequestTotal24h).toBe(18);
    expect(result.current.metricsHighlighted).toBe(true);
    expect(result.current.recentNewIds.has(202)).toBe(true);
  });

  it("refreshes dashboard data silently for the manual reload action", async () => {
    const fetchDashboardData = vi.fn().mockResolvedValue(undefined);

    const { result } = renderHook(() =>
      useDashboardRealtime({
        fetchDashboardData,
        latestDashboardRequestIdRef: { current: 0 },
        selectedProfileId: 1,
        setApiFamilyStats: vi.fn(),
        setRecentRequests: vi.fn(),
        setRoutingDiagramData: vi.fn(),
        setRoutingDiagramError: vi.fn(),
        setSpending: vi.fn(),
        setStats: vi.fn(),
        setThroughput: vi.fn(),
      })
    );

    await act(async () => {
      await result.current.refreshDashboard();
    });

    expect(fetchDashboardData).toHaveBeenCalledWith({
      forceRefresh: true,
      silent: true,
    });
    expect(result.current.isRefreshing).toBe(false);
    expect(result.current.metricsHighlighted).toBe(true);
  });
});
