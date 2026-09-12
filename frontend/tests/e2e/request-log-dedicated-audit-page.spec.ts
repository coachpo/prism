import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";

import {
  copiedText,
  createRequestLogDetail,
  documentBodyCases,
  expectAuditWindow,
  installCopyHarness,
  longRepeatedRequestToken,
  mockPrismRoutes,
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
      await expect(detail.getByRole("region", { name: "请求头" })).toHaveCount(0);
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

  test("conversation excludes headers, internal fields and raw payload controls", async ({ page }) => {
    await mockPrismRoutes(page, "json_headers");
    await page.goto("/observe/requests/101/audit?audit_id=201");
    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible();
    await expect(detail.getByRole("region", { name: "请求头" })).toHaveCount(0);
    await expect(detail).not.toContainText(/authorization|user-agent|prism-postdual|Bearer|原始 JSON|request_method/);
  });

  test("long repeated-token request bodies scroll inside the Request Body content area only", async ({ page }) => {
    await mockPrismRoutes(page, "long_body");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    const detail = page.getByTestId("dedicated-audit-detail");
    await expect(detail).toBeVisible({ timeout: 15000 });
    const requestSection = detail.getByRole("region", { name: "请求", exact: true });
    const requestContent = requestSection.getByTestId("request-log-request-body-content");
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

    const responseSection = detail.getByRole("region", { name: "响应（200）" });
    await expect(responseSection.getByTestId("request-log-request-body-content")).toHaveCount(0);
  });

  test("copy keeps the readable conversation and uses the page fallback scope", async ({ page, context }) => {
    await installCopyHarness(page, context);
    await mockPrismRoutes(page, "openai_document");
    await page.goto("/observe/requests/101/audit?audit_id=201");
    const request = page.getByTestId("dedicated-audit-detail").getByRole("region", { name: "请求", exact: true });
    await request.getByRole("button", { name: "复制" }).click();
    await expect.poll(() => copiedText(page)).toBe("系统\nYou are concise.\n\n用户\nReply with exactly ok.");
    await expect.poll(() => usedDedicatedFallbackRoot(page)).toBe(true);
  });

  test("direct selected audit_id route fetches only the selected audit detail", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit?audit_id=202");

    await expect(page.getByTestId("dedicated-request-log-audit-page")).toBeVisible({ timeout: 15000 });
    // Breadcrumbs are fixed at group -> page -> entity, and the leaf is the
    // entity rather than a generic word.
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("使用情况");
    await expect(page.getByTestId("shell-breadcrumb")).toContainText("请求内容");
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

    await expect(page.getByText("这次请求未开启内容保存").first()).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("audit-request-result")).toContainText("完成");
    const interrupted = createRequestLogDetail("disabled");
    await page.route("**/api/stats/requests/101", route => route.fulfill({ json: {
      ...interrupted,
      summary: { ...interrupted.summary, is_stream: true, upstream_status_code: 200, stream_outcome: "upstream_read_error", stream_error_kind: "upstream_read_failed" },
    } }));
    await page.reload();
    const result = page.getByTestId("audit-request-result");
    await expect(result).toContainText("回复中断");
    await expect(result).not.toContainText("完成");
    await expect(result).not.toContainText("200");
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("metadata-only content states that conversation was not saved", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "metadata_only");
    await page.goto("/observe/requests/101/audit");
    await expect(page.getByText("仅保存概要").first()).toBeVisible();
    await expect(page.getByText("仅保存概要审计不会存储正文。")).toHaveCount(2);
    await expect(page.getByTestId("dedicated-audit-detail").getByRole("button", { name: "复制" })).toHaveCount(0);
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

    await expect(page.getByText("此请求未找到内容记录。")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("unmatched audit_id renders missing-audit state with a return action", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    await page.goto("/observe/requests/101/audit?audit_id=999");

    await expect(page.getByText("此请求未找到该内容记录")).toBeVisible({ timeout: 15000 });
    await expect(page.getByRole("link", { name: "显示默认内容记录" })).toHaveAttribute("href", "/observe/requests/101/audit");
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit list failure does not fetch audit details", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "list_failure");

    await page.goto("/observe/requests/101/audit");

    await expect(page.getByText("内容记录加载失败")).toBeVisible({ timeout: 15000 });
    expect(counters.auditListSearchParams).toHaveLength(1);
    expect(counters.auditDetailRequests).toEqual([]);
  });

  test("audit detail failure preserves the audit list", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "detail_failure");

    await page.goto("/observe/requests/101/audit?audit_id=201");

    await expect(page.getByText("内容记录加载失败")).toBeVisible({ timeout: 15000 });
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

  test("failed request explains recovery and preserves the list when returning from content", async ({ page }) => {
    const counters = await mockPrismRoutes(page, "full");

    const detail = createRequestLogDetail("full");
    await page.route("**/api/models", route => route.fulfill({ json: [{ id: 7, model_id: "gpt-4o-mini", api_family: "openai", direct_request_enabled: true, incoming_model_target_count: 0, access_targets: [], loadbalance_strategy: null, configuration_warnings: [] }] }));
    await page.route("**/api/stats/requests/101", route => route.fulfill({ json: {
      ...detail, summary: { ...detail.summary, upstream_status_code: 404 },
      failure: { category: "upstream_http", detail: "upstream_http_404 SQL /private/debug", code: "upstream_http_404" },
      terminal_target: { owner_model_config_id: "gpt-4o-mini", terminal_target_id: "9", configured: true },
    } }));
    await page.goto("/observe/requests?view=attempts&status_code=404");
    const lookup = page.getByRole("textbox", { name: "查找请求", exact: true });
    await expect(lookup).toHaveCount(1);
    await page.route("**/api/stats/requests/999", route => route.fulfill({ status: 404, json: { detail: "Request not found" } }));
    await lookup.fill("#999");
    await page.getByRole("button", { name: "查找", exact: true }).click();
    await expect(page.getByText("未找到请求", { exact: true })).toBeVisible();
    await lookup.fill("ingress-101");
    const ingressRead = page.waitForRequest(request => {
      const url = new URL(request.url());
      return url.pathname === "/api/stats/requests" && url.searchParams.get("ingress_request_id") === "ingress-101";
    });
    await lookup.press("Enter");
    await ingressRead;
    await expect(page).toHaveURL(/ingress_request_id=ingress-101/);
    await expect(page.getByTestId("filter-chip-ingress_request_id")).toContainText("ingress-101");
    await expect(page.getByText("未找到请求", { exact: true })).toHaveCount(0);
    await lookup.fill("#101");
    await page.getByRole("button", { name: "查找", exact: true }).click();
    await expect(page).toHaveURL(/request_id=101/);
    expect(new URL(page.url()).searchParams.has("ingress_request_id")).toBe(false);

    const drawer = page.getByTestId("request-log-detail-sheet");
    await expect(drawer).toBeVisible({ timeout: 15000 });
    await expect(drawer.getByRole("tab", { name: "审计" })).toHaveCount(0);
    await expect(drawer.getByText("查看这次请求的结果、费用和使用的服务；遇到失败时，可直接检查相关配置。")).toBeVisible();
    await expect(drawer.getByText("/v1/responses")).toHaveCount(0);
    await expect(drawer.getByRole("link", { name: "查看输入与回复" })).toHaveAttribute("href", /\/observe\/requests\/101\/audit\?return_to=/);
    const recovery = drawer.getByTestId("request-failure-recovery");
    await expect(recovery).toContainText("记录本身不能确定是哪一项有误");
    await expect(recovery.getByRole("link", { name: "检查模型配置" })).toHaveAttribute("href", /\/route\/models\/7.*focus_connection_id=9/);
    await expect(recovery.getByRole("link", { name: "检查服务地址与密钥" })).toHaveAttribute("href", "/route/endpoints?endpoint_id=1");
    await expect(drawer).not.toContainText(/upstream_http_404|SQL|private\/debug|provider-corr/);
    expect(counters.auditListSearchParams).toEqual([]);
    expect(counters.auditDetailRequests).toEqual([]);
    const listUrl = page.url();
    await drawer.getByRole("link", { name: "查看输入与回复" }).click();
    await expect(page.getByTestId("dedicated-request-log-audit-page").getByTestId("request-failure-recovery")).toBeVisible();
    await page.getByRole("link", { name: "返回请求列表", exact: true }).click();
    await expect(page).toHaveURL(listUrl);
    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
    await expect(page.getByTestId("filter-chip-status_code")).toContainText("404");
    await page.keyboard.press("Escape");
    await expect(page.getByRole("textbox", { name: "查找请求", exact: true })).toHaveValue("");
  });

  test("Requests exposes retained upstream identity and exports the same field", async ({
    page,
  }) => {
    await mockPrismRoutes(page, "full");
    await page.goto("/observe/requests?view=attempts");

    await page.getByTestId("request-log-column-toggle-trigger").click();
    const upstreamColumn = page.getByRole("menuitemcheckbox", {
      name: "服务要求的模型名称",
    });
    await expect(upstreamColumn).toHaveAttribute("aria-checked", "false");
    await upstreamColumn.click();
    await expect(upstreamColumn).toHaveAttribute("aria-checked", "true");
    // 列选择器现在是共享的 Radix 菜单：Esc 关闭并把焦点还给触发器。
    await page.keyboard.press("Escape");

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
