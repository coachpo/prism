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

function createRequestLogsResponse() {
  return {
    items: [],
    total: 0,
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
    await expect(routingCard.getByText("Endpoint A")).toBeVisible();
    await expect(routingCard.getByText("Model A")).toBeVisible();

    await routingCard.getByText("Endpoint A").click();
    await expect(page).toHaveURL(/\/dashboard\?tab=overview$/);

    await routingCard.getByText("Model A").click();
    await expect(page).toHaveURL(/\/models\/101$/);
  });
});
