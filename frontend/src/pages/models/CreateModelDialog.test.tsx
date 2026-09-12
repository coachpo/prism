import { useState } from "react";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { endpoints } from "@/lib/api/endpoints";
import { models } from "@/lib/api/models";
import { ApiError } from "@/lib/api/request";
import type { Endpoint, LoadbalanceStrategy, ModelConfig } from "@/lib/types";
import { CreateModelDialog } from "./CreateModelDialog";
import { ModelDialog } from "./ModelDialog";
import { DEFAULT_MODEL_FORM_DATA, type ModelFormData } from "./modelFormState";

vi.mock("@/lib/api/endpoints", () => ({ endpoints: { list: vi.fn() } }));
vi.mock("@/lib/api/models", () => ({ models: { create: vi.fn() } }));

const service = { id: 12, name: "演示服务", base_url: "https://service.example.test" } as Endpoint;
const strategy = { id: 3, name: "默认", is_default: true, legacy_strategy_type: "fill-first" } as LoadbalanceStrategy;

function renderDialog() {
  const onCreated = vi.fn();
  render(<LocaleProvider><TooltipProvider><CreateModelDialog isOpen initialEndpointId={12} loadbalanceStrategies={[strategy]} onClose={vi.fn()} onCreated={onCreated} /></TooltipProvider></LocaleProvider>);
  return onCreated;
}

const editingModel: ModelConfig = {
  id: 8, profile_id: 1, api_family: "openai", model_id: "daily-chat", display_name: "提交名称",
  openai_accepted_format: "dual_native", openai_image_operations: null,
  direct_request_enabled: true, is_enabled: false, loadbalance_strategy_id: null,
  loadbalance_strategy: null, access_targets: [], incoming_model_target_count: 0,
  configuration_warnings: [], created_at: "2026-09-12T00:00:00Z", updated_at: "2026-09-12T00:00:00Z",
};

function EditDialogHarness({ save }: { save: () => Promise<void> }) {
  const [formData, setFormData] = useState<ModelFormData>({ ...DEFAULT_MODEL_FORM_DATA, model_id: "daily-chat", display_name: "提交名称", loadbalance_strategy_id: strategy.id });
  const [formError, setFormError] = useState<string | null>(null);
  return <LocaleProvider><ModelDialog editingModel={editingModel} formData={formData} formError={formError} isDialogOpen loadbalanceStrategies={[strategy]} setFormData={setFormData} setIsDialogOpen={vi.fn()} setLoadbalanceStrategyId={vi.fn()} onSubmit={async () => { await save(); setFormError("暂时无法保存，请重试。"); }} /></LocaleProvider>;
}

describe("model setup recovery", () => {
  beforeEach(() => {
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
    vi.clearAllMocks();
    vi.mocked(endpoints.list).mockResolvedValue([service]);
  });

  it("preselects the service and retains the draft after a failed save for retry", async () => {
    const user = userEvent.setup();
    vi.mocked(models.create).mockRejectedValueOnce(new ApiError("SQL failure at /internal/store", 503, {})).mockResolvedValueOnce({ model: { id: 8, model_id: "daily-chat" } as ModelConfig, configuration_warnings: [] });
    const onCreated = renderDialog();
    await waitFor(() => expect(screen.getByRole("combobox", { name: "使用的服务" })).toHaveTextContent("演示服务"));
    await user.type(screen.getByRole("textbox", { name: /客户端模型名称/ }), "daily-chat");
    await user.click(screen.getByRole("button", { name: "创建并启用" }));
    expect(await screen.findByText(/无法确认模型是否已保存/)).toBeInTheDocument();
    expect(screen.queryByText(/SQL|internal\/store/)).not.toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: /客户端模型名称/ })).toHaveValue("daily-chat");
    expect(onCreated).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "创建并启用" }));
    await waitFor(() => expect(onCreated).toHaveBeenCalledOnce());
    expect(models.create).toHaveBeenLastCalledWith(expect.objectContaining({ model_id: "daily-chat", initial_terminal_target: expect.objectContaining({ endpoint_id: 12, upstream_model_id: "daily-chat" }) }));
  });

  it("reports a failed service read separately from an empty list and retries without losing input", async () => {
    const user = userEvent.setup();
    vi.mocked(endpoints.list).mockRejectedValueOnce(new Error("unavailable")).mockResolvedValueOnce([service]);
    renderDialog();
    await screen.findByText(/暂时无法读取已有服务/);
    await user.type(screen.getByRole("textbox", { name: /客户端模型名称/ }), "preserved-name");
    await user.click(screen.getByRole("button", { name: "重试" }));
    await waitFor(() => expect(screen.getByRole("combobox", { name: "使用的服务" })).toHaveTextContent("演示服务"));
    expect(screen.getByRole("textbox", { name: /客户端模型名称/ })).toHaveValue("preserved-name");
    expect(models.create).not.toHaveBeenCalled();
  });

  it("locks edits during a save and restores the draft after failure", async () => {
    const user = userEvent.setup();
    let complete!: () => void;
    const pending = new Promise<void>((resolve) => { complete = resolve; });
    render(<EditDialogHarness save={() => pending} />);
    const name = screen.getByRole("textbox", { name: "显示名称" });
    await user.click(screen.getByRole("button", { name: "保存更改" }));
    expect(name).toBeDisabled();
    await user.type(name, "不会写入");
    expect(name).toHaveValue("提交名称");
    expect(screen.getAllByRole("combobox").every((field) => field.matches(":disabled"))).toBe(true);
    await act(async () => { complete(); });
    expect(screen.getByText("暂时无法保存，请重试。")).toBeVisible();
    expect(name).toBeEnabled();
    await user.type(name, "已恢复");
    expect(name).toHaveValue("提交名称已恢复");
    expect(screen.getByRole("button", { name: "保存更改" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "取消" })).toBeEnabled();
  });
});
