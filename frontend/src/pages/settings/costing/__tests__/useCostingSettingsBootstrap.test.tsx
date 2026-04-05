import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { clearSharedReferenceData } from "@/lib/referenceData";
import { useCostingSettingsBootstrap } from "../useCostingSettingsBootstrap";

const api = vi.hoisted(() => ({
  models: {
    list: vi.fn(),
  },
  settings: {
    costing: {
      get: vi.fn(),
    },
  },
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}));

describe("useCostingSettingsBootstrap", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearSharedReferenceData();
    api.models.list.mockResolvedValue([
      {
        id: 1,
        vendor_id: 10,
        vendor: {
          id: 10,
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
          strategy_type: "single",
          auto_recovery: { mode: "disabled" },
        },
        is_enabled: true,
        connection_count: 1,
        active_connection_count: 1,
        health_success_rate: 100,
        health_total_requests: 10,
        created_at: "",
        updated_at: "",
      },
    ]);
    api.settings.costing.get.mockResolvedValue({
      report_currency_code: "USD",
      report_currency_symbol: "$",
      endpoint_fx_mappings: [],
      timezone_preference: "UTC",
    });
  });

  it("loads costing settings and shared models in parallel", async () => {
    const { result } = renderHook(() => useCostingSettingsBootstrap(1));

    await waitFor(() => {
      expect(result.current.costingLoading).toBe(false);
      expect(result.current.models).toHaveLength(1);
      expect(result.current.costingForm.timezone_preference).toBe("UTC");
    });

    expect(api.models.list).toHaveBeenCalledTimes(1);
    expect(api.settings.costing.get).toHaveBeenCalledTimes(1);
  });
});
