import { expect, test, type Page } from "@playwright/test";
import type { DashboardRecentActivityItem } from "../../src/lib/types";
import {
  createDashboardRecentActivityResponse,
  createDashboardSnapshot,
} from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-11T00:00:00Z";
const canonicalCurrency = {
  code: "EUR",
  symbol: "€",
};

function createModelListItem() {
  return {
    id: 1,
    profile_id: 1,
    api_family: "openai" as const,
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini",
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createStatsSummary() {
  return {
    total_requests: 12,
    success_count: 11,
    error_count: 1,
    success_rate: 91.7,
    avg_response_time_ms: 250,
    p95_response_time_ms: 420,
    total_input_tokens: 1200,
    total_output_tokens: 600,
    total_tokens: 1800,
    groups: [],
  };
}

function createSpendingReport() {
  return {
    summary: {
      total_cost_micros: 1250000,
      successful_request_count: 2,
      priced_request_count: 2,
      unpriced_request_count: 0,
      total_input_tokens: 1200,
      total_output_tokens: 600,
      total_cache_read_input_tokens: 0,
      total_cache_creation_input_tokens: 0,
      total_reasoning_tokens: 0,
      total_tokens: 1800,
      avg_cost_per_successful_request_micros: 625000,
    },
    groups: [],
    groups_total: 0,
    top_spending_models: [
      {
        model_id: "gpt-4o-mini",
        model_label: "GPT 4o Mini",
        total_cost_micros: 1250000,
      },
    ],
    top_spending_endpoints: [],
    unpriced_breakdown: {},
    report_currency_code: canonicalCurrency.code,
    report_currency_symbol: canonicalCurrency.symbol,
  };
}

function createThroughputStats() {
  return {
    average_rpm: 1.5,
    peak_rpm: 2.5,
    current_rpm: 1,
    total_requests: 12,
    time_window_seconds: 86400,
    buckets: [],
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
    caller_client_display: "Payload symbol row",
    upstream_client_display: "Payload symbol row",
    user_agent_overridden: false,
    api_family: "openai" as const,
    endpoint_id: 1,
    endpoint_label: "Primary endpoint",
    connection_id: null,
    status_code: 200,
    ttft_ms: 95,
    completion_duration_ms: 500,
    response_time_ms: 125,
    is_stream: false,
    stream_outcome: "not_streaming" as const,
    stream_error_kind: null,
    output_tokens: 80,
    total_tokens: 150,
    total_cost_user_currency_micros: 750000,
    priced_flag: true,
    unpriced_reason: null,
    reasoning_effort: "low",
    report_currency_symbol: "$",
    ...overrides,
  };
}

function createRequestLogDetail({
  requestId,
  clientDisplay,
  totalCostMicros,
  reportCurrencySymbol,
  pricedFlag,
  unpricedReason,
}: {
  requestId: number;
  clientDisplay: string;
  totalCostMicros: number | null;
  reportCurrencySymbol: string | null;
  pricedFlag: boolean;
  unpricedReason: string | null;
}) {
  const hasComputedCost = totalCostMicros !== null;

  return {
    summary: {
      id: requestId,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      model_label: "GPT-4o mini",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      is_proxy_origin: false,
      api_family: "openai" as const,
      status_code: 200,
      response_time_ms: hasComputedCost ? 180 : 240,
      ttft_ms: hasComputedCost ? 60 : null,
      completion_duration_ms: hasComputedCost ? 180 : null,
      is_stream: false,
    },
    request: {
      request_path: "/v1/chat/completions",
      ingress_request_id: `ingress-${requestId}`,
      attempt_number: 1,
      provider_correlation_id: `provider-corr-${requestId}`,
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: clientDisplay,
      upstream_user_agent: clientDisplay,
      caller_client_display: clientDisplay,
      upstream_client_display: clientDisplay,
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
      input_tokens: hasComputedCost ? 40 : 30,
      output_tokens: hasComputedCost ? 20 : 10,
      total_tokens: hasComputedCost ? 60 : 40,
      success_flag: true,
      billable_flag: true,
      priced_flag: pricedFlag,
      unpriced_reason: unpricedReason,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: hasComputedCost ? 0 : null,
      output_cost_micros: hasComputedCost ? 0 : null,
      cache_read_input_cost_micros: hasComputedCost ? 0 : null,
      cache_creation_input_cost_micros: hasComputedCost ? 0 : null,
      reasoning_cost_micros: hasComputedCost ? 0 : null,
      total_cost_original_micros: totalCostMicros,
      total_cost_user_currency_micros: totalCostMicros,
      currency_code_original: hasComputedCost ? canonicalCurrency.code : null,
      report_currency_code: canonicalCurrency.code,
      report_currency_symbol: reportCurrencySymbol,
      fx_rate_used: hasComputedCost ? "1" : null,
      fx_rate_source: hasComputedCost ? "manual" : null,
    },
    pricing: {
      pricing_snapshot_unit: hasComputedCost ? "1M tokens" : null,
      pricing_snapshot_input: hasComputedCost ? "0.10" : null,
      pricing_snapshot_output: hasComputedCost ? "0.20" : null,
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: hasComputedCost ? 1 : null,
    },
  };
}

function createRequestLogItems() {
  return [
    createRequestLogItem(),
    createRequestLogItem({
      id: 102,
      caller_client_display: "Canonical fallback row",
      upstream_client_display: "Canonical fallback row",
      ttft_ms: null,
      completion_duration_ms: null,
      response_time_ms: 240,
      output_tokens: 50,
      total_tokens: 90,
      total_cost_user_currency_micros: 500000,
      reasoning_effort: null,
      report_currency_symbol: null,
    }),
    createRequestLogItem({
      id: 103,
      caller_client_display: "Zero spend row",
      upstream_client_display: "Zero spend row",
      response_time_ms: 180,
      output_tokens: 0,
      total_tokens: 40,
      total_cost_user_currency_micros: 0,
      report_currency_symbol: "$",
    }),
    createRequestLogItem({
      id: 104,
      caller_client_display: "Unpriced row",
      upstream_client_display: "Unpriced row",
      ttft_ms: null,
      completion_duration_ms: null,
      response_time_ms: 300,
      output_tokens: 25,
      total_tokens: 70,
      total_cost_user_currency_micros: null,
      priced_flag: false,
      unpriced_reason: "MISSING_PRICE_DATA",
      report_currency_symbol: "$",
    }),
  ];
}

type RequestLogFixtureItem = ReturnType<typeof createRequestLogItem>;

function createDashboardRecentActivityItemFromRequest(
  item: RequestLogFixtureItem,
): DashboardRecentActivityItem {
  return {
    request_log_id: item.id,
    created_at: item.created_at,
    model_id: item.model_id,
    model_label: item.model_label,
    resolved_target_model_id: item.resolved_target_model_id,
    resolved_target_model_label: item.resolved_target_model_label,
    endpoint_id: item.endpoint_id,
    endpoint_label: item.endpoint_label,
    status_code: item.status_code,
    response_time_ms: item.response_time_ms,
    ttft_ms: item.ttft_ms,
    completion_duration_ms: item.completion_duration_ms,
    is_stream: item.is_stream,
    stream_outcome: item.stream_outcome,
    total_tokens: item.total_tokens,
    total_cost_user_currency_micros: item.total_cost_user_currency_micros,
    priced_flag: item.priced_flag,
    unpriced_reason: item.unpriced_reason,
    report_currency_symbol: item.report_currency_symbol,
  };
}

function createDashboardRecentActivityItems(
  requestLogItems: RequestLogFixtureItem[],
): DashboardRecentActivityItem[] {
  return requestLogItems.map(createDashboardRecentActivityItemFromRequest);
}

function createRequestLogsResponse(
  requestLogItems: ReturnType<typeof createRequestLogItems>,
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

async function mockCurrencyRoutes(
  page: Page,
  options: {
    costingSettingsFailure?: boolean;
  } = {},
) {
  const model = createModelListItem();
  const requestLogItems = createRequestLogItems();
  const requestLogDetails = new Map<number, unknown>([
    [
      103,
      createRequestLogDetail({
        requestId: 103,
        clientDisplay: "Zero spend row",
        totalCostMicros: 0,
        reportCurrencySymbol: "$",
        pricedFlag: true,
        unpricedReason: null,
      }),
    ],
    [
      104,
      createRequestLogDetail({
        requestId: 104,
        clientDisplay: "Unpriced row",
        totalCostMicros: null,
        reportCurrencySymbol: "$",
        pricedFlag: false,
        unpricedReason: "MISSING_PRICE_DATA",
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
      if (options.costingSettingsFailure) {
        return fulfillJson({ detail: "costing unavailable" }, 500);
      }

      return fulfillJson({
        report_currency_code: canonicalCurrency.code,
        report_currency_symbol: canonicalCurrency.symbol,
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/models") {
      if (page.url().includes("/observe/requests")) {
        throw new Error(
          "Unexpected /api/models request during request-logs browse mode",
        );
      }
      return fulfillJson([model]);
    }


    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/connections") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      throw new Error(
        "Unexpected /api/endpoints request during request-logs browse mode",
      );
    }

    if (pathname === "/api/stats/models/metrics" && request.method() === "POST") {
      return fulfillJson({
        items: [
          {
            model_id: model.model_id,
            success_rate: 99.2,
            request_count_24h: 12,
            p95_latency_ms: 420,
            spend_30d_micros: 1250000,
          },
        ],
      });
    }

    if (pathname === "/api/stats/requests") {
      return fulfillJson(createRequestLogsResponse(requestLogItems, searchParams));
    }

    if (pathname === "/api/stats/dashboard") {
      return fulfillJson(createDashboardSnapshot({
        metricSnapshot: {
          total_cost: 1250000,
          total_requests: 12,
        },
        topSpendingModels: [
          {
            model_id: model.model_id,
            model_label: model.display_name,
            total_cost_micros: 1250000,
          },
        ],
      }));
    }

    if (pathname === "/api/stats/dashboard/recent-activity") {
      return fulfillJson(
        createDashboardRecentActivityResponse(
          createDashboardRecentActivityItems(requestLogItems),
        ),
      );
    }

    if (pathname.startsWith("/api/stats/requests/")) {
      const requestId = Number.parseInt(pathname.split("/").pop() ?? "", 10);
      const detail = requestLogDetails.get(requestId);
      return detail ? fulfillJson(detail) : fulfillJson({}, 404);
    }

    if (pathname === "/api/stats/summary") {
      return fulfillJson(createStatsSummary());
    }

    if (pathname === "/api/stats/spending") {
      return fulfillJson(createSpendingReport());
    }

    if (pathname === "/api/stats/throughput") {
      return fulfillJson(createThroughputStats());
    }

    if (pathname === "/api/stats/connection-success-rates") {
      return fulfillJson([]);
    }

    if (pathname === "/api/models/connections/batch" && request.method() === "POST") {
      return fulfillJson({
        items: [
          {
            model_config_id: model.id,
            connections: [],
          },
        ],
      });
    }

    return fulfillJson({}, 404);
  });
}

test.describe("models and request logs reporting currency", () => {
  test("uses the canonical reporting currency for models 30d totals", async ({ page }) => {
    await mockCurrencyRoutes(page);

    await page.goto("/models");

    await expect(page.getByRole("row", { name: /GPT-4o mini/ }).getByText("€1.25")).toBeVisible();
    await expect(page.getByText("$1.25")).toHaveCount(0);
  });

  test("uses fallback currency when reporting currency settings cannot be verified", async ({ page }) => {
    await mockCurrencyRoutes(page, { costingSettingsFailure: true });

    await page.goto("/models");

    await expect(page.getByRole("row", { name: /GPT-4o mini/ }).getByText("$1.25")).toBeVisible();
  });

  test("keeps browse-mode request-log spend distinct for payload symbols, canonical fallback, priced zero, and missing cost", async ({ page }) => {
    await mockCurrencyRoutes(page);

    await page.goto("/observe/requests");

    const payloadSymbolReasoning = page
      .getByRole("button")
      .filter({ hasText: "Payload symbol row" })
      .locator(":scope > div")
      .nth(9);
    const canonicalFallbackReasoning = page
      .getByRole("button")
      .filter({ hasText: "Canonical fallback row" })
      .locator(":scope > div")
      .nth(9);
    const payloadSymbolSpend = page
      .getByRole("button")
      .filter({ hasText: "Payload symbol row" })
      .locator(":scope > div")
      .nth(12);
    const canonicalFallbackSpend = page
      .getByRole("button")
      .filter({ hasText: "Canonical fallback row" })
      .locator(":scope > div")
      .nth(12);
    const zeroSpend = page
      .getByRole("button")
      .filter({ hasText: "Zero spend row" })
      .locator(":scope > div")
      .nth(12);
    const unpricedSpend = page
      .getByRole("button")
      .filter({ hasText: "Unpriced row" })
      .locator(":scope > div")
      .nth(12);

    await expect(page.getByText("Reasoning effort")).toBeVisible();
    await expect(payloadSymbolReasoning).toContainText("low");
    await expect(canonicalFallbackReasoning).toContainText("—");
    await expect(payloadSymbolSpend).toContainText("$0.75");
    await expect(canonicalFallbackSpend).toContainText("€0.50 EUR");
    await expect(canonicalFallbackSpend).not.toContainText("$0.50");
    await expect(zeroSpend).toContainText("$0.00");
    await expect(unpricedSpend).toContainText("Unpriced");
  });

  test("keeps detail-mode request-log spend distinct for priced zero and missing cost", async ({ page }) => {
    await mockCurrencyRoutes(page);

    await page.goto("/observe/requests?request_id=103");

    const pricedZeroSummary = page.getByTestId("request-log-summary-strip").locator("[data-slot='metric-value']").nth(4);
    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
    await expect(pricedZeroSummary).toContainText("$0.00");

    await page.goto("/observe/requests?request_id=104");

    const unpricedSummary = page.getByTestId("request-log-summary-strip").locator("[data-slot='metric-value']").nth(4);
    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
    await expect(unpricedSummary).toContainText("Unpriced");
    await expect(unpricedSummary).not.toContainText("$0.00");
    await expect(page.getByRole("link", { name: "Open Pricing Templates" })).toBeVisible();
  });

  test("uses the canonical fallback in dashboard recent activity when request payload symbols are missing", async ({ page }) => {
    await mockCurrencyRoutes(page);

    await page.goto("/observe?tab=overview");

    const recentActivity = page.locator('[data-slot="card"]').filter({ hasText: "Recent Activity" }).first();

    const recentRows = recentActivity.getByRole("button");

    await expect(recentRows.nth(0)).toContainText("$0.75");
    await expect(recentRows.nth(1)).toContainText("€0.50 EUR");
    await expect(recentRows.nth(3)).toContainText("Unpriced");
    await expect(recentActivity.getByText("$0.50")).toHaveCount(0);
  });
});
