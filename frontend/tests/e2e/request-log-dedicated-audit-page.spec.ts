import { expect, test } from "@playwright/test";

import {
  copiedText,
  documentBodyCases,
  expectAuditWindow,
  expectNoRedundantPayloadShell,
  installCopyHarness,
  longRepeatedRequestToken,
  mockPrismRoutes,
  openAiDocumentRequestBody,
  redactedHeaders,
  usedDedicatedFallbackRoot,
} from "./request-log-dedicated-audit-fixtures";

const removedScopeLabels = new RegExp([["Default", "profile"].join("\\s+"), "Global"].join("|"));

test.describe("dedicated request-log audit page", () => {
  for (const bodyCase of documentBodyCases) {
    test(`renders ${bodyCase.label} request and response audit bodies as documents`, async ({ page }) => {
      const counters = await mockPrismRoutes(page, bodyCase.scenario);

      await page.goto("/observe/requests/101/audit?audit_id=201");

      const detail = page.getByTestId("dedicated-audit-detail");
      await expect(detail).toBeVisible({ timeout: 15000 });
      await expect(page.locator("header")).not.toContainText(removedScopeLabels);
      const requestSection = detail.getByRole("region", { name: "请求", exact: true });
      const responseSection = detail.getByRole("region", { name: "响应（200）" });
      await expectNoRedundantPayloadShell(requestSection);
      await expectNoRedundantPayloadShell(responseSection);
      await expect(detail.getByText("[REDACTED]").first()).toBeVisible();
      for (const label of bodyCase.requestLabels) {
        await expect(detail.getByText(label).first()).toBeVisible();
      }
      for (const label of bodyCase.responseLabels) {
        await expect(detail.getByText(label).first()).toBeVisible();
      }
      await expect(detail.getByText(bodyCase.rawBodyPattern)).toHaveCount(0);
      expect(counters.auditDetailRequests).toEqual([201]);
    });
  }

  test("renders JSON and newline headers as key-value rows with sensitive values masked", async ({ page }) => {
    await mockPrismRoutes(page, "json_headers");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });

    const requestHeaders = detail.getByRole("region", { name: "请求头" });
    await expectNoRedundantPayloadShell(requestHeaders);
    await expect(requestHeaders.locator("dt", { hasText: "authorization" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(requestHeaders.locator("dt", { hasText: "content-type" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "application/json" })).toBeVisible();
    await expect(requestHeaders.locator("dt", { hasText: "user-agent" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "prism-postdual-overflow-gpt55-deepseek-1781125557" })).toBeVisible();
    await expect(requestHeaders.getByText("Bearer live-secret-token")).toHaveCount(0);
    await expect(requestHeaders.getByText("session=live-cookie")).toHaveCount(0);
    await expect(requestHeaders.getByText(/\{\s*"authorization"/)).toHaveCount(0);

    const responseHeaders = detail.getByRole("region", { name: "响应头" });
    await expectNoRedundantPayloadShell(responseHeaders);
    await expect(responseHeaders.locator("dt", { hasText: "access-control-allow-credentials" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "true" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "strict-transport-security" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "max-age=31536000; includeSubDomains; preload" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "set-cookie" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "x-client-credential" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(responseHeaders.getByText("session=live-response-cookie")).toHaveCount(0);
    await expect(responseHeaders.getByText("live-client-credential")).toHaveCount(0);

    await requestHeaders.getByRole("button", { name: "原始 JSON" }).click();
    await expect(requestHeaders.locator("pre")).toContainText('"authorization": "[REDACTED]"');
    await expect(requestHeaders.locator("pre")).toContainText('"user-agent": "prism-postdual-overflow-gpt55-deepseek-1781125557"');
    await expect(requestHeaders.locator("pre")).not.toContainText("Bearer live-secret-token");

    await responseHeaders.getByRole("button", { name: "原始 JSON" }).click();
    await expect(responseHeaders.locator("pre")).toContainText('"access-control-allow-credentials": "true"');
    await expect(responseHeaders.locator("pre")).toContainText('"set-cookie": "[REDACTED]"');
    await expect(responseHeaders.locator("pre")).toContainText('"x-client-credential": "[REDACTED]"');
    await expect(responseHeaders.locator("pre")).toContainText('"vary": "origin, access-control-request-method, access-control-request-headers"');
    await expect(responseHeaders.locator("pre")).not.toContainText("session=live-response-cookie");
    await expect(responseHeaders.locator("pre")).not.toContainText("live-client-credential");
  });

  test("local Raw JSON toggle pretty-prints parseable request bodies", async ({ page }) => {
    await mockPrismRoutes(page, "openai_document");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });
    const requestSection = detail.getByRole("region", { name: "请求", exact: true });
    await expect(requestSection.getByText("Message transcript")).toBeVisible();
    await requestSection.getByRole("button", { name: "原始 JSON" }).click();
    await expect(requestSection.getByRole("button", { name: "原始 JSON" })).toHaveAttribute("aria-pressed", "true");
    await expect(requestSection.locator("pre")).toContainText('"model": "gpt-4o-mini"');
    await expect(requestSection.locator("pre")).toContainText('"messages": [');
    await expect(requestSection.getByText("Message transcript")).toHaveCount(0);
  });

  test("long repeated-token request bodies scroll inside the Request Body content area only", async ({ page }) => {
    await mockPrismRoutes(page, "long_body");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });
    const requestSection = detail.getByRole("region", { name: "请求", exact: true });
    await expectNoRedundantPayloadShell(requestSection);
    const requestContent = requestSection.getByTestId("request-log-request-body-content");
    await expect(requestSection.getByRole("button", { name: "渲染视图" })).toBeVisible();
    await expect(requestSection.getByRole("button", { name: "原始 JSON" })).toBeVisible();
    await expect(requestSection.getByRole("button", { name: "复制" })).toBeVisible();
    await expect(requestSection.getByText(longRepeatedRequestToken.slice(0, 80))).toBeVisible();
    await expect(requestContent).toHaveCSS("overflow-y", "auto");
    await expect(requestContent.locator("[data-radix-scroll-area-viewport], article .overflow-y-auto")).toHaveCount(0);

    const renderedMetrics = await requestContent.evaluate((element) => ({
      clientHeight: element.clientHeight,
      maxHeight: getComputedStyle(element).maxHeight,
      scrollHeight: element.scrollHeight,
      viewportHeight: window.innerHeight,
    }));
    expect(renderedMetrics.maxHeight).not.toBe("none");
    expect(renderedMetrics.clientHeight).toBeLessThanOrEqual(Math.ceil(renderedMetrics.viewportHeight * 0.9) + 2);
    expect(renderedMetrics.scrollHeight).toBeGreaterThan(renderedMetrics.clientHeight);

    await requestSection.getByRole("button", { name: "原始 JSON" }).click();
    await expect(requestSection.getByRole("button", { name: "原始 JSON" })).toHaveAttribute("aria-pressed", "true");
    await expect(requestContent.locator("pre")).toContainText('"input": "request-token');
    const rawMetrics = await requestContent.evaluate((element) => ({
      clientHeight: element.clientHeight,
      scrollHeight: element.scrollHeight,
    }));
    expect(rawMetrics.scrollHeight).toBeGreaterThan(rawMetrics.clientHeight);

    const responseSection = detail.getByRole("region", { name: "响应（200）" });
    await expect(responseSection.getByTestId("request-log-request-body-content")).toHaveCount(0);
  });

  test("raw-mode copy writes the pretty-printed JSON shown in that section", async ({ page, context }) => {
    await installCopyHarness(page, context);
    await mockPrismRoutes(page, "openai_document");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });
    const requestSection = detail.getByRole("region", { name: "请求", exact: true });
    await requestSection.getByRole("button", { name: "原始 JSON" }).click();
    await requestSection.getByRole("button", { name: "复制" }).click();
    await expect.poll(() => copiedText(page)).toBe(JSON.stringify(JSON.parse(openAiDocumentRequestBody), null, 2));
    await expect.poll(() => usedDedicatedFallbackRoot(page)).toBe(true);
  });

  test("direct selected audit_id route fetches only the selected audit detail", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit?audit_id=202");

    await expect(page.getByTestId("dedicated-request-log-audit-page")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("请求日志");
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("#101");
    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("审计");
    await expect(page.getByText("selected audit request body")).toBeVisible();
    await expect(page.getByText("selected audit response body")).toBeVisible();
    await expect(page.getByText("original request body")).toHaveCount(0);
    expect(counters.auditListSearchParams).toHaveLength(1);
    expectAuditWindow(counters.auditListSearchParams[0]);
    expect(counters.auditDetailRequests).toEqual([202]);
  });

  test("audit cursor pagination keeps the cursor in the URL and fetches the next page", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByTestId("dedicated-audit-list")).toContainText("#201", { timeout: 15000 });
    await expect(page.getByRole("link", { name: "下一页" })).toHaveAttribute("href", "/observe/requests/101/audit?cursor=page-2");
    await page.getByRole("link", { name: "下一页" }).click();

    await expect(page).toHaveURL(/\/observe\/requests\/101\/audit\?cursor=page-2$/);
    await expect(page.getByTestId("dedicated-audit-list")).toContainText("#202");
    await expect(page.getByRole("link", { name: "上一页" })).toHaveAttribute("href", "/observe/requests/101/audit");
    expect(counters.auditListSearchParams).toHaveLength(2);
    expect(new URLSearchParams(counters.auditListSearchParams[1]).get("cursor")).toBe("page-2");
    expect(counters.auditDetailRequests).toEqual([201, 202]);
  });

  test("disabled audit requests do not call audit APIs", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "disabled");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("请求开始时已禁用审计").first()).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("metadata-only audit shows no-body copy state and copies original redacted headers", async ({ page, context }) => {
    await installCopyHarness(page, context);
    const counters = await mockPrismRoutes(page, "metadata_only");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("仅元数据").first()).toBeVisible({ timeout: 15000 });
    const requestHeaders = page.getByTestId("dedicated-audit-detail").getByRole("region", { name: "请求头" });
    await expect(requestHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(page.getByText("由于该请求使用的是仅元数据审计捕获，因此请求正文被有意不存储。")).toBeVisible();
    await expect(page.getByText("由于该请求使用的是仅元数据审计捕获，因此响应正文被有意不存储。")).toBeVisible();
    const copyButtons = page.getByTestId("dedicated-audit-detail").getByRole("button", { name: /^复制$/ });
    await expect(copyButtons).toHaveCount(4);
    await expect(copyButtons.nth(1)).toBeDisabled();
    await expect(copyButtons.nth(3)).toBeDisabled();
    await requestHeaders.locator("dd", { hasText: "[REDACTED]" }).first().evaluate((element) => {
      element.textContent = "mutated header text";
    });
    await copyButtons.first().click();
    await expect.poll(() => copiedText(page)).toBe(redactedHeaders);
    await expect.poll(() => usedDedicatedFallbackRoot(page)).toBe(true);
    expect(counters.auditDetailRequests).toEqual([201]);
  });

  test("missing request state renders without audit calls", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "missing_request");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("未找到请求")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("no audit records state does not fetch audit details", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "no_records");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("此请求未找到审计记录。")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("unmatched audit_id renders missing-audit state with a return action", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit?audit_id=999");

    await expect(page.getByText("此请求未找到该审计记录")).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole("link", { name: "显示默认审计记录" })).toHaveAttribute("href", "/observe/requests/101/audit");
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit list failure does not fetch audit details", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "list_failure");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("审计记录加载失败")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit detail failure preserves the audit list", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "detail_failure");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    await expect(page.getByText("审计记录加载失败")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("dedicated-audit-list")).toContainText("#201");
    expect(counters.auditDetailRequests).toEqual([201]);
  });

  test("invalid request timestamp prevents audit lookup", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "invalid_created");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("请求时间戳无效")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("request-log row clicks still open the overview drawer with a full audit page link", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests");
    const requestLogRow = page.getByTestId("request-logs-table").getByRole("button").filter({ hasText: "GPT-4o mini" });
    await requestLogRow.click();

    const drawer = page.getByTestId("request-log-detail-sheet");
    await expect(drawer).toBeVisible({ timeout: 15000 });
    await expect(drawer.getByRole("tab", { name: "审计" })).toHaveCount(0);
    await expect(drawer.getByText("查看请求模型、最终目标模型、已选择的终端目标，以及路由、令牌、费用和请求时审计来源。")).toBeVisible();
    await expect(drawer.getByTestId("request-log-overview-grid").getByText("/v1/responses")).toBeVisible();
    await expect(drawer.getByRole("link", { name: "打开完整审计页" })).toHaveAttribute("href", "/observe/requests/101/audit");
    await expect(page).toHaveURL(/\/observe\/requests\?selected_request_id=101$/);
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });
});
