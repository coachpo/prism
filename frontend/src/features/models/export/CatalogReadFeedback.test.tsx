import { TooltipProvider } from "@/components/ui/tooltip";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { LocaleProvider } from "@/i18n/LocaleProvider";
import { CatalogReadFeedback } from "./CatalogReadFeedback";

vi.mock("@/hooks/useTimezone", () => ({ useTimezone: () => ({ format: (value: string) => value }) }));

it("labels last-good evidence, disables overlapping retries, and clears the failure on recovery", async () => {
  const retry = vi.fn();
  const catalog = { status: "stale" as const, failure_code: "checksum", checked_at: "2026-09-10T01:00:00Z" };
  const { rerender } = render(<LocaleProvider><TooltipProvider><CatalogReadFeedback catalog={catalog} pending={false} onRetry={retry} /></TooltipProvider></LocaleProvider>);
  expect(screen.getByText(/目录内容校验失败/)).toBeVisible();
  expect(screen.getByText(/显示上次读取的目录/)).toBeVisible();
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  expect(retry).toHaveBeenCalledOnce();
  rerender(<LocaleProvider><TooltipProvider><CatalogReadFeedback catalog={catalog} pending onRetry={retry} /></TooltipProvider></LocaleProvider>);
  expect(screen.getByRole("button", { name: "重试" })).toBeDisabled();
  rerender(<LocaleProvider><TooltipProvider><CatalogReadFeedback catalog={{ status: "fresh" }} pending={false} onRetry={retry} /></TooltipProvider></LocaleProvider>);
  expect(screen.queryByText(/目录内容校验失败/)).toBeNull();
  expect(screen.queryByText(/显示上次读取的目录/)).toBeNull();
});

it.each([
  ["timeout", "目录读取超时"], ["format", "目录格式或字段校验失败"],
  ["version", "目录版本不兼容"], ["too_large", "目录响应超过大小上限"],
  ["cancelled", "目录读取已取消"], ["unrecognized", "目录暂时不可用"],
])("explains unavailable %s without fabricating last-good facts", (code, text) => {
  render(<LocaleProvider><TooltipProvider><CatalogReadFeedback catalog={{ status: "unavailable", failure_code: code }} pending={false} onRetry={() => {}} /></TooltipProvider></LocaleProvider>);
  expect(screen.getByText(new RegExp(text))).toBeVisible();
  expect(screen.getByText(/暂时无法读取目录/)).toBeVisible();
  expect(screen.queryByText(/显示上次读取的目录/)).toBeNull();
});
