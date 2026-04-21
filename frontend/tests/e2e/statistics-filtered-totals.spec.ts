import { expect, test } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";
const usageStatisticsStorageKey = "prism.statistics.usage-state";

function createUsageSnapshot() {
  return {
    generated_at: timestamp,
    time_range: {
      preset: "30d",
      start_at: "2026-03-11T00:00:00Z",
      end_at: timestamp,
    },
    currency: { code: "USD", symbol: "$" },
    overview: {
      total_requests: 6,
      success_requests: 5,
      failed_requests: 1,
      success_rate: 83.3,
      total_tokens: 2400,
      input_tokens: 1400,
      output_tokens: 900,
      cached_tokens: 50,
      reasoning_tokens: 50,
      average_rpm: 0.2,
      average_tpm: 80,
      total_cost_micros: 710000,
      rolling_window_minutes: 30,
      rolling_request_count: 2,
      rolling_token_count: 800,
      rolling_rpm: 0.07,
      rolling_tpm: 26.67,
    },
    service_health: {
      availability_percentage: 83.3,
      request_count: 6,
      success_count: 5,
      failed_count: 1,
      interval_minutes: 15,
      cells: [],
    },
    request_trends: {
      hourly: [
        { key: "all", label: "All requests", total_requests: 6, points: [] },
        { key: "gpt-5.4", label: "Primary canonical model", total_requests: 4, points: [] },
        {
          key: "claude-3.7-sonnet",
          label: "Secondary global-only model",
          total_requests: 2,
          points: [],
        },
      ],
      daily: [
        { key: "all", label: "All requests", total_requests: 6, points: [] },
        { key: "gpt-5.4", label: "Primary canonical model", total_requests: 4, points: [] },
        {
          key: "claude-3.7-sonnet",
          label: "Secondary global-only model",
          total_requests: 2,
          points: [],
        },
      ],
    },
    token_usage_trends: {
      hourly: [
        { key: "all", label: "All models", total_tokens: 2400, points: [] },
        {
          key: "gpt-5.4",
          label: "Primary canonical model",
          total_tokens: 1600,
          points: [],
        },
        {
          key: "claude-3.7-sonnet",
          label: "Secondary global-only model",
          total_tokens: 800,
          points: [],
        },
      ],
      daily: [
        { key: "all", label: "All models", total_tokens: 2400, points: [] },
        {
          key: "gpt-5.4",
          label: "Primary canonical model",
          total_tokens: 1600,
          points: [],
        },
        {
          key: "claude-3.7-sonnet",
          label: "Secondary global-only model",
          total_tokens: 800,
          points: [],
        },
      ],
    },
    token_type_breakdown: {
      hourly: [],
      daily: [],
    },
    cost_overview: {
      total_cost_micros: 710000,
      priced_request_count: 3,
      unpriced_request_count: 2,
      hourly: [{ bucket_start: "2026-04-10T00:00:00Z", total_cost_micros: 710000 }],
      daily: [{ bucket_start: "2026-04-10T00:00:00Z", total_cost_micros: 710000 }],
    },
    endpoint_statistics: [
      {
        endpoint_id: 10,
        endpoint_label: "Primary canonical endpoint",
        p50_ttft_ms: 120,
        p95_ttft_ms: 220,
        request_count: 4,
        success_rate: 75,
        total_tokens: 1600,
        avg_output_rate_tps: 81.63,
        total_cost_micros: 620000,
      },
      {
        endpoint_id: 20,
        endpoint_label: "Secondary global-only endpoint",
        p50_ttft_ms: 120,
        p95_ttft_ms: 120,
        request_count: 2,
        success_rate: 100,
        total_tokens: 800,
        avg_output_rate_tps: 81.63,
        total_cost_micros: 90000,
      },
    ],
    model_statistics: [
      {
        model_id: "gpt-5.4",
        model_label: "Primary canonical model",
        p50_ttft_ms: 120,
        p95_ttft_ms: 220,
        success_count: 3,
        failed_count: 1,
        priced_request_count: 2,
        unpriced_request_count: 1,
        request_count: 4,
        success_rate: 75,
        input_tokens: 900,
        output_tokens: 650,
        cached_tokens: 25,
        reasoning_tokens: 25,
        total_tokens: 1600,
        avg_output_rate_tps: 81.63,
        total_cost_micros: 620000,
      },
      {
        model_id: "claude-3.7-sonnet",
        model_label: "Secondary global-only model",
        p50_ttft_ms: 120,
        p95_ttft_ms: 120,
        success_count: 2,
        failed_count: 0,
        priced_request_count: 1,
        unpriced_request_count: 1,
        request_count: 2,
        success_rate: 100,
        input_tokens: 500,
        output_tokens: 250,
        cached_tokens: 25,
        reasoning_tokens: 25,
        total_tokens: 800,
        avg_output_rate_tps: 81.63,
        total_cost_micros: 90000,
      },
    ],
    proxy_api_key_statistics: [],
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

async function mockStatisticsRoutes(page: Parameters<typeof test>[0]["page"]) {
  const endpointModelRequestCounts = {
    10: 0,
    20: 0,
  };

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
      return fulfillJson([
        createModel("gpt-5.4", "Primary canonical model", 1),
        createModel("claude-3.7-sonnet", "Secondary global-only model", 2),
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
      return fulfillJson(createUsageSnapshot());
    }

    if (pathname === "/api/stats/endpoints/10/models") {
      endpointModelRequestCounts[10] += 1;
      return fulfillJson([
        {
          model_id: "gpt-5.4",
          model_label: "Primary canonical model",
          p50_ttft_ms: 120,
          p95_ttft_ms: 220,
          success_count: 3,
          failed_count: 1,
          priced_request_count: 2,
          unpriced_request_count: 1,
          request_count: 4,
          success_rate: 75,
          input_tokens: 900,
          output_tokens: 650,
          cached_tokens: 25,
          reasoning_tokens: 25,
          total_tokens: 1600,
          avg_output_rate_tps: 81.63,
          total_cost_micros: 620000,
        },
      ]);
    }

    if (pathname === "/api/stats/endpoints/20/models") {
      endpointModelRequestCounts[20] += 1;
      return fulfillJson([
        {
          model_id: "claude-3.7-sonnet",
          model_label: "Secondary global-only model",
          p50_ttft_ms: 120,
          p95_ttft_ms: 120,
          success_count: 2,
          failed_count: 0,
          priced_request_count: 1,
          unpriced_request_count: 1,
          request_count: 2,
          success_rate: 100,
          input_tokens: 500,
          output_tokens: 250,
          cached_tokens: 25,
          reasoning_tokens: 25,
          total_tokens: 800,
          avg_output_rate_tps: 81.63,
          total_cost_micros: 90000,
        },
      ]);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  return { endpointModelRequestCounts };
}

async function seedUsageStatisticsState(
  page: Parameters<typeof test>[0]["page"],
  selectedModelLines: string[],
) {
  await page.addInitScript(
    ({ selectedModelLines: nextSelectedModelLines, storageKey }) => {
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
    { selectedModelLines, storageKey: usageStatisticsStorageKey },
  );
}

function getMetricCard(page: Parameters<typeof test>[0]["page"], label: string) {
  return page.locator('[data-testid="usage-kpi-card"]').filter({ hasText: label }).first();
}

test.describe("statistics selected-model totals", () => {
  test("scopes overview totals and model tables to the selected model line", async ({ page }) => {
    const { endpointModelRequestCounts } = await mockStatisticsRoutes(page);
    await seedUsageStatisticsState(page, ["gpt-5.4"]);

    await page.goto("/dashboard?tab=analytics");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("Dashboard");
    await expect(page.getByTestId("usage-model-line-section")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("usage-model-line-section")).toContainText("gpt-5.4");    const requestsCard = getMetricCard(page, "Requests");
    await expect(requestsCard.locator('[data-slot="metric-value"]')).toHaveText("4");

    const tokensCard = getMetricCard(page, "Total Tokens");
    await expect(tokensCard.locator('[data-slot="metric-value"]')).toHaveText("1,600");

    const spendCard = getMetricCard(page, "Total Spend");
    await expect(spendCard.locator('[data-slot="metric-value"]')).toHaveText(/\$0\.62(?:\sUSD)?/);
    await expect(spendCard).toContainText("Request-based spend");
    await expect(spendCard).toContainText("2 priced");
    await expect(spendCard).toContainText("1 unpriced");
    await expect(page.getByTestId("usage-cost-summary-total")).toHaveText(/\$0\.62(?:\sUSD)?/);
    await expect(page.getByText("2 priced", { exact: true })).toBeVisible();
    await expect(page.getByText("1 unpriced", { exact: true })).toBeVisible();

    const topEndpointsCard = page.getByTestId("usage-top-endpoints-card");
    await expect(topEndpointsCard).toContainText("Primary canonical endpoint");
    await expect(topEndpointsCard).not.toContainText("Secondary global-only endpoint");
    await expect.poll(() => endpointModelRequestCounts[10]).toBe(1);
    await expect.poll(() => endpointModelRequestCounts[20]).toBe(1);

    const topModelsCard = page.getByTestId("usage-top-models-card");
    await expect(topModelsCard).toContainText("Primary canonical model");
    await expect(topModelsCard).not.toContainText("Secondary global-only model");

    const modelTable = page.getByTestId("statistics-model-table");
    await expect(modelTable).toContainText("Primary canonical model");
    await expect(modelTable).not.toContainText("Secondary global-only model");

    await expect(page.getByTestId("usage-health-availability-badge")).toHaveText("83.3%");

    await page.getByRole("button", { name: "Remove line gpt-5.4" }).click();

    await expect(page.getByTestId("usage-model-line-section")).toContainText("No data available");
    await expect(requestsCard.locator('[data-slot="metric-value"]')).toHaveText("6");
    await expect(tokensCard.locator('[data-slot="metric-value"]')).toHaveText("2,400");
    await expect(spendCard.locator('[data-slot="metric-value"]')).toHaveText(/\$0\.71(?:\sUSD)?/);
    await expect(spendCard).toContainText("Request-based spend");
    await expect(spendCard).toContainText("3 priced");
    await expect(spendCard).toContainText("2 unpriced");
    await expect(page.getByTestId("usage-cost-summary-total")).toHaveText(/\$0\.71(?:\sUSD)?/);
    await expect(page.getByText("3 priced", { exact: true })).toBeVisible();
    await expect(page.getByText("2 unpriced", { exact: true })).toBeVisible();
    await expect(topEndpointsCard).toContainText("Primary canonical endpoint");
    await expect(topEndpointsCard).toContainText("Secondary global-only endpoint");
    await expect(topModelsCard).toContainText("Primary canonical model");
    await expect(topModelsCard).toContainText("Secondary global-only model");
    await expect(modelTable).toContainText("Primary canonical model");
    await expect(modelTable).toContainText("Secondary global-only model");
    await expect(page.getByTestId("usage-health-availability-badge")).toHaveText("83.3%");
    await expect.poll(() => endpointModelRequestCounts[10]).toBe(1);
    await expect.poll(() => endpointModelRequestCounts[20]).toBe(1);

    const modelLineSection = page.getByTestId("usage-model-line-section");
    await modelLineSection.getByRole("combobox").click();
    await page.getByRole("option", { name: "gpt-5.4" }).click();
    await modelLineSection.getByRole("button", { name: "Add line" }).click();

    await expect(page.getByTestId("usage-model-line-section")).toContainText("gpt-5.4");
    await expect(topEndpointsCard).toContainText("Primary canonical endpoint");
    await expect(topEndpointsCard).not.toContainText("Secondary global-only endpoint");
    await expect.poll(() => endpointModelRequestCounts[10]).toBe(1);
    await expect.poll(() => endpointModelRequestCounts[20]).toBe(1);
  });

  test("keeps global totals when multiple selected model lines cover all models", async ({ page }) => {
    const { endpointModelRequestCounts } = await mockStatisticsRoutes(page);
    await seedUsageStatisticsState(page, ["gpt-5.4", "claude-3.7-sonnet"]);

    await page.goto("/dashboard?tab=analytics");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("Dashboard");
    await expect(page.getByTestId("usage-model-line-section")).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId("usage-model-line-section")).toContainText("2 / 9");
    const requestsCard = getMetricCard(page, "Requests");
    const tokensCard = getMetricCard(page, "Total Tokens");
    const spendCard = getMetricCard(page, "Total Spend");

    await expect(requestsCard.locator('[data-slot="metric-value"]')).toHaveText("6");
    await expect(tokensCard.locator('[data-slot="metric-value"]')).toHaveText("2,400");
    await expect(spendCard.locator('[data-slot="metric-value"]')).toHaveText(/\$0\.71(?:\sUSD)?/);
    await expect(page.getByTestId("usage-cost-summary-total")).toHaveText(/\$0\.71(?:\sUSD)?/);

    const topModelsCard = page.getByTestId("usage-top-models-card");
    await expect(topModelsCard).toContainText("Primary canonical model");
    await expect(topModelsCard).toContainText("Secondary global-only model");

    const modelTable = page.getByTestId("statistics-model-table");
    await expect(modelTable).toContainText("Primary canonical model");
    await expect(modelTable).toContainText("Secondary global-only model");
    await expect(page.getByTestId("usage-health-availability-badge")).toHaveText("83.3%");
    await expect.poll(() => endpointModelRequestCounts[10]).toBe(1);
    await expect.poll(() => endpointModelRequestCounts[20]).toBe(1);
  });
});
