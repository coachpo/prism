import { expect, test, type Page } from "@playwright/test";
import {
  createDashboardModel,
  createDashboardRecentActivityResponse,
  createDashboardSnapshot,
  createUsageSnapshot,
} from "./dashboard-aggregate-fixtures";

const usageStatisticsStorageKey = "prism.statistics.usage-state";

async function mockUsageRoutes(
  page: Page,
  options: {
    empty?: boolean;
    requestBreakdowns?: boolean;
  } = {},
) {
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
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
    if (pathname === "/api/stats/dashboard") {
      return fulfillJson(createDashboardSnapshot());
    }
    if (pathname === "/api/stats/dashboard/recent-activity") {
      return fulfillJson(createDashboardRecentActivityResponse([]));
    }
    if (pathname === "/api/models") {
      return fulfillJson(
        options.requestBreakdowns
          ? [
              createDashboardModel("gpt-5.4", "Primary canonical model", 1),
              createDashboardModel("claude-3.7-sonnet", "Secondary global-only model", 2),
            ]
          : [createDashboardModel("gpt-5.4", "Primary canonical model", 1)],
      );
    }
    if (pathname === "/api/loadbalance/strategies" || pathname === "/api/endpoints") {
      return fulfillJson([]);
    }
    if (pathname === "/api/stats/usage-snapshot") {
      return fulfillJson(createUsageSnapshot(options));
    }
    if (/^\/api\/stats\/endpoints\/\d+\/models$/.test(pathname)) {
      return fulfillJson([]);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

async function seedUsageStatisticsState(
  page: Page,
  options: {
    selectedModelLines?: string[];
    chartGranularity?: Partial<{
      costOverview: "hourly" | "daily";
      latencyTrends: "hourly" | "daily";
      requestTrends: "hourly" | "daily";
      tokenTypeBreakdown: "hourly" | "daily";
      tokenUsageTrends: "hourly" | "daily";
    }>;
  } = {},
) {
  await page.addInitScript(
    ({ storageKey, selectedModelLines, chartGranularity }) => {
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          version: 1,
          state: {
            selectedTimeRange: "30d",
            selectedModelLines,
            chartGranularity: {
              costOverview: "hourly",
              latencyTrends: "hourly",
              requestTrends: "hourly",
              tokenTypeBreakdown: "hourly",
              tokenUsageTrends: "hourly",
              ...chartGranularity,
            },
          },
        }),
      );
    },
    {
      storageKey: usageStatisticsStorageKey,
      selectedModelLines: options.selectedModelLines ?? ["gpt-5.4"],
      chartGranularity: options.chartGranularity ?? {},
    },
  );
}

async function openAnalytics(page: Page) {
  await page.goto("/observe?tab=analytics");
  await expect(page).toHaveURL(/\/observe\?tab=analytics$/);
  await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({ timeout: 15_000 });
}

function chartCardByHeading(page: Page, name: string | RegExp) {
  return page
    .getByRole("heading", { name })
    .locator("xpath=ancestor::*[contains(concat(' ', normalize-space(@class), ' '), ' operator-section-surface ')][1]");
}

const requestTrendsHeading = /Request Trends|请求趋势/;
const latencyTrendsHeading = /Latency Trends|延迟趋势/;
const tokenUsageHeading = /Token Usage Trends|令牌用量趋势/;
const tokenBreakdownHeading = /Token Type Breakdown|令牌类型拆分/;
const topModelsHeading = /Top Models by Requests|按请求数排序的模型/;
const topEndpointsHeading = /Top Endpoints by Requests|按请求数排序的端点/;
const totalTokensCard = /Total Tokens|总令牌数/;
const noDataAvailable = /No data available|暂无可用数据/;
const noLatencyData = /No latency data|暂无延迟数据/;
const noTokenUsage = /No token usage|无令牌使用/;

function hourlyButton(card: ReturnType<typeof chartCardByHeading>) {
  return card.getByRole("button", { name: /按小时|By hour/i });
}

function dailyButton(card: ReturnType<typeof chartCardByHeading>) {
  return card.getByRole("button", { name: /按天|By day/i });
}

test.describe("shared chart statistics regression", () => {
  test("renders analytics charts and persists granularity changes", async ({ page }) => {
    await mockUsageRoutes(page);
    await seedUsageStatisticsState(page);
    await openAnalytics(page);

    await expect(page.getByTestId("usage-trends-grid")).toBeVisible();
    await expect(page.getByTestId("usage-cost-summary-card")).toContainText(/\$0\.62(?:\sUSD)?/);
    await expect(page.getByTestId("usage-kpi-card").filter({ hasText: totalTokensCard }).first()).toContainText(
      /Input 1,400|输入 1,400/,
    );

    const requestTrendsCard = chartCardByHeading(page, requestTrendsHeading);
    const tokenUsageCard = chartCardByHeading(page, tokenUsageHeading);
    const tokenBreakdownCard = chartCardByHeading(page, tokenBreakdownHeading);

    await expect(requestTrendsCard.locator(".recharts-cartesian-grid")).toBeVisible();
    await expect(tokenUsageCard.locator(".recharts-area-curve")).toHaveCount(1);
    await expect(tokenBreakdownCard.locator(".recharts-area-curve")).toHaveCount(4);

    await expect(hourlyButton(tokenUsageCard)).toHaveAttribute("aria-pressed", "true");
    await dailyButton(tokenUsageCard).click();
    await expect(dailyButton(tokenUsageCard)).toHaveAttribute("aria-pressed", "true");
    await expect(hourlyButton(tokenUsageCard)).toHaveAttribute("aria-pressed", "false");

    await dailyButton(requestTrendsCard).click();
    await expect(dailyButton(requestTrendsCard)).toHaveAttribute("aria-pressed", "true");

    await expect
      .poll(() =>
        page.evaluate((storageKey) => {
          const raw = localStorage.getItem(storageKey);
          return raw ? JSON.parse(raw).state?.chartGranularity ?? null : null;
        }, usageStatisticsStorageKey),
      )
      .toEqual({
        costOverview: "hourly",
        latencyTrends: "hourly",
        requestTrends: "daily",
        tokenTypeBreakdown: "hourly",
        tokenUsageTrends: "daily",
      });
  });

  test("shows tooltip details for request breakdown charts", async ({ page }) => {
    await mockUsageRoutes(page, { requestBreakdowns: true });
    await seedUsageStatisticsState(page, {
      selectedModelLines: ["gpt-5.4", "claude-3.7-sonnet"],
    });
    await openAnalytics(page);

    const modelPieCard = chartCardByHeading(page, topModelsHeading);
    const endpointPieCard = chartCardByHeading(page, topEndpointsHeading);

    await expect(modelPieCard.locator(".recharts-pie-sector")).toHaveCount(2);
    await expect(endpointPieCard.locator(".recharts-pie-sector")).toHaveCount(2);
    await expect(endpointPieCard).toContainText("Sub-CPA-B");
    await expect(endpointPieCard).toContainText("DeepSeek");
    await expect(endpointPieCard).not.toContainText("Endpoint 2");
    await expect(endpointPieCard).not.toContainText("Endpoint 3");

    await modelPieCard.locator(".recharts-pie-sector").first().hover();
    await expect(page.locator(".recharts-tooltip-wrapper").filter({ hasText: "Primary canonical model" })).toContainText(
      "4",
    );

    await endpointPieCard.locator(".recharts-pie-sector").nth(1).hover();
    await expect(page.locator(".recharts-tooltip-wrapper").filter({ hasText: "DeepSeek" })).toContainText("2");
    await expect(page.locator(".recharts-tooltip-wrapper").filter({ hasText: "Endpoint 3" })).toHaveCount(0);
  });

  test("keeps empty-state headers visible", async ({ page }) => {
    await mockUsageRoutes(page, { empty: true });
    await seedUsageStatisticsState(page);
    await openAnalytics(page);

    const trendsGrid = page.getByTestId("usage-trends-grid");
    await expect(page.getByTestId("usage-service-health-card")).toHaveCount(0);
    await expect(page.getByRole("heading", { name: requestTrendsHeading })).toHaveCount(1);
    await expect(page.getByRole("heading", { name: latencyTrendsHeading })).toHaveCount(1);
    await expect(page.getByRole("heading", { name: tokenUsageHeading })).toHaveCount(1);
    await expect(page.getByRole("heading", { name: tokenBreakdownHeading })).toHaveCount(1);
    await expect(page.getByRole("heading", { name: "Service Health" })).toHaveCount(0);
    await expect(trendsGrid.getByText(noDataAvailable)).toBeVisible();
    await expect(trendsGrid.getByText(noLatencyData)).toBeVisible();
    await expect.poll(() => page.getByText(noTokenUsage).count()).toBe(2);
    await expect(page.getByTestId("usage-cost-summary-card")).toHaveCount(0);
  });
});
