import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-05-21T00:00:00Z";

function createRequestLogItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 201,
    created_at: timestamp,
    model_id: "gpt-4o-mini",
    model_label: "GPT-4o mini",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Optional zero row",
    upstream_client_display: "Optional zero row",
    user_agent_overridden: false,
    api_family: "openai",
    endpoint_id: 1,
    endpoint_label: "Primary endpoint",
    connection_id: null,
    ttft_ms: 40,
    completion_duration_ms: 120,
    status_code: 200,
    response_time_ms: 120,
    is_stream: false,
    stream_outcome: "not_streaming",
    stream_error_kind: null,
    reasoning_effort: null,
    output_tokens: 0,
    total_tokens: 60,
    total_cost_user_currency_micros: 0,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
    ...overrides,
  };
}

function createRequestLogDetail(overrides: Record<string, unknown> = {}) {
  return {
    summary: {
      id: 201,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      model_label: "GPT-4o mini",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      is_proxy_origin: false,
      api_family: "openai",
      status_code: 200,
      response_time_ms: 120,
      ttft_ms: 40,
      completion_duration_ms: 120,
      is_stream: false,
      stream_outcome: "not_streaming",
      stream_error_kind: null,
      stream_error_detail: null,
    },
    request: {
      request_path: "/v1/chat/completions",
      ingress_request_id: "ingress-201",
      attempt_number: 1,
      provider_correlation_id: "provider-corr-201",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Prism QA Browser",
      upstream_user_agent: "Prism QA Browser",
      caller_client_display: "Optional zero row",
      upstream_client_display: "Optional zero row",
      user_agent_overridden: false,
      request_generation_params: null,
      request_generation_params_status: null,
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
      audit_capture_bodies_at_request: true,
    },
    usage: {
      input_tokens: 40,
      output_tokens: 0,
      total_tokens: 45,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 5,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 0,
      output_cost_micros: 0,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 0,
      total_cost_user_currency_micros: 0,
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
      pricing_snapshot_cache_read_input: "0",
      pricing_snapshot_cache_creation_input: "0",
      pricing_snapshot_reasoning: "0",
      pricing_config_version_used: 7,
    },
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

async function mockRequestLogRoutes(page: Page) {
  const requestLogItems = [
    createRequestLogItem(),
    createRequestLogItem({
      id: 202,
      caller_client_display: "Historical missing price data",
      upstream_client_display: "Historical missing price data",
      total_cost_user_currency_micros: null,
      priced_flag: false,
      unpriced_reason: "MISSING_PRICE_DATA",
    }),
  ];
  const detailById = new Map<number, unknown>([
    [201, createRequestLogDetail()],
    [
      202,
      createRequestLogDetail({
        summary: { ...createRequestLogDetail().summary, id: 202 },
        request: {
          ...createRequestLogDetail().request,
          ingress_request_id: "ingress-202",
          provider_correlation_id: "provider-corr-202",
          caller_client_display: "Historical missing price data",
          upstream_client_display: "Historical missing price data",
        },
        usage: {
          ...createRequestLogDetail().usage,
          priced_flag: false,
          unpriced_reason: "MISSING_PRICE_DATA",
          cache_read_input_tokens: null,
          cache_creation_input_tokens: null,
          reasoning_tokens: null,
        },
        costing: {
          ...createRequestLogDetail().costing,
          total_cost_original_micros: null,
          total_cost_user_currency_micros: null,
        },
        pricing: {
          pricing_snapshot_unit: null,
          pricing_snapshot_input: null,
          pricing_snapshot_output: null,
          pricing_snapshot_cache_read_input: null,
          pricing_snapshot_cache_creation_input: null,
          pricing_snapshot_reasoning: null,
          pricing_config_version_used: null,
        },
      }),
    ],
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

    if (pathname === "/api/models" || pathname === "/api/endpoints") {
      throw new Error(`Unexpected ${pathname} request during request-logs browse mode`);
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
}

function overviewValue(page: Page, label: string) {
  return page
    .getByTestId("request-log-overview-grid")
    .getByText(label, { exact: true })
    .locator("xpath=following-sibling::div[1]");
}

function pricingSnapshotValue(page: Page, label: string) {
  return overviewValue(page, label);
}

test.describe("request logs optional zero pricing", () => {
  test("keeps priced-zero rows priced while historical missing-price rows remain unpriced", async ({ page }) => {
    await mockRequestLogRoutes(page);

    await page.goto("/observe/requests");

    const pricedZeroSpend = page
      .getByRole("button")
      .filter({ hasText: "Optional zero row" })
      .locator(":scope > div")
      .nth(12);
    const historicalUnpricedSpend = page
      .getByRole("button")
      .filter({ hasText: "Historical missing price data" })
      .locator(":scope > div")
      .nth(12);

    await expect(pricedZeroSpend).toContainText("$0.00");
    await expect(pricedZeroSpend).not.toContainText("Unpriced");
    await expect(historicalUnpricedSpend).toContainText("Unpriced");
    await expect(historicalUnpricedSpend).not.toContainText("$0.00");
  });

  test("renders effective-zero pricing snapshots and split token zero-vs-unknown detail", async ({ page }) => {
    await mockRequestLogRoutes(page);

    await page.goto("/observe/requests?request_id=201");

    const summaryStrip = page.getByTestId("request-log-summary-strip");
    const totalCostSummary = summaryStrip.locator("[data-slot='metric-value']").nth(4);

    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
    await expect(totalCostSummary).toContainText("$0.00");
    await expect(totalCostSummary).not.toContainText("Unpriced");
    await expect(overviewValue(page, "Cache read")).toHaveText("0");
    await expect(overviewValue(page, "Cache creation")).toHaveText("5");
    await expect(overviewValue(page, "Reasoning")).toHaveText("0");
    await expect(pricingSnapshotValue(page, "Pricing snapshot cache read")).toHaveText("0");
    await expect(pricingSnapshotValue(page, "Pricing snapshot cache creation")).toHaveText("0");
    await expect(pricingSnapshotValue(page, "Pricing snapshot reasoning")).toHaveText("0");
    await expect(page.getByText("0 (default)")).toHaveCount(0);

    await page.goto("/observe/requests?request_id=202");
    await expect(overviewValue(page, "Cache read")).toHaveText("—");
    await expect(overviewValue(page, "Cache creation")).toHaveText("—");
    await expect(overviewValue(page, "Reasoning")).toHaveText("—");
  });
});
