import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";
const modelOptionLabel = "Bootstrap Filter Model";
const endpointOptionLabel = "Bootstrap Filter Endpoint";

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
    display_name: modelOptionLabel,
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
    caller_client_display: "Browse Fixture Row",
    upstream_client_display: "Browse Fixture Row",
    user_agent_overridden: false,
    api_family: "openai",
    vendor_id: 1,
    vendor_key: "openai",
    vendor_name: "OpenAI",
    endpoint_id: 1,
    endpoint_label: endpointOptionLabel,
    connection_id: null,
    ttft_ms: 95,
    completion_duration_ms: 280,
    status_code: 200,
    response_time_ms: 1250,
    is_stream: true,
    output_tokens: 150,
    total_tokens: 200,
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
      endpoints: [
        {
          endpoint_id: 1,
          endpoint_label: endpointOptionLabel,
        },
      ],
    },
  };
}

interface MockRouteState {
  failModels: boolean;
  failRequestLogs: boolean;
}

async function mockRequestLogRoutes(
  page: Page,
  state: MockRouteState,
) {
  const profile = createProfile();
  const model = createModelListItem();
  const requestLogItems = [createRequestLogItem()];

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
      if (state.failModels) {
        return fulfillJson({ detail: "Failed to load models" }, 500);
      }

      return fulfillJson([model]);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
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
      if (state.failRequestLogs) {
        return fulfillJson({ detail: "Failed to load request logs" }, 500);
      }

      return fulfillJson(createRequestLogsResponse(requestLogItems, searchParams));
    }

    return fulfillJson({}, 404);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

test.describe("request logs filter option loading", () => {
  test("keeps filter controls unloaded until models and stats endpoint options have both succeeded once", async ({
    page,
  }) => {
    const routeState: MockRouteState = {
      failModels: true,
      failRequestLogs: false,
    };

    await mockRequestLogRoutes(page, routeState);

    await page.goto("/request-logs");

    await expect(page.getByText("Browse Fixture Row")).toBeVisible();

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toHaveCount(0);
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toHaveCount(0);
    await page.keyboard.press("Escape");

    routeState.failModels = false;
    await page.getByRole("button", { name: "Refresh request logs" }).click();

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toBeVisible();
  });

  test("retains loaded filter controls after later request-list failures", async ({ page }) => {
    const routeState: MockRouteState = {
      failModels: false,
      failRequestLogs: false,
    };

    await mockRequestLogRoutes(page, routeState);

    await page.goto("/request-logs");

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    routeState.failRequestLogs = true;
    await page.getByRole("button", { name: "Refresh request logs" }).click();

    await expect(page.getByText("Failed to load request logs")).toBeVisible();

    await page.getByText("All models", { exact: true }).click();
    await expect(page.getByRole("option", { name: modelOptionLabel })).toBeVisible();
    await page.keyboard.press("Escape");

    await page.getByText("All endpoints", { exact: true }).click();
    await expect(page.getByRole("option", { name: endpointOptionLabel })).toBeVisible();
  });
});
