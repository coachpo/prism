import { expect, test } from "@playwright/test";

import {
  copiedText,
  createRequestLogListItem,
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

  test("renders JSON array headers as key-value rows with sensitive values masked", async ({ page }) => {
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

    const responseHeaders = detail.getByRole("region", { name: "响应头" });
    await expectNoRedundantPayloadShell(responseHeaders);
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

// Honesty Contract: a failed list read owns the table area. The 422 case used
// to render the failure callout and an empty result at the same time, so the
// screen said "0 行 / 当前范围内没有匹配的请求日志" about a read that never
// returned any rows to count.
test("request-log list read failure replaces the table instead of reading as an empty result", async ({ page }) => {
  await mockPrismRoutes(page, "metadata_only");
  let listReadFails = true;
  await page.route("**/api/stats/requests*", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname !== "/api/stats/requests") return route.fallback();
    if (listReadFails) {
      return route.fulfill({
        status: 422,
        contentType: "application/json",
        body: JSON.stringify({ detail: "view=attempts 与当前筛选不兼容" }),
      });
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [createRequestLogListItem("metadata_only")],
        total: 1,
        limit: 100,
        offset: 0,
        filter_options: { ingress_models: [], endpoints: [], clients: [], attempt_target_models: [] },
        caliber: {},
        dataset_coverage: {},
        samples: {},
      }),
    });
  });

  await page.goto("/observe/requests?view=attempts");

  const failure = page.getByTestId("request-logs-load-error");
  await expect(failure).toBeVisible();
  await expect(failure).toContainText("加载请求日志失败");

  // None of the empty-result wording may appear for a read that failed.
  await expect(page.getByTestId("request-logs-table")).toHaveCount(0);
  await expect(page.getByText("当前范围内没有匹配的请求日志")).toHaveCount(0);
  await expect(page.getByText("共 0 行")).toHaveCount(0);
  await expect(page.getByText("0 条结果")).toHaveCount(0);

  // The server's reason stays reachable behind the details disclosure.
  await failure.getByText("查看详情").click();
  await expect(failure.getByText("view=attempts 与当前筛选不兼容")).toBeVisible();

  // The failure surface carries its own retry.
  listReadFails = false;
  await failure.getByRole("button", { name: "重试" }).click();
  await expect(page.getByTestId("request-log-row-101")).toBeVisible();
  await expect(page.getByTestId("request-logs-load-error")).toHaveCount(0);
});

test("ingress-chains view renders chains with nested rows and expands", async ({ page }) => {
  const chains = {
    view: "ingress_chains",
    query_context: null,
    source_ingress_total: 1,
    retained_ingress_total: 1,
    retained_upstream_attempt_total: 2,
    retained_request_log_row_total: 2,
    legacy_unknown_row_total: 0,
    items: [
      {
        ingress_request_id: "ingress-abc",
        started_at: "2026-08-09T10:00:00Z",
        completed_at: "2026-08-09T10:00:02Z",
        elapsed_ms: 2000,
        elapsed_evidence_state: "authoritative",
        finalized_evidence_state: "authoritative",
        finalized_summary: {
          request_log_id: "113",
          final_status_code: 200,
          final_result: "completed",
          final_error_code: null,
          ingress_model: { id: "entry-a", label: "Model A" },
          final_target_model: { id: "target-c", label: "Model C" },
          terminal_target: { id: 17, label: "TT-C", configured: true, owner_model_id: "target-c" },
          endpoint: { id: 8, label: "Endpoint C" },
          ttft_ms: 610,
          total_tokens: 120,
          total_cost_user_currency_micros: 4000,
          report_currency_symbol: "$",
          final_pricing_status: "priced",
          attempt_count: 2,
        },
        expected_attempt_count: 2,
        expected_request_log_row_count: 2,
        retained_upstream_attempt_count: 2,
        retained_request_log_row_count: 2,
        legacy_unknown_row_count: 0,
        chain_complete: true,
        same_target_retry_occurred: false,
        hedge_occurred: false,
        failover_occurred: true,
        routing_evidence_complete: true,
        retained_rows_loaded_count: 2,
        retained_rows_page_complete: true,
        retained_row_count: 2,
        matched_row_count: 2,
        next_row_cursor: null,
        retained_rows: [
          {
            request_log_id: "112",
            row_kind: "upstream",
            ingress_model_id: "entry-a",
            attempt_target_model_id: "target-b",
            attempt_target_model_label: "Model B",
            endpoint_id: 7,
            endpoint_label: "Endpoint B",
            terminal_target_id: 16,
            terminal_target_label: "TT-B",
            created_at: "2026-08-09T10:00:01Z",
            attempt_number: 1,
            attempt_trigger: "initial",
            attempt_result: "http_error",
            attempt_duration_ms: 820,
            total_tokens: null,
            total_cost_user_currency_micros: null,
            pricing_status: "unknown",
            pricing_evidence_trust: "legacy_untrusted",
            is_winner: false,
            upstream_status_code: 503,
            gateway_status_code: null,
            legacy_status_code: null,
            stream_outcome: "not_streaming",
            stream_error_kind: null,
          },
          {
            request_log_id: "113",
            row_kind: "upstream",
            ingress_model_id: "entry-a",
            attempt_target_model_id: "target-c",
            attempt_target_model_label: "Model C",
            endpoint_id: 8,
            endpoint_label: "Endpoint C",
            terminal_target_id: 17,
            terminal_target_label: "TT-C",
            created_at: "2026-08-09T10:00:02Z",
            attempt_number: 2,
            attempt_trigger: "failover",
            attempt_result: "completed",
            attempt_duration_ms: 610,
            total_tokens: 120,
            total_cost_user_currency_micros: 4000,
            pricing_status: "priced",
            pricing_evidence_trust: "trusted",
            is_winner: true,
            upstream_status_code: 200,
            gateway_status_code: null,
            legacy_status_code: null,
            stream_outcome: "not_streaming",
            stream_error_kind: null,
          },
        ],
      },
    ],
    has_more_chains: true,
    next_chain_cursor: "signed-chain-cursor",
    page_ingress_count: 1,
    page_upstream_attempt_count: 2,
    page_request_log_row_count: 2,
    filter_options: {
      ingress_models: [],
      endpoints: [],
      clients: [],
      attempt_target_models: [],
    },
    caliber: {},
    dataset_coverage: {},
    samples: {},
  };
  await mockPrismRoutes(page, "metadata_only");
  await page.route("**/api/stats/requests*", (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/stats/requests/113") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...createRequestLogDetailFixture(), summary: { ...createRequestLogDetailFixture().summary, request_log_id: "113", attempt_target_model_id: "target-c", attempt_target_model_label: "Model C" } }) });
    }
    if (url.searchParams.get("view") === "ingress_chains") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(chains) });
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [], total: 0, limit: 100, offset: 0, filter_options: { ingress_models: [], endpoints: [], clients: [], attempt_target_models: [] }, caliber: {}, dataset_coverage: {}, samples: {} }) });
  });
  await page.route("**/api/stats/requests/113", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...createRequestLogDetailFixture(), summary: { ...createRequestLogDetailFixture().summary, request_log_id: "113", attempt_target_model_id: "target-c", attempt_target_model_label: "Model C" } }) }),
  );

  await page.goto("/observe/requests?view=ingress_chains");
  const table = page.getByTestId("ingress-chains-table");
  await expect(table).toBeVisible();
  await expect(page.getByTestId("chain-page-counts")).toContainText("1 个入口");
  // The landing view renders the finalized summary the backend already
  // returns, not just an identifier and a count.
  const summaryRow = page.getByTestId("chain-summary-ingress-abc");
  await expect(summaryRow).toBeVisible();
  await expect(page.getByTestId("chain-more")).toBeVisible();

  await summaryRow.getByRole("button", { expanded: false }).click();
  await expect(page.getByTestId("chain-row-112")).toBeVisible();
  await expect(page.getByTestId("chain-row-113")).toBeVisible();
  // Enum keys never reach the screen; the row kind is labelled.
  await expect(page.getByTestId("chain-row-112")).toContainText("上游尝试");
  await expect(page.getByTestId("chain-row-112")).toContainText("HTTP 失败");
  await expect(page.getByTestId("chain-row-113")).toContainText("故障转移");
  await expect(page.getByTestId("chain-row-113")).toContainText("胜出");
  await expect(page.getByText("initial", { exact: true })).toHaveCount(0);
  await expect(page.getByText("failover", { exact: true })).toHaveCount(0);

  // Row click opens the ordinary detail sheet without fetching audit payload.
  await page.getByTestId("chain-row-113").click();
  const sheet = page.getByTestId("request-log-detail-sheet");
  await expect(sheet).toBeVisible();
  await expect(sheet).toContainText("请求 #113");
});
