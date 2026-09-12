import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { getStaticMessages } from "@/i18n/staticMessages";
import { PiThinkingOptionsEditor } from "./PiThinkingOptionsEditor";
import { buildPiOverrideFields } from "./piOverrideDraft";

it("edits named reasoning levels while preserving explicit omission and other levels", async () => {
  Object.defineProperties(HTMLElement.prototype, {
    scrollIntoView: { configurable: true, value: vi.fn() },
    hasPointerCapture: { configurable: true, value: vi.fn(() => false) },
    releasePointerCapture: { configurable: true, value: vi.fn() },
    setPointerCapture: { configurable: true, value: vi.fn() },
  });
  const user = userEvent.setup();
  const changed = vi.fn();
  function Harness() {
    const [raw, setRaw] = useState('{"low":"vendor-low","high":null}');
    return <PiThinkingOptionsEditor raw={raw} disabled={false} copy={getStaticMessages().modelExportPage} onChange={(next) => { setRaw(next); changed(buildPiOverrideFields({ thinking_level_map: { mode: "value", raw: next } })); }} />;
  }
  render(<Harness />);
  screen.getByRole("combobox", { name: "低" }).focus();
  await user.keyboard("{ArrowDown}");
  await user.click(await screen.findByRole("option", { name: "不发送" }));
  expect(changed).toHaveBeenLastCalledWith({ fields: { thinking_level_map: { low: null, high: null } }, errors: {} });
  screen.getByRole("combobox", { name: "更高" }).focus();
  await user.keyboard("{ArrowDown}");
  await user.click(await screen.findByRole("option", { name: "填写手动值" }));
  await user.type(screen.getByRole("textbox", { name: "更高 选项值" }), "vendor-extra");
  expect(changed).toHaveBeenLastCalledWith({ fields: { thinking_level_map: { low: null, high: null, xhigh: "vendor-extra" } }, errors: {} });
  expect(screen.queryByText("thinking_level_map")).not.toBeInTheDocument();
});
