import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T12:00:00Z";
const newModelButton = /New Model|新建模型/;
const newModelDialog = /New Model|新建模型/;
const editModelDialog = /Edit Model|编辑模型/;
const cancelButton = /Cancel|取消/;
const createDefaultsButton = /Create Defaults|创建默认策略/;
const modelIdLabel = /Model ID|模型 ID/;
const displayNameLabel = /Display Name|显示名称/;
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

export async function mockModelRoutes(
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
    if (pathname === "/api/endpoints") {
      return fulfillJson([]);
    }
    if (pathname === "/api/pricing-templates") {
      return fulfillJson([]);
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
        model: {
          id: 50,
          profile_id: 1,
          api_family: payload.api_family,
          model_id: payload.model_id,
          display_name: payload.display_name,
          openai_accepted_format: payload.openai_accepted_format ?? "dual_native",
          loadbalance_strategy_id: payload.loadbalance_strategy_id ?? null,
          loadbalance_strategy: createStrategy(),
          access_targets: payload.access_targets ?? [],
          is_enabled: payload.is_enabled ?? true,
          created_at: timestamp,
          updated_at: timestamp,
        },
        configuration_warnings: [],
      });
    }

    return fulfillJson({});
  });

  return {
    getCreatedPayloads: () => createdPayloads,
    getDefaultsRequests: () => defaultsRequests,
  };
}

test("create model dialog disables submit when no loadbalance strategies exist", async ({ page }) => {
  const routes = await mockModelRoutes(page, { strategies: [] });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const submitButton = dialog.getByRole("button", { name: /创建并启用|创建为停用/ });

  await expect(dialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(submitButton).toBeDisabled();

  await page.getByRole("textbox", { name: modelIdLabel }).fill("zero-strategy-model");
  await page.getByRole("textbox", { name: displayNameLabel }).fill("Zero Strategy Model");
  await expect(submitButton).toBeDisabled();

  await page.getByRole("textbox", { name: displayNameLabel }).press("Enter");
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.getByRole("button", { name: cancelButton }).click();
  await page.getByRole("button", { name: /Edit Model: Target Alpha|编辑模型: Target Alpha/ }).click();

  const editDialog = page.getByRole("dialog", { name: editModelDialog });
  await expect(editDialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(editDialog.getByRole("button", { name: createDefaultsButton })).toHaveCount(0);
});

test("create model dialog creates default strategy and saves a configure-later draft", async ({ page }) => {
  const routes = await mockModelRoutes(page, { strategies: [] });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const submitButton = dialog.getByRole("button", { name: /创建并启用|创建为停用/ });
  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toBeVisible();
  await expect(submitButton).toBeDisabled();

  await dialog.getByRole("button", { name: createDefaultsButton }).click();

  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toHaveCount(0);
  await expect(dialog.locator("#create-model-strategy")).toContainText("Default fill-first routing");

  // Ready mode needs an endpoint; configure-later omits the initial target
  // and forces disabled — the targetless draft path.
  await page.getByRole("textbox", { name: modelIdLabel }).fill("defaults-created-model");
  await dialog.getByRole("switch", { name: /稍后配置/ }).check();
  await expect(dialog.getByRole("button", { name: "创建为停用" })).toBeEnabled();

  await dialog.getByRole("button", { name: "创建为停用" }).click();
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

test("create model dialog keeps empty strategy state when default creation fails", async ({ page }) => {
  const routes = await mockModelRoutes(page, {
    strategies: [],
    defaultsStatus: 500,
    defaultsResponse: { detail: { message: "Default creation failed" } },
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const submitButton = dialog.getByRole("button", { name: /创建并启用|创建为停用/ });
  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toBeVisible();
  await expect(submitButton).toBeDisabled();

  await dialog.getByRole("button", { name: createDefaultsButton }).click();

  await expect(page.getByText("Default creation failed")).toBeVisible();
  await expect(dialog.getByRole("button", { name: createDefaultsButton })).toBeVisible();
  await expect(dialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(submitButton).toBeDisabled();

  await page.getByRole("textbox", { name: modelIdLabel }).fill("defaults-failed-model");
  await page.getByRole("textbox", { name: displayNameLabel }).press("Enter");

  expect(routes.getDefaultsRequests()).toHaveLength(1);
  expect(routes.getCreatedPayloads()).toHaveLength(0);
});

test("create model dialog does not apply delayed defaults response to edit dialog", async ({ page }) => {
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

test("reopened create model dialog adopts the canonical default strategy after defaults resolve", async ({ page }) => {
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
  const reopenedSubmit = reopenedDialog.getByRole("button", { name: /创建并启用|创建为停用/ });
  await expect(reopenedSubmit).toBeDisabled();

  deferredDefaults.resolve();

  // The canonical Default fill-first routing strategy now exists, so the
  // reopened dialog selects it by canonical identity (never array index).
  await expect(page.getByText(defaultStrategiesCreatedToast)).toBeVisible();
  await expect(reopenedDialog.locator("#create-model-strategy")).toContainText("Default fill-first routing");
});

test("create model dialog saves a configure-later targetless disabled draft", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  await page.getByRole("textbox", { name: modelIdLabel }).fill("draft-openai");
  await dialog.getByRole("switch", { name: /稍后配置/ }).check();
  await expect(dialog.getByRole("button", { name: "创建为停用" })).toBeEnabled();

  await dialog.getByRole("button", { name: "创建为停用" }).click();
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
});

test("create model dialog keeps legacy access-target authoring out of the create flow", async ({ page }) => {
  await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  await expect(dialog.locator("#access-target-select")).toHaveCount(0);
  await expect(dialog.getByRole("button", { name: /New terminal target|新建终端目标/ })).toHaveCount(0);
  await expect(dialog.getByText("Tier")).toHaveCount(0);
  await expect(dialog.getByText("Weight")).toHaveCount(0);
  // The one-shot create flow owns the initial Terminal Target section instead.
  await expect(dialog.getByText("首个终端目标", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("switch", { name: /稍后配置/ })).toBeVisible();
});

const detailTimestamp = "2026-08-08T12:00:00Z";

function createDetailStrategy() {
  return {
    id: 11,
    profile_id: 1,
    name: "Default single routing",
    legacy_strategy_type: "single",
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
    created_at: detailTimestamp,
    updated_at: detailTimestamp,
  };
}

function createDetailEndpoint() {
  return {
    id: 1,
    profile_id: 1,
    name: "OpenAI Primary",
    base_url: "https://api.openai.com/v1",
    has_api_key: true,
    masked_api_key: "********",
    position: 0,
    created_at: detailTimestamp,
    updated_at: detailTimestamp,
  };
}

function createDetailConnection(id: number, name: string, capability: string, priority: number) {
  return {
    id,
    profile_id: 1,
    api_family: "openai",
    endpoint_id: 1,
    endpoint: createDetailEndpoint(),
    is_active: true,
    priority,
    name,
    auth_type: null,
    custom_headers: { "X-Trace": "on" },
    openai_text_capability: capability,
    pricing_template_id: null,
    pricing_template: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    created_at: detailTimestamp,
    updated_at: detailTimestamp,
  };
}

function createDetailDiagnostics() {
  return {
    model_config_id: 7,
    strategy: { id: 11, type: "single" },
    accepted_operations: ["openai.chat_completions", "openai.responses", "openai.responses.input_tokens", "openai.responses.compact"],
    stages: [
      {
        stage: "model_targets",
        order: 1,
        entered_when: "always",
        targets: [],
      },
      {
        stage: "terminal_targets",
        order: 2,
        entered_when: "model_targets_has_no_eligible_candidate",
        targets: [
          {
            access_target_id: 91,
            authored_stage_position: 0,
            enabled_strategy_index: 0,
            connection_id: 15,
            coverage: "partial",
            supported_operations: ["openai.responses", "openai.responses.input_tokens", "openai.responses.compact"],
            unsupported_accepted_operations: ["openai.chat_completions"],
            operation_results: [
              { operation_name: "openai.chat_completions", disposition: "incompatible" },
              { operation_name: "openai.responses", disposition: "candidate", terminal_connection_ids: [15] },
              { operation_name: "openai.responses.input_tokens", disposition: "candidate", terminal_connection_ids: [15] },
              { operation_name: "openai.responses.compact", disposition: "candidate", terminal_connection_ids: [15] },
            ],
          },
          {
            access_target_id: 92,
            authored_stage_position: 1,
            enabled_strategy_index: 1,
            connection_id: 16,
            coverage: "full",
            supported_operations: ["openai.chat_completions", "openai.responses", "openai.responses.input_tokens", "openai.responses.compact"],
            unsupported_accepted_operations: [],
            operation_results: [
              { operation_name: "openai.chat_completions", disposition: "truncated_by_single" },
              { operation_name: "openai.responses", disposition: "truncated_by_single" },
              { operation_name: "openai.responses.input_tokens", disposition: "truncated_by_single" },
              { operation_name: "openai.responses.compact", disposition: "truncated_by_single" },
            ],
          },
        ],
      },
    ],
    operation_coverage: [
      {
        operation_name: "openai.chat_completions",
        accepted: true,
        capability_covered: true,
        statically_routable: false,
        resolved_stage: null,
        compatible_access_target_ids: [92],
        access_target_ids: [92],
      },
      {
        operation_name: "openai.responses",
        accepted: true,
        capability_covered: true,
        statically_routable: true,
        resolved_stage: "terminal_targets",
        compatible_access_target_ids: [91, 92],
        access_target_ids: [91],
      },
      {
        operation_name: "openai.responses.input_tokens",
        accepted: true,
        capability_covered: true,
        statically_routable: true,
        resolved_stage: "terminal_targets",
        compatible_access_target_ids: [91, 92],
        access_target_ids: [91],
      },
      {
        operation_name: "openai.responses.compact",
        accepted: true,
        capability_covered: true,
        statically_routable: true,
        resolved_stage: "terminal_targets",
        compatible_access_target_ids: [91, 92],
        access_target_ids: [91],
      },
    ],
    configuration_warnings: [
      {
        code: "openai_target_partial_coverage",
        severity: "warning",
        message: "该目标只承接部分入口能力。",
        path: "openai_text_capability",
        model_config_id: 7,
        access_target_id: 91,
        connection_id: 15,
        operation_names: ["openai.chat_completions"],
        details: { stage: "terminal_targets" },
      },
      {
        code: "openai_operation_uncovered",
        severity: "danger",
        message: "存在兼容目标，但当前没有可参与路由的目标。",
        path: "openai_accepted_format",
        model_config_id: 7,
        access_target_id: null,
        connection_id: null,
        operation_names: ["openai.chat_completions"],
        details: { reason: "no_static_eligible_target" },
      },
      {
        code: "single_strategy_truncates_targets",
        severity: "warning",
        message: "该阶段只有第一个启用目标会参与路由。",
        path: "loadbalance_strategy_id",
        model_config_id: 7,
        access_target_id: null,
        connection_id: null,
        operation_names: [],
        details: { stage: "terminal_targets" },
      },
    ],
  };
}

export async function mockModelDetailRoutes(page: Page) {
  const connection15 = createDetailConnection(15, "Primary Responses", "responses_only", 0);
  const connection16 = createDetailConnection(16, "Fallback Dual", "dual_native", 1);
  const strategy = createDetailStrategy();
  const modelDetail = {
    id: 7,
    profile_id: 1,
    api_family: "openai",
    model_id: "detail-openai",
    display_name: "Detail OpenAI",
    openai_accepted_format: "dual_native",
    loadbalance_strategy_id: 11,
    loadbalance_strategy: strategy,
    access_targets: [
      {
        id: 91,
        target_type: "connection",
        target_model_id: null,
        connection_id: 15,
        terminal_target_id: 15,
        position: 0,
        is_enabled: true,
        target_model: null,
        connection: connection15,
        terminal_target: { id: 15, name: "Primary Responses", is_active: true, endpoint_id: 1, endpoint: { id: 1, name: "OpenAI Primary", base_url: "https://api.openai.com/v1" } },
        created_at: detailTimestamp,
        updated_at: detailTimestamp,
      },
      {
        id: 92,
        target_type: "connection",
        target_model_id: null,
        connection_id: 16,
        terminal_target_id: 16,
        position: 1,
        is_enabled: true,
        target_model: null,
        connection: connection16,
        terminal_target: { id: 16, name: "Fallback Dual", is_active: true, endpoint_id: 1, endpoint: { id: 1, name: "OpenAI Primary", base_url: "https://api.openai.com/v1" } },
        created_at: detailTimestamp,
        updated_at: detailTimestamp,
      },
    ],
    is_enabled: true,
    created_at: detailTimestamp,
    updated_at: detailTimestamp,
  };
  const resetRequests: string[] = [];

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
    if (pathname === "/api/models/7/routing-diagnostics") {
      return fulfillJson(createDetailDiagnostics());
    }
    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({
        items: [
          {
            connection_id: 15,
            window_started_at: null,
            window_request_count: 0,
            in_flight_non_stream: 1,
            in_flight_stream: 0,
            cycle_retry_attempts: 2,
            cumulative_retry_attempts: 2,
            next_retry_at: "2026-08-08T12:05:00Z",
            last_retry_delay_ms: 60000,
            ban_mode: "off",
            banned_until_at: null,
            last_failure_kind: "transient_http",
            last_success_at: "2026-08-08T11:30:00Z",
            last_success_response_headers_latency_ms: 412,
            state: "retry_wait",
            created_at: "2026-08-08T10:00:00Z",
            updated_at: "2026-08-08T11:59:00Z",
          },
          {
            connection_id: 16,
            window_started_at: null,
            window_request_count: 0,
            in_flight_non_stream: 0,
            in_flight_stream: 0,
            cycle_retry_attempts: 4,
            cumulative_retry_attempts: 4,
            next_retry_at: null,
            last_retry_delay_ms: 900000,
            ban_mode: "until_reset",
            banned_until_at: null,
            last_failure_kind: "connect_error",
            last_success_at: "2026-08-08T08:00:00Z",
            last_success_response_headers_latency_ms: 980,
            state: "banned",
            created_at: "2026-08-08T07:00:00Z",
            updated_at: "2026-08-08T08:30:00Z",
          },
        ],
      });
    }
    if (pathname === "/api/models/7" && request.method() === "GET") {
      return fulfillJson(modelDetail);
    }
    if (pathname === "/api/models/7/connections") {
      return fulfillJson([connection15, connection16]);
    }
    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([{ ...modelDetail, connection_count: 2, active_connection_count: 2, health_success_rate: null, health_total_requests: 0, routing_summary: null }]);
    }
    if (pathname === "/api/endpoints") {
      return fulfillJson([createDetailEndpoint()]);
    }
    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([strategy]);
    }
    if (pathname === "/api/pricing-templates") {
      return fulfillJson([]);
    }
    if (pathname === "/api/stats/spending") {
      return fulfillJson({
        summary: { model_id: "detail-openai", group_by: "endpoint", preset: "all", total_spend_micros: 0, total_input_tokens: 0, total_output_tokens: 0, total_cache_read_input_tokens: 0, total_cache_creation_input_tokens: 0, total_reasoning_tokens: 0, request_count: 0 },
        report_currency_symbol: "$",
        report_currency_code: "USD",
      });
    }
    if (pathname === "/api/loadbalance/current-state/16/reset" && request.method() === "POST") {
      resetRequests.push(pathname);
      return fulfillJson({
        connection_id: 16,
        cleared: true,
        state: {
          connection_id: 16,
          window_started_at: null,
          window_request_count: 0,
          in_flight_non_stream: 0,
          in_flight_stream: 0,
          cycle_retry_attempts: 0,
          cumulative_retry_attempts: 0,
          next_retry_at: null,
          last_retry_delay_ms: 0,
          ban_mode: "off",
          banned_until_at: null,
          last_failure_kind: null,
          last_success_at: "2026-08-08T08:00:00Z",
          last_success_response_headers_latency_ms: 980,
          state: "available",
          created_at: "2026-08-08T07:00:00Z",
          updated_at: "2026-08-08T12:06:00Z",
        },
      });
    }
    return fulfillJson({});
  });

  return { getResetRequests: () => resetRequests };
}

test("model detail shows two-stage order, single truncation and cooldown reset", async ({ page }) => {
  const routes = await mockModelDetailRoutes(page);

  await page.goto("/models/7");

  await expect(page.getByText("操作能力覆盖")).toBeVisible();
  await expect(page.getByText("模型目标（先尝试）")).toBeVisible();
  await expect(page.getByText("终端目标（无模型候选时回落）")).toBeVisible();
  await expect(page.getByText("可路由", { exact: true }).first()).toBeVisible();
  await expect(page.getByText("存在能力但当前不参与")).toBeVisible();
  await expect(page.getByText("由终端目标阶段形成候选")).toBeVisible();
  await expect(page.getByText("该目标只承接部分入口能力。")).toBeVisible();
  await expect(page.getByText("被 single 截断").first()).toBeVisible();
  await expect(page.getByText(/重试等待至/)).toBeVisible();

  const bannedCard = page.getByTestId("terminal-target-card-16");
  await expect(bannedCard.getByText("重置冷却")).toBeVisible();
  await bannedCard.getByRole("button", { name: "重置冷却" }).click();
  await expect(routes.getResetRequests()).toHaveLength(1);
  await expect(bannedCard.getByText("当前无冷却限制")).toBeVisible();
});

test("model detail canonicalizes dead tab URLs and consumes one-shot action once", async ({ page }) => {
  await mockModelDetailRoutes(page);

  // Old ?tab=connections URL is normalized to the canonical route.
  await page.goto("/models/7?tab=connections");
  await page.getByTestId("model-detail-feature-page").waitFor({ timeout: 15000 });
  await expect(page).toHaveURL(/\/models\/7$/);

  // One-shot create action opens the dialog and clears itself (replace), so a
  // refresh never reopens it.
  await page.goto("/models/7?action=create-terminal-target&endpoint_id=1");
  await page.getByTestId("model-detail-feature-page").waitFor({ timeout: 15000 });
  const dialog = page.getByRole("dialog", { name: /终端目标|Terminal Target/ });
  await expect(dialog).toBeVisible();
  await expect(page).toHaveURL(/\/models\/7$/);
  await page.reload();
  await page.getByTestId("model-detail-feature-page").waitFor({ timeout: 15000 });
  await expect(page.getByRole("dialog", { name: /终端目标|Terminal Target/ })).toHaveCount(0);

  // focus_connection_id focuses the target card and clears itself once.
  await page.goto("/models/7?focus_connection_id=16");
  await page.getByTestId("model-detail-feature-page").waitFor({ timeout: 15000 });
  await expect(page.getByTestId("terminal-target-card-16")).toHaveCount(1);
  await expect(page).toHaveURL(/\/models\/7$/);
});

test("model detail exposes separate model and terminal stages with stage-local reorder", async ({ page }) => {
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
    if (pathname === "/api/models/7/routing-diagnostics") {
      return fulfillJson({
        model_config_id: 7,
        strategy: { id: 11, type: "fill-first" },
        accepted_operations: [],
        stages: [],
        operation_coverage: [],
        configuration_warnings: [],
      });
    }
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
      return fulfillJson({ access_targets: targets, configuration_warnings: [] });
    }
    const targetMatch = pathname.match(/^\/api\/models\/7\/targets\/(\d+)$/);
    if (targetMatch && request.method() === "PATCH") {
      const body = request.postDataJSON();
      toggleRequests.push({ url: pathname, body });
      const targetId = Number(targetMatch[1]);
      targets = (targets as Array<Record<string, unknown>>).map((item) =>
        item.id === targetId ? { ...item, is_enabled: body.is_enabled ?? item.is_enabled } : item,
      );
      return fulfillJson({ access_targets: targets, configuration_warnings: [] });
    }
    if (targetMatch && request.method() === "DELETE") {
      deleteRequests.push(pathname);
      const targetId = Number(targetMatch[1]);
      targets = (targets as Array<{ id: number }>)
        .filter((item) => item.id !== targetId)
        .map((item, index) => ({ ...item, position: index }));
      return fulfillJson({ access_targets: targets, configuration_warnings: [] });
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

  const modelStage = page.getByTestId("access-target-stage-model_targets");
  const terminalStage = page.getByTestId("access-target-stage-terminal_targets");
  await expect(modelStage.getByTestId("model-target-row-child-model")).toContainText(/Child Model/);
  await expect(modelStage.getByText(/位置 1/)).toBeVisible();
  const terminalRows = () => terminalStage.locator("[data-testid^='terminal-target-card-']");
  await expect(terminalRows()).toHaveCount(2);
  await expect(terminalRows().nth(0)).toHaveAttribute("data-testid", "terminal-target-card-91");
  await expect(terminalRows().nth(1)).toHaveAttribute("data-testid", "terminal-target-card-92");
  await expect(terminalRows().nth(0)).toContainText(/Terminal One/);
  await expect(terminalRows().nth(1)).toContainText(/Terminal Two/);
  await expect(terminalRows().nth(0)).toContainText(/位置 1/);
  await expect(terminalRows().nth(1)).toContainText(/位置 2/);

  // Reorder remains within the terminal fallback stage; it never crosses the model stage.
  await terminalRows().nth(1).getByRole("button", { name: /上移目标 Terminal Two/ }).click();
  expect(moveRequests).toEqual([
    { url: "/api/models/7/targets/513/position", body: { to_index: 0 } },
  ]);

  // Toggle and edit continue to address the persistent terminal target row/connection.
  await terminalRows().nth(1).getByRole("switch").click();
  expect(toggleRequests).toEqual([
    { url: "/api/models/7/targets/512", body: { is_enabled: false } },
  ]);
  await expect(terminalRows().nth(1).getByRole("button", { name: /编辑 Terminal One/ })).toBeVisible();

  // Delete remains scoped to the terminal fallback stage.
  await terminalRows().nth(0).getByRole("button", { name: /删除目标 Terminal Two/ }).click();
  expect(deleteRequests).toEqual(["/api/models/7/targets/513"]);
});
