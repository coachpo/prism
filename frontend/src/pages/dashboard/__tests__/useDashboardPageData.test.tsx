import { StrictMode, type ReactNode } from "react";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getDefaultAutoRecovery } from "@/lib/loadbalanceRoutingPolicy";
import { clearSharedReferenceData } from "@/lib/referenceData";
import { useDashboardPageData } from "../useDashboardPageData";

const api = vi.hoisted(() => ({
  connections: {
    byModels: vi.fn(),
  },
  models: {
    list: vi.fn(),
  },
  stats: {
    connectionSuccessRates: vi.fn(),
    requests: vi.fn(),
    spending: vi.fn(),
    summary: vi.fn(),
    throughput: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("../useDashboardRealtime", () => ({
  useDashboardRealtime: () => ({
    clearRecentRequestHighlight: vi.fn(),
    connectionState: "connected",
    isSyncing: false,
    metricsHighlighted: false,
    recentNewIds: new Set<number>(),
  }),
}));

function StrictWrapper({ children }: { children: ReactNode }) {
  return <StrictMode>{children}</StrictMode>;
}

describe("useDashboardPageData", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearSharedReferenceData();

    api.models.list.mockResolvedValue([
      {
        id: 10,
        vendor_id: 1,
        vendor: {
          id: 1,
          key: "openai",
          name: "OpenAI",
          description: null,
          audit_enabled: false,
          audit_capture_bodies: true,
          created_at: "",
          updated_at: "",
        },
        api_family: "openai",
        model_id: "gpt-5.4",
        display_name: "GPT-5.4",
        model_type: "native",
        loadbalance_strategy_id: 100,
        loadbalance_strategy: {
          id: 100,
          name: "single-primary",
          strategy_type: "legacy",
          legacy_strategy_type: "single",
          auto_recovery: getDefaultAutoRecovery("single"),
        },
        is_enabled: true,
        connection_count: 1,
        active_connection_count: 1,
        health_success_rate: 99.5,
        health_total_requests: 12,
        created_at: "",
        updated_at: "",
      },
      {
        id: 11,
        vendor_id: 2,
        vendor: {
          id: 2,
          key: "anthropic",
          name: "Anthropic",
          description: null,
          audit_enabled: false,
          audit_capture_bodies: true,
          created_at: "",
          updated_at: "",
        },
        api_family: "anthropic",
        model_id: "claude-sonnet-4-6",
        display_name: "Claude Sonnet 4.6",
        model_type: "native",
        loadbalance_strategy_id: 101,
        loadbalance_strategy: {
          id: 101,
          name: "round-robin-primary",
          strategy_type: "adaptive",
          routing_policy: {
            kind: "adaptive",
            routing_objective: "maximize_availability",
          },
        },
        is_enabled: true,
        connection_count: 2,
        active_connection_count: 1,
        health_success_rate: 98,
        health_total_requests: 8,
        created_at: "",
        updated_at: "",
      },
    ]);

    api.stats.connectionSuccessRates.mockResolvedValue([
      {
        connection_id: 101,
        total_requests: 6,
        success_count: 6,
        error_count: 0,
        success_rate: 100,
      },
      {
        connection_id: 102,
        total_requests: 4,
        success_count: 3,
        error_count: 1,
        success_rate: 75,
      },
    ]);

    api.stats.summary
      .mockResolvedValueOnce({
        total_requests: 24,
        success_count: 19,
        error_count: 1,
        success_rate: 95,
        avg_response_time_ms: 320,
        p95_response_time_ms: 880,
        total_input_tokens: 100,
        total_output_tokens: 200,
        total_tokens: 300,
        groups: [],
      })
      .mockResolvedValueOnce({
        total_requests: 24,
        success_count: 19,
        error_count: 1,
        success_rate: 95,
        avg_response_time_ms: 320,
        p95_response_time_ms: 880,
        total_input_tokens: 100,
        total_output_tokens: 200,
        total_tokens: 300,
        groups: [
          {
            key: "openai",
            total_requests: 12,
            success_count: 12,
            error_count: 0,
            avg_response_time_ms: 250,
            total_tokens: 180,
          },
        ],
      });

    api.stats.spending
      .mockResolvedValueOnce({
        summary: {
          total_cost_micros: 123456,
          successful_request_count: 19,
          priced_request_count: 19,
          unpriced_request_count: 0,
          total_input_tokens: 100,
          total_output_tokens: 200,
          total_cache_read_input_tokens: 0,
          total_cache_creation_input_tokens: 0,
          total_reasoning_tokens: 0,
          total_tokens: 300,
          avg_cost_per_successful_request_micros: 6497,
        },
        groups: [],
        groups_total: 0,
        top_spending_models: [{ model_id: "gpt-5.4", total_cost_micros: 123456 }],
        top_spending_endpoints: [],
        unpriced_breakdown: {},
        report_currency_code: "USD",
        report_currency_symbol: "$",
      })
      .mockResolvedValueOnce({
        summary: {
          total_cost_micros: 123456,
          successful_request_count: 19,
          priced_request_count: 19,
          unpriced_request_count: 0,
          total_input_tokens: 100,
          total_output_tokens: 200,
          total_cache_read_input_tokens: 0,
          total_cache_creation_input_tokens: 0,
          total_reasoning_tokens: 0,
          total_tokens: 300,
          avg_cost_per_successful_request_micros: 6497,
        },
        groups: [
          {
            key: "gpt-5.4#500",
            total_cost_micros: 100000,
            total_requests: 6,
            priced_requests: 6,
            unpriced_requests: 0,
            total_tokens: 100,
          },
          {
            key: "claude-sonnet-4-6#501",
            total_cost_micros: 23456,
            total_requests: 4,
            priced_requests: 4,
            unpriced_requests: 0,
            total_tokens: 60,
          },
        ],
        groups_total: 2,
        top_spending_models: [],
        top_spending_endpoints: [],
        unpriced_breakdown: {},
        report_currency_code: "USD",
        report_currency_symbol: "$",
      });

    api.stats.throughput.mockResolvedValue({
      average_rpm: 0.833,
      peak_rpm: 2,
      current_rpm: 1,
      total_requests: 20,
      time_window_seconds: 1440,
      buckets: [
        {
          timestamp: "2026-03-16T10:00:00+00:00",
          request_count: 1,
          rpm: 1,
        },
      ],
    });

    api.stats.requests.mockResolvedValue({
      items: [
        {
          id: 9001,
          model_id: "gpt-5.4",
          profile_id: 1,
          api_family: "openai",
          endpoint_id: 500,
          connection_id: 101,
          endpoint_base_url: "https://api.openai.com",
          endpoint_description: "Primary",
          status_code: 200,
          response_time_ms: 220,
          is_stream: false,
          input_tokens: null,
          output_tokens: null,
          total_tokens: null,
          success_flag: true,
          billable_flag: true,
          priced_flag: true,
          unpriced_reason: null,
          cache_read_input_tokens: null,
          cache_creation_input_tokens: null,
          reasoning_tokens: null,
          input_cost_micros: null,
          output_cost_micros: null,
          cache_read_input_cost_micros: null,
          cache_creation_input_cost_micros: null,
          reasoning_cost_micros: null,
          total_cost_original_micros: null,
          total_cost_user_currency_micros: null,
          currency_code_original: null,
          report_currency_code: null,
          report_currency_symbol: null,
          fx_rate_used: null,
          fx_rate_source: null,
          pricing_snapshot_unit: null,
          pricing_snapshot_input: null,
          pricing_snapshot_output: null,
          pricing_snapshot_cache_read_input: null,
          pricing_snapshot_cache_creation_input: null,
          pricing_snapshot_reasoning: null,
          pricing_snapshot_missing_special_token_price_policy: null,
          pricing_config_version_used: null,
          request_path: "/v1/chat/completions",
          error_detail: null,
          created_at: "",
        },
      ],
      total: 1,
      limit: 12,
      offset: 0,
    });

    api.connections.byModels.mockResolvedValue({
      items: [
        {
          model_config_id: 10,
          connections: [
            {
              id: 101,
              model_config_id: 10,
              endpoint_id: 500,
              endpoint: {
                id: 500,
                profile_id: 1,
                name: "Primary",
                base_url: "https://api.openai.com",
                has_api_key: true,
                masked_api_key: "****",
                position: 0,
                created_at: "",
                updated_at: "",
              },
              is_active: true,
              priority: 0,
              name: null,
              auth_type: null,
              custom_headers: null,
              pricing_template_id: null,
              pricing_template: null,
              health_status: "healthy",
              health_detail: null,
              last_health_check: null,
              created_at: "",
              updated_at: "",
            },
          ],
        },
        {
          model_config_id: 11,
          connections: [
            {
              id: 102,
              model_config_id: 11,
              endpoint_id: 501,
              endpoint: {
                id: 501,
                profile_id: 1,
                name: "Fallback",
                base_url: "https://api.anthropic.com/v1",
                has_api_key: true,
                masked_api_key: "****",
                position: 1,
                created_at: "",
                updated_at: "",
              },
              is_active: true,
              priority: 0,
              name: null,
              auth_type: null,
              custom_headers: null,
              pricing_template_id: null,
              pricing_template: null,
              health_status: "healthy",
              health_detail: null,
              last_health_check: null,
              created_at: "",
              updated_at: "",
            },
          ],
        },
      ],
    });
  });

  afterEach(() => {
    clearSharedReferenceData();
    cleanup();
  });

  it("deduplicates StrictMode bootstrap fetches and batches dashboard connections", async () => {
    const { result } = renderHook(
      () =>
        useDashboardPageData({
          revision: 1,
          selectedProfileId: 1,
        }),
      { wrapper: StrictWrapper },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
      expect(result.current.routingDiagramLoading).toBe(false);
    });

    expect(api.models.list).toHaveBeenCalledTimes(1);
    expect(api.connections.byModels).toHaveBeenCalledTimes(1);
    expect(api.connections.byModels).toHaveBeenCalledWith({ model_config_ids: [10, 11] });
    expect(api.stats.connectionSuccessRates).toHaveBeenCalledTimes(1);
    expect(api.stats.summary).toHaveBeenCalledTimes(2);
    expect(api.stats.spending).toHaveBeenCalledTimes(2);
    expect(api.stats.throughput).toHaveBeenCalledTimes(1);
    expect(api.stats.requests).toHaveBeenCalledTimes(1);
    expect(result.current.metricSnapshot.averageRpm).toBe(0.833);
    expect(result.current.metricSnapshot.averageRpmRequestTotal).toBe(20);
    expect(result.current.metricSnapshot.totalRequests).toBe(24);
    expect(result.current.metricSnapshot.totalModels).toBe(2);
    expect(result.current.strategyFamilySummary).toEqual({
      adaptiveCount: 1,
      legacyCount: 1,
      unassignedCount: 0,
    });
    expect(result.current.routingDiagramData?.links).toHaveLength(2);
    expect(result.current.modelDisplayNames.get("gpt-5.4")).toBe("GPT-5.4");
  });

  it("skips the batch connections request when no models are configured", async () => {
    api.models.list.mockResolvedValueOnce([]);

    const { result } = renderHook(
      () =>
        useDashboardPageData({
          revision: 1,
          selectedProfileId: 1,
        }),
      { wrapper: StrictWrapper },
    );

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
      expect(result.current.routingDiagramLoading).toBe(false);
    });

    expect(api.connections.byModels).not.toHaveBeenCalled();
    expect(api.stats.connectionSuccessRates).not.toHaveBeenCalled();
    expect(result.current.routingDiagramError).toBeNull();
    expect(result.current.routingDiagramData?.nodes).toHaveLength(0);
  });
});
