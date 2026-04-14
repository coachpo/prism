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

function createModelListItem() {
  return {
    id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createRequestLogItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 101,
    created_at: timestamp,
    model_id: "gpt-4o-mini",
    resolved_target_model_id: null,
    caller_client_display: "Completed Stream",
    upstream_client_display: "Completed Stream",
    user_agent_overridden: false,
    api_family: "openai",
    vendor_id: 1,
    vendor_key: "openai",
    vendor_name: "OpenAI",
    endpoint_id: 1,
    connection_id: null,
    ttft_ms: 120,
    completion_duration_ms: 300,
    status_code: 200,
    response_time_ms: 4500,
    is_stream: true,
    output_tokens: 150,
    total_tokens: 150,
    total_cost_user_currency_micros: 750000,
    report_currency_symbol: "$",
    ...overrides,
  };
}

function createRequestLogDetail(overrides: Record<string, unknown> = {}) {
  return {
    summary: {
      id: 103,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      status_code: 200,
      response_time_ms: 900,
      ttft_ms: 80,
      completion_duration_ms: null,
      is_stream: true,
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
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      endpoint_id: 1,
      connection_id: null,
      endpoint_base_url: "https://api.example.test",
      endpoint_description: "Primary endpoint",
    },
    usage: {
      input_tokens: 20,
      output_tokens: 25,
      total_tokens: 45,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 200000,
      output_cost_micros: 300000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 500000,
      total_cost_user_currency_micros: 500000,
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

async function mockRequestLogRoutes(page: Page) {
  const profile = createProfile();
  const model = createModelListItem();
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
      output_tokens: 25,
      total_tokens: 45,
      total_cost_user_currency_micros: 500000,
    }),
    createRequestLogItem({
      id: 104,
      caller_client_display: "Legacy Buffered",
      upstream_client_display: "Legacy Buffered",
      ttft_ms: null,
      completion_duration_ms: null,
      response_time_ms: 240,
      is_stream: false,
      output_tokens: null,
      total_tokens: 90,
      total_cost_user_currency_micros: 250000,
    }),
  ];
  const detailById = new Map<number, unknown>([
    [103, createRequestLogDetail()],
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

    if (pathname === "/api/models") {
      return fulfillJson([model]);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([
        {
          id: 1,
          name: "Primary endpoint",
          base_url: "https://api.example.test",
          provider: "openai",
          created_at: timestamp,
          updated_at: timestamp,
        },
      ]);
    }

    if (pathname === "/api/stats/requests") {
      const limit = Number.parseInt(searchParams.get("limit") ?? String(requestLogItems.length), 10);
      const offset = Number.parseInt(searchParams.get("offset") ?? "0", 10);

      return fulfillJson({
        items: requestLogItems,
        total: requestLogItems.length,
        limit,
        offset,
      });
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

    await page.goto("/request-logs");

    const table = page.getByTestId("request-logs-table");
    const headerCells = table.locator(".sticky > div");

    await expect(headerCells.nth(2)).toHaveText("Latency");
    await expect(headerCells.nth(3)).toHaveText("TTFT");
    await expect(headerCells.nth(4)).toHaveText("Output Rate");

    const completedStreamRow = page.getByRole("button").filter({ hasText: "Completed Stream" });
    await expect(completedStreamRow.locator(":scope > div").nth(2)).toHaveText("4,500ms");
    await expect(completedStreamRow.locator(":scope > div").nth(3)).toHaveText("120ms");
    await expect(completedStreamRow.locator(":scope > div").nth(4)).toHaveText("833.3 tok/s");

    const bufferedCompletedRow = page.getByRole("button").filter({ hasText: "Buffered Completed" });
    await expect(bufferedCompletedRow.locator(":scope > div").nth(2)).toHaveText("2,100ms");
    await expect(bufferedCompletedRow.locator(":scope > div").nth(3)).toHaveText("—");
    await expect(bufferedCompletedRow.locator(":scope > div").nth(4)).toHaveText("—");

    const interruptedStreamRow = page.getByRole("button").filter({ hasText: "Interrupted Stream" });
    await expect(interruptedStreamRow.locator(":scope > div").nth(2)).toHaveText("900ms");
    await expect(interruptedStreamRow.locator(":scope > div").nth(3)).toHaveText("80ms");
    await expect(interruptedStreamRow.locator(":scope > div").nth(4)).toHaveText("—");

    const legacyBufferedRow = page.getByRole("button").filter({ hasText: "Legacy Buffered" });
    await expect(legacyBufferedRow.locator(":scope > div").nth(2)).toHaveText("240ms");
    await expect(legacyBufferedRow.locator(":scope > div").nth(3)).toHaveText("—");
    await expect(legacyBufferedRow.locator(":scope > div").nth(4)).toHaveText("—");
  });

  test("detail renders TTFT summary strip in the committed six-stat order", async ({ page }) => {
    await mockRequestLogRoutes(page);

    await page.goto("/request-logs");
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
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(3)).toHaveText("45");
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(4)).toHaveText("$0.50");
    await expect(summaryStrip.locator("[data-slot='metric-value']").nth(5)).not.toHaveText("");
  });
});
