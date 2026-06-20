import { expect, test, type Page } from "@playwright/test";
import type {
  Connection,
  Endpoint,
  OpenAIProbeEndpointVariant,
  OpenAITextCapability,
} from "../../src/lib/types";

const timestamp = "2026-04-09T00:00:00Z";

type TestAccessTarget = {
  id: number;
  target_type: "connection";
  target_model_id: null;
  connection_id: number;
  position: number;
  is_enabled: boolean;
  target_model: null;
  connection: Connection;
  created_at: string;
  updated_at: string;
};

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
  connections?: Connection[];
}) {
  return {
    id,
    profile_id: 1,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: connections.map<TestAccessTarget>((connection, index) => ({
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
  openaiTextCapability = "responses_only",
  contextWindowTokens = 16384,
  defaultOutputTokenReserve = 4096,
  maxContextUtilization = 0.9,
  preferredContextUtilizationThreshold = null,
  contextCapabilityOverrides = {
    context_window_tokens: null,
    default_output_token_reserve: null,
    max_context_utilization: null,
    preferred_context_utilization_threshold: null,
  },
}: {
  id: number;
  modelConfigId: number;
  endpoint: Endpoint;
  name: string | null;
  openaiProbeEndpointVariant: OpenAIProbeEndpointVariant | null;
  openaiTextCapability?: OpenAITextCapability | null;
  contextWindowTokens?: number | null;
  defaultOutputTokenReserve?: number;
  maxContextUtilization?: number;
  preferredContextUtilizationThreshold?: number | null;
  contextCapabilityOverrides?: {
    context_window_tokens: number | null;
    default_output_token_reserve: number | null;
    max_context_utilization: number | null;
    preferred_context_utilization_threshold: number | null;
  };
}): Connection {
  return {
    id,
    profile_id: 1,
    model_config_id: modelConfigId,
    endpoint_id: endpoint.id,
    endpoint,
    api_family: endpoint.base_url.includes("anthropic") ? "anthropic" : "openai",
    is_active: true,
    priority: 0,
    name,
    auth_type: null,
    custom_headers: null,
    openai_text_capability: endpoint.base_url.includes("anthropic") ? null : openaiTextCapability,
    openai_probe_endpoint_variant: openaiProbeEndpointVariant,
    context_window_tokens: contextWindowTokens,
    default_output_token_reserve: defaultOutputTokenReserve,
    max_context_utilization: maxContextUtilization,
    preferred_context_utilization_threshold: preferredContextUtilizationThreshold,
    context_capability_overrides: contextCapabilityOverrides,
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

type ConnectionMutationPayload = {
  name?: string | null;
  is_active?: boolean;
  custom_headers?: Record<string, string> | null;
  openai_text_capability?: OpenAITextCapability | null;
  openai_probe_endpoint_variant?: OpenAIProbeEndpointVariant | null;
  pricing_template_id?: number | null;
  qps_limit?: number | null;
  max_in_flight_non_stream?: number | null;
  max_in_flight_stream?: number | null;
  context_window_tokens?: number | null;
  default_output_token_reserve?: number | null;
  max_context_utilization?: number | null;
  preferred_context_utilization_threshold?: number | null;
};

function applyConnectionPayload(
  connection: Connection,
  payload: ConnectionMutationPayload,
): Connection {
  const nextContextWindowTokens =
    payload.context_window_tokens === undefined
      ? connection.context_window_tokens
      : payload.context_window_tokens ?? connection.context_window_tokens;
  const nextDefaultOutputTokenReserve =
    payload.default_output_token_reserve === undefined
      ? connection.default_output_token_reserve
      : payload.default_output_token_reserve ?? connection.default_output_token_reserve;
  const nextMaxContextUtilization =
    payload.max_context_utilization === undefined
      ? connection.max_context_utilization
      : payload.max_context_utilization ?? connection.max_context_utilization;
  const nextPreferredContextUtilizationThreshold =
    payload.preferred_context_utilization_threshold === undefined
      ? connection.preferred_context_utilization_threshold
      : payload.preferred_context_utilization_threshold ?? connection.preferred_context_utilization_threshold;

  return {
    ...connection,
    ...payload,
    context_window_tokens: nextContextWindowTokens,
    default_output_token_reserve: nextDefaultOutputTokenReserve,
    max_context_utilization: nextMaxContextUtilization,
    preferred_context_utilization_threshold: nextPreferredContextUtilizationThreshold,
    context_capability_overrides: {
      context_window_tokens:
        payload.context_window_tokens === undefined
          ? connection.context_capability_overrides?.context_window_tokens ?? null
          : payload.context_window_tokens,
      default_output_token_reserve:
        payload.default_output_token_reserve === undefined
          ? connection.context_capability_overrides?.default_output_token_reserve ?? null
          : payload.default_output_token_reserve,
      max_context_utilization:
        payload.max_context_utilization === undefined
          ? connection.context_capability_overrides?.max_context_utilization ?? null
          : payload.max_context_utilization,
      preferred_context_utilization_threshold:
        payload.preferred_context_utilization_threshold === undefined
          ? connection.context_capability_overrides?.preferred_context_utilization_threshold ?? null
          : payload.preferred_context_utilization_threshold,
    },
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
  let accessTargets: TestAccessTarget[] = [...model.access_targets];

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
      const payload = request.postDataJSON() as ConnectionMutationPayload;
      savePayloads.push(payload);
      const connection = applyConnectionPayload(
        createConnection({
          id: 101,
          modelConfigId: model.id,
          endpoint,
          name: payload.name ?? endpoint.name,
          openaiProbeEndpointVariant: payload.openai_probe_endpoint_variant ?? null,
          openaiTextCapability: payload.openai_text_capability ?? null,
        }),
        payload,
      );
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
      const payload = request.postDataJSON() as ConnectionMutationPayload;
      savePayloads.push(payload);
      const currentConnection = accessTargets[0]?.connection;
      const updatedConnection = currentConnection
        ? applyConnectionPayload(currentConnection, payload)
        : currentConnection;
      accessTargets = accessTargets.map((target) =>
        target.connection_id === 301 && updatedConnection
          ? { ...target, connection: updatedConnection }
          : target,
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
  await page.getByRole("button", { name: "New terminal target" }).first().click();

  await expect(page.getByTestId("connection-dialog-main-grid")).toHaveAttribute("data-layout", "compact-flat");
  await expect(page.getByTestId("connection-dialog-openai-capability-section")).toBeVisible();
  await expect(page.getByTestId("connection-dialog-probe-section")).toBeVisible();
  await page.locator("#conn-selected-endpoint").click();
  await page.getByRole("option", { name: /OpenAI Primary/ }).click();
  await page.locator("#conn-openai-text-capability").click();
  await page.getByRole("option", { name: /Dual native/ }).click();
  await page.locator("#conn-probe-api").click();
  await page.getByRole("option", { name: /Chat Completions API/ }).click();
  await page.locator("#conn-probe-reasoning-mode").click();
  await page.getByRole("option", { name: /Disable reasoning/ }).click();

  await page.getByRole("button", { name: "Save Terminal Target" }).click();
  await expect.poll(() => savePayloads.length).toBe(1);
  expect(savePayloads[0]).toMatchObject({
    openai_text_capability: "dual_native",
    openai_probe_endpoint_variant: "chat_completions_reasoning_none",
  });
});

test("non-OpenAI connection dialog hides OpenAI capability and probe sections", async ({ page }) => {
  const model = createModelResponse({
    id: 2,
    apiFamily: "anthropic",
    modelId: "claude-sonnet",
    displayName: "Claude Sonnet",
  });
  await stubModelDetailRoutes(page, model);

  await page.goto("/models/2");
  await page.getByRole("button", { name: "New terminal target" }).first().click();

  await expect(page.getByTestId("connection-dialog-main-grid")).toHaveAttribute("data-layout", "compact-flat");
  await expect(page.getByTestId("connection-dialog-openai-capability-section")).toHaveCount(0);
  await expect(page.getByTestId("connection-dialog-probe-section")).toHaveCount(0);
});

test("editing an OpenAI connection hydrates the saved probe settings into both selectors", async ({ page }) => {
  const endpoint: Endpoint = {
    id: 11,
    profile_id: 1,
    name: "OpenAI Primary",
    base_url: "https://api.openai.com/v1",
    has_api_key: true,
    masked_api_key: "••••demo",
    position: 0,
    created_at: timestamp,
    updated_at: timestamp,
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
        name: "Saved Terminal Target",
        openaiProbeEndpointVariant: "chat_completions_reasoning_none",
        openaiTextCapability: "chat_completions_only",
      }),
    ],
  });
  await stubModelDetailRoutes(page, model);

  await page.goto("/models/3");
  await page.getByRole("button", { name: "Edit Saved Terminal Target" }).first().click();

  await expect(page.getByTestId("connection-dialog-main-grid")).toHaveAttribute("data-layout", "compact-flat");
  await expect(page.getByTestId("connection-dialog-openai-capability-section")).toBeVisible();
  await expect(page.getByTestId("connection-dialog-probe-section")).toBeVisible();
  await expect(page.locator("#conn-openai-text-capability")).toContainText("Chat Completions only");
  await expect(page.locator("#conn-probe-api")).toContainText("Chat Completions API");
  await expect(page.locator("#conn-probe-reasoning-mode")).toContainText("Disable reasoning");
});

test("terminal target capability editor submits mixed default and override payloads", async ({ page }) => {
  const model = createModelResponse({
    id: 4,
    apiFamily: "openai",
    modelId: "gpt-4.1-context-create",
    displayName: "GPT 4.1 Context Create",
  });
  const { savePayloads } = await stubModelDetailRoutes(page, model);

  await page.goto("/models/4");
  await page.getByRole("button", { name: "New terminal target" }).first().click();

  const contextWindowField = page.getByTestId("conn-context-window-tokens-field");
  await expect(contextWindowField).toContainText("Using default capability: Not set");
  await page.locator("#conn-selected-endpoint").click();
  await page.getByRole("option", { name: /OpenAI Primary/ }).click();

  await contextWindowField.getByRole("button", { name: "Override" }).click();
  await page.locator("#conn-context-window-tokens").fill("32768");
  await contextWindowField.getByRole("button", { name: "Reset to default" }).click();
  await expect(contextWindowField).toContainText("Using default capability: Not set");

  const reserveField = page.getByTestId("conn-default-output-token-reserve-field");
  await reserveField.getByRole("button", { name: "Override" }).click();
  await page.locator("#conn-default-output-token-reserve").fill("8192");

  const utilizationField = page.getByTestId("conn-max-context-utilization-field");
  await utilizationField.getByRole("button", { name: "Override" }).click();
  await page.locator("#conn-max-context-utilization").fill("0.75");

  const preferredField = page.getByTestId("conn-preferred-context-utilization-threshold-field");
  await expect(preferredField).toContainText("Using default capability: Not set");
  await preferredField.getByRole("button", { name: "Override" }).click();
  await page.locator("#conn-preferred-context-utilization-threshold").fill("0.6");

  await page.getByRole("button", { name: "Save Terminal Target" }).click();
  await expect.poll(() => savePayloads.length).toBe(1);
  expect(savePayloads[0]).toMatchObject({
    context_window_tokens: null,
    default_output_token_reserve: 8192,
    max_context_utilization: 0.75,
    preferred_context_utilization_threshold: 0.6,
  });
});

test("terminal target capability editor reopens same-as-default explicit overrides in override mode", async ({ page }) => {
  const endpoint: Endpoint = {
    id: 11,
    profile_id: 1,
    name: "OpenAI Primary",
    base_url: "https://api.openai.com/v1",
    has_api_key: true,
    masked_api_key: "••••demo",
    position: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const model = createModelResponse({
    id: 5,
    apiFamily: "openai",
    modelId: "gpt-4.1-context-edit",
    displayName: "GPT 4.1 Context Edit",
    connections: [
      createConnection({
        id: 301,
        modelConfigId: 5,
        endpoint,
        name: "Saved Terminal Target",
        openaiProbeEndpointVariant: "responses_minimal",
        contextWindowTokens: 16384,
        defaultOutputTokenReserve: 4096,
        maxContextUtilization: 0.9,
        preferredContextUtilizationThreshold: 0.7,
        contextCapabilityOverrides: {
          context_window_tokens: 16384,
          default_output_token_reserve: null,
          max_context_utilization: 0.9,
          preferred_context_utilization_threshold: 0.7,
        },
      }),
    ],
  });
  const { savePayloads } = await stubModelDetailRoutes(page, model);

  await page.goto("/models/5");
  await page.getByRole("button", { name: "Edit Saved Terminal Target" }).first().click();

  await expect(page.locator("#conn-context-window-tokens")).toHaveValue("16384");
  await expect(page.locator("#conn-max-context-utilization")).toHaveValue("0.9");
  await expect(page.locator("#conn-preferred-context-utilization-threshold")).toHaveValue("0.7");
  await expect(page.getByTestId("conn-context-window-tokens-field")).not.toContainText("Using default capability: 16384");
  await expect(page.getByTestId("conn-context-window-tokens-field")).toContainText("Reset to default");
  await expect(page.getByTestId("conn-preferred-context-utilization-threshold-field")).toContainText("Reset to default");

  await page.locator("#conn-name").fill("Saved Terminal Target Renamed");
  await page.getByRole("button", { name: "Save Terminal Target" }).click();
  await expect.poll(() => savePayloads.length).toBe(1);
  expect(savePayloads[0]).toMatchObject({
    context_window_tokens: 16384,
    default_output_token_reserve: null,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.7,
  });

  await page.getByRole("button", { name: "Edit Saved Terminal Target Renamed" }).first().click();
  await expect(page.locator("#conn-context-window-tokens")).toHaveValue("16384");
  await expect(page.locator("#conn-max-context-utilization")).toHaveValue("0.9");
  await expect(page.locator("#conn-preferred-context-utilization-threshold")).toHaveValue("0.7");
  await expect(page.getByTestId("conn-context-window-tokens-field")).toContainText("Reset to default");
  await expect(page.getByTestId("conn-preferred-context-utilization-threshold-field")).toContainText("Reset to default");
});


test("terminal target capability editor blocks preferred threshold above the effective max before save", async ({ page }) => {
  const model = createModelResponse({
    id: 6,
    apiFamily: "openai",
    modelId: "gpt-4.1-context-invalid",
    displayName: "GPT 4.1 Context Invalid",
  });
  const { savePayloads } = await stubModelDetailRoutes(page, model);

  await page.goto("/models/6");
  await page.getByRole("button", { name: "New terminal target" }).first().click();
  await page.locator("#conn-selected-endpoint").click();
  await page.getByRole("option", { name: /OpenAI Primary/ }).click();

  const utilizationField = page.getByTestId("conn-max-context-utilization-field");
  await utilizationField.getByRole("button", { name: "Override" }).click();
  await page.locator("#conn-max-context-utilization").fill("0.6");
  const preferredField = page.getByTestId("conn-preferred-context-utilization-threshold-field");
  await preferredField.getByRole("button", { name: "Override" }).click();
  await page.locator("#conn-preferred-context-utilization-threshold").fill("0.7");

  await page.getByRole("button", { name: "Save Terminal Target" }).click();
  await expect(page.getByText("Preferred context utilization threshold must be less than or equal to max context utilization.")).toBeVisible();
  expect(savePayloads).toHaveLength(0);
});
