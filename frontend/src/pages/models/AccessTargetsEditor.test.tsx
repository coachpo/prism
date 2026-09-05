import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { zhCNMessages } from "@/i18n/messages";
import type {
  Connection,
  LoadbalanceCurrentStateItem,
  ModelAccessTarget,
  ModelAccessTargetModelSummary,
  ModelConfigListItem,
} from "@/lib/types";
import { AccessTargetsEditor } from "./AccessTargetsEditor";

const ACCESS_TARGET_DRAG_TYPE = "application/x-prism-access-target";

/** jsdom 不提供 DataTransfer；表格按 MIME 过滤拖放，所以事件必须带上它。 */
function createDragDataTransfer() {
  const store = new Map<string, string>([[ACCESS_TARGET_DRAG_TYPE, ""]]);
  return {
    dropEffect: "none",
    effectAllowed: "all",
    get types() {
      return [...store.keys()];
    },
    setData: (type: string, value: string) => {
      store.set(type, value);
    },
    getData: (type: string) => store.get(type) ?? "",
    clearData: () => store.clear(),
    setDragImage: () => {},
  };
}

/** 从把手按下 → 拖起 → 放到目标行，与真实交互路径一致。 */
function dragRowOnto(fromTestId: string, toTestId: string) {
  const fromRow = screen.getByTestId(fromTestId);
  const dataTransfer = createDragDataTransfer();
  fireEvent.pointerDown(
    within(fromRow).getByRole("button", { name: /拖动以调整目标/ }),
  );
  fireEvent.dragStart(fromRow, { dataTransfer });
  const toRow = screen.getByTestId(toTestId);
  fireEvent.dragOver(toRow, { dataTransfer });
  fireEvent.drop(toRow, { dataTransfer });
}

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

function createTerminalTarget(
  rowId: number,
  connectionId: number,
  position: number,
  enabled = true,
): ModelAccessTarget {
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
      api_key_fingerprint: "test-fingerprint-ab12cd34ef56",
      api_key_updated_at: "2026-08-08T00:00:00Z",
      config_revision: 1,
      created_at: "2026-08-08T00:00:00Z",
      updated_at: "2026-08-08T00:00:00Z",
    },
    is_active: true,
    name,
    priority: 0,
    auth_type: null,
    upstream_model_id: `provider/Model-${id}`,
    custom_headers: null,
    custom_headers_redacted: null,
    custom_request_parameters: null,
    routing_schedule: null,
    routing_schedule_state: null,
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

function observedRuntime(connectionId: number): LoadbalanceCurrentStateItem {
  return {
    connection_id: connectionId,
    window_started_at: null,
    window_request_count: 0,
    in_flight_non_stream: 0,
    in_flight_stream: 0,
    cycle_retry_attempts: 0,
    cumulative_retry_attempts: 0,
    next_retry_at: null,
    last_retry_delay_ms: 0,
    ban_mode: "off",
    banned_until_at: null,
    last_failure_kind: null,
    last_success_at: "2026-08-08T08:00:00Z",
    last_success_response_headers_latency_ms: 412,
    state: "available",
    created_at: "2026-08-08T07:00:00Z",
    updated_at: "2026-08-08T08:00:00Z",
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
    direct_request_enabled: false,
    loadbalance_strategy_id: 11,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
    incoming_model_target_count: 1,
    configuration_warnings: [],
    created_at: "2026-08-08T00:00:00Z",
    updated_at: "2026-08-08T00:00:00Z",
  },
];

function renderEditor(
  props: Partial<Parameters<typeof AccessTargetsEditor>[0]> = {},
) {
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
      <TooltipProvider>
        <AccessTargetsEditor
          apiFamilyLabel="openai"
          accessTargets={[terminalA, modelTarget, terminalB]}
          modelOptions={modelOptions}
          connectionOptions={[
            createConnection(901, "Terminal A"),
            createConnection(902, "Terminal B"),
          ]}
          {...handlers}
          {...props}
        />
      </TooltipProvider>
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
    expect(
      within(rows[0]).getByTitle("上游模型 ID: provider/Model-901"),
    ).toBeTruthy();
    expect(within(rows[0]).getByText("1")).toBeTruthy();
    expect(within(rows[1]).getByText("2")).toBeTruthy();
    expect(within(rows[2]).getByText("模型目标")).toBeTruthy();
    expect(within(rows[2]).queryByText(/provider\/Model-/)).toBeNull();
    expect(within(rows[2]).getByText("3")).toBeTruthy();
  });

  it("holds a reorder as a draft and commits it with row id and global to_index", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();

    // Drag the model row (global position 3) onto the second row.
    dragRowOnto("access-target-501", "access-target-503");

    // Nothing is written until the operator commits: dragging must not fire a
    // request per hop.
    expect(handlers.onMoveTarget).not.toHaveBeenCalled();
    expect(
      screen
        .getAllByTestId(/^access-target-\d+$/)
        .map((row) => row.getAttribute("data-testid")),
    ).toEqual(["access-target-502", "access-target-501", "access-target-503"]);

    await user.click(screen.getByRole("button", { name: "保存顺序" }));
    await waitFor(() => {
      expect(handlers.onMoveTarget).toHaveBeenCalledWith(501, 1);
    });
  });

  it("reverts a drafted reorder without writing anything", () => {
    const { handlers } = renderEditor();

    dragRowOnto("access-target-501", "access-target-502");
    fireEvent.click(screen.getByRole("button", { name: "撤销改动" }));

    expect(handlers.onMoveTarget).not.toHaveBeenCalled();
    expect(
      screen
        .getAllByTestId(/^access-target-\d+$/)
        .map((row) => row.getAttribute("data-testid")),
    ).toEqual(["access-target-502", "access-target-503", "access-target-501"]);
  });

  it("toggles and deletes by target row id while connection edit passes the connection id", async () => {
    const user = userEvent.setup();
    const { handlers } = renderEditor();
    await user.click(
      within(screen.getByTestId("access-target-502")).getByRole("switch"),
    );
    expect(handlers.onToggleTarget).toHaveBeenCalledExactlyOnceWith(502, false);

    await user.click(
      within(screen.getByTestId("access-target-503")).getByRole("button", {
        name: /更多操作/,
      }),
    );
    await user.click(
      await screen.findByRole("menuitem", { name: /移除目标 Terminal B/ }),
    );
    // 菜单项本身不是确认：物理删除只能来自确认对话框。
    expect(handlers.onDeleteTarget).not.toHaveBeenCalled();
    await user.click(
      await screen.findByTestId("delete-access-target-confirm"),
    );
    expect(handlers.onDeleteTarget).toHaveBeenCalledExactlyOnceWith(503);

    await user.click(
      within(screen.getByTestId("access-target-502")).getByRole("button", {
        name: /编辑 Terminal A/,
      }),
    );
    expect(handlers.onEditConnection).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ id: 901 }),
    );
  });

  it("keeps a read-only terminal row in the list but without connection-scoped actions", () => {
    renderEditor({
      isConnectionTargetMutable: (connectionId) => connectionId !== 901,
    });
    const firstRow = screen.getByTestId("access-target-502");

    expect(within(firstRow).queryByRole("switch")).toBeNull();
    expect(within(firstRow).queryByRole("button", { name: /编辑/ })).toBeNull();
    expect(
      within(firstRow).queryByRole("button", { name: /更多操作/ }),
    ).toBeNull();
    // It keeps its ordering affordance and its enabled state stays legible.
    expect(
      within(firstRow).getByRole("button", { name: /拖动以调整目标/ }),
    ).toBeTruthy();
    expect(within(firstRow).getByText("已启用")).toBeTruthy();

    // The second terminal row stays fully editable.
    expect(
      within(screen.getByTestId("access-target-503")).getByRole("switch"),
    ).toBeEnabled();
  });

  it("keeps disabled rows in their authored position with their state legible", () => {
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
    expect(within(rows[0]).getByRole("switch")).not.toBeChecked();
  });

  it("renders no runtime or limit numbers for a model target", () => {
    renderEditor();
    const modelRow = screen.getByTestId("access-target-501");
    // Not applicable is an em dash with a reason, never a zero.
    const dashes = within(modelRow).getAllByText("—");
    expect(dashes.length).toBeGreaterThanOrEqual(2);
  });
});

describe("AccessTargetsEditor capability column keeps absence distinguishable", () => {
  const copy = zhCNMessages.modelsUi;

  it("shows the capability basis explanation from the question-mark trigger", async () => {
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );

    try {
      const user = userEvent.setup();
      renderEditor({ accessTargets: [terminalA] });

      await user.hover(
        screen.getByRole("button", { name: copy.targetColumnCapabilityBasis }),
      );

      const tooltip = await screen.findByRole("tooltip");
      expect(tooltip.textContent).toContain(copy.targetColumnCapabilityBasis);
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("gives every absent capability its own reason instead of one bare dash", () => {
    renderEditor({
      accessTargets: [terminalA, modelTarget, terminalB],
      // Terminal B's connection is withheld: it is not in this model's
      // same-family option set, which is not the same as "declares nothing".
      connectionOptions: [
        {
          ...createConnection(901, "Terminal A"),
          openai_text_capability: null,
          openai_image_capability: null,
        },
      ],
    });

    // A Model Target does not declare a capability of its own.
    expect(
      within(screen.getByTestId("access-target-501")).getByText(
        copy.targetCapabilityNotApplicableModel,
      ),
    ).toBeTruthy();
    // A Terminal Target that declares neither text nor image capability.
    expect(
      within(screen.getByTestId("access-target-502")).getByText(
        copy.targetCapabilityUnknown,
      ),
    ).toBeTruthy();
    // A Terminal Target whose connection could not be resolved in this scope.
    expect(
      within(screen.getByTestId("access-target-503")).getAllByText(
        copy.targetConnectionOutOfScope,
      ).length,
    ).toBeGreaterThan(0);
  });

  it("renders an image-only Terminal Target instead of blanking it", () => {
    renderEditor({
      accessTargets: [terminalA],
      connectionOptions: [
        {
          ...createConnection(901, "Terminal A"),
          openai_text_capability: null,
          openai_image_capability: "generations_and_edits",
        },
      ],
    });

    const row = screen.getByTestId("access-target-502");
    expect(
      within(row).getByText(copy.openaiImageOperationsGenerationsAndEdits),
    ).toBeTruthy();
    expect(within(row).queryByText(copy.targetCapabilityUnknown)).toBeNull();
  });

  it("marks a non-OpenAI family as not applicable rather than as missing data", () => {
    renderEditor({
      apiFamilyLabel: "anthropic",
      accessTargets: [terminalA],
      connectionOptions: [
        {
          ...createConnection(901, "Terminal A"),
          api_family: "anthropic",
          openai_text_capability: null,
          openai_image_capability: null,
        },
      ],
    });

    expect(
      within(screen.getByTestId("access-target-502")).getByText(
        /不使用 OpenAI 能力矩阵/,
      ),
    ).toBeTruthy();
  });
});

describe("AccessTargetsEditor runtime column keeps absence distinguishable", () => {
  const routingCopy = zhCNMessages.routing;

  it("shows a read failure as a failure, not as an unobserved target", () => {
    renderEditor({
      accessTargets: [terminalA],
      currentStateFailure: {
        message: "upstream unreachable",
        staleData: false,
      },
    });

    const row = screen.getByTestId("access-target-502");
    expect(within(row).getByText(routingCopy.runtimeReadFailed)).toBeTruthy();
    expect(
      within(row).queryByText(routingCopy.noRuntimeObservation),
    ).toBeNull();
  });

  it("separates a partially observed row from a never-observed one", () => {
    renderEditor({
      accessTargets: [terminalA, terminalB],
      currentStateGapByConnectionId: new Map([
        [901, "partial" as const],
        [902, "unobserved" as const],
      ]),
      currentStateCompleteness: {
        state: "partial",
        complete: false,
        configured_target_count: 2,
        observed_target_count: 1,
        unobserved_target_count: 1,
        observed_subset_counts: {},
        hasMore: false,
      },
    });

    expect(
      within(screen.getByTestId("access-target-502")).getByText(
        new RegExp(routingCopy.runtimePartialObservation),
      ),
    ).toBeTruthy();
    expect(
      within(screen.getByTestId("access-target-503")).getByText(
        routingCopy.noRuntimeObservation,
      ),
    ).toBeTruthy();
  });

  it("does not claim a switched-off row is unobserved", () => {
    renderEditor({
      accessTargets: [createTerminalTarget(502, 901, 0, false)],
      currentStateCompleteness: {
        state: "ready",
        complete: true,
        configured_target_count: 0,
        observed_target_count: 0,
        unobserved_target_count: 0,
        observed_subset_counts: {},
        hasMore: false,
      },
    });

    const row = screen.getByTestId("access-target-502");
    // The read model requires an enabled access-target edge, so this row is out
    // of scope for observation rather than never observed.
    expect(
      within(row).getByText(routingCopy.runtimeOutOfCohortReason),
    ).toBeTruthy();
    expect(
      within(row).queryByText(routingCopy.noRuntimeObservation),
    ).toBeNull();
  });

  it("does not report an uncounted in-flight gauge as a measured zero", () => {
    renderEditor({
      accessTargets: [terminalA],
      currentStateByConnectionId: new Map([[901, observedRuntime(901)]]),
      currentStateCompleteness: {
        state: "ready",
        complete: true,
        configured_target_count: 1,
        observed_target_count: 1,
        unobserved_target_count: 0,
        observed_subset_counts: {},
        hasMore: false,
      },
    });

    const row = screen.getByTestId("access-target-502");
    // The fixture connection sets no in-flight limits, so the runtime never
    // increments these counters; a literal 0 would read as a measurement.
    expect(within(row).getByText(routingCopy.noCooldown)).toBeTruthy();
    expect(within(row).queryByText("0 / 0")).toBeNull();
    expect(within(row).getAllByText("—").length).toBeGreaterThanOrEqual(2);
  });

  it("never presents per-type first labels or partition copy", () => {
    const modelsUi = zhCNMessages.modelsUi;
    expect(modelsUi.accessTargetsDescription).not.toMatch(
      /模型目标阶段|终端目标阶段/,
    );
    expect(modelsUi.accessTargetsDescription).toMatch(/混合列表/);
  });
});

describe("AccessTargetsEditor model target detail entry", () => {
  const targetModelSummary: ModelAccessTargetModelSummary = {
    id: 17,
    profile_id: 1,
    api_family: "openai",
    model_id: targetModelId,
    display_name: null,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    direct_request_enabled: false,
    incoming_model_target_count: 0,
    loadbalance_strategy_id: 11,
    is_enabled: true,
  };
  // ID domains stay distinct on purpose: the row id (501) and the option-list
  // config id (9) both differ from the only navigable identity, the linked
  // config record's own id (17).
  const linkedModelTarget: ModelAccessTarget = {
    ...modelTarget,
    target_model: targetModelSummary,
  };

  function openRowMenu(rowTestId: string) {
    return within(screen.getByTestId(rowTestId)).getByRole("button", {
      name: /更多操作/,
    });
  }

  it("navigates by keyboard to the linked config id, never the row or option id", async () => {
    const user = userEvent.setup();
    const onViewViewModelTargetDetail = vi.fn();
    renderEditor({
      accessTargets: [linkedModelTarget],
      onViewModelTargetDetail: onViewViewModelTargetDetail,
    });

    openRowMenu("access-target-501").focus();
    await user.keyboard("{Enter}");
    const item = await screen.findByRole("menuitem", {
      name: "查看模型配置 Child Model 的详情",
    });
    if (item.getAttribute("data-highlighted") == null) {
      await user.keyboard("{ArrowDown}");
    }
    await user.keyboard("{Enter}");

    expect(onViewViewModelTargetDetail).toHaveBeenCalledExactlyOnceWith(17);
  });

  it("keeps the entry out of terminal-target menus and their actions intact", async () => {
    const user = userEvent.setup();
    const onViewViewModelTargetDetail = vi.fn();
    renderEditor({
      accessTargets: [terminalA, linkedModelTarget, terminalB],
      onCopyTarget: vi.fn(),
      onGeneratePricing: vi.fn(),
      onRefreshRuntimeState: vi.fn(),
      onViewModelTargetDetail: onViewViewModelTargetDetail,
    });

    await user.click(openRowMenu("access-target-502"));
    let menu = await screen.findByRole("menu");
    expect(
      within(menu).queryByRole("menuitem", { name: /查看模型/ }),
    ).toBeNull();
    expect(
      within(menu).getByRole("menuitem", { name: /复制终端目标 Terminal A/ }),
    ).toBeTruthy();
    await user.keyboard("{Escape}");

    await user.click(openRowMenu("access-target-501"));
    menu = await screen.findByRole("menu");
    expect(
      within(menu).getByRole("menuitem", {
        name: /查看模型配置 Child Model 的详情/,
      }),
    ).toBeTruthy();
    expect(onViewViewModelTargetDetail).not.toHaveBeenCalled();
  });

  it("hides the entry when target_model is missing instead of building a dead link", async () => {
    const user = userEvent.setup();
    const onViewViewModelTargetDetail = vi.fn();
    renderEditor({ onViewModelTargetDetail: onViewViewModelTargetDetail });

    await user.click(openRowMenu("access-target-501"));
    const menu = await screen.findByRole("menu");
    expect(
      within(menu).queryByRole("menuitem", { name: /查看模型/ }),
    ).toBeNull();
    // The row keeps its ordinary actions; nothing navigates.
    expect(
      within(menu).getByRole("menuitem", { name: /移除目标/ }),
    ).toBeTruthy();
    expect(onViewViewModelTargetDetail).not.toHaveBeenCalled();
  });

  it("renders no entry when the host provides no navigation callback", () => {
    renderEditor({ accessTargets: [linkedModelTarget] });

    expect(openRowMenu("access-target-501")).toBeTruthy();
    // Without a callback there is no menu content beyond removal, so the host
    // contract stays optional rather than growing a dead item.
    expect(screen.queryByTestId("model-view-detail-action")).toBeNull();
  });

  it("keeps the entry usable for a disabled model target", async () => {
    const user = userEvent.setup();
    const onViewViewModelTargetDetail = vi.fn();
    renderEditor({
      accessTargets: [{ ...linkedModelTarget, is_enabled: false }],
      onViewModelTargetDetail: onViewViewModelTargetDetail,
    });
    const row = screen.getByTestId("access-target-501");
    expect(within(row).getByRole("switch")).not.toBeChecked();

    await user.click(openRowMenu("access-target-501"));
    await user.click(
      await screen.findByRole("menuitem", {
        name: /查看模型配置 Child Model 的详情/,
      }),
    );

    expect(onViewViewModelTargetDetail).toHaveBeenCalledExactlyOnceWith(17);
  });
});
