import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";

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
    api_family: "openai",
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
    api_family: "openai",
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
    output_tokens: 48,
    total_tokens: 120,
    total_cost_user_currency_micros: 250000,
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
      api_family: "openai",
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

async function mockDashboardRoutes(page: Page) {
  const profiles = [createProfile(1, "Red Team", true)];
  const modelListItem = createModelListItem();
  const modelDetail = createModelDetail();
  const connection = createConnection();
  const requestLogDetail = createRequestLogDetail();

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
        profiles,
        active_profile: profiles[0],
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/stats/summary") {
      return fulfillJson({
        total_requests: 42,
        success_requests: 41,
        failed_requests: 1,
        success_rate: 97.6,
        avg_response_time_ms: 123,
        p95_response_time_ms: 180,
        total_tokens: 2048,
        input_tokens: 1200,
        output_tokens: 700,
        cached_tokens: 100,
        reasoning_tokens: 48,
        groups: [],
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

    if (pathname === "/api/stats/spending") {
      if (searchParams.get("group_by") === "model_endpoint") {
        return fulfillJson({
          summary: {
            total_cost_micros: 250000,
            successful_request_count: 41,
            priced_request_count: 41,
            unpriced_request_count: 0,
            total_input_tokens: 1200,
            total_output_tokens: 700,
            total_cache_read_input_tokens: 100,
            total_cache_creation_input_tokens: 0,
            total_reasoning_tokens: 48,
            total_tokens: 2048,
            avg_cost_per_successful_request_micros: 6098,
          },
          groups: [
            {
              key: "model-a#201",
              total_cost_micros: 250000,
              total_requests: 42,
              priced_requests: 41,
              unpriced_requests: 0,
              total_tokens: 2048,
            },
          ],
          groups_total: 1,
          top_spending_models: [{ model_id: "model-a", total_cost_micros: 250000 }],
          top_spending_endpoints: [{ endpoint_id: 201, endpoint_label: "Endpoint A", total_cost_micros: 250000 }],
          unpriced_breakdown: {},
          report_currency_code: "USD",
          report_currency_symbol: "$",
        });
      }

      return fulfillJson({
        summary: {
          total_cost_micros: 250000,
          successful_request_count: 41,
          priced_request_count: 41,
          unpriced_request_count: 0,
          total_input_tokens: 1200,
          total_output_tokens: 700,
          total_cache_read_input_tokens: 100,
          total_cache_creation_input_tokens: 0,
          total_reasoning_tokens: 48,
          total_tokens: 2048,
          avg_cost_per_successful_request_micros: 6098,
        },
        groups: [],
        groups_total: 0,
        top_spending_models: [{ model_id: "model-a", total_cost_micros: 250000 }],
        top_spending_endpoints: [{ endpoint_id: 201, endpoint_label: "Endpoint A", total_cost_micros: 250000 }],
        unpriced_breakdown: {},
        report_currency_code: "USD",
        report_currency_symbol: "$",
      });
    }

    if (pathname === "/api/stats/throughput") {
      return fulfillJson({
        average_rpm: 1.2,
        peak_rpm: 2,
        current_rpm: 1,
        total_requests: 42,
        time_window_seconds: 86400,
        buckets: [],
      });
    }

    if (pathname === "/api/stats/requests") {
      return fulfillJson(createRequestLogsResponse());
    }

    if (pathname === "/api/stats/requests/301") {
      return fulfillJson(requestLogDetail);
    }

    if (pathname === "/api/stats/api-family") {
      return fulfillJson({ groups: [] });
    }

    if (pathname === "/api/stats/connection-success-rates") {
      return fulfillJson([
        {
          connection_id: 501,
          total_requests: 42,
          success_count: 41,
          error_count: 1,
          success_rate: 97.6,
        },
      ]);
    }

    if (pathname === "/api/models/connections/batch") {
      return fulfillJson({
        items: [{ model_config_id: 101, connections: [connection] }],
      });
    }

    if (pathname === "/api/models") {
      return fulfillJson([modelListItem]);
    }

    if (pathname === "/api/models/101") {
      return fulfillJson(modelDetail);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([]);
    }

    if (pathname === "/api/loadbalance-strategies") {
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

test.describe("dashboard routing shell renovation", () => {
  test("keeps the routing shell chrome and model-node activation behavior", async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text());
      }
    });

    await mockDashboardRoutes(page);

    await page.goto("/dashboard?tab=overview");

    const routingCard = page.locator('[data-slot="card"]').filter({ hasText: "Routing Health Map" }).first();

    await expect(routingCard).toBeVisible();
    await expect(routingCard.getByText("Link width reflects active connection count. Color reflects 24h route success rate.")).toBeVisible();
    await expect(routingCard.getByText("Click model nodes to open details")).toBeVisible();
    await expect(routingCard.getByText("1 endpoint")).toBeVisible();
    await expect(routingCard.getByText("1 model")).toBeVisible();
    await expect(routingCard.getByText("1 active route")).toBeVisible();
    await expect(routingCard.getByText("42 successful requests in 24h")).toBeVisible();
    await expect(routingCard.getByText("Healthy")).toBeVisible();
    await expect(routingCard.getByText("Degraded")).toBeVisible();
    await expect(routingCard.getByText("Failing")).toBeVisible();
    await expect(routingCard.getByText("No data")).toBeVisible();
    expect(
      consoleErrors.filter(
        (message) =>
          message.includes("cannot be a descendant") || message.includes("cannot contain a nested")
      )
    ).toEqual([]);
    await expect(routingCard.getByText("Endpoint A")).toBeVisible();
    await expect(routingCard.getByText("Model A")).toBeVisible();

    await routingCard.getByText("Endpoint A").click();
    await expect(page).toHaveURL(/\/dashboard\?tab=overview$/);

    await routingCard.getByText("Model A").click();
    await expect(page).toHaveURL(/\/models\/101$/);
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
