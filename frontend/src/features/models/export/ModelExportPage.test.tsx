import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import type { ExportSourceResponse } from "./exportTypes";
import { ModelExportPage } from "./ModelExportPage";
import {
  fetchModelExportSource,
  renderModelExport,
} from "@/lib/api/modelExport";
import { ExportResultSheet } from "./ExportResultSheet";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => ({}),
}));

const sourceFixture = (
  overrides?: Partial<ExportSourceResponse>,
): ExportSourceResponse => ({
  platform: "pi",
  target_version: "0.84.3",
  source_digest: "a".repeat(64),
  models: [
    {
      model_config_id: 3,
      model_id: "gpt-x",
      api_family: "openai",
      display_name: "GPT Friendly",
      is_enabled: true,
      default_selected: true,
      selectable: true,
      openai_accepted_format: "dual_native",
      catalog: { bound: false, has_overrides: false },
      enrichment: {
        available: true,
        offering_provider_id: "openai",
        offering_model_id: "gpt-x",
      },
      prism_metadata: {},
      models_dev_metadata: {},
      merged_metadata: { name: "Catalog GPT" },
      metadata_provenance: {},
      missing_metadata: [],
      platform_completeness: {
        metadata_fields: { name: true, reasoning: true },
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
      enrichment_candidate: null,
    },
    {
      model_config_id: 5,
      model_id: "glm-5.2",
      api_family: "openai",
      display_name: null,
      is_enabled: true,
      default_selected: true,
      selectable: true,
      openai_accepted_format: "chat_completions_only",
      catalog: {
        bound: true,
        provider_id: "openai",
        catalog_model_id: "glm-5.2",
        has_overrides: false,
      },
      enrichment: { available: false },
      prism_metadata: {},
      models_dev_metadata: {},
      merged_metadata: {},
      metadata_provenance: {},
      missing_metadata: ["name"],
      platform_completeness: {
        metadata_fields: { name: false, reasoning: true },
        cost_exportable: false,
      },
      targets: [],
      price_risk: { exportable: false, warning_codes: ["price_no_template"] },
      warnings: [
        "enrichment_unavailable",
        "metadata_incomplete",
        "pi_compat_may_require_manual_override",
        "unsupported_input_modality",
      ],
      enrichment_candidate: null,
    },
  ],
  ...overrides,
});

vi.mock("@/lib/api/modelExport", () => ({
  fetchModelExportSource: vi.fn(() => Promise.resolve(sourceFixture())),
  renderModelExport: vi.fn(() =>
    Promise.resolve({
      platform: "pi",
      target_version: "0.84.3",
      content: "{}\n",
      content_sha256: "c".repeat(64),
      file_name: "prism-pi-models.json",
      mime_type: "application/json;charset=utf-8",
      model_results: [],
    }),
  ),
}));

function renderPage(options?: { strict?: boolean }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const page = (
    <QueryClientProvider client={client}>
      <LocaleProvider>
        <TooltipProvider>
          <ModelExportPage />
        </TooltipProvider>
      </LocaleProvider>
    </QueryClientProvider>
  );
  const view = render(
    options?.strict ? <StrictMode>{page}</StrictMode> : page,
  );
  return { ...view, client };
}

function resultFixture(
  overrides?: Partial<import("./exportTypes").ExportRenderResponse>,
) {
  return {
    platform: "pi" as const,
    target_version: "0.84.3",
    content:
      '{"providers":{"home":{"name":"Prism","models":[]}}}\n',
    content_sha256: "f".repeat(64),
    file_name: "prism-pi-models.json",
    mime_type: "application/json;charset=utf-8",
    model_results: [],
    ...overrides,
  };
}

describe("ModelExportPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
  });

  it("adopts backend default selection on first load and keeps rows interactive", async () => {
    renderPage();
    await screen.findByTestId("export-row-3");
    const first = screen.getByRole("checkbox", { name: "gpt-x" });
    const second = screen.getByRole("checkbox", { name: "glm-5.2" });
    expect(first.getAttribute("aria-checked")).toBe("true");
    expect(second.getAttribute("aria-checked")).toBe("true");
  });

  it("adopts backend defaults under React StrictMode", async () => {
    renderPage({ strict: true });
    await screen.findByTestId("export-row-3");
    await waitFor(() =>
      expect(screen.getByRole("checkbox", { name: "gpt-x" })).toBeChecked(),
    );
  });

  it("intersects user selection on source refresh without adopting new defaults", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByTestId("export-row-3");
    await user.click(screen.getByRole("checkbox", { name: "glm-5.2" }));

    const refreshed = sourceFixture({
      source_digest: "b".repeat(64),
      models: [
        ...sourceFixture().models,
        {
          ...sourceFixture().models[0],
          model_config_id: 7,
          model_id: "new-default",
          display_name: "New Default",
          default_selected: true,
        },
      ],
    });
    vi.mocked(fetchModelExportSource).mockResolvedValueOnce(refreshed);
    await user.click(screen.getByRole("button", { name: "刷新导出源" }));

    await screen.findByTestId("export-row-7");
    expect(screen.getByRole("checkbox", { name: "gpt-x" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "glm-5.2" })).not.toBeChecked();
    expect(
      screen.getByRole("checkbox", { name: "new-default" }),
    ).not.toBeChecked();

    vi.mocked(fetchModelExportSource).mockResolvedValueOnce({
      ...refreshed,
      source_digest: "c".repeat(64),
      models: refreshed.models.map((model) =>
        model.model_config_id === 3
          ? {
              ...model,
              default_selected: false,
              selectable: false,
              unselectable_reason: "no_route",
            }
          : model,
      ),
    });
    await user.click(screen.getByRole("button", { name: "刷新导出源" }));
    await waitFor(() =>
      expect(screen.getByRole("checkbox", { name: "gpt-x" })).not.toBeChecked(),
    );
  });

  it("price-complete filter narrows visible rows without unchecking hidden selections", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByTestId("export-row-5");
    // Both defaults selected; toggle price-complete filter on.
    const filter = screen.getByRole("switch", { name: /仅显示价格完整/ });
    await user.click(filter);
    // glm-5.2 lacks complete pricing and disappears from view while the
    // filter is on; toggling the filter off restores the row with its
    // selection intact — filtering never mutates user selections.
    expect(screen.queryByTestId("export-row-5")).toBeNull();
    await user.click(filter);
    const glmCheckbox = screen.getByRole("checkbox", { name: "glm-5.2" });
    expect(glmCheckbox.getAttribute("aria-checked")).toBe("true");
  });

  it("filters by display/catalog name and platform metadata completeness", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByTestId("export-row-3");

    const search = screen.getByLabelText("搜索模型 ID 或名称");
    await user.type(search, "friendly");
    expect(screen.getByTestId("export-row-3")).toBeInTheDocument();
    expect(screen.queryByTestId("export-row-5")).toBeNull();
    await user.clear(search);

    const metadataSelect = screen.getByRole("combobox", {
      name: "元信息完整度",
    });
    metadataSelect.focus();
    await user.keyboard("{Enter}");
    const incompleteOption = await screen.findByRole("option", {
      name: "元信息有缺失",
    });
    incompleteOption.focus();
    await user.keyboard("{Enter}");
    expect(screen.queryByTestId("export-row-3")).toBeNull();
    expect(screen.getByTestId("export-row-5")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "glm-5.2" })).toBeChecked();
  });

  it("applies the price-complete filter to unselectable rows too", async () => {
    const user = userEvent.setup();
    vi.mocked(fetchModelExportSource).mockResolvedValueOnce(
      sourceFixture({
        models: [
          sourceFixture().models[0],
          {
            ...sourceFixture().models[1],
            default_selected: false,
            selectable: false,
            unselectable_reason: "no_route",
          },
        ],
      }),
    );
    renderPage();
    await screen.findByTestId("export-row-5");
    await user.click(
      screen.getByRole("switch", { name: /仅显示价格完整/ }),
    );
    expect(screen.queryByTestId("export-row-5")).toBeNull();
  });

  it("summarizes selected metadata, cost, and enrichment risks", async () => {
    renderPage();
    await screen.findByTestId("export-row-3");
    expect(screen.getByTestId("export-risk-metadata-count")).toHaveTextContent(
      "1",
    );
    expect(screen.getByTestId("export-risk-cost-count")).toHaveTextContent("1");
    expect(
      screen.getByTestId("export-risk-enrichment-count"),
    ).toHaveTextContent("1");
    expect(
      screen.getByText("元信息不完整，缺失字段保持省略"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Pi compat 可能需要人工核对或覆盖"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("存在客户端不支持的输入模态，已安全省略"),
    ).toBeInTheDocument();
  });

  it("shows the digest label after load", async () => {
    renderPage();
    await screen.findByText(/source_digest/);
    expect(screen.getAllByText("a".repeat(64)).length).toBeGreaterThan(0);
  });

  it("sends only the final manual credential and exact slash-containing model enhancements", async () => {
    const user = userEvent.setup();
    vi.mocked(fetchModelExportSource).mockResolvedValueOnce(
      sourceFixture({
        models: [
          {
            ...sourceFixture().models[0],
            model_id: "vendor/gpt-x",
          },
        ],
      }),
    );
    renderPage();
    await screen.findByRole("checkbox", { name: "vendor/gpt-x" });

    const upload = screen.getByLabelText("上传现有客户端配置");
    await user.upload(
      upload,
      new File(
        [
          JSON.stringify({
            providers: {
              relay: {
                headers: {
                  "x-trace": "safe",
                  accessToken: "must-drop",
                },
                models: [
                  {
                    id: "vendor/gpt-x",
                    thinkingLevelMap: { high: "high" },
                  },
                ],
              },
            },
          }),
        ],
        "models.json",
        { type: "application/json" },
      ),
    );
    expect((upload as HTMLInputElement).value).toBe("");
    const header = await screen.findByText("x-trace");
    await user.click(header);
    await user.click(screen.getByRole("button", { name: "应用到已选模型" }));

    await user.clear(screen.getByLabelText("Provider ID"));
    await user.type(screen.getByLabelText("Provider ID"), " home ");
    await user.click(screen.getByRole("button", { name: /生成配置文件/ }));
    await user.click(screen.getByText("手动输入统一密钥"));
    await user.type(screen.getByLabelText("Prism 代理密钥"), "  proxy-key  ");
    await user.click(screen.getByRole("button", { name: "确认生成" }));

    await waitFor(() => expect(renderModelExport).toHaveBeenCalledTimes(1));
    const [body] = vi.mocked(renderModelExport).mock.calls[0];
    expect(body.provider_id).toBe("home");
    expect(body.credential).toEqual({ include: true, api_key: "proxy-key" });
    expect(body.enhancements?.[3]?.fields).toEqual({
      thinkingLevelMap: { high: "high" },
      headers: { "x-trace": "safe" },
    });
    expect(JSON.stringify(body)).not.toContain("must-drop");
    expect("include_api_keys" in body).toBe(false);
    expect("enrichment_candidates" in body).toBe(false);
  });

  it("keeps an explicitly empty manually entered key distinct from no credential", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByTestId("export-row-3");

    await user.click(screen.getByRole("button", { name: /生成配置文件/ }));
    await user.click(screen.getByText("手动输入统一密钥"));
    const confirm = screen.getByRole("button", { name: "确认生成" });
    expect(confirm).toBeEnabled();
    await user.click(confirm);

    await waitFor(() => expect(renderModelExport).toHaveBeenCalledTimes(1));
    const [body] = vi.mocked(renderModelExport).mock.calls[0];
    expect(body.credential).toEqual({ include: true, api_key: "" });
  });

  it("scopes header confirmation to one upload and removes an unchecked prior header", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByTestId("export-row-3");
    const upload = screen.getByLabelText("上传现有客户端配置");
    const file = (name: string, headerValue: string) =>
      new File(
        [
          JSON.stringify({
            providers: {
              relay: {
                headers: { "x-trace": headerValue },
                models: [{ id: "gpt-x", name }],
              },
            },
          }),
        ],
        `${name}.json`,
        { type: "application/json" },
      );

    await user.upload(upload, file("first", "one"));
    const firstHeader = await screen.findByRole("checkbox", {
      name: /x-trace.*one/,
    });
    await user.click(firstHeader);
    await user.click(screen.getByRole("button", { name: "应用到已选模型" }));

    await user.upload(upload, file("second", "two"));
    const secondHeader = await screen.findByRole("checkbox", {
      name: /x-trace.*two/,
    });
    expect(secondHeader).not.toBeChecked();
    await user.click(screen.getByRole("button", { name: "应用到已选模型" }));

    await user.click(screen.getByRole("button", { name: /生成配置文件/ }));
    await user.click(screen.getByRole("button", { name: "确认生成" }));
    await waitFor(() => expect(renderModelExport).toHaveBeenCalledTimes(1));
    const [body] = vi.mocked(renderModelExport).mock.calls[0];
    expect(body.enhancements?.[3]).toBeNull();
  });
});

describe("ExportResultSheet", () => {
  it("copies full content and the locally derived Pi merge fragment separately", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    const result = resultFixture();
    const view = render(
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={result} platform="pi" onClose={vi.fn()} />
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
    view.unmount();
  });

  it("revokes raw-view Blob URLs when content is cleared and on unmount", async () => {
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

    const renderSheet = (
      result: import("./exportTypes").ExportRenderResponse | null,
    ) => (
      <LocaleProvider>
        <TooltipProvider>
          <ExportResultSheet result={result} platform="pi" onClose={vi.fn()} />
        </TooltipProvider>
      </LocaleProvider>
    );

    const view = render(renderSheet(resultFixture()));
    await user.click(
      screen.getByRole("button", { name: "在新标签页查看原始 JSON" }),
    );
    expect(createObjectURL).toHaveBeenCalledTimes(1);
    view.rerender(renderSheet(null));
    await waitFor(() =>
      expect(revokeObjectURL).toHaveBeenCalledWith("blob:pi-one"),
    );

    view.rerender(
      renderSheet(
        resultFixture({ content_sha256: "e".repeat(64) }),
      ),
    );
    await user.click(
      screen.getByRole("button", { name: "在新标签页查看原始 JSON" }),
    );
    view.unmount();
    expect(revokeObjectURL).toHaveBeenCalledWith("blob:pi-two");

    anchorClick.mockRestore();
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: originalCreate,
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: originalRevoke,
    });
  });
});
