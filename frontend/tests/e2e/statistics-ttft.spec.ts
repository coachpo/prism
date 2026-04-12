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
      return fulfillJson(options.endpointModelStatisticsByEndpointId[endpointId] ?? []);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

test.describe("statistics endpoint TTFT percentiles", () => {
  test("renders P50/P95 TTFT columns before avg token rate for endpoint and model rows", async ({ page }) => {
    await mockStatisticsRoutes(page, {
      usageSnapshot: createUsageSnapshot([
        {
          endpoint_id: 7,
          endpoint_label: "TTFT endpoint",
          p50_ttft_ms: 123.4,
          p95_ttft_ms: 456.6,
          avg_token_rate: 120.45,
          request_count: 8,
          success_rate: 87.5,
          total_tokens: 960,
          total_cost_micros: 120000,
        },
        {
          endpoint_id: null,
          endpoint_label: "Unknown TTFT endpoint",
          p50_ttft_ms: 10.2,
          p95_ttft_ms: 99.8,
          avg_token_rate: 45.55,
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
            model_label: "TTFT model",
            p50_ttft_ms: 222.2,
            p95_ttft_ms: 777.7,
            avg_token_rate: 95.44,
            request_count: 5,
            success_rate: 100,
            total_tokens: 600,
            total_cost_micros: 80000,
          },
        ],
      },
    });
    await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

    await page.goto("/statistics");

    const table = page.getByTestId("statistics-endpoint-table");
    const tableContainer = table.locator(".overflow-hidden.rounded-xl").first();
    const headerRow = tableContainer.locator(":scope > div").nth(0);
    await expect(headerRow.locator(":scope > div").nth(0)).toHaveText("Endpoint");
    await expect(headerRow.locator(":scope > div").nth(1)).toHaveText("P50 TTFT");
    await expect(headerRow.locator(":scope > div").nth(2)).toHaveText("P95 TTFT");
    await expect(headerRow.locator(":scope > div").nth(3)).toHaveText("Avg Token Rate");
    await expect(headerRow.locator(":scope > div").nth(4)).toHaveText("Requests");
    await expect(headerRow.locator(":scope > div").nth(5)).toHaveText("Success Rate");
    await expect(headerRow.locator(":scope > div").nth(6)).toHaveText("Total Tokens");
    await expect(headerRow.locator(":scope > div").nth(7)).toHaveText("Total Spend");

    const unknownEndpointRow = table
      .getByText("Unknown TTFT endpoint")
      .locator("xpath=ancestor::div[contains(@class, 'grid')][1]");
    await expect(unknownEndpointRow.locator(":scope > div").nth(1)).toHaveText("10ms");
    await expect(unknownEndpointRow.locator(":scope > div").nth(2)).toHaveText("100ms");
    await expect(unknownEndpointRow.locator(":scope > div").nth(3)).toHaveText("45.6 tok/s");

    const endpointRow = page.getByRole("button", { name: "#7 TTFT endpoint" });
    await expect(endpointRow.locator(":scope > div").nth(1)).toHaveText("123ms");
    await expect(endpointRow.locator(":scope > div").nth(2)).toHaveText("457ms");
    await expect(endpointRow.locator(":scope > div").nth(3)).toHaveText("120.5 tok/s");

    await endpointRow.click();

    const endpointModelTable = page.getByTestId("statistics-endpoint-model-table-7");
    await expect(endpointModelTable).toBeVisible();
    const endpointModelHeaders = endpointModelTable.locator("thead th");
    await expect(endpointModelHeaders.nth(0)).toHaveText("Model");
    await expect(endpointModelHeaders.nth(1)).toHaveText("P50 TTFT");
    await expect(endpointModelHeaders.nth(2)).toHaveText("P95 TTFT");
    await expect(endpointModelHeaders.nth(3)).toHaveText("Avg Token Rate");
    await expect(endpointModelHeaders.nth(4)).toHaveText("Requests");
    await expect(endpointModelHeaders.nth(5)).toHaveText("Success Rate");
    await expect(endpointModelHeaders.nth(6)).toHaveText("Total Tokens");
    await expect(endpointModelHeaders.nth(7)).toHaveText("Total Spend");

    const firstModelRowCells = endpointModelTable.locator("tbody tr").nth(0).locator("td");
    await expect(firstModelRowCells.nth(0)).toHaveText("TTFT model");
    await expect(firstModelRowCells.nth(1)).toHaveText("222ms");
    await expect(firstModelRowCells.nth(2)).toHaveText("778ms");
    await expect(firstModelRowCells.nth(3)).toHaveText("95.4 tok/s");
  });

  test("renders em dash for null P50/P95 TTFT values in endpoint and model rows", async ({ page }) => {
    await mockStatisticsRoutes(page, {
      usageSnapshot: createUsageSnapshot([
        {
          endpoint_id: 8,
          endpoint_label: "Null TTFT endpoint",
          p50_ttft_ms: null,
          p95_ttft_ms: null,
          avg_token_rate: null,
          request_count: 5,
          success_rate: 80,
          total_tokens: 500,
          total_cost_micros: 100000,
        },
        {
          endpoint_id: null,
          endpoint_label: "Null TTFT unknown endpoint",
          p50_ttft_ms: null,
          p95_ttft_ms: null,
          avg_token_rate: null,
          request_count: 2,
          success_rate: 50,
          total_tokens: 200,
          total_cost_micros: 0,
        },
      ]),
      endpointModelStatisticsByEndpointId: {
        8: [
          {
            model_id: "null-ttft-model",
            model_label: "Null TTFT model",
            p50_ttft_ms: null,
            p95_ttft_ms: null,
            avg_token_rate: null,
            request_count: 5,
            success_rate: 80,
            total_tokens: 500,
            total_cost_micros: 100000,
          },
        ],
      },
    });
    await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));

    await page.goto("/statistics");

    const table = page.getByTestId("statistics-endpoint-table");
    const unknownEndpointRow = table
      .getByText("Null TTFT unknown endpoint")
      .locator("xpath=ancestor::div[contains(@class, 'grid')][1]");
    await expect(unknownEndpointRow.locator(":scope > div").nth(1)).toHaveText("—");
    await expect(unknownEndpointRow.locator(":scope > div").nth(2)).toHaveText("—");

    const endpointRow = page.getByRole("button", { name: "#8 Null TTFT endpoint" });
    await expect(endpointRow.locator(":scope > div").nth(1)).toHaveText("—");
    await expect(endpointRow.locator(":scope > div").nth(2)).toHaveText("—");

    await endpointRow.click();

    const endpointModelTable = page.getByTestId("statistics-endpoint-model-table-8");
    await expect(endpointModelTable).toBeVisible();
    const firstModelRowCells = endpointModelTable.locator("tbody tr").nth(0).locator("td");
    await expect(firstModelRowCells.nth(0)).toHaveText("Null TTFT model");
    await expect(firstModelRowCells.nth(1)).toHaveText("—");
    await expect(firstModelRowCells.nth(2)).toHaveText("—");
  });
});
