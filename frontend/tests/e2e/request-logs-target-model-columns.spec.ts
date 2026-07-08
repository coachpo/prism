import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";

function createRequestLogItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 101,
    created_at: timestamp,
    model_id: "gpt-5.4-nano",
    model_label: "GPT 5.4 Nano",
    resolved_target_model_id: "gpt-5.3-codex-spark",
    resolved_target_model_label: "GPT 5.3 Codex Spark",
    caller_client_display: "Redirected Target",
    upstream_client_display: "Redirected Target",
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
    stream_outcome: "completed",
    stream_error_kind: null,
    reasoning_effort: "medium",
    output_tokens: 100,
    total_tokens: 150,
    total_cost_user_currency_micros: 750000,
    priced_flag: true,
    unpriced_reason: null,
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
        { model_id: "gpt-5.4-nano", model_label: "GPT 5.4 Nano" },
        { model_id: "gpt-5.4-mini", model_label: "GPT 5.4 Mini" },
      ],
      endpoints: [
        { endpoint_id: 1, endpoint_label: "Primary endpoint" },
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

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([]);
    if (pathname === "/api/models") throw new Error("Unexpected /api/models request during request-logs browse mode");
    if (pathname === "/api/endpoints") throw new Error("Unexpected /api/endpoints request during request-logs browse mode");
    if (pathname === "/api/stats/requests") return fulfillJson(createRequestLogsResponse(requestLogItems, searchParams));

    return fulfillJson({}, 404);
  });
}

test("request logs table separates requested and final target model columns", async ({ page }) => {
  await mockRequestLogRoutes(page, [
    createRequestLogItem(),
    createRequestLogItem({
      id: 102,
      model_id: "gpt-5.4-mini",
      model_label: "GPT 5.4 Mini",
      resolved_target_model_id: "gpt-5.4-mini-fast",
      resolved_target_model_label: null,
      caller_client_display: "Fallback Final Target",
      upstream_client_display: "Fallback Final Target",
    }),
  ]);

  await page.goto("/observe/requests");

  const table = page.getByTestId("request-logs-table");
  await expect(table.getByText("Requested Model", { exact: true })).toBeVisible();
  await expect(table.getByText("Final Target Model", { exact: true })).toBeVisible();
  await expect(table.getByText("Requested Model / Final Target Model", { exact: true })).toHaveCount(0);

  const redirectedRow = page.getByRole("button").filter({ hasText: "Redirected Target" });
  await expect(redirectedRow.locator(":scope > div").nth(5)).toHaveText("GPT 5.4 Nano");
  await expect(redirectedRow.locator(":scope > div").nth(6)).toHaveText("GPT 5.3 Codex Spark");

  const fallbackRow = page.getByRole("button").filter({ hasText: "Fallback Final Target" });
  await expect(fallbackRow.locator(":scope > div").nth(5)).toHaveText("GPT 5.4 Mini");
  await expect(fallbackRow.locator(":scope > div").nth(6)).toHaveText("gpt-5.4-mini-fast");
});
