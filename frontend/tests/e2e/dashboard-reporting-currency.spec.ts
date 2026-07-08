import { expect, test, type Page } from "@playwright/test";
import {
  createDashboardRecentActivityResponse,
  createDashboardSnapshot,
} from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-11T00:00:00Z";
const reportingCurrencyExpectationTimeout = 15_000;

function createModelListItem() {
  return {
    id: 1,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini P1",
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

function createCostingSettings() {
  return {
    report_currency_code: "CNY",
    report_currency_symbol: "¥",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

async function mockDashboardRoutes(page: Page) {
  let lastProfileHeader: string | null = null;

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (pathname.startsWith("/api/")) {
      lastProfileHeader = request.headers()["x-profile-id"] ?? "1";
    }

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson(createCostingSettings());
    }

    if (pathname === "/api/stats/dashboard") {
      return fulfillJson(createDashboardSnapshot({
        metricSnapshot: {
          total_cost: 250000,
          priced_request_count: 9,
          unpriced_request_count: 2,
          total_requests: 20,
        },
        topSpendingModels: [
          {
            model_id: "gpt-4o-mini-p1",
            model_label: "GPT 4o Mini P1",
            total_cost_micros: 250000,
          },
        ],
      }));
    }

    if (pathname === "/api/stats/dashboard/recent-activity") {
      return fulfillJson(createDashboardRecentActivityResponse([]));
    }

    if (pathname === "/api/models") {
      return fulfillJson([createModelListItem()]);
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

  return {
    getLastProfileHeader: () => lastProfileHeader,
  };
}

test.describe("dashboard reporting currency", () => {
  test("renders dashboard totals with the active reporting currency", async ({ page }) => {
    const { getLastProfileHeader } = await mockDashboardRoutes(page);

    await page.goto("/observe?tab=overview");

    const metricValues = page.locator('[data-slot="metric-card"] [data-slot="metric-value"]');
    const spendingMetric = metricValues.nth(2);
    const spendingCard = page.locator('[data-slot="metric-card"]').filter({ hasText: "30d Total Spend" }).first();

    await expect(spendingMetric).toHaveText("¥0.25 CNY", {
      timeout: reportingCurrencyExpectationTimeout,
    });
    await expect(spendingCard).toContainText("Request-based spend");
    await expect(spendingCard).toContainText("9 priced");
    await expect(spendingCard).toContainText("2 unpriced");
    await expect(page.getByText("Top Models by Spend")).toBeVisible();
    await expect(page.getByText("Highest request-based spend in the last 30 days")).toBeVisible();
    await expect(page.getByText("¥0.25 CNY")).toHaveCount(2, {
      timeout: reportingCurrencyExpectationTimeout,
    });
    await expect.poll(getLastProfileHeader).toBe("1");
  });
});
