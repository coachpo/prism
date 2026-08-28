import { expect, test, type Page } from "@playwright/test";
import {
  createEmptyIngressSpendingReport,
  expectIngressSpendingRequest,
} from "./spending-report-fixtures";

const timestamp = "2026-04-27T12:00:00Z";
const newModelButton = /New Model|新建模型/;
const newModelDialog = /New Model|新建模型/;
const editModelDialog = /Edit Model|编辑模型/;
const cancelButton = /Cancel|取消/;
const createDefaultsButton = /Create Defaults|创建默认策略/;
const modelIdLabel = /Model ID|模型 ID/;
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
      return fulfillJson([]);
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

test("models table switches three metric scopes without refetching its composite snapshot", async ({
  page,
}) => {
  await mockModelRoutes(page, {
    models: [
      createModelListItem(1, "entry-a", "Entry A"),
      createModelListItem(2, "target-c", "Target C"),
    ],
  });
  let metricReads = 0;
  const metricBlock = (
    scope: "ingress" | "final_execution" | "route_attempt",
    requestCount: number,
    knownCostMicros: number | null,
  ) => ({
    request_count: requestCount,
    success_rate: requestCount === 0 ? null : 100,
    p95_latency_ms: requestCount === 0 ? null : 610,
    known_cost_micros: knownCostMicros,
    caliber: {
      scope,
      grain: scope,
      identity_basis: scope,
      outcome_basis:
        scope === "route_attempt" ? "attempt" : "finalized_ingress",
      latency_basis: "attempt_duration",
      cost_basis: scope === "route_attempt" ? "none" : "trusted_cost",
      datasets: [],
    },
    samples: {
      observation_count: requestCount,
      latency_sample_count: requestCount,
      latency_missing_count: 0,
      cost_sample_count: knownCostMicros === null ? 0 : requestCount,
      cost_missing_count: knownCostMicros === null ? requestCount : 0,
    },
  });
  await page.route("**/api/stats/models/metrics", (route) => {
    metricReads += 1;
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        items: [
          {
            model_id: "entry-a",
            ingress: metricBlock("ingress", 1, 5_000),
            final_execution: metricBlock("final_execution", 0, 0),
            route_attempt: metricBlock("route_attempt", 0, null),
          },
          {
            model_id: "target-c",
            ingress: metricBlock("ingress", 0, 0),
            final_execution: metricBlock("final_execution", 1, 4_000),
            route_attempt: metricBlock("route_attempt", 1, null),
          },
        ],
        coverage: { quality: {}, spending: {} },
      }),
    });
  });

  await page.goto("/models");
  await expect(
    page.getByRole("columnheader", { name: /^入口请求 24h/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("columnheader", { name: /^请求总已知成本 30d/ }),
  ).toBeVisible();
  await expect.poll(() => metricReads).toBe(1);

  await page.getByRole("tab", { name: "最终承载" }).click();
  await expect(page).toHaveURL(/scope=final_execution/);
  await expect(
    page.getByRole("columnheader", { name: /^承载请求 24h/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("columnheader", { name: /^归属已知成本 30d/ }),
  ).toBeVisible();
  expect(metricReads).toBe(1);

  await page.getByRole("tab", { name: "路由尝试" }).click();
  await expect(page).toHaveURL(/scope=route_attempt/);
  await expect(
    page.getByRole("columnheader", { name: /^尝试数 24h/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("columnheader", { name: /^尝试成本 30d/ }),
  ).toBeVisible();
  await expect(
    page.getByTitle(
      "路由尝试口径不声明成本；失败尝试是否产生上游费用未知。",
    ).first(),
  ).toBeVisible();
  expect(metricReads).toBe(1);

  await page.getByRole("tab", { name: "入口" }).click();
  await expect(page).not.toHaveURL(/scope=/);
});

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
      name: /Edit Model: Target Alpha|编辑模型: Target Alpha/,
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

  await dialog.getByRole("button", { name: createDefaultsButton }).click();

  await expect(
    dialog.getByRole("button", { name: createDefaultsButton }),
  ).toHaveCount(0);
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
      name: /Edit Model: Edit No Strategy|编辑模型: Edit No Strategy/,
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

test("create model dialog saves a configure-later targetless disabled draft", async ({
  page,
}) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  await page.getByRole("textbox", { name: modelIdLabel }).fill("draft-openai");
  await dialog.getByRole("switch", { name: /稍后配置/ }).check();
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
    },
  ]);
});

test("create model dialog keeps legacy access-target authoring out of the create flow", async ({
  page,
}) => {
  await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: newModelButton }).click();

  const dialog = page.getByRole("dialog", { name: newModelDialog });
  await expect(dialog.locator("#access-target-select")).toHaveCount(0);
  await expect(
    dialog.getByRole("button", { name: /New terminal target|新建终端目标/ }),
  ).toHaveCount(0);
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
  const resetRequests: string[] = [];
  const currentStateModelIdQueries: (string | null)[] = [];

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
    if (pathname === "/api/models/7/routing-diagnostics") {
      return fulfillJson(createDetailDiagnostics());
    }
    if (pathname === "/api/loadbalance/current-state") {
      // Record the filter the page actually sends. The read model compares it
      // against `model_configs.model_id`, so a numeric config id here returns an
      // empty cohort that the table would render as "never observed".
      currentStateModelIdQueries.push(
        new URL(request.url()).searchParams.get("model_id"),
      );
      // Mirror the shipped response shape: target identity lives in
      // `terminal_target`, and completeness/has_more ride alongside the items.
      const observedRow = (
        terminalTargetId: number,
        label: string,
        overrides: Record<string, unknown>,
      ) => ({
        model: {
          model_config_id: 7,
          id: "detail-openai",
          label: "Detail OpenAI",
          configured: true,
        },
        endpoint: { id: 1, label: "OpenAI Primary", configured: true },
        terminal_target: { id: terminalTargetId, label, configured: true },
        observation_state: "observed",
        available: true,
        qps_window_started_at: null,
        qps_window_request_count: 0,
        in_flight_non_stream: 0,
        in_flight_stream: 0,
        cycle_retry_attempts: 0,
        cumulative_retry_attempts: 0,
        next_retry_at: null,
        last_retry_delay_ms: 0,
        ban_mode: "off",
        banned_until_at: null,
        last_failure_kind: null,
        last_success_at: "2026-08-08T11:30:00Z",
        last_success_response_headers_latency_ms: 412,
        state: "available",
        created_at: "2026-08-08T10:00:00Z",
        updated_at: "2026-08-08T11:59:00Z",
        ...overrides,
      });
      return fulfillJson({
        generated_at: "2026-08-08T12:00:00Z",
        scope: "process",
        instance_id: "e2e-instance",
        configuration_revision: "1",
        completeness: {
          state: "ready",
          complete: true,
          configured_target_count: 2,
          observed_target_count: 2,
          unobserved_target_count: 0,
          observed_subset_counts: {},
        },
        items: [
          observedRow(15, "Primary Chat", {
            in_flight_non_stream: 1,
            cycle_retry_attempts: 2,
            cumulative_retry_attempts: 2,
            next_retry_at: "2026-08-08T12:05:00Z",
            last_retry_delay_ms: 60000,
            last_failure_kind: "transient_http",
            state: "retry_wait",
            available: false,
          }),
          observedRow(16, "Fallback Dual", {
            cycle_retry_attempts: 4,
            cumulative_retry_attempts: 4,
            last_retry_delay_ms: 900000,
            ban_mode: "until_reset",
            last_failure_kind: "connect_error",
            last_success_at: "2026-08-08T08:00:00Z",
            last_success_response_headers_latency_ms: 980,
            state: "banned",
            available: false,
            created_at: "2026-08-08T07:00:00Z",
            updated_at: "2026-08-08T08:30:00Z",
          }),
        ],
        has_more: false,
        next_cursor: null,
      });
    }
    if (pathname === "/api/models/7" && request.method() === "GET") {
      return fulfillJson(modelDetail);
    }
    if (pathname === "/api/models/7/connections") {
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
    if (
      pathname === "/api/loadbalance/current-state/16/reset" &&
      request.method() === "POST"
    ) {
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

  return {
    getResetRequests: () => resetRequests,
    getCurrentStateModelIdQueries: () => currentStateModelIdQueries,
  };
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
  await expect(menu.getByRole("menuitem", { name: /查看模型/ })).toHaveCount(0);
  await expect(
    menu.getByRole("menuitem", { name: /复制终端目标 Primary Responses/ }),
  ).toBeVisible();
  await page.keyboard.press("Escape");

  // The model-target row offers the entry and lands on the canonical detail
  // URL of the TARGET config id (17), not this row's or option list's ids.
  await page
    .getByTestId("access-target-162")
    .getByRole("button", { name: /更多操作/ })
    .click();
  menu = page.getByRole("menu");
  const viewEntry = menu.getByRole("menuitem", {
    name: /查看模型 Beta Detail 的详情/,
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

test("model detail shows mixed order, single truncation and cooldown reset", async ({
  page,
}) => {
  const routes = await mockModelDetailRoutes(page);

  await page.goto("/models/7");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });

  await expect(page.getByTestId("access-targets-mixed-list")).toBeVisible();
  await expect(
    page.getByText("只会尝试第 1 个已启用目标；其余 1 个不会用于故障转移。"),
  ).toBeVisible();

  const rows = page
    .getByTestId("access-targets-mixed-list")
    .locator("[data-testid^='access-target-']");
  await expect(rows).toHaveCount(2);
  await expect(rows.nth(0)).toHaveAttribute("data-testid", "access-target-91");
  await expect(rows.nth(1)).toHaveAttribute("data-testid", "access-target-92");
  await expect(rows.nth(0)).toContainText("Primary Responses");
  await expect(rows.nth(1)).toContainText("Fallback Dual");

  // The runtime column is only trustworthy if the cohort filter addresses the
  // model the way the read model indexes it: by public model id, not by the
  // numeric config id in the route.
  expect(routes.getCurrentStateModelIdQueries().length).toBeGreaterThan(0);
  expect(new Set(routes.getCurrentStateModelIdQueries())).toEqual(
    new Set(["detail-openai"]),
  );

  const bannedRow = page.getByTestId("access-target-92");
  await expect(bannedRow.getByText(/冷却\/封禁中/)).toBeVisible();
  await expect(
    bannedRow.getByRole("button", { name: "重置冷却" }),
  ).toBeVisible();
  await bannedRow.getByRole("button", { name: "重置冷却" }).click();
  expect(routes.getResetRequests()).toHaveLength(1);
  await expect(bannedRow.getByText("当前无冷却限制")).toBeVisible();
});

test("model detail canonicalizes dead tab and one-shot target actions", async ({
  page,
}) => {
  await mockModelDetailRoutes(page);

  // Model detail no longer exposes a tab in the canonical URL; unsupported
  // legacy tab state is removed while one-shot actions remain supported.
  await page.goto("/models/7?tab=connections");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  await expect(page).toHaveURL(/\/models\/7$/);

  // One-shot create action opens the dialog and clears itself (replace), so a
  // refresh never reopens it.
  await page.goto("/models/7?action=create-terminal-target&endpoint_id=1");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  const dialog = page.getByRole("dialog", { name: /终端目标|Terminal Target/ });
  await expect(dialog).toBeVisible();
  await expect(page).toHaveURL(/\/models\/7$/);
  await page.reload();
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  await expect(
    page.getByRole("dialog", { name: /终端目标|Terminal Target/ }),
  ).toHaveCount(0);

  // focus_connection_id focuses the target row and clears itself once.
  await page.goto("/models/7?focus_connection_id=16");
  await page
    .getByTestId("model-detail-feature-page")
    .waitFor({ timeout: 15000 });
  await expect(page.getByTestId("access-target-92")).toHaveCount(1);
  await expect(page).toHaveURL(/\/models\/7$/);
});

test("endpoint journey: direct references, fail-closed 503, blocked delete, rotation, verify and no reorder", async ({
  page,
}) => {
  const timestamp = "2026-08-09T12:00:00Z";
  const endpointOne = {
    id: 21,
    profile_id: 1,
    name: "endpoint-one",
    base_url: "https://one.example",
    has_api_key: true,
    api_key_fingerprint: "fp_v1_ab12cd34ef56",
    api_key_updated_at: timestamp,
    config_revision: 1,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const endpointTwo = {
    id: 22,
    profile_id: 1,
    name: "endpoint-two",
    base_url: "https://two.example",
    has_api_key: false,
    api_key_fingerprint: null,
    api_key_updated_at: null,
    config_revision: 1,
    created_at: timestamp,
    updated_at: timestamp,
  };
  const referencedItem = {
    kind: "owned_terminal_target",
    connection_id: 91,
    terminal_target_id: 91,
    terminal_target_name: "Terminal One",
    api_family: "openai",
    connection_is_active: true,
    access_target: { id: 512, position: 0, is_enabled: true },
    owner_model: {
      id: 7,
      model_id: "gpt-4o",
      display_name: "Primary GPT",
      is_enabled: true,
      openai_accepted_format: "dual_native",
    },
    openai_text_capability: "dual_native",
    pricing_template: {
      id: 2,
      name: "Default",
      current_revision_id: null,
      current_version: 3,
    },
    enabled: true,
    inactive_reasons: [],
  };
  const orphanItem = {
    kind: "orphan_connection",
    connection_id: 99,
    terminal_target_id: 99,
    terminal_target_name: null,
    api_family: "openai",
    connection_is_active: false,
    access_target: null,
    owner_model: null,
    openai_text_capability: "dual_native",
    pricing_template: null,
    enabled: false,
    inactive_reasons: ["orphaned"],
  };
  const summary = (direct: number, enabled: number, orphan = 0) => ({
    direct_reference_count: direct,
    referencing_model_count: direct > 0 ? 1 : 0,
    enabled_reference_count: enabled,
    orphan_reference_count: orphan,
  });
  let referencesMode: "ready" | "error" = "ready";
  let rotated = false;
  const deleteAttempts: string[] = [];
  const verifyRequests: Array<{ body: unknown }> = [];

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
    if (pathname === "/api/auth/status")
      return fulfillJson({
        state: "disabled",
        transition_state: null,
        login_available: false,
        effective_generation: "1",
        retry_after_seconds: null,
      });
    if (pathname === "/api/settings/costing")
      return fulfillJson({
        report_currency_code: "EUR",
        report_currency_symbol: "€",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    if (pathname === "/api/settings/timezone")
      return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/endpoints" && request.method() === "GET") {
      const list = rotated
        ? [
            {
              ...endpointOne,
              api_key_fingerprint: "fp_v1_9999aaaa0000",
              api_key_updated_at: "2026-08-09T13:00:00Z",
              config_revision: 2,
            },
            endpointTwo,
          ]
        : [endpointOne, endpointTwo];
      return fulfillJson(list);
    }
    if (
      pathname === "/api/endpoints/references/batch" &&
      request.method() === "POST"
    ) {
      if (referencesMode === "error") {
        return fulfillJson({ detail: "upstream unavailable" }, 503);
      }
      return fulfillJson({
        items: [
          { endpoint_id: 21, summary: summary(1, 1) },
          { endpoint_id: 22, summary: summary(0, 0) },
        ],
      });
    }
    if (
      pathname === "/api/endpoints/21/references" &&
      request.method() === "GET"
    ) {
      if (referencesMode === "error") {
        return fulfillJson({ detail: "upstream unavailable" }, 503);
      }
      return fulfillJson({
        endpoint_id: 21,
        summary: summary(2, 1, 1),
        reference_page: {
          items: [referencedItem, orphanItem],
          total_count: 2,
          next_cursor: null,
          reference_snapshot_hash: "opaque-hash-1",
        },
      });
    }
    if (pathname === "/api/endpoints/21" && request.method() === "DELETE") {
      deleteAttempts.push(pathname);
      return fulfillJson(
        {
          detail: {
            code: "endpoint_in_use",
            message: "Endpoint is referenced by Terminal Targets",
            endpoint_id: 21,
            summary: summary(2, 1, 1),
            reference_page: {
              items: [referencedItem, orphanItem],
              total_count: 2,
              next_cursor: null,
              reference_snapshot_hash: "opaque-hash-1",
            },
            references_url: "/api/endpoints/21/references",
          },
        },
        409,
      );
    }
    if (
      pathname === "/api/endpoints/21/orphan-connections/99" &&
      request.method() === "DELETE"
    ) {
      return fulfillJson({ deleted: true, connection_id: 99 });
    }
    if (
      pathname === "/api/endpoints/21/verify" &&
      request.method() === "POST"
    ) {
      const body = request.postDataJSON();
      verifyRequests.push({ body });
      return fulfillJson({
        endpoint_id: 21,
        api_family: body.api_family,
        config_revision: 1,
        api_key_fingerprint: "fp_v1_ab12cd34ef56",
        is_current: true,
        outcome: "verified",
        probe_path: "/v1/models",
        upstream_status: 200,
        duration_ms: 120,
        error_summary: null,
      });
    }
    if (pathname === "/api/endpoints/21" && request.method() === "PUT") {
      const body = request.postDataJSON();
      const updated = {
        ...endpointOne,
        name: body.name ?? endpointOne.name,
        base_url: body.base_url ?? endpointOne.base_url,
        api_key_updated_at: endpointOne.api_key_updated_at,
      };
      return fulfillJson(updated);
    }
    if (
      pathname === "/api/endpoints/21/duplicate" &&
      request.method() === "POST"
    ) {
      return fulfillJson({
        ...endpointOne,
        id: 23,
        name: "endpoint-one copy",
        api_key_updated_at: timestamp,
      });
    }
    return route.continue();
  });

  await page.goto("/route/endpoints");
  const table = page.getByTestId("endpoints-table-desktop");

  // 1. Compact table with fingerprint identity and direct-reference counts.
  await expect(table.getByText("endpoint-one")).toBeVisible();
  await expect(table.getByText("fp_v1_ab12cd34ef56")).toBeVisible();
  await expect(table.getByText(/1 个终端目标/)).toBeVisible();
  await expect(table.getByText("无直接引用")).toBeVisible();
  // 2. No reorder controls exist.
  await expect(page.getByRole("button", { name: /上移端点/ })).toHaveCount(0);
  await expect(page.getByRole("button", { name: /下移端点/ })).toHaveCount(0);

  // 3. Fail-closed: batch 503 shows failure, never a fake zero.
  referencesMode = "error";
  await page.reload();
  await expect(table.getByText("引用未知").first()).toBeVisible();
  await expect(page.getByText("无直接引用")).toHaveCount(0);

  // 4. Blocked delete: fresh preflight surfaces the typed blocker.
  referencesMode = "ready";
  await page.reload();
  const row = page.getByTestId("endpoint-row-21");
  await row.getByRole("button", { name: /确定要删除/ }).click();
  await expect(page.getByTestId("delete-blocked-heading")).toBeVisible();
  await expect(page.getByText(/共有 2 个终端目标直接引用此端点/)).toBeVisible();
  await expect(page.getByTestId("delete-blocker-91")).toBeVisible();
  await expect(page.getByTestId("delete-blocker-99")).toBeVisible();
  await expect(page.getByTestId("delete-endpoint-confirm")).toHaveCount(0);
  await page.getByRole("button", { name: /取消/ }).click();

  // 5. Orphan cleanup runs its own destructive confirmation.
  await row.getByRole("button", { name: /确定要删除/ }).click();
  await page
    .getByTestId("delete-blocker-99")
    .getByRole("button", { name: /清理孤立配置/ })
    .click();
  await expect(page.getByTestId("orphan-cleanup-confirm")).toBeVisible();
  await page.getByTestId("orphan-cleanup-confirm").click();
  await expect(page.getByText("孤立终端配置已清理")).toBeVisible();

  // 6. Key rotation evidence: rotated fingerprint/time/revision from server.
  rotated = true;
  await page.reload();
  await expect(table.getByText("fp_v1_9999aaaa0000")).toBeVisible();

  // 7. Save-and-verify: two ordered phases, dual result inline.
  await row.getByRole("button", { name: /编辑端点/ }).click();
  await page.getByRole("button", { name: /保存并验证/ }).click();
  await expect(page.getByTestId("verify-section")).toBeVisible();
  await page.getByTestId("verify-section").getByRole("combobox").click();
  await page.getByRole("option", { name: "OpenAI" }).click();
  await expect(page.getByTestId("endpoint-save-only")).toBeEnabled();
  await page.getByTestId("endpoint-save-only").click();
  await expect(page.getByTestId("verify-result")).toBeVisible();
  await expect(page.getByText(/验证请求成功/)).toBeVisible();
  expect(verifyRequests).toEqual([
    { body: { api_family: "openai", expected_config_revision: 1 } },
  ]);
  await page.getByRole("button", { name: /取消/ }).click();

  // 8. DELETE race: lock-time 409 replaces the dialog state with the latest
  //    blocker page; no stale deletion path.
  const endpointThree = {
    id: 24,
    profile_id: 1,
    name: "endpoint-three",
    base_url: "https://three.example",
    has_api_key: false,
    api_key_fingerprint: null,
    api_key_updated_at: null,
    config_revision: 1,
    created_at: timestamp,
    updated_at: timestamp,
  };
  await page.route("**/api/endpoints", async (route) => {
    if (route.request().method() === "GET") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify([
          {
            ...endpointOne,
            api_key_fingerprint: "fp_v1_ab12cd34ef56",
            config_revision: 1,
          },
          endpointTwo,
          endpointThree,
        ]),
      });
    }
    return route.continue();
  });
  await page.route("**/api/endpoints/24/references", async (route) => {
    if (route.request().method() === "GET") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          endpoint_id: 24,
          summary: summary(0, 0),
          reference_page: {
            items: [],
            total_count: 0,
            next_cursor: null,
            reference_snapshot_hash: "opaque-hash-zero",
          },
        }),
      });
    }
    return route.continue();
  });
  let raceReturned409 = false;
  await page.route("**/api/endpoints/24", async (route) => {
    if (route.request().method() === "DELETE") {
      raceReturned409 = true;
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          detail: {
            code: "endpoint_in_use",
            message: "Endpoint is referenced by Terminal Targets",
            endpoint_id: 24,
            summary: summary(1, 1),
            reference_page: {
              items: [referencedItem],
              total_count: 1,
              next_cursor: null,
              reference_snapshot_hash: "opaque-hash-race",
            },
            references_url: "/api/endpoints/24/references",
          },
        }),
      });
    }
    return route.continue();
  });
  await page.reload();
  const threeRow = page.getByTestId("endpoint-row-24");
  await threeRow.getByRole("button", { name: /确定要删除/ }).click();
  await expect(page.getByTestId("delete-endpoint-confirm")).toBeVisible();
  await page.getByTestId("delete-endpoint-confirm").click();
  await expect(page.getByTestId("delete-blocked-heading")).toBeVisible();
  await expect(page.getByText(/共有 1 个终端目标直接引用此端点/)).toBeVisible();
  expect(raceReturned409).toBe(true);
});
