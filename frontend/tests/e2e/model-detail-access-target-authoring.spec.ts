import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-28T12:00:00Z";
const modelConfigId = 50;

function createAccessTarget(targetModelId: string, position: number, displayName: string, isEnabled = true) {
  return {
    id: 700 + position,
    target_type: "model",
    target_model_id: targetModelId,
    connection_id: null,
    position,
    is_enabled: isEnabled,
    target_model: {
      id: 100 + position,
      profile_id: 1,
      api_family: "openai",
      model_id: targetModelId,
      display_name: displayName,
      loadbalance_strategy_id: null,
      is_enabled: true,
    },
    connection: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createEndpoint(id = 21) {
  return {
    id,
    profile_id: 1,
    name: "Reusable OpenAI Endpoint",
    base_url: "https://api.openai.test/v1",
    has_api_key: true,
    masked_api_key: "sk-...test",
    position: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createConnection(id: number, ownerModelConfigId: number, endpoint: ReturnType<typeof createEndpoint>, name: string, priority: number) {
  return {
    id,
    profile_id: 1,
    model_config_id: ownerModelConfigId,
    api_family: "openai",
    endpoint_id: endpoint.id,
    endpoint,
    is_active: true,
    priority,
    name,
    auth_type: null,
    custom_headers: null,
    openai_probe_endpoint_variant: "responses_minimal",
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

function createConnectionAccessTarget(connection: ReturnType<typeof createConnection>, position: number, isEnabled = true) {
  return {
    id: 800 + position,
    target_type: "connection",
    target_model_id: null,
    connection_id: connection.id,
    position,
    is_enabled: isEnabled,
    target_model: null,
    connection,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelListItem(
  id: number,
  modelId: string,
  displayName: string,
  apiFamily: "openai" | "anthropic",
  accessTargets: Array<ReturnType<typeof createAccessTarget> | ReturnType<typeof createConnectionAccessTarget>> = [],
  isEnabled = true,
) {
  return {
    id,
    profile_id: 1,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    loadbalance_strategy_id: 11,
    loadbalance_strategy: {
      id: 11,
      name: "Default fill-first routing",
      legacy_strategy_type: "fill-first",
      failure_status_codes: [429, 500],
      ban_mode: "off",
      retry_base_delay_ms: 1000,
      retry_backoff_multiplier: 2,
      retry_jitter_ratio: 0.2,
      retry_max_delay_ms: 8000,
      cycle_retry_attempt_limit: 3,
      ban_cumulative_retry_attempt_threshold: 0,
      ban_duration_seconds: 0,
    },
    access_targets: accessTargets,
    is_enabled: isEnabled,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createAccessTargetModelDetail(
  accessTargets: Array<ReturnType<typeof createAccessTarget> | ReturnType<typeof createConnectionAccessTarget>>,
  isEnabled = true,
) {
  return createModelListItem(modelConfigId, "routed-openai", "Routed OpenAI", "openai", accessTargets, isEnabled);
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

async function mockModelDetailRoutes(page: Page) {
  const updatePayloads: unknown[] = [];
  let currentAccessTargets = [createAccessTarget("target-alpha", 0, "Target Alpha")];
  let currentModelEnabled = true;

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/endpoints") return fulfillJson([]);
    if (pathname === `/api/models/${modelConfigId}/connections`) return fulfillJson([]);
    if (pathname === "/api/connections") return fulfillJson([]);
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([
      {
        id: 11,
        profile_id: 1,
        name: "Default fill-first routing",
        legacy_strategy_type: "fill-first",
        failure_status_codes: [429, 500],
        ban_mode: "off",
        retry_base_delay_ms: 1000,
        retry_backoff_multiplier: 2,
        retry_jitter_ratio: 0.2,
        retry_max_delay_ms: 8000,
        cycle_retry_attempt_limit: 3,
        ban_cumulative_retry_attempt_threshold: 0,
        ban_duration_seconds: 0,
        attached_model_count: 1,
        created_at: timestamp,
        updated_at: timestamp,
      },
    ]);
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingResponse());

    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([
        createModelListItem(modelConfigId, "routed-openai", "Routed OpenAI", "openai", currentAccessTargets, currentModelEnabled),
        createModelListItem(1, "target-alpha", "Target Alpha", "openai"),
        createModelListItem(2, "target-beta", "Target Beta", "openai"),
        createModelListItem(3, "shadow-openai", "Shadow OpenAI", "openai", [createAccessTarget("target-alpha", 0, "Target Alpha")]),
        createModelListItem(4, "claude-sonnet", "Claude Sonnet", "anthropic"),
      ]);
    }

    if (pathname === `/api/models/${modelConfigId}` && request.method() === "GET") {
      return fulfillJson(createAccessTargetModelDetail(currentAccessTargets, currentModelEnabled));
    }

    if (pathname === `/api/models/${modelConfigId}` && request.method() === "PUT") {
      const payload = request.postDataJSON() as {
        api_family: "openai" | "anthropic";
        model_id: string;
        display_name: string | null;
        access_targets: Array<{ target_type: "model"; target_model_id: string; position: number; is_enabled?: boolean }>;
        loadbalance_strategy_id: number;
        is_enabled: boolean;
      };
      updatePayloads.push(payload);
      currentModelEnabled = payload.is_enabled;
      currentAccessTargets = (payload.access_targets ?? []).map((target) =>
        createAccessTarget(
          target.target_model_id,
          target.position,
          target.target_model_id === "target-alpha" ? "Target Alpha" : "Target Beta",
          target.is_enabled ?? true,
        ),
      );
      return fulfillJson({
        ...createAccessTargetModelDetail(currentAccessTargets, currentModelEnabled),
        api_family: payload.api_family,
        model_id: payload.model_id,
        display_name: payload.display_name,
        loadbalance_strategy_id: payload.loadbalance_strategy_id,
        is_enabled: payload.is_enabled,
      });
    }

    return fulfillJson({ error: `Unhandled ${request.method()} ${pathname}` }, 500);
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getUpdatePayloads: () => updatePayloads,
  };
}

test("model detail editing supports disabled targetless drafts and later enabled attachment", async ({ page }) => {
  const routes = await mockModelDetailRoutes(page);

  await page.goto(`/models/${modelConfigId}`);
  await expect(page.getByRole("heading", { name: "Routed OpenAI" })).toBeVisible();
  await expect(page.getByText("Access targets").first()).toBeVisible();
  await expect(page.getByTestId("access-targets-editor").getByText("Target Alpha")).toBeVisible();
  await expect(page.getByRole("button", { name: "Add target" })).toHaveCount(1);

  await page.getByRole("button", { name: /edit model/i }).click();

  const dialog = page.getByRole("dialog", { name: "Model Settings" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "New terminal target" })).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: "Add target" })).toHaveCount(1);

  await dialog.getByRole("button", { name: "Remove target 1" }).click();
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Add at least one enabled same-family access target before saving an enabled model.").last()).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);

  const enabledSwitch = dialog.getByRole("switch", { name: "Enabled" });
  await enabledSwitch.click();
  await expect(enabledSwitch).toHaveAttribute("data-state", "unchecked");
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Model updated").last()).toBeVisible();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText("Needs target")).toBeVisible();

  expect(routes.getUpdatePayloads()).toEqual([
    {
      api_family: "openai",
      model_id: "routed-openai",
      display_name: "Routed OpenAI",
      openai_accepted_format: "dual_native",
      access_targets: [],
      loadbalance_strategy_id: 11,
      is_enabled: false,
    },
  ]);

  await page.getByRole("button", { name: /edit model/i }).click();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "New terminal target" })).toHaveCount(0);
  await dialog.locator("#access-target-select").click();
  await expect(page.getByRole("option", { name: /connection|standalone/i })).toHaveCount(0);
  await expect(page.getByRole("option", { name: /Target Alpha/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Target Beta/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Shadow OpenAI/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Claude Sonnet/ })).toHaveCount(0);
  await page.getByRole("option", { name: /Target Alpha/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();
  await expect(dialog.getByTestId("access-target-model:target-alpha").getByText("Target Alpha")).toBeVisible();

  await dialog.getByRole("switch", { name: "Enabled" }).click();
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Model updated").last()).toBeVisible();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText("Needs target")).toHaveCount(0);

  expect(routes.getUpdatePayloads()).toEqual([
    {
      api_family: "openai",
      model_id: "routed-openai",
      display_name: "Routed OpenAI",
      openai_accepted_format: "dual_native",
      access_targets: [],
      loadbalance_strategy_id: 11,
      is_enabled: false,
    },
    {
      api_family: "openai",
      model_id: "routed-openai",
      display_name: "Routed OpenAI",
      openai_accepted_format: "dual_native",
      access_targets: [
        { target_type: "model", target_model_id: "target-alpha", position: 0, is_enabled: true },
      ],
      loadbalance_strategy_id: 11,
      is_enabled: true,
    },
  ]);
  const savedTarget = (routes.getUpdatePayloads()[1] as { access_targets: Array<Record<string, unknown>> }).access_targets[0];
  expect(Object.prototype.hasOwnProperty.call(savedTarget, "weight")).toBe(false);
  expect(Object.prototype.hasOwnProperty.call(savedTarget, "target_priority")).toBe(false);

  const accessTargetsEditor = page.getByTestId("access-targets-editor");
  await expect(page.getByRole("button", { name: "Add target" })).toHaveCount(1);
  await expect(accessTargetsEditor.getByText("Target Alpha")).toBeVisible();
  await expect(accessTargetsEditor.getByText("Position 1").first()).toBeVisible();
  await expect(accessTargetsEditor.getByText("Priority 1")).toHaveCount(0);
  await expect(accessTargetsEditor.getByText("Tier")).toHaveCount(0);
  await expect(accessTargetsEditor.getByText("Weight")).toHaveCount(0);
});

async function mockPrivateConnectionRoutes(page: Page) {
  const endpoint = createEndpoint();
  const peerModelConfigId = 99;
  let nextConnectionId = 303;
  let ownerConnections = [
    createConnection(301, modelConfigId, endpoint, "Owned primary", 0),
    createConnection(302, modelConfigId, endpoint, "Owned secondary", 1),
  ];
  const peerConnection = createConnection(401, peerModelConfigId, endpoint, "Peer private", 2);
  let currentAccessTargets = [
    createConnectionAccessTarget(ownerConnections[0], 0),
    createConnectionAccessTarget(ownerConnections[1], 1),
    createConnectionAccessTarget(peerConnection, 2),
  ];
  const requests = {
    creates: [] as unknown[],
    updates: [] as unknown[],
    targetPatches: [] as unknown[],
    targetDeletes: [] as string[],
    healthChecks: [] as string[],
    publicMutations: [] as string[],
  };

  const sortedTargets = () => [...currentAccessTargets].sort((left, right) => left.position - right.position);

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    const method = request.method();

    if (!pathname.startsWith("/api/")) return route.continue();

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/endpoints") return fulfillJson([endpoint]);
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([]);
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingResponse());

    if (pathname === "/api/models" && method === "GET") {
      return fulfillJson([
        createModelListItem(modelConfigId, "routed-openai", "Routed OpenAI", "openai", sortedTargets()),
        createModelListItem(peerModelConfigId, "peer-openai", "Peer OpenAI", "openai", [createConnectionAccessTarget(peerConnection, 0)]),
      ]);
    }
    if (pathname === `/api/models/${modelConfigId}` && method === "GET") {
      return fulfillJson(createAccessTargetModelDetail(sortedTargets()));
    }
    if (pathname === `/api/models/${modelConfigId}/connections` && method === "GET") {
      return fulfillJson(ownerConnections);
    }
    if (pathname === `/api/models/${modelConfigId}/connections` && method === "POST") {
      const payload = request.postDataJSON() as { endpoint_id?: number; name?: string | null };
      requests.creates.push({ path: pathname, payload });
      const connection = createConnection(nextConnectionId++, modelConfigId, endpoint, payload.name ?? "Created private", ownerConnections.length);
      ownerConnections = [...ownerConnections, connection];
      currentAccessTargets = [...currentAccessTargets, createConnectionAccessTarget(connection, currentAccessTargets.length)];
      return fulfillJson(connection);
    }

    const connectionMatch = pathname.match(new RegExp(`^/api/models/${modelConfigId}/connections/(\\d+)$`));
    if (connectionMatch && method === "PATCH") {
      const connectionId = Number.parseInt(connectionMatch[1], 10);
      const payload = request.postDataJSON() as { name?: string; is_active?: boolean };
      requests.updates.push({ path: pathname, payload });
      ownerConnections = ownerConnections.map((connection) =>
        connection.id === connectionId ? { ...connection, ...payload, updated_at: timestamp } : connection,
      );
      currentAccessTargets = currentAccessTargets.map((target) =>
        target.connection_id === connectionId
          ? { ...target, connection: ownerConnections.find((connection) => connection.id === connectionId) ?? target.connection }
          : target,
      );
      return fulfillJson(ownerConnections.find((connection) => connection.id === connectionId));
    }
    if (pathname.match(new RegExp(`^/api/models/${modelConfigId}/connections/\\d+/health$`)) && method === "POST") {
      requests.healthChecks.push(pathname);
      return fulfillJson({ connection_id: 301, health_status: "healthy", checked_at: timestamp, detail: "Owner ok", response_time_ms: 42 });
    }

    if (pathname === `/api/models/${modelConfigId}/targets` && method === "GET") return fulfillJson(sortedTargets());
    const targetPositionMatch = pathname.match(new RegExp(`^/api/models/${modelConfigId}/targets/(\\d+)/position$`));
    if (targetPositionMatch && method === "PATCH") {
      const targetId = Number.parseInt(targetPositionMatch[1], 10);
      const payload = request.postDataJSON() as { to_index?: number };
      requests.targetPatches.push({ path: pathname, payload });
      if (typeof payload.to_index === "number") {
        const moved = currentAccessTargets.find((target) => target.id === targetId);
        currentAccessTargets = currentAccessTargets.filter((target) => target.id !== targetId);
        if (moved) currentAccessTargets.splice(payload.to_index, 0, moved);
        currentAccessTargets = currentAccessTargets.map((target, position) => ({ ...target, position }));
      }
      return fulfillJson(sortedTargets());
    }
    const targetMatch = pathname.match(new RegExp(`^/api/models/${modelConfigId}/targets/(\\d+)$`));
    if (targetMatch && method === "PATCH") {
      const targetId = Number.parseInt(targetMatch[1], 10);
      const payload = request.postDataJSON() as { is_enabled?: boolean };
      requests.targetPatches.push({ path: pathname, payload });
      currentAccessTargets = currentAccessTargets.map((target) =>
        target.id === targetId ? { ...target, ...payload, updated_at: timestamp } : target,
      );
      return fulfillJson(sortedTargets());
    }

    if (targetMatch && method === "DELETE") {
      requests.targetDeletes.push(pathname);
      const targetId = Number.parseInt(targetMatch[1], 10);
      const connectionId = currentAccessTargets.find((target) => target.id === targetId)?.connection_id;
      currentAccessTargets = currentAccessTargets.filter((target) => target.id !== targetId)
        .map((target, position) => ({ ...target, position }));
      ownerConnections = ownerConnections.filter((connection) => connection.id !== connectionId);
      return fulfillJson(sortedTargets());
    }
    if (pathname.startsWith("/api/connections") && method !== "GET") {
      requests.publicMutations.push(`${method} ${pathname}`);
      return fulfillJson({ error: "public mutation route should not be used" }, 500);
    }
    if (pathname === "/api/connections" && method === "GET") return fulfillJson([]);

    return fulfillJson({ error: `Unhandled ${method} ${pathname}` }, 500);
  });

  return { endpoint, requests };
}

test("private connection owner flows use model-scoped routes and hide cross-owner controls", async ({ page }) => {
  const { endpoint, requests } = await mockPrivateConnectionRoutes(page);

  await page.goto(`/models/${modelConfigId}`);
  await expect(page.getByRole("heading", { name: "Routed OpenAI" })).toBeVisible();

  const editor = page.getByTestId("access-targets-editor").first();
  await expect(editor.getByText("Owned primary")).toBeVisible();
  await expect(editor.getByText("Owned secondary")).toBeVisible();
  await expect(editor.getByText("Terminal target 401")).toBeVisible();
  await expect(editor.getByRole("button", { name: /Health Check Owned primary/ })).toBeVisible();
  await expect(editor.getByRole("button", { name: /Health Check Terminal target 401/ })).toHaveCount(0);
  await expect(editor.getByRole("button", { name: /Edit Terminal target 401/ })).toHaveCount(0);
  await expect(editor.getByRole("switch", { name: "Enable access target 3" })).toHaveCount(0);

  await editor.getByRole("button", { name: "New terminal target" }).click();
  await page.locator("#conn-selected-endpoint").click();
  await page.getByRole("option", { name: /Reusable OpenAI Endpoint/ }).click();
  await page.locator("#conn-name").fill("Owner-created terminal target");
  await page.getByRole("button", { name: "Save Terminal Target" }).click();
  await expect.poll(() => requests.creates.length).toBe(1);
  expect(requests.creates[0]).toMatchObject({
    path: `/api/models/${modelConfigId}/connections`,
    payload: { endpoint_id: endpoint.id, name: "Owner-created terminal target" },
  });

  await expect(editor.getByText("Owner-created terminal target")).toBeVisible();

  await editor.getByRole("button", { name: /Edit Owned primary/ }).click();
  await page.locator("#conn-name").fill("Owner renamed");
  await page.getByRole("button", { name: "Save Terminal Target" }).click();
  await expect.poll(() => requests.updates.length).toBe(1);
  expect(requests.updates[0]).toMatchObject({
    path: `/api/models/${modelConfigId}/connections/301`,
    payload: { endpoint_id: endpoint.id, name: "Owner renamed" },
  });
  await expect(editor.getByText("Owner renamed")).toBeVisible();

  await editor.getByRole("button", { name: /Health Check Owner renamed/ }).click();
  await expect.poll(() => requests.healthChecks.length).toBe(1);
  expect(requests.healthChecks[0]).toBe(`/api/models/${modelConfigId}/connections/301/health`);

  await editor.getByRole("switch", { name: "Enable access target 1" }).click();
  await expect.poll(() => requests.targetPatches.length).toBe(1);
  expect(requests.targetPatches[0]).toMatchObject({
    path: `/api/models/${modelConfigId}/targets/800`,
    payload: { is_enabled: false },
  });

  await editor.getByRole("button", { name: "Move target 1 down" }).click();
  await expect.poll(() => requests.targetPatches.length).toBe(2);
  expect(requests.targetPatches[1]).toMatchObject({
    path: `/api/models/${modelConfigId}/targets/800/position`,
    payload: { to_index: 1 },
  });
  const movedOwnerTarget = editor.getByTestId("access-target-connection:301");
  await expect(movedOwnerTarget.getByText("Priority 2")).toBeVisible();

  await movedOwnerTarget.getByRole("button", { name: "Remove target 2" }).click();
  await expect.poll(() => requests.targetDeletes.length).toBe(1);
  expect(requests.targetDeletes[0]).toBe(`/api/models/${modelConfigId}/targets/800`);
  expect(requests.publicMutations).toEqual([]);
});
