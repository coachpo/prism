import { managementErrorMessage } from "@/lib/api/errorMessage";
import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { fetchOpenCodeExportSource, renderOpenCodeExport } from "@/lib/api/opencodeExport";
import { fetchModelExportSource } from "@/lib/api/modelExport";
import type { OpenCodeExportSourceResponse, ExportRenderResponse } from "@/lib/types";
import { ModelExportPage } from "./ModelExportPage";

vi.mock("@/lib/api/opencodeExport", () => ({ fetchOpenCodeExportSource: vi.fn(), renderOpenCodeExport: vi.fn() }));
vi.mock("@/lib/api/modelExport", () => ({ fetchModelExportSource: vi.fn() }));
vi.mock("@/hooks/useTimezone", () => ({ useTimezone: () => ({ format: (value: string) => value }) }));
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => vi.fn(), useSearch: () => ({}) }));

function sourceFixture(): OpenCodeExportSourceResponse {
  return { target_version: "1.18.27", source_digest: "a".repeat(64), models: [{
    readiness: { status: "ready", blocking_reasons: [], repair_path: "/route/models/9" },
    model_config_id: 9, model_id: "team/model", display_name: "演示模型", api_family: "openai", is_enabled: true, direct_request_enabled: true, selectable: true,
    npm: "@ai-sdk/openai", api_path: "/v1", source_metadata: { reasoning: true, limit_context: 64000, limit_output: 8000 }, override_metadata: { reasoning: false, limit_input: 0 },
    merged_metadata: { name: "演示模型", reasoning: false, limit_context: 64000, limit_input: 0, limit_output: 8000 }, metadata_provenance: { name: "prism_display_name", reasoning: "models_dev_override", limit_context: "models_dev_source", limit_input: "models_dev_override" },
    missing_metadata: ["tool_call"], metadata_issues: [{ field: "tool_call", source: "none", reason: "missing" }], targets: [], price_risk: { exportable: false, warning_codes: ["price_no_template"] },
  }] };
}
function resultFixture(): ExportRenderResponse {
  return { target_version: "1.18.27", content: '{"provider":{"prism":{"env":["PRISM_API_KEY"],"models":{"team/model":{}}}}}\n', content_sha256: "b".repeat(64), file_name: "opencode-prism.json", mime_type: "application/json;charset=utf-8", source_digest: "a".repeat(64), model_results: [] };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><LocaleProvider><TooltipProvider><ModelExportPage /></TooltipProvider></LocaleProvider></QueryClientProvider>);
  return { ...view, client };
}
async function chooseOpenCode(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("radio", { name: "OpenCode" }));
  return screen.findByTestId("opencode-export-row-9");
}
async function generate(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /生成配置文件/ }));
  await user.click(screen.getByRole("button", { name: "确认生成" }));
}
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(fetchModelExportSource).mockResolvedValue({ target_version: "0.84.3", catalog: { status: "fresh" }, models: [], source_digest: "p".repeat(64) });
  vi.mocked(fetchOpenCodeExportSource).mockResolvedValue(sourceFixture());
  vi.mocked(renderOpenCodeExport).mockResolvedValue(resultFixture());
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});

it("isolates delayed source reads across target switches and adopts only the new target selection", async () => {
  const user = userEvent.setup();
  const late = deferred<OpenCodeExportSourceResponse>();
  vi.mocked(fetchOpenCodeExportSource).mockReturnValueOnce(late.promise);
  mount();
  expect(screen.getByRole("radio", { name: "Pi" })).toHaveAttribute("aria-checked", "true");
  await user.click(screen.getByRole("radio", { name: "OpenCode" }));
  const signal = vi.mocked(fetchOpenCodeExportSource).mock.calls[0][0];
  await user.click(screen.getByRole("radio", { name: "Pi" }));
  expect(signal?.aborted).toBe(true);
  await act(async () => { late.resolve(sourceFixture()); });
  expect(screen.queryByTestId("opencode-export-row-9")).toBeNull();
  const row = await chooseOpenCode(user);
  expect(within(row).getByRole("checkbox")).toBeChecked();
  expect(fetchOpenCodeExportSource).toHaveBeenCalledTimes(2);
});

it("shows explicit false, zero, missing evidence and an action to the existing metadata editor", async () => {
  const user = userEvent.setup();
  const fixture = sourceFixture();
  fixture.models[0].selectable = false;
  fixture.models[0].unselectable_reason = "invalid_metadata_limits";
  vi.mocked(fetchOpenCodeExportSource).mockResolvedValue(fixture);
  mount();
  const row = await chooseOpenCode(user);
  await user.click(within(row).getByText("查看资料与来源"));
  const reasoning = within(row).getByText("推理能力").closest("tr")!;
  expect(within(reasoning).getAllByText("否")).toHaveLength(2);
  expect(within(reasoning).getByText("models.dev 手动调整")).toBeVisible();
  expect(within(row).getAllByText("0")).toHaveLength(2);
  expect(within(row).getByText(/无有效来源/)).toBeVisible();
  expect(within(row).getByRole("button", { name: /补全此模型的资料/ })).toBeEnabled();
  expect(within(row).getByRole("checkbox")).toBeDisabled();
  expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
});

it("discards a delayed credential render after closing, switching targets and opening a new session", async () => {
  const user = userEvent.setup();
  const late = deferred<ExportRenderResponse>();
  vi.mocked(renderOpenCodeExport).mockReturnValueOnce(late.promise);
  const { client } = mount();
  const storage = vi.spyOn(Storage.prototype, "setItem");
  await chooseOpenCode(user);
  await user.click(screen.getByRole("button", { name: /生成配置文件/ }));
  await user.click(screen.getByRole("radio", { name: /手动输入统一密钥/ }));
  await user.type(screen.getByLabelText(/^Prism 客户端密钥/), " synthetic-final ");
  await user.click(screen.getByRole("button", { name: "确认生成" }));
  const signal = vi.mocked(renderOpenCodeExport).mock.calls[0][1];
  await user.click(screen.getByRole("button", { name: "取消" }));
  expect(signal?.aborted).toBe(true);
  await user.click(screen.getByRole("radio", { name: "Pi" }));
  await chooseOpenCode(user);
  await user.click(screen.getByRole("button", { name: /生成配置文件/ }));
  await act(async () => { late.resolve({ ...resultFixture(), content: '"synthetic-final"\n' }); });
  expect(screen.getByTestId("export-key-dialog")).toBeVisible();
  expect(screen.queryByTestId("export-result-sheet")).toBeNull();
  expect(screen.queryByLabelText(/^Prism 客户端密钥/)).toBeNull();
  expect(JSON.stringify(client.getQueryCache().getAll().map(query => query.state.data))).not.toContain("synthetic-final");
  expect(client.getMutationCache().getAll()).toHaveLength(0);
  expect(storage).not.toHaveBeenCalled();
  storage.mockRestore();
});

it("aborts a render on route unmount and ignores the late response", async () => {
  const user = userEvent.setup();
  const late = deferred<ExportRenderResponse>();
  vi.mocked(renderOpenCodeExport).mockReturnValueOnce(late.promise);
  const { unmount, client } = mount();
  await chooseOpenCode(user);
  await generate(user);
  const signal = vi.mocked(renderOpenCodeExport).mock.calls[0][1];
  unmount();
  expect(signal?.aborted).toBe(true);
  await act(async () => { late.resolve(resultFixture()); });
  expect(screen.queryByTestId("export-result-sheet")).toBeNull();
  expect(client.getMutationCache().getAll()).toHaveLength(0);
});

it.each([401, 409, 422])("keeps render %i errors recoverable in the key dialog", async (status) => {
  const user = userEvent.setup();
  vi.mocked(renderOpenCodeExport).mockRejectedValueOnce(Object.assign(new Error("controlled failure"), { status }));
  mount();
  await chooseOpenCode(user);
  await generate(user);
  const dialog = screen.getByTestId("export-key-dialog");
  expect(await within(dialog).findByText(status === 409 ? /模型资料在生成前已更新/ : managementErrorMessage(status))).toBeVisible();
  await user.click(within(dialog).getByRole("button", { name: "确认生成" }));
  expect(await screen.findByTestId("export-result-sheet")).toBeVisible();
  expect(renderOpenCodeExport).toHaveBeenLastCalledWith(expect.objectContaining({ credential: { include: false } }), expect.any(AbortSignal));
});

it("keeps failed source reads distinct from empty state and retains labeled last-good rows", async () => {
  const user = userEvent.setup();
  vi.mocked(fetchOpenCodeExportSource).mockRejectedValueOnce(new Error("initial source failure"));
  mount();
  await user.click(screen.getByRole("radio", { name: "OpenCode" }));
  expect(await screen.findByText(getStaticMessages().common.requestErrors.unknown)).toBeVisible();
  expect(screen.queryByTestId("opencode-export-row-9")).toBeNull();
  await user.click(screen.getByRole("button", { name: "重试" }));
  const row = await screen.findByTestId("opencode-export-row-9");
  vi.mocked(fetchOpenCodeExportSource).mockRejectedValueOnce(new Error("refresh source failure"));
  await user.click(screen.getByRole("button", { name: "刷新模型资料" }));
  expect(await screen.findByText(/上次成功刷新/)).toBeVisible();
  expect(row).toBeVisible();
  expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "刷新模型资料" }));
  expect(screen.queryByText(/上次成功刷新/)).toBeNull();
  expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeEnabled();
});


it("keeps selection when repairing in place and reconciles a concurrent invalidation on return", async () => {
  const user = userEvent.setup();
  const catalog = vi.spyOn(api.models.catalog, "get").mockResolvedValue({ bound: false, source: null, override: null, effective: null });
  mount();
  const row = await chooseOpenCode(user);
  expect(within(row).getByRole("checkbox")).toBeChecked();
  await user.click(within(row).getByRole("button", { name: "补全模型资料" }));
  expect(await screen.findByText("修复客户端接入资料")).toBeVisible();
  expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
  expect(within(row).getByRole("checkbox")).toBeChecked();
  const changed = sourceFixture();
  changed.source_digest = "c".repeat(64);
  changed.models[0].selectable = false;
  changed.models[0].unselectable_reason = "invalid_metadata_limits";
  vi.mocked(fetchOpenCodeExportSource).mockResolvedValue(changed);
  await user.click(screen.getByRole("button", { name: "返回导出" }));
  await waitFor(() => expect(within(screen.getByTestId("opencode-export-row-9")).getByRole("checkbox")).toBeDisabled());
  expect(within(screen.getByTestId("opencode-export-row-9")).getByRole("checkbox")).not.toBeChecked();
  expect(screen.getByRole("button", { name: /生成配置文件/ })).toBeDisabled();
  catalog.mockRestore();
});
