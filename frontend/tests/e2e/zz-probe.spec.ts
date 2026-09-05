import { expect, test } from "@playwright/test";

import { mockPrismRoutes } from "./request-log-dedicated-audit-fixtures";

test("probe drawer render error", async ({ page }) => {
  const logs: string[] = [];
  page.on("console", (msg) => logs.push(`${msg.type()}: ${msg.text()}`));
  page.on("pageerror", (err) => logs.push(`pageerror: ${err.message}\n${err.stack}`));
  await mockPrismRoutes(page, "full");
  await page.goto("/observe/requests?view=attempts");
  await page.getByTestId("request-log-row-101").click();
  await page.waitForTimeout(2500);
  console.log("=== CONSOLE ===\n" + logs.join("\n---\n"));
  expect(true).toBe(true);
});
