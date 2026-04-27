import { expect, test, type Page, type Route } from "@playwright/test";

const timestamp = "2026-04-10T00:00:00Z";

function createCostingSettings() {
  return {
    report_currency_code: "USD",
    report_currency_symbol: "$",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

function fulfillJson(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function canonicalLegacyRow() {
  return {
    id: 11,
    profile_id: 1,
    name: "Default legacy routing",
    strategy_type: "legacy",
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
    legacy_strategy_type: "round-robin",
    auto_recovery: {
      mode: "enabled",
      status_codes: [403, 422, 429, 500, 502, 503, 504, 529],
      cooldown: {
        base_seconds: 60,
        failure_threshold: 2,
        backoff_multiplier: 2,
        max_cooldown_seconds: 900,
      },
      ban: { mode: "off" },
    },
  };
}

function canonicalAdaptiveRow() {
  return {
    id: 12,
    profile_id: 1,
    name: "Default adaptive routing",
    strategy_type: "adaptive",
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
    routing_policy: {
      kind: "adaptive",
      routing_objective: "minimize_latency",
      hedge: {
        enabled: false,
        delay_ms: 1500,
        max_additional_attempts: 1,
      },
      circuit_breaker: {
        failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529],
        base_open_seconds: 60,
        failure_threshold: 2,
        backoff_multiplier: 2,
        max_open_seconds: 900,
        ban_mode: "off",
        max_open_strikes_before_ban: 0,
        ban_duration_seconds: 0,
      },
      admission: {
        respect_qps_limit: true,
        respect_in_flight_limits: true,
      },
    },
  };
}

function occupiedLegacyRow() {
  return {
    id: 21,
    profile_id: 1,
    name: "Default legacy routing",
    strategy_type: "legacy",
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
    legacy_strategy_type: "round-robin",
    auto_recovery: {
      mode: "enabled",
      status_codes: [403, 422, 429, 500, 502, 503, 504, 529],
      cooldown: {
        base_seconds: 60,
        failure_threshold: 2,
        backoff_multiplier: 2,
        max_cooldown_seconds: 900,
      },
      ban: { mode: "manual", max_cooldown_strikes_before_ban: 1 },
    },
  };
}

async function mockLoadbalanceRoutes(
  page: Page,
  strategies: unknown[],
  postResponse: unknown,
  postStatus = 200,
) {
  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    if (pathname === "/api/auth/status") {
      return fulfillJson(route, { auth_enabled: false });
    }

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson(route, {
        profiles: [
          {
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
          },
        ],
        active_profile: {
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
        },
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson(route, strategies);
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson(route, createCostingSettings());
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson(route, { timezone_preference: "UTC" });
    }

    if (pathname === "/api/models") {
      return fulfillJson(route, []);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson(route, []);
    }

    if (pathname === "/api/loadbalance/strategies/defaults" && request.method() === "POST") {
      return fulfillJson(route, postResponse, postStatus);
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });
}

test("creates defaults from the loadbalance strategies page", async ({ page }) => {
  const canonicalRows = [canonicalLegacyRow(), canonicalAdaptiveRow()];

  await mockLoadbalanceRoutes(page, [], {
    items: canonicalRows,
    created_count: 2,
    created_names: ["Default legacy routing", "Default adaptive routing"],
    existing_names: [],
  });

  await page.goto("/loadbalance-strategies");

  await expect(page.getByRole("button", { name: "Create Defaults" }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Add Strategy" }).first()).toBeVisible();
  await expect(page.getByRole("row", { name: /Default legacy routing/ })).toHaveCount(0);
  await expect(page.getByRole("row", { name: /Default adaptive routing/ })).toHaveCount(0);

  await page.getByRole("button", { name: "Create Defaults" }).first().click();

  await expect(page.getByRole("row", { name: /Default legacy routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default adaptive routing/ })).toHaveCount(1);
  await expect(page.getByRole("table")).toContainText("Default legacy routing");
  await expect(page.getByRole("table")).toContainText("Default adaptive routing");
  await expect(page.getByRole("button", { name: "Create Defaults" })).toBeVisible();
});

test("repeat click does not duplicate defaults", async ({ page }) => {
  const canonicalRows = [canonicalLegacyRow(), canonicalAdaptiveRow()];

  await mockLoadbalanceRoutes(page, canonicalRows, {
    items: canonicalRows,
    created_count: 0,
    created_names: [],
    existing_names: ["Default legacy routing", "Default adaptive routing"],
  });

  await page.goto("/loadbalance-strategies");

  await expect(page.getByRole("row", { name: /Default legacy routing/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Default adaptive routing/ })).toBeVisible();

  const createDefaults = page.getByRole("button", { name: "Create Defaults" }).first();
  await createDefaults.click();
  await createDefaults.click();

  await expect(page.getByRole("row", { name: /Default legacy routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default adaptive routing/ })).toHaveCount(1);
  await expect(page.getByRole("table")).toContainText("Default legacy routing");
  await expect(page.getByRole("table")).toContainText("Default adaptive routing");
  await expect(page.getByText("Default loadbalance strategies already exist").first()).toBeVisible();
});

test("shows conflict error when canonical names are occupied", async ({ page }) => {
  const occupied = [occupiedLegacyRow(), canonicalAdaptiveRow()];

  await mockLoadbalanceRoutes(page, occupied, {
    detail: {
      message: "Canonical loadbalance strategy default name conflict",
      conflicting_names: ["Default legacy routing"],
    },
  }, 409);

  await page.goto("/loadbalance-strategies");

  await expect(page.getByRole("row", { name: /Default legacy routing/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Default adaptive routing/ })).toBeVisible();

  await page.getByRole("button", { name: "Create Defaults" }).first().click();

  await expect(page.getByRole("row", { name: /Default legacy routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default adaptive routing/ })).toHaveCount(1);
  await expect(page.getByText("Canonical loadbalance strategy default name conflict")).toBeVisible();
});
