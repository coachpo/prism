import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T12:00:00Z";
const contextWindowHelperCopy = "Leave blank when the model context window is unknown.";
const contextWindowValidationCopy = "Context window tokens must be a positive integer or blank.";
const modelIdRequiredCopy = "Model ID is required.";
const outputReserveValidationCopy = "Default output token reserve must be a positive integer.";
const maxContextUtilizationValidationCopy = "Max context utilization must be a decimal greater than 0 and less than or equal to 1.";

function createProfile() {
  return {
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
}

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

function createModelListItem(id: number, modelId: string, displayName: string) {
  return {
    id,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: modelId,
    display_name: displayName,
    loadbalance_strategy_id: 11,
    loadbalance_strategy: createStrategy(),
    context_window_tokens: null,
    default_output_token_reserve: 4096,
    max_context_utilization: 0.9,
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

async function mockModelRoutes(page: Page) {
  const profile = createProfile();
  const strategies = [createStrategy()];
  const models = [
    createModelListItem(1, "target-alpha", "Target Alpha"),
    createModelListItem(2, "target-beta", "Target Beta"),
  ];
  const createdPayloads: unknown[] = [];

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
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
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
    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }
    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson(strategies);
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
        vendor_id: payload.vendor_id ?? null,
        vendor: null,
        api_family: payload.api_family,
        model_id: payload.model_id,
        display_name: payload.display_name,
        loadbalance_strategy_id: payload.loadbalance_strategy_id ?? null,
        loadbalance_strategy: createStrategy(),
        context_window_tokens: payload.context_window_tokens ?? null,
        default_output_token_reserve: payload.default_output_token_reserve ?? 4096,
        max_context_utilization: payload.max_context_utilization ?? 0.9,
        access_targets: payload.access_targets ?? [],
        is_enabled: payload.is_enabled ?? true,
        connections: [],
        created_at: timestamp,
        updated_at: timestamp,
      });
    }

    return fulfillJson({});
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getCreatedPayloads: () => createdPayloads,
  };
}

test("context-capability-authoring: submits parsed context routing defaults on create", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });
  await expect(dialog.getByText(contextWindowHelperCopy)).toBeVisible();

  await dialog.locator("#model-id").fill("context-routed-model");
  await dialog.locator("#model-context-window-tokens").fill("131072");
  await dialog.locator("#model-default-output-token-reserve").fill("8192");
  await dialog.locator("#model-max-context-utilization").fill("0.75");
  await dialog.getByRole("button", { name: "Save" }).click();

  await expect(page.getByText("Model created")).toBeVisible();
  expect(routes.getCreatedPayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "openai",
      model_id: "context-routed-model",
      display_name: "context-routed-model",
      access_targets: [],
      loadbalance_strategy_id: 11,
      is_enabled: false,
      context_window_tokens: 131072,
      default_output_token_reserve: 8192,
      max_context_utilization: 0.75,
    },
  ]);
});

test("context-capability-authoring: blocks invalid context routing defaults before save", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });

  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByText(modelIdRequiredCopy).first()).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#model-id").fill("invalid-context-model");
  await dialog.locator("#model-context-window-tokens").fill("0");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByText(contextWindowValidationCopy)).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#model-context-window-tokens").fill("");
  await dialog.locator("#model-default-output-token-reserve").fill("");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByText(outputReserveValidationCopy)).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#model-default-output-token-reserve").fill("4096");
  await dialog.locator("#model-max-context-utilization").fill("1.2");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByText(maxContextUtilizationValidationCopy)).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);
});
