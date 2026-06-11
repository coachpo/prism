import { expect, test } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";

function createUsageSnapshot(endpointStatistics: Array<Record<string, unknown>>) {
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
      hourly: [{ key: "all", label: "All requests", total_requests: 11, points: [] }],
      daily: [{ key: "all", label: "All requests", total_requests: 11, points: [] }],
    },
    token_usage_trends: {
      hourly: [{ key: "all", label: "All models", total_tokens: 1650, points: [] }],
      daily: [{ key: "all", label: "All models", total_tokens: 1650, points: [] }],
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
    endpoint_statistics: endpointStatistics,
    model_statistics: [],
    proxy_api_key_statistics: [],
  };
}

async function mockStatisticsRoutes(
  page: Parameters<typeof test>[0]["page"],
  options: {
    endpointModelStatisticsByEndpointId: Record<number, unknown[]>;
    onEndpointModelsRequest?: (endpointId: number) => void;
    usageSnapshot: ReturnType<typeof createUsageSnapshot>;
  },
) {
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
      return fulfillJson(options.usageSnapshot);
    }

    const endpointModelsMatch = pathname.match(/^\/api\/stats\/endpoints\/(\d+)\/models$/);
    if (endpointModelsMatch) {
      const endpointId = Number(endpointModelsMatch[1]);
      options.onEndpointModelsRequest?.(endpointId);
      return fulfillJson(options.endpointModelStatisticsByEndpointId[endpointId] ?? []);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

test.describe("statistics endpoint avg output rate", () => {
  test("renders numeric avg output rates for eligible groups in top-level and expanded endpoint rows", async ({ page }) => {
    const endpointModelRequests: number[] = [];

    await mockStatisticsRoutes(page, {
      usageSnapshot: createUsageSnapshot([
        {
          endpoint_id: 7,
          endpoint_label: "Buffered + completed endpoint",
          p50_ttft_ms: 120,
          p95_ttft_ms: 250,
          avg_output_rate_tps: 120.45,
          request_count: 8,
          success_rate: 87.5,
          total_tokens: 960,
          total_cost_micros: 120000,
        },
        {
          endpoint_id: null,
          endpoint_label: "Buffered + completed unknown endpoint",
          p50_ttft_ms: 80,
          p95_ttft_ms: 180,
          avg_output_rate_tps: 45.55,
          request_count: 3,
          success_rate: 100,
          total_tokens: 690,
          total_cost_micros: 130000,
        },
      ]),
      endpointModelStatisticsByEndpointId: {
        7: [
          {
            model_id: "gpt-4o-mini",
            model_label: "Buffered success model",
            p50_ttft_ms: 110,
            p95_ttft_ms: 210,
            avg_output_rate_tps: 95.44,
            request_count: 5,
            success_rate: 100,
            total_tokens: 600,
            total_cost_micros: 80000,
          },
          {
            model_id: "claude-3-5-sonnet",
            model_label: "Completed stream model",
            p50_ttft_ms: 140,
            p95_ttft_ms: 320,
            avg_output_rate_tps: 150.04,
            request_count: 3,
            success_rate: 66.7,
            total_tokens: 360,
            total_cost_micros: 40000,
          },
        ],
      },
      onEndpointModelsRequest: (endpointId) => endpointModelRequests.push(endpointId),
    });
    await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

    await page.goto("/observe?tab=analytics");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("Dashboard");
    const table = page.getByTestId("statistics-endpoint-table");
    const tableContainer = table.locator(".overflow-hidden.rounded-xl").first();
    await expect(tableContainer).toBeVisible({ timeout: 15000 });
    const headerRow = tableContainer.locator(":scope > div").nth(0);
    await expect(headerRow.locator(":scope > div").nth(0)).toHaveText("Endpoint");
    await expect(headerRow.locator(":scope > div").nth(1)).toHaveText("P50 TTFT");
    await expect(headerRow.locator(":scope > div").nth(2)).toHaveText("P95 TTFT");
    await expect(headerRow.locator(":scope > div").nth(3)).toHaveText("Avg Output Rate");
    await expect(headerRow.locator(":scope > div").nth(4)).toHaveText("Requests");
    await expect(headerRow.locator(":scope > div").nth(5)).toHaveText("Success Rate");
    await expect(headerRow.locator(":scope > div").nth(6)).toHaveText("Total Tokens");
    await expect(headerRow.locator(":scope > div").nth(7)).toHaveText("Total Spend");

    const unknownEndpointRow = table
      .getByText("Buffered + completed unknown endpoint")
      .locator("xpath=ancestor::div[contains(@class, 'grid')][1]");
    await expect(unknownEndpointRow.locator(":scope > div").nth(3)).toHaveText("45.6 tok/s");

    const endpointRow = page.getByRole("button", { name: "#7 Buffered + completed endpoint" });
    await expect(endpointRow.locator(":scope > div").nth(3)).toHaveText("120.5 tok/s");

    await endpointRow.click();

    await expect.poll(() => [...endpointModelRequests]).toEqual([7]);

    const endpointModelTable = page.getByTestId("statistics-endpoint-model-table-7");
    await expect(endpointModelTable).toBeVisible();
    const endpointModelHeaders = endpointModelTable.locator("thead th");
    await expect(endpointModelHeaders.nth(0)).toHaveText("Model");
    await expect(endpointModelHeaders.nth(1)).toHaveText("P50 TTFT");
    await expect(endpointModelHeaders.nth(2)).toHaveText("P95 TTFT");
    await expect(endpointModelHeaders.nth(3)).toHaveText("Avg Output Rate");
    await expect(endpointModelHeaders.nth(4)).toHaveText("Requests");
    await expect(endpointModelHeaders.nth(5)).toHaveText("Success Rate");
    await expect(endpointModelHeaders.nth(6)).toHaveText("Total Tokens");
    await expect(endpointModelHeaders.nth(7)).toHaveText("Total Spend");

    const firstModelRowCells = endpointModelTable.locator("tbody tr").nth(0).locator("td");
    await expect(firstModelRowCells.nth(0)).toHaveText("Buffered success model");
    await expect(firstModelRowCells.nth(3)).toHaveText("95.4 tok/s");

    const secondModelRowCells = endpointModelTable.locator("tbody tr").nth(1).locator("td");
    await expect(secondModelRowCells.nth(0)).toHaveText("Completed stream model");
    await expect(secondModelRowCells.nth(3)).toHaveText("150.0 tok/s");
  });

  test("renders numeric avg output rates for mixed top-level and expanded rows", async ({ page }) => {
    await mockStatisticsRoutes(page, {
      usageSnapshot: createUsageSnapshot([
        {
          endpoint_id: 8,
          endpoint_label: "Mixed / ineligible endpoint",
          p50_ttft_ms: 90,
          p95_ttft_ms: 190,
          avg_output_rate_tps: 100,
          request_count: 5,
          success_rate: 80,
          total_tokens: 500,
          total_cost_micros: 100000,
        },
        {
          endpoint_id: null,
          endpoint_label: "All ineligible unknown endpoint",
          p50_ttft_ms: null,
          p95_ttft_ms: null,
          avg_output_rate_tps: null,
          request_count: 2,
          success_rate: 50,
          total_tokens: 200,
          total_cost_micros: 0,
        },
      ]),
      endpointModelStatisticsByEndpointId: {
        8: [
          {
            model_id: "legacy-model",
            model_label: "Mixed / ineligible model",
            p50_ttft_ms: null,
            p95_ttft_ms: null,
            avg_output_rate_tps: null,
            request_count: 5,
            success_rate: 80,
            total_tokens: 500,
            total_cost_micros: 100000,
          },
        ],
      },
    });
    await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

    await page.goto("/observe?tab=analytics");

    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText(/Dashboard|仪表盘/);
    const table = page.getByTestId("statistics-endpoint-table");
    await expect(table).toBeVisible({ timeout: 15000 });
    const unknownEndpointRow = table
      .getByText("All ineligible unknown endpoint")
      .locator("xpath=ancestor::div[contains(@class, 'grid')][1]");
    await expect(unknownEndpointRow.locator(":scope > div").nth(3)).toHaveText("—");

    const endpointRow = page.getByRole("button", { name: "#8 Mixed / ineligible endpoint" });
    await expect(endpointRow.locator(":scope > div").nth(3)).toHaveText("100.0 tok/s");

    await endpointRow.click();

    const endpointModelTable = page.getByTestId("statistics-endpoint-model-table-8");
    await expect(endpointModelTable).toBeVisible();
    const firstModelRowCells = endpointModelTable.locator("tbody tr").nth(0).locator("td");
    await expect(firstModelRowCells.nth(0)).toHaveText("Mixed / ineligible model");
    await expect(firstModelRowCells.nth(3)).toHaveText("—");
  });
});
