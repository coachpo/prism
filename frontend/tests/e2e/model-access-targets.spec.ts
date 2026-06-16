import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-29T12:00:00Z";
const staleConnectionTargetMessage = "connection access targets are managed through model-scoped connection routes";

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
    api_family: "openai",
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

async function mockStaleConnectionTargetRoutes(page: Page) {
  const profile = createProfile();
  const stalePayloads: unknown[] = [];
  const standaloneConnectionRequests: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    const method = request.method();

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
      return fulfillJson({ report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/models" && method === "GET") {
      return fulfillJson([
        createModelListItem(1, "target-alpha", "Target Alpha"),
        createModelListItem(2, "target-beta", "Target Beta"),
      ]);
    }
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([createStrategy()]);
    if (pathname === "/api/stats/models/metrics") return fulfillJson({ items: [] });
    if (pathname === "/api/endpoints/connections") return fulfillJson({ items: [] });
    if (pathname === "/api/connections") {
      standaloneConnectionRequests.push(`${method} ${pathname}`);
      return fulfillJson([]);
    }
    if (pathname === "/api/models" && method === "POST") {
      const payload = request.postDataJSON() as { access_targets?: unknown[] };
      const stalePayload = {
        ...payload,
        access_targets: [
          ...(payload.access_targets ?? []),
          { target_type: "connection", connection_id: 77, position: payload.access_targets?.length ?? 0, is_enabled: true },
        ],
      };
      stalePayloads.push(stalePayload);
      return fulfillJson({ detail: staleConnectionTargetMessage }, 400);
    }

    return fulfillJson({ detail: `Unhandled ${method} ${pathname}` }, 500);
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getStalePayloads: () => stalePayloads,
    getStandaloneConnectionRequests: () => standaloneConnectionRequests,
  };
}

test("stale connection target payload is rejected and surfaced in the model create dialog", async ({ page }) => {
  const routes = await mockStaleConnectionTargetRoutes(page);

  await page.goto("/models");
  await page.getByRole("button", { name: "New Model" }).click();

  const dialog = page.getByRole("dialog", { name: "New Model" });
  await expect(dialog.getByRole("button", { name: "New terminal target" })).toHaveCount(0);
  await page.getByRole("textbox", { name: "Model ID" }).fill("stale-connection-target");

  await dialog.locator("#access-target-select").click();
  await expect(page.getByRole("option", { name: /connection|standalone/i })).toHaveCount(0);
  await page.getByRole("option", { name: /Target Alpha/ }).click();
  await dialog.getByRole("button", { name: "Add target" }).click();
  await expect(dialog.getByTestId("access-target-model:target-alpha")).toContainText("Target Alpha");

  await dialog.getByRole("switch", { name: "Enabled" }).click();
  await dialog.getByRole("button", { name: "Save" }).click();

  await expect(dialog.getByTestId("access-targets-error")).toContainText(staleConnectionTargetMessage);
  await expect(page.getByText(staleConnectionTargetMessage).last()).toBeVisible();
  expect(routes.getStandaloneConnectionRequests()).toEqual([]);
  expect(routes.getStalePayloads()).toHaveLength(1);
  expect(routes.getStalePayloads()[0]).toMatchObject({
    access_targets: [
      { target_type: "model", target_model_id: "target-alpha", position: 0, is_enabled: true },
      { target_type: "connection", connection_id: 77, position: 1, is_enabled: true },
    ],
  });
  const firstTarget = (routes.getStalePayloads()[0] as { access_targets: Array<Record<string, unknown>> }).access_targets[0];
  expect(Object.prototype.hasOwnProperty.call(firstTarget, "weight")).toBe(false);
  expect(Object.prototype.hasOwnProperty.call(firstTarget, "target_priority")).toBe(false);
  await expect(dialog.getByTestId("access-target-connection:77")).toHaveCount(0);
});
