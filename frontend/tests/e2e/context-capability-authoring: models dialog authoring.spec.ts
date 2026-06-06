import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T12:00:00Z";
const contextWindowHelperCopy = "Leave blank when the model context window is unknown.";
const contextWindowValidationCopy = "Context window tokens must be a positive integer or blank.";
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
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelListItem(
  id: number,
  modelId: string,
  displayName: string,
  contextOverflowPromotionTargetId: string | null = null,
) {
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
    preferred_context_utilization_threshold: null,
    context_overflow_promotion_target_id: contextOverflowPromotionTargetId,
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

type UpdateErrorResponse = {
  body: Record<string, unknown>;
  status: number;
};

interface MockModelRoutesOptions {
  models?: Array<ReturnType<typeof createModelListItem>>;
  updateErrorResponseFactory?: (payload: Record<string, unknown>) => UpdateErrorResponse | null;
}

async function mockModelRoutes(page: Page, options: MockModelRoutesOptions = {}) {
  const profile = createProfile();
  const strategies = [createStrategy()];
  let models = options.models ?? [
    createModelListItem(1, "target-alpha", "Target Alpha"),
    createModelListItem(2, "target-beta", "Target Beta"),
  ];
  const createdPayloads: unknown[] = [];
  const updatedPayloads: unknown[] = [];

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
      const createdModel = {
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
        preferred_context_utilization_threshold: payload.preferred_context_utilization_threshold ?? null,
        context_overflow_promotion_target_id: payload.context_overflow_promotion_target_id ?? null,
        access_targets: payload.access_targets ?? [],
        is_enabled: payload.is_enabled ?? true,
        connections: [],
        created_at: timestamp,
        updated_at: timestamp,
      };
      models = [...models, { ...createdModel, connection_count: 0, active_connection_count: 0, health_success_rate: null, health_total_requests: 0 }];
      return fulfillJson(createdModel);
    }
    if (pathname.startsWith("/api/models/") && request.method() === "PUT") {
      const payload = request.postDataJSON() as Record<string, unknown>;
      updatedPayloads.push(payload);
      const updateErrorResponse = options.updateErrorResponseFactory?.(payload);
      if (updateErrorResponse) {
        return fulfillJson(updateErrorResponse.body, updateErrorResponse.status);
      }
      const modelId = Number(pathname.split("/").at(-1));
      const existingModel = models.find((model) => model.id === modelId);
      if (!existingModel) {
        return fulfillJson({ detail: "Model not found" }, 404);
      }
      const updatedModel = {
        id: existingModel.id,
        profile_id: existingModel.profile_id,
        vendor_id: (payload.vendor_id as number | null | undefined) ?? existingModel.vendor_id,
        vendor: existingModel.vendor,
        api_family: (payload.api_family as string | undefined) ?? existingModel.api_family,
        model_id: (payload.model_id as string | undefined) ?? existingModel.model_id,
        display_name: (payload.display_name as string | null | undefined) ?? existingModel.display_name,
        loadbalance_strategy_id: (payload.loadbalance_strategy_id as number | null | undefined) ?? existingModel.loadbalance_strategy_id,
        loadbalance_strategy: createStrategy(),
        context_window_tokens: (payload.context_window_tokens as number | null | undefined) ?? existingModel.context_window_tokens,
        default_output_token_reserve: (payload.default_output_token_reserve as number | undefined) ?? existingModel.default_output_token_reserve,
        max_context_utilization: (payload.max_context_utilization as number | undefined) ?? existingModel.max_context_utilization,
        preferred_context_utilization_threshold:
          (payload.preferred_context_utilization_threshold as number | null | undefined)
          ?? existingModel.preferred_context_utilization_threshold,
        context_overflow_promotion_target_id:
          (payload.context_overflow_promotion_target_id as string | null | undefined)
          ?? existingModel.context_overflow_promotion_target_id,
        access_targets: existingModel.access_targets,
        is_enabled: (payload.is_enabled as boolean | undefined) ?? existingModel.is_enabled,
        created_at: existingModel.created_at,
        updated_at: timestamp,
      };
      models = models.map((model) =>
        model.id === existingModel.id
          ? {
              ...model,
              ...updatedModel,
              connection_count: model.connection_count,
              active_connection_count: model.active_connection_count,
              health_success_rate: model.health_success_rate,
              health_total_requests: model.health_total_requests,
            }
          : model,
      );
      return fulfillJson(updatedModel);
    }

    return fulfillJson({});
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getCreatedPayloads: () => createdPayloads,
    getUpdatedPayloads: () => updatedPayloads,
  };
}

test("context-capability-authoring: submits parsed context routing defaults on create", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });
  await expect(dialog.getByText("Define the selected-profile entry model, its routing defaults, and the policy it will use to reach terminal targets.")).toBeVisible();
  await expect(dialog.getByText("Set the entry-model context window, reserve, max utilization, and preferred band before terminal-target overrides apply.")).toBeVisible();
  await expect(dialog.getByText("Access targets combine grouped same-family model fallback targets with model-private terminal targets. Manage fallback tiers here, then finish terminal-target routing from Model Detail.")).toBeVisible();
  await expect(dialog.getByText("Choose the Ban Policy and terminal-target selection family this entry model uses after access-target routing.")).toBeVisible();
  await expect(dialog.getByText(contextWindowHelperCopy)).toBeVisible();

  await dialog.locator("#model-id").fill("context-routed-model");
  await expect(dialog.locator("#model-preferred-context-utilization-threshold")).toBeVisible();
  await dialog.locator("#model-context-window-tokens").fill("131072");
  await dialog.locator("#model-default-output-token-reserve").fill("8192");
  await dialog.locator("#model-max-context-utilization").fill("0.75");
  await dialog.locator("#model-preferred-context-utilization-threshold").fill("0.7");
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
      preferred_context_utilization_threshold: 0.7,
      context_overflow_promotion_target_id: null,
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

  await dialog.locator("#model-max-context-utilization").fill("0.9");
  await dialog.locator("#model-preferred-context-utilization-threshold").fill("1.2");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByText(preferredContextUtilizationThresholdValidationCopy)).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#model-preferred-context-utilization-threshold").fill("0.95");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByText(preferredThresholdExceedsMaxValidationCopy)).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);
});

test("context-capability-authoring: overflow promotion target valid", async ({ page }) => {
  const routes = await mockModelRoutes(page, {
    models: [
      { ...createModelListItem(1, "gpt-small", "GPT Small"), is_enabled: false },
      createModelListItem(2, "gpt-large", "GPT Large"),
    ],
  });

  await page.goto("/models");
  const gptSmallRow = page.getByText("GPT Small").locator("xpath=ancestor::div[contains(@class, 'group')][1]");
  await gptSmallRow.getByRole("button", { name: "Edit Model: GPT Small" }).click();

  const dialog = page.getByRole("dialog", { name: "Edit Model" });
  const promotionTargetField = dialog.getByLabel("Overflow promotion target");

  await promotionTargetField.click();
  await page.getByRole("option", { name: "GPT Large (gpt-large)" }).click();
  await dialog.getByRole("button", { name: "Save" }).click();

  await expect(page.getByText("Model updated")).toBeVisible();
  expect(routes.getUpdatedPayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "openai",
      display_name: "GPT Small",
      model_id: "gpt-small",
      is_enabled: false,
      access_targets: [],
      loadbalance_strategy_id: 11,
      context_window_tokens: null,
      default_output_token_reserve: 4096,
      max_context_utilization: 0.9,
      preferred_context_utilization_threshold: null,
      context_overflow_promotion_target_id: "gpt-large",
    },
  ]);

  await page.reload();
  const reloadedGptSmallRow = page.getByText("GPT Small").locator("xpath=ancestor::div[contains(@class, 'group')][1]");
  await expect(reloadedGptSmallRow.getByText("Overflow promote → gpt-large")).toBeVisible();
  await reloadedGptSmallRow.getByRole("button", { name: "Edit Model: GPT Small" }).click();

  const reopenedDialog = page.getByRole("dialog", { name: "Edit Model" });
  await expect(reopenedDialog.getByLabel("Overflow promotion target")).toContainText("GPT Large (gpt-large)");
});

test("context-capability-authoring: overflow promotion target validation error", async ({ page }) => {
  const routes = await mockModelRoutes(page, {
    models: [
      { ...createModelListItem(1, "gpt-small", "GPT Small"), is_enabled: false },
      createModelListItem(2, "gpt-large", "GPT Large"),
    ],
    updateErrorResponseFactory: (payload) => {
      if (payload.context_overflow_promotion_target_id !== "gpt-small") {
        return null;
      }
      return {
        status: 400,
        body: {
          detail: "context_overflow_promotion_target_id cannot reference the source model",
          routing_plan_issues: [
            {
              code: "self_target",
              path: "context_overflow_promotion_target_id",
              message: "context_overflow_promotion_target_id cannot reference the source model",
            },
          ],
        },
      };
    },
  });

  await page.goto("/models");
  const gptSmallRow = page.getByText("GPT Small").locator("xpath=ancestor::div[contains(@class, 'group')][1]");
  await gptSmallRow.getByRole("button", { name: "Edit Model: GPT Small" }).click();

  const dialog = page.getByRole("dialog", { name: "Edit Model" });
  const promotionTargetField = dialog.getByLabel("Overflow promotion target");

  await promotionTargetField.click();
  await page.getByRole("option", { name: "GPT Small (gpt-small)" }).click();
  await dialog.getByRole("button", { name: "Save" }).click();

  await expect(dialog.getByText("context_overflow_promotion_target_id (self_target): context_overflow_promotion_target_id cannot reference the source model")).toBeVisible();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("Overflow promotion target")).toContainText("GPT Small (gpt-small)");
  await expect(page.getByText("Model updated")).toHaveCount(0);
  expect(routes.getUpdatedPayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "openai",
      display_name: "GPT Small",
      model_id: "gpt-small",
      is_enabled: false,
      access_targets: [],
      loadbalance_strategy_id: 11,
      context_window_tokens: null,
      default_output_token_reserve: 4096,
      max_context_utilization: 0.9,
      preferred_context_utilization_threshold: null,
      context_overflow_promotion_target_id: "gpt-small",
    },
  ]);
});
