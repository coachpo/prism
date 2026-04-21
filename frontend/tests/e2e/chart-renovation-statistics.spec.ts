import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-12T00:00:00Z";
const usageStatisticsStorageKey = "prism.statistics.usage-state";
const evidenceDirectory = resolve(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  ".sisyphus",
  "evidence",
);
const populatedEvidencePath = resolve(evidenceDirectory, "task-3-chart-populated.txt");
const emptyEvidencePath = resolve(evidenceDirectory, "task-3-chart-empty.txt");

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

function createModel(modelId: string, displayName: string, id: number) {
  return {
    id,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: modelId,
    display_name: displayName,
    model_type: "proxy",
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

function createHealthCell(
  bucketStart: string,
  status: "ok" | "degraded" | "down" | "empty",
  availabilityPercentage: number | null,
  requestCount: number,
  successCount: number,
  failedCount: number,
) {
  return {
    bucket_start: bucketStart,
    request_count: requestCount,
    success_count: successCount,
    failed_count: failedCount,
    availability_percentage: availabilityPercentage,
    status,
  };
}

function createUsageSnapshot(options?: { empty?: boolean; profileId?: number }) {
  const empty = options?.empty ?? false;
  const profileId = options?.profileId ?? 1;
  const modelId = profileId === 2 ? "claude-3.7-sonnet" : "gpt-5.4";
  const modelLabel = profileId === 2 ? "Secondary global-only model" : "Primary canonical model";

  return {
    generated_at: timestamp,
    time_range: {
      preset: "30d",
      start_at: "2026-03-13T00:00:00Z",
      end_at: timestamp,
    },
    currency: { code: "USD", symbol: "$" },
    overview: {
      total_requests: empty ? 0 : 6,
      success_requests: empty ? 0 : 5,
      failed_requests: empty ? 0 : 1,
      success_rate: empty ? 0 : 83.3,
      total_tokens: empty ? 0 : 2400,
      input_tokens: empty ? 0 : 1400,
      output_tokens: empty ? 0 : 900,
      cached_tokens: empty ? 0 : 50,
      reasoning_tokens: empty ? 0 : 50,
      average_rpm: empty ? 0 : 0.2,
      average_tpm: empty ? 0 : 80,
      total_cost_micros: empty ? 0 : 620000,
      rolling_window_minutes: 30,
      rolling_request_count: empty ? 0 : 2,
      rolling_token_count: empty ? 0 : 800,
      rolling_rpm: empty ? 0 : 0.07,
      rolling_tpm: empty ? 0 : 26.67,
    },
    service_health: {
      availability_percentage: empty ? null : 97.5,
      request_count: empty ? 0 : 6,
      success_count: empty ? 0 : 5,
      failed_count: empty ? 0 : 1,
      interval_minutes: 60,
      cells: empty
        ? []
        : [
            createHealthCell("2026-04-09T00:00:00Z", "ok", 100, 1, 1, 0),
            createHealthCell("2026-04-09T01:00:00Z", "ok", 100, 2, 2, 0),
            createHealthCell("2026-04-09T02:00:00Z", "degraded", 50, 2, 1, 1),
            createHealthCell("2026-04-09T03:00:00Z", "ok", 100, 1, 1, 0),
          ],
    },
    request_trends: {
      hourly: [
        {
          key: "all",
          label: "All requests",
          total_requests: empty ? 0 : 6,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  request_count: 2,
                  success_count: 2,
                  failed_count: 0,
                  rpm: 2,
                },
                {
                  bucket_start: "2026-04-09T01:00:00Z",
                  request_count: 4,
                  success_count: 3,
                  failed_count: 1,
                  rpm: 4,
                },
              ],
        },
        {
          key: modelId,
          label: modelLabel,
          total_requests: empty ? 0 : 6,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  request_count: 2,
                  success_count: 2,
                  failed_count: 0,
                  rpm: 2,
                },
                {
                  bucket_start: "2026-04-09T01:00:00Z",
                  request_count: 4,
                  success_count: 3,
                  failed_count: 1,
                  rpm: 4,
                },
              ],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All requests",
          total_requests: empty ? 0 : 6,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-08T00:00:00Z",
                  request_count: 2,
                  success_count: 2,
                  failed_count: 0,
                  rpm: 0.08,
                },
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  request_count: 4,
                  success_count: 3,
                  failed_count: 1,
                  rpm: 0.17,
                },
              ],
        },
        {
          key: modelId,
          label: modelLabel,
          total_requests: empty ? 0 : 6,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-08T00:00:00Z",
                  request_count: 2,
                  success_count: 2,
                  failed_count: 0,
                  rpm: 0.08,
                },
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  request_count: 4,
                  success_count: 3,
                  failed_count: 1,
                  rpm: 0.17,
                },
              ],
        },
      ],
    },
    token_usage_trends: {
      hourly: [
        {
          key: "all",
          label: "All models",
          total_tokens: empty ? 0 : 2400,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  total_tokens: 900,
                  input_tokens: 500,
                  output_tokens: 350,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 900,
                },
                {
                  bucket_start: "2026-04-09T01:00:00Z",
                  total_tokens: 1500,
                  input_tokens: 900,
                  output_tokens: 550,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 1500,
                },
              ],
        },
        {
          key: modelId,
          label: modelLabel,
          total_tokens: empty ? 0 : 2400,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  total_tokens: 900,
                  input_tokens: 500,
                  output_tokens: 350,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 900,
                },
                {
                  bucket_start: "2026-04-09T01:00:00Z",
                  total_tokens: 1500,
                  input_tokens: 900,
                  output_tokens: 550,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 1500,
                },
              ],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All models",
          total_tokens: empty ? 0 : 2400,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-08T00:00:00Z",
                  total_tokens: 900,
                  input_tokens: 500,
                  output_tokens: 350,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 37.5,
                },
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  total_tokens: 1500,
                  input_tokens: 900,
                  output_tokens: 550,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 62.5,
                },
              ],
        },
        {
          key: modelId,
          label: modelLabel,
          total_tokens: empty ? 0 : 2400,
          points: empty
            ? []
            : [
                {
                  bucket_start: "2026-04-08T00:00:00Z",
                  total_tokens: 900,
                  input_tokens: 500,
                  output_tokens: 350,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 37.5,
                },
                {
                  bucket_start: "2026-04-09T00:00:00Z",
                  total_tokens: 1500,
                  input_tokens: 900,
                  output_tokens: 550,
                  cached_tokens: 25,
                  reasoning_tokens: 25,
                  tpm: 62.5,
                },
              ],
        },
      ],
    },
    token_type_breakdown: {
      hourly: empty
        ? []
        : [
            {
              bucket_start: "2026-04-09T00:00:00Z",
              input_tokens: 500,
              output_tokens: 350,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
            {
              bucket_start: "2026-04-09T01:00:00Z",
              input_tokens: 900,
              output_tokens: 550,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
          ],
      daily: empty
        ? []
        : [
            {
              bucket_start: "2026-04-08T00:00:00Z",
              input_tokens: 500,
              output_tokens: 350,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
            {
              bucket_start: "2026-04-09T00:00:00Z",
              input_tokens: 900,
              output_tokens: 550,
              cached_tokens: 25,
              reasoning_tokens: 25,
            },
          ],
    },
    cost_overview: {
      total_cost_micros: empty ? 0 : 620000,
      priced_request_count: empty ? 0 : 2,
      unpriced_request_count: empty ? 0 : 0,
      hourly: empty
        ? []
        : [
            { bucket_start: "2026-04-09T00:00:00Z", total_cost_micros: 170000 },
            { bucket_start: "2026-04-09T01:00:00Z", total_cost_micros: 450000 },
          ],
      daily: empty
        ? []
        : [
            { bucket_start: "2026-04-08T00:00:00Z", total_cost_micros: 170000 },
            { bucket_start: "2026-04-09T00:00:00Z", total_cost_micros: 450000 },
          ],
    },
    endpoint_statistics: empty
      ? []
      : [
          {
            endpoint_id: 10,
            endpoint_label: "Primary canonical endpoint",
            p50_ttft_ms: 120,
            p95_ttft_ms: 220,
            request_count: 6,
            success_rate: 83.3,
            total_tokens: 2400,
            avg_output_rate_tps: 81.63,
            total_cost_micros: 620000,
          },
        ],
    model_statistics: [
      {
        model_id: modelId,
        model_label: modelLabel,
        p50_ttft_ms: empty ? null : 120,
        p95_ttft_ms: empty ? null : 220,
        success_count: empty ? 0 : 5,
        failed_count: empty ? 0 : 1,
        priced_request_count: empty ? 0 : 2,
        unpriced_request_count: empty ? 0 : 0,
        request_count: empty ? 0 : 6,
        success_rate: empty ? 0 : 83.3,
        input_tokens: empty ? 0 : 1400,
        output_tokens: empty ? 0 : 900,
        cached_tokens: empty ? 0 : 50,
        reasoning_tokens: empty ? 0 : 50,
        total_tokens: empty ? 0 : 2400,
        avg_output_rate_tps: empty ? null : 81.63,
        total_cost_micros: empty ? 0 : 620000,
      },
    ],
    proxy_api_key_statistics: [],
  };
}

async function mockUsageRoutes(page: Page, options?: { empty?: boolean }) {
  const profiles = [createProfile(1, "Red Team", true), createProfile(2, "Blue Team")];

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
      return fulfillJson([
        createModel(profileId === 2 ? "claude-3.7-sonnet" : "gpt-5.4", profileId === 2 ? "Secondary global-only model" : "Primary canonical model", profileId),
      ]);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([]);
    }

    if (pathname === "/api/stats/usage-snapshot") {
      return fulfillJson(createUsageSnapshot({ empty: options?.empty, profileId }));
    }

    const endpointModelsMatch = pathname.match(/^\/api\/stats\/endpoints\/(\d+)\/models$/);
    if (endpointModelsMatch) {
      return fulfillJson([]);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

async function seedUsageStatisticsState(page: Page, selectedModelLines: string[]) {
  await page.addInitScript(
    ({ nextSelectedModelLines, storageKey }) => {
      localStorage.setItem("prism.locale", "en");
      localStorage.setItem(
        storageKey,
        JSON.stringify({
          version: 1,
          state: {
            selectedTimeRange: "30d",
            selectedModelLines: nextSelectedModelLines,
            chartGranularity: {
              costOverview: "hourly",
              requestTrends: "hourly",
              tokenTypeBreakdown: "hourly",
              tokenUsageTrends: "hourly",
            },
          },
        }),
      );
    },
    { nextSelectedModelLines: selectedModelLines, storageKey: usageStatisticsStorageKey },
  );
}

async function writeEvidenceFile(filePath: string, lines: string[]) {
  await mkdir(dirname(filePath), { recursive: true });
  await writeFile(filePath, `${lines.join("\n")}\n`, "utf8");
}

async function expectSharedPopulatedSurface(page: Page) {
  await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible();
  await expect(page.getByTestId("usage-trends-grid")).toBeVisible();
  await expect(page.getByTestId("usage-cost-summary-card")).toBeVisible();
  await expect(page.getByTestId("usage-service-health-card")).toBeVisible();
  await expect(page.getByTestId("usage-health-heatmap")).toBeVisible();

  await expect(page.getByRole("heading", { name: "Request Trends" }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Token Usage Trends" }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Token Type Breakdown" }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Cost Overview" }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Service Health" }).first()).toBeVisible();

  await expect(page.getByTestId("usage-trends-grid").getByText("No data available", { exact: true })).toHaveCount(0);
  await expect(page.getByText("No token usage", { exact: true })).toHaveCount(0);
  await expect(page.getByText("No cost records found.", { exact: true })).toHaveCount(0);
  await expect(page.getByTestId("usage-cost-summary-total")).toHaveText(/\$0\.62(?:\sUSD)?/);
  await expect(page.getByTestId("usage-health-availability-badge")).toHaveText("97.5%");
  await expect(page.locator('[data-testid="usage-health-cell"][data-status="ok"]').first()).toBeVisible();
}

test.describe("chart renovation statistics regression", () => {
  test("covers populated statistics and dashboard analytics chart surfaces from the shared usage snapshot content", async ({ page }) => {
    await mockUsageRoutes(page);
    await seedUsageStatisticsState(page, ["gpt-5.4"]);

    await page.goto("/dashboard?tab=analytics");

    await expect(page).toHaveURL(/\/dashboard\?tab=analytics$/);
    await expect(page.getByRole("tab", { name: "Analytics" })).toHaveAttribute("data-state", "active");
    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText(/Dashboard|仪表盘/);
    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({ timeout: 15000 });
    await expectSharedPopulatedSurface(page);

    const statisticsOkCells = await page
      .locator('[data-testid="usage-health-cell"][data-status="ok"]')
      .count();

    const analyticsOkCells = statisticsOkCells;

    await writeEvidenceFile(populatedEvidencePath, [
      "scenario=populated-shared-chart-surfaces",
      "dashboard.route=/dashboard?tab=analytics",
      "statistics.selectors=usage-controls-toolbar,usage-trends-grid,usage-cost-summary-card,usage-service-health-card,usage-health-heatmap",
      `statistics.okHealthCells=${statisticsOkCells}`,
      "statistics.costSummary=$0.62 USD",
      "dashboard.sharedSurface=UsageStatisticsContent",
      "dashboard.selectors=usage-controls-toolbar,usage-trends-grid,usage-cost-summary-card,usage-service-health-card,usage-health-heatmap",
      `dashboard.okHealthCells=${analyticsOkCells}`,
      "dashboard.analyticsTab=active",
    ]);
  });

  test("navigates the overview analytics CTA to the analytics tab", async ({ page }) => {
    await mockUsageRoutes(page);
    await seedUsageStatisticsState(page, ["gpt-5.4"]);

    await page.goto("/dashboard?tab=overview");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText(/Dashboard|仪表盘/);
    const analyticsCta = page.getByRole("button", { name: "Analytics" });
    await expect(analyticsCta).toBeVisible();

    await analyticsCta.click();

    await expect(page).toHaveURL(/\/dashboard\?tab=analytics$/);
    await expect(page.getByRole("tab", { name: "Analytics" })).toHaveAttribute("data-state", "active");
    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({ timeout: 15000 });
  });

  test("keeps empty statistics chart headers visible while service health falls back to idle cells", async ({ page }) => {
    await mockUsageRoutes(page, { empty: true });
    await seedUsageStatisticsState(page, ["gpt-5.4"]);

    await page.goto("/dashboard?tab=analytics");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText(/Dashboard|仪表盘/);
    const trendsGrid = page.getByTestId("usage-trends-grid");

    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({ timeout: 15000 });
    await expect(trendsGrid).toBeVisible();
    await expect(page.getByTestId("usage-service-health-card")).toBeVisible();
    await expect(page.getByTestId("usage-health-heatmap")).toBeVisible();

    await expect(page.getByRole("heading", { name: "Request Trends" }).first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "Token Usage Trends" }).first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "Token Type Breakdown" }).first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "Cost Overview" }).first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "Service Health" }).first()).toBeVisible();

    await expect(trendsGrid.getByText("No data available", { exact: true })).toBeVisible();
    await expect.poll(() => page.getByText("No token usage", { exact: true }).count()).toBe(2);
    await expect(page.getByText("No cost records found.", { exact: true })).toBeVisible();
    await expect(page.getByTestId("usage-cost-summary-card")).toHaveCount(0);
    await expect(page.getByTestId("usage-health-availability-badge")).toHaveText("—");
    await expect(page.getByTestId("usage-health-window-label")).toHaveText("Last day");
    await expect(page.locator('[data-testid="usage-health-cell"][data-status="empty"]').first()).toBeVisible();

    const emptyHealthCellCount = await page
      .locator('[data-testid="usage-health-cell"][data-status="empty"]')
      .count();

    await writeEvidenceFile(emptyEvidencePath, [
      "scenario=empty-statistics-chart-surfaces",
      "dashboard.route=/dashboard?tab=analytics",
      "visibleHeaders=Request Trends,Token Usage Trends,Token Type Breakdown,Cost Overview,Service Health",
      "visibleEmptyTitles=No data available,No token usage,No cost records found.",
      `emptyHealthCells=${emptyHealthCellCount}`,
      "availabilityBadge=—",
      "costSummaryCard=hidden",
    ]);
  });
});
