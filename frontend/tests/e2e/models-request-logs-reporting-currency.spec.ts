import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";
const canonicalCurrency = {
  code: "EUR",
  symbol: "€",
};

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

function createRequestLogItems() {
  return [
    {
      id: 101,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      caller_client_display: "Prism QA Browser",
      upstream_client_display: "Prism QA Browser",
      user_agent_overridden: false,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      endpoint_id: 1,
      connection_id: null,
      status_code: 200,
      response_time_ms: 125,
      is_stream: false,
      total_tokens: 150,
      total_cost_user_currency_micros: 750000,
      report_currency_symbol: "$",
    },
    {
      id: 102,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      caller_client_display: "Prism QA Browser",
      upstream_client_display: "Prism QA Browser",
      user_agent_overridden: false,
      api_family: "openai",
      vendor_id: 1,
      vendor_key: "openai",
      vendor_name: "OpenAI",
      endpoint_id: 1,
      connection_id: null,
      status_code: 200,
      response_time_ms: 240,
      is_stream: false,
      total_tokens: 90,
      total_cost_user_currency_micros: 500000,
      report_currency_symbol: null,
    },
  ];
}

async function mockCurrencyRoutes(page: Page) {
  const profile = createProfile();
  const model = createModelListItem();
  const requestLogItems = createRequestLogItems();

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
      const limit = Number.parseInt(searchParams.get("limit") ?? String(requestLogItems.length), 10);
      const offset = Number.parseInt(searchParams.get("offset") ?? "0", 10);

      return fulfillJson({
        items: requestLogItems,
        total: requestLogItems.length,
        limit,
        offset,
      });
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

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("models and request logs reporting currency", () => {
  test("uses the canonical reporting currency for models 30d totals", async ({ page }) => {
    await mockCurrencyRoutes(page);

    await page.goto("/models");

    await expect(page.getByText("€1.25 EUR spend")).toBeVisible();
    await expect(page.getByText("$1.25")).toHaveCount(0);
  });

  test("keeps payload symbols explicit in request logs and falls back missing symbols to the canonical reporting currency", async ({ page }) => {
    await mockCurrencyRoutes(page);

    await page.goto("/request-logs");

    const table = page.getByTestId("request-logs-table");
    await expect(table).toContainText("$0.75");
    await expect(table).toContainText("€0.50 EUR");
    await expect(table).not.toContainText("$0.50");
  });

  test("uses the canonical fallback in dashboard recent activity when request payload symbols are missing", async ({ page }) => {
    await mockCurrencyRoutes(page);

    await page.goto("/dashboard?tab=overview");

    await expect(page.getByText("$0.75")).toBeVisible();
    await expect(page.getByText("€0.50 EUR")).toBeVisible();
    await expect(page.getByText("$0.50")).toHaveCount(0);
  });
});
