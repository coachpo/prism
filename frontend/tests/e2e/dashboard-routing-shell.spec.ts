import { expect, test, type Locator, type Page } from "@playwright/test";
import { createDashboardSnapshot } from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-11T00:00:00Z";
const mobileViewport = { width: 390, height: 844 };
const desktopViewport = { width: 1280, height: 900 };

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

function createModelListItem() {
  return {
    id: 101,
    vendor_id: null,
    vendor: null,
    api_family: "openai" as const,
    model_id: "model-a",
    display_name: "Model A",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 97.6,
    health_total_requests: 42,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createConnection() {
  return {
    id: 501,
    model_config_id: 101,
    endpoint_id: 201,
    endpoint: {
      id: 201,
      name: "Endpoint A",
      base_url: "https://endpoint-a.example",
      has_api_key: true,
      masked_api_key: "••••",
      position: 0,
      created_at: timestamp,
      updated_at: timestamp,
    },
    is_active: true,
    priority: 0,
    name: null,
    auth_type: null,
    custom_headers: null,
    openai_probe_endpoint_variant: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    health_status: "healthy",
    health_detail: null,
    last_health_check: timestamp,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelDetail() {
  return {
    ...createModelListItem(),
    connections: [createConnection()],
  };
}

function createRecentRequestLogItem() {
  return {
    id: 301,
    created_at: timestamp,
    model_id: "model-a",
    model_label: "Model A",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Dashboard Fixture Row",
    upstream_client_display: "Dashboard Fixture Row",
    user_agent_overridden: false,
    api_family: "openai" as const,
    vendor_id: null,
    vendor_key: null,
    vendor_name: null,
    endpoint_id: 201,
    endpoint_label: "Endpoint A",
    connection_id: 501,
    ttft_ms: 80,
    completion_duration_ms: 240,
    status_code: 200,
    response_time_ms: 640,
    is_stream: false,
    stream_outcome: "not_streaming" as const,
    stream_error_kind: null,
    reasoning_effort: null,
    output_tokens: 48,
    total_tokens: 120,
    total_cost_user_currency_micros: 250000,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
  };
}

function createRequestLogDetail() {
  return {
    summary: {
      id: 301,
      created_at: timestamp,
      model_id: "model-a",
      model_label: "Model A",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      api_family: "openai" as const,
      vendor_id: null,
      vendor_key: null,
      vendor_name: null,
      status_code: 200,
      response_time_ms: 640,
      is_stream: false,
      is_proxy_origin: false,
      ttft_ms: 80,
      completion_duration_ms: 240,
    },
    request: {
      request_path: "/v1/responses",
      ingress_request_id: "dashboard-ingress-301",
      attempt_number: 1,
      provider_correlation_id: "provider-corr-301",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Dashboard Fixture Row",
      upstream_user_agent: "Dashboard Fixture Row",
      caller_client_display: "Dashboard Fixture Row",
      upstream_client_display: "Dashboard Fixture Row",
      user_agent_overridden: false,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      model_id: "model-a",
      resolved_target_model_id: null,
      endpoint_id: 201,
      endpoint_label: "Endpoint A",
      endpoint_base_url: "https://endpoint-a.example",
      endpoint_description: "Primary endpoint",
      connection_id: 501,
      audit_enabled_at_request: true,
    },
    usage: {
      input_tokens: 72,
      output_tokens: 48,
      total_tokens: 120,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 100000,
      output_cost_micros: 150000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 250000,
      total_cost_user_currency_micros: 250000,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: "1",
      fx_rate_source: "manual",
    },
    pricing: {
      pricing_snapshot_unit: "1M tokens",
      pricing_snapshot_input: "0.10",
      pricing_snapshot_output: "0.20",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: 1,
    },
  };
}

function createRequestLogsResponse() {
  return {
    items: [createRecentRequestLogItem()],
    total: 1,
    limit: 12,
    offset: 0,
    filter_options: {
      endpoints: [],
    },
  };
}

async function mockDashboardRoutes(
  page: Page,
  options: { dashboardSnapshot?: ReturnType<typeof createDashboardSnapshot> } = {},
) {
  const profiles = [createProfile(1, "Red Team", true)];
  const modelDetail = createModelDetail();
  const requestLogDetail = createRequestLogDetail();
  const dashboardSnapshot = options.dashboardSnapshot ?? createDashboardSnapshot({
    recentRequests: [createRecentRequestLogItem()],
  });

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

    if (pathname === "/api/stats/dashboard") {
      return fulfillJson(dashboardSnapshot);
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

    if (pathname === "/api/stats/requests") {
      return fulfillJson(createRequestLogsResponse());
    }

    if (pathname === "/api/stats/requests/301") {
      return fulfillJson(requestLogDetail);
    }

    if (pathname === "/api/models") {
      return fulfillJson([createModelListItem()]);
    }

    if (pathname === "/api/models/101") {
      return fulfillJson(modelDetail);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([]);
    }

    if (pathname === "/api/pricing-templates") {
      return fulfillJson([]);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({ items: [] });
    }

    return fulfillJson({});
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

function getRoutingCard(page: Page) {
  return page.getByTestId("routing-diagram-card");
}

async function tabUntilFocused(page: Page, locator: Locator, maxTabs = 40) {
  await expect(locator).toBeVisible();

  for (let index = 0; index < maxTabs; index += 1) {
    const isFocused = await locator.evaluate((element) => element === document.activeElement);
    if (isFocused) {
      await expect(locator).toBeFocused();
      return;
    }

    await page.keyboard.press("Tab");
  }

  await expect(locator).toBeFocused();
}

async function expectNoHorizontalOverflow(page: Page, locator: Locator) {
  const [pageRootDimensions, elementDimensions] = await Promise.all([
    page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
    })),
    locator.evaluate((element) => ({
      clientWidth: element.clientWidth,
      scrollWidth: element.scrollWidth,
    })),
  ]);

  expect(pageRootDimensions.scrollWidth).toBeLessThanOrEqual(pageRootDimensions.clientWidth);
  expect(elementDimensions.scrollWidth).toBeLessThanOrEqual(elementDimensions.clientWidth);
}

test.describe("dashboard routing shell", () => {
  test("keeps the desktop routing shell chrome and Sankey drill-down behavior", async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text());
      }
    });

    await page.setViewportSize(desktopViewport);
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=overview");

    const routingCard = getRoutingCard(page);

    await expect(routingCard).toBeVisible();
    await expect(page.getByTestId("routing-diagram-sankey")).toBeVisible();
    await expect(page.getByTestId("routing-diagram-mobile")).toHaveCount(0);
    await expect(routingCard.getByText(/Desktop shows the backend-owned routing graph/i)).toBeVisible();
    await expect(
      routingCard.getByText("Activate model or endpoint targets to open details or request logs"),
    ).toBeVisible();
    await expect(routingCard.getByText(/Entry Model -> Planner -> Access Targets -> Terminal Target -> Endpoint/i)).toBeVisible();
    await expect(routingCard.getByText(/terminal-target topology after planner and access-target resolution/i)).toBeVisible();
    await expect(routingCard.getByText(/browser does not reconstruct graph edges from management reads/i)).toBeVisible();
    await expect(routingCard.getByText("1 endpoint")).toBeVisible();
    await expect(routingCard.getByText("2 models")).toBeVisible();
    await expect(routingCard.getByText("1 active target")).toBeVisible();
    await expect(routingCard.getByText("42 successful requests in 24h")).toBeVisible();
    await expect(routingCard.getByText("Model", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Terminal Targets", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Endpoint", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Disabled", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Inactive", { exact: true })).toBeVisible();
    expect(
      consoleErrors.filter(
        (message) =>
          message.includes("cannot be a descendant") || message.includes("cannot contain a nested"),
      ),
    ).toEqual([]);
    await expect(routingCard.getByText("Disabled Model", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Primary Target", { exact: true })).toBeVisible();
    await expect(routingCard.getByText("Backup Target", { exact: true })).toBeVisible();

    await routingCard.getByRole("button", { name: "Model A", exact: true }).click();
    await expect(page).toHaveURL(/\/models\/101$/);

    await page.goto("/dashboard?tab=overview");
    const refreshedRoutingCard = getRoutingCard(page);
    await refreshedRoutingCard.getByRole("button", { name: "Endpoint A", exact: true }).click();
    await expect(page).toHaveURL(/\/request-logs\?endpoint_id=201$/);
  });

  test("covers the compact routing list at 390x844 with keyboard drill-down and no overflow", async ({ page }) => {
    await page.setViewportSize(mobileViewport);
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=overview");

    const routingCard = getRoutingCard(page);
    const modelAction = routingCard
      .getByRole("article")
      .filter({ has: page.getByRole("heading", { name: "Model A", level: 5 }) })
      .getByRole("button", { name: "View model details for Model A" });

    await expect(routingCard).toBeVisible();
    await expect(page.getByTestId("routing-diagram-mobile")).toBeVisible();
    await expect(page.getByTestId("routing-diagram-sankey")).toHaveCount(0);
    await expect(routingCard.getByText(/Desktop shows the backend-owned routing graph/i)).toBeVisible();
    await expect(
      routingCard.getByText("Activate model or endpoint targets to open details or request logs"),
    ).toBeVisible();
    await expectNoHorizontalOverflow(page, routingCard);
    await tabUntilFocused(page, modelAction);
    await expect(modelAction).toBeFocused();
    await expect(modelAction).toBeInViewport();
    await modelAction.press("Enter");
    await expect(page).toHaveURL(/\/models\/101$/);

    await page.goto("/dashboard?tab=overview");
    const refreshedRoutingCard = getRoutingCard(page);
    const endpointAction = refreshedRoutingCard
      .getByRole("article")
      .filter({ has: page.getByRole("heading", { name: "Endpoint A", level: 5 }) })
      .getByRole("button", {
        name: "View Request Logs: Endpoint A",
      });

    await tabUntilFocused(page, endpointAction);
    await expect(endpointAction).toBeFocused();
    await expect(endpointAction).toBeInViewport();
    await endpointAction.press("Enter");
    await expect(page).toHaveURL(/\/request-logs\?endpoint_id=201$/);
  });

  test("does not render the removed routing strategy mix card", async ({ page }) => {
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=overview");

    await expect(page.getByText("Routing strategy mix")).toHaveCount(0);
    await expect(page.getByText("Legacy strategy 1")).toHaveCount(0);
    await expect(page.getByText("Adaptive strategy 1")).toHaveCount(0);
    await expect(page.getByText("Strategy not configured 1")).toHaveCount(0);
  });

  test("opens exact request investigation from recent activity", async ({ page }) => {
    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=overview");

    const recentActivityCard = page.locator('[data-slot="card"]').filter({ hasText: "Recent Activity" }).first();

    await expect(recentActivityCard.getByRole("button", { name: /Model A/ })).toBeVisible();
    await recentActivityCard.getByRole("button", { name: /Model A/ }).click();

    await expect(page).toHaveURL(/\/request-logs\?request_id=301$/);
    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Request #301" })).toBeVisible();
  });
});
