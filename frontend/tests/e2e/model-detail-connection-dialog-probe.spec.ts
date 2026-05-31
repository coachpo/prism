import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-09T00:00:00Z";

function createModelResponse({
  id,
  apiFamily,
  modelId,
  displayName,
  connections = [],
}: {
  id: number;
  apiFamily: "openai" | "anthropic";
  modelId: string;
  displayName: string;
  connections?: Array<ReturnType<typeof createConnection>>;
}) {
  return {
    id,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: connections.map((connection, index) => ({
      id: 700 + index,
      target_type: "connection",
      target_model_id: null,
      connection_id: connection.id,
      position: index,
      is_enabled: true,
      target_model: null,
      connection,
      created_at: timestamp,
      updated_at: timestamp,
    })),
    is_enabled: true,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createConnection({
  id,
  modelConfigId,
  endpoint,
  name,
  openaiProbeEndpointVariant,
}: {
  id: number;
  modelConfigId: number;
  endpoint: { id: number; name: string; base_url: string };
  name: string;
  openaiProbeEndpointVariant: string | null;
}) {
  return {
    id,
    model_config_id: modelConfigId,
    endpoint_id: endpoint.id,
    endpoint,
    api_family: endpoint.base_url.includes("anthropic") ? "anthropic" : "openai",
    is_active: true,
    priority: 0,
    name,
    auth_type: null,
    custom_headers: null,
    openai_probe_endpoint_variant: openaiProbeEndpointVariant,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    health_status: "unknown",
    health_detail: null,
    last_health_check: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelListItem({
  id,
  apiFamily,
  modelId,
  displayName,
}: {
  id: number;
  apiFamily: "openai" | "anthropic";
  modelId: string;
  displayName: string;
}) {
  return {
    id,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createSpendingResponse() {
  return {
    summary: {
      total_cost_micros: 0,
      successful_request_count: 0,
      priced_request_count: 0,
      unpriced_request_count: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_read_input_tokens: 0,
      total_cache_creation_input_tokens: 0,
      total_reasoning_tokens: 0,
      total_tokens: 0,
      avg_cost_per_successful_request_micros: 0,
    },
    groups: [],
    groups_total: 0,
    top_spending_models: [],
    top_spending_endpoints: [],
    unpriced_breakdown: {},
    report_currency_code: "USD",
    report_currency_symbol: "$",
  };
}

async function stubModelDetailRoutes(page: Page, model: ReturnType<typeof createModelResponse>) {
  const endpoint = {
    id: 11,
    profile_id: 1,
    name: model.api_family === "openai" ? "OpenAI Primary" : "Anthropic Primary",
    base_url: model.api_family === "openai" ? "https://api.openai.com/v1" : "https://api.anthropic.com/v1",
    has_api_key: true,
    masked_api_key: "••••demo",
    position: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const profile = {
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
  const previewPayloads: unknown[] = [];
  const savePayloads: unknown[] = [];
  let accessTargets = [...model.access_targets];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const method = request.method();
    const pathname = url.pathname;

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

    if (pathname === `/api/models/${model.id}`) {
      return fulfillJson({ ...model, access_targets: accessTargets });
    }

    if (pathname === "/api/models") {
      return fulfillJson([
        createModelListItem({
          id: model.id,
          apiFamily: model.api_family,
          modelId: model.model_id,
          displayName: model.display_name ?? model.model_id,
        }),
      ]);
    }

    if (pathname === "/api/endpoints") {
      return fulfillJson([endpoint]);
    }

    if (pathname === `/api/models/${model.id}/connections` && method === "GET") {
      return fulfillJson(accessTargets.map((target) => target.connection).filter(Boolean));
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

    if (pathname === "/api/stats/spending") {
      return fulfillJson(createSpendingResponse());
    }

    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({ items: [] });
    }

    if (pathname === `/api/models/${model.id}/targets` && method === "GET") {
      return fulfillJson(accessTargets);
    }

    if (pathname === `/api/models/${model.id}/connections` && method === "POST") {
      const payload = request.postDataJSON();
      savePayloads.push(payload);
      const connection = {
        id: 101,
        model_config_id: model.id,
        endpoint_id: endpoint.id,
        endpoint,
        api_family: model.api_family,
        is_active: payload.is_active ?? true,
        priority: accessTargets.length,
        name: payload.name ?? endpoint.name,
        auth_type: null,
        custom_headers: payload.custom_headers ?? null,
        openai_probe_endpoint_variant: payload.openai_probe_endpoint_variant ?? null,
        pricing_template_id: payload.pricing_template_id ?? null,
        qps_limit: payload.qps_limit ?? null,
        max_in_flight_non_stream: payload.max_in_flight_non_stream ?? null,
        max_in_flight_stream: payload.max_in_flight_stream ?? null,
        pricing_template: null,
        health_status: "unknown",
        health_detail: null,
        last_health_check: null,
        created_at: timestamp,
        updated_at: timestamp,
      };
      accessTargets = [
        ...accessTargets,
        {
          id: 701 + accessTargets.length,
          target_type: "connection",
          target_model_id: null,
          connection_id: connection.id,
          position: accessTargets.length,
          is_enabled: true,
          target_model: null,
          connection,
          created_at: timestamp,
          updated_at: timestamp,
        },
      ];
      return fulfillJson(connection, 201);
    }

    if (pathname === `/api/models/${model.id}/connections/301` && method === "PATCH") {
      const payload = request.postDataJSON();
      savePayloads.push(payload);
      const updatedConnection = {
        ...(accessTargets[0]?.connection ?? {}),
        ...payload,
        model_config_id: model.id,
      };
      accessTargets = accessTargets.map((target) =>
        target.connection_id === 301 ? { ...target, connection: updatedConnection } : target,
      );
      return fulfillJson(updatedConnection);
    }

    if (pathname === `/api/models/${model.id}/connections/301/health` && method === "POST") {
      previewPayloads.push({ connection_id: 301 });
      return fulfillJson({
        health_status: "healthy",
        checked_at: timestamp,
        detail: "Preview ok",
        response_time_ms: 123,
      });
    }

    return fulfillJson({ error: `Unhandled ${method} ${pathname}` }, 500);
  });

  return { previewPayloads, savePayloads };
}

test("OpenAI connection dialog exposes probe controls and sends the resolved raw variant", async ({ page }) => {
  const model = createModelResponse({
    id: 1,
    apiFamily: "openai",
    modelId: "gpt-4.1",
    displayName: "GPT 4.1",
  });
  const { savePayloads } = await stubModelDetailRoutes(page, model);

  await page.goto("/models/1");
  await page.getByRole("button", { name: "New connection" }).first().click();

  await expect(page.getByTestId("connection-dialog-probe-section")).toBeVisible();
  await page.locator("#conn-selected-endpoint").click();
  await page.getByRole("option", { name: /OpenAI Primary/ }).click();
  await page.locator("#conn-probe-api").click();
  await page.getByRole("option", { name: /Chat Completions API/ }).click();
  await page.locator("#conn-probe-reasoning-mode").click();
  await page.getByRole("option", { name: /Disable reasoning/ }).click();

  await page.getByRole("button", { name: "Save Connection" }).click();
  await expect.poll(() => savePayloads.length).toBe(1);
  expect(savePayloads[0]).toMatchObject({
    openai_probe_endpoint_variant: "chat_completions_reasoning_none",
  });
});

test("non-OpenAI connection dialog hides the probe section", async ({ page }) => {
  const model = createModelResponse({
    id: 2,
    apiFamily: "anthropic",
    modelId: "claude-sonnet",
    displayName: "Claude Sonnet",
  });
  await stubModelDetailRoutes(page, model);

  await page.goto("/models/2");
  await page.getByRole("button", { name: "New connection" }).first().click();

  await expect(page.getByTestId("connection-dialog-probe-section")).toHaveCount(0);
});

test("editing an OpenAI connection hydrates the saved probe settings into both selectors", async ({ page }) => {
  const endpoint = {
    id: 11,
    name: "OpenAI Primary",
    base_url: "https://api.openai.com/v1",
  };
  const model = createModelResponse({
    id: 3,
    apiFamily: "openai",
    modelId: "gpt-4.1-edit",
    displayName: "GPT 4.1 Edit",
    connections: [
      createConnection({
        id: 301,
        modelConfigId: 3,
        endpoint,
        name: "Saved Connection",
        openaiProbeEndpointVariant: "chat_completions_reasoning_none",
      }),
    ],
  });
  await stubModelDetailRoutes(page, model);

  await page.goto("/models/3");
  await page.getByRole("button", { name: "Edit Saved Connection" }).first().click();

  await expect(page.getByTestId("connection-dialog-probe-section")).toBeVisible();
  await expect(page.locator("#conn-probe-api")).toContainText("Chat Completions API");
  await expect(page.locator("#conn-probe-reasoning-mode")).toContainText("Disable reasoning");
});
