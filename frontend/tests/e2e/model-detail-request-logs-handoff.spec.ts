import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T00:00:00Z";
const modelConfigId = 101;
const modelId = "model-a";
const otherModelId = "model-b";

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

function createModelListItem(id: number, model_id: string, display_name: string) {
  return {
    id,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id,
    display_name,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 99.1,
    health_total_requests: 12,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createConnection() {
  return {
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
  };
}

function createModelDetail() {
  const connection = createConnection();

  return {
    ...createModelListItem(modelConfigId, modelId, "Model A"),
    access_targets: [
      {
        id: 701,
        target_type: "connection",
        target_model_id: null,
        connection_id: connection.id,
        position: 0,
        is_enabled: true,
        target_model: null,
        connection,
        created_at: timestamp,
        updated_at: timestamp,
      },
    ],
  };
}

function createRequestLogItem(id: number, requestedModelId: string, modelLabel: string) {
  return {
    id,
    created_at: timestamp,
    model_id: requestedModelId,
    model_label: modelLabel,
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: `${modelLabel} Request`,
    upstream_client_display: `${modelLabel} Request`,
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
    total_tokens: 120,
    total_cost_user_currency_micros: 125000,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
  };
}

function createSpendingReport() {
  return {
    summary: {
      total_cost_micros: 125000,
      successful_request_count: 1,
      priced_request_count: 1,
      unpriced_request_count: 0,
      total_input_tokens: 60,
      total_output_tokens: 40,
      total_cache_read_input_tokens: 10,
      total_cache_creation_input_tokens: 5,
      total_reasoning_tokens: 5,
      total_tokens: 120,
      avg_cost_per_successful_request_micros: 125000,
    },
    groups: [],
    groups_total: 0,
    top_spending_models: [{ model_id: modelId, model_label: "Model A", total_cost_micros: 125000 }],
    top_spending_endpoints: [],
    unpriced_breakdown: {},
    report_currency_code: "USD",
    report_currency_symbol: "$",
  };
}

function createRequestLogsResponse(searchParams: URLSearchParams) {
  const items = [
    createRequestLogItem(301, modelId, "Model A"),
    createRequestLogItem(302, otherModelId, "Model B"),
  ].filter((item) => {
    const selectedModelId = searchParams.get("model_id");
    return !selectedModelId || item.model_id === selectedModelId;
  });

  return {
    items,
    total: items.length,
    limit: Number.parseInt(searchParams.get("limit") ?? "100", 10),
    offset: Number.parseInt(searchParams.get("offset") ?? "0", 10),
    filter_options: {
      models: [
        { model_id: modelId, model_label: "Model A" },
        { model_id: otherModelId, model_label: "Model B" },
      ],
      endpoints: [{ endpoint_id: 201, endpoint_label: "Endpoint A" }],
    },
  };
}

async function mockModelDetailRequestLogRoutes(page: Page) {
  const profile = createProfile();
  const requestSearches: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, searchParams } = url;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/models") {
      return fulfillJson([
        createModelListItem(modelConfigId, modelId, "Model A"),
        createModelListItem(102, otherModelId, "Model B"),
      ]);
    }
    if (pathname === `/api/models/${modelConfigId}`) return fulfillJson(createModelDetail());
    if (pathname === "/api/endpoints") return fulfillJson([]);
    if (pathname === "/api/connections") return fulfillJson([createConnection()]);
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([]);
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/vendors") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingReport());
    if (pathname === "/api/stats/requests") {
      requestSearches.push(searchParams.toString());
      return fulfillJson(createRequestLogsResponse(searchParams));
    }

    return fulfillJson({ error: `Unhandled ${pathname}` }, 500);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
  return { requestSearches };
}

test("model detail overview CTA preserves the existing model_id request-log browse contract", async ({ page }) => {
  const { requestSearches } = await mockModelDetailRequestLogRoutes(page);

  await page.goto(`/models/${modelConfigId}`);
  await expect(page.getByRole("heading", { name: "Model A" })).toBeVisible();
  await expect(page.getByText("120 tokens")).toBeVisible();
  await expect(page.getByText("Input 60 · Output 40 · Cached 15 · Reasoning 5")).toBeVisible();
  await expect(page.getByRole("button", { name: "View Request Logs" })).toBeVisible();

  await page.getByRole("button", { name: "View Request Logs" }).click();

  await expect(page).toHaveURL(/\/request-logs\?model_id=model-a$/);
  await expect(page.getByRole("button", { name: "Clear Filters" })).toBeVisible();
  await expect(page.getByTestId("request-logs-table")).toBeVisible();
  await expect(page.getByRole("button").filter({ hasText: "Model A Request" })).toBeVisible();
  await expect(page.getByRole("button").filter({ hasText: "Model B Request" })).toHaveCount(0);

  await expect.poll(() => requestSearches.length).toBeGreaterThan(0);
  const lastRequestSearch = requestSearches[requestSearches.length - 1] ?? "";
  expect(lastRequestSearch).toContain("model_id=model-a");
  expect(lastRequestSearch).not.toContain("request_id=");
  expect(lastRequestSearch).not.toContain("model_config_id=");
});
