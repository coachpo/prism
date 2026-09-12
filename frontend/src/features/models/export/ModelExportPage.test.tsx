import { getStaticMessages } from "@/i18n/staticMessages";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { ExportRenderResponse, ExportSourceResponse } from "@/lib/types";
import { ModelExportPage } from "./ModelExportPage";
import {
  bindModelPi,
  clearModelPiOverride,
  fetchModelExportSource,
  putModelPiOverride,
  refreshModelPiPreview,
  refreshModelPiCommit,
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
      readiness: { status: "ready", blocking_reasons: [], repair_path: "/route/models/3" },
      model_config_id: 3,
      model_id: "gpt-x",
      api_family: "openai",
      display_name: "GPT Friendly",
      is_enabled: true,
      direct_request_enabled: true,
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
      readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/5" },
      model_config_id: 5,
      model_id: "glm-5.2",
      api_family: "openai",
      display_name: null,
      is_enabled: true,
      direct_request_enabled: true,
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
      offset: 0,
      total: 0,
      returned: 0,
      truncated: false,
      selected: false,
      catalog: { status: "fresh", revision: "rev-1" },
      fetched_at: "2026-08-30T00:00:00Z",
      checked_at: "2026-08-30T00:00:00Z",
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
  await user.click(within(row).getByRole("button", { name: "模型资料操作" }));
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

  it("excludes unready rows while keeping their binding repair action available", async () => {
    const user = userEvent.setup();
    renderPage();
    const blocked = await screen.findByTestId("export-row-5");
    expect(within(blocked).getByRole("checkbox")).toBeDisabled();
    expect(within(blocked).getByRole("checkbox")).not.toBeChecked();
    expect(within(blocked).getByText("请先选择或补全 Pi 模型资料")).toBeVisible();
    expect(within(blocked).getByRole("button", { name: "选择 Pi 资料" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "全选当前可见" }));
    expect(within(blocked).getByRole("checkbox")).not.toBeChecked();
    expect(screen.getByRole("button", { name: /生成配置文件 \(1\)/ })).toBeEnabled();
  });

  it("withdraws missing readiness on refresh even with the same digest and requires explicit re-selection after recovery", async () => {
    const user = userEvent.setup();
    const ready = sourceFixture({ models: [sourceFixture().models[0]] });
    vi.mocked(fetchModelExportSource).mockResolvedValue(ready);
    renderPage();
    const row = await screen.findByTestId("export-row-3");
    expect(within(row).getByRole("checkbox")).toBeChecked();
    const missing = structuredClone(ready);
    // An incomplete wire response must fail closed, even if its digest is unchanged.
    Reflect.deleteProperty(missing.models[0], "readiness");
    vi.mocked(fetchModelExportSource).mockResolvedValue(missing);
    await user.click(screen.getByRole("button", { name: "刷新模型资料" }));
    await waitFor(() => expect(within(row).getByRole("checkbox")).toBeDisabled());
    expect(within(row).getByRole("checkbox")).not.toBeChecked();
    expect(within(row).getByText("暂时无法确认能否导出，请刷新模型资料")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "全选当前可见" }));
    expect(within(row).getByRole("checkbox")).not.toBeChecked();
    vi.mocked(fetchModelExportSource).mockResolvedValue(ready);
    await user.click(screen.getByRole("button", { name: "刷新模型资料" }));
    await waitFor(() => expect(within(row).getByRole("checkbox")).toBeEnabled());
    expect(within(row).getByRole("checkbox")).not.toBeChecked();
    await user.click(within(row).getByRole("checkbox"));
    expect(screen.getByRole("button", { name: /生成配置文件 \(1\)/ })).toBeEnabled();
  });

  it("does not treat an optional missing thinking map as incomplete metadata", async () => {
    renderPage();
    await screen.findByTestId("export-row-3");
    expect(screen.getByTestId("export-risk-metadata-count")).toHaveTextContent(
      "0",
    );
  });

  it("shows catalog status and a bound candidate's coordinate", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByText(/目录状态/);
    expect(screen.queryByText("a".repeat(64))).not.toBeInTheDocument();
    expect(document.querySelector('[title="price_no_template"], [title="metadata_incomplete"]')).toBeNull();
    expect(screen.getByText("openai/gpt-x")).toBeVisible();
    expect(screen.getByText(/有 1 项不支持的目录设置未采用/)).toBeVisible();
    expect(screen.getByText(/关联时的客户端模型名称/)).toBeVisible();
    expect(
      screen.getByText(/部分目录设置不受支持/),
    ).toBeVisible();
    const row = screen.getByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "更换来源" }));
    expect(
      within(screen.getByRole("dialog")).getByText(
        (_, element) =>
          element?.tagName === "P" &&
          element.textContent?.includes("关联的客户端模型名称") === true,
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
        readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/3" },
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
    expect(within(row).getByText("找到一个同名模型")).toBeVisible();
    await user.click(within(row).getByRole("button", { name: "选择 Pi 资料" }));

    const apply = screen.getByRole("button", { name: "关联所选模型" });
    expect(apply).toBeEnabled();
    expect(
      within(screen.getByRole("dialog")).getByText(
        (_, element) => element?.textContent === "未采用的目录设置: 无",
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
        readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/3" },
        pi_binding_source: null,
        pi_binding_override: null,
        pi_binding_effective: null,
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);
    vi.mocked(searchModelPiCatalog)
      .mockRejectedValueOnce(new Error("pi_catalog_unavailable"))
      .mockResolvedValue({
        query: "GPT-X",
        api: "openai-responses",
        limit: 20,
        offset: 0,
        total: 1,
        returned: 1,
        truncated: false,
        selected: false,
        catalog: { status: "fresh", revision: "rev-2" },
        fetched_at: "2026-08-30T00:00:00Z",
        checked_at: "2026-08-30T00:00:00Z",
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
    await user.click(within(row).getByRole("button", { name: "选择 Pi 资料" }));

    expect(screen.getByRole("button", { name: "关联所选模型" })).toBeDisabled();
    expect(screen.getByText("当前客户端模型")).toBeVisible();
    expect(screen.getAllByText("codex/gpt-x").length).toBeGreaterThan(0);

    const searchInput = screen.getByRole("textbox", {
      name: "模型名称关键词",
    });
    await user.type(searchInput, "GPT-X");
    await user.click(screen.getByRole("button", { name: "搜索目录" }));
    await waitFor(() =>
      expect(searchModelPiCatalog).toHaveBeenCalledWith(
        3,
        {
          model_id_query: "GPT-X",
          limit: 20,
          offset: 0,
        },
        expect.any(AbortSignal),
      ),
    );
    expect(searchInput).not.toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText("目录搜索失败")).toBeVisible();
    expect(screen.getByText(getStaticMessages().common.requestErrors.unknown)).toBeVisible();

    await user.click(screen.getByRole("button", { name: "搜索目录" }));
    await waitFor(() => expect(searchModelPiCatalog).toHaveBeenCalledTimes(2));

    expect(screen.getByRole("button", { name: "关联所选模型" })).toBeDisabled();

    const searchOption = await screen.findByRole("option", {
      name: /openai\/gpt-x/,
    });
    expect(searchOption).toHaveTextContent("GPT X");
    await user.click(searchOption);

    expect(screen.getByText("已选模型来源")).toBeVisible();
    expect(
      within(screen.getByRole("dialog")).getAllByText("openai/gpt-x").length,
    ).toBeGreaterThan(0);
    expect(screen.getByText(/使用其他名称的模型资料|来源模型使用其他名称/)).toBeVisible();
    expect(screen.getAllByText(/200000/).length).toBeGreaterThan(0);
    expect(screen.queryByText(/headers/)).not.toBeInTheDocument();
    expect(screen.getByText(/不支持的目录设置未采用/)).toBeVisible();

    const apply = screen.getByRole("button", { name: "关联所选模型" });
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
        readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/3" },
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
      offset: 0,
      total: 1,
      returned: 1,
      truncated: false,
      selected: false,
      catalog: { status: "stale", revision: "rev-1" },
      fetched_at: "2026-08-30T00:00:00Z",
      checked_at: "2026-08-30T00:00:00Z",
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
    await user.click(within(row).getByRole("button", { name: "选择 Pi 资料" }));
    await user.type(
      screen.getByRole("textbox", { name: "模型名称关键词" }),
      "gpt-x",
    );
    await user.click(screen.getByRole("button", { name: "搜索目录" }));
    await user.click(
      await screen.findByRole("option", { name: /openai\/gpt-x/ }),
    );
    expect(
      (await screen.findAllByText(/目录暂时无法更新/)).length,
    ).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: "关联所选模型" })).toBeDisabled();
    expect(bindModelPi).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "取消" }));
    const secondRow = screen.getByTestId("export-row-5");
    await user.click(
      within(secondRow).getByRole("button", { name: "选择 Pi 资料" }),
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
        readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/3" },
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
        readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/3" },
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    expect(
      within(row).getByText(/Pi 尚不支持此模型的连接方式/),
    ).toBeVisible();
    expect(within(row).queryByRole("button", { name: "选择 Pi 资料" })).toBeNull();
    const boundRow = screen.getByTestId("export-row-4");
    expect(within(boundRow).getByText(/使用其他名称的模型资料|来源模型使用其他名称/)).toBeVisible();
    await openBindingMenu(user, boundRow);
    expect(screen.getByRole("menuitem", { name: "更换来源" })).toHaveAttribute(
      "data-disabled",
    );
    await user.keyboard("{Escape}");
    expect(within(boundRow).getByText(/关联时的客户端模型名称/)).toBeVisible();
  });

  it("does not fabricate a missing bind-time identity from the current model id", async () => {
    const fixture = sourceFixture();
    const bound = { ...fixture.models[0] };
    delete bound.pi_binding_prism_model_id;
    fixture.models = [bound];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);
    renderPage();

    const row = await screen.findByTestId("export-row-3");
    expect(within(row).getByText("关联信息不完整")).toBeVisible();
    expect(within(row).getByText(/无法确认资料是否适用于当前模型/)).toBeVisible();
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
        readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/3" },
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
    expect(within(row).getByText("找到多个同名模型")).toBeVisible();
    expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
    await user.click(within(row).getByRole("button", { name: "选择 Pi 资料" }));

    const apply = screen.getByRole("button", { name: "关联所选模型" });
    expect(apply).toBeDisabled();
    await user.selectOptions(
      screen.getByRole("combobox", { name: "选择候选来源" }),
      screen.getByRole("option", {
        name: /qwen-token-plan-cn\/qwen3\.8-flash/,
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
    await user.click(screen.getByRole("menuitem", { name: "调整资料" }));

    expect(screen.getByText("接受的内容格式")).toBeVisible();
    expect(screen.getByText("Pi 推理强度设置")).toBeVisible();
    expect(screen.getByText("Pi 客户端兼容选项")).toBeVisible();
    expect(screen.getAllByText(/目录值: GPT X/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/目录值: 200000/).length).toBeGreaterThan(0);
    const save = screen.getByRole("button", { name: "保存资料" });
    expect(save).toBeDisabled();

    await user.click(screen.getByRole("combobox", { name: "名称 本次操作" }));
    await user.click(screen.getByRole("option", { name: "填写手动值" }));
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

  it("shows the real refresh-preview failure inline and retries", async () => {
    const user = userEvent.setup();
    vi.mocked(refreshModelPiPreview)
      .mockRejectedValueOnce(new Error("preview transport failed"))
      .mockResolvedValueOnce({
        bound: true,
        provider_id: "openai",
        catalog_model_id: "gpt-x",
        api: "openai-responses",
        changed: false,
        changes: [],
        catalog_revision: "rev-1",
        binding_updated_at: "2026-08-31T00:00:00Z",
        fetched_at: "2026-08-31T00:00:00Z",
      });
    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "检查资料更新" }));

    expect(await screen.findByText(getStaticMessages().common.requestErrors.unknown)).toBeVisible();
    expect(screen.queryByText("正在获取最新目录数据...")).toBeNull();
    await user.click(screen.getByRole("button", { name: "重试刷新预览" }));
    expect(await screen.findByText("目录数据未发生变化。")).toBeVisible();
  });

  it("keeps a refresh commit failure distinct from preview failure", async () => {
    const user = userEvent.setup();
    vi.mocked(refreshModelPiPreview).mockResolvedValueOnce({
      bound: true,
      provider_id: "openai",
      catalog_model_id: "gpt-x",
      api: "openai-responses",
      changed: false,
      changes: [],
      catalog_revision: "rev-1",
      binding_updated_at: "2026-08-31T00:00:00Z",
      fetched_at: "2026-08-31T00:00:00Z",
    });
    vi.mocked(refreshModelPiCommit).mockRejectedValueOnce(
      new Error("pi_binding_stale"),
    );
    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    await user.click(screen.getByRole("menuitem", { name: "检查资料更新" }));
    await screen.findByText("目录数据未发生变化。");
    await user.click(screen.getByRole("button", { name: "更新资料" }));

    expect(await screen.findByText("刷新提交失败")).toBeVisible();
    expect(screen.getByText(getStaticMessages().common.requestErrors.unknown)).toBeVisible();
    expect(screen.queryByText("刷新预览读取失败")).toBeNull();
    expect(screen.getByText("目录数据未发生变化。")).toBeVisible();
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

    const apply = screen.getByRole("button", { name: "关联所选模型" });
    expect(apply).toBeDisabled();
    await user.selectOptions(
      screen.getByRole("combobox", { name: "选择候选来源" }),
      screen.getByRole("option", {
        name: /openrouter\/gpt-x/,
      }),
    );
    expect(screen.getByText(/将清除旧来源的全部手动调整/)).toBeVisible();
    const destructiveApply = screen.getByRole("button", {
      name: "更换来源并清除手动调整",
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
    await user.click(screen.getByRole("menuitem", { name: "解除 Pi 绑定" }));
    expect(screen.getByText(/当前资料含有手动调整/)).toBeVisible();
    await user.click(screen.getByTestId("pi-unbind-confirm"));

    const dialog = screen.getByRole("dialog", { name: "解除 Pi 绑定？" });
    expect(
      await within(dialog).findByText(/变更已保存，但页面暂时无法读取最新结果/),
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
    await user.click(screen.getByRole("menuitem", { name: "调整资料" }));
    await user.click(screen.getByRole("button", { name: "恢复全部目录值" }));
    expect(clearModelPiOverride).not.toHaveBeenCalled();
    expect(screen.getByText("清除全部 Pi 手动调整？")).toBeVisible();
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
    expect(await within(dialog).findByText(/模型资料在生成前已更新/)).toBeVisible();
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
        readiness: { status: "blocked", blocking_reasons: ["pi_binding_required"], repair_path: "/route/models/3" },
        pi_binding_prism_model_id: "renamed-away-gpt-x",
      },
    ];
    vi.mocked(fetchModelExportSource).mockResolvedValue(fixture);

    renderPage();
    const row = await screen.findByTestId("export-row-3");
    await openBindingMenu(user, row);
    expect(screen.getByRole("menuitem", { name: "检查资料更新" })).toHaveAttribute(
      "data-disabled",
    );
    expect(screen.getByRole("menuitem", { name: "调整资料" })).toHaveAttribute(
      "data-disabled",
    );
    expect(
      screen.getByRole("menuitem", { name: "更换来源" }),
    ).not.toHaveAttribute("data-disabled");
    expect(screen.getByRole("menuitem", { name: "解除 Pi 绑定" })).not.toHaveAttribute(
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
            riskSummary={{ costOmitted: 2, metadataIncomplete: 0 }}
            error={null}
            onClose={vi.fn()}
            onConfirm={onConfirm}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    // 最后一次能反悔的步骤要复述本次导出范围与已知代价。
    expect(screen.getByText(/已选 1 个模型/)).toBeVisible();
    expect(screen.getByText(/其中 2 个不包含价格信息/)).toBeVisible();
    expect(screen.queryByText(/元信息有缺失/)).toBeNull();

    await user.click(screen.getByRole("radio", { name: /手动输入统一密钥/ }));
    const confirm = screen.getByRole("button", { name: "确认生成" });
    const input = screen.getByLabelText(/^Prism 客户端密钥/);
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

  it("submits from the key input with Enter", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn(async () => undefined);
    render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportKeyDialog
            open
            selectedCount={1}
            riskSummary={{ costOmitted: 0, metadataIncomplete: 0 }}
            error={null}
            onClose={vi.fn()}
            onConfirm={onConfirm}
          />
        </TooltipProvider>
      </LocaleProvider>,
    );

    await user.click(screen.getByRole("radio", { name: /手动输入统一密钥/ }));
    await user.type(screen.getByLabelText(/^Prism 客户端密钥/), "proxy-key{Enter}");

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
      screen.getByText(/部分目录设置不受支持/),
    ).toBeVisible();
    unmount();
  });

  it("falls back to execCommand copy on insecure origins", async () => {
    // http://LAN-IP origins have no navigator.clipboard at all; the sheet
    // must still copy through the shared textarea/execCommand fallback.
    const user = userEvent.setup();
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      configurable: true,
      writable: true,
    });
    const execCommand = vi.fn(() => true);
    Object.defineProperty(document, "execCommand", {
      value: execCommand,
      configurable: true,
      writable: true,
    });
    const result: ExportRenderResponse = {
      target_version: "0.84.3",
      content: '{"providers":{"home":{"name":"Prism","models":[]}}}\n',
      content_sha256: "f".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
      warnings: [],
    };
    render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={result} onClose={vi.fn()} />
        </TooltipProvider>
      </LocaleProvider>,
    );
    await user.click(screen.getByRole("button", { name: "复制" }));
    expect(execCommand).toHaveBeenCalledWith("copy");
    // 「已复制」只是瞬时反馈：剪贴板会被别的内容顶掉，按钮必须还能再点。
    expect(screen.getByRole("button", { name: "已复制" })).toBeEnabled();
    delete (document as unknown as Record<string, unknown>).execCommand;
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      configurable: true,
      writable: true,
    });
  });

  it("reports a failed copy inline instead of staying silent", async () => {
    const user = userEvent.setup();
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      configurable: true,
      writable: true,
    });
    Object.defineProperty(document, "execCommand", {
      value: vi.fn(() => false),
      configurable: true,
      writable: true,
    });
    const result: ExportRenderResponse = {
      target_version: "0.84.3",
      content: '{"providers":{"home":{"name":"Prism","models":[]}}}\n',
      content_sha256: "f".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
      warnings: [],
    };
    render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={result} onClose={vi.fn()} />
        </TooltipProvider>
      </LocaleProvider>,
    );

    await user.click(screen.getByRole("button", { name: "复制" }));
    expect(screen.getByTestId("export-copy-failed")).toBeVisible();
    expect(screen.getByRole("button", { name: "复制" })).toBeEnabled();
    delete (document as unknown as Record<string, unknown>).execCommand;
  });

  it("keeps the payload preview focusable and collapsible", async () => {
    const user = userEvent.setup();
    const result: ExportRenderResponse = {
      target_version: "0.84.3",
      content: '{"providers":{"home":{"name":"Prism","models":[]}}}\n',
      content_sha256: "f".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
      source_digest: "a".repeat(64),
      warnings: [],
    };
    render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={result} onClose={vi.fn()} />
        </TooltipProvider>
      </LocaleProvider>,
    );

    const preview = screen.getByTestId("export-content-preview");
    expect(preview).toHaveAttribute("tabindex", "0");
    const toggle = screen.getByRole("button", { name: "展开全文" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    await user.click(toggle);
    expect(
      screen.getByRole("button", { name: "收起预览" }),
    ).toHaveAttribute("aria-expanded", "true");
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
        screen.getByRole("button", { name: "在新标签页查看配置文件" }),
      );
      view.rerender(renderSheet(null));
      await waitFor(() =>
        expect(revokeObjectURL).toHaveBeenCalledWith("blob:pi-one"),
      );

      view.rerender(renderSheet({ ...result, content_sha256: "e".repeat(64) }));
      await user.click(
        screen.getByRole("button", { name: "在新标签页查看配置文件" }),
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
