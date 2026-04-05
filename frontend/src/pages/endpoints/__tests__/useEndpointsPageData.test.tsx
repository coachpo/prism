import { StrictMode, type ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterAll, afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DragEndEvent } from "@dnd-kit/core";
import { toast } from "sonner";
import { clearSharedReferenceData } from "@/lib/referenceData";
import { useEndpointsPageData } from "../useEndpointsPageData";

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const api = vi.hoisted(() => ({
  endpoints: {
    create: vi.fn(),
    delete: vi.fn(),
    duplicate: vi.fn(),
    list: vi.fn(),
    movePosition: vi.fn(),
    update: vi.fn(),
  },
  models: {
    byEndpoints: vi.fn(),
  },
}));

const revisionState = vi.hoisted(() => ({ value: 1 }));

vi.mock("@/lib/api", () => ({ api }));
vi.mock("@/context/ProfileContext", () => ({
  useProfileContext: () => ({ revision: revisionState.value }),
}));
vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({ format: vi.fn(() => "formatted") }),
}));
vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

const consoleErrorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

function StrictWrapper({ children }: { children: ReactNode }) {
  return <StrictMode>{children}</StrictMode>;
}

function buildEndpoint(overrides: Record<string, unknown> = {}) {
  return {
    id: 10,
    profile_id: 1,
    name: "Primary",
    base_url: "https://api.openai.com",
    has_api_key: true,
    masked_api_key: "sk-***",
    position: 0,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function buildVendor(overrides: Record<string, unknown> = {}) {
  return {
    id: 10,
    key: "openai",
    name: "OpenAI",
    description: null,
    audit_enabled: false,
    audit_capture_bodies: true,
    created_at: "",
    updated_at: "",
    ...overrides,
  };
}

function buildModel(overrides: Record<string, unknown> = {}) {
  return {
    id: 1,
    vendor_id: 10,
    vendor: buildVendor(),
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
    ...overrides,
  };
}

function createDragEndEvent(activeId: number, overId: number): DragEndEvent {
  return {
    activatorEvent: new Event("pointerdown"),
    active: {
      id: activeId,
      data: { current: undefined },
      rect: { current: { initial: null, translated: null } },
    },
    collisions: null,
    delta: { x: 0, y: 0 },
    over: {
      id: overId,
      rect: { top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0 },
      disabled: false,
      data: { current: undefined },
    },
  };
}

describe("useEndpointsPageData", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearSharedReferenceData();
    document.documentElement.lang = "en";
    revisionState.value = 1;
    api.endpoints.list.mockResolvedValue([buildEndpoint()]);
    api.models.byEndpoints.mockResolvedValue({
      items: [
        {
          endpoint_id: 10,
          models: [buildModel()],
        },
      ],
    });
  });

  afterEach(() => {
    clearSharedReferenceData();
    cleanup();
  });

  afterAll(() => {
    consoleErrorSpy.mockRestore();
  });

  it("deduplicates StrictMode bootstrap fetches and batches endpoint models", async () => {
    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    expect(api.endpoints.list).toHaveBeenCalledTimes(1);
    expect(api.models.byEndpoints).toHaveBeenCalledTimes(1);
    expect(api.models.byEndpoints).toHaveBeenCalledWith({ endpoint_ids: [10] });
    expect(result.current.endpoints).toHaveLength(1);
    expect(result.current.endpointModels[10]).toHaveLength(1);
    expect(result.current.searchQuery).toBe("");
    expect(result.current.reviewFilter).toBe("all");
    expect(result.current.filteredEndpoints.map((endpoint) => endpoint.id)).toEqual([10]);
    expect(result.current.visibleEndpointIds).toEqual([10]);
    expect(result.current.hasActiveReviewFilters).toBe(false);
  });

  it("ignores stale in-flight bootstrap results after revision changes", async () => {
    const deferredEndpoints = createDeferred<Parameters<typeof api.endpoints.list.mockResolvedValue>[0]>();

    api.endpoints.list
      .mockImplementationOnce(() => deferredEndpoints.promise)
      .mockResolvedValueOnce([
        buildEndpoint({
          id: 20,
          profile_id: 2,
          name: "Secondary",
          base_url: "https://api.anthropic.com/v1",
        }),
      ]);
    api.models.byEndpoints.mockImplementation(({ endpoint_ids }) =>
      Promise.resolve({
        items: endpoint_ids.map((endpointId: number) => ({
          endpoint_id: endpointId,
          models:
            endpointId === 20
              ? [
                  {
                    id: 2,
                    vendor_id: 20,
                    vendor: buildVendor({ id: 20, key: "anthropic", name: "Anthropic" }),
                    api_family: "anthropic",
                    model_id: "claude-sonnet-4-6",
                    display_name: "Claude Sonnet 4.6",
                    model_type: "native",
                    loadbalance_strategy_id: 101,
                      loadbalance_strategy: {
                        id: 101,
                        name: "single-secondary",
                        strategy_type: "single",
                        auto_recovery: { mode: "disabled" },
                      },
                    is_enabled: true,
                    connection_count: 1,
                    active_connection_count: 1,
                    health_success_rate: 100,
                    health_total_requests: 5,
                    created_at: "",
                    updated_at: "",
                  },
                ]
              : [],
        })),
      })
    );

    const { result, rerender } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    revisionState.value = 2;
    rerender();

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
      expect(result.current.endpoints[0]?.id).toBe(20);
    });

    deferredEndpoints.resolve([
      buildEndpoint(),
    ]);

    await waitFor(() => {
      expect(result.current.endpoints[0]?.id).toBe(20);
      expect(result.current.endpointModels[20]).toHaveLength(1);
      expect(result.current.endpointModels[10]).toBeUndefined();
    });
  });

  it("keeps the new profile's endpoints when batched model loading fails", async () => {
    const { result, rerender } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
      expect(result.current.endpoints[0]?.id).toBe(10);
      expect(result.current.endpointModels[10]).toHaveLength(1);
    });

    api.endpoints.list.mockResolvedValueOnce([
      buildEndpoint({
        id: 20,
        profile_id: 2,
        name: "Secondary",
        base_url: "https://api.anthropic.com/v1",
      }),
    ]);
    api.models.byEndpoints.mockRejectedValueOnce(new Error("batch failed"));

    revisionState.value = 2;
    rerender();

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
      expect(result.current.endpoints[0]?.id).toBe(20);
      expect(result.current.endpointModels[20]).toBeUndefined();
      expect(result.current.filteredEndpoints.map((endpoint) => endpoint.id)).toEqual([20]);
      expect(result.current.visibleEndpointIds).toEqual([20]);
    });
  });

  it("filters endpoints for review mode and disables reorder while a search is active", async () => {
    api.endpoints.list.mockResolvedValueOnce([
      buildEndpoint({ id: 10, name: "OpenAI Primary", position: 0 }),
      buildEndpoint({
        id: 20,
        name: "Anthropic Backup",
        base_url: "https://api.anthropic.com/v1",
        position: 1,
      }),
    ]);
    api.models.byEndpoints.mockResolvedValueOnce({
      items: [
        { endpoint_id: 10, models: [buildModel()] },
        { endpoint_id: 20, models: [] },
      ],
    });

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => {
      result.current.setSearchQuery("anthropic");
    });

    expect(result.current.filteredEndpoints.map((endpoint) => endpoint.id)).toEqual([20]);
    expect(result.current.visibleEndpointIds).toEqual([20]);
    expect(result.current.hasActiveReviewFilters).toBe(true);
    expect(result.current.canReorder).toBe(false);
  });

  it("filters endpoints by usage state for review mode", async () => {
    api.endpoints.list.mockResolvedValueOnce([
      buildEndpoint({ id: 10, name: "OpenAI Primary", position: 0 }),
      buildEndpoint({ id: 20, name: "Anthropic Backup", position: 1 }),
      buildEndpoint({ id: 30, name: "Gemini Sandbox", position: 2 }),
    ]);
    api.models.byEndpoints.mockResolvedValueOnce({
      items: [
        { endpoint_id: 10, models: [buildModel()] },
        { endpoint_id: 20, models: [] },
        { endpoint_id: 30, models: [] },
      ],
    });

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => {
      result.current.setReviewFilter("unused");
    });

    expect(result.current.filteredEndpoints.map((endpoint) => endpoint.id)).toEqual([20, 30]);
    expect(result.current.visibleEndpointIds).toEqual([20, 30]);
    expect(result.current.hasActiveReviewFilters).toBe(true);
    expect(result.current.canReorder).toBe(false);

    act(() => {
      result.current.setReviewFilter("in-use");
    });

    expect(result.current.filteredEndpoints.map((endpoint) => endpoint.id)).toEqual([10]);
    expect(result.current.visibleEndpointIds).toEqual([10]);
  });

  it("creates endpoints and closes the create dialog while keeping local state in sync", async () => {
    const created = buildEndpoint({
      id: 11,
      name: "Anthropic Backup",
      base_url: "https://api.anthropic.com/v1",
      position: 1,
    });
    api.endpoints.create.mockResolvedValue(created);

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    act(() => {
      result.current.setIsCreateOpen(true);
    });

    await act(async () => {
      await result.current.handleCreate({
        name: created.name,
        base_url: created.base_url,
        api_key: "sk-created",
      });
    });

    expect(api.endpoints.create).toHaveBeenCalledWith({
      name: created.name,
      base_url: created.base_url,
      api_key: "sk-created",
    });
    expect(result.current.isCreateOpen).toBe(false);
    expect(result.current.endpoints.map((endpoint) => endpoint.id)).toEqual([10, 11]);
    expect(result.current.endpointModels[11]).toEqual([]);
  });

  it("updates endpoints and clears edit mode while keeping local state in sync", async () => {
    const updated = buildEndpoint({
      id: 10,
      name: "Primary Updated",
      base_url: "https://api.openai.com/v2",
    });
    api.endpoints.update.mockResolvedValue(updated);

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    act(() => {
      result.current.setEditingEndpoint(result.current.endpoints[0]);
    });

    await act(async () => {
      await result.current.handleUpdate({
        name: updated.name,
        base_url: updated.base_url,
        api_key: "",
      });
    });

    expect(api.endpoints.update).toHaveBeenCalledWith(10, {
      name: updated.name,
      base_url: updated.base_url,
    });
    expect(result.current.editingEndpoint).toBeNull();
    expect(result.current.endpoints[0]).toMatchObject({
      id: 10,
      name: updated.name,
      base_url: updated.base_url,
    });
  });

  it("duplicates and deletes endpoints while keeping local state in sync", async () => {
    api.endpoints.duplicate.mockResolvedValue(
      buildEndpoint({ id: 11, name: "Primary Copy", position: 1 })
    );
    api.endpoints.delete.mockResolvedValue(undefined);

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    await act(async () => {
      await result.current.handleDuplicateEndpoint(result.current.endpoints[0]);
    });

    expect(api.endpoints.duplicate).toHaveBeenCalledWith(10);
    expect(result.current.endpoints.map((endpoint) => endpoint.id)).toEqual([10, 11]);
    expect(result.current.endpointModels[11]).toEqual([]);
    expect(result.current.duplicatingEndpointId).toBeNull();

    act(() => {
      result.current.setDeleteTarget(result.current.endpoints[0]);
    });

    await act(async () => {
      await result.current.handleDelete(10);
    });

    expect(api.endpoints.delete).toHaveBeenCalledWith(10);
    expect(result.current.endpoints.map((endpoint) => endpoint.id)).toEqual([11]);
    expect(result.current.endpointModels[10]).toBeUndefined();
  });

  it("shows a destructive toast when endpoint deletion is blocked by connections", async () => {
    api.endpoints.delete.mockRejectedValueOnce(
      new Error("Cannot delete endpoint that is referenced by connections")
    );

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    act(() => {
      result.current.setDeleteTarget(result.current.endpoints[0]);
    });

    await act(async () => {
      await result.current.handleDelete(10);
    });

    expect(result.current.deleteTarget).toMatchObject({
      id: 10,
      name: "Primary",
    });
    expect(toast.error).toHaveBeenCalledWith(
      "Cannot delete endpoint that is referenced by connections"
    );
  });

  it("emits localized duplication success when the locale is Chinese", async () => {
    document.documentElement.lang = "zh-CN";
    api.endpoints.duplicate.mockResolvedValue(
      buildEndpoint({ id: 11, name: "Primary Copy", position: 1 })
    );

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
    });

    await act(async () => {
      await result.current.handleDuplicateEndpoint(result.current.endpoints[0]);
    });

    expect(toast.success).toHaveBeenCalledWith("端点已复制为 Primary Copy");
  });

  it("reorders endpoints and rolls back when movePosition fails", async () => {
    api.endpoints.list.mockResolvedValueOnce([
      buildEndpoint({ id: 10, position: 0 }),
      buildEndpoint({ id: 20, name: "Secondary", base_url: "https://api.anthropic.com/v1", position: 1 }),
    ]);
    api.models.byEndpoints.mockResolvedValueOnce({ items: [] });
    api.endpoints.movePosition.mockResolvedValueOnce([
      buildEndpoint({ id: 20, name: "Secondary", base_url: "https://api.anthropic.com/v1", position: 0 }),
      buildEndpoint({ id: 10, position: 1 }),
    ]);

    const { result } = renderHook(() => useEndpointsPageData(), { wrapper: StrictWrapper });

    await waitFor(() => {
      expect(result.current.isLoading).toBe(false);
      expect(result.current.endpoints).toHaveLength(2);
    });

    await act(async () => {
      await result.current.handleDragEnd(createDragEndEvent(20, 10));
    });

    expect(api.endpoints.movePosition).toHaveBeenCalledWith(20, 0);
    expect(result.current.endpoints.map((endpoint) => endpoint.id)).toEqual([20, 10]);

    api.endpoints.movePosition.mockRejectedValueOnce(new Error("move failed"));

    await act(async () => {
      await result.current.handleDragEnd(createDragEndEvent(10, 20));
    });

    expect(result.current.endpoints.map((endpoint) => endpoint.id)).toEqual([20, 10]);
  });
});
