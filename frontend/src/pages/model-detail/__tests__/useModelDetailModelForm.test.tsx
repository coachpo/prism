import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@/lib/api";
import { toast } from "sonner";
import { useModelDetailModelForm } from "../useModelDetailModelForm";

vi.mock("@/lib/api", () => ({
  api: {
    models: {
      update: vi.fn(),
    },
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@/lib/referenceData", () => ({
  clearSharedReferenceData: vi.fn(),
}));

function buildProxyModel() {
  return {
    id: 9,
    vendor_id: 1,
    vendor: {
      id: 1,
      key: "openai",
      name: "OpenAI",
      description: null,
      icon_key: "openai",
      audit_enabled: false,
      audit_capture_bodies: false,
      created_at: "",
      updated_at: "",
    },
    api_family: "openai" as const,
    model_id: "gateway-proxy",
    display_name: "Gateway Proxy",
    model_type: "proxy" as const,
    proxy_targets: [{ target_model_id: "e2e-native-a", position: 0 }],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connections: [],
    created_at: "",
    updated_at: "",
  };
}

function buildSubmitEvent(form: HTMLFormElement) {
  return {
    currentTarget: form,
    preventDefault: vi.fn(),
  } as unknown as React.FormEvent<HTMLFormElement>;
}

describe("useModelDetailModelForm", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("does not submit proxy target assignments through the model settings dialog flow", async () => {
    const proxyModel = buildProxyModel();
    vi.mocked(api.models.update).mockResolvedValue(proxyModel);

    const setIsEditModelDialogOpen = vi.fn();
    const setAllModels = vi.fn();
    const setModel = vi.fn();
    const setEditLoadbalanceStrategyId = vi.fn();
    const form = document.createElement("form");

    for (const [name, value] of [
      ["vendor_id", "1"],
      ["api_family", "openai"],
      ["display_name", "Gateway Proxy"],
      ["model_id", "gateway-proxy"],
    ]) {
      const input = document.createElement("input");
      input.name = name;
      input.value = value;
      form.append(input);
    }

    const { result } = renderHook(() =>
      useModelDetailModelForm({
        editLoadbalanceStrategyId: "",
        model: proxyModel,
        allModels: [],
        isEditModelDialogOpen: true,
        revision: 1,
        setEditLoadbalanceStrategyId,
        setIsEditModelDialogOpen,
        setAllModels,
        setModel,
      }),
    );

    await act(async () => {
      await result.current.handleEditModelSubmit(buildSubmitEvent(form));
    });

    expect(api.models.update).toHaveBeenCalledWith(
      9,
      expect.objectContaining({
        vendor_id: 1,
        api_family: "openai",
        display_name: "Gateway Proxy",
        model_id: "gateway-proxy",
        loadbalance_strategy_id: null,
      }),
    );
    expect(vi.mocked(api.models.update).mock.calls[0]?.[1]).not.toHaveProperty("proxy_targets");
    expect(toast.success).toHaveBeenCalled();
  });
});
