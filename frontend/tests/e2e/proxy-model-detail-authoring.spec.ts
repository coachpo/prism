import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-28T12:00:00Z";
const proxyModelConfigId = 50;

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

function createModelListItem(
  id: number,
  modelId: string,
  displayName: string,
  modelType: "native" | "proxy",
  apiFamily: "openai" | "anthropic",
  proxyTargets: Array<{ target_model_id: string; position: number }> = [],
) {
  return {
    id,
    vendor_id: null,
    vendor: null,
    api_family: apiFamily,
    model_id: modelId,
    display_name: displayName,
    model_type: modelType,
    proxy_targets: proxyTargets,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: modelType === "native" ? 1 : 0,
    active_connection_count: modelType === "native" ? 1 : 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createProxyModelDetail(proxyTargets: Array<{ target_model_id: string; position: number }>) {
  return {
    ...createModelListItem(proxyModelConfigId, "proxy-openai", "Proxy OpenAI", "proxy", "openai", proxyTargets),
    connections: [],
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

async function mockProxyModelDetailRoutes(page: Page) {
  const profile = createProfile();
  const updatePayloads: unknown[] = [];
  let currentProxyTargets = [{ target_model_id: "native-a", position: 0 }];

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
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([]);
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/vendors") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingResponse());

    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([
        createModelListItem(proxyModelConfigId, "proxy-openai", "Proxy OpenAI", "proxy", "openai", currentProxyTargets),
        createModelListItem(1, "native-a", "Native A", "native", "openai"),
        createModelListItem(2, "native-b", "Native B", "native", "openai"),
        createModelListItem(3, "proxy-shadow", "Proxy Shadow", "proxy", "openai", [{ target_model_id: "native-a", position: 0 }]),
        createModelListItem(4, "claude-sonnet", "Claude Sonnet", "native", "anthropic"),
      ]);
    }

    if (pathname === `/api/models/${proxyModelConfigId}` && request.method() === "GET") {
      return fulfillJson(createProxyModelDetail(currentProxyTargets));
    }

    if (pathname === `/api/models/${proxyModelConfigId}` && request.method() === "PUT") {
      const payload = request.postDataJSON() as {
        vendor_id: number | null;
        api_family: "openai" | "anthropic";
        model_id: string;
        display_name: string | null;
        model_type: "proxy";
        proxy_targets: Array<{ target_model_id: string; position: number }>;
        loadbalance_strategy_id: null;
        is_enabled: boolean;
      };
      updatePayloads.push(payload);
      currentProxyTargets = payload.proxy_targets ?? [];
      return fulfillJson({
        ...createProxyModelDetail(currentProxyTargets),
        vendor_id: payload.vendor_id ?? null,
        api_family: payload.api_family,
        model_id: payload.model_id,
        display_name: payload.display_name,
        is_enabled: payload.is_enabled,
      });
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

test("proxy detail editing reuses the same ordered same-family native-target contract", async ({ page }) => {
  const routes = await mockProxyModelDetailRoutes(page);

  await page.goto(`/models/${proxyModelConfigId}/proxy`);
  await expect(page.getByRole("heading", { name: "Proxy OpenAI" })).toBeVisible();
  await expect(page.getByText("Ordered priority routing").first()).toBeVisible();
  await expect(page.getByText("Native A")).toBeVisible();
  await expect(page.getByRole("button", { name: "Add Target" })).toHaveCount(0);

  await page.getByRole("button", { name: /edit model/i }).click();

  const dialog = page.getByRole("dialog", { name: "Model Settings" });
  await expect(dialog).toBeVisible();
  await expect(page.getByRole("button", { name: "Add Target" })).toHaveCount(1);

  await dialog.getByRole("button", { name: "Remove target native-a" }).click();
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Please add at least one ordered proxy target for proxy models").last()).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);

  await dialog.locator("#proxy-target-select").click();
  await expect(page.getByRole("option", { name: "Native A" })).toBeVisible();
  await expect(page.getByRole("option", { name: "Native B" })).toBeVisible();
  await expect(page.getByRole("option", { name: "Proxy Shadow" })).toHaveCount(0);
  await expect(page.getByRole("option", { name: "Claude Sonnet" })).toHaveCount(0);
  await page.keyboard.press("Escape");

  await dialog.locator("#proxy-target-select").click();
  await page.getByRole("option", { name: "Native A" }).click();
  await dialog.getByRole("button", { name: "Add Target" }).click();

  await dialog.locator("#proxy-target-select").click();
  await page.getByRole("option", { name: "Native B" }).click();
  await dialog.getByRole("button", { name: "Add Target" }).click();
  await dialog.getByRole("button", { name: "Move target native-b up" }).click();

  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Model updated")).toBeVisible();
  await expect(dialog).toHaveCount(0);

  expect(routes.getUpdatePayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "openai",
      model_id: "proxy-openai",
      display_name: "Proxy OpenAI",
      model_type: "proxy",
      proxy_targets: [
        { target_model_id: "native-b", position: 0 },
        { target_model_id: "native-a", position: 1 },
      ],
      loadbalance_strategy_id: null,
      is_enabled: true,
    },
  ]);

  await expect(page.getByRole("button", { name: "Add Target" })).toHaveCount(0);
  await expect(page.getByText("Native B")).toBeVisible();
  await expect(page.getByText("Priority 1")).toBeVisible();
  await expect(page.getByText("Native A")).toBeVisible();
  await expect(page.getByText("Priority 2")).toBeVisible();
});
