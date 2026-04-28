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
    name: "Default legacy routing",
    strategy_type: "legacy",
    legacy_strategy_type: "round-robin",
    auto_recovery: { mode: "disabled" },
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
    vendor_id: null,
    vendor: null,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: 11,
    loadbalance_strategy: {
      id: 11,
      name: "Default legacy routing",
      strategy_type: "legacy",
      legacy_strategy_type: "round-robin",
      auto_recovery: { mode: "disabled" },
    },
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
    createModelListItem(1, "native-a", "Native A", "openai"),
    createModelListItem(2, "native-b", "Native B", "openai"),
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
        model_type: payload.model_type,
        proxy_targets: payload.proxy_targets ?? [],
        loadbalance_strategy_id: payload.loadbalance_strategy_id ?? null,
        loadbalance_strategy: null,
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

test("main model dialog authors ordered proxy targets before save", async ({ page }) => {
  const routes = await mockModelRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  await page.getByRole("textbox", { name: "Model ID" }).fill("proxy-openai");
  await page.locator("#model-type").click();
  await page.getByRole("option", { name: "Proxy" }).click();

  await page.getByRole("dialog", { name: "New Model" }).getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Please add at least one ordered proxy target for proxy models").last()).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await page.locator("#proxy-target-select").click();
  await page.getByRole("option", { name: "Native A" }).click();
  await page.getByRole("button", { name: "Add Target" }).click();

  await page.locator("#proxy-target-select").click();
  await page.getByRole("option", { name: "Native B" }).click();
  await page.getByRole("button", { name: "Add Target" }).click();
  await page.getByRole("button", { name: "Move target native-b up" }).click();

  await page.getByRole("dialog", { name: "New Model" }).getByRole("combobox").nth(1).click();
  await page.getByRole("option", { name: /Anthropic/i }).click();
  await expect(page.getByText("Add at least one proxy target before saving this model.")).toBeVisible();

  await page.locator("#proxy-target-select").click();
  await expect(page.getByRole("option", { name: "Claude Sonnet" })).toBeVisible();
  await expect(page.getByRole("option", { name: "Native A" })).toHaveCount(0);
  await page.keyboard.press("Escape");

  await page.getByRole("dialog", { name: "New Model" }).getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Please add at least one ordered proxy target for proxy models").last()).toBeVisible();
  expect(routes.getCreatedPayloads()).toHaveLength(0);

  await page.locator("#proxy-target-select").click();
  await page.getByRole("option", { name: "Claude Sonnet" }).click();
  await page.getByRole("button", { name: "Add Target" }).click();

  await page.getByRole("dialog", { name: "New Model" }).getByRole("button", { name: "Save" }).click();
  await expect(page.getByText("Model created")).toBeVisible();

  expect(routes.getCreatedPayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "anthropic",
      model_id: "proxy-openai",
      display_name: "proxy-openai",
      model_type: "proxy",
      proxy_targets: [{ target_model_id: "claude-sonnet", position: 0 }],
      loadbalance_strategy_id: null,
      is_enabled: true,
    },
  ]);
});
