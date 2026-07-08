import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";

function createRequestLogItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 101,
    created_at: timestamp,
    model_id: "gpt-4o-mini",
    model_label: "GPT-4o mini",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Prism QA Browser",
    upstream_client_display: "Prism QA Browser",
    user_agent_overridden: false,
    api_family: "openai",
    endpoint_id: 1,
    endpoint_label: "Primary endpoint",
    connection_id: null,
    completion_duration_ms: 125,
    ttft_ms: 25,
    status_code: 200,
    response_time_ms: 125,
    is_stream: true,
    output_tokens: 100,
    total_tokens: 150,
    total_cost_user_currency_micros: 750000,
    report_currency_symbol: "$",
    ...overrides,
  };
}

function createRequestLogsResponse(
  requestLogItems: Record<string, unknown>[],
  searchParams: URLSearchParams,
) {
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
      models: [
        {
          model_id: "gpt-4o-mini",
          model_label: "GPT-4o mini",
        },
      ],
      endpoints: [
        {
          endpoint_id: 1,
          endpoint_label: "Primary endpoint",
        },
      ],
      clients: [],
      resolved_target_models: [],
    },
  };
}

async function mockRequestLogRoutes(page: Page, requestLogItems: Record<string, unknown>[]) {
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
      throw new Error(
        "Unexpected /api/models request during request-logs browse mode",
      );
    }


    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      throw new Error(
        "Unexpected /api/endpoints request during request-logs browse mode",
      );
    }

    if (pathname === "/api/stats/requests") {
      return fulfillJson(createRequestLogsResponse(requestLogItems, searchParams));
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("request logs token rate", () => {
  test("renders Output Rate only for post-TTFT-eligible request-log rows", async ({ page }) => {
    await mockRequestLogRoutes(page, [
      createRequestLogItem({
        id: 101,
        caller_client_display: "Eligible Stream",
        upstream_client_display: "Eligible Stream",
        ttft_ms: 100,
        completion_duration_ms: 300,
        output_tokens: 150,
        total_tokens: 220,
        response_time_ms: 2100,
      }),
      createRequestLogItem({
        id: 102,
        caller_client_display: "Buffered Completed",
        upstream_client_display: "Buffered Completed",
        is_stream: false,
        ttft_ms: null,
        output_tokens: 120,
        total_tokens: 160,
        completion_duration_ms: 500,
        response_time_ms: 2500,
      }),
      createRequestLogItem({
        id: 103,
        caller_client_display: "Interrupted Stream",
        upstream_client_display: "Interrupted Stream",
        is_stream: true,
        ttft_ms: 80,
        output_tokens: 45,
        total_tokens: 90,
        completion_duration_ms: null,
        response_time_ms: 900,
      }),
      createRequestLogItem({
        id: 104,
        caller_client_display: "Missing TTFT",
        upstream_client_display: "Missing TTFT",
        is_stream: true,
        ttft_ms: null,
        output_tokens: 75,
        total_tokens: 110,
        completion_duration_ms: 300,
        response_time_ms: 1500,
      }),
      createRequestLogItem({
        id: 105,
        caller_client_display: "Zero Decode Window",
        upstream_client_display: "Zero Decode Window",
        is_stream: true,
        ttft_ms: 300,
        output_tokens: 40,
        total_tokens: 90,
        completion_duration_ms: 300,
        response_time_ms: 1800,
      }),
      createRequestLogItem({
        id: 106,
        caller_client_display: "Zero Output Stream",
        upstream_client_display: "Zero Output Stream",
        is_stream: true,
        ttft_ms: 100,
        output_tokens: 0,
        total_tokens: 60,
        completion_duration_ms: 500,
        response_time_ms: 2200,
      }),
      createRequestLogItem({
        id: 107,
        caller_client_display: "Legacy Buffered",
        upstream_client_display: "Legacy Buffered",
        is_stream: false,
        ttft_ms: null,
        output_tokens: null,
        total_tokens: 90,
        completion_duration_ms: null,
        response_time_ms: 240,
      }),
    ]);

    await page.goto("/observe/requests");

    const table = page.getByTestId("request-logs-table");
    await expect(table.getByText("Output Rate", { exact: true })).toBeVisible();

    const eligibleStreamRow = page.getByRole("button").filter({ hasText: "Eligible Stream" });
    await expect(eligibleStreamRow.locator(":scope > div").nth(2)).toHaveText("2,100ms");
    await expect(eligibleStreamRow.locator(":scope > div").nth(4)).toHaveText("750.0 tok/s");

    const bufferedRow = page.getByRole("button").filter({ hasText: "Buffered Completed" });
    await expect(bufferedRow.locator(":scope > div").nth(2)).toHaveText("2,500ms");
    await expect(bufferedRow.locator(":scope > div").nth(4)).toHaveText("—");

    const interruptedStreamRow = page.getByRole("button").filter({ hasText: "Interrupted Stream" });
    await expect(interruptedStreamRow.locator(":scope > div").nth(2)).toHaveText("900ms");
    await expect(interruptedStreamRow.locator(":scope > div").nth(4)).toHaveText("—");

    const missingTtftRow = page.getByRole("button").filter({ hasText: "Missing TTFT" });
    await expect(missingTtftRow.locator(":scope > div").nth(2)).toHaveText("1,500ms");
    await expect(missingTtftRow.locator(":scope > div").nth(4)).toHaveText("—");

    const zeroDecodeRow = page.getByRole("button").filter({ hasText: "Zero Decode Window" });
    await expect(zeroDecodeRow.locator(":scope > div").nth(2)).toHaveText("1,800ms");
    await expect(zeroDecodeRow.locator(":scope > div").nth(4)).toHaveText("—");

    const zeroOutputRow = page.getByRole("button").filter({ hasText: "Zero Output Stream" });
    await expect(zeroOutputRow.locator(":scope > div").nth(2)).toHaveText("2,200ms");
    await expect(zeroOutputRow.locator(":scope > div").nth(4)).toHaveText("0.0 tok/s");

    const legacyBufferedRow = page.getByRole("button").filter({ hasText: "Legacy Buffered" });
    await expect(legacyBufferedRow.locator(":scope > div").nth(2)).toHaveText("240ms");
    await expect(legacyBufferedRow.locator(":scope > div").nth(4)).toHaveText("—");

    await expect(table).not.toContainText("Infinity");
    await expect(table).not.toContainText("NaN");
  });
});
