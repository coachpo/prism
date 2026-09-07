import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import type { ExportRenderResponse } from "@/lib/types";
import { ExportResultSheet } from "./ExportResultSheet";

it("copies a singular provider fragment without activating OpenCode literal substitutions", async () => {
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, "writeText");
  const content = '{"provider":{"prism":{"options":{"apiKey":"\\u007benv:LITERAL}"},"models":{"team/\\u007bfile:LITERAL}":{}}}}}\n';
  const result: ExportRenderResponse = { content, content_sha256: "a".repeat(64), file_name: "opencode-prism.json", mime_type: "application/json;charset=utf-8", target_version: "1.18.27", source_digest: "b".repeat(64), model_results: [] };
  render(<LocaleProvider><ExportResultSheet target="opencode" result={result} onClose={vi.fn()} /></LocaleProvider>);
  await user.click(screen.getByRole("button", { name: "复制" }));
  expect(clipboard).toHaveBeenLastCalledWith(content);
  await user.click(screen.getByRole("button", { name: "复制 provider 合并片段" }));
  const fragment = clipboard.mock.calls.at(-1)![0];
  expect(fragment).not.toContain("{env:");
  expect(fragment).not.toContain("{file:");
  expect(JSON.parse(fragment)).toEqual(JSON.parse(content).provider);
  expect(fragment.endsWith("\n")).toBe(true);
  expect(screen.getByText("opencode-prism.json")).toBeVisible();
});
