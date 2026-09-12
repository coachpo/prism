import { expect, test } from "@playwright/test";
import {
  createRequestLogListItem,
  mockPrismRoutes,
} from "./request-log-dedicated-audit-fixtures";

test("request comparison is explicit and mobile summary preserves investigation", async ({
  page,
}) => {
  const calls = await mockPrismRoutes(page, "full");
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/observe/requests");
  const summary = page.getByTestId("request-mobile-summary");
  await expect(summary).toBeVisible();
  await expect(
    summary.getByRole("button", { name: "查看结果与下一步" }),
  ).toBeVisible();
  expect(calls.auditDetailRequests).toEqual([]);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await summary.getByRole("button", { name: "查看结果与下一步" }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  expect(calls.auditDetailRequests).toEqual([]);
  await page.keyboard.press("Escape");
  await expect(summary.getByRole("button", { name: "查看结果与下一步" })).toBeFocused();
  await summary.getByRole("link", { name: "查看输入与回复" }).click();
  await expect(page).toHaveURL(/\/observe\/requests\/101\/audit/);
  await expect.poll(() => calls.auditDetailRequests.length).toBeGreaterThan(0);

  await page.route("**/api/stats/requests?**", async (route) => {
    if (new URL(route.request().url()).searchParams.get("view") !== "attempts")
      return route.fallback();
    const first = createRequestLogListItem();
    await route.fulfill({
      json: {
        items: [first, { ...first, request_log_id: "102" }],
        total: 2,
        total_is_exact: true,
        has_more: false,
        limit: 100,
        offset: 0,
        filter_options: {
          ingress_models: [],
          endpoints: [],
          clients: [],
          attempt_target_models: [],
        },
        caliber: {},
        dataset_coverage: {},
        samples: {},
      },
    });
  });
  await page.route("**/api/stats/requests/102", (route) =>
    route.fulfill({
      json: {
        summary: {
          request_log_id: "102",
          created_at: "2026-08-09T10:00:00Z",
          ingress_model_id: "other-model",
          api_family: "openai",
          row_kind: "upstream",
          upstream_status_code: 200,
          attempt_duration_ms: 50,
        },
        routing: {
          audit_enabled_at_request: true,
          audit_capture_bodies_at_request: true,
        },
      },
    }),
  );
  await page.route("**/api/audit/logs?**", async (route) => {
    if (
      new URL(route.request().url()).searchParams.get("request_log_id") !==
      "102"
    )
      return route.fallback();
    await route.fulfill({
      json: {
        items: [{ id: 302, request_log_id: "102", row_kind: "upstream" }],
        has_more: false,
        next_cursor: null,
      },
    });
  });
  await page.route("**/api/audit/logs/302", (route) =>
    route.fulfill({
      json: {
        id: 302,
        request_log_id: "102",
        request_body_stored: true,
        request_body_base64: Buffer.from('{"messages":[{"role":"user","content":"other-model"}]}').toString(
          "base64",
        ),
        response_body_stored: true,
        response_body_base64: Buffer.from(
          '{"choices":[{"message":{"role":"assistant","content":"different"}}]}',
        ).toString("base64"),
        response_body_truncated: true,
        response_body_bytes_observed: 200,
        response_body_bytes_stored: 29,
      },
    }),
  );
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/observe/requests?view=attempts");
  const prior = calls.auditDetailRequests.length;
  await page.getByRole("button", { name: "打开请求比较" }).click();
  await page.getByRole("combobox", { name: "基准请求 A", exact: true }).click();
  await page.getByRole("option", { name: /^#101/ }).click();
  await page.getByRole("combobox", { name: "对照请求 B", exact: true }).click();
  await page.getByRole("option", { name: /^#102/ }).click();
  await expect(
    page.getByRole("combobox", { name: "基准请求 A 选择内容记录" }),
  ).toBeVisible();
  expect(calls.auditDetailRequests).toHaveLength(prior);
  for (const side of ["基准请求 A", "对照请求 B"]) {
    await page
      .getByRole("combobox", { name: `${side} 选择内容记录` })
      .click();
    await page.getByRole("option").first().click();
    await page
      .getByRole("region", { name: side, exact: true })
      .getByRole("button", { name: "读取所选内容" })
      .click();
  }
  await expect(page.getByTestId("request-comparison-diff")).toContainText(
    "different",
  );
  await expect(page.getByText("已截断：仅比较保留前缀")).toBeVisible();
  await page.getByRole("combobox", { name: "比较方向", exact: true }).click();
  await page.getByRole("option", { name: "请求方向", exact: true }).click();
  await expect(page.getByTestId("request-comparison-diff")).toContainText(
    "other-model",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});
