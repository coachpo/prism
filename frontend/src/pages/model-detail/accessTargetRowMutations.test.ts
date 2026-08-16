import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ModelAccessTarget, ModelConfig } from "@/lib/types";
import { useModelDetailConnectionMutations } from "./useModelDetailConnectionMutations";

const mocks = vi.hoisted(() => ({
  update: vi.fn(),
  movePosition: vi.fn(),
  remove: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@/lib/api", () => ({
  api: {
    models: {
      targets: {
        update: mocks.update,
        movePosition: mocks.movePosition,
        delete: mocks.remove,
      },
    },
  },
}));

vi.mock("sonner", () => ({ toast: { error: mocks.toastError, success: vi.fn() } }));

vi.mock("@/lib/referenceData", () => ({ clearSharedReferenceData: vi.fn() }));

function terminalRow(rowId: number, connectionId: number, position: number): ModelAccessTarget {
  return {
    id: rowId,
    target_type: "connection",
    target_model_id: null,
    connection_id: connectionId,
    terminal_target_id: connectionId,
    position,
    is_enabled: true,
    target_model: null,
    connection: null,
    terminal_target: null,
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-08T00:00:00Z",
  };
}

function modelRow(rowId: number, targetModelId: string, position: number): ModelAccessTarget {
  return {
    id: rowId,
    target_type: "model",
    target_model_id: targetModelId,
    connection_id: null,
    terminal_target_id: null,
    position,
    is_enabled: true,
    target_model: null,
    connection: null,
    terminal_target: null,
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-08T00:00:00Z",
  };
}

// Row IDs deliberately do not coincide with array indices, and one of them
// (id 1) IS a valid index into the four-row array. A positional lookup would
// silently act on the wrong row for that one and no-op for the rest.
const accessTargets: ModelAccessTarget[] = [
  modelRow(19, "child-a", 0),
  modelRow(20, "child-b", 1),
  terminalRow(1, 12, 2),
  terminalRow(9, 14, 3),
];

const model = {
  id: 8,
  profile_id: 1,
  api_family: "openai",
  model_id: "deepseek-v4-flash",
  display_name: "DeepSeek V4 Flash",
  loadbalance_strategy_id: 2,
  loadbalance_strategy: null,
  openai_accepted_format: "chat_completions_only",
  openai_image_operations: null,
  access_targets: accessTargets,
  is_enabled: true,
  created_at: "2026-08-08T00:00:00Z",
  updated_at: "2026-08-08T00:00:00Z",
} as unknown as ModelConfig;

function renderMutations() {
  return renderHook(() =>
    useModelDetailConnectionMutations({
      id: "8",
      revision: 0,
      model,
      modelApiFamily: "openai",
      createMode: "select",
      selectedEndpointId: "",
      newEndpointForm: { name: "", base_url: "", api_key: "" },
      connectionForm: {} as never,
      headerRows: [],
      customRequestParametersDraft: "",
      setCustomRequestParametersError: vi.fn(),
      editingConnection: null,
      pricingTemplates: [],
      endpointSourceDefaultName: null,
      refreshCurrentState: vi.fn(),
      setIsConnectionDialogOpen: vi.fn(),
      setAllModels: vi.fn(),
      setConnections: vi.fn(),
      setAllConnections: vi.fn(),
      setModel: vi.fn(),
      setGlobalEndpoints: vi.fn(),
    }),
  );
}

describe("access target mutations address rows by row id", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.update.mockResolvedValue({ access_targets: accessTargets });
    mocks.movePosition.mockResolvedValue({ access_targets: accessTargets });
    mocks.remove.mockResolvedValue({ access_targets: accessTargets });
  });

  it("toggles the row carrying the given row id, not the row at that array index", async () => {
    const { result } = renderMutations();

    // Row id 20 is the second Model Target. Read positionally it would be the
    // out-of-range index 20 and the write would vanish without a trace.
    await result.current.handleToggleAccessTarget(20, false);

    await waitFor(() => {
      expect(mocks.update).toHaveBeenCalledExactlyOnceWith(8, 20, { is_enabled: false });
    });
    expect(mocks.toastError).not.toHaveBeenCalled();
  });

  it("does not act on a different row when a row id happens to be a valid index", async () => {
    const { result } = renderMutations();

    // Row id 1 is the third row (connection 12). Index 1 is the second Model
    // Target (row id 20) — a positional lookup would send row 20 to the server
    // while the operator acted on row 1.
    await result.current.handleDeleteAccessTarget(1);

    await waitFor(() => {
      expect(mocks.remove).toHaveBeenCalledExactlyOnceWith(8, 1);
    });
  });

  it("moves by row id and keeps the destination index untouched", async () => {
    const { result } = renderMutations();

    await result.current.handleMoveAccessTarget(9, 0);

    await waitFor(() => {
      expect(mocks.movePosition).toHaveBeenCalledExactlyOnceWith(8, 9, 0);
    });
  });

  it("reports an unresolvable row id instead of silently doing nothing", async () => {
    const { result } = renderMutations();

    await result.current.handleToggleAccessTarget(4242, true);

    expect(mocks.update).not.toHaveBeenCalled();
    expect(mocks.toastError).toHaveBeenCalledOnce();
  });
});
