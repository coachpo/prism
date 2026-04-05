import { renderHook, act } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { getUserTimezonePreference, clearUserTimezonePreference } from "@/lib/timezone";
import { useCostingSettingsSave } from "../useCostingSettingsSave";

const api = vi.hoisted(() => ({
  settings: {
    costing: {
      update: vi.fn(),
    },
    timezone: {
      get: vi.fn(),
      update: vi.fn(),
    },
  },
}));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

describe("useCostingSettingsSave", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearUserTimezonePreference();
    api.settings.timezone.get.mockResolvedValue({ timezone_preference: "Europe/Helsinki" });
    api.settings.costing.update.mockResolvedValue({
      report_currency_code: "USD",
      report_currency_symbol: "$",
      endpoint_fx_mappings: [],
      timezone_preference: "UTC",
    });
    api.settings.timezone.update.mockResolvedValue({ timezone_preference: "UTC" });
  });

  it("clears cached timezone preference after saving timezone settings", async () => {
    await getUserTimezonePreference("profile:1");
    expect(api.settings.timezone.get).toHaveBeenCalledTimes(1);

    const { result } = renderHook(() =>
      useCostingSettingsSave({
        normalizedCurrentCosting: {
          report_currency_code: "USD",
          report_currency_symbol: "$",
          endpoint_fx_mappings: [],
          timezone_preference: "UTC",
        },
        savedCostingForm: {
          report_currency_code: "USD",
          report_currency_symbol: "$",
          endpoint_fx_mappings: [],
          timezone_preference: "Europe/Helsinki",
        },
        setCostingForm: vi.fn(),
        setCostingUnavailable: vi.fn(),
        setRecentlySavedSection: vi.fn(),
        setSavedCostingForm: vi.fn(),
      })
    );

    await act(async () => {
      await result.current.handleSaveCostingSettings("timezone");
    });

    api.settings.timezone.get.mockResolvedValueOnce({ timezone_preference: "UTC" });

    await getUserTimezonePreference("profile:1");

    expect(api.settings.timezone.update).toHaveBeenCalledTimes(1);
    expect(api.settings.timezone.get).toHaveBeenCalledTimes(2);
  });
});
