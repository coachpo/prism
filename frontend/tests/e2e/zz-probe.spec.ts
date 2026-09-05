import { expect, test } from "@playwright/test";

import { mockPrismRoutes } from "./request-log-dedicated-audit-fixtures";

test("probe column toggle then row click", async ({ page }) => {
  const logs: string[] = [];
  page.on("console", (msg) => logs.push(`${msg.type()}: ${msg.text()}`));
  page.on("pageerror", (err) => logs.push(`pageerror: ${err.message}\n${err.stack}`));
  await mockPrismRoutes(page, "full");
  await page.goto("/observe/requests?view=attempts");
  await page.getByTestId("request-log-column-toggle-trigger").click();
  const upstreamColumn = page.getByRole("menuitemcheckbox", { name: "上游模型 ID" });
  await expect(upstreamColumn).toHaveAttribute("aria-checked", "false");
  await upstreamColumn.click();
  await page.waitForTimeout(500);
  console.log("=== AFTER TOGGLE ===\n" + logs.join("\n---\n"));
  await page.keyboard.press("Escape");
  await page.getByTestId("request-log-row-101").click();
  await page.waitForTimeout(1500);
  console.log("=== CONSOLE ===\n" + logs.join("\n---\n"));
  expect(true).toBe(true);
});
