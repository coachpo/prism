import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRealtimeData } from "@/hooks/useRealtimeData";
import type { DashboardRealtimeUpdatePayload, RequestLogEntry } from "@/lib/types";
import type { RealtimeMessage } from "@/lib/websocket";

type MockHandler = (message: RealtimeMessage) => void;

const handlers = new Set<MockHandler>();
const channelCounts = new Map<string, number>();

let mockConnected = true;
let mockConnectionState: "connected" | "reconnecting" | "connecting" | "disconnected" =
  "connected";

const mockClient = {
  connect: vi.fn(),
  subscribeChannel: vi.fn((profileId: number, channel: string) => {
    if (profileId <= 0) {
      return;
    }

    channelCounts.set(channel, (channelCounts.get(channel) ?? 0) + 1);
  }),
  unsubscribeChannel: vi.fn((channel: string) => {
    const nextCount = Math.max(0, (channelCounts.get(channel) ?? 0) - 1);
    if (nextCount === 0) {
      channelCounts.delete(channel);
      return;
    }

    channelCounts.set(channel, nextCount);
  }),
  on: vi.fn((handler: MockHandler) => {
    handlers.add(handler);
    return () => handlers.delete(handler);
  }),
  isConnected: vi.fn(() => mockConnected),
  getConnectionState: vi.fn(() => mockConnectionState),
  hasChannelSubscription: vi.fn((channel: string, profileId: number | null) => {
    return profileId !== null && (channelCounts.get(channel) ?? 0) > 0;
  }),
};

vi.mock("@/lib/websocket", async () => {
  const actual = await vi.importActual<typeof import("@/lib/websocket")>(
    "@/lib/websocket"
  );

  return {
    ...actual,
    getWebSocketClient: () => mockClient,
  };
});

function emit(message: RealtimeMessage) {
  act(() => {
    handlers.forEach((handler) => {
      handler(message);
    });
  });
}

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

function makeDashboardUpdatePayload(
  overrides: Partial<DashboardRealtimeUpdatePayload> = {}
): DashboardRealtimeUpdatePayload {
  return {
    request_log: makeRequestLog(),
    stats_summary_24h: {
      total_requests: 24,
      success_count: 23,
      error_count: 1,
      success_rate: 95.83,
      avg_response_time_ms: 140,
      p95_response_time_ms: 240,
      total_input_tokens: 120,
      total_output_tokens: 180,
      total_tokens: 300,
      groups: [],
    },
    api_family_summary_24h: {
      total_requests: 24,
      success_count: 23,
      error_count: 1,
      success_rate: 95.83,
      avg_response_time_ms: 140,
      p95_response_time_ms: 240,
      total_input_tokens: 120,
      total_output_tokens: 180,
      total_tokens: 300,
      groups: [],
    },
    spending_summary_30d: {
      summary: {
        total_cost_micros: 250,
        successful_request_count: 23,
        priced_request_count: 23,
        unpriced_request_count: 0,
        total_input_tokens: 120,
        total_output_tokens: 180,
        total_cache_read_input_tokens: 0,
        total_cache_creation_input_tokens: 0,
        total_reasoning_tokens: 0,
        total_tokens: 300,
        avg_cost_per_successful_request_micros: 10,
      },
      groups: [],
      groups_total: 0,
      top_spending_models: [{ model_id: "gpt-4o-mini", total_cost_micros: 250 }],
      top_spending_endpoints: [],
      unpriced_breakdown: {},
      report_currency_code: "USD",
      report_currency_symbol: "$",
    },
    throughput_24h: {
      average_rpm: 1,
      peak_rpm: 2,
      current_rpm: 1,
      total_requests: 24,
      time_window_seconds: 3600,
      buckets: [{ timestamp: "2026-03-13T12:00:00Z", request_count: 1, rpm: 1 }],
    },
    routing_route_24h: {
      model_id: "gpt-4o-mini",
      model_config_id: 14,
      model_label: "GPT-4o Mini",
      endpoint_id: 2,
      endpoint_label: "Primary endpoint",
      active_connection_count: 1,
      traffic_request_count_24h: 12,
      request_count_24h: 12,
      success_count_24h: 11,
      error_count_24h: 1,
      success_rate_24h: 91.67,
    },
    ...overrides,
  };
}

describe("useRealtimeData", () => {
  beforeEach(() => {
    mockConnected = true;
    mockConnectionState = "connected";
    handlers.clear();
    channelCounts.clear();
    vi.clearAllMocks();
    vi.useFakeTimers();
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it("fires onData for pushed payload messages", () => {
    const onData = vi.fn();
    const payload = makeDashboardUpdatePayload();

    const { result } = renderHook(() =>
      useRealtimeData({
        profileId: 1,
        channel: "dashboard",
        onData,
      })
    );

    emit({ type: "dashboard.update", ...payload });

    expect(onData).toHaveBeenCalledWith(payload);
    expect(result.current.lastData).toEqual(payload);
  });

  it("fires onReconnect for synthetic reconnect messages", () => {
    const onReconnect = vi.fn();
    const { result } = renderHook(() =>
      useRealtimeData({
        profileId: 1,
        channel: "dashboard",
        onReconnect,
      })
    );

    emit({ type: "reconnected" });

    expect(onReconnect).toHaveBeenCalledTimes(1);
    expect(result.current.isSyncing).toBe(true);
  });

  it("buffers pushed payloads during sync until markSyncComplete is called", () => {
    const onData = vi.fn();
    const payload = makeDashboardUpdatePayload({
      request_log: makeRequestLog({ id: 202 }),
    });

    const { result } = renderHook(() =>
      useRealtimeData({
        profileId: 1,
        channel: "dashboard",
        onData,
        onReconnect: vi.fn(),
      })
    );

    emit({ type: "reconnected" });
    emit({ type: "dashboard.update", ...payload });

    expect(onData).not.toHaveBeenCalled();

    act(() => {
      result.current.markSyncComplete();
    });

    expect(onData).toHaveBeenCalledWith(payload);
    expect(result.current.isSyncing).toBe(false);
  });

  it("does not enter syncing mode on reconnect when no reconcile callback is provided", () => {
    const onData = vi.fn();
    const payload = makeDashboardUpdatePayload({
      request_log: makeRequestLog({ id: 303 }),
    });

    const { result } = renderHook(() =>
      useRealtimeData({
        profileId: 1,
        channel: "dashboard",
        onData,
      })
    );

    emit({ type: "reconnected" });

    expect(result.current.isSyncing).toBe(false);

    emit({ type: "dashboard.update", ...payload });

    expect(onData).toHaveBeenCalledWith(payload);
  });

  it("keeps a shared channel subscribed while another hook instance remains mounted", () => {
    const firstHook = renderHook(() =>
      useRealtimeData({
        profileId: 1,
        channel: "dashboard",
      })
    );
    const secondHook = renderHook(() =>
      useRealtimeData({
        profileId: 1,
        channel: "dashboard",
      })
    );

    expect(channelCounts.get("dashboard")).toBe(2);

    firstHook.unmount();

    act(() => {
      vi.advanceTimersByTime(600);
    });

    expect(channelCounts.get("dashboard")).toBe(1);
    expect(secondHook.result.current.isSubscribed).toBe(true);
  });
});
