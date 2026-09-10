import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useBatchMaintenance } from "./useBatchMaintenance";

const mocks = vi.hoisted(() => ({ preview: vi.fn(), apply: vi.fn(), models: vi.fn(), refs: vi.fn(), clear: vi.fn() }));
vi.mock("@/lib/api", () => ({ api: { batchMaintenance: { preview: mocks.preview, apply: mocks.apply } } }));
vi.mock("@/lib/referenceData", () => ({ getSharedModels: mocks.models, getSharedConnectionOptions: mocks.refs, getSharedLoadbalanceStrategies: mocks.refs, getSharedPricingTemplates: mocks.refs, clearSharedReferenceData: mocks.clear }));
const preview = { action: "model_limits", preview_token: "snapshot", can_apply: true, applied: false, items: [{ id: 1, label: "model", before: { context_limit: 100 }, after: { context_limit: 200 }, manual_override: true }] };
async function fill(result: { current: ReturnType<typeof useBatchMaintenance> }) {
  await waitFor(() => expect(result.current.choices).not.toBeNull());
  act(() => { result.current.toggle(1); result.current.changeLimit(1, "context", "200"); result.current.changeLimit(1, "output", "100"); });
}
describe("batch maintenance sessions", () => {
  beforeEach(() => {
    vi.clearAllMocks(); mocks.models.mockResolvedValue([{ id: 1, model_id: "model" }]); mocks.refs.mockResolvedValue([]);
    mocks.preview.mockResolvedValue(preview); mocks.apply.mockResolvedValue({ ...preview, applied: true });
  });
  it("retains explicit inputs after a concurrent conflict and requires another preview", async () => {
    mocks.apply.mockRejectedValue(new Error("batch_preview_stale"));
    const onApplied = vi.fn(); const { result } = renderHook(() => useBatchMaintenance(true, onApplied));
    await fill(result);
    await act(async () => { await result.current.run(false); });
    act(() => result.current.setConfirmed(true));
    await act(async () => { await result.current.run(true); });
    expect(result.current.preview).toBeNull(); expect(result.current.selected).toEqual([1]);
    expect(result.current.limits[1]).toEqual({ context: "200", output: "100" });
    expect(result.current.error).toContain("预览后配置或引用已变更"); expect(onApplied).not.toHaveBeenCalled();
  });
  it("does not report success until authoritative reads and the host refresh finish", async () => {
    let finish!: () => void;
    const refreshed = new Promise<void>((resolve) => { finish = resolve; });
    const onApplied = vi.fn(() => refreshed); const { result } = renderHook(() => useBatchMaintenance(true, onApplied));
    await fill(result); await act(async () => { await result.current.run(false); });
    act(() => result.current.setConfirmed(true));
    let pending!: Promise<void>; act(() => { pending = result.current.run(true); });
    await waitFor(() => expect(onApplied).toHaveBeenCalled());
    expect(result.current.success).toBe(false); expect(result.current.busy).toBe(true);
    expect(mocks.apply).toHaveBeenCalledWith("model_limits", expect.objectContaining({ preview_token: "snapshot", confirm_manual_overrides: true, client: "opencode" }), expect.any(AbortSignal));
    await act(async () => { finish(); await pending; });
    expect(result.current.success).toBe(true); expect(mocks.models).toHaveBeenCalledTimes(2);
  });
  it("rejects missing references before transport and invalidates confirmation on edits", async () => {
    const { result } = renderHook(() => useBatchMaintenance(true, vi.fn()));
    await fill(result);
    act(() => { result.current.changeAction("model_strategy"); });
    act(() => { result.current.toggle(1); });
    await act(async () => { await result.current.run(false); });
    expect(mocks.preview).not.toHaveBeenCalled();
    expect(result.current.error).toContain("请选择仍有效");
    act(() => result.current.changeReference("11"));
    await act(async () => { await result.current.run(false); });
    act(() => result.current.setConfirmed(true));
    act(() => result.current.changeReference("12"));
    expect(result.current.confirmed).toBe(false);
    expect(result.current.preview).toBeNull();
  });
  it("fences a preview that resolves after the dialog closes", async () => {
    let finish!: (value: typeof preview) => void;
    mocks.preview.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    const { result, rerender } = renderHook(({ open }) => useBatchMaintenance(open, vi.fn()), { initialProps: { open: true } });
    await fill(result); let pending!: Promise<void>; act(() => { pending = result.current.run(false); });
    rerender({ open: false });
    await act(async () => { finish(preview); await pending; });
    expect(result.current.preview).toBeNull();
    expect(mocks.preview.mock.calls[0][2].aborted).toBe(true);
  });
});
