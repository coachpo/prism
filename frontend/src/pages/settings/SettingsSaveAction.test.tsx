import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { SettingsSaveAction, type SettingsPendingSave } from "./SettingsSaveAction";

function renderAction(overrides?: {
  pending?: SettingsPendingSave[];
  saving?: boolean;
  blockedReason?: string | null;
}) {
  render(
    <LocaleProvider>
      <SettingsSaveAction
        blockedReason={overrides?.blockedReason ?? null}
        pending={overrides?.pending ?? []}
        saving={overrides?.saving ?? false}
      />
    </LocaleProvider>,
  );
}

describe("SettingsSaveAction", () => {
  it("counts the cards with unsaved edits and names them under the button", () => {
    renderAction({
      pending: [
        { label: "口径与显示", save: vi.fn() },
        { label: "审计与隐私", save: vi.fn() },
      ],
    });

    expect(screen.getByRole("button", { name: "保存更改（2 处未保存）" })).toBeEnabled();
    expect(screen.getByText("口径与显示 · 审计与隐私")).toBeTruthy();
  });

  it("saves every pending card in order", async () => {
    const user = userEvent.setup();
    const order: string[] = [];
    renderAction({
      pending: [
        { label: "口径与显示", save: () => void order.push("costing") },
        { label: "审计与隐私", save: () => void order.push("audit") },
      ],
    });

    await user.click(screen.getByRole("button", { name: "保存更改（2 处未保存）" }));

    expect(order).toEqual(["costing", "audit"]);
  });

  it("states the blocking reason instead of silently disabling", () => {
    renderAction({
      pending: [{ label: "口径与显示", save: vi.fn() }],
      blockedReason: "设置接口不可用。",
    });

    expect(screen.getByRole("button", { name: "保存更改（1 处未保存）" })).toBeDisabled();
    expect(screen.getByText("暂时无法保存：设置接口不可用。")).toBeTruthy();
  });

  it("explains an empty pending list rather than showing a bare disabled button", () => {
    renderAction();

    expect(screen.getByRole("button", { name: "保存更改" })).toBeDisabled();
    expect(screen.getByText("当前没有未保存的更改。")).toBeTruthy();
  });
});
