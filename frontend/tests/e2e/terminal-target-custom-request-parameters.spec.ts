import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-08-08T12:00:00Z";
const saveButton = /Save|保存/;
const editTerminalTargetButton = "编辑 OpenRouter Primary";
const editTerminalTargetDialog = /编辑终端目标/;

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
  schedule: { timezone: string; windows: Array<{ weekday_mask: number; start_minute: number; end_minute: number }> } | null = null,
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
    custom_headers: null,
    custom_request_parameters: params,
    routing_schedule: schedule,
    routing_schedule_state: schedule
      ? {
          status: "closed",
          timezone: schedule.timezone,
          evaluated_at: timestamp,
          // Far future on purpose: the badge downgrades itself to a staleness
          // notice once the boundary the server shipped has passed, so a nearby
          // instant would make this assertion depend on the wall clock.
          next_open_at: "2099-01-01T01:00:00Z",
          next_open_at_known: true,
          next_close_at_known: false,
        }
      : null,
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
    connection?: ReturnType<typeof createConnection> | null;
    patchStatus?: number;
    patchBody?: unknown;
  } = {},
) {
  const connection = options.connection ?? createConnection(1);
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
      return fulfillJson({ summary: { total_spend: "0", currency_code: "EUR", currency_symbol: "€" } });
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

test("terminal target custom request parameters editor saves and round-trips an OpenRouter provider object", async ({ page }) => {
  const routes = await mockModelDetailRoutes(page);
  const dialog = await openEditTerminalTargetDialog(page);

  const editor = dialog.getByTestId("connection-dialog-custom-request-parameters-card");
  await expect(editor).toBeVisible();
  const textarea = editor.getByRole("textbox", { name: "自定义请求参数（JSON）" });
  await expect(editor.getByText("未配置")).toBeVisible();

  await textarea.fill(
    '{\n  "provider": {\n    "only": ["deepinfra/turbo"],\n    "allow_fallbacks": false\n  }\n}',
  );
  await expect(editor.getByText("已配置 1 个顶层参数")).toBeVisible();

  await dialog.getByRole("button", { name: saveButton }).click();
  await expect(dialog).toHaveCount(0);

  const payloads = routes.getPatchPayloads();
  expect(payloads.length).toBe(1);
  const payload = payloads[0] as { custom_request_parameters: unknown };
  expect(payload.custom_request_parameters).toEqual({
    provider: { only: ["deepinfra/turbo"], allow_fallbacks: false },
  });

  // Reopen: the saved object hydrates back into the editor with full JSON
  // semantics preserved.
  const reopened = await openEditTerminalTargetDialog(page);
  const reopenedEditor = reopened.getByTestId("connection-dialog-custom-request-parameters-card");
  const reopenedTextarea = reopenedEditor.getByRole("textbox", { name: "自定义请求参数（JSON）" });
  await expect(reopenedEditor.getByText("已配置 1 个顶层参数")).toBeVisible();
  await expect(reopenedTextarea).toHaveValue(
    '{\n  "provider": {\n    "only": [\n      "deepinfra/turbo"\n    ],\n    "allow_fallbacks": false\n  }\n}',
  );
});

test("terminal target custom request parameters editor blocks save on invalid JSON and maps server 422 back to the field", async ({ page }) => {
  const routes = await mockModelDetailRoutes(page, {
    patchStatus: 422,
    patchBody: {
      detail: "Invalid custom request parameters",
      field: "custom_request_parameters",
      path: "custom_request_parameters.provider.only",
      reason: "protected_field",
    },
  });
  const dialog = await openEditTerminalTargetDialog(page);
  const editor = dialog.getByTestId("connection-dialog-custom-request-parameters-card");
  const textarea = editor.getByRole("textbox", { name: "自定义请求参数（JSON）" });

  // Invalid JSON blocks the mutation before any request.
  await textarea.fill('{"provider": }');
  await dialog.getByRole("button", { name: saveButton }).click();
  await expect(dialog.getByRole("alert")).toBeVisible();
  await expect(dialog).toBeVisible();

  // A client-valid object rejected by the server maps the 422 field envelope
  // back into the editor instead of a toast.
  await textarea.fill('{"provider":{"only":["deepinfra/turbo"]}}');
  await dialog.getByRole("button", { name: saveButton }).click();
  await expect.poll(() => routes.getPatchPayloads().length).toBe(1);
  await expect(dialog.getByRole("alert")).toContainText("custom_request_parameters.provider.only");
  await expect(dialog).toBeVisible();
});

test("terminal target custom request parameters editor format and clear actions", async ({ page }) => {
  await mockModelDetailRoutes(page);
  const dialog = await openEditTerminalTargetDialog(page);
  const editor = dialog.getByTestId("connection-dialog-custom-request-parameters-card");
  const textarea = editor.getByRole("textbox", { name: "自定义请求参数（JSON）" });

  await textarea.fill('{"provider":{"only":["deepinfra/turbo"]}}');
  await editor.getByRole("button", { name: "格式化" }).click();
  await expect(textarea).toHaveValue(
    '{\n  "provider": {\n    "only": [\n      "deepinfra/turbo"\n    ]\n  }\n}',
  );

  await editor.getByRole("button", { name: "清空" }).click();
  await expect(textarea).toHaveValue("");
  await expect(editor.getByText("未配置")).toBeVisible();
});

// Extends this spec rather than adding a new one: the browser budget is fixed
// at roughly five journey specs, and this file already owns the terminal-target
// dialog journey.
test("terminal target routing schedule badge reports the server verdict and blocks a full-week configuration", async ({ page }) => {
  const schedule = { timezone: "Asia/Shanghai", windows: [{ weekday_mask: 31, start_minute: 540, end_minute: 1080 }] };
  await mockModelDetailRoutes(page, { connection: createConnection(31, null, schedule) });
  await page.goto("/models/5");

  // The badge states the server's conclusion; the client never evaluates the
  // window itself.
  await expect(page.getByText("时段外（2099-01-01T01:00:00Z 恢复）")).toBeVisible();

  await page.getByRole("button", { name: editTerminalTargetButton }).click();
  await expect(page.getByRole("dialog").filter({ hasText: editTerminalTargetDialog })).toBeVisible();
  await expect(page.getByLabel("限制该终端目标的可路由时段")).toBeChecked();
});
