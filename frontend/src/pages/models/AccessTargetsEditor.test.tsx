import { render, screen, within } from "@testing-library/react";
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
    expect(within(rows[0]).getByText(/终端目标/)).toBeTruthy();
    expect(within(rows[0]).getByText(/位置 1/)).toBeTruthy();
    expect(within(rows[1]).getByText(/位置 2/)).toBeTruthy();
    expect(within(rows[2]).getByText(/模型目标/)).toBeTruthy();
    expect(within(rows[2]).getByText(/位置 3/)).toBeTruthy();
  });

  it("moves rows across adjacent rows using the target row id and a global to_index", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();
    // Model row at global position 3 moves up across the terminal row above it.
    await user.click(within(screen.getByTestId("access-target-501")).getByRole("button", { name: /将目标 3 上移/ }));
    expect(handlers.onMoveTarget).toHaveBeenCalledExactlyOnceWith(501, 1);
    // Terminal row at global position 2 moves down across the model row below it.
    await user.click(within(screen.getByTestId("access-target-503")).getByRole("button", { name: /将目标 2 下移/ }));
    expect(handlers.onMoveTarget).toHaveBeenLastCalledWith(503, 2);
  });

  it("toggles and deletes by target row id while connection edit passes the connection id", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();
    await user.click(within(screen.getByTestId("access-target-502")).getByRole("switch"));
    expect(handlers.onToggleTarget).toHaveBeenCalledExactlyOnceWith(502, false);
    await user.click(within(screen.getByTestId("access-target-503")).getByRole("button", { name: /移除目标 2/ }));
    expect(handlers.onDeleteTarget).toHaveBeenCalledExactlyOnceWith(503);
    await user.click(within(screen.getByTestId("access-target-502")).getByRole("button", { name: /编辑 Terminal A/ }));
    expect(handlers.onEditConnection).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ id: 901 }));
  });

  it("disables first-row move up and last-row move down, and keeps read-only terminal rows movable", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor({ isConnectionTargetMutable: (connectionId) => connectionId !== 901 });
    const firstRow = screen.getByTestId("access-target-502");
    const lastRow = screen.getByTestId("access-target-501");
    expect(within(firstRow).getByRole("button", { name: /将目标 1 上移/ })).toBeDisabled();
    expect(within(lastRow).getByRole("button", { name: /将目标 3 下移/ })).toBeDisabled();
    // A read-only terminal row keeps its position controls but loses
    // connection-scoped actions (toggle, edit, delete).
    expect(within(firstRow).queryByRole("switch")).toBeNull();
    expect(within(firstRow).queryByRole("button", { name: /编辑/ })).toBeNull();
    expect(within(firstRow).queryByRole("button", { name: /移除目标 1/ })).toBeNull();
    expect(within(firstRow).getByRole("button", { name: /将目标 1 下移/ })).toBeEnabled();
    await user.click(within(firstRow).getByRole("button", { name: /将目标 1 下移/ }));
    expect(handlers.onMoveTarget).toHaveBeenCalledExactlyOnceWith(502, 1);
    // The second terminal row stays fully editable.
    const secondRow = screen.getByTestId("access-target-503");
    expect(within(secondRow).getByRole("switch")).toBeEnabled();
  });

  it("keeps disabled rows in their authored position with status copy", () => {
    renderEditor({
      accessTargets: [
        createTerminalTarget(502, 901, 0, false),
        modelTarget,
        terminalB,
      ],
    });
    const rows = screen.getAllByTestId(/^access-target-\d+$/);
    expect(rows.map((row) => row.getAttribute("data-testid"))).toEqual([
      "access-target-502",
      "access-target-503",
      "access-target-501",
    ]);
    expect(within(rows[0]).getByText(/已禁用/)).toBeTruthy();
  });

  it("never presents per-type first labels or partition copy", () => {
    const modelsUi = zhCNMessages.modelsUi;
    expect(modelsUi).not.toHaveProperty("priority");
    expect(modelsUi).not.toHaveProperty("terminalTargetsDescription");
    expect(modelsUi).not.toHaveProperty("modelFallbackTargetsDescription");
    expect(modelsUi.accessTargetsDescription).not.toMatch(/先在这里管理模型目标顺序/);
    expect(modelsUi.accessTargetsDescription).toMatch(/同一条顺序/);
    expect(zhCNMessages.modelDetail).not.toHaveProperty("dragToReorderConnection");
  });
});
