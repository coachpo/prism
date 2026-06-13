import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";

function createProfile() {
  return {
    id: 1,
    name: "Default",
    description: null,
    is_active: true,
    is_default: true,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  };
}

function createRequestLogItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 101,
    created_at: timestamp,
    model_id: "gpt-4o-mini",
    model_label: "GPT-4o mini",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Completed Stream",
    upstream_client_display: "Completed Stream",
    user_agent_overridden: false,
    api_family: "openai",
    endpoint_id: 1,
    endpoint_label: "Primary endpoint",
    connection_id: null,
    ttft_ms: 120,
    completion_duration_ms: 300,
    status_code: 200,
    response_time_ms: 4500,
    is_stream: true,
    stream_outcome: "completed",
    stream_error_kind: null,
    output_tokens: 150,
    total_tokens: 150,
    total_cost_user_currency_micros: 750000,
    report_currency_symbol: "$",
    ...overrides,
  };
}

function createRequestLogsResponse(
  requestLogItems: Record<string, unknown>[],
  searchParams: URLSearchParams,
) {
  const limit = Number.parseInt(
    searchParams.get("limit") ?? String(requestLogItems.length),
    10,
  );
  const offset = Number.parseInt(searchParams.get("offset") ?? "0", 10);

  return {
    items: requestLogItems.slice(offset, offset + limit),
    total: requestLogItems.length,
    limit,
    offset,
    filter_options: {
      models: [
        {
          model_id: "gpt-4o-mini",
          model_label: "GPT-4o mini",
        },
      ],
      endpoints: [
        {
          endpoint_id: 1,
          endpoint_label: "Primary endpoint",
        },
      ],
      clients: [],
      resolved_target_models: [],
    },
  };
}

function createRequestLogDetail(overrides: Record<string, unknown> = {}) {
  return {
    summary: {
      id: 103,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      model_label: "GPT-4o mini",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      is_proxy_origin: false,
      api_family: "openai",
      status_code: 200,
      response_time_ms: 900,
      ttft_ms: 80,
      completion_duration_ms: null,
      is_stream: false,
      stream_outcome: "client_disconnected",
      stream_error_kind: "client_write_failed",
      stream_error_detail: "client closed response stream",
    },
    request: {
      request_path: "/v1/chat/completions",
      ingress_request_id: "ingress-103",
      attempt_number: 1,
      provider_correlation_id: "provider-corr-103",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Prism QA Browser",
      upstream_user_agent: "Prism QA Browser",
      caller_client_display: "Interrupted Stream",
      upstream_client_display: "Interrupted Stream",
      user_agent_overridden: false,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      endpoint_label: "Primary endpoint",
      endpoint_id: 1,
      connection_id: null,
      endpoint_base_url: "https://api.example.test",
      endpoint_description: "Primary endpoint",
      audit_enabled_at_request: true,
    },
    usage: {
      input_tokens: null,
      output_tokens: null,
      total_tokens: null,
      success_flag: true,
      billable_flag: true,
      priced_flag: false,
      unpriced_reason: "STREAM_USAGE_UNAVAILABLE",
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: null,
      output_cost_micros: null,
      cache_read_input_cost_micros: null,
      cache_creation_input_cost_micros: null,
      reasoning_cost_micros: null,
      total_cost_original_micros: null,
      total_cost_user_currency_micros: null,
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
    },
    ...overrides,
  };
}

function createHistoricalUnknownRequestLogDetail() {
  const detail = createRequestLogDetail();

  return {
    ...detail,
    summary: {
      ...detail.summary,
      id: 105,
      stream_outcome: "unknown",
      stream_error_kind: null,
      stream_error_detail: null,
    },
    request: {
      ...detail.request,
      ingress_request_id: "ingress-105",
      provider_correlation_id: "provider-corr-105",
      caller_client_display: "Historical Unknown",
      upstream_client_display: "Historical Unknown",
    },
    usage: {
      ...detail.usage,
      input_tokens: null,
      output_tokens: null,
      total_tokens: null,
      priced_flag: false,
      unpriced_reason: "MISSING_TOKEN_USAGE",
    },
    costing: {
      ...detail.costing,
      input_cost_micros: null,
      output_cost_micros: null,
      total_cost_original_micros: null,
      total_cost_user_currency_micros: null,
    },
  };
}

async function mockRequestLogRoutes(page: Page) {
  const profile = createProfile();
  const requestLogItems = [
    createRequestLogItem(),
    createRequestLogItem({
      id: 102,
      caller_client_display: "Buffered Completed",
      upstream_client_display: "Buffered Completed",
      ttft_ms: null,
      completion_duration_ms: 400,
      response_time_ms: 2100,
      is_stream: false,
      stream_outcome: "not_streaming",
      stream_error_kind: null,
      output_tokens: 120,
      total_tokens: 120,
      total_cost_user_currency_micros: 500000,
    }),
    createRequestLogItem({
      id: 103,
      caller_client_display: "Interrupted Stream",
      upstream_client_display: "Interrupted Stream",
      ttft_ms: 80,
      completion_duration_ms: null,
      response_time_ms: 900,
      is_stream: false,
      stream_outcome: "client_disconnected",
      stream_error_kind: "client_write_failed",
      output_tokens: null,
      total_tokens: null,
      total_cost_user_currency_micros: null,
      priced_flag: false,
      unpriced_reason: "STREAM_USAGE_UNAVAILABLE",
    }),
    createRequestLogItem({
      id: 104,
      caller_client_display: "Legacy Buffered",
      upstream_client_display: "Legacy Buffered",
      ttft_ms: null,
      completion_duration_ms: null,
      response_time_ms: 240,
      is_stream: false,
      stream_outcome: "not_streaming",
      stream_error_kind: null,
      output_tokens: null,
      total_tokens: 90,
      total_cost_user_currency_micros: 250000,
    }),
    createRequestLogItem({
      id: 105,
      caller_client_display: "Historical Unknown",
      upstream_client_display: "Historical Unknown",
      ttft_ms: null,
      completion_duration_ms: null,
      response_time_ms: 1200,
      stream_outcome: "unknown",
      stream_error_kind: null,
      output_tokens: null,
      total_tokens: null,
      total_cost_user_currency_micros: null,
      priced_flag: false,
      unpriced_reason: "MISSING_TOKEN_USAGE",
    }),
  ];
  const detailById = new Map<number, unknown>([
    [103, createRequestLogDetail()],
    [105, createHistoricalUnknownRequestLogDetail()],
  ]);

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, searchParams } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({
        profiles: [profile],
        active_profile: profile,
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson({
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }


    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      throw new Error(
        "Unexpected /api/endpoints request during request-logs browse mode",
      );
    }

    if (pathname === "/api/stats/requests") {
      return fulfillJson(createRequestLogsResponse(requestLogItems, searchParams));
    }

    if (pathname.startsWith("/api/stats/requests/")) {
      const requestId = Number.parseInt(pathname.split("/").pop() ?? "", 10);
      const detail = detailById.get(requestId);
      return detail ? fulfillJson(detail) : fulfillJson({}, 404);
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("request logs TTFT", () => {
  test("table renders TTFT between latency and Output Rate with post-TTFT null handling", async ({ page }) => {
    await mockRequestLogRoutes(page);

    await page.goto("/observe/requests");

    const table = page.getByTestId("request-logs-table");
    const headerCells = table.locator(".sticky > div");

    await expect(headerCells.nth(2)).toHaveText("Latency");
    await expect(headerCells.nth(3)).toHaveText("TTFT");
    await expect(headerCells.nth(4)).toHaveText("Output Rate");

    const completedStreamRow = page.getByRole("button").filter({ hasText: "Completed Stream" });
    await expect(completedStreamRow.locator(":scope > div").nth(2)).toHaveText("4,500ms");
    await expect(completedStreamRow.locator(":scope > div").nth(3)).toHaveText("120ms");
    await expect(completedStreamRow.locator(":scope > div").nth(4)).toHaveText("833.3 tok/s");
    await expect(completedStreamRow.locator(":scope > div").nth(12)).toContainText("$0.75");
    await expect(completedStreamRow.locator(":scope > div").nth(13)).toContainText("Streaming");

    const bufferedCompletedRow = page.getByRole("button").filter({ hasText: "Buffered Completed" });
    await expect(bufferedCompletedRow.locator(":scope > div").nth(2)).toHaveText("2,100ms");
    await expect(bufferedCompletedRow.locator(":scope > div").nth(3)).toHaveText("—");
    await expect(bufferedCompletedRow.locator(":scope > div").nth(4)).toHaveText("—");

    const interruptedStreamRow = page.getByRole("button").filter({ hasText: "Interrupted Stream" });
    await expect(interruptedStreamRow.locator(":scope > div").nth(2)).toHaveText("900ms");
    await expect(interruptedStreamRow.locator(":scope > div").nth(3)).toHaveText("80ms");
    await expect(interruptedStreamRow.locator(":scope > div").nth(4)).toHaveText("—");
    await expect(interruptedStreamRow.locator(":scope > div").nth(11)).toHaveText("—");
    await expect(interruptedStreamRow.locator(":scope > div").nth(12)).toContainText("Usage unavailable");
    await expect(interruptedStreamRow.locator(":scope > div").nth(13)).toContainText(
      "Stream interrupted - client disconnected",
    );

    const legacyBufferedRow = page.getByRole("button").filter({ hasText: "Legacy Buffered" });
    await expect(legacyBufferedRow.locator(":scope > div").nth(2)).toHaveText("240ms");
    await expect(legacyBufferedRow.locator(":scope > div").nth(3)).toHaveText("—");
    await expect(legacyBufferedRow.locator(":scope > div").nth(4)).toHaveText("—");

    const historicalUnknownRow = page.getByRole("button").filter({ hasText: "Historical Unknown" });
    await expect(historicalUnknownRow.locator(":scope > div").nth(12)).toContainText("Unpriced");
    await expect(historicalUnknownRow.locator(":scope > div").nth(13)).toContainText("Historical stream state unknown");
  });

  test("detail renders TTFT summary strip in the committed six-stat order", async ({ page }) => {
    await mockRequestLogRoutes(page);

    await page.goto("/observe/requests");
    await page.getByRole("button").filter({ hasText: "Interrupted Stream" }).click();

    const drawer = page.getByTestId("request-log-detail-sheet");
    const summaryStrip = page.getByTestId("request-log-summary-strip");

    await expect(drawer).toBeVisible();
    await expect(summaryStrip).toBeVisible();
    await expect(summaryStrip).toHaveClass(/sm:grid-cols-3/);
    await expect(summaryStrip).toHaveClass(/xl:w-\[540px\]/);

    await expect(summaryStrip.locator("[data-slot='metric-label']")).toHaveText([
      "Latency",
      "TTFT",
      "Output Rate",
      "Total tokens",
      "Total cost",
      "Timestamp",
    ]);

    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(0)).toHaveText("900ms");
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(1)).toHaveText("80ms");
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(2)).toHaveText("—");
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(3)).toHaveText("Usage unavailable");
    const totalCostSummary = summaryStrip.locator("[data-slot='metric-value']").nth(4);
    await expect(totalCostSummary).toContainText("Usage unavailable");
    await expect(page.getByText("Stream interrupted - client disconnected").first()).toBeVisible();
    await expect(page.getByText("client closed response stream")).toBeVisible();
    await expect(page.getByText("Open Pricing Templates")).toHaveCount(0);
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(5)).not.toHaveText("");
  });

  test("historical unknown stream detail keeps missing-usage context without pricing-template guidance", async ({ page }) => {
    await mockRequestLogRoutes(page);

    await page.goto("/observe/requests");
    await page.getByRole("button").filter({ hasText: "Historical Unknown" }).click();

    const drawer = page.getByTestId("request-log-detail-sheet");
    const summaryStrip = page.getByTestId("request-log-summary-strip");

    await expect(drawer).toBeVisible();
    await expect(page.getByText("Historical stream state unknown").first()).toBeVisible();
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(3)).toHaveText("—");
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(4)).toContainText("Unpriced");
    await expect(page.getByText("Missing token usage")).toBeVisible();
    await expect(page.getByText("Open Pricing Templates")).toHaveCount(0);
  });
});
