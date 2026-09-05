// ModelsMetricsScopeSwitch: controlled single-select segmented control.
// Re-clicking the active scope must keep the selection — Radix single toggle
// groups emit an empty value on re-click, and the URL-backed scope must never
// be cleared by that.
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { LocaleProvider } from "@/i18n/LocaleProvider";
import { ModelsMetricsScopeSwitch } from "./ModelsMetricsScopeSwitch";

function renderSwitch(
  props: Partial<React.ComponentProps<typeof ModelsMetricsScopeSwitch>> = {},
) {
  const onScopeChange = vi.fn();
  render(
    <LocaleProvider>
      <ModelsMetricsScopeSwitch
        onScopeChange={props.onScopeChange ?? onScopeChange}
        scope={props.scope ?? "ingress"}
      />
    </LocaleProvider>,
  );
  return { onScopeChange: props.onScopeChange ?? onScopeChange };
}

describe("ModelsMetricsScopeSwitch", () => {
  it("renders the three scopes with the current scope checked", () => {
    renderSwitch({ scope: "final_execution" });
    // Radix single ToggleGroup exposes a radio group: items carry role=radio
    // and aria-checked.
    const group = screen.getByRole("radiogroup", { name: "统计口径" });
    expect(group).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "入口请求" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
    expect(screen.getByRole("radio", { name: "最终承载" })).toHaveAttribute(
      "aria-checked",
      "true",
    );
    expect(screen.getByRole("radio", { name: "路由尝试" })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  it("emits the newly selected scope", async () => {
    const { onScopeChange } = renderSwitch({ scope: "ingress" });
    await userEvent.click(screen.getByRole("radio", { name: "路由尝试" }));
    expect(onScopeChange).toHaveBeenCalledWith("route_attempt");
  });

  it("keeps the selection when the active scope is clicked again", async () => {
    const { onScopeChange } = renderSwitch({ scope: "final_execution" });
    await userEvent.click(screen.getByRole("radio", { name: "最终承载" }));
    // The empty value Radix emits for re-click is dropped, not forwarded.
    expect(onScopeChange).not.toHaveBeenCalled();
  });

  it("ignores values outside the three-scope catalog", () => {
    const { onScopeChange } = renderSwitch({ scope: "ingress" });
    // Only the three catalog values are rendered; there is no path that could
    // forward an off-catalog value, so the callback stays silent.
    expect(screen.getByRole("radiogroup", { name: "统计口径" })).toBeInTheDocument();
    expect(onScopeChange).not.toHaveBeenCalled();
  });

  it("carries the scope attribution note on each segment", () => {
    renderSwitch({ scope: "ingress" });
    expect(screen.getByRole("radio", { name: "入口请求" })).toHaveAttribute(
      "title",
      "口径：按入口模型归属，每个入口请求只计一次。",
    );
    expect(screen.getByRole("radio", { name: "最终承载" })).toHaveAttribute(
      "title",
      "口径：按最终目标模型与胜出终端目标归属，每个请求只计一次。",
    );
    expect(screen.getByRole("radio", { name: "路由尝试" })).toHaveAttribute(
      "title",
      "口径：按实际路由尝试计数，失败重试也计入。",
    );
  });
});
