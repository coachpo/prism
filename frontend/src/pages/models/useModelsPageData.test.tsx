import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ManagedModelConfigListItem } from "@/lib/api/models";
import type { LoadbalanceStrategy, ModelConfig } from "@/lib/types";
import { useModelsPageData } from "./useModelsPageData";

const mocks = vi.hoisted(() => ({
  createDefaults: vi.fn(),
  createModel: vi.fn(),
  deleteModel: vi.fn(),
  getSharedLoadbalanceStrategies: vi.fn(),
  getSharedModels: vi.fn(),
  modelUpdate: vi.fn(),
  setSharedLoadbalanceStrategies: vi.fn(),
  setSharedModels: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
}));

vi.mock("@/lib/referenceData", () => ({
  getSharedLoadbalanceStrategies: mocks.getSharedLoadbalanceStrategies,
  getSharedModels: mocks.getSharedModels,
  setSharedLoadbalanceStrategies: mocks.setSharedLoadbalanceStrategies,
  setSharedModels: mocks.setSharedModels,
}));

vi.mock("@/lib/api", () => ({
  ApiError: class MockApiError extends Error {
    detail: unknown;
    status: number;

    constructor(message: string, status: number, detail: unknown) {
      super(message);
      this.status = status;
      this.detail = detail;
    }
  },
  api: {
    loadbalanceStrategies: { createDefaults: mocks.createDefaults },
    models: {
      create: mocks.createModel,
      delete: mocks.deleteModel,
      update: mocks.modelUpdate,
    },
  },
}));

vi.mock("@/i18n/staticMessages", () => ({
  getStaticMessages: () => ({
    loadbalanceStrategiesData: {
      defaultsAlreadyExisted: "Defaults already existed",
      defaultsCreated: "Defaults created",
      saveFailed: "Strategy save failed",
    },
    modelDetail: {
      noLoadbalanceStrategiesAvailable: "No strategies",
    },
    modelsData: {
      created: "Model created",
      deleted: "Model deleted",
      fetchFailed: "Fetch failed",
      modelIdRequired: "Model id required",
      openaiAcceptedFormatInvalid: "Accepted format invalid",
      openaiCapabilityRequired: "Capability required",
      openaiImageOperationsInvalid: "Image operations invalid",
      saveFailed: "Model save failed",
      selectApiFamily: "Select API family",
      selectLoadbalanceStrategy: "Select strategy",
      updated: "Model updated",
    },
    modelsPage: {
      bulkDone: (succeeded: string, failed: string) =>
        `${succeeded} succeeded, ${failed} failed`,
      toggleFailed: "Toggle failed",
      toggleEnabledDone: (name: string) => `Enabled ${name}`,
      toggleDisabledDone: (name: string) => `Disabled ${name}`,
      toggleUndo: "Undo",
      toggleUndone: "Undone",
    },
  }),
}));

vi.mock("sonner", () => ({
  toast: { error: mocks.toastError, success: mocks.toastSuccess },
}));

vi.mock("./useModelMetrics24h", () => ({
  useModelMetrics24h: () => ({
    coverage: null,
    metricsFailed: false,
    metricsLoading: false,
    modelMetricsByScope: {},
  }),
}));

const strategy = (id: number, updatedAt: string): LoadbalanceStrategy =>
  ({
    id,
    name: `Strategy ${id}`,
    updated_at: updatedAt,
    is_default: id === 7,
  }) as LoadbalanceStrategy;

const initialStrategies = [strategy(7, "2026-08-01T00:00:00Z")];
const nextStrategies = [strategy(8, "2026-08-02T00:00:00Z")];

function modelConfig(overrides: Partial<ModelConfig> = {}): ModelConfig {
  return {
    id: 11,
    profile_id: 1,
    api_family: "openai",
    model_id: "gpt-entry",
    display_name: "GPT Entry",
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    direct_request_enabled: true,
    loadbalance_strategy_id: 7,
    loadbalance_strategy: initialStrategies[0],
    access_targets: [],
    is_enabled: true,
    incoming_model_target_count: 0,
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    ...overrides,
  } as ModelConfig;
}

function modelListItem(overrides: Partial<ModelConfig> = {}) {
  const model = modelConfig(overrides);
  return {
    ...model,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
    configuration_warnings: [],
  } as ManagedModelConfigListItem;
}

describe("useModelsPageData lifecycle owners", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getSharedLoadbalanceStrategies.mockResolvedValue(initialStrategies);
    mocks.getSharedModels.mockResolvedValue([modelListItem()]);
    mocks.createDefaults.mockResolvedValue({ created: [8] });
  });

  it("keeps the public metrics contract while patching row enablement from the server DTO", async () => {
    const updated = modelConfig({ is_enabled: false });
    mocks.modelUpdate.mockResolvedValue({ model: updated });
    const { result } = renderHook(() => useModelsPageData(3));

    await waitFor(() => expect(result.current.loading).toBe(false));
    const current = result.current.models[0];
    await act(async () => {
      await result.current.setModelEnabled(current, false);
    });

    expect(mocks.modelUpdate).toHaveBeenCalledWith(11, { is_enabled: false });
    expect(result.current.models[0]?.is_enabled).toBe(false);
    expect(result.current.metricsLoading).toBe(false);
    expect(result.current.modelMetrics24h).toEqual({});
    expect(result.current.togglingModelIds).toEqual(new Set());
    expect(mocks.setSharedModels).toHaveBeenCalledWith(3, expect.any(Array));
  });

  it("refreshes local strategies after dialog close without publishing shared cache", async () => {
    const rereadStrategies = [strategy(9, "2026-08-03T00:00:00Z")];
    mocks.getSharedLoadbalanceStrategies
      .mockReset()
      .mockResolvedValueOnce(initialStrategies)
      .mockResolvedValueOnce(rereadStrategies);
    const { result } = renderHook(() => useModelsPageData(3));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mocks.setSharedLoadbalanceStrategies).not.toHaveBeenCalled();
    act(() => {
      result.current.setCreateDialogOpen(true);
      result.current.setCreateDialogOpen(false);
    });
    await waitFor(() =>
      expect(result.current.loadbalanceStrategies[0]?.id).toBe(9),
    );

    expect(mocks.getSharedLoadbalanceStrategies).toHaveBeenLastCalledWith(3);
    expect(mocks.setSharedLoadbalanceStrategies).not.toHaveBeenCalled();
  });

  it("re-reads strategy defaults and updates only an active create session", async () => {
    mocks.getSharedLoadbalanceStrategies
      .mockReset()
      .mockResolvedValueOnce(initialStrategies)
      .mockResolvedValueOnce(nextStrategies);
    const { result } = renderHook(() => useModelsPageData(3));

    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => {
      result.current.setCreateDialogOpen(true);
    });
    await act(async () => {
      await result.current.handleCreateLoadbalanceStrategyDefaults();
    });

    expect(mocks.createDefaults).toHaveBeenCalledTimes(1);
    expect(mocks.getSharedLoadbalanceStrategies).toHaveBeenLastCalledWith(
      3,
      true,
    );
    expect(result.current.loadbalanceStrategies[0]?.id).toBe(8);
    expect(result.current.formData.loadbalance_strategy_id).toBe(8);
    expect(result.current.loadbalanceStrategyDefaultsCreating).toBe(false);
    expect(mocks.setSharedLoadbalanceStrategies).toHaveBeenCalledTimes(1);
    expect(mocks.setSharedLoadbalanceStrategies).toHaveBeenCalledWith(
      3,
      nextStrategies,
    );
  });
});
