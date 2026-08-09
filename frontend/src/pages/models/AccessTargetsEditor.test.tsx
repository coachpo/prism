import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { zhCNMessages } from "@/i18n/messages";
import type { Connection, ModelAccessTargetMutation, ModelConfigListItem } from "@/lib/types";
import { AccessTargetsEditor } from "./AccessTargetsEditor";

const targetModelId = "child-model";
const modelTarget: ModelAccessTargetMutation = {
  target_type: "model",
  target_model_id: targetModelId,
  position: 0,
  is_enabled: true,
};

function createTerminalTarget(connectionId: number, position: number, enabled = true): ModelAccessTargetMutation {
  return {
    target_type: "connection",
    connection_id: connectionId,
    position,
    is_enabled: enabled,
  };
}

const terminalA = createTerminalTarget(901, 1);
const terminalB = createTerminalTarget(902, 2);

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
      masked_api_key: null,
      position: 0,
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
    routing_summary: null,
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
        accessTargets={[modelTarget, terminalA, terminalB]}
        modelOptions={modelOptions}
        connectionOptions={[createConnection(901, "Terminal A"), createConnection(902, "Terminal B")]}
        {...handlers}
        {...props}
      />
    </LocaleProvider>,
  );
  return { ...utils, handlers };
}

describe("AccessTargetsEditor two-stage routing", () => {
  it("renders model-first and terminal-fallback stages with stage-local numbering", () => {
    renderEditor();

    const modelStage = screen.getByTestId("access-target-stage-model_targets");
    const terminalStage = screen.getByTestId("access-target-stage-terminal_targets");
    expect(within(modelStage).getByText("模型目标（先尝试）")).toBeInTheDocument();
    expect(within(modelStage).getByText(/位置 1/)).toBeInTheDocument();
    expect(within(terminalStage).getByText("终端目标（无模型候选时回落）")).toBeInTheDocument();
    expect(within(terminalStage).getByText(/位置 1/)).toBeInTheDocument();
    expect(within(terminalStage).getByText(/位置 2/)).toBeInTheDocument();
    expect(screen.getByTestId("model-target-row-child-model")).toBeInTheDocument();
    expect(screen.getByTestId("terminal-target-card-901")).toBeInTheDocument();
    expect(screen.getByTestId("terminal-target-card-902")).toBeInTheDocument();
  });

  it("moves rows only within their runtime stage", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();

    await user.click(within(screen.getByTestId("terminal-target-card-902")).getByRole("button", { name: /上移目标 Terminal B/ }));
    expect(handlers.onMoveTarget).toHaveBeenCalledExactlyOnceWith(2, 1);
    expect(handlers.onMoveTarget).not.toHaveBeenCalledWith(2, 0);
  });

  it("toggles and deletes by the mutation source index while editing by connection id", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();
    const terminal = screen.getByTestId("terminal-target-card-901");

    await user.click(within(terminal).getByRole("switch"));
    expect(handlers.onToggleTarget).toHaveBeenCalledExactlyOnceWith(1, false);
    await user.click(within(terminal).getByRole("button", { name: /删除目标 Terminal A/ }));
    expect(handlers.onDeleteTarget).toHaveBeenCalledExactlyOnceWith(1);
    await user.click(within(terminal).getByRole("button", { name: /编辑 Terminal A/ }));
    expect(handlers.onEditConnection).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ id: 901 }));
  });

  it("keeps read-only terminal rows movable but removes connection actions", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor({ isConnectionTargetMutable: (connectionId) => connectionId !== 901 });
    const firstTerminal = screen.getByTestId("terminal-target-card-901");

    expect(within(firstTerminal).queryByRole("switch")).toBeNull();
    expect(within(firstTerminal).queryByRole("button", { name: /编辑/ })).toBeNull();
    expect(within(firstTerminal).queryByRole("button", { name: /删除目标/ })).toBeNull();
    expect(within(firstTerminal).getByRole("button", { name: /下移目标 Terminal A/ })).toBeEnabled();
    await user.click(within(firstTerminal).getByRole("button", { name: /下移目标 Terminal A/ }));
    expect(handlers.onMoveTarget).toHaveBeenCalledExactlyOnceWith(1, 2);
  });

  it("keeps disabled rows in authored stage position and uses two-stage copy", () => {
    renderEditor({ accessTargets: [modelTarget, createTerminalTarget(901, 1, false), terminalB] });
    const terminalStage = screen.getByTestId("access-target-stage-terminal_targets");
    expect(within(terminalStage).getByText(/位置 1/)).toBeInTheDocument();
    expect(within(screen.getByTestId("terminal-target-card-901")).getByText("已禁用")).toBeInTheDocument();
    expect(zhCNMessages.modelsUi.accessTargetsDescription).toMatch(/模型目标阶段先执行/);
    expect(zhCNMessages.modelsUi.accessTargetsDescription).toMatch(/终端目标阶段在其后回落/);
  });
});
