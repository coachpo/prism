import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T12:00:00Z";
const newModelButton = /New Model|新建模型/;
const newModelDialog = /New Model|新建模型/;
const editModelDialog = /Edit Model|编辑模型/;
const saveButton = /Save|保存/;
const cancelButton = /Cancel|取消/;
const createDefaultsButton = /Create Defaults|创建默认策略/;
const modelIdLabel = /Model ID|模型 ID/;
const displayNameLabel = /Display Name|显示名称/;
const enabledSwitchLabel = /Enabled|启用/;
const noStrategiesCopy =
  /No loadbalance strategies are available\. Create one on the Loadbalance Strategies page first\.|没有可用的负载均衡策略。请先在负载均衡策略页面创建一个。/;
const modelCreatedToast = /Model created|模型已创建/;
const defaultStrategiesCreatedToast = /Default loadbalance strategies created|默认负载均衡策略已创建/;

function createStrategy() {
  return {
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
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelListItem(
  id: number,
  modelId: string,
  displayName: string,
  apiFamily: "openai" | "anthropic" | "gemini" = "openai",
  loadbalanceStrategyId: number | null = 11,
  loadbalanceStrategy: ReturnType<typeof createStrategy> | null = createStrategy(),
) {
  return {
    id,
    profile_id: 1,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    loadbalance_strategy_id: loadbalanceStrategyId,
    loadbalance_strategy: loadbalanceStrategy,
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

function createDeferredDefaultsResponse() {
  let resolveReady: (() => void) | null = null;
  const ready = new Promise<void>((resolve) => {
    resolveReady = resolve;
  });
  return {
    ready,
    resolve: () => {
      resolveReady?.();
    },
  };
}

async function mockModelRoutes(
  page: Page,
  options: {
    defaultsDelay?: Promise<void>;
    defaultsResponse?: unknown;
    defaultsStatus?: number;
    models?: ReturnType<typeof createModelListItem>[];
    strategies?: ReturnType<typeof createStrategy>[];
  } = {},
) {
  const strategies = options.strategies ?? [createStrategy()];
  const models = options.models ?? [
    createModelListItem(1, "target-alpha", "Target Alpha", "openai"),
    createModelListItem(2, "target-beta", "Target Beta", "openai"),
    createModelListItem(3, "claude-sonnet", "Claude Sonnet", "anthropic"),
  ];
  const createdPayloads: unknown[] = [];
  const defaultsRequests: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson(models);
    }
    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson(strategies);
    }
    if (pathname === "/api/loadbalance/strategies/defaults" && request.method() === "POST") {
      defaultsRequests.push(pathname);
      await options.defaultsDelay;
      return fulfillJson(
        options.defaultsResponse ?? {
          items: [createStrategy()],
          created_count: 1,
          created_names: ["Default fill-first routing"],
          existing_names: [],
        },
        options.defaultsStatus ?? 200,
      );
    }
    if (pathname === "/api/endpoints/connections") {
      return fulfillJson({ items: [] });
    }
    if (pathname === "/api/connections") {
      return fulfillJson([]);
    }
    if (pathname === "/api/stats/models/metrics") {
      return fulfillJson({ items: [] });
    }
    if (pathname === "/api/models" && request.method() === "POST") {
      const payload = request.postDataJSON();
      createdPayloads.push(payload);
      return fulfillJson({
        id: 50,
        profile_id: 1,
        api_family: payload.api_family,
        model_id: payload.model_id,
        display_name: payload.display_name,
        loadbalance_strategy_id: payload.loadbalance_strategy_id ?? null,
        loadbalance_strategy: createStrategy(),
        access_targets: payload.access_targets ?? [],
        is_enabled: payload.is_enabled ?? true,
        connections: [],
        created_at: timestamp,
        updated_at: timestamp,
      });
    }

    return fulfillJson({});
  });

  return {
    getCreatedPayloads: () => createdPayloads,
    getDefaultsRequests: () => defaultsRequests,
  };
}

test("main model dialog disables save when no loadbalance strategies exist", async ({ page }) => {
  const routes = await mockModelRoutes(page, { strategies: [] });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const saveModelButton = dialog.getByRole("button", { name: saveButton });

  await expect(dialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(saveModelButton).toBeDisabled();

  await page.getByRole("textbox", { name: modelIdLabel }).fill("zero-strategy-model");
  await page.getByRole("textbox", { name: displayNameLabel }).fill("Zero Strategy Model");
  await expect(saveModelButton).toBeDisabled();

  await page.getByRole("textbox", { name: displayNameLabel }).press("Enter");
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.getByRole("button", { name: cancelButton }).click();
  await page.getByRole("button", { name: /Edit Model: Target Alpha|编辑模型: Target Alpha/ }).click();

  const editDialog = page.getByRole("dialog", { name: editModelDialog });
  await expect(editDialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(editDialog.getByRole("button", { name: createDefaultsButton })).toHaveCount(0);
});

test("main model dialog creates default strategy from empty loadbalance strategy state", async ({ page }) => {
  const routes = await mockModelRoutes(page, { strategies: [] });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const saveModelButton = dialog.getByRole("button", { name: saveButton });
  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toBeVisible();
  await expect(saveModelButton).toBeDisabled();

  await dialog.getByRole("button", { name: createDefaultsButton }).click();

  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toHaveCount(0);
  await expect(dialog.locator("#model-loadbalance-strategy")).toContainText("Default fill-first routing");
  await expect(saveModelButton).toBeEnabled();

  await page.getByRole("textbox", { name: modelIdLabel }).fill("defaults-created-model");
  await saveModelButton.click();
  await expect(page.getByText(modelCreatedToast)).toBeVisible();

  expect(routes.getDefaultsRequests()).toHaveLength(1);
  expect(routes.getCreatedPayloads()).toEqual([
    {
      api_family: "openai",
      model_id: "defaults-created-model",
      display_name: "defaults-created-model",
      openai_accepted_format: "dual_native",
      loadbalance_strategy_id: 11,
      is_enabled: false,
    },
  ]);
});

test("main model dialog keeps empty strategy state when default creation fails", async ({ page }) => {
  const routes = await mockModelRoutes(page, {
    strategies: [],
    defaultsStatus: 500,
    defaultsResponse: { detail: { message: "Default creation failed" } },
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const saveModelButton = dialog.getByRole("button", { name: saveButton });
  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toBeVisible();
  await expect(saveModelButton).toBeDisabled();

  await dialog.getByRole("button", { name: createDefaultsButton }).click();

  await expect(page.getByText("Default creation failed")).toBeVisible();
  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toBeVisible();
  await expect(dialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(saveModelButton).toBeDisabled();

  await page.getByRole("textbox", { name: modelIdLabel }).fill("defaults-failed-model");
  await page.getByRole("textbox", { name: displayNameLabel }).press("Enter");

  expect(routes.getDefaultsRequests()).toHaveLength(1);
  expect(routes.getCreatedPayloads()).toHaveLength(0);
});

test("main model dialog does not apply delayed defaults response to edit dialog", async ({ page }) => {
  const deferredDefaults = createDeferredDefaultsResponse();
  const routes = await mockModelRoutes(page, {
    strategies: [],
    defaultsDelay: deferredDefaults.ready,
    models: [createModelListItem(1, "edit-no-strategy", "Edit No Strategy", "openai", null, null)],
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const createDialog = page.getByRole("dialog", { name: newModelDialog });
  await createDialog.getByRole("button", { name: createDefaultsButton }).click();
  expect(routes.getDefaultsRequests()).toHaveLength(1);
  await createDialog.getByRole("button", { name: cancelButton }).click();

  await page.getByRole("button", { name: /Edit Model: Edit No Strategy|编辑模型: Edit No Strategy/ }).click();
  const editDialog = page.getByRole("dialog", { name: editModelDialog });
  await expect(editDialog.getByText(noStrategiesCopy)).toBeVisible();

  deferredDefaults.resolve();

  await expect(page.getByText(defaultStrategiesCreatedToast)).toBeVisible();
  await expect(editDialog.getByRole("button", { name: createDefaultsButton })).toHaveCount(0);
  await expect(editDialog.locator("#model-loadbalance-strategy")).not.toContainText("Default fill-first routing");
});

test("main model dialog keeps save disabled when stale defaults response reaches reopened create dialog", async ({ page }) => {
  const deferredDefaults = createDeferredDefaultsResponse();
  const routes = await mockModelRoutes(page, {
    strategies: [],
    defaultsDelay: deferredDefaults.ready,
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const createDialog = page.getByRole("dialog", { name: newModelDialog });
  await createDialog.getByRole("button", { name: createDefaultsButton }).click();
  expect(routes.getDefaultsRequests()).toHaveLength(1);
  await createDialog.getByRole("button", { name: cancelButton }).click();

  await page.getByRole("button", { name: newModelButton }).click();
  const reopenedDialog = page.getByRole("dialog", { name: newModelDialog });
  const saveModelButton = reopenedDialog.getByRole("button", { name: saveButton });
  await expect(saveModelButton).toBeDisabled();

  deferredDefaults.resolve();

  await expect(page.getByText(defaultStrategiesCreatedToast)).toBeVisible();
  await expect(reopenedDialog.locator("#model-loadbalance-strategy")).not.toContainText("Default fill-first routing");
  await expect(saveModelButton).toBeDisabled();
});

test("main model dialog saves targetless disabled drafts", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  await expect(dialog.getByRole("switch", { name: enabledSwitchLabel })).toHaveAttribute("data-state", "unchecked");

  await page.getByRole("textbox", { name: modelIdLabel }).fill("draft-openai");
  await dialog.getByRole("button", { name: saveButton }).click();
  await expect(page.getByText(modelCreatedToast)).toBeVisible();

  expect(routes.getCreatedPayloads()).toEqual([
    {
      api_family: "openai",
      model_id: "draft-openai",
      display_name: "draft-openai",
      openai_accepted_format: "dual_native",
      loadbalance_strategy_id: 11,
      is_enabled: false,
    },
  ]);

  const draftRow = page.getByRole("row", { name: /draft-openai/ });
  await expect(draftRow.getByText(/Needs target|需要目标/)).toBeVisible();
  await expect(draftRow.getByText(/Disabled|已禁用/)).toBeVisible();
});

test("main model dialog keeps access-target authoring out of the create flow", async ({ page }) => {
  await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  await expect(dialog.locator("#access-target-select")).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: /New terminal target|新建终端目标/ })).toHaveCount(0);
  await expect(dialog.getByText("Tier")).toHaveCount(0);
  await expect(dialog.getByText("Weight")).toHaveCount(0);
  await expect(dialog.getByRole("switch", { name: enabledSwitchLabel })).toHaveAttribute("data-state", "unchecked");
});

test("model detail exposes one mixed access-target list with cross-type reorder by target row id", async ({ page }) => {
  const timestamp = "2026-08-08T12:00:00Z";
  // ID domains stay distinct: target rows 511-513, connections 91-92,
  // target model id "child-model".
  const connectionOne = {
    id: 91,
    profile_id: 1,
    model_config_id: 7,
    api_family: "openai",
    endpoint_id: 21,
    endpoint: { id: 21, name: "endpoint-one", base_url: "https://one.example", has_api_key: true, masked_api_key: null, position: 0, created_at: timestamp, updated_at: timestamp },
    is_active: true,
    priority: 0,
    name: "Terminal One",
    auth_type: null,
    custom_headers: null,
    openai_text_capability: "dual_native",
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const connectionTwo = { ...connectionOne, id: 92, endpoint_id: 22, endpoint: { ...connectionOne.endpoint, id: 22, name: "endpoint-two", base_url: "https://two.example" }, name: "Terminal Two", priority: 2 };
  const childModel = {
    id: 8,
    profile_id: 1,
    api_family: "openai",
    model_id: "child-model",
    display_name: "Child Model",
    openai_accepted_format: "dual_native",
    loadbalance_strategy_id: 11,
    is_enabled: true,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const modelTarget = {
    id: 511,
    target_type: "model",
    target_model_id: "child-model",
    connection_id: null,
    terminal_target_id: null,
    position: 1,
    is_enabled: true,
    target_model: childModel,
    connection: null,
    terminal_target: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const terminalTargetOne = {
    id: 512,
    target_type: "connection",
    target_model_id: null,
    connection_id: 91,
    terminal_target_id: 91,
    position: 0,
    is_enabled: true,
    target_model: null,
    connection: connectionOne,
    terminal_target: connectionOne,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const terminalTargetTwo = {
    id: 513,
    target_type: "connection",
    target_model_id: null,
    connection_id: 92,
    terminal_target_id: 92,
    position: 2,
    is_enabled: true,
    target_model: null,
    connection: connectionTwo,
    terminal_target: connectionTwo,
    created_at: timestamp,
    updated_at: timestamp,
  };
  let targets: unknown[] = [terminalTargetOne, modelTarget, terminalTargetTwo];
  const moveRequests: Array<{ url: string; body: unknown }> = [];
  const toggleRequests: Array<{ url: string; body: unknown }> = [];
  const deleteRequests: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }
    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], timezone_preference: null });
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([createStrategy()]);
    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([
        {
          id: 7,
          profile_id: 1,
          api_family: "openai",
          model_id: "router-mixed",
          display_name: "Router Mixed",
          openai_accepted_format: "dual_native",
          loadbalance_strategy_id: 11,
          loadbalance_strategy: createStrategy(),
          access_targets: targets,
          is_enabled: true,
          connection_count: 2,
          active_connection_count: 2,
          health_success_rate: null,
          health_total_requests: 0,
          created_at: timestamp,
          updated_at: timestamp,
        },
        {
          id: 8,
          profile_id: 1,
          api_family: "openai",
          model_id: "child-model",
          display_name: "Child Model",
          openai_accepted_format: "dual_native",
          loadbalance_strategy_id: 11,
          loadbalance_strategy: createStrategy(),
          access_targets: [],
          is_enabled: true,
          connection_count: 0,
          active_connection_count: 0,
          health_success_rate: null,
          health_total_requests: 0,
          created_at: timestamp,
          updated_at: timestamp,
        },
      ]);
    }
    if (pathname === "/api/models/7" && request.method() === "GET") {
      return fulfillJson({
        id: 7,
        profile_id: 1,
        api_family: "openai",
        model_id: "router-mixed",
        display_name: "Router Mixed",
        openai_accepted_format: "dual_native",
        loadbalance_strategy_id: 11,
        loadbalance_strategy: createStrategy(),
        access_targets: targets,
        is_enabled: true,
        connection_count: 2,
        active_connection_count: 2,
        health_success_rate: null,
        health_total_requests: 0,
        created_at: timestamp,
        updated_at: timestamp,
      });
    }
    if (pathname === "/api/models/7/connections") return fulfillJson([connectionOne, connectionTwo]);
    if (pathname === "/api/models/7/targets" && request.method() === "GET") return fulfillJson(targets);
    const positionMatch = pathname.match(/^\/api\/models\/7\/targets\/(\d+)\/position$/);
    if (positionMatch && request.method() === "PATCH") {
      const body = request.postDataJSON();
      moveRequests.push({ url: pathname, body });
      const targetId = Number(positionMatch[1]);
      const current = [...targets] as Array<{ id: number; position: number }>;
      const moved = current.find((item) => item.id === targetId);
      if (!moved) return fulfillJson({}, 404);
      const toIndex = body.to_index as number;
      const rest = current.filter((item) => item.id !== targetId);
      rest.splice(toIndex, 0, moved);
      targets = rest.map((item, index) => ({ ...item, position: index }));
      return fulfillJson(targets);
    }
    const targetMatch = pathname.match(/^\/api\/models\/7\/targets\/(\d+)$/);
    if (targetMatch && request.method() === "PATCH") {
      const body = request.postDataJSON();
      toggleRequests.push({ url: pathname, body });
      const targetId = Number(targetMatch[1]);
      targets = (targets as Array<Record<string, unknown>>).map((item) =>
        item.id === targetId ? { ...item, is_enabled: body.is_enabled ?? item.is_enabled } : item,
      );
      return fulfillJson(targets);
    }
    if (targetMatch && request.method() === "DELETE") {
      deleteRequests.push(pathname);
      const targetId = Number(targetMatch[1]);
      targets = (targets as Array<{ id: number }>)
        .filter((item) => item.id !== targetId)
        .map((item, index) => ({ ...item, position: index }));
      return fulfillJson(targets);
    }
    if (pathname === "/api/endpoints") return fulfillJson([]);
    if (pathname === "/api/endpoints/connections") return fulfillJson({ items: [] });
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname.startsWith("/api/stats/spending")) {
      return fulfillJson({ summary: null, report_currency_code: "EUR", report_currency_symbol: "€" });
    }
    return fulfillJson({});
  });

  await page.goto("/models");
  await page.getByRole("button", { name: /View details: Router Mixed|查看模型 Router Mixed 的详情/ }).click();
  await expect(page).toHaveURL(/\/models\/7/);

  const editor = page.getByTestId("access-targets-mixed-list");
  const rowOrder = () => editor.locator("div[data-testid^='access-target-']");
  await expect(rowOrder()).toHaveCount(3);
  await expect(rowOrder().nth(0)).toHaveAttribute("data-testid", "access-target-512");
  await expect(rowOrder().nth(1)).toHaveAttribute("data-testid", "access-target-511");
  await expect(rowOrder().nth(2)).toHaveAttribute("data-testid", "access-target-513");
  await expect(rowOrder().nth(0)).toContainText(/Terminal One/);
  await expect(rowOrder().nth(1)).toContainText(/Child Model/);
  await expect(rowOrder().nth(0)).toContainText(/位置 1/);
  await expect(rowOrder().nth(2)).toContainText(/位置 3/);

  // Cross-type move: model row 511 up across terminal row 512.
  await rowOrder().nth(1).getByRole("button", { name: /将目标 2 上移/ }).click();
  await expect(rowOrder().nth(0)).toHaveAttribute("data-testid", "access-target-511");
  await expect(rowOrder().nth(1)).toHaveAttribute("data-testid", "access-target-512");
  await expect(rowOrder().nth(2)).toHaveAttribute("data-testid", "access-target-513");
  expect(moveRequests).toEqual([
    { url: "/api/models/7/targets/511/position", body: { to_index: 0 } },
  ]);

  // Reload keeps the mixed order; no partition appears.
  await page.reload();
  await expect(rowOrder()).toHaveCount(3);
  await expect(rowOrder().nth(0)).toHaveAttribute("data-testid", "access-target-511");
  await expect(rowOrder().nth(1)).toHaveAttribute("data-testid", "access-target-512");
  await expect(rowOrder().nth(2)).toHaveAttribute("data-testid", "access-target-513");

  // Toggle by target row id keeps the row in place.
  await rowOrder().nth(1).getByRole("switch").click();
  await expect(rowOrder().nth(1)).toContainText(/已禁用/);
  expect(toggleRequests).toEqual([
    { url: "/api/models/7/targets/512", body: { is_enabled: false } },
  ]);

  // Type-specific connection edit stays available on the terminal row.
  await expect(rowOrder().nth(1).getByRole("button", { name: /编辑 Terminal One/ })).toBeVisible();

  // Delete by target row id compacts the mixed list across types.
  await rowOrder().nth(2).getByRole("button", { name: /移除目标 3/ }).click();
  await expect(rowOrder()).toHaveCount(2);
  await expect(rowOrder().nth(0)).toHaveAttribute("data-testid", "access-target-511");
  await expect(rowOrder().nth(1)).toHaveAttribute("data-testid", "access-target-512");
  expect(deleteRequests).toEqual(["/api/models/7/targets/513"]);
});
