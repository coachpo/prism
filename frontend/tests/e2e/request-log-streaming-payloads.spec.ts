import { expect, test } from "@playwright/test";
import { mockPrismRoutes } from "./request-log-dedicated-audit-fixtures";

test.describe("saved conversation content", () => {
  test("streaming replies are readable without transport event controls", async ({ page }) => {
    await mockPrismRoutes(page, "openai_stream");
    await page.goto("/observe/requests/101/audit?audit_id=201");
    const response = page.getByTestId("dedicated-audit-detail").getByRole("region", { name: "响应（200）" });
    await expect(response.getByText("Hello from the stream")).toBeVisible();
    await expect(response.getByRole("button", { name: /JSON|SSE|原始/ })).toHaveCount(0);
    await expect(response).not.toContainText(/finish_reason|data:|\[DONE\]/);
  });
  test("tool activity is understandable without exposing implementation arguments", async ({ page }) => {
    await mockPrismRoutes(page, "openai_stream_tools");
    await page.goto("/observe/requests/101/audit?audit_id=201");
    const response = page.getByTestId("dedicated-audit-detail").getByRole("region", { name: "响应（200）" });
    await expect(response.getByText("使用了工具")).toBeVisible();
    await expect(response).not.toContainText(/get_weather|city|tool_calls/);
  });
  test("non-stream input retains the user's message", async ({ page }) => {
    await mockPrismRoutes(page, "openai_document");
    await page.goto("/observe/requests/101/audit?audit_id=201");
    const request = page.getByTestId("dedicated-audit-detail").getByRole("region", { name: "请求", exact: true });
    await expect(request.getByText("Reply with exactly ok.")).toBeVisible();
  });
});
