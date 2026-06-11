import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-28T12:00:00Z";
const modelConfigId = 50;
const contextWindowHelperCopy = "Leave blank when the model context window is unknown.";
const modelIdRequiredCopy = "Model ID is required.";
const outputReserveValidationCopy = "Default output token reserve must be a positive integer.";
const maxContextUtilizationValidationCopy = "Max context utilization must be a decimal greater than 0 and less than or equal to 1.";
const preferredContextUtilizationThresholdValidationCopy = "Preferred context utilization threshold must be a decimal greater than 0 and less than or equal to 1.";
const preferredThresholdExceedsMaxValidationCopy = "Preferred context utilization threshold must be less than or equal to max context utilization.";

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
    attached_model_count: 1,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createAccessTarget(targetModelId: string, position: number, displayName: string) {
  return {
    id: 700 + position,
    target_type: "model",
    target_model_id: targetModelId,
    connection_id: null,
    position,
    weight: 1,
    target_priority: 0,
    is_enabled: true,
    target_model: {
      id: 100 + position,
      profile_id: 1,
      vendor_id: null,
      api_family: "openai",
      model_id: targetModelId,
      display_name: displayName,
      loadbalance_strategy_id: 11,
      is_enabled: true,
    },
    connection: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelRecord(
  contextWindowTokens: number | null,
  defaultOutputTokenReserve: number,
  maxContextUtilization: number,
  preferredContextUtilizationThreshold: number | null,
) {
  return {
    id: modelConfigId,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai" as const,
    model_id: "routed-openai",
    display_name: "Routed OpenAI",
    loadbalance_strategy_id: 11,
    loadbalance_strategy: createStrategy(),
    context_window_tokens: contextWindowTokens,
    default_output_token_reserve: defaultOutputTokenReserve,
    max_context_utilization: maxContextUtilization,
    preferred_context_utilization_threshold: preferredContextUtilizationThreshold,
    access_targets: [createAccessTarget("target-alpha", 0, "Target Alpha")],
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

async function mockModelSettingsRoutes(page: Page) {
  const profile = createProfile();
  const updatePayloads: Array<Record<string, unknown>> = [];
  let currentModel = createModelRecord(65536, 4096, 0.9, 0.7);

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false });
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" });
    if (pathname === "/api/endpoints") return fulfillJson([]);
    if (pathname === `/api/models/${modelConfigId}/connections`) return fulfillJson([]);
    if (pathname === "/api/connections") return fulfillJson([]);
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([createStrategy()]);
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/vendors") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingResponse());

    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([
        currentModel,
        {
          ...createModelRecord(null, 4096, 0.9, null),
          id: 1,
          model_id: "target-alpha",
          display_name: "Target Alpha",
          access_targets: [],
        },
        {
          ...createModelRecord(null, 4096, 0.9, null),
          id: 2,
          model_id: "target-beta",
          display_name: "Target Beta",
          access_targets: [],
        },
      ]);
    }

    if (pathname === `/api/models/${modelConfigId}` && request.method() === "GET") {
      return fulfillJson(currentModel);
    }

    if (pathname === `/api/models/${modelConfigId}` && request.method() === "PUT") {
      const payload = request.postDataJSON() as Record<string, unknown>;
      updatePayloads.push(payload);
      currentModel = {
        ...currentModel,
        vendor_id: (payload.vendor_id as number | null | undefined) ?? null,
        api_family: (payload.api_family as "openai") ?? "openai",
        model_id: (payload.model_id as string) ?? currentModel.model_id,
        display_name: (payload.display_name as string | null) ?? currentModel.display_name,
        loadbalance_strategy_id: (payload.loadbalance_strategy_id as number) ?? currentModel.loadbalance_strategy_id,
        context_window_tokens: (payload.context_window_tokens as number | null | undefined) ?? null,
        default_output_token_reserve: (payload.default_output_token_reserve as number) ?? currentModel.default_output_token_reserve,
        max_context_utilization: (payload.max_context_utilization as number) ?? currentModel.max_context_utilization,
        preferred_context_utilization_threshold:
          (payload.preferred_context_utilization_threshold as number | null | undefined) ?? null,
        is_enabled: (payload.is_enabled as boolean) ?? currentModel.is_enabled,
      };
      return fulfillJson(currentModel);
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

test("context-capability-authoring: model settings clears blank context window tokens to null and rehydrates saved values on reopen", async ({ page }) => {
  const routes = await mockModelSettingsRoutes(page);

  await page.goto(`/models/${modelConfigId}`);
  await page.getByRole("button", { name: /edit model/i }).click();

  const dialog = page.getByRole("dialog", { name: "Model Settings" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(contextWindowHelperCopy)).toBeVisible();
  await expect(dialog.locator("#model-context-window-tokens")).toHaveValue("65536");
  await expect(dialog.locator("#model-preferred-context-utilization-threshold")).toHaveValue("0.7");

  await dialog.locator("#model-context-window-tokens").fill("");
  await dialog.locator("#model-default-output-token-reserve").fill("8192");
  await dialog.locator("#model-max-context-utilization").fill("0.75");
  await dialog.locator("#model-preferred-context-utilization-threshold").fill("");
  await dialog.getByRole("button", { name: "Save Changes" }).click();

  await expect(page.getByText("Model updated").last()).toBeVisible();
  expect(routes.getUpdatePayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "openai",
      model_id: "routed-openai",
      display_name: "Routed OpenAI",
      access_targets: [{ target_type: "model", target_model_id: "target-alpha", position: 0, weight: 1, target_priority: 0, is_enabled: true }],
      context_overflow_promotion_target_id: null,
      loadbalance_strategy_id: 11,
      is_enabled: true,
      context_window_tokens: null,
      default_output_token_reserve: 8192,
      max_context_utilization: 0.75,
      preferred_context_utilization_threshold: null,
    },
  ]);

  await page.getByRole("button", { name: /edit model/i }).click();
  await expect(dialog.locator("#model-context-window-tokens")).toHaveValue("");
  await expect(dialog.locator("#model-default-output-token-reserve")).toHaveValue("8192");
  await expect(dialog.locator("#model-max-context-utilization")).toHaveValue("0.75");
  await expect(dialog.locator("#model-preferred-context-utilization-threshold")).toHaveValue("");
});

test("context-capability-authoring: model settings blocks invalid reserve and utilization before patch", async ({ page }) => {
  const routes = await mockModelSettingsRoutes(page);

  await page.goto(`/models/${modelConfigId}`);
  await page.getByRole("button", { name: /edit model/i }).click();

  const dialog = page.getByRole("dialog", { name: "Model Settings" });
  await expect(dialog).toBeVisible();

  await dialog.locator("#model-id").fill("");
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText(modelIdRequiredCopy).last()).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);

  await dialog.locator("#model-id").fill("routed-openai");
  await dialog.locator("#model-default-output-token-reserve").fill("");
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(dialog.getByText(outputReserveValidationCopy)).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);

  await dialog.locator("#model-default-output-token-reserve").fill("4096");
  await dialog.locator("#model-max-context-utilization").fill("1.2");
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(dialog.getByText(maxContextUtilizationValidationCopy)).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);

  await dialog.locator("#model-max-context-utilization").fill("0.9");
  await dialog.locator("#model-preferred-context-utilization-threshold").fill("1.2");
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(dialog.getByText(preferredContextUtilizationThresholdValidationCopy)).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);

  await dialog.locator("#model-preferred-context-utilization-threshold").fill("0.95");
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(dialog.getByText(preferredThresholdExceedsMaxValidationCopy)).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);
});
