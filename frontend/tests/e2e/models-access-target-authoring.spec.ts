import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T12:00:00Z";

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
    retry_max_attempts: 3,
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
    vendor_id: null,
    vendor: null,
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

async function mockModelRoutes(page: Page) {
  const profile = createProfile();
  const strategies = [createStrategy()];
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
        vendor_id: payload.vendor_id ?? null,
        vendor: null,
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

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getCreatedPayloads: () => createdPayloads,
  };
}

test("main model dialog authors ordered model access targets before save", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });
  await page.getByRole("textbox", { name: "Model ID" }).fill("routed-openai");

  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Add at least one valid same-family access target before saving.").last()).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#access-target-kind").click();
  await page.getByRole("option", { name: "Model" }).click();
  await dialog.locator("#access-target-select").click();
  await page.getByRole("option", { name: /Target Alpha/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();

  await dialog.locator("#access-target-select").click();
  await page.getByRole("option", { name: /Target Beta/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();
  await dialog.getByRole("button", { name: "Move target 2 up" }).click();

  await dialog.getByRole("combobox").filter({ hasText: "OpenAI" }).click();
  await page.getByRole("option", { name: /Anthropic/i }).click();
  await expect(page.getByText("No access targets selected. Add at least one enabled target before enabling this model.")).toBeVisible();

  await dialog.locator("#access-target-kind").click();
  await page.getByRole("option", { name: "Model" }).click();
  await dialog.locator("#access-target-select").click();
  await expect(page.getByRole("option", { name: /Claude Sonnet/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Target Alpha/ })).toHaveCount(0);
  await page.keyboard.press("Escape");

  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Add at least one valid same-family access target before saving.").last()).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await dialog.locator("#access-target-select").click();
  await page.getByRole("option", { name: /Claude Sonnet/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();

  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Model created")).toBeVisible();

  expect(routes.getCreatedPayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "anthropic",
      model_id: "routed-openai",
      display_name: "routed-openai",
      access_targets: [{ target_type: "model", target_model_id: "claude-sonnet", position: 0, is_enabled: true }],
      loadbalance_strategy_id: 11,
      is_enabled: true,
    },
  ]);
});
