import { expect, test, type Page } from "@playwright/test";
import {
  createDashboardRecentActivityResponse,
  createDashboardSnapshot,
} from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-11T00:00:00Z";
const reportingCurrencyExpectationTimeout = 15_000;

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

function createCostingSettings(profileId: number) {
  return {
    report_currency_code: profileId === 2 ? "USD" : "CNY",
    report_currency_symbol: profileId === 2 ? "$" : "¥",
    endpoint_fx_mappings: [],
    timezone_preference: null,
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
    const profileId = Number(request.headers()["x-profile-id"] ?? "1");

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
      return fulfillJson(createCostingSettings(profileId));
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
            model_id: `gpt-4o-mini-p${profileId}`,
            model_label: `GPT 4o Mini P${profileId}`,
            total_cost_micros: 250000,
          },
        ],
      }));
    }

    if (pathname === "/api/stats/dashboard/recent-activity") {
      return fulfillJson(createDashboardRecentActivityResponse([]));
    }

    if (pathname === "/api/models") {
      return fulfillJson([createModelListItem(profileId)]);
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("dashboard reporting currency", () => {
  test("renders dashboard totals with the active reporting currency", async ({ page }) => {
    await mockDashboardRoutes(page);

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

    await page.getByTestId("shell-profile-switcher").getByRole("button").click();
    await page.getByRole("menuitem", { name: /Blue Team/ }).click();

    await expect(page.getByText("Loading application...")).toHaveCount(0, {
      timeout: reportingCurrencyExpectationTimeout,
    });
    await expect(spendingMetric).toHaveText("$0.25 USD", {
      timeout: reportingCurrencyExpectationTimeout,
    });
    await expect(page.getByText("$0.25 USD")).toHaveCount(2, {
      timeout: reportingCurrencyExpectationTimeout,
    });
  });
});
