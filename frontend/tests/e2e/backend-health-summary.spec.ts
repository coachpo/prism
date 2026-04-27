import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T00:00:00Z";
const modelConfigId = 101;
const modelId = "model-a";
const backendHealth = {
  status: "ok",
  version: "0.3.8",
  liveness: "ok",
  readiness: "ready",
  startup: "complete",
};

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
    id: modelConfigId,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: modelId,
    display_name: "Model A",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 99.1,
    health_total_requests: 12,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelDetail() {
  return {
    ...createModelListItem(),
    connections: [
      {
        id: 501,
        model_config_id: modelConfigId,
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
      },
    ],
  };
}

function createStatsSummary(groups: Array<Record<string, unknown>> = []) {
  return {
    total_requests: 20,
    success_count: 19,
    error_count: 1,
    success_rate: 95,
    avg_response_time_ms: 280,
    p95_response_time_ms: 540,
    total_input_tokens: 900,
    total_output_tokens: 600,
    total_tokens: 1500,
    groups,
  };
}

function createSpendingReport() {
  return {
    summary: {
      total_cost_micros: 125000,
      successful_request_count: 19,
      priced_request_count: 19,
      unpriced_request_count: 0,
      total_input_tokens: 900,
      total_output_tokens: 600,
      total_cache_read_input_tokens: 0,
      total_cache_creation_input_tokens: 0,
      total_reasoning_tokens: 0,
      total_tokens: 1500,
      avg_cost_per_successful_request_micros: 6578,
    },
    groups: [],
    groups_total: 0,
    top_spending_models: [{ model_id: modelId, total_cost_micros: 125000 }],
    top_spending_endpoints: [],
    unpriced_breakdown: {},
    report_currency_code: "USD",
    report_currency_symbol: "$",
  };
}

function createRequestLogsResponse() {
  return {
    items: [
      {
        id: 301,
        created_at: timestamp,
        model_id: modelId,
        model_label: "Model A",
        resolved_target_model_id: null,
        resolved_target_model_label: null,
        is_proxy_origin: false,
        caller_client_display: "Model A Request",
        upstream_client_display: "Model A Request",
        user_agent_overridden: false,
        api_family: "openai",
        vendor_id: null,
        vendor_key: null,
        vendor_name: null,
        endpoint_id: 201,
        endpoint_label: "Endpoint A",
        connection_id: 501,
        status_code: 200,
        ttft_ms: 75,
        completion_duration_ms: 220,
        response_time_ms: 300,
        is_stream: false,
        output_tokens: 40,
        total_tokens: 100,
        total_cost_user_currency_micros: 125000,
        priced_flag: true,
        unpriced_reason: null,
        report_currency_symbol: "$",
      },
    ],
    total: 1,
    limit: 12,
    offset: 0,
    filter_options: { endpoints: [], models: [{ model_id: modelId, model_label: "Model A" }] },
  };
}

async function mockBackendHealthRoutes(page: Page) {
  const profile = createProfile();

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, searchParams } = url;
    const method = request.method();

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/health") {
      return fulfillJson(backendHealth);
    }

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/models" && method === "GET") return fulfillJson([createModelListItem()]);
    if (pathname === `/api/models/${modelConfigId}`) return fulfillJson(createModelDetail());
    if (pathname === "/api/endpoints") return fulfillJson([]);
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([]);
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/vendors") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname === "/api/stats/summary") {
      const groupBy = searchParams.get("group_by");
      return fulfillJson(
        groupBy === "api_family"
          ? createStatsSummary([
              {
                key: "openai",
                total_requests: 20,
                success_count: 19,
                error_count: 1,
                avg_response_time_ms: 280,
                total_tokens: 1500,
              },
            ])
          : createStatsSummary(),
      );
    }
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingReport());
    if (pathname === "/api/stats/throughput") return fulfillJson({ average_rpm: 0.5, total_requests: 20 });
    if (pathname === "/api/stats/requests") return fulfillJson(createRequestLogsResponse());
    if (pathname === "/api/stats/connection-success-rates") return fulfillJson([]);
    if (pathname === "/api/stats/models/metrics" && method === "POST") {
      return fulfillJson({ items: [{ model_id: modelId, success_rate: 99.1, request_count_24h: 12, p95_latency_ms: 420, spend_30d_micros: 125000 }] });
    }
    if (pathname === "/api/models/connections/batch" && method === "POST") {
      return fulfillJson({ items: [{ model_config_id: modelConfigId, connections: [] }] });
    }

    return fulfillJson({ error: `Unhandled ${pathname}` }, 500);
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });
}

async function expectBackendHealthSummary(page: Page) {
  await expect(page.getByTestId("backend-health-summary")).toBeVisible();
  await expect(page.getByTestId("backend-health-status")).toContainText("ok");
  await expect(page.getByTestId("backend-health-liveness")).toContainText("ok");
  await expect(page.getByTestId("backend-health-readiness")).toContainText("ready");
  await expect(page.getByTestId("backend-health-startup")).toContainText("complete");
  await expect(page.getByTestId("backend-health-version")).toContainText("0.3.8");
}

test("shows the explicit backend /health readiness contract on dashboard, models, and model detail", async ({ page }) => {
  await mockBackendHealthRoutes(page);

  await page.goto("/dashboard?tab=overview");
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await expectBackendHealthSummary(page);

  await page.goto("/models");
  await expect(page.getByRole("heading", { name: "Models" })).toBeVisible();
  await expectBackendHealthSummary(page);

  await page.goto(`/models/${modelConfigId}`);
  await expect(page.getByRole("heading", { name: "Model A" })).toBeVisible();
  await expectBackendHealthSummary(page);
});
