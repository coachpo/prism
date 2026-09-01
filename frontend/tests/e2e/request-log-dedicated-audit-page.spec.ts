import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";

import {
  copiedText,
  documentBodyCases,
  expectAuditWindow,
  installCopyHarness,
  longRepeatedRequestToken,
  mockPrismRoutes,
  openAiDocumentRequestBody,
  redactedHeaders,
  usedDedicatedFallbackRoot,
} from "./request-log-dedicated-audit-fixtures";

const removedScopeLabels = new RegExp([["Default", "profile"].join("\\s+"), "Global"].join("|"));

function createRequestLogDetailFixture() {
  return {
    summary: {
      request_log_id: "101",
      created_at: "2026-08-09T10:00:00Z",
      ingress_model_id: "gpt-4o",
      model_label: "GPT-4o",
      attempt_target_model_id: null,
      attempt_target_model_label: null,
      is_proxy_origin: false,
      api_family: "openai",
      status_code: 200,
      response_time_ms: 300,
      ttft_ms: 120,
      completion_duration_ms: null,
      is_stream: false,
      stream_outcome: "not_streaming",
      stream_error_kind: null,
      stream_error_detail: null,
    },
    request: {
      request_path: "/v1/chat/completions",
      ingress_request_id: "ingress-101",
      attempt_number: 1,
      provider_correlation_id: "corr-101",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Prism QA Browser",
      upstream_user_agent: "Prism QA Browser",
      caller_client_display: "Prism QA Browser",
      upstream_client_display: "Prism QA Browser",
      user_agent_overridden: false,
      request_generation_params: null,
      request_generation_params_status: null,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      endpoint_label: "OpenRouter",
      endpoint_id: 7,
      terminal_target_id: null,
      selected_terminal_target_id: null,
      endpoint_base_url: "https://openrouter.test",
      endpoint_description: "OpenRouter",
      audit_enabled_at_request: true,
      audit_capture_bodies_at_request: false,
    },
    usage: {
      input_tokens: 12,
      output_tokens: 100,
      total_tokens: 500,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 1000,
      output_cost_micros: 2000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 1200,
      total_cost_user_currency_micros: 1200,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: "1",
      fx_rate_source: "manual",
    },
    pricing: {
      pricing_snapshot_unit: "1M tokens",
      pricing_snapshot_input: "0.10",
      pricing_snapshot_output: "0.20",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: 1,
      pricing_status: "priced",
      pricing_evidence_trust: "trusted",
      pricing_template_id: null,
      reporting_currency_epoch: 1,
      legacy_pricing_evidence: null,
    },
    caliber: {},
    dataset_coverage: {},
    samples: {},
  };
}

test.describe("dedicated request-log audit page", () => {
  for (const bodyCase of documentBodyCases) {
    test(`renders ${bodyCase.label} request and response audit bodies as documents`, async ({ page }) => {
      const counters = await mockPrismRoutes(page, bodyCase.scenario);

      await page.goto("/observe/requests/101/audit?audit_id=201");

      const detail = page.getByTestId("dedicated-audit-detail");
      await expect(detail).toBeVisible({ timeout: 15000 });
      await expect(page.locator("header")).not.toContainText(removedScopeLabels);
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

  test("renders JSON array headers as key-value rows with sensitive values masked", async ({ page }) => {
    await mockPrismRoutes(page, "json_headers");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });

    const requestHeaders = detail.getByRole("region", { name: "请求头" });
    await expect(requestHeaders.locator("dt", { hasText: "authorization" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(requestHeaders.locator("dt", { hasText: "content-type" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "application/json" })).toBeVisible();
    await expect(requestHeaders.locator("dt", { hasText: "user-agent" })).toBeVisible();
    await expect(requestHeaders.locator("dd", { hasText: "prism-postdual-overflow-gpt55-deepseek-1781125557" })).toBeVisible();
    await expect(requestHeaders.getByText("Bearer live-secret-token")).toHaveCount(0);
    await expect(requestHeaders.getByText("session=live-cookie")).toHaveCount(0);

    const responseHeaders = detail.getByRole("region", { name: "响应头" });
    await expect(responseHeaders.locator("dt", { hasText: "access-control-allow-credentials" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "true" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "strict-transport-security" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "max-age=31536000; includeSubDomains; preload" })).toBeVisible();
    await expect(responseHeaders.locator("dt", { hasText: "set-cookie" })).toHaveCount(2);
    await expect(responseHeaders.locator("dt", { hasText: "x-client-credential" })).toBeVisible();
    await expect(responseHeaders.locator("dd", { hasText: "[REDACTED]" }).first()).toBeVisible();
    await expect(responseHeaders.getByText("session=live-response-cookie")).toHaveCount(0);
    await expect(responseHeaders.getByText("live-client-credential")).toHaveCount(0);

    await requestHeaders.getByRole("button", { name: "原始 JSON" }).click();
    await expect(requestHeaders.locator("pre")).toContainText('"name": "authorization"');
    await expect(requestHeaders.locator("pre")).toContainText('"value": "[REDACTED]"');
    await expect(requestHeaders.locator("pre")).toContainText('"value": "prism-postdual-overflow-gpt55-deepseek-1781125557"');
    await expect(requestHeaders.locator("pre")).not.toContainText("Bearer live-secret-token");

    await responseHeaders.getByRole("button", { name: "原始 JSON" }).click();
    await expect(responseHeaders.locator("pre")).toContainText('"name": "access-control-allow-credentials"');
    await expect(responseHeaders.locator("pre")).toContainText('"value": "true"');
    await expect(responseHeaders.locator("pre")).toContainText('"name": "set-cookie"');
    await expect(responseHeaders.locator("pre")).toContainText('"value": "[REDACTED]"');
    await expect(responseHeaders.locator("pre")).toContainText('"name": "x-client-credential"');
    await expect(responseHeaders.locator("pre")).toContainText('"name": "vary"');
    await expect(responseHeaders.locator("pre")).toContainText('"value": "origin, access-control-request-method, access-control-request-headers"');
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
    // Breadcrumbs are fixed at group -> page -> entity, and the leaf is the
    // entity rather than a generic word.
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("可观测性");
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("请求审计");
    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("#101");
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
    console.log("AUDIT URL AFTER CLICK:", page.url());

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
    // 请求与响应两处共用同一条合并后的文案，因此断言出现两次而不是两条不同文案。
    await expect(page.getByText("仅元数据审计不会存储正文。")).toHaveCount(2);
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

    await page.goto("/observe/requests?view=attempts");
    const requestLogRow = page.getByTestId("request-logs-table").getByRole("button").filter({ hasText: "GPT-4o mini" });
    await requestLogRow.click();

    const drawer = page.getByTestId("request-log-detail-sheet");
    await expect(drawer).toBeVisible({ timeout: 15000 });
    await expect(drawer.getByRole("tab", { name: "审计" })).toHaveCount(0);
    await expect(drawer.getByText("查看入口模型、本次尝试目标模型、规划首选与实际终端目标，以及路由、令牌、费用和请求时审计来源。")).toBeVisible();
    await expect(drawer.getByTestId("request-log-overview-grid").getByText("/v1/responses")).toBeVisible();
    await expect(drawer.getByRole("link", { name: "打开完整审计页" })).toHaveAttribute("href", "/observe/requests/101/audit");
    await expect(page).toHaveURL(/\/observe\/requests\?selected_request_id=101&view=attempts$/);
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("Requests exposes retained upstream identity and exports the same field", async ({
    page,
  }) => {
    await mockPrismRoutes(page, "full");
    await page.goto("/observe/requests?view=attempts");

    await page.getByTestId("request-log-column-toggle-trigger").click();
    const upstreamColumn = page.getByRole("menuitemcheckbox", {
      name: "上游模型 ID",
    });
    await expect(upstreamColumn).toHaveAttribute("aria-checked", "false");
    await upstreamColumn.click();
    await expect(upstreamColumn).toHaveAttribute("aria-checked", "true");
    await page.getByRole("button", { name: "关闭列选择" }).click();

    await page.getByTestId("request-log-row-101").click();
    const drawer = page.getByTestId("request-log-detail-sheet");
    await expect(
      drawer
        .getByTestId("request-log-overview-grid")
        .getByText("provider/gpt-4o-mini", { exact: true }),
    ).toBeVisible();
    await expect(drawer.getByTestId("final-upstream-model-id")).toContainText(
      "provider/gpt-4o-mini",
    );
    await page.keyboard.press("Escape");

    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.getByTestId("request-logs-export-csv").click(),
    ]);
    const downloadPath = await download.path();
    expect(downloadPath).not.toBeNull();
    expect(await readFile(downloadPath!, "utf8")).toBe(
      "attempt_target_model_id,upstream_model_id\ngpt-4o-mini,provider/gpt-4o-mini",
    );
  });
});

test("request-log sheet navigates previous/next with named controls and ArrowUp/Down", async ({ page }) => {
  const rows = [
    {
      request_log_id: "101",
      profile_id: 1,
      created_at: "2026-08-09T10:00:00Z",
      ingress_model_id: "gpt-4o",
      model_label: "GPT-4o",
      attempt_target_model_id: "gpt-4o",
      attempt_target_model_label: "GPT-4o",
      api_family: "openai",
      endpoint_id: 7,
      endpoint_label: "OpenRouter",
      status_code: 200,
      response_time_ms: 300,
      ttft_ms: 120,
      is_stream: false,
      stream_outcome: "not_streaming",
      output_tokens: 100,
      total_tokens: 500,
      total_cost_user_currency_micros: 1200,
      priced_flag: true,
      row_kind: "upstream",
      attempt_number: 1,
    },
    {
      request_log_id: "102",
      profile_id: 1,
      created_at: "2026-08-09T10:01:00Z",
      ingress_model_id: "gpt-4o",
      model_label: "GPT-4o",
      attempt_target_model_id: "gpt-4o",
      attempt_target_model_label: "GPT-4o",
      api_family: "openai",
      endpoint_id: 7,
      endpoint_label: "OpenRouter",
      status_code: 200,
      response_time_ms: 320,
      ttft_ms: 130,
      is_stream: false,
      stream_outcome: "not_streaming",
      output_tokens: 90,
      total_tokens: 480,
      total_cost_user_currency_micros: 1100,
      priced_flag: true,
      row_kind: "upstream",
      attempt_number: 1,
    },
  ];
  await mockPrismRoutes(page, "metadata_only");
  await page.route("**/api/stats/requests*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: rows,
        total: 2,
        limit: 50,
        offset: 0,
        filter_options: { endpoints: [], ingress_models: [], clients: [], attempt_target_models: [] },
        caliber: {},
        dataset_coverage: {},
        samples: {},
      }),
    }),
  );
  await page.route("**/api/stats/requests/101", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...createRequestLogDetailFixture(), summary: { ...createRequestLogDetailFixture().summary, request_log_id: "101" } }) }),
  );
  await page.route("**/api/stats/requests/102", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...createRequestLogDetailFixture(), summary: { ...createRequestLogDetailFixture().summary, request_log_id: "102" } }) }),
  );

  await page.goto("/observe/requests?view=attempts");
  const table = page.getByTestId("request-logs-table");
  await expect(table).toBeVisible();
  await page.getByTestId("request-log-row-101").click();
  const sheet = page.getByTestId("request-log-detail-sheet");
  await expect(sheet).toBeVisible();

  const previousButton = page.getByTestId("sheet-previous");
  const nextButton = page.getByTestId("sheet-next");
  // First row: previous disabled, next enabled.
  await expect(previousButton).toBeDisabled();
  await expect(nextButton).toBeEnabled();

  // ArrowDown navigates to the next loaded row.
  await page.keyboard.press("ArrowDown");
  await expect(sheet).toContainText("请求 #102");
  await expect(nextButton).toBeDisabled();
  await expect(previousButton).toBeEnabled();

  // ArrowUp navigates back.
  await page.keyboard.press("ArrowUp");
  await expect(sheet).toContainText("请求 #101");
});
