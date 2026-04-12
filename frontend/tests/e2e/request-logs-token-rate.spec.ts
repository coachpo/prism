import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";

function createProfile() {
  return {
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
  };
}

function createModelListItem() {
  return {
    id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini",
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

function createRequestLogItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 101,
    created_at: timestamp,
    model_id: "gpt-4o-mini",
    resolved_target_model_id: null,
    caller_client_display: "Prism QA Browser",
    upstream_client_display: "Prism QA Browser",
    user_agent_overridden: false,
    api_family: "openai",
    vendor_id: 1,
    vendor_key: "openai",
    vendor_name: "OpenAI",
    endpoint_id: 1,
    connection_id: null,
    completion_duration_ms: 125,
    status_code: 200,
    response_time_ms: 125,
    is_stream: true,
    total_tokens: 150,
    total_cost_user_currency_micros: 750000,
    report_currency_symbol: "$",
    ...overrides,
  };
}

async function mockRequestLogRoutes(page: Page, requestLogItems: Record<string, unknown>[]) {
  const profile = createProfile();
  const model = createModelListItem();

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

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({
        profiles: [profile],
        active_profile: profile,
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
      return fulfillJson([model]);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([
        {
          id: 1,
          name: "Primary endpoint",
          base_url: "https://api.example.test",
          provider: "openai",
          created_at: timestamp,
          updated_at: timestamp,
        },
      ]);
    }

    if (pathname === "/api/stats/requests") {
      const limit = Number.parseInt(searchParams.get("limit") ?? String(requestLogItems.length), 10);
      const offset = Number.parseInt(searchParams.get("offset") ?? "0", 10);

      return fulfillJson({
        items: requestLogItems,
        total: requestLogItems.length,
        limit,
        offset,
      });
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("request logs token rate", () => {
  test("renders buffered and completed-stream token rates from completion duration", async ({ page }) => {
    await mockRequestLogRoutes(page, [
      createRequestLogItem({
        id: 101,
        caller_client_display: "Buffered Zero Tokens",
        upstream_client_display: "Buffered Zero Tokens",
        is_stream: false,
        total_tokens: 0,
        completion_duration_ms: 500,
        response_time_ms: 2100,
      }),
      createRequestLogItem({
        id: 102,
        caller_client_display: "Completed Stream",
        upstream_client_display: "Completed Stream",
        is_stream: true,
        total_tokens: 150,
        completion_duration_ms: 300,
        response_time_ms: 4500,
      }),
    ]);

    await page.goto("/request-logs");

    const table = page.getByTestId("request-logs-table");
    await expect(table.getByText("Token Rate", { exact: true })).toBeVisible();

    const bufferedRow = page.getByRole("button").filter({ hasText: "Buffered Zero Tokens" });
    await expect(bufferedRow.locator(":scope > div").nth(2)).toHaveText("2,100ms");
    await expect(bufferedRow.locator(":scope > div").nth(4)).toHaveText("0.0 tok/s");

    const completedStreamRow = page.getByRole("button").filter({ hasText: "Completed Stream" });
    await expect(completedStreamRow.locator(":scope > div").nth(2)).toHaveText("4,500ms");
    await expect(completedStreamRow.locator(":scope > div").nth(4)).toHaveText("500.0 tok/s");
  });

  test("renders an em dash for legacy and incomplete rows without completion duration", async ({ page }) => {
    await mockRequestLogRoutes(page, [
      createRequestLogItem({
        id: 103,
        caller_client_display: "Legacy Buffered",
        upstream_client_display: "Legacy Buffered",
        is_stream: false,
        total_tokens: 90,
        completion_duration_ms: null,
        response_time_ms: 240,
      }),
      createRequestLogItem({
        id: 104,
        caller_client_display: "Incomplete Stream",
        upstream_client_display: "Incomplete Stream",
        is_stream: true,
        total_tokens: 45,
        completion_duration_ms: null,
        response_time_ms: 900,
      }),
    ]);

    await page.goto("/request-logs");

    const legacyBufferedRow = page.getByRole("button").filter({ hasText: "Legacy Buffered" });
    await expect(legacyBufferedRow.locator(":scope > div").nth(2)).toHaveText("240ms");
    await expect(legacyBufferedRow.locator(":scope > div").nth(4)).toHaveText("—");
    await expect(legacyBufferedRow).not.toContainText("375.0 tok/s");
    await expect(legacyBufferedRow).not.toContainText("Infinity");
    await expect(legacyBufferedRow).not.toContainText("NaN");

    const incompleteStreamRow = page.getByRole("button").filter({ hasText: "Incomplete Stream" });
    await expect(incompleteStreamRow.locator(":scope > div").nth(2)).toHaveText("900ms");
    await expect(incompleteStreamRow.locator(":scope > div").nth(4)).toHaveText("—");
    await expect(incompleteStreamRow).not.toContainText("50.0 tok/s");
  });
});
