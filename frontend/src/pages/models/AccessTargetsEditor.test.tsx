import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { zhCNMessages } from "@/i18n/messages";
import type { Connection, ModelAccessTarget, ModelConfigListItem } from "@/lib/types";
import { AccessTargetsEditor } from "./AccessTargetsEditor";

// ID domains must stay distinct: target row IDs (50x), connection IDs (90x)
// and target model IDs (string model ids) never overlap.
const targetModelId = "child-model";
const modelTarget: ModelAccessTarget = {
  id: 501,
  target_type: "model",
  target_model_id: targetModelId,
  connection_id: null,
  terminal_target_id: null,
  position: 2,
  is_enabled: true,
  target_model: null,
  connection: null,
  terminal_target: null,
  created_at: "2026-08-08T00:00:00Z",
  updated_at: "2026-08-08T00:00:00Z",
};

function createTerminalTarget(rowId: number, connectionId: number, position: number, enabled = true): ModelAccessTarget {
  return {
    id: rowId,
    target_type: "connection",
    target_model_id: null,
    connection_id: connectionId,
    terminal_target_id: connectionId,
    position,
    is_enabled: enabled,
    target_model: null,
    connection: null,
    terminal_target: null,
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-08T00:00:00Z",
  };
}

const terminalA = createTerminalTarget(502, 901, 0);
const terminalB = createTerminalTarget(503, 902, 1);

function createConnection(id: number, name: string): Connection {
  return {
    id,
    profile_id: 1,
    model_config_id: 7,
    api_family: "openai",
    endpoint_id: id + 100,
    endpoint: {
      id: id + 100,
      name: `endpoint-${id}`,
      base_url: `https://upstream-${id}.example`,
      has_api_key: true,
      api_key_fingerprint: "fp_v1_ab12cd34ef56",
      api_key_updated_at: "2026-08-08T00:00:00Z",
      config_revision: 1,
      created_at: "2026-08-08T00:00:00Z",
      updated_at: "2026-08-08T00:00:00Z",
    },
    is_active: true,
    name,
    priority: 0,
    auth_type: null,
    custom_headers: null,
    custom_request_parameters: null,
    openai_text_capability: "dual_native",
    openai_image_capability: null,
    pricing_template_id: 3,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-08T00:00:00Z",
  };
}

const modelOptions: ModelConfigListItem[] = [
  {
    id: 9,
    profile_id: 1,
    api_family: "openai",
    model_id: targetModelId,
    display_name: "Child Model",
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    loadbalance_strategy_id: 11,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-08T00:00:00Z",
  },
];

function renderEditor(props: Partial<Parameters<typeof AccessTargetsEditor>[0]> = {}) {
  const handlers = {
    onAddTarget: vi.fn(),
    onCreateConnection: vi.fn(),
    onDeleteTarget: vi.fn(),
    onEditConnection: vi.fn(),
    onMoveTarget: vi.fn(),
    onToggleTarget: vi.fn(),
  };
  const utils = render(
    <LocaleProvider>
      <AccessTargetsEditor
        apiFamilyLabel="openai"
        accessTargets={[terminalA, modelTarget, terminalB]}
        modelOptions={modelOptions}
        connectionOptions={[createConnection(901, "Terminal A"), createConnection(902, "Terminal B")]}
        {...handlers}
        {...props}
      />
    </LocaleProvider>,
  );
  return { ...utils, handlers };
}

describe("AccessTargetsEditor mixed ordering", () => {
  it("renders one mixed list sorted by (position, id) with global 1..N numbering", () => {
    renderEditor();
    const rows = screen.getAllByTestId(/^access-target-\d+$/);
    expect(rows.map((row) => row.getAttribute("data-testid"))).toEqual([
      "access-target-502",
      "access-target-503",
      "access-target-501",
    ]);
    expect(within(rows[0]).getByText("终端目标")).toBeTruthy();
    expect(within(rows[0]).getByText("1")).toBeTruthy();
    expect(within(rows[1]).getByText("2")).toBeTruthy();
    expect(within(rows[2]).getByText("模型目标")).toBeTruthy();
    expect(within(rows[2]).getByText("3")).toBeTruthy();
  });

  it("holds a reorder as a draft and commits it with row id and global to_index", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();

    // Drag the model row (global position 3) onto the second row.
    fireEvent.dragStart(screen.getByTestId("access-target-501"));
    fireEvent.dragOver(screen.getByTestId("access-target-503"));
    fireEvent.drop(screen.getByTestId("access-target-503"));

    // Nothing is written until the operator commits: dragging must not fire a
    // request per hop.
    expect(handlers.onMoveTarget).not.toHaveBeenCalled();
    expect(screen.getAllByTestId(/^access-target-\d+$/).map((row) => row.getAttribute("data-testid"))).toEqual([
      "access-target-502",
      "access-target-501",
      "access-target-503",
    ]);

    await user.click(screen.getByRole("button", { name: "保存顺序" }));
    await waitFor(() => {
      expect(handlers.onMoveTarget).toHaveBeenCalledWith(501, 1);
    });
  });

  it("reverts a drafted reorder without writing anything", () => {
    const { handlers } = renderEditor();

    fireEvent.dragStart(screen.getByTestId("access-target-501"));
    fireEvent.drop(screen.getByTestId("access-target-502"));
    fireEvent.click(screen.getByRole("button", { name: "撤销改动" }));

    expect(handlers.onMoveTarget).not.toHaveBeenCalled();
    expect(screen.getAllByTestId(/^access-target-\d+$/).map((row) => row.getAttribute("data-testid"))).toEqual([
      "access-target-502",
      "access-target-503",
      "access-target-501",
    ]);
  });

  it("toggles and deletes by target row id while connection edit passes the connection id", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();
    await user.click(within(screen.getByTestId("access-target-502")).getByRole("switch"));
    expect(handlers.onToggleTarget).toHaveBeenCalledExactlyOnceWith(502, false);

    await user.click(within(screen.getByTestId("access-target-503")).getByRole("button", { name: /更多操作/ }));
    await user.click(await screen.findByRole("menuitem", { name: /移除目标 2/ }));
    expect(handlers.onDeleteTarget).toHaveBeenCalledExactlyOnceWith(503);

    await user.click(within(screen.getByTestId("access-target-502")).getByRole("button", { name: /编辑 Terminal A/ }));
    expect(handlers.onEditConnection).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ id: 901 }));
  });

  it("keeps a read-only terminal row in the list but without connection-scoped actions", () => {
    renderEditor({ isConnectionTargetMutable: (connectionId) => connectionId !== 901 });
    const firstRow = screen.getByTestId("access-target-502");

    expect(within(firstRow).queryByRole("switch")).toBeNull();
    expect(within(firstRow).queryByRole("button", { name: /编辑/ })).toBeNull();
    expect(within(firstRow).queryByRole("button", { name: /更多操作/ })).toBeNull();
    // It keeps its ordering affordance and its enabled state stays legible.
    expect(within(firstRow).getByRole("button", { name: /拖动以调整目标/ })).toBeTruthy();
    expect(within(firstRow).getByText("已启用")).toBeTruthy();

    // The second terminal row stays fully editable.
    expect(within(screen.getByTestId("access-target-503")).getByRole("switch")).toBeEnabled();
  });

  it("keeps disabled rows in their authored position with their state legible", () => {
    renderEditor({
      accessTargets: [createTerminalTarget(502, 901, 0, false), modelTarget, terminalB],
    });
    const rows = screen.getAllByTestId(/^access-target-\d+$/);
    expect(rows.map((row) => row.getAttribute("data-testid"))).toEqual([
      "access-target-502",
      "access-target-503",
      "access-target-501",
    ]);
    expect(within(rows[0]).getByRole("switch")).not.toBeChecked();
  });

  it("renders no runtime or limit numbers for a model target", () => {
    renderEditor();
    const modelRow = screen.getByTestId("access-target-501");
    // Not applicable is an em dash with a reason, never a zero.
    const dashes = within(modelRow).getAllByText("—");
    expect(dashes.length).toBeGreaterThanOrEqual(2);
  });

  it("never presents per-type first labels or partition copy", () => {
    const modelsUi = zhCNMessages.modelsUi;
    expect(modelsUi.accessTargetsDescription).not.toMatch(/模型目标阶段|终端目标阶段/);
    expect(modelsUi.accessTargetsDescription).toMatch(/混合列表/);
  });
});
