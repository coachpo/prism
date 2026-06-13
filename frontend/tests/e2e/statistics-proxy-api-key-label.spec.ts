import { expect, test } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";

function createUsageSnapshot() {
  return {
    generated_at: timestamp,
    time_range: {
      preset: "24h",
      start_at: "2026-04-09T00:00:00Z",
      end_at: timestamp,
    },
    currency: { code: "USD", symbol: "$" },
    overview: {
      total_requests: 11,
      success_requests: 10,
      failed_requests: 1,
      success_rate: 90.9,
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
      availability_percentage: 90.9,
      request_count: 11,
      success_count: 10,
      failed_count: 1,
      interval_minutes: 60,
      cells: [],
    },
    request_trends: {
      hourly: [
        {
          key: "all",
          label: "All requests",
          total_requests: 11,
          points: [],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All requests",
          total_requests: 11,
          points: [],
        },
      ],
    },
    token_usage_trends: {
      hourly: [
        {
          key: "all",
          label: "All models",
          total_tokens: 1650,
          points: [],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All models",
          total_tokens: 1650,
          points: [],
        },
      ],
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
    endpoint_statistics: [
      {
        endpoint_id: 7,
        endpoint_label: "Proxy stats control endpoint",
        p50_ttft_ms: 120,
        p95_ttft_ms: 340,
        request_count: 4,
        success_rate: 100,
        total_tokens: 600,
        avg_output_rate_tps: 88.2,
        total_cost_micros: 100000,
      },
    ],
    model_statistics: [
      {
        model_id: "proxy-stats-control-model",
        model_label: "Proxy stats control model",
        p50_ttft_ms: 95,
        p95_ttft_ms: 210,
        request_count: 4,
        success_rate: 100,
        total_tokens: 600,
        avg_output_rate_tps: 88.2,
        total_cost_micros: 100000,
      },
    ],
    proxy_api_key_statistics: [
      {
        proxy_api_key_id: 1,
        proxy_api_key_label: "No proxy API key",
        request_count: 8,
        success_rate: 100,
        total_tokens: 1200,
        total_cost_micros: 0,
      },
      {
        proxy_api_key_id: 2,
        proxy_api_key_label: "Team A key",
        request_count: 3,
        success_rate: 66.7,
        total_tokens: 450,
        total_cost_micros: 250000,
      },
    ],
  };
}

async function mockStatisticsRoutes(page: Parameters<typeof test>[0]["page"]) {
  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

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
        profiles: [
          {
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
          },
        ],
        active_profile: {
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
        },
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
      return fulfillJson([]);
    }


    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([]);
    }

    if (pathname === "/api/stats/usage-snapshot") {
      return fulfillJson(createUsageSnapshot());
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

test.describe("statistics proxy API key label regression", () => {
  test("renders English proxy API key labels", async ({ page }) => {
    await mockStatisticsRoutes(page);
    await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

    await page.goto("/observe?tab=analytics");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText(/Dashboard|仪表盘/);
    const table = page.getByTestId("statistics-proxy-key-table");
    await expect(table).toBeVisible({ timeout: 15000 });
    await expect(table).toContainText("No proxy API key");
    await expect(table).toContainText("Team A key");
  });

  test("renders Chinese fallback label while preserving real key labels", async ({ page }) => {
    await mockStatisticsRoutes(page);
    await page.addInitScript(() => localStorage.setItem("prism.locale", "zh-CN"));

    await page.goto("/observe?tab=analytics");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText(/Dashboard|仪表盘/);
    const table = page.getByTestId("statistics-proxy-key-table");
    await expect(table).toBeVisible({ timeout: 15000 });
    await expect(table).toContainText("无代理 API 密钥");    await expect(table).toContainText("Team A key");
  });
});
