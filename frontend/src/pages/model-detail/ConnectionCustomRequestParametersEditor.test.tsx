import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { TooltipProvider } from "@/components/ui/tooltip";
import { ConnectionCustomRequestParametersEditor } from "./ConnectionCustomRequestParametersEditor";
import { parseCustomRequestParametersDraft } from "./customRequestParameters";

function EditorHarness({ initial, changed }: { initial: string; changed: (value: string) => void }) {
  const [draft, setDraft] = useState(initial);
  return <LocaleProvider><TooltipProvider><ConnectionCustomRequestParametersEditor draft={draft} error={null} onDraftChange={(value) => { setDraft(value); changed(value); }} /></TooltipProvider></LocaleProvider>;
}

describe("structured service settings", () => {
  it("edits nested values while preserving lists, groups, empty values and number types", async () => {
    const changed = vi.fn();
    const initial = { provider: { only: ["example/a", "example/b"], allow_fallbacks: false }, options: [{ enabled: true }, null, 1.5], empty_group: {}, empty_list: [], service_note: "first line\nsecond line" };
    render(<EditorHarness initial={JSON.stringify(initial)} changed={changed} />);
    const user = userEvent.setup();
    const model = screen.getByRole("textbox", { name: "服务附加设置 · provider · only · 第 1 项" });
    await user.clear(model);
    await user.type(model, "example/c");
    expect(JSON.parse(changed.mock.lastCall?.[0])).toEqual({ ...initial, provider: { ...initial.provider, only: ["example/c", "example/b"] } });
    expect(document.querySelector('textarea[name="custom_request_parameters"]')).toBeNull();
    expect(screen.queryByText(/custom_request_parameters|管理 API|systemInstruction/)).not.toBeInTheDocument();
  });

  it("keeps an unfinished number visible and invalid instead of saving its previous value", async () => {
    const changed = vi.fn();
    render(<EditorHarness initial='{"temperature":0.7,"options":[true,null]}' changed={changed} />);
    const user = userEvent.setup();
    const value = screen.getByRole("textbox", { name: "服务附加设置 · temperature" });
    await user.clear(value);
    expect(value).toHaveValue("");
    expect(parseCustomRequestParametersDraft(changed.mock.lastCall?.[0]).error).not.toBeNull();
    await user.type(value, "0.5");
    expect(JSON.parse(changed.mock.lastCall?.[0])).toEqual({ temperature: 0.5, options: [true, null] });
    const names = screen.getAllByRole("textbox", { name: "设置名称" });
    await user.clear(names[1]);
    await user.type(names[1], "temperature");
    expect(parseCustomRequestParametersDraft(changed.mock.lastCall?.[0]).error?.reason).toBe("duplicate_key");
    expect(screen.getByRole("alert")).toHaveTextContent("重复的设置名称");
  });
});
