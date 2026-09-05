import { expect, test } from "@playwright/test";
import { mockPrismRoutes } from "./request-log-dedicated-audit-fixtures";

test.describe("request-log streaming payload views", () => {
  test("SSE stream renders message, JSON events, and Raw SSE views with distinct content", async ({ page }) => {
    await mockPrismRoutes(page, "openai_stream");

    await page.goto("/observe/requests/101/audit?audit_id=201");
    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });

    const responseSection = detail.getByRole("region", { name: "响应（200）" });

    // Three-view availability for streaming bodies (payload-view toggle row).
    const payloadToggles = responseSection.getByLabel("响应（200） payload view");
    await expect(payloadToggles.getByRole("button", { name: "消息" })).toBeVisible();
    await expect(payloadToggles.getByRole("button", { name: "JSON 事件" })).toBeVisible();
    await expect(payloadToggles.getByRole("button", { name: "原始 SSE" })).toBeVisible();

    // Message view reassembles the delta stream.
    await expect(responseSection.getByText("Hello from the stream")).toBeVisible();

    // JSON events view virtualizes per-event JSON.
    await payloadToggles.getByRole("button", { name: "JSON 事件" }).click();
    await expect(responseSection.getByTestId("json-event").first()).toBeVisible();
    await expect(responseSection.getByText('"finish_reason": "stop"')).toBeVisible();

    // Raw SSE shows the byte-exact text, not the reassembled message.
    await payloadToggles.getByRole("button", { name: "原始 SSE" }).click();
    await expect(responseSection.getByText('data: {"choices":[{"delta":{"role":"assistant","content":"Hello"}}]}')).toBeVisible();
    await expect(responseSection.getByText("Hello from the stream")).toHaveCount(0);
  });

  test("SSE stream with tool calls renders a tool-call card in the message view", async ({ page }) => {
    await mockPrismRoutes(page, "openai_stream_tools");

    await page.goto("/observe/requests/101/audit?audit_id=201");
    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });

    const responseSection = detail.getByRole("region", { name: "响应（200）" });
    await responseSection.getByLabel("响应（200） payload view").getByRole("button", { name: "消息" }).click();
    const toolCard = responseSection.getByTestId("tool-call-card");
    await expect(toolCard).toBeVisible();
    await expect(toolCard.getByText("get_weather")).toBeVisible();
    await expect(toolCard.getByText('{"city":"Paris"}')).toBeVisible();
  });

  test("non-stream JSON request renders the operation-aware message document", async ({ page }) => {
    await mockPrismRoutes(page, "openai_document");

    await page.goto("/observe/requests/101/audit?audit_id=201");
    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });

    const requestSection = detail.getByRole("region", { name: "请求", exact: true });
    await expect(requestSection.getByText("消息记录")).toBeVisible();
    await expect(requestSection.getByText("Reply with exactly ok.")).toBeVisible();
  });
});
