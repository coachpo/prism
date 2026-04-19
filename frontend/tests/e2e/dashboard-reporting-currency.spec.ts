import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";

function createProfile(id: number, name: string, isActive = false) {
  return {
    id,
    name,
    description: null,
    is_active: isActive,
    is_default: id === 1,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  };
}

function createModelListItem(profileId: number) {
  return {
    id: profileId,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: `GPT-4o mini P${profileId}`,
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

function createDashboardUsageSnapshot(profileId: number) {
  return {
    generated_at: timestamp,
    time_range: {
      preset: "24h",
      start_at: "2026-04-10T00:00:00Z",
      end_at: timestamp,
    },
    currency: {
      code: profileId === 2 ? "USD" : "CNY",
      symbol: profileId === 2 ? "$" : "¥",
    },
    overview: {
      total_requests: 20,
      success_requests: 19,
      failed_requests: 1,
      success_rate: 95,
      total_tokens: 1650,
      input_tokens: 900,
      output_tokens: 600,
      cached_tokens: 100,
      reasoning_tokens: 50,
      average_rpm: 0.5,
      average_tpm: 68.8,
      total_cost_micros: 250000,
    },
    service_health: {
      availability_percentage: 95,
      request_count: 20,
      success_count: 19,
      failed_count: 1,
      interval_minutes: 60,
      cells: [],
    },
    request_trends: {
      hourly: [
        {
          key: "all",
          label: "All requests",
          total_requests: 20,
          points: [],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All requests",
          total_requests: 20,
          points: [],
        },
      ],
    },
    token_usage_trends: {
      hourly: [],
      daily: [],
    },
    token_type_breakdown: {
      hourly: [],
      daily: [],
    },
    cost_overview: {
      total_cost_micros: 250000,
      priced_request_count: 9,
      unpriced_request_count: 2,
      hourly: [],
      daily: [],
    },
    endpoint_statistics: [],
    model_statistics: [],
    proxy_api_key_statistics: [],
  };
}

function createStatsSummary(totalRequests: number) {
  return {
    total_requests: totalRequests,
    success_requests: totalRequests - 1,
    failed_requests: 1,
    success_rate: 95,
    total_tokens: 1650,
    input_tokens: 900,
    output_tokens: 600,
    cached_tokens: 100,
    reasoning_tokens: 50,
    average_rpm: 0.5,
    average_tpm: 68.8,
  };
}

function createCostingSettings(profileId: number) {
  return {
    report_currency_code: profileId === 2 ? "USD" : "CNY",
    report_currency_symbol: profileId === 2 ? "$" : "¥",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

function createRequestLogsResponse(searchParams: URLSearchParams) {
  const requestLogItems = [
    {
      id: 1,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      caller_client_display: null,
      upstream_client_display: null,
      user_agent_overridden: false,
      api_family: "openai",
      vendor_id: null,
      vendor_key: null,
      vendor_name: null,
      endpoint_id: null,
      endpoint_label: "Unknown Endpoint",
      connection_id: null,
      ttft_ms: null,
      completion_duration_ms: null,
      status_code: 200,
      response_time_ms: 123,
      is_stream: false,
      output_tokens: 10,
      total_tokens: 10,
      total_cost_user_currency_micros: 1000,
      report_currency_symbol: "¥",
    },
  ];
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
      endpoints: [],
    },
  };
}

async function mockDashboardRoutes(page: Page) {
  const profiles = [
    createProfile(1, "Red Team", true),
    createProfile(2, "Blue Team"),
  ];

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
        profiles,
        active_profile: profiles[0],
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/costing") {
      const profileId = Number(request.headers()["x-profile-id"] ?? "1");
      return fulfillJson(createCostingSettings(profileId));
    }

    if (pathname === "/api/stats/summary") {
      return fulfillJson(createStatsSummary(20));
    }

    if (pathname === "/api/models") {
      const profileId = Number(request.headers()["x-profile-id"] ?? "1");
      return fulfillJson([createModelListItem(profileId)]);
    }

    if (pathname === "/api/stats/requests") {
      return fulfillJson(createRequestLogsResponse(searchParams));
    }

    if (pathname === "/api/stats/usage-snapshot") {
      const profileId = Number(request.headers()["x-profile-id"] ?? "1");
      return fulfillJson(createDashboardUsageSnapshot(profileId));
    }

    if (pathname === "/api/stats/spending") {
      const profileId = Number(request.headers()["x-profile-id"] ?? "1");
      return fulfillJson({
        summary: {
          total_cost_micros: 250000,
          successful_request_count: 11,
          priced_request_count: 9,
          unpriced_request_count: 2,
        },
        top_spending_models: [
          {
            model_id: `gpt-4o-mini-p${profileId}`,
            total_cost_micros: 250000,
          },
        ],
      });
    }

    if (pathname === "/api/stats/throughput") {
      return fulfillJson({ average_rpm: 0.5, total_requests: 20 });
    }

    if (pathname === "/api/stats/api-family") {
      return fulfillJson({ groups: [] });
    }

    if (pathname === "/api/stats/connection-success-rates") {
      return fulfillJson([]);
    }

    if (pathname === "/api/connections/by-models") {
      return fulfillJson([]);
    }

    if (pathname === "/api/routing-diagram") {
      return fulfillJson({ nodes: [], links: [] });
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("dashboard reporting currency", () => {
  test("renders dashboard totals with the active reporting currency", async ({ page }) => {
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=overview");

    const metricValues = page.locator('[data-slot="metric-card"] [data-slot="metric-value"]');
    const spendingMetric = metricValues.nth(2);
    const spendingCard = page.locator('[data-slot="metric-card"]').filter({ hasText: "30d Total Spend" }).first();

    await expect(spendingMetric).toHaveText("¥0.25 CNY");
    await expect(spendingCard).toContainText("Request-based spend");
    await expect(spendingCard).toContainText("9 priced");
    await expect(spendingCard).toContainText("2 unpriced");
    await expect(page.getByText("Top Models by Spend")).toBeVisible();
    await expect(page.getByText("Highest request-based spend in the last 30 days")).toBeVisible();
    await expect(page.getByText("¥0.25 CNY")).toHaveCount(2);

    await page.getByTestId("shell-profile-switcher").getByRole("button").click();
    await page.getByRole("menuitem", { name: /Blue Team/ }).click();

    await expect(spendingMetric).toHaveText("$0.25 USD");
    await expect(page.getByText("$0.25 USD")).toHaveCount(2);
  });
});
