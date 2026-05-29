import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-28T12:00:00Z";
const modelConfigId = 50;

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

function createAccessTarget(targetModelId: string, position: number, displayName: string, isEnabled = true) {
  return {
    id: 700 + position,
    target_type: "model",
    target_model_id: targetModelId,
    connection_id: null,
    position,
    is_enabled: isEnabled,
    target_model: {
      id: 100 + position,
      profile_id: 1,
      vendor_id: null,
      api_family: "openai",
      model_id: targetModelId,
      display_name: displayName,
      loadbalance_strategy_id: null,
      is_enabled: true,
    },
    connection: null,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createModelListItem(
  id: number,
  modelId: string,
  displayName: string,
  apiFamily: "openai" | "anthropic",
  accessTargets: ReturnType<typeof createAccessTarget>[] = [],
  isEnabled = true,
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
    loadbalance_strategy: {
      id: 11,
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
    },
    access_targets: accessTargets,
    is_enabled: isEnabled,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function createAccessTargetModelDetail(
  accessTargets: ReturnType<typeof createAccessTarget>[],
  isEnabled = true,
) {
  return createModelListItem(modelConfigId, "routed-openai", "Routed OpenAI", "openai", accessTargets, isEnabled);
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

async function mockModelDetailRoutes(page: Page) {
  const profile = createProfile();
  const updatePayloads: unknown[] = [];
  let currentAccessTargets = [createAccessTarget("target-alpha", 0, "Target Alpha")];
  let currentModelEnabled = true;

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
    if (pathname === "/api/connections") return fulfillJson([]);
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([
      {
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
        attached_model_count: 1,
        created_at: timestamp,
        updated_at: timestamp,
      },
    ]);
    if (pathname === "/api/pricing-templates") return fulfillJson([]);
    if (pathname === "/api/vendors") return fulfillJson([]);
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] });
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingResponse());

    if (pathname === "/api/models" && request.method() === "GET") {
      return fulfillJson([
        createModelListItem(modelConfigId, "routed-openai", "Routed OpenAI", "openai", currentAccessTargets, currentModelEnabled),
        createModelListItem(1, "target-alpha", "Target Alpha", "openai"),
        createModelListItem(2, "target-beta", "Target Beta", "openai"),
        createModelListItem(3, "shadow-openai", "Shadow OpenAI", "openai", [createAccessTarget("target-alpha", 0, "Target Alpha")]),
        createModelListItem(4, "claude-sonnet", "Claude Sonnet", "anthropic"),
      ]);
    }

    if (pathname === `/api/models/${modelConfigId}` && request.method() === "GET") {
      return fulfillJson(createAccessTargetModelDetail(currentAccessTargets, currentModelEnabled));
    }

    if (pathname === `/api/models/${modelConfigId}` && request.method() === "PUT") {
      const payload = request.postDataJSON() as {
        vendor_id: number | null;
        api_family: "openai" | "anthropic";
        model_id: string;
        display_name: string | null;
        access_targets: Array<{ target_type: "model"; target_model_id: string; position: number; is_enabled?: boolean }>;
        loadbalance_strategy_id: number;
        is_enabled: boolean;
      };
      updatePayloads.push(payload);
      currentModelEnabled = payload.is_enabled;
      currentAccessTargets = (payload.access_targets ?? []).map((target) =>
        createAccessTarget(
          target.target_model_id,
          target.position,
          target.target_model_id === "target-alpha" ? "Target Alpha" : "Target Beta",
          target.is_enabled ?? true,
        ),
      );
      return fulfillJson({
        ...createAccessTargetModelDetail(currentAccessTargets, currentModelEnabled),
        vendor_id: payload.vendor_id ?? null,
        api_family: payload.api_family,
        model_id: payload.model_id,
        display_name: payload.display_name,
        loadbalance_strategy_id: payload.loadbalance_strategy_id,
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

test("model detail editing supports disabled targetless drafts and later enabled attachment", async ({ page }) => {
  const routes = await mockModelDetailRoutes(page);

  await page.goto(`/models/${modelConfigId}`);
  await expect(page.getByRole("heading", { name: "Routed OpenAI" })).toBeVisible();
  await expect(page.getByText("Access targets").first()).toBeVisible();
  await expect(page.getByTestId("access-targets-editor").getByText("Target Alpha")).toBeVisible();
  await expect(page.getByRole("button", { name: "Add target" })).toHaveCount(1);

  await page.getByRole("button", { name: /edit model/i }).click();

  const dialog = page.getByRole("dialog", { name: "Model Settings" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Add target" })).toHaveCount(1);

  await dialog.getByRole("button", { name: "Remove target 1" }).click();
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Add at least one enabled same-family access target before saving an enabled model.").last()).toBeVisible();
  expect(routes.getUpdatePayloads()).toHaveLength(0);

  await dialog.getByRole("switch").last().click();
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Model updated").last()).toBeVisible();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText("Needs target")).toBeVisible();

  expect(routes.getUpdatePayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "openai",
      model_id: "routed-openai",
      display_name: "Routed OpenAI",
      access_targets: [],
      loadbalance_strategy_id: 11,
      is_enabled: false,
    },
  ]);

  await page.getByRole("button", { name: /edit model/i }).click();
  await expect(dialog).toBeVisible();
  await dialog.locator("#access-target-kind").click();
  await page.getByRole("option", { name: "Model" }).click();
  await dialog.locator("#access-target-select").click();
  await expect(page.getByRole("option", { name: /Target Alpha/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Target Beta/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Shadow OpenAI/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /Claude Sonnet/ })).toHaveCount(0);
  await page.getByRole("option", { name: /Target Alpha/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();
  await expect(dialog.getByText("Target Alpha")).toBeVisible();

  await dialog.getByRole("switch").last().click();
  await dialog.getByRole("button", { name: "Save Changes" }).click();
  await expect(page.getByText("Model updated").last()).toBeVisible();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByText("Needs target")).toHaveCount(0);

  expect(routes.getUpdatePayloads()).toEqual([
    {
      vendor_id: null,
      api_family: "openai",
      model_id: "routed-openai",
      display_name: "Routed OpenAI",
      access_targets: [],
      loadbalance_strategy_id: 11,
      is_enabled: false,
    },
    {
      vendor_id: null,
      api_family: "openai",
      model_id: "routed-openai",
      display_name: "Routed OpenAI",
      access_targets: [
        { target_type: "model", target_model_id: "target-alpha", position: 0, is_enabled: true },
      ],
      loadbalance_strategy_id: 11,
      is_enabled: true,
    },
  ]);

  const accessTargetsEditor = page.getByTestId("access-targets-editor");
  await expect(page.getByRole("button", { name: "Add target" })).toHaveCount(1);
  await expect(accessTargetsEditor.getByText("Target Alpha")).toBeVisible();
  await expect(accessTargetsEditor.getByText("Priority 1")).toBeVisible();
});
