import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T12:00:00Z";
const disabledDraftAccessTargetCopy = /No model targets selected\./;
const enabledTargetRequiredCopy = "Enabled models need at least one enabled same-family access target. Save with Enabled off to attach targets later.";

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
  apiFamily: "openai" | "anthropic" | "gemini" = "openai",
) {
  return {
    id,
    profile_id: 1,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
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
  };
}

async function mockModelRoutes(
  page: Page,
  options: { strategies?: ReturnType<typeof createStrategy>[] } = {},
) {
  const profile = createProfile();
  const strategies = options.strategies ?? [createStrategy()];
  const models = [
    createModelListItem(1, "target-alpha", "Target Alpha", "openai"),
    createModelListItem(2, "target-beta", "Target Beta", "openai"),
    createModelListItem(3, "claude-sonnet", "Claude Sonnet", "anthropic"),
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

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getCreatedPayloads: () => createdPayloads,
  };
}

test("main model dialog disables save when no loadbalance strategies exist", async ({ page }) => {
  const routes = await mockModelRoutes(page, { strategies: [] });

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });
  const saveButton = dialog.getByRole("button", { name: "Save" });
  const noStrategiesCopy = "No loadbalance strategies are available for this profile. Create one on the Loadbalance Strategies page first.";

  await expect(dialog.getByText(noStrategiesCopy)).toBeVisible();
  await expect(saveButton).toBeDisabled();

  await page.getByRole("textbox", { name: "Model ID" }).fill("zero-strategy-model");
  await page.getByRole("textbox", { name: "Display Name" }).fill("Zero Strategy Model");
  await expect(saveButton).toBeDisabled();

  await page.getByRole("textbox", { name: "Display Name" }).press("Enter");
  expect(routes.getCreatedPayloads()).toHaveLength(0);
});

test("main model dialog saves targetless disabled drafts", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });
  await expect(dialog.getByText(disabledDraftAccessTargetCopy)).toBeVisible();
  await expect(dialog.getByRole("button", { name: "New terminal target" })).toHaveCount(0);
  await expect(dialog.getByRole("switch", { name: "Enabled" })).toHaveAttribute("data-state", "unchecked");

  await page.getByRole("textbox", { name: "Model ID" }).fill("draft-openai");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Model created")).toBeVisible();

  expect(routes.getCreatedPayloads()).toEqual([
    {
      api_family: "openai",
      model_id: "draft-openai",
      display_name: "draft-openai",
      openai_accepted_format: "dual_native",
      access_targets: [],
      loadbalance_strategy_id: 11,
      is_enabled: false,
    },
  ]);

  const draftRow = page.getByRole("row", { name: /draft-openai/ });
  await expect(draftRow.getByText("Needs target")).toBeVisible();
  await expect(draftRow.getByText("Disabled")).toBeVisible();
});

test("main model dialog keeps connection option absent while authoring ordered model access targets", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });
  await expect(dialog.getByText(disabledDraftAccessTargetCopy)).toBeVisible();
  await expect(dialog.getByRole("button", { name: "New terminal target" })).toHaveCount(0);
  await expect(dialog.getByText("Tier")).toHaveCount(0);
  await expect(dialog.getByText("Weight")).toHaveCount(0);
  const enabledSwitch = dialog.getByRole("switch", { name: "Enabled" });
  await expect(enabledSwitch).toHaveAttribute("data-state", "unchecked");
  await page.getByRole("textbox", { name: "Model ID" }).fill("routed-openai");

  await enabledSwitch.click();
  await expect(enabledSwitch).toHaveAttribute("data-state", "checked");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(enabledTargetRequiredCopy).last()).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#access-target-select").click();
  await expect(page.getByRole("option", { name: /connection|standalone/i })).toHaveCount(0);
  await page.getByRole("option", { name: /Target Alpha/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();

  await dialog.locator("#access-target-select").click();
  await page.getByRole("option", { name: /Target Beta/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();
  await dialog.getByRole("button", { name: "Move target 2 up" }).click();

  await dialog.getByRole("combobox").filter({ hasText: "OpenAI" }).click();
  await page.getByRole("option", { name: /Anthropic/i }).click();
  await expect(page.getByText(disabledDraftAccessTargetCopy)).toBeVisible();

  await dialog.locator("#access-target-select").click();
  await expect(page.getByRole("option", { name: /connection|standalone/i })).toHaveCount(0);
  await expect(page.getByRole("option", { name: /Claude Sonnet/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Target Alpha/ })).toHaveCount(0);
  await page.keyboard.press("Escape");

  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText(enabledTargetRequiredCopy).last()).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#access-target-select").click();
  await page.getByRole("option", { name: /Claude Sonnet/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();

  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Model created")).toBeVisible();

  expect(routes.getCreatedPayloads()).toEqual([
    {
      api_family: "anthropic",
      model_id: "routed-openai",
      display_name: "routed-openai",
      access_targets: [{ target_type: "model", target_model_id: "claude-sonnet", position: 0, is_enabled: true }],
      loadbalance_strategy_id: 11,
      is_enabled: true,
    },
  ]);
  const createdTarget = (routes.getCreatedPayloads()[0] as { access_targets: Array<Record<string, unknown>> }).access_targets[0];
  expect(Object.prototype.hasOwnProperty.call(createdTarget, "weight")).toBe(false);
  expect(Object.prototype.hasOwnProperty.call(createdTarget, "target_priority")).toBe(false);
});
