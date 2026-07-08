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
  /No loadbalance strategies are available for the Default profile\. Create one on the Loadbalance Strategies page first\.|默认配置档案没有可用的负载均衡策略。请先在负载均衡策略页面创建一个。/;
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
