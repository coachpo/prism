import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import type { LoadbalanceStrategy } from "@/lib/types";
import { getDefaultAutoRecovery } from "@/lib/loadbalanceRoutingPolicy";
import { getDefaultAutoRecoveryDraft } from "../loadbalanceStrategyFormState";
import { useLoadbalanceStrategiesPageData } from "../useLoadbalanceStrategiesPageData";

type LegacyStrategy = Extract<LoadbalanceStrategy, { strategy_type: "legacy" }>;

function buildAdaptiveRoutingPolicy(overrides: Record<string, unknown> = {}) {
  return {
    kind: "adaptive" as const,
    routing_objective: "maximize_availability" as const,
    deadline_budget_ms: 1500,
    hedge: {
      enabled: false,
      delay_ms: 75,
      max_additional_attempts: 1,
    },
    circuit_breaker: {
      failure_status_codes: [429, 503, 504],
      base_open_seconds: 60,
      failure_threshold: 2,
      backoff_multiplier: 2,
      max_open_seconds: 900,
      jitter_ratio: 0.2,
      ban_mode: "off" as const,
      max_open_strikes_before_ban: 0,
      ban_duration_seconds: 0,
    },
    admission: {
      respect_qps_limit: true,
      respect_in_flight_limits: true,
    },
    ...overrides,
  };
}

const api = vi.hoisted(() => ({
  loadbalanceStrategies: {
    get: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
}));

const referenceData = vi.hoisted(() => ({
  getSharedLoadbalanceStrategies: vi.fn(),
  setSharedLoadbalanceStrategies: vi.fn(),
}));

vi.mock("@/lib/api", () => ({ api, ApiError: class ApiError extends Error { status = 409; detail = null; } }));
vi.mock("@/lib/referenceData", () => ({
  getSharedLoadbalanceStrategies: referenceData.getSharedLoadbalanceStrategies,
  setSharedLoadbalanceStrategies: referenceData.setSharedLoadbalanceStrategies,
}));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

function buildStrategy(overrides: Partial<LegacyStrategy> = {}): LegacyStrategy {
  return {
    id: 1,
    profile_id: 1,
    name: "Primary single",
    strategy_type: "legacy",
    legacy_strategy_type: "single",
    auto_recovery: { mode: "disabled" },
    attached_model_count: 0,
    created_at: "2026-03-30T09:00:00Z",
    updated_at: "2026-03-30T10:00:00Z",
    ...overrides,
  };
}

describe("useLoadbalanceStrategiesPageData i18n", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.documentElement.lang = "zh-CN";
    referenceData.getSharedLoadbalanceStrategies.mockResolvedValue([]);
  });

  it("emits localized validation errors when the strategy name is missing", async () => {
    const { result } = renderHook(() => useLoadbalanceStrategiesPageData(1));

    await waitFor(() => {
      expect(result.current.loadbalanceStrategiesLoading).toBe(false);
    });

    await act(async () => {
      await result.current.handleSaveLoadbalanceStrategy();
    });

    expect(toast.error).toHaveBeenCalledWith("名称为必填项");
  });

  it("keeps the adaptive create draft stable while closing after a successful save", async () => {
    const created: Extract<LoadbalanceStrategy, { strategy_type: "adaptive" }> = {
      id: 3,
      profile_id: 1,
      name: "Adaptive availability",
      strategy_type: "adaptive",
      routing_policy: buildAdaptiveRoutingPolicy(),
      attached_model_count: 0,
      created_at: "2026-03-30T09:00:00Z",
      updated_at: "2026-03-30T11:00:00Z",
    };
    api.loadbalanceStrategies.create.mockResolvedValue(created);

    const { result } = renderHook(() => useLoadbalanceStrategiesPageData(1));

    await waitFor(() => {
      expect(result.current.loadbalanceStrategiesLoading).toBe(false);
    });

    act(() => {
      result.current.openCreateLoadbalanceStrategyDialog();
      result.current.setLoadbalanceStrategyForm(() => ({
        name: "Adaptive availability",
        strategy_type: "adaptive",
        routing_policy: buildAdaptiveRoutingPolicy(),
      }));
    });

    await act(async () => {
      await result.current.handleSaveLoadbalanceStrategy();
    });

    expect(api.loadbalanceStrategies.create).toHaveBeenCalledWith({
      name: "Adaptive availability",
      strategy_type: "adaptive",
      routing_policy: buildAdaptiveRoutingPolicy(),
    });
    expect(result.current.loadbalanceStrategyDialogOpen).toBe(false);
    expect(result.current.loadbalanceStrategyForm).toEqual({
      name: "Adaptive availability",
      strategy_type: "adaptive",
      routing_policy: buildAdaptiveRoutingPolicy(),
    });
  });

  it("keeps the edit snapshot stable while closing after a successful save", async () => {
    const summary = buildStrategy({
      id: 7,
      name: "Original strategy",
      strategy_type: "legacy",
      legacy_strategy_type: "single",
      updated_at: "2026-03-30T10:00:00Z",
    });
    const loaded = buildStrategy({
      id: 7,
      name: "Original strategy",
      strategy_type: "legacy",
      legacy_strategy_type: "single",
      updated_at: "2026-03-30T10:00:00Z",
    });
    const updated = buildStrategy({
      id: 7,
      name: "Updated round robin",
      strategy_type: "legacy",
      legacy_strategy_type: "round-robin",
      auto_recovery: getDefaultAutoRecovery("round-robin"),
      updated_at: "2026-03-30T12:00:00Z",
    });
    referenceData.getSharedLoadbalanceStrategies.mockResolvedValueOnce([summary]);
    api.loadbalanceStrategies.get.mockResolvedValue(loaded);
    api.loadbalanceStrategies.update.mockResolvedValue(updated);

    const { result } = renderHook(() => useLoadbalanceStrategiesPageData(1));

    await waitFor(() => {
      expect(result.current.loadbalanceStrategiesLoading).toBe(false);
      expect(result.current.loadbalanceStrategies[0]?.id).toBe(7);
    });

    await act(async () => {
      await result.current.handleEditLoadbalanceStrategy(summary);
    });

    act(() => {
      result.current.setLoadbalanceStrategyForm((prev) => ({
        ...prev,
        name: "Updated round robin",
        strategy_type: "legacy",
        legacy_strategy_type: "round-robin",
        auto_recovery: getDefaultAutoRecoveryDraft("round-robin"),
      }));
    });

    await act(async () => {
      await result.current.handleSaveLoadbalanceStrategy();
    });

    expect(api.loadbalanceStrategies.update).toHaveBeenCalledWith(7, {
      name: "Updated round robin",
      strategy_type: "legacy",
      legacy_strategy_type: "round-robin",
      auto_recovery: getDefaultAutoRecovery("round-robin"),
    });
    expect(result.current.loadbalanceStrategyDialogOpen).toBe(false);
    expect(result.current.editingLoadbalanceStrategy?.id).toBe(7);
    expect(result.current.loadbalanceStrategyForm).toMatchObject({
      name: "Updated round robin",
      strategy_type: "legacy",
      legacy_strategy_type: "round-robin",
      auto_recovery: {
        mode: "enabled",
      },
    });
  });

  it("closes the delete dialog without dropping the displayed strategy snapshot", async () => {
    const strategy = buildStrategy({ id: 11, name: "Disposable strategy" });
    referenceData.getSharedLoadbalanceStrategies.mockResolvedValueOnce([strategy]);
    api.loadbalanceStrategies.delete.mockResolvedValue(undefined);

    const { result } = renderHook(() => useLoadbalanceStrategiesPageData(1));

    await waitFor(() => {
      expect(result.current.loadbalanceStrategiesLoading).toBe(false);
      expect(result.current.loadbalanceStrategies[0]?.id).toBe(11);
    });

    act(() => {
      result.current.handleDeleteLoadbalanceStrategyClick(strategy);
    });

    await act(async () => {
      await result.current.handleDeleteLoadbalanceStrategy();
    });

    expect(result.current.loadbalanceStrategies).toEqual([]);
    expect(result.current.deleteLoadbalanceStrategyDialogOpen).toBe(false);
    expect(result.current.deleteLoadbalanceStrategyConfirm).toBeNull();
    expect(result.current.displayedDeleteLoadbalanceStrategyConfirm).toMatchObject({
      id: 11,
      name: "Disposable strategy",
    });
  });
});
