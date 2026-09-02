import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { ModelConfig } from "@/lib/types";
import { useModelDetailModelForm } from "./useModelDetailModelForm";

const mocks = vi.hoisted(() => ({
  clearSharedReferenceData: vi.fn(),
  update: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: { models: { update: mocks.update } },
}));

vi.mock("@/lib/referenceData", () => ({
  clearSharedReferenceData: mocks.clearSharedReferenceData,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

const model: ModelConfig = {
  id: 7,
  profile_id: 1,
  api_family: "openai",
  model_id: "gpt-test",
  display_name: "GPT Test",
  openai_accepted_format: "dual_native",
  openai_image_operations: null,
  direct_request_enabled: true,
  loadbalance_strategy_id: 3,
  loadbalance_strategy: null,
  access_targets: [],
  is_enabled: false,
  incoming_model_target_count: 0,
  configuration_warnings: [],
  created_at: "2026-08-28T00:00:00Z",
  updated_at: "2026-08-28T00:00:00Z",
};

describe("useModelDetailModelForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.update.mockResolvedValue({
      model: { ...model, display_name: "Updated" },
      configuration_warnings: [],
    });
  });

  it("refreshes authoritative models and diagnostics after a successful edit", async () => {
    const refreshDiagnostics = vi.fn();
    const refreshModels = vi.fn().mockResolvedValue(undefined);
    const setDialogOpen = vi.fn();
    const setModel = vi.fn();

    const { result } = renderHook(() =>
      useModelDetailModelForm({
        model,
        revision: 0,
        setIsEditModelDialogOpenState: setDialogOpen,
        setModel,
        refreshDiagnostics,
        refreshModels,
      }),
    );

    act(() => result.current.setIsEditModelDialogOpen(true));
    await act(async () => {
      await result.current.handleEditModelSubmit({ preventDefault: vi.fn() });
    });

    expect(mocks.update).toHaveBeenCalledWith(
      7,
      expect.objectContaining({ loadbalance_strategy_id: 3 }),
    );
    expect(refreshModels).toHaveBeenCalledOnce();
    expect(refreshDiagnostics).toHaveBeenCalledOnce();
    expect(setModel).toHaveBeenCalledWith(
      expect.objectContaining({ display_name: "Updated" }),
    );
    expect(setDialogOpen).toHaveBeenLastCalledWith(false);
  });
});
