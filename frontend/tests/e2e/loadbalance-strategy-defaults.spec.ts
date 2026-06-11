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

function canonicalRow(
  id: number,
  name: string,
  legacyStrategyType: "single" | "fill-first" | "round-robin",
  banMode: "off" | "temporary" | "until_reset" = "off",
) {
  return {
    id,
    profile_id: 1,
    name,
    legacy_strategy_type: legacyStrategyType,
    failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529],
    ban_mode: banMode,
    retry_base_delay_ms: 60000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900000,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: banMode === "off" ? 0 : 4,
    ban_duration_seconds: banMode === "temporary" ? 28800 : 0,
    attached_model_count: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

function canonicalRows() {
  return [
    canonicalRow(11, "Default single routing", "single"),
    canonicalRow(12, "Default fill-first routing", "fill-first"),
    canonicalRow(13, "Default round-robin routing", "round-robin"),
  ];
}

function occupiedSingleRow() {
  return canonicalRow(21, "Default single routing", "fill-first", "until_reset");
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
  const rows = canonicalRows();

  await mockLoadbalanceRoutes(page, [], {
    items: rows,
    created_count: 3,
    created_names: ["Default single routing", "Default fill-first routing", "Default round-robin routing"],
    existing_names: [],
  });

  await page.goto("/route/ban-policies");

  await expect(page.getByRole("button", { name: "Create Defaults" }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Add Strategy" }).first()).toBeVisible();
  await expect(page.getByRole("row", { name: /Default single routing/ })).toHaveCount(0);
  await expect(page.getByRole("row", { name: /Default fill-first routing/ })).toHaveCount(0);
  await expect(page.getByRole("row", { name: /Default round-robin routing/ })).toHaveCount(0);

  await page.getByRole("button", { name: "Create Defaults" }).first().click();

  await expect(page.getByRole("row", { name: /Default single routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default fill-first routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default round-robin routing/ })).toHaveCount(1);
  await expect(page.getByRole("table")).toContainText("Default single routing");
  await expect(page.getByRole("table")).toContainText("Default fill-first routing");
  await expect(page.getByRole("table")).toContainText("Default round-robin routing");
  await expect(page.getByRole("button", { name: "Create Defaults" })).toBeVisible();
});

test("repeat click does not duplicate defaults", async ({ page }) => {
  const rows = canonicalRows();

  await mockLoadbalanceRoutes(page, rows, {
    items: rows,
    created_count: 0,
    created_names: [],
    existing_names: ["Default single routing", "Default fill-first routing", "Default round-robin routing"],
  });

  await page.goto("/route/ban-policies");

  await expect(page.getByRole("row", { name: /Default single routing/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Default fill-first routing/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Default round-robin routing/ })).toBeVisible();

  const createDefaults = page.getByRole("button", { name: "Create Defaults" }).first();
  await createDefaults.click();
  await createDefaults.click();

  await expect(page.getByRole("row", { name: /Default single routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default fill-first routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default round-robin routing/ })).toHaveCount(1);
  await expect(page.getByRole("table")).toContainText("Default single routing");
  await expect(page.getByRole("table")).toContainText("Default fill-first routing");
  await expect(page.getByRole("table")).toContainText("Default round-robin routing");
  await expect(page.getByText("Default Ban Policy strategies already exist").first()).toBeVisible();
});

test("shows conflict error when canonical names are occupied", async ({ page }) => {
  const occupied = [occupiedSingleRow(), ...canonicalRows().slice(1)];

  await mockLoadbalanceRoutes(page, occupied, {
    detail: {
      message: "Canonical loadbalance strategy default name conflict",
      conflicting_names: ["Default single routing"],
    },
  }, 409);

  await page.goto("/route/ban-policies");

  await expect(page.getByRole("row", { name: /Default single routing/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Default fill-first routing/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /Default round-robin routing/ })).toBeVisible();

  await page.getByRole("button", { name: "Create Defaults" }).first().click();

  await expect(page.getByRole("row", { name: /Default single routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default fill-first routing/ })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /Default round-robin routing/ })).toHaveCount(1);
  await expect(page.getByText("Canonical loadbalance strategy default name conflict")).toBeVisible();
});
