import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type { LoadbalanceStrategySummary, SpendingSummary } from "@/lib/types";
import { buildProxyTargetOptions } from "../useModelDetailDataSupport";
import { useModelDetailBootstrap } from "../useModelDetailBootstrap";

type LegacyStrategySummary = Extract<LoadbalanceStrategySummary, { strategy_type: "legacy" }>;

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });

  return { promise, resolve };
}

const api = vi.hoisted(() => ({
  endpoints: {
    list: vi.fn(),
  },
  loadbalanceStrategies: {
    list: vi.fn(),
  },
  models: {
    get: vi.fn(),
    list: vi.fn(),
  },
  pricingTemplates: {
    list: vi.fn(),
  },
  stats: {
    spending: vi.fn(),
  },
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}));

function buildLoadbalanceStrategySummary(overrides: Partial<LegacyStrategySummary> = {}): LegacyStrategySummary {
  return {
    id: 100,
    name: "single-primary",
    strategy_type: "legacy",
    legacy_strategy_type: "single",
    auto_recovery: { mode: "disabled" },
    ...overrides,
  };
}

function buildLoadbalanceStrategy(overrides: Record<string, unknown> = {}) {
  return {
    ...buildLoadbalanceStrategySummary(overrides),
    profile_id: 1,
    attached_model_count: 1,
    created_at: "",
    updated_at: "",
  };
}

function buildVendor(overrides: Record<string, unknown> = {}) {
  return {
    id: 10,
    key: "openai",
    name: "OpenAI",
    description: null,
    icon_key: null,
    audit_enabled: false,
    audit_capture_bodies: false,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

describe("useModelDetailBootstrap", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearSharedReferenceData();
    api.endpoints.list.mockResolvedValue([]);
    api.loadbalanceStrategies.list.mockResolvedValue([buildLoadbalanceStrategy()]);
    api.models.list.mockResolvedValue([]);
    api.pricingTemplates.list.mockResolvedValue([]);
  });

  afterEach(() => {
    clearSharedReferenceData();
  });

  it("builds proxy target options from matching api families even when the vendor differs", () => {
    const options = buildProxyTargetOptions(
      {
        id: 9,
        vendor_id: 30,
        vendor: buildVendor({ id: 30, key: "together-ai", name: "Together AI" }),
        api_family: "openai",
        model_id: "friendly-proxy",
        display_name: "Friendly Proxy",
        model_type: "proxy",
        proxy_targets: [],
        loadbalance_strategy_id: null,
        loadbalance_strategy: null,
        is_enabled: true,
        connections: [],
        created_at: "",
        updated_at: "",
      },
      [
        {
          id: 1,
          vendor_id: 10,
          vendor: buildVendor({ id: 10, key: "openai", name: "OpenAI" }),
          api_family: "openai",
          model_id: "gpt-5.4",
          display_name: "GPT-5.4",
          model_type: "native",
          proxy_targets: [],
          loadbalance_strategy_id: 100,
          loadbalance_strategy: buildLoadbalanceStrategySummary(),
          is_enabled: true,
          connection_count: 1,
          active_connection_count: 1,
          health_success_rate: 100,
          health_total_requests: 10,
          created_at: "",
          updated_at: "",
        },
        {
          id: 2,
          vendor_id: 30,
          vendor: buildVendor({ id: 30, key: "together-ai", name: "Together AI" }),
          api_family: "gemini",
          model_id: "gemini-2.5-pro",
          display_name: "Gemini 2.5 Pro",
          model_type: "native",
          proxy_targets: [],
          loadbalance_strategy_id: 100,
          loadbalance_strategy: buildLoadbalanceStrategySummary(),
          is_enabled: true,
          connection_count: 1,
          active_connection_count: 1,
          health_success_rate: 100,
          health_total_requests: 10,
          created_at: "",
          updated_at: "",
        },
      ],
    );

    expect(options).toEqual([{ modelId: "gpt-5.4", label: "GPT-5.4 (gpt-5.4)" }]);
  });

  it("ignores stale spending responses after switching models", async () => {
    const firstSpending = createDeferred<{
      summary: SpendingSummary;
      report_currency_symbol: string;
      report_currency_code: string;
    }>();

    api.models.get
      .mockResolvedValueOnce({
        id: 1,
        vendor_id: 10,
        vendor: buildVendor({ id: 10, key: "openai", name: "OpenAI" }),
        api_family: "openai",
        model_id: "gpt-5.4",
        display_name: "GPT-5.4",
        model_type: "native",
        loadbalance_strategy_id: 100,
        loadbalance_strategy: buildLoadbalanceStrategySummary(),
        is_enabled: true,
        connections: [],
        created_at: "",
        updated_at: "",
      })
      .mockResolvedValueOnce({
        id: 2,
        vendor_id: 20,
        vendor: buildVendor({ id: 20, key: "anthropic", name: "Anthropic" }),
        api_family: "anthropic",
        model_id: "claude-sonnet-4-6",
        display_name: "Claude Sonnet 4.6",
        model_type: "native",
        loadbalance_strategy_id: 101,
        loadbalance_strategy: buildLoadbalanceStrategySummary({
          id: 101,
          name: "single-secondary",
        }),
        is_enabled: true,
        connections: [],
        created_at: "",
        updated_at: "",
      });

    api.stats.spending
      .mockImplementationOnce(() => firstSpending.promise)
      .mockResolvedValueOnce({
        summary: {
          total_cost_micros: 222,
          successful_request_count: 2,
          priced_request_count: 2,
          unpriced_request_count: 0,
          total_input_tokens: 20,
          total_output_tokens: 30,
          total_cache_read_input_tokens: 0,
          total_cache_creation_input_tokens: 0,
          total_reasoning_tokens: 0,
          total_tokens: 50,
          avg_cost_per_successful_request_micros: 111,
        },
        groups: [],
        groups_total: 0,
        top_spending_models: [],
        top_spending_endpoints: [],
        unpriced_breakdown: {},
        report_currency_symbol: "$",
        report_currency_code: "USD",
      });

    const setSpending = vi.fn();
    const { rerender } = renderHook(
      ({ id, revision }) =>
        useModelDetailBootstrap({
          id,
          revision,
          navigate: vi.fn(),
          setModel: vi.fn(),
          setConnections: vi.fn(),
          setGlobalEndpoints: vi.fn(),
          setLoadbalanceStrategies: vi.fn(),
          setAllModels: vi.fn(),
          setPricingTemplates: vi.fn(),
          setLoading: vi.fn(),
          setSpending,
          setSpendingLoading: vi.fn(),
          setSpendingCurrencySymbol: vi.fn(),
          setSpendingCurrencyCode: vi.fn(),
        }),
      {
        initialProps: {
          id: "1",
          revision: 1,
        },
      }
    );

    await waitFor(() => {
      expect(api.stats.spending).toHaveBeenCalledTimes(1);
    });

    rerender({ id: "2", revision: 2 });

    await waitFor(() => {
      expect(api.stats.spending).toHaveBeenCalledTimes(2);
    });

    await waitFor(() => {
      expect(setSpending).toHaveBeenLastCalledWith({
        total_cost_micros: 222,
        successful_request_count: 2,
        priced_request_count: 2,
        unpriced_request_count: 0,
        total_input_tokens: 20,
        total_output_tokens: 30,
        total_cache_read_input_tokens: 0,
        total_cache_creation_input_tokens: 0,
        total_reasoning_tokens: 0,
        total_tokens: 50,
        avg_cost_per_successful_request_micros: 111,
      });
    });

    firstSpending.resolve({
      summary: {
        total_cost_micros: 111,
        successful_request_count: 1,
        priced_request_count: 1,
        unpriced_request_count: 0,
        total_input_tokens: 10,
        total_output_tokens: 15,
        total_cache_read_input_tokens: 0,
        total_cache_creation_input_tokens: 0,
        total_reasoning_tokens: 0,
        total_tokens: 25,
        avg_cost_per_successful_request_micros: 111,
      },
      report_currency_symbol: "$",
      report_currency_code: "USD",
    });

    await waitFor(() => {
      expect(setSpending).toHaveBeenLastCalledWith({
        total_cost_micros: 222,
        successful_request_count: 2,
        priced_request_count: 2,
        unpriced_request_count: 0,
        total_input_tokens: 20,
        total_output_tokens: 30,
        total_cache_read_input_tokens: 0,
        total_cache_creation_input_tokens: 0,
        total_reasoning_tokens: 0,
        total_tokens: 50,
        avg_cost_per_successful_request_micros: 111,
      });
    });

  });

  it("reloads spending when switching models", async () => {
    api.models.get.mockResolvedValue({
      id: 1,
      vendor_id: 10,
      vendor: buildVendor({ id: 10, key: "openai", name: "OpenAI" }),
      api_family: "openai",
      model_id: "gpt-5.4",
      display_name: "GPT-5.4",
          model_type: "native",
      loadbalance_strategy_id: 100,
      loadbalance_strategy: buildLoadbalanceStrategySummary(),
      is_enabled: true,
      connections: [],
      created_at: "",
      updated_at: "",
    });
    api.stats.spending.mockResolvedValue({
      summary: {
        total_cost_micros: 111,
        successful_request_count: 1,
        priced_request_count: 1,
        unpriced_request_count: 0,
        total_input_tokens: 10,
        total_output_tokens: 15,
        total_cache_read_input_tokens: 0,
        total_cache_creation_input_tokens: 0,
        total_reasoning_tokens: 0,
        total_tokens: 25,
        avg_cost_per_successful_request_micros: 111,
      },
      groups: [],
      groups_total: 0,
      top_spending_models: [],
      top_spending_endpoints: [],
      unpriced_breakdown: {},
      report_currency_symbol: "$",
      report_currency_code: "USD",
    });

    const { rerender } = renderHook(
      ({ id, revision }) =>
        useModelDetailBootstrap({
          id,
          revision,
          navigate: vi.fn(),
          setModel: vi.fn(),
          setConnections: vi.fn(),
          setGlobalEndpoints: vi.fn(),
          setLoadbalanceStrategies: vi.fn(),
          setAllModels: vi.fn(),
          setPricingTemplates: vi.fn(),
          setLoading: vi.fn(),
          setSpending: vi.fn(),
          setSpendingLoading: vi.fn(),
          setSpendingCurrencySymbol: vi.fn(),
          setSpendingCurrencyCode: vi.fn(),
          }),
      {
        initialProps: { id: "1", revision: 1 },
      }
    );

    await waitFor(() => {
      expect(api.models.get).toHaveBeenCalledTimes(1);
    });

    rerender({ id: "2", revision: 2 });

  });

  it("reuses spending fetches during same-model refreshes", async () => {
    api.models.get.mockResolvedValue({
      id: 1,
      vendor_id: 10,
      vendor: buildVendor({ id: 10, key: "openai", name: "OpenAI" }),
      api_family: "openai",
      model_id: "gpt-5.4",
      display_name: "GPT-5.4",
          model_type: "native",
      loadbalance_strategy_id: 100,
      loadbalance_strategy: buildLoadbalanceStrategySummary(),
      is_enabled: true,
      connections: [],
      created_at: "",
      updated_at: "",
    });
    api.stats.spending.mockResolvedValue({
      summary: {
        total_cost_micros: 111,
        successful_request_count: 1,
        priced_request_count: 1,
        unpriced_request_count: 0,
        total_input_tokens: 10,
        total_output_tokens: 15,
        total_cache_read_input_tokens: 0,
        total_cache_creation_input_tokens: 0,
        total_reasoning_tokens: 0,
        total_tokens: 25,
        avg_cost_per_successful_request_micros: 111,
      },
      groups: [],
      groups_total: 0,
      top_spending_models: [],
      top_spending_endpoints: [],
      unpriced_breakdown: {},
      report_currency_symbol: "$",
      report_currency_code: "USD",
    });

    const { result } = renderHook(() =>
      useModelDetailBootstrap({
        id: "1",
        revision: 1,
        navigate: vi.fn(),
        setModel: vi.fn(),
        setConnections: vi.fn(),
        setGlobalEndpoints: vi.fn(),
        setLoadbalanceStrategies: vi.fn(),
        setAllModels: vi.fn(),
        setPricingTemplates: vi.fn(),
        setLoading: vi.fn(),
        setSpending: vi.fn(),
        setSpendingLoading: vi.fn(),
        setSpendingCurrencySymbol: vi.fn(),
        setSpendingCurrencyCode: vi.fn(),
      })
    );

    await waitFor(() => {
      expect(api.models.get).toHaveBeenCalledTimes(1);
    });

    await act(async () => {
      await result.current.fetchModel();
    });

    expect(api.models.get).toHaveBeenCalledTimes(2);
  });
});
