import { expect, test, type Locator, type Page } from "@playwright/test";
import {
  createEmptyIngressSpendingReport,
  expectIngressSpendingRequest,
} from "./spending-report-fixtures";
import {
  createUnavailablePiModelRead,
  createUnboundModelsDevCatalog,
} from "./model-detail-catalog-fixtures";

const timestamp = "2026-08-08T12:00:00Z";
const saveButton = /Save|保存/;
const editTerminalTargetButton = "编辑 OpenRouter Primary";
const editTerminalTargetDialog = /编辑服务连接/;

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

function createEndpoint(id: number, name: string) {
  return {
    id,
    profile_id: 1,
    name,
    base_url: "https://openrouter.example.test/v1",
    has_api_key: true,
    masked_api_key: "sk-…abcd",
    position: id - 1,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createConnection(
  id: number,
  params: Record<string, unknown> | null = null,
) {
  return {
    id,
    profile_id: 1,
    model_config_id: 5,
    api_family: "openai",
    endpoint_id: 1,
    endpoint: createEndpoint(1, "OpenRouter"),
    is_active: true,
    priority: 0,
    name: "OpenRouter Primary",
    auth_type: "openai",
    upstream_model_id: "router-model",
    custom_headers: null,
    custom_request_parameters: params,
    routing_schedule: null,
    routing_schedule_state: null,
    openai_text_capability: "dual_native",
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelDetail(connection: ReturnType<typeof createConnection> | null) {
  return {
    id: 5,
    profile_id: 1,
    api_family: "openai",
    model_id: "router-model",
    display_name: "Router Model",
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    direct_request_enabled: true,
    incoming_model_target_count: 0,
    configuration_warnings: [],
    loadbalance_strategy_id: 11,
    loadbalance_strategy: createStrategy(),
    access_targets: connection
      ? [
          {
            id: 101,
            target_type: "connection",
            target_model_id: null,
            connection_id: connection.id,
            terminal_target_id: connection.id,
            position: 0,
            is_enabled: true,
            target_model: null,
            connection,
            terminal_target: connection,
            created_at: timestamp,
            updated_at: timestamp,
          },
        ]
      : [],
    is_enabled: true,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

async function mockModelDetailRoutes(
  page: Page,
  options: {
    patchStatus?: number;
    patchBody?: unknown;
    parameters?: Record<string, unknown>;
  } = {},
) {
  const connection = createConnection(1, options.parameters ?? null);
  const patchPayloads: unknown[] = [];
  let updatedConnection = connection;

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }
    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson([createStrategy()]);
    }
    if (pathname === "/api/endpoints") {
      return fulfillJson([createEndpoint(1, "OpenRouter")]);
    }
    if (pathname === "/api/endpoints/connections") {
      return fulfillJson({ items: [{ id: 1, endpoint_id: 1, name: "OpenRouter" }] });
    }
    if (pathname === "/api/models") {
      return fulfillJson([createModelDetail(updatedConnection)]);
    }
    if (pathname === "/api/pricing-templates") {
      return fulfillJson([]);
    }
    if (pathname === "/api/connections") {
      return fulfillJson([updatedConnection]);
    }
    if (pathname === "/api/stats/models/metrics") {
      return fulfillJson({ items: [] });
    }
    if (pathname === "/api/stats/spending") {
      expectIngressSpendingRequest(request, "router-model");
      return fulfillJson(
        createEmptyIngressSpendingReport({ currencyCode: "EUR", currencySymbol: "€" }),
      );
    }
    if (pathname === "/api/loadbalance/current-state") {
      return fulfillJson({ items: [] });
    }
    if (pathname === "/api/models/5" && request.method() === "GET") {
      return fulfillJson(createModelDetail(updatedConnection));
    }
    if (pathname === "/api/models/5/routing-diagnostics") {
      return fulfillJson({
        model_config_id: 5,
        strategy: { id: 11, type: "fill-first" },
        accepted_operations: [],
        stages: [],
        operation_coverage: [],
        configuration_warnings: [],
      });
    }
    if (pathname === "/api/models/5/catalog" && request.method() === "GET") {
      return fulfillJson(createUnboundModelsDevCatalog());
    }
    if (pathname === "/api/models/5/pi" && request.method() === "GET") {
      return fulfillJson(
        createUnavailablePiModelRead({
          modelConfigId: 5,
          modelId: "router-model",
        }),
      );
    }
    if (pathname === "/api/models/5/connections" && request.method() === "GET") {
      return fulfillJson([updatedConnection]);
    }
    if (pathname === "/api/models/5/connections/1" && request.method() === "PATCH") {
      const payload = request.postDataJSON();
      patchPayloads.push(payload);
      if (options.patchStatus && options.patchStatus >= 400) {
        return route.fulfill({
          status: options.patchStatus,
          contentType: "application/json",
          body: JSON.stringify(options.patchBody ?? { detail: "Invalid custom request parameters" }),
        });
      }
      updatedConnection = { ...updatedConnection, ...payload };
      return fulfillJson({
        connection: updatedConnection,
        access_targets: [],
        configuration_warnings: [],
      });
    }
    if (pathname === "/api/models/5/targets" && request.method() === "GET") {
      return fulfillJson(createModelDetail(updatedConnection).access_targets);
    }

    return fulfillJson({});
  });

  return { getPatchPayloads: () => patchPayloads };
}

async function openEditTerminalTargetDialog(page: Page) {
  await page.goto("/models/5");
  await expect(page.getByRole("heading", { name: "Router Model" })).toBeVisible();
  const dialog = page.getByRole("dialog", { name: editTerminalTargetDialog });
  await page.getByRole("button", { name: editTerminalTargetButton }).click();
  await expect(dialog).toBeVisible();
  return dialog;
}

/**
 * The custom request parameters editor lives inside the dialog's 高级请求设置
 * disclosure group. That group opens by default only when the Terminal Target
 * already carries a limiter, a custom header, or custom parameters; otherwise it
 * stays folded and its summary line states what is folded away. Assert the
 * summary and the disclosure state, then reach the editor the way an operator
 * does.
 */
async function revealCustomRequestParametersEditor(
  dialog: Locator,
  options: { summary: string; expanded: boolean },
) {
  const trigger = dialog.getByRole("button", { name: /^高级请求设置/ });
  await expect(trigger).toHaveAccessibleName(`高级请求设置 ${options.summary}`);
  await expect(trigger).toHaveAttribute(
    "aria-expanded",
    options.expanded ? "true" : "false",
  );
  if (!options.expanded) {
    await trigger.click();
    await expect(trigger).toHaveAttribute("aria-expanded", "true");
  }

  const editor = dialog.getByTestId("connection-dialog-custom-request-parameters-card");
  await expect(editor).toBeVisible();
  return editor;
}

test("service settings editor edits nested values and round-trips the complete configuration", async ({ page }) => {
  const routes = await mockModelDetailRoutes(page, { parameters: { provider: { only: ["example/previous"], allow_fallbacks: false }, options: [null, 1.5, true], empty_group: {} } });
  const dialog = await openEditTerminalTargetDialog(page);
  const editor = await revealCustomRequestParametersEditor(dialog, {
    summary: "未限流 · 0 个请求头 · 有自定义参数",
    expanded: true,
  });
  await editor.getByRole("textbox", { name: "服务附加设置 · provider · only · 第 1 项" }).fill("deepinfra/turbo");
  await expect(editor.locator('textarea[name="custom_request_parameters"]')).toHaveCount(0);
  await dialog.getByRole("button", { name: saveButton }).click();
  await expect(dialog).toHaveCount(0);
  expect(routes.getPatchPayloads()).toHaveLength(1);
  expect(routes.getPatchPayloads()[0]).toMatchObject({ custom_request_parameters: {
    provider: { only: ["deepinfra/turbo"], allow_fallbacks: false }, options: [null, 1.5, true], empty_group: {},
  } });
  const reopened = await openEditTerminalTargetDialog(page);
  const reopenedEditor = await revealCustomRequestParametersEditor(reopened, { summary: "未限流 · 0 个请求头 · 有自定义参数", expanded: true });
  await expect(reopenedEditor.getByRole("textbox", { name: "服务附加设置 · provider · only · 第 1 项" })).toHaveValue("deepinfra/turbo");
  await expect(reopenedEditor.getByRole("textbox", { name: "服务附加设置 · options · 第 2 项" })).toHaveValue("1.5");
});

test("service settings block incomplete numbers and show an actionable server rejection", async ({ page }) => {
  const routes = await mockModelDetailRoutes(page, { patchStatus: 422, patchBody: {
    detail: "Invalid custom request parameters", field: "custom_request_parameters", path: "custom_request_parameters.temperature", reason: "protected_field",
  } });
  const dialog = await openEditTerminalTargetDialog(page);
  const editor = await revealCustomRequestParametersEditor(dialog, { summary: "未限流 · 0 个请求头 · 无自定义参数", expanded: false });
  await editor.getByRole("button", { name: "添加设置" }).click();
  await page.getByRole("menuitem", { name: "数字", exact: true }).click();
  await editor.getByRole("textbox", { name: "设置名称", exact: true }).fill("temperature");
  const value = editor.getByRole("textbox", { name: "服务附加设置 · temperature" });
  await value.fill("invalid");
  await dialog.getByRole("button", { name: saveButton }).click();
  await expect(editor.getByText("请填写有效且可表示的数字。")).toBeVisible();
  expect(routes.getPatchPayloads()).toHaveLength(0);
  await value.fill("0.5");
  await dialog.getByRole("button", { name: saveButton }).click();
  await expect.poll(() => routes.getPatchPayloads().length).toBe(1);
  await expect(dialog.getByRole("alert")).toContainText("此名称不能用于附加设置");
  await expect(dialog.getByRole("alert")).not.toContainText("custom_request_parameters");
  await expect(value).toHaveValue("0.5");
  await expect(dialog).toBeVisible();
});

// This dialog is the tallest form in the console and the only e2e that opens
// it, so its scroll contract is asserted here. The body scrolls inside a
// ScrollArea whose height comes from the surrounding flex column; a viewport
// sized with a percentage height cannot resolve against that and grows to the
// full content height instead, which the ScrollArea root then clips away —
// the operator sees a form cut off mid-field with no way to reach the footer.
test("terminal target dialog scrolls to the bottom of the form on a short viewport", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 600 });
  await mockModelDetailRoutes(page);
  const dialog = await openEditTerminalTargetDialog(page);

  const scroll = await dialog.evaluate((node) => {
    const root = node.querySelector('[data-slot="scroll-area"]');
    const viewport = node.querySelector('[data-slot="scroll-area-viewport"]');
    if (!root || !viewport) return null;
    viewport.scrollTop = viewport.scrollHeight;
    return {
      overflowing: viewport.scrollHeight > viewport.clientHeight,
      // A viewport taller than its scroll area is content clipped out of reach.
      clipped: viewport.clientHeight > root.clientHeight,
      reachedBottom:
        viewport.scrollTop === viewport.scrollHeight - viewport.clientHeight,
    };
  });
  expect(scroll).toEqual({
    overflowing: true,
    clipped: false,
    reachedBottom: true,
  });

  // The routing-schedule block is the last thing in the form: reaching the
  // bottom has to actually put it on screen, next to the footer buttons.
  await expect(
    dialog.getByText("限制该服务连接的可路由时段"),
  ).toBeInViewport();
  await expect(dialog.getByRole("button", { name: saveButton })).toBeInViewport();
});
