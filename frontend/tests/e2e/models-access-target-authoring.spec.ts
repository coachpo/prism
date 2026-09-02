import { expect, test, type Page } from "@playwright/test";
import {
  createEmptyIngressSpendingReport,
  expectIngressSpendingRequest,
} from "./spending-report-fixtures";
import {
  createUnavailablePiModelRead,
  createUnboundModelsDevCatalog,
} from "./model-detail-catalog-fixtures";

const timestamp = "2026-04-27T12:00:00Z";
const newModelButton = /New Model|新建模型配置/;
const newModelDialog = /New Model|新建入口模型/;
const editModelDialog = /Edit Model|编辑入口模型/;
const cancelButton = /Cancel|取消/;
const createDefaultsButton = /Create Defaults|创建默认策略/;
const modelIdLabel = /Entry Model ID|入口模型 ID/;
const displayNameLabel = /Display Name|显示名称/;
const noStrategiesCopy =
  /No loadbalance strategies are available\. Create one on the Loadbalance Strategies page first\.|没有可用的路由策略。请先在路由策略页面创建一个。/;
const modelCreatedToast = /Model created|模型已创建/;
const defaultStrategiesCreatedToast =
  /Default loadbalance strategies created|默认路由策略已创建/;

function createStrategy() {
  return {
    id: 11,
    profile_id: 1,
    name: "Default fill-first routing",
    legacy_strategy_type: "fill-first",
    is_default: true,
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
  loadbalanceStrategy: ReturnType<
    typeof createStrategy
  > | null = createStrategy(),
) {
  return {
    id,
    profile_id: 1,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    openai_accepted_format:
      apiFamily === "openai" ? "dual_native" : null,
    openai_image_operations: null,
    loadbalance_strategy_id: loadbalanceStrategyId,
    loadbalance_strategy: loadbalanceStrategy,
    access_targets: [],
    direct_request_enabled: true,
    incoming_model_target_count: 0,
    configuration_warnings: [],
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
    endpoints?: ReturnType<typeof createDetailEndpoint>[];
    models?: ReturnType<typeof createModelListItem>[];
    strategies?: ReturnType<typeof createStrategy>[];
  } = {},
) {
  const strategies = options.strategies ?? [createStrategy()];
  const endpoints = options.endpoints ?? [];
  const models = options.models ?? [
    createModelListItem(1, "target-alpha", "Target Alpha", "openai"),
    createModelListItem(2, "target-beta", "Target Beta", "openai"),
    createModelListItem(3, "claude-sonnet", "Claude Sonnet", "anthropic"),
  ];
  const createdPayloads: unknown[] = [];
  const updatedPayloads: Record<string, unknown>[] = [];
  const defaultsRequests: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

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
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
      });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({
        report_currency_code: "EUR",
        report_currency_symbol: "€",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson(models);
    }
    if (pathname === "/api/endpoints") {
      return fulfillJson(endpoints);
    }
    if (pathname === "/api/pricing-templates") {
      return fulfillJson([]);
    }
    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson(strategies);
    }
    if (
      pathname === "/api/loadbalance/strategies/defaults" &&
      request.method() === "POST"
    ) {
      defaultsRequests.push(pathname);
      await options.defaultsDelay;
      const response = options.defaultsResponse ?? {
        created: [
          { canonical_name: "Default fill-first routing", strategy_id: 11 },
        ],
        existing: [],
        default_strategy_id: 11,
        default_changed: true,
        complete: false,
      };
      if (
        options.defaultsStatus !== undefined &&
        options.defaultsStatus !== 200
      ) {
        return fulfillJson(
          options.defaultsResponse ?? { detail: "defaults failed" },
          options.defaultsStatus,
        );
      }
      const defaultStrategyId = (
        response as { default_strategy_id?: number | null }
      ).default_strategy_id;
      if (defaultStrategyId != null) {
        strategies.length = 0;
        strategies.push({ ...createStrategy(), id: defaultStrategyId });
      }
      return fulfillJson(response);
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
          openai_accepted_format:
            payload.openai_accepted_format ?? "dual_native",
          loadbalance_strategy_id: payload.loadbalance_strategy_id ?? null,
          loadbalance_strategy: createStrategy(),
          access_targets: payload.access_targets ?? [],
          direct_request_enabled: payload.direct_request_enabled !== false,
          incoming_model_target_count: 0,
          configuration_warnings: [],
          is_enabled: payload.is_enabled ?? true,
          created_at: timestamp,
          updated_at: timestamp,
        },
        configuration_warnings: [],
      });
    }
    const modelUpdateMatch = pathname.match(/^\/api\/models\/(\d+)$/);
    if (modelUpdateMatch && request.method() === "PUT") {
      const id = Number(modelUpdateMatch[1]);
      const payload = request.postDataJSON() as Record<string, unknown>;
      const model = models.find((item) => item.id === id);
      updatedPayloads.push(payload);
      return fulfillJson({
        model: { ...model, ...payload, updated_at: timestamp },
        configuration_warnings: [],
      });
    }

    return fulfillJson({});
  });

  return {
    getCreatedPayloads: () => createdPayloads,
    getDefaultsRequests: () => defaultsRequests,
    getUpdatedPayloads: () => updatedPayloads,
  };
}

test("create model dialog disables submit when no loadbalance strategies exist", async ({
  page,
}) => {
  const routes = await mockModelRoutes(page, { strategies: [] });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const submitButton = dialog.getByRole("button", {
    name: /保存|创建并启用|创建为停用/,
  });

  await expect(dialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(submitButton).toBeDisabled();

  await page
    .getByRole("textbox", { name: modelIdLabel })
    .fill("zero-strategy-model");
  await page
    .getByRole("textbox", { name: displayNameLabel })
    .fill("Zero Strategy Model");
  await expect(submitButton).toBeDisabled();

  await page.getByRole("textbox", { name: displayNameLabel }).press("Enter");
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.getByRole("button", { name: cancelButton }).click();
  await page
    .getByRole("button", {
      name: /Edit Model: Target Alpha|编辑入口模型: Target Alpha/,
    })
    .click();

  const editDialog = page.getByRole("dialog", { name: editModelDialog });
  await expect(editDialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(
    editDialog.getByRole("button", { name: createDefaultsButton }),
  ).toHaveCount(0);
});

test("create model dialog creates default strategy and saves a configure-later draft", async ({
  page,
}) => {
  const routes = await mockModelRoutes(page, { strategies: [] });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const submitButton = dialog.getByRole("button", {
    name: /保存|创建并启用|创建为停用/,
  });
  await expect(
    dialog.getByRole("button", { name: createDefaultsButton }),
  ).toBeVisible();
  await expect(submitButton).toBeDisabled();

  const directEntrySwitch = dialog.getByRole("switch", {
    name: "允许客户端直接请求",
  });
  await expect(directEntrySwitch).toBeChecked();
  await directEntrySwitch.uncheck();

  await dialog.getByRole("button", { name: createDefaultsButton }).click();

  await expect(
    dialog.getByRole("button", { name: createDefaultsButton }),
  ).toHaveCount(0);
  await expect(directEntrySwitch).not.toBeChecked();
  await expect(dialog.locator("#create-model-strategy")).toContainText(
    "Default fill-first routing",
  );

  // Ready mode needs an endpoint; configure-later omits the initial target
  // and forces disabled — the targetless draft path.
  await page
    .getByRole("textbox", { name: modelIdLabel })
    .fill("defaults-created-model");
  await dialog.getByRole("switch", { name: /稍后配置/ }).check();
  await expect(
    dialog.getByRole("button", { name: "创建为停用" }),
  ).toBeEnabled();

  await dialog.getByRole("button", { name: "创建为停用" }).click();
  await expect(page.getByText(modelCreatedToast)).toBeVisible();

  expect(routes.getDefaultsRequests()).toHaveLength(1);
  expect(routes.getCreatedPayloads()).toEqual([
    {
      api_family: "openai",
      model_id: "defaults-created-model",
      display_name: "defaults-created-model",
      openai_accepted_format: "dual_native",
      openai_image_operations: null,
      loadbalance_strategy_id: 11,
      is_enabled: false,
      direct_request_enabled: false,
    },
  ]);
});

test("create model dialog keeps empty strategy state when default creation fails", async ({
  page,
}) => {
  const routes = await mockModelRoutes(page, {
    strategies: [],
    defaultsStatus: 500,
    defaultsResponse: { detail: { message: "Default creation failed" } },
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  const submitButton = dialog.getByRole("button", {
    name: /保存|创建并启用|创建为停用/,
  });
  await expect(
    dialog.getByRole("button", { name: createDefaultsButton }),
  ).toBeVisible();
  await expect(submitButton).toBeDisabled();

  await dialog.getByRole("button", { name: createDefaultsButton }).click();

  await expect(page.getByText("Default creation failed")).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: createDefaultsButton }),
  ).toBeVisible();
  await expect(dialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(submitButton).toBeDisabled();

  await page
    .getByRole("textbox", { name: modelIdLabel })
    .fill("defaults-failed-model");
  await page.getByRole("textbox", { name: displayNameLabel }).press("Enter");

  expect(routes.getDefaultsRequests()).toHaveLength(1);
  expect(routes.getCreatedPayloads()).toHaveLength(0);
});

test("create model dialog does not apply delayed defaults response to edit dialog", async ({
  page,
}) => {
  const deferredDefaults = createDeferredDefaultsResponse();
  const routes = await mockModelRoutes(page, {
    strategies: [],
    defaultsDelay: deferredDefaults.ready,
    models: [
      createModelListItem(
        1,
        "edit-no-strategy",
        "Edit No Strategy",
        "openai",
        null,
        null,
      ),
    ],
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const createDialog = page.getByRole("dialog", { name: newModelDialog });
  await createDialog
    .getByRole("button", { name: createDefaultsButton })
    .click();
  expect(routes.getDefaultsRequests()).toHaveLength(1);
  await createDialog.getByRole("button", { name: cancelButton }).click();

  await page
    .getByRole("button", {
      name: /Edit Model: Edit No Strategy|编辑入口模型: Edit No Strategy/,
    })
    .click();
  const editDialog = page.getByRole("dialog", { name: editModelDialog });
  await expect(editDialog.getByText(noStrategiesCopy)).toBeVisible();

  deferredDefaults.resolve();

  await expect(page.getByText(defaultStrategiesCreatedToast)).toBeVisible();
  await expect(
    editDialog.getByRole("button", { name: createDefaultsButton }),
  ).toHaveCount(0);
  await expect(editDialog.locator("#model-loadbalance-strategy")).toHaveCount(
    0,
  );
});

test("reopened create model dialog adopts the canonical default strategy after defaults resolve", async ({
  page,
}) => {
  const deferredDefaults = createDeferredDefaultsResponse();
  const routes = await mockModelRoutes(page, {
    strategies: [],
    defaultsDelay: deferredDefaults.ready,
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const createDialog = page.getByRole("dialog", { name: newModelDialog });
  await createDialog
    .getByRole("button", { name: createDefaultsButton })
    .click();
  expect(routes.getDefaultsRequests()).toHaveLength(1);
  await createDialog.getByRole("button", { name: cancelButton }).click();

  await page.getByRole("button", { name: newModelButton }).click();
  const reopenedDialog = page.getByRole("dialog", { name: newModelDialog });
  const reopenedSubmit = reopenedDialog.getByRole("button", {
    name: /保存|创建并启用|创建为停用/,
  });
  await expect(reopenedSubmit).toBeDisabled();

  deferredDefaults.resolve();

  // The canonical Default fill-first routing strategy now exists, so the
  // reopened dialog selects it by canonical identity (never array index).
  await expect(page.getByText(defaultStrategiesCreatedToast)).toBeVisible();
  await expect(reopenedDialog.locator("#create-model-strategy")).toContainText(
    "Default fill-first routing",
  );
});

test("create model dialog supports configure-later and a decoupled initial target", async ({
  page,
}) => {
  const routes = await mockModelRoutes(page, {
    endpoints: [createDetailEndpoint()],
  });

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  await page.getByRole("textbox", { name: modelIdLabel }).fill("draft-openai");
  await dialog.getByRole("switch", { name: /稍后配置/ }).check();
  await expect(
    dialog.getByRole("textbox", { name: "上游模型 ID" }),
  ).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: "创建为停用" }),
  ).toBeEnabled();

  await dialog.getByRole("button", { name: "创建为停用" }).click();
  await expect(page.getByText(modelCreatedToast)).toBeVisible();

  expect(routes.getCreatedPayloads()).toEqual([
    {
      api_family: "openai",
      model_id: "draft-openai",
      display_name: "draft-openai",
      openai_accepted_format: "dual_native",
      openai_image_operations: null,
      loadbalance_strategy_id: 11,
      is_enabled: false,
      direct_request_enabled: true,
    },
  ]);

  await page.getByRole("button", { name: newModelButton }).click();
  const entryModelId = dialog.getByRole("textbox", { name: modelIdLabel });
  const upstreamModelId = dialog.getByRole("textbox", { name: "上游模型 ID" });

  await entryModelId.fill("entry-a");
  await expect(upstreamModelId).toHaveValue("entry-a");
  await dialog.locator("#create-target-endpoint").click();
  await page.getByRole("option", { name: "OpenAI Primary" }).click();
  await upstreamModelId.fill("");
  await dialog.getByRole("button", { name: "创建并启用" }).click();
  await expect(dialog.getByText("上游模型 ID 不能为空白")).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(1);
  await upstreamModelId.fill("provider/Model-B");
  await entryModelId.fill("entry-c");
  await expect(upstreamModelId).toHaveValue("provider/Model-B");

  await dialog.getByRole("button", { name: "创建并启用" }).click();
  expect(routes.getCreatedPayloads()).toHaveLength(2);
  expect(routes.getCreatedPayloads()[1]).toMatchObject({
    model_id: "entry-c",
    initial_terminal_target: {
      endpoint_id: 1,
      upstream_model_id: "provider/Model-B",
    },
  });
  await page
    .getByRole("button", { name: "编辑入口模型: Target Alpha" })
    .click();
  const editDialog = page.getByRole("dialog", { name: editModelDialog });
  await editDialog
    .getByRole("switch", { name: "允许客户端直接请求" })
    .uncheck();
  await editDialog.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => routes.getUpdatedPayloads().length).toBe(1);
  expect(routes.getUpdatedPayloads()[0]).toMatchObject({
    direct_request_enabled: false,
    is_enabled: true,
  });
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

function createDetailConnection(
  id: number,
  name: string,
  capability: string,
  priority: number,
) {
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
    upstream_model_id:
      id === 15 ? "provider/Responses-Primary" : "provider/Dual-Fallback",
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
    accepted_operations: [
      "openai.chat_completions",
      "openai.responses",
      "openai.responses.input_tokens",
      "openai.responses.compact",
    ],
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
            supported_operations: [
              "openai.responses",
              "openai.responses.input_tokens",
              "openai.responses.compact",
            ],
            unsupported_accepted_operations: ["openai.chat_completions"],
            operation_results: [
              {
                operation_name: "openai.chat_completions",
                disposition: "incompatible",
              },
              {
                operation_name: "openai.responses",
                disposition: "candidate",
                terminal_connection_ids: [15],
              },
              {
                operation_name: "openai.responses.input_tokens",
                disposition: "candidate",
                terminal_connection_ids: [15],
              },
              {
                operation_name: "openai.responses.compact",
                disposition: "candidate",
                terminal_connection_ids: [15],
              },
            ],
          },
          {
            access_target_id: 92,
            authored_stage_position: 1,
            enabled_strategy_index: 1,
            connection_id: 16,
            coverage: "full",
            supported_operations: [
              "openai.chat_completions",
              "openai.responses",
              "openai.responses.input_tokens",
              "openai.responses.compact",
            ],
            unsupported_accepted_operations: [],
            operation_results: [
              {
                operation_name: "openai.chat_completions",
                disposition: "truncated_by_single",
              },
              {
                operation_name: "openai.responses",
                disposition: "truncated_by_single",
              },
              {
                operation_name: "openai.responses.input_tokens",
                disposition: "truncated_by_single",
              },
              {
                operation_name: "openai.responses.compact",
                disposition: "truncated_by_single",
              },
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
  const mutationPaths: string[] = [];
  const targetPayloads: Record<string, unknown>[] = [];
  const connection15 = createDetailConnection(
    15,
    "Primary Responses",
    "responses_only",
    0,
  );
  const connection16 = createDetailConnection(
    16,
    "Fallback Dual",
    "dual_native",
    1,
  );
  const strategy = createDetailStrategy();
  const modelDetail = {
    id: 7,
    profile_id: 1,
    api_family: "openai",
    model_id: "detail-openai",
    display_name: "Detail OpenAI",
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    direct_request_enabled: true,
    incoming_model_target_count: 0,
    configuration_warnings: [],
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
        terminal_target: {
          id: 15,
          name: "Primary Responses",
          is_active: true,
          endpoint_id: 1,
          endpoint: {
            id: 1,
            name: "OpenAI Primary",
            base_url: "https://api.openai.com/v1",
          },
        },
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
        terminal_target: {
          id: 16,
          name: "Fallback Dual",
          is_active: true,
          endpoint_id: 1,
          endpoint: {
            id: 1,
            name: "OpenAI Primary",
            base_url: "https://api.openai.com/v1",
          },
        },
        created_at: detailTimestamp,
        updated_at: detailTimestamp,
      },
    ],
    is_enabled: true,
    created_at: detailTimestamp,
    updated_at: detailTimestamp,
  };
  const internalTargetModel = {
    ...createModelListItem(
      8,
      "internal-detail-target",
      "Internal Detail Target",
    ),
    direct_request_enabled: false,
    incoming_model_target_count: 1,
  };

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }
    if (request.method() !== "GET" && pathname.includes("/connections")) {
      mutationPaths.push(pathname);
    }
    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (pathname === "/api/auth/status") {
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
      });
    }
    if (pathname === "/api/models/7/routing-diagnostics") {
      return fulfillJson(createDetailDiagnostics());
    }
    if (pathname === "/api/models/7/catalog" && request.method() === "GET") {
      return fulfillJson(createUnboundModelsDevCatalog());
    }
    if (pathname === "/api/models/7/pi" && request.method() === "GET") {
      return fulfillJson(
        createUnavailablePiModelRead({
          modelConfigId: 7,
          modelId: "detail-openai",
        }),
      );
    }
    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({
        generated_at: detailTimestamp,
        scope: "process",
        instance_id: "e2e-instance",
        configuration_revision: "1",
        completeness: {
          state: "ready",
          complete: true,
          configured_target_count: 0,
          observed_target_count: 0,
          unobserved_target_count: 0,
          observed_subset_counts: {},
        },
        items: [],
        has_more: false,
        next_cursor: null,
      });
    }
    if (pathname === "/api/models/7" && request.method() === "GET") {
      return fulfillJson(modelDetail);
    }
    if (
      pathname === "/api/models/7/targets" &&
      request.method() === "POST"
    ) {
      const payload = request.postDataJSON() as Record<string, unknown>;
      targetPayloads.push(payload);
      return fulfillJson({
        access_targets: modelDetail.access_targets,
        configuration_warnings: [],
      });
    }
    if (
      pathname === "/api/models/7/connections" &&
      request.method() === "GET"
    ) {
      return fulfillJson([connection15, connection16]);
    }
    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([
        {
          ...modelDetail,
          connection_count: 2,
          active_connection_count: 2,
          health_success_rate: null,
          health_total_requests: 0,
          routing_summary: null,
        },
        internalTargetModel,
      ]);
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
      expectIngressSpendingRequest(request, "detail-openai");
      return fulfillJson(createEmptyIngressSpendingReport());
    }
    return fulfillJson({});
  });

  return { mutationPaths, targetPayloads };
}

// A model pair for SPA navigation: model 16 holds one Terminal Target plus a
// Model Target whose `target_model.id` is 17, so following the detail entry
// must move 16 → 17 within one route match.
function createLinkedPairModels() {
  const strategy = createDetailStrategy();
  const connection15 = createDetailConnection(
    15,
    "Primary Responses",
    "responses_only",
    0,
  );
  const targetModelSummary = {
    id: 17,
    profile_id: 1,
    api_family: "openai",
    model_id: "detail-beta",
    display_name: "Beta Detail",
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    direct_request_enabled: false,
    incoming_model_target_count: 1,
    loadbalance_strategy_id: 11,
    is_enabled: true,
  };
  const model16 = {
    id: 16,
    profile_id: 1,
    api_family: "openai",
    model_id: "detail-alpha",
    display_name: "Alpha Detail",
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    direct_request_enabled: true,
    incoming_model_target_count: 0,
    configuration_warnings: [],
    loadbalance_strategy_id: 11,
    loadbalance_strategy: strategy,
    access_targets: [
      {
        id: 161,
        target_type: "connection",
        target_model_id: null,
        connection_id: 15,
        terminal_target_id: 15,
        position: 0,
        is_enabled: true,
        target_model: null,
        connection: connection15,
        terminal_target: {
          id: 15,
          name: "Primary Responses",
          is_active: true,
          endpoint_id: 1,
          endpoint: {
            id: 1,
            name: "OpenAI Primary",
            base_url: "https://api.openai.com/v1",
          },
        },
        created_at: detailTimestamp,
        updated_at: detailTimestamp,
      },
      {
        id: 162,
        target_type: "model",
        target_model_id: "detail-beta",
        connection_id: null,
        terminal_target_id: null,
        position: 1,
        is_enabled: true,
        // The only navigable identity: the linked config record's own id.
        target_model: targetModelSummary,
        connection: null,
        terminal_target: null,
        created_at: detailTimestamp,
        updated_at: detailTimestamp,
      },
    ],
    is_enabled: true,
    created_at: detailTimestamp,
    updated_at: detailTimestamp,
  };
  const model17 = {
    ...model16,
    id: 17,
    model_id: "detail-beta",
    display_name: "Beta Detail",
    direct_request_enabled: false,
    incoming_model_target_count: 1,
    configuration_warnings: [],
    access_targets: [],
  };
  return { strategy, connection15, model16, model17 };
}

async function mockLinkedModelPairRoutes(page: Page) {
  const { strategy, connection15, model16, model17 } = createLinkedPairModels();
  // Model 17 stays unresponsive until released, so the loading window between
  // the two models can be asserted deterministically.
  let releaseModel17: (() => void) | null = null;
  const model17Gate = new Promise<void>((resolve) => {
    releaseModel17 = resolve;
  });
  const listItem = (model: typeof model16, connections: number) => ({
    ...model,
    connection_count: connections,
    active_connection_count: connections,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
  });

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
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
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
      });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({
        report_currency_code: "EUR",
        report_currency_symbol: "€",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/models/16/routing-diagnostics") {
      return fulfillJson({ ...createDetailDiagnostics(), model_config_id: 16 });
    }
    if (pathname === "/api/models/17/routing-diagnostics") {
      return fulfillJson({ ...createDetailDiagnostics(), model_config_id: 17 });
    }
    if (pathname === "/api/models/16/catalog" && request.method() === "GET") {
      return fulfillJson(createUnboundModelsDevCatalog());
    }
    if (pathname === "/api/models/17/catalog" && request.method() === "GET") {
      return fulfillJson(createUnboundModelsDevCatalog());
    }
    if (pathname === "/api/models/16/pi" && request.method() === "GET") {
      return fulfillJson(
        createUnavailablePiModelRead({
          modelConfigId: 16,
          modelId: "detail-alpha",
        }),
      );
    }
    if (pathname === "/api/models/17/pi" && request.method() === "GET") {
      return fulfillJson(
        createUnavailablePiModelRead({
          modelConfigId: 17,
          modelId: "detail-beta",
        }),
      );
    }
    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({
        generated_at: detailTimestamp,
        scope: "process",
        instance_id: "e2e-instance",
        configuration_revision: "1",
        completeness: {
          state: "ready",
          complete: true,
          configured_target_count: 0,
          observed_target_count: 0,
          unobserved_target_count: 0,
          observed_subset_counts: {},
        },
        items: [],
        has_more: false,
        next_cursor: null,
      });
    }
    if (pathname === "/api/models/16" && request.method() === "GET") {
      return fulfillJson(model16);
    }
    if (pathname === "/api/models/17" && request.method() === "GET") {
      await model17Gate;
      return fulfillJson(model17);
    }
    if (pathname === "/api/models/16/connections") {
      return fulfillJson([connection15]);
    }
    if (pathname === "/api/models/17/connections") {
      return fulfillJson([]);
    }
    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([listItem(model16, 1), listItem(model17, 0)]);
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
      expectIngressSpendingRequest(request, ["detail-alpha", "detail-beta"]);
      return fulfillJson(createEmptyIngressSpendingReport());
    }
    return fulfillJson({});
  });

  return { releaseModel17: () => releaseModel17?.() };
}

test("model target detail entry switches entities over SPA without stale state", async ({
  page,
}) => {
  const controls = await mockLinkedModelPairRoutes(page);

  await page.goto("/route/models/16");
  await expect(page.getByTestId("model-detail-feature-page")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Alpha Detail" }),
  ).toBeVisible();

  // Terminal-target menus keep their actions and gain no view-details entry.
  await page
    .getByTestId("access-target-161")
    .getByRole("button", { name: /更多操作/ })
    .click();
  let menu = page.getByRole("menu");
  await expect(
    menu.getByRole("menuitem", { name: /查看入口模型/ }),
  ).toHaveCount(0);
  await expect(
    menu.getByRole("menuitem", { name: /复制终端目标 Primary Responses/ }),
  ).toBeVisible();
  await menu
    .getByRole("menuitem", { name: /复制终端目标 Primary Responses/ })
    .click();
  const copyDialog = page.getByRole("dialog", { name: "复制终端目标" });
  await expect(
    copyDialog.getByTestId("copy-upstream-model-note"),
  ).toContainText("保留源终端目标的上游模型 ID");
  await copyDialog.getByRole("button", { name: "取消" }).click();

  // The model-target row offers the entry and lands on the canonical detail
  // URL of the TARGET config id (17), not this row's or option list's ids.
  await page
    .getByTestId("access-target-162")
    .getByRole("button", { name: /更多操作/ })
    .click();
  menu = page.getByRole("menu");
  const viewEntry = menu.getByRole("menuitem", {
    name: /查看模型配置 Beta Detail 的详情/,
  });
  await expect(viewEntry).toBeVisible();
  await viewEntry.click();
  await expect(page).toHaveURL(/\/route\/models\/17$/);

  // While model 17 is still loading, nothing from model 16 may remain visible
  // or operable: no old title, no drafts, no leftover dialogs.
  await expect(page.getByTestId("model-detail-feature-loading")).toBeVisible();
  await expect(page.getByText("Alpha Detail")).toHaveCount(0);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "保存顺序" })).toHaveCount(0);

  controls.releaseModel17();
  await expect(
    page.getByRole("heading", { name: "Beta Detail" }),
  ).toBeVisible();
  await expect(page.getByText("Alpha Detail")).toHaveCount(0);

  // Browser back returns to model 16 as a freshly loaded entity.
  await page.goBack();
  await expect(page).toHaveURL(/\/route\/models\/16$/);
  await expect(
    page.getByRole("heading", { name: "Alpha Detail" }),
  ).toBeVisible();
  await expect(page.getByTestId("access-target-162")).toBeVisible();
});

test("model detail canonicalizes dead tab and one-shot target actions", async ({
  page,
}) => {
  const routes = await mockModelDetailRoutes(page);

  // Model detail no longer exposes a tab in the canonical URL; unsupported
  // legacy tab state is removed while one-shot actions remain supported.
  await page.goto("/models/7?tab=connections");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  await expect(page).toHaveURL(/\/models\/7$/);
  await expect(
    page
      .getByTestId("access-target-91")
      .getByTitle("上游模型 ID: provider/Responses-Primary"),
  ).toBeVisible();
  await page.getByRole("button", { name: "编辑入口模型" }).click();
  const modelSettings = page.getByRole("dialog", { name: "入口模型设置" });
  await expect(
    modelSettings.getByText(
      /修改入口模型 ID 不会改写已有终端目标的上游模型 ID/,
    ),
  ).toBeVisible();
  await expect(
    modelSettings.getByRole("textbox", { name: "入口模型 ID" }),
  ).toBeVisible();
  await modelSettings.getByRole("button", { name: "取消" }).click();

  // One-shot create action opens the dialog and clears itself (replace), so a
  // refresh never reopens it.
  await page.goto("/models/7?action=create-terminal-target&endpoint_id=1");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  const dialog = page.getByRole("dialog", { name: /终端目标|Terminal Target/ });
  await expect(dialog).toBeVisible();
  await expect(page).toHaveURL(/\/models\/7$/);
  const upstreamModelId = dialog.getByRole("textbox", {
    name: "上游模型 ID",
  });
  await expect(upstreamModelId).toHaveValue("detail-openai");
  await upstreamModelId.fill("");
  await dialog.getByRole("button", { name: "保存终端目标" }).click();
  await expect(dialog.getByText("上游模型 ID 不能为空白")).toBeVisible();
  expect(routes.mutationPaths).toHaveLength(0);
  await dialog.locator("#conn-prefill-source").click();
  await page.getByRole("option", { name: "Primary Responses" }).click();
  await expect(upstreamModelId).toHaveValue("provider/Responses-Primary");
  await page.reload();
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  await expect(
    page.getByRole("dialog", { name: /终端目标|Terminal Target/ }),
  ).toHaveCount(0);

  await page.getByRole("button", { name: "编辑 Primary Responses" }).click();
  const editDialog = page.getByRole("dialog", { name: "编辑终端目标" });
  const editUpstreamModelId = editDialog.getByRole("textbox", {
    name: "上游模型 ID",
  });
  await expect(editUpstreamModelId).toHaveValue("provider/Responses-Primary");
  await editUpstreamModelId.fill("");
  await editDialog.getByRole("button", { name: "保存终端目标" }).click();
  await expect(
    editDialog.getByText(
      "上游模型 ID 不能清空；如需改回入口模型 ID，请显式填写。",
    ),
  ).toBeVisible();
  expect(routes.mutationPaths).toHaveLength(0);

  // focus_connection_id focuses the target row and clears itself once.
  await page.goto("/models/7?focus_connection_id=16");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  await expect(page.getByTestId("access-target-92")).toHaveCount(1);
  await expect(page).toHaveURL(/\/models\/7$/);

  await page.getByLabel("选择目标模型").click();
  await page
    .getByRole("option", {
      name: "Internal Detail Target (internal-detail-target)",
    })
    .click();
  await page.getByRole("button", { name: "添加目标" }).click();

  await expect.poll(() => routes.targetPayloads.length).toBe(1);
  expect(routes.targetPayloads[0]).toEqual({
    target_type: "model",
    target_model_id: "internal-detail-target",
    position: 2,
    is_enabled: true,
  });
});

// Entry-model list journey extensions: sidebar/breadcrumb terminology, the
// URL-backed stats scope switch, and the identity flag filters. The exit
// mapping is asserted at the projection boundary (see
// src/features/models/modelExitMapping.test.ts); Playwright stays at the
// route-flow level per tests/e2e/AGENTS.md.
test("entry-model list journey: navigation, scope switch, and identity filters", async ({
  page,
}) => {
  await mockModelRoutes(page, {
    models: [
      // entry-with-targets: one Terminal Target whose upstream identity is
      // decoupled from the entry id (case-sensitive), plus a Model Target.
      {
        ...createModelListItem(1, "Entry-A", "Entry A"),
        access_targets: [
          {
            id: 301,
            target_type: "connection",
            target_model_id: null,
            connection_id: 311,
            terminal_target_id: 311,
            position: 0,
            is_enabled: true,
            target_model: null,
            connection: {
              id: 311,
              profile_id: 1,
              api_family: "openai",
              endpoint_id: 1,
              endpoint: {
                id: 1,
                name: "OpenAI Primary",
                base_url: "https://api.openai.com/v1",
              },
              is_active: true,
              priority: 0,
              name: "conn-311",
              auth_type: null,
              upstream_model_id: "entry-a",
              custom_headers: null,
              openai_text_capability: "dual_native",
              openai_image_capability: null,
              pricing_template_id: null,
              qps_limit: null,
              max_in_flight_non_stream: null,
              max_in_flight_stream: null,
              created_at: timestamp,
              updated_at: timestamp,
            },
            terminal_target: {
              id: 311,
              profile_id: 1,
              api_family: "openai",
              endpoint_id: 1,
              endpoint: {
                id: 1,
                name: "OpenAI Primary",
                base_url: "https://api.openai.com/v1",
              },
              is_active: true,
              priority: 0,
              name: "conn-311",
              auth_type: null,
              upstream_model_id: "entry-a",
              custom_headers: null,
              openai_text_capability: "dual_native",
              openai_image_capability: null,
              pricing_template_id: null,
              qps_limit: null,
              max_in_flight_non_stream: null,
              max_in_flight_stream: null,
              created_at: timestamp,
              updated_at: timestamp,
            },
            created_at: timestamp,
            updated_at: timestamp,
          },
          {
            id: 302,
            target_type: "model",
            target_model_id: "Entry-B",
            connection_id: null,
            terminal_target_id: null,
            position: 1,
            is_enabled: true,
            target_model: {
              id: 2,
              profile_id: 1,
              api_family: "openai",
              model_id: "Entry-B",
              display_name: "Entry B",
              openai_accepted_format: null,
              openai_image_operations: null,
              direct_request_enabled: true,
              incoming_model_target_count: 1,
              loadbalance_strategy_id: 11,
              is_enabled: true,
            },
            connection: null,
            terminal_target: null,
            created_at: timestamp,
            updated_at: timestamp,
          },
        ],
        routing_summary: {
          enabled_access_target_count: 2,
          total_access_target_count: 2,
          openai_mode: "dual_native",
          coverage: "full",
          operation_groups: [],
          single_truncated_access_target_ids: [],
          warning_codes: [],
        },
      },
      {
        ...createModelListItem(2, "Entry-B", "Entry B"),
        incoming_model_target_count: 1,
      },
    ],
  });

  // The sidebar names the management surface 入口模型, and the breadcrumb
  // carries the same fixed term under the routing group.
  await page.goto("/route/models");
  await expect(page.getByTestId("models-feature-page")).toBeVisible();
  await expect(
    page.getByRole("link", { name: new RegExp("^入口模型$") }),
  ).toHaveAttribute("href", /\/route\/models$/);
  await expect(page.getByTestId("shell-breadcrumb")).toContainText(
    "路由配置入口模型",
  );

  // Stats scope: a controlled single-select segmented control with the
  // attribution note; each selection round-trips through the scope URL.
  const scopeSwitch = page.getByTestId("models-scope-switcher");
  await expect(
    scopeSwitch.getByRole("radio", { name: "入口请求" }),
  ).toHaveAttribute("aria-checked", "true");
  await expect(page).not.toHaveURL(/scope=/);
  await scopeSwitch.getByRole("radio", { name: "最终承载" }).click();
  await expect(page).toHaveURL(/[?&]scope=final_execution/);
  await expect(
    scopeSwitch.getByRole("radio", { name: "最终承载" }),
  ).toHaveAttribute("aria-checked", "true");
  // Re-clicking the active scope must not clear the selection.
  await scopeSwitch.getByRole("radio", { name: "最终承载" }).click();
  await expect(
    scopeSwitch.getByRole("radio", { name: "最终承载" }),
  ).toHaveAttribute("aria-checked", "true");
  await scopeSwitch.getByRole("radio", { name: "路由尝试" }).click();
  await expect(page).toHaveURL(/[?&]scope=route_attempt/);
  // The scope note names the attribution basis for the active scope.
  await expect(
    page.getByText("口径：按实际路由尝试计数，失败重试也计入。"),
  ).toBeVisible();

  // Identity flag filter: upstream_decoupled matches the case-sensitive
  // decoupled entry and URL round-trips.
  await page.getByRole("combobox", { name: "身份筛选" }).click();
  await page.getByRole("option", { name: "上游 ID 已解耦" }).click();
  await expect(page).toHaveURL(/[?&]flag=upstream_decoupled/);
  await expect(page.getByTestId("models-table-row-1")).toBeVisible();
  await expect(page.getByTestId("models-table-row-2")).toHaveCount(0);

  // The model-target filter matches the entry carrying a Model Target row.
  await page.getByRole("combobox", { name: "身份筛选" }).click();
  await page.getByRole("option", { name: "包含模型目标" }).click();
  await expect(page).toHaveURL(/[?&]flag=has_model_target/);
  await expect(page.getByTestId("models-table-row-1")).toBeVisible();

  // Deep-linking the flag directly applies the same filter after reload.
  await page.reload();
  await expect(page.getByTestId("models-table-row-1")).toBeVisible();
  await expect(page.getByTestId("models-table-row-2")).toHaveCount(0);
});

test("model inventory view deep links switch between entries, targets, and all", async ({
  page,
}) => {
  await mockModelRoutes(page, {
    models: [
      createModelListItem(1, "entry-visible", "Entry Visible"),
      {
        ...createModelListItem(2, "internal-target", "Internal Target"),
        direct_request_enabled: false,
      },
    ],
  });

  await page.goto("/route/models?view=model_targets");
  const viewSwitch = page.getByRole("group", { name: "模型视图" });
  const kpiValues = page.locator(
    '[data-slot="kpi-card"] [data-slot="metric-value"]',
  );
  await expect(viewSwitch.getByRole("radio", { name: "仅模型目标" })).toBeChecked();
  await expect(page).toHaveURL(/view=model_targets/);
  await expect(page.getByTestId("models-table-row-1")).toHaveCount(0);
  await expect(page.getByTestId("models-table-row-2")).toHaveCount(1);
  await expect(kpiValues).toHaveText(["1", "1", "0", "0", "0"]);

  await page.reload();
  await expect(viewSwitch.getByRole("radio", { name: "仅模型目标" })).toBeChecked();
  await expect(page.getByTestId("models-table-row-2")).toHaveCount(1);

  await viewSwitch.getByRole("radio", { name: "全部模型配置" }).click();
  await expect(page).toHaveURL(/view=all/);
  await expect(page.getByTestId("models-table-row-1")).toHaveCount(1);
  await expect(page.getByTestId("models-table-row-2")).toHaveCount(1);
  await expect(kpiValues).toHaveText(["2", "2", "0", "0", "0"]);

  await viewSwitch.getByRole("radio", { name: "入口模型" }).click();
  await expect(page).not.toHaveURL(/view=/);
  await expect(page.getByTestId("models-table-row-1")).toHaveCount(1);
  await expect(page.getByTestId("models-table-row-2")).toHaveCount(0);
  await expect(kpiValues).toHaveText(["1", "1", "0", "0", "0"]);
});
