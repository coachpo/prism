import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { ExportRenderResponse, ExportSourceResponse } from "./exportTypes";
import { ModelExportPage } from "./ModelExportPage";
import {
  bindModelPi,
  clearModelPiOverride,
  fetchModelExportSource,
  putModelPiOverride,
  renderModelExport,
  searchModelPiCatalog,
  unbindModelPi,
} from "@/lib/api/modelExport";
import { ExportResultSheet } from "./ExportResultSheet";
import { ExportKeyDialog } from "./ExportKeyDialog";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => ({}),
}));

vi.mock("@/hooks/useTimezone", () => ({
  useTimezone: () => ({
    format: (value: string) => value,
    loading: false,
    refresh: vi.fn(),
    timezone: "UTC",
  }),
}));

const sourceFixture = (
  overrides?: Partial<ExportSourceResponse>,
): ExportSourceResponse => ({
  target_version: "0.84.3",
  catalog: {
    status: "fresh",
    revision: "rev-1",
    minimum_version: "0.80.7",
    etag: "etag-1",
  },
  source_digest: "a".repeat(64),
  models: [
    {
      model_config_id: 3,
      model_id: "gpt-x",
      api_family: "openai",
      display_name: "GPT Friendly",
      is_enabled: true,
      selectable: true,
      openai_accepted_format: "dual_native",
      pi_api: "openai-responses",
      prism_metadata: {},
      merged_metadata: { name: "gpt-x", reasoning: true },
      metadata_provenance: {},
      missing_metadata: [],
      completeness: {
        metadata_fields: {
          name: true,
          reasoning: true,
          thinkingLevelMap: false,
        },
        cost_exportable: true,
      },
      targets: [
        {
          terminal_target_id: 11,
          position: 0,
          endpoint_id: 21,
          endpoint_name: "primary",
        },
      ],
      price_risk: { exportable: true },
      warnings: ["pi_source_fields_dropped"],
      pi_candidates: [
        {
          provider_id: "openai",
          model_id: "gpt-x",
          api: "openai-responses",
          name: "GPT X",
          context_window: 200000,
        },
      ],
      pi_selected: {
        provider_id: "openai",
        model_id: "gpt-x",
        api: "openai-responses",
      },
      candidate_status: "single",
      pi_binding_status: "bound",
      pi_binding_renderable: true,
      pi_bind_source: "single_candidate",
      pi_binding_prism_model_id: "gpt-x",
      pi_binding_dropped_fields: ["compat.openRouterRouting"],
      pi_binding_source: {
        name: "GPT X",
        reasoning: true,
        input: ["text", "image"],
        context_window: 200000,
        max_tokens: 8192,
        thinking_level_map: { low: "low" },
        compat: { supportsStore: true },
      },
      pi_binding_override: {
        name: null,
        reasoning: false,
        input: null,
        context_window: null,
        max_tokens: null,
        thinking_level_map: null,
        compat: null,
      },
      pi_binding_effective: {
        name: "GPT X",
        reasoning: false,
        input: ["text", "image"],
        context_window: 200000,
        max_tokens: 8192,
        thinking_level_map: { low: "low" },
        compat: { supportsStore: true },
      },
    },
    {
      model_config_id: 5,
      model_id: "glm-5.2",
      api_family: "openai",
      display_name: null,
      is_enabled: true,
      selectable: true,
      openai_accepted_format: "chat_completions_only",
      pi_api: "openai-completions",
      prism_metadata: {},
      merged_metadata: {},
      metadata_provenance: {},
      missing_metadata: ["name"],
      completeness: {
        metadata_fields: { name: false, reasoning: false },
        cost_exportable: false,
      },
      targets: [],
      price_risk: { exportable: false, warning_codes: ["price_no_template"] },
      warnings: ["metadata_incomplete"],
      pi_candidates: [],
      candidate_status: "not_in_catalog",
      pi_binding_status: "unbound",
      pi_binding_renderable: false,
    },
  ],
  ...overrides,
});

vi.mock("@/lib/api/modelExport", () => ({
  fetchModelExportSource: vi.fn(() => Promise.resolve(sourceFixture())),
  renderModelExport: vi.fn(() =>
    Promise.resolve({
      target_version: "0.84.3",
      content: "{}\n",
      content_sha256: "c".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
      warnings: ["pi_source_fields_dropped"],
    }),
  ),
  bindModelPi: vi.fn(),
  searchModelPiCatalog: vi.fn(() =>
    Promise.resolve({
      query: "gpt",
      api: "openai-responses",
      limit: 20,
      total: 0,
      returned: 0,
      truncated: false,
      selected: false,
      catalog: { status: "fresh", revision: "rev-1" },
      fetched_at: "2026-08-30T00:00:00Z",
      export_identity: {
        model_config_id: 3,
        model_id: "gpt-x",
        api: "openai-responses",
        provider_id_source: "operator_input",
      },
      results: [],
    }),
  ),
  refreshModelPiPreview: vi.fn(),
  refreshModelPiCommit: vi.fn(),
  putModelPiOverride: vi.fn(),
  clearModelPiOverride: vi.fn(),
  unbindModelPi: vi.fn(),
}));

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <TooltipProvider>
          <ModelExportPage />
        </TooltipProvider>
      </LocaleProvider>
    </QueryClientProvider>,
  );
}

async function openBindingMenu(
  user: ReturnType<typeof userEvent.setup>,
  row: HTMLElement,
) {
  await user.click(within(row).getByRole("button", { name: "绑定操作" }));
}

describe("ModelExportPage Pi-only", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchModelExportSource).mockImplementation(() =>
      Promise.resolve(sourceFixture()),
    );
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
    Object.defineProperties(HTMLElement.prototype, {
      hasPointerCapture: {
        configurable: true,
        value: vi.fn(() => false),
      },
      releasePointerCapture: {
        configurable: true,
        value: vi.fn(),
      },
      setPointerCapture: {
        configurable: true,
        value: vi.fn(),
      },
    });
  });

  it("adopts selectable models on first load", async () => {
    renderPage();
    await screen.findByTestId("export-row-3");
    expect(screen.getByRole("checkbox", { name: "gpt-x" })).toBeChecked();
  });

  it("does not treat an optional missing thinking map as incomplete metadata", async () => {
    renderPage();
    await screen.findByTestId("export-row-3");
    expect(screen.getByTestId("export-risk-metadata-count")).toHaveTextContent(
      "1",
    );
  });

  it("shows catalog status and a bound candidate's coordinate", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText(/目录状态/);
    expect(screen.getAllByText("a".repeat(64)).length).toBeGreaterThan(0);
    expect(screen.getByText("openai/gpt-x (openai-responses)")).toBeVisible();
    expect(screen.getByText(/compat\.openRouterRouting/)).toBeVisible();
    expect(screen.getByText(/绑定时 Prism ID/)).toBeVisible();
    expect(
      screen.getByText(/pi\.dev 来源包含不安全或不受支持的字段/),
    ).toBeVisible();
    const row = screen.getByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "更换来源" }));
    expect(
      within(screen.getByRole("dialog")).getByText(
        (_, element) => element?.tagName === "P" && element.textContent?.includes("当前绑定的 Prism ID") === true,
      ),
    ).toBeVisible();
  });

  it("uses the backend unique-candidate path for an unbound single candidate", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture();
    fixture.models = [
      {
        ...fixture.models[0],
        pi_selected: null,
        pi_binding_status: "unbound",
        pi_binding_renderable: false,
        pi_binding_source: null,
        pi_binding_override: null,
        pi_binding_effective: null,
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);
    vi.mocked(bindModelPi).mockResolvedValue({
      bound: true,
      bind_source: "single_candidate",
      provider_id: "openai",
      catalog_model_id: "gpt-x",
      api: "openai-responses",
      catalog_revision: "rev-1",
      source: null,
      override: null,
      effective: null,
    });

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    expect(within(row).getByText("单个默认候选")).toBeVisible();
    await user.click(within(row).getByRole("button", { name: "绑定来源" }));

    const apply = screen.getByRole("button", { name: "应用绑定" });
    expect(apply).toBeEnabled();
    expect(
      within(screen.getByRole("dialog")).getByText(
        (_, element) =>
          element?.textContent === "已丢弃的不安全目录字段: 无",
      ),
    ).toBeVisible();
    await user.click(apply);

    await waitFor(() =>
      expect(bindModelPi).toHaveBeenCalledWith(3, {
        expected_catalog_revision: "rev-1",
        expected_prism_model_id: "gpt-x",
        expected_pi_api: "openai-responses",
      }),
    );
  });

  it("binds an explicitly chosen coordinate and never auto-selects a search hit", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture();
    fixture.catalog = { ...fixture.catalog, status: "stale" };
    fixture.models = [
      {
        ...fixture.models[0],
        model_id: "codex/gpt-x",
        pi_api: "openai-responses",
        pi_candidates: [],
        candidate_status: "not_in_catalog",
        pi_selected: null,
        pi_binding_status: "unbound",
        pi_binding_renderable: false,
        pi_binding_source: null,
        pi_binding_override: null,
        pi_binding_effective: null,
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);
    vi.mocked(searchModelPiCatalog).mockResolvedValue({
      query: "GPT-X",
      api: "openai-responses",
      limit: 20,
      total: 1,
      returned: 1,
      truncated: false,
      selected: false,
      catalog: { status: "fresh", revision: "rev-2" },
      fetched_at: "2026-08-30T00:00:00Z",
      export_identity: {
        model_config_id: 3,
        model_id: "codex/gpt-x",
        api: "openai-responses",
        provider_id_source: "operator_input",
      },
      results: [
        {
          provider_id: "openai",
          model_id: "gpt-x",
          api: "openai-responses",
          name: "GPT X",
          context_window: 200000,
          dropped_fields: ["headers"],
        },
      ],
    });
    vi.mocked(bindModelPi).mockResolvedValue({
      bound: true,
      bind_source: "manual",
      provider_id: "openai",
      catalog_model_id: "gpt-x",
      api: "openai-responses",
      catalog_revision: "rev-1",
      source: null,
      override: null,
      effective: null,
    });

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await user.click(within(row).getByRole("button", { name: "绑定来源" }));

    expect(screen.getByRole("button", { name: "应用绑定" })).toBeDisabled();
    expect(screen.getByText("最终导出身份（由 Prism 决定）")).toBeVisible();
    expect(screen.getAllByText("codex/gpt-x").length).toBeGreaterThan(0);

    await user.type(
      screen.getByRole("textbox", { name: "目录 model_id 片段" }),
      "GPT-X",
    );
    await user.click(screen.getByRole("button", { name: "搜索目录" }));
    await waitFor(() =>
      expect(searchModelPiCatalog).toHaveBeenCalledWith(3, {
        model_id_query: "GPT-X",
      }),
    );

    expect(screen.getByRole("button", { name: "应用绑定" })).toBeDisabled();

    await user.click(
      screen.getByRole("combobox", { name: "选择目录搜索结果" }),
    );
    const searchOption = screen.getByRole("option", {
      name: /openai\/gpt-x \(openai-responses\)GPT X/,
    });
    expect(searchOption).toHaveTextContent("GPT X");
    expect(searchOption).toHaveTextContent("上下文窗口（tokens）: 200000");
    await user.click(searchOption);

    expect(screen.getByText("已选目录坐标")).toBeVisible();
    expect(
      within(screen.getByRole("dialog")).getAllByText(
        "openai/gpt-x (openai-responses)",
      ).length,
    ).toBeGreaterThan(0);
    expect(screen.getByText(/跨目录绑定/)).toBeVisible();
    expect(screen.getAllByText(/200000/).length).toBeGreaterThan(0);
    expect(screen.getByText(/headers/)).toBeVisible();

    const apply = screen.getByRole("button", { name: "应用绑定" });
    expect(apply).toBeEnabled();
    await user.click(apply);

    await waitFor(() =>
      expect(bindModelPi).toHaveBeenCalledWith(3, {
        provider_id: "openai",
        catalog_model_id: "gpt-x",
        expected_catalog_revision: "rev-2",
        expected_prism_model_id: "codex/gpt-x",
        expected_pi_api: "openai-responses",
      }),
    );
  });

  it("keeps a stale directory search read-only even when source is fresh", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture();
    fixture.models = [
      {
        ...fixture.models[0],
        model_id: "codex/gpt-x",
        pi_candidates: [],
        candidate_status: "not_in_catalog",
        pi_selected: null,
        pi_binding_status: "unbound",
        pi_binding_renderable: false,
        pi_binding_source: null,
        pi_binding_override: null,
        pi_binding_effective: null,
      },
      fixture.models[1],
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);
    vi.mocked(searchModelPiCatalog).mockResolvedValue({
      query: "gpt-x",
      api: "openai-responses",
      limit: 20,
      total: 1,
      returned: 1,
      truncated: false,
      selected: false,
      catalog: { status: "stale", revision: "rev-1" },
      fetched_at: "2026-08-30T00:00:00Z",
      export_identity: {
        model_config_id: 3,
        model_id: "codex/gpt-x",
        api: "openai-responses",
        provider_id_source: "operator_input",
      },
      results: [
        {
          provider_id: "openai",
          model_id: "gpt-x",
          api: "openai-responses",
          name: "GPT X",
        },
      ],
    });

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await user.click(within(row).getByRole("button", { name: "绑定来源" }));
    await user.type(
      screen.getByRole("textbox", { name: "目录 model_id 片段" }),
      "gpt-x",
    );
    await user.click(screen.getByRole("button", { name: "搜索目录" }));
    await user.click(
      await screen.findByRole("combobox", { name: "选择目录搜索结果" }),
    );
    await user.click(
      screen.getByRole("option", { name: /openai\/gpt-x/ }),
    );
    expect(
      await screen.findByText(/last-known-good 目录证据/),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "应用绑定" })).toBeDisabled();
    expect(bindModelPi).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "取消" }));
    const secondRow = screen.getByTestId("export-row-5");
    await user.click(
      within(secondRow).getByRole("button", { name: "绑定来源" }),
    );
    expect(
      screen.queryByRole("combobox", { name: "选择目录搜索结果" }),
    ).toBeNull();
  });

  it("offers no directory entry for a model with no Pi text API mapping", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture();
    fixture.models = [
      {
        ...fixture.models[0],
        pi_api: "",
        pi_candidates: [],
        candidate_status: "api_mismatch",
        pi_selected: null,
        pi_binding_status: "unbound",
        pi_binding_renderable: false,
        pi_binding_source: null,
        pi_binding_override: null,
        pi_binding_effective: null,
      },
      {
        ...fixture.models[0],
        model_config_id: 4,
        model_id: "bound-without-pi-api",
        pi_api: "",
        pi_selected: {
          provider_id: "directory-provider",
          model_id: "directory-alias",
          api: "openai-responses",
        },
        pi_binding_prism_model_id: "bound-without-pi-api",
        pi_binding_status: "bound_drifted",
        pi_binding_renderable: false,
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    expect(
      within(row).getByText(/没有 Pi 文本 API 映射，无法绑定目录来源/),
    ).toBeVisible();
    expect(within(row).queryByRole("button", { name: "绑定来源" })).toBeNull();
    const boundRow = screen.getByTestId("export-row-4");
    expect(within(boundRow).getByText(/跨目录绑定/)).toBeVisible();
    await openBindingMenu(user, boundRow);
    expect(screen.getByRole("menuitem", { name: "更换来源" })).toHaveAttribute(
      "data-disabled",
    );
    await user.keyboard("{Escape}");
    expect(within(boundRow).getByText(/绑定时 Prism ID/)).toBeVisible();
  });

  it("requires an explicit coordinate choice for identical multi-candidate templates", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture();
    fixture.models = [
      {
        ...fixture.models[0],
        model_id: "qwen3.8-flash",
        display_name: "Qwen Flash",
        openai_accepted_format: "chat_completions_only",
        pi_api: "openai-completions",
        pi_selected: null,
        pi_binding_status: "unbound",
        pi_binding_renderable: false,
        pi_binding_source: null,
        pi_binding_override: null,
        pi_binding_effective: null,
        candidate_status: "multiple",
        pi_candidates: [
          {
            provider_id: "qwen-token-plan",
            model_id: "qwen3.8-flash",
            api: "openai-completions",
            name: "Qwen Flash",
            reasoning: true,
            context_window: 300000,
          },
          {
            provider_id: "qwen-token-plan-cn",
            model_id: "qwen3.8-flash",
            api: "openai-completions",
            name: "Qwen Flash",
            reasoning: true,
            context_window: 300000,
          },
        ],
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    expect(within(row).getByText("多个默认候选")).toBeVisible();
    expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
    await user.click(within(row).getByRole("button", { name: "绑定来源" }));

    const apply = screen.getByRole("button", { name: "应用绑定" });
    expect(apply).toBeDisabled();
    await user.click(screen.getByRole("combobox", { name: "选择候选来源" }));
    await user.click(
      screen.getByRole("option", {
        name: "qwen-token-plan-cn/qwen3.8-flash (openai-completions)",
      }),
    );
    await waitFor(() => expect(apply).toBeEnabled());
    expect(screen.getByText("300000")).toBeVisible();
    await user.click(apply);

    await waitFor(() =>
      expect(bindModelPi).toHaveBeenCalledWith(3, {
        provider_id: "qwen-token-plan-cn",
        catalog_model_id: "qwen3.8-flash",
        expected_catalog_revision: "rev-1",
        expected_prism_model_id: "qwen3.8-flash",
        expected_pi_api: "openai-completions",
      }),
    );
  });

  it("submits only the override field explicitly edited by the operator", async () => {
    const user = userEvent.setup();
    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "覆盖" }));

    expect(screen.getByText("输入模态")).toBeVisible();
    expect(screen.getByText("思考等级映射")).toBeVisible();
    expect(screen.getByText("Pi compat")).toBeVisible();
    const save = screen.getByRole("button", { name: "保存覆盖" });
    expect(save).toBeDisabled();

    await user.click(screen.getByRole("combobox", { name: "名称 本次操作" }));
    await user.click(screen.getByRole("option", { name: "写入手动值" }));
    const nameInput = screen.getByRole("textbox", { name: "名称" });
    await user.clear(nameInput);
    const nameError = screen.getByText("名称不能为空。");
    expect(nameInput).toHaveAttribute("aria-invalid", "true");
    expect(nameInput).toHaveAttribute("aria-describedby", nameError.id);
    await user.type(nameInput, "Renamed by operator");
    expect(nameInput).not.toHaveAttribute("aria-invalid");
    await user.click(save);

    await waitFor(() =>
      expect(putModelPiOverride).toHaveBeenCalledWith(3, {
        name: "Renamed by operator",
      }),
    );
  });

  it("requires an explicit candidate choice when rebinding a bound row", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture();
    fixture.models = [
      {
        ...fixture.models[0],
        candidate_status: "multiple",
        pi_candidates: [
          ...fixture.models[0].pi_candidates,
          {
            provider_id: "openrouter",
            model_id: "gpt-x",
            api: "openai-responses",
            name: "GPT X via OpenRouter",
          },
        ],
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);
    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "更换来源" }));

    const apply = screen.getByRole("button", { name: "应用绑定" });
    expect(apply).toBeDisabled();
    await user.click(screen.getByRole("combobox", { name: "选择候选来源" }));
    await user.click(
      screen.getByRole("option", {
        name: "openrouter/gpt-x (openai-responses)",
      }),
    );
    expect(screen.getByText(/永久清除这个模型的全部手动覆盖/)).toBeVisible();
    const destructiveApply = screen.getByRole("button", {
      name: "重新绑定并清除覆盖",
    });
    expect(destructiveApply).toBeEnabled();
    await user.click(destructiveApply);

    await waitFor(() =>
      expect(bindModelPi).toHaveBeenCalledWith(3, {
        provider_id: "openrouter",
        catalog_model_id: "gpt-x",
        expected_catalog_revision: "rev-1",
        expected_prism_model_id: "gpt-x",
        expected_pi_api: "openai-responses",
      }),
    );
  });

  it("keeps committed mutations blocked and visible when source reconciliation fails", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture({ models: [sourceFixture().models[0]] });
    vi.mocked(fetchModelExportSource)
      .mockResolvedValueOnce(fixture)
      .mockRejectedValueOnce(new Error("source read failed"));
    vi.mocked(unbindModelPi).mockResolvedValue({
      bound: false,
      source: null,
      override: null,
      effective: null,
    });

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "解绑" }));
    expect(screen.getByText(/当前绑定含有手动覆盖/)).toBeVisible();
    await user.click(screen.getByTestId("pi-unbind-confirm"));

    const dialog = screen.getByRole("dialog", { name: "解除 Pi 绑定？" });
    expect(
      await within(dialog).findByText(/变更已保存，但导出源刷新失败/),
    ).toBeVisible();
    expect(screen.getByTestId("pi-unbind-confirm")).toBeDisabled();
    await user.click(within(dialog).getByRole("button", { name: "取消" }));
    expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
    expect(screen.getByText(/上次成功刷新/)).toBeVisible();
  });

  it("confirms before clearing every manual override", async () => {
    const user = userEvent.setup();
    vi.mocked(clearModelPiOverride).mockResolvedValue({
      bound: true,
      source: null,
      override: null,
      effective: null,
    });

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "覆盖" }));
    await user.click(screen.getByRole("button", { name: "清除全部覆盖" }));
    expect(clearModelPiOverride).not.toHaveBeenCalled();
    expect(screen.getByText("清除全部 Pi 手动覆盖？")).toBeVisible();
    await user.click(screen.getByTestId("pi-clear-overrides-confirm"));
    await waitFor(() => expect(clearModelPiOverride).toHaveBeenCalledWith(3));
  });

  it("reports a stale render inside the still-open key dialog", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture({ models: [sourceFixture().models[0]] });
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);
    vi.mocked(renderModelExport).mockRejectedValueOnce(
      Object.assign(new Error("source drifted"), { status: 409 }),
    );

    renderPage();
    await screen.findByTestId("export-row-3");
    await user.click(screen.getByRole("button", { name: /生成配置文件/ }));
    const dialog = screen.getByTestId("export-key-dialog");
    await user.click(within(dialog).getByRole("button", { name: "确认生成" }));
    expect(await within(dialog).findByText(/源事实已漂移/)).toBeVisible();
  });

  it("keeps a frozen binding renderable when only live catalog evidence drifted", async () => {
    const fixture = sourceFixture();
    fixture.models = [
      {
        ...fixture.models[0],
        pi_binding_status: "bound_drifted",
        pi_binding_renderable: true,
        pi_candidates: [],
        candidate_status: "not_in_catalog",
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);

    renderPage();
    await screen.findByTestId("export-row-3");
    expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeEnabled();
  });

  it("blocks render-only binding actions when Prism identity no longer matches", async () => {
    const user = userEvent.setup();
    const fixture = sourceFixture();
    fixture.models = [
      {
        ...fixture.models[0],
        pi_binding_status: "bound_drifted",
        pi_binding_renderable: false,
        pi_binding_prism_model_id: "renamed-away-gpt-x",
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    expect(screen.getByRole("menuitem", { name: "刷新" })).toHaveAttribute(
      "data-disabled",
    );
    expect(screen.getByRole("menuitem", { name: "覆盖" })).toHaveAttribute(
      "data-disabled",
    );
    expect(
      screen.getByRole("menuitem", { name: "更换来源" }),
    ).not.toHaveAttribute("data-disabled");
    expect(screen.getByRole("menuitem", { name: "解绑" })).not.toHaveAttribute(
      "data-disabled",
    );
    expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
  });
});

describe("ExportKeyDialog Pi-only", () => {
  it("requires a non-empty trimmed key in manual mode", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn(async () => undefined);
    render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportKeyDialog
            open
            selectedCount={1}
            error={null}
            onClose={vi.fn()}
            onConfirm={onConfirm}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    await user.click(screen.getByRole("radio", { name: /手动输入统一密钥/ }));
    const confirm = screen.getByRole("button", { name: "确认生成" });
    const input = screen.getByLabelText("Prism 代理密钥");
    expect(confirm).toBeDisabled();
    await user.type(input, "   ");
    expect(confirm).toBeDisabled();
    await user.type(input, " proxy-key ");
    expect(confirm).toBeEnabled();
    await user.click(confirm);

    await waitFor(() =>
      expect(onConfirm).toHaveBeenCalledWith({
        mode: "manual",
        manualKey: "proxy-key",
      }),
    );
  });
});

describe("ExportResultSheet Pi-only", () => {
  it("copies full content and Pi merge fragment", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    const result: ExportRenderResponse = {
      target_version: "0.84.3",
      content: '{"providers":{"home":{"name":"Prism","models":[]}}}\n',
      content_sha256: "f".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
      warnings: ["pi_source_fields_dropped"],
    };
    const { unmount } = render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={result} onClose={vi.fn()} />
        </TooltipProvider>
      </LocaleProvider>,
    );
    await user.click(screen.getByRole("button", { name: "复制" }));
    expect(writeText).toHaveBeenLastCalledWith(result.content);
    await user.click(
      screen.getByRole("button", { name: "复制 providers 合并片段" }),
    );
    expect(writeText).toHaveBeenLastCalledWith(
      '{\n  "home": {\n    "name": "Prism",\n    "models": []\n  }\n}\n',
    );
    expect(
      screen.getByText(/pi\.dev 来源包含不安全或不受支持的字段/),
    ).toBeVisible();
    unmount();
  });

  it("revokes raw-view Blob URLs when content clears and on unmount", async () => {
    const user = userEvent.setup();
    const createObjectURL = vi
      .fn()
      .mockReturnValueOnce("blob:pi-one")
      .mockReturnValueOnce("blob:pi-two");
    const revokeObjectURL = vi.fn();
    const originalCreate = URL.createObjectURL;
    const originalRevoke = URL.revokeObjectURL;
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: createObjectURL,
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: revokeObjectURL,
    });
    const anchorClick = vi
      .spyOn(HTMLAnchorElement.prototype, "click")
      .mockImplementation(() => undefined);
    const result: ExportRenderResponse = {
      target_version: "0.84.3",
      content: '{"providers":{"home":{"models":[]}}}\n',
      content_sha256: "f".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
    };
    const renderSheet = (value: ExportRenderResponse | null) => (
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={value} onClose={vi.fn()} />
        </TooltipProvider>
      </LocaleProvider>
    );

    try {
      const view = render(renderSheet(result));
      await user.click(
        screen.getByRole("button", { name: "在新标签页查看原始 JSON" }),
      );
      view.rerender(renderSheet(null));
      await waitFor(() =>
        expect(revokeObjectURL).toHaveBeenCalledWith("blob:pi-one"),
      );

      view.rerender(renderSheet({ ...result, content_sha256: "e".repeat(64) }));
      await user.click(
        screen.getByRole("button", { name: "在新标签页查看原始 JSON" }),
      );
      view.unmount();
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:pi-two");
    } finally {
      anchorClick.mockRestore();
      Object.defineProperty(URL, "createObjectURL", {
        configurable: true,
        value: originalCreate,
      });
      Object.defineProperty(URL, "revokeObjectURL", {
        configurable: true,
        value: originalRevoke,
      });
    }
  });
});
