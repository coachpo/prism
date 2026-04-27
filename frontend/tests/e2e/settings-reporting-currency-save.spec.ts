import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";

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

function createAuthSettings() {
  return {
    auth_enabled: false,
    username: null,
    has_password: false,
    email: null,
    pending_email: null,
    email_bound_at: null,
    email_verification_required: false,
  };
}

function createModelListItem() {
  return {
    id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

type CostingSettings = {
  report_currency_code: string;
  report_currency_symbol: string;
  endpoint_fx_mappings: unknown[];
  timezone_preference: string | null;
};

function createCostingSettings(overrides?: {
  report_currency_code?: string;
  report_currency_symbol?: string;
  timezone_preference?: string | null;
}): CostingSettings {
  return {
    report_currency_code: overrides?.report_currency_code ?? "EUR",
    report_currency_symbol: overrides?.report_currency_symbol ?? "€",
    endpoint_fx_mappings: [],
    timezone_preference: overrides?.timezone_preference ?? null,
  };
}

function createRetentionSettings() {
  return {
    profile_id: 1,
    request_logs_retention_days: 30,
    statistics_retention_days: 30,
    audit_logs_retention_days: 30,
  };
}

async function mockSettingsRoutes(
  page: Page,
  options: { failBillingSave?: boolean } = {},
) {
  const profile = createProfile();
  let costingState = createCostingSettings();

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({
        profiles: [profile],
        active_profile: profile,
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/costing" && request.method() === "GET") {
      return fulfillJson(costingState);
    }

    if (pathname === "/api/settings/costing" && request.method() === "PUT") {
      if (options.failBillingSave) {
        return fulfillJson({ detail: "Billing save rejected" }, 500);
      }

      const payload = request.postDataJSON() as {
        endpoint_fx_mappings?: unknown[];
        report_currency_code?: string;
        report_currency_symbol?: string;
        timezone_preference?: string | null;
      };

      costingState = {
        report_currency_code: payload.report_currency_code ?? costingState.report_currency_code,
        report_currency_symbol: payload.report_currency_symbol ?? costingState.report_currency_symbol,
        endpoint_fx_mappings: payload.endpoint_fx_mappings ?? costingState.endpoint_fx_mappings,
        timezone_preference: payload.timezone_preference ?? costingState.timezone_preference,
      };

      return fulfillJson(costingState);
    }

    if (pathname === "/api/settings/auth") {
      return fulfillJson(createAuthSettings());
    }

    if (pathname === "/api/settings/retention") {
      return fulfillJson(createRetentionSettings());
    }

    if (pathname === "/api/models") {
      return fulfillJson([createModelListItem()]);
    }

    if (pathname === "/api/vendors") {
      return fulfillJson([]);
    }

    if (pathname === "/api/config/header-blocklist-rules") {
      return fulfillJson([]);
    }

    if (pathname === "/api/config/user-agent-client-rules") {
      return fulfillJson([]);
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"));
}

function getBillingSection(page: Page) {
  return page.locator("section#billing-currency");
}

async function openSettings(page: Page) {
  await page.goto("/settings");
  const billingSection = getBillingSection(page);
  await expect(billingSection).toBeVisible();
  return billingSection;
}

async function fillBillingCurrency(
  billingSection: ReturnType<typeof getBillingSection>,
  code: string,
  symbol: string,
) {
  await billingSection.getByLabel("Code").fill(code);
  await billingSection.getByLabel("Symbol").fill(symbol);
}

test.describe("settings reporting currency save", () => {
  test("successful billing save shows the updated reporting-currency summary", async ({ page }) => {
    await mockSettingsRoutes(page);

    const billingSection = await openSettings(page);
    await expect(billingSection.getByText("Reporting currency: EUR (€)")).toBeVisible();

    await fillBillingCurrency(billingSection, "GBP", "£");
    await billingSection.getByRole("button", { name: "Save" }).click();

    await expect(page.getByText("Billing and currency settings saved")).toBeVisible();
    await expect(billingSection.getByText("Reporting currency: GBP (£)")).toBeVisible();

    await page.reload();

    const reloadedBillingSection = getBillingSection(page);
    await expect(reloadedBillingSection).toBeVisible();
    await expect(reloadedBillingSection.getByText("Reporting currency: GBP (£)")).toBeVisible();
  });

  test("failed billing save preserves the prior saved reporting-currency summary", async ({ page }) => {
    await mockSettingsRoutes(page, { failBillingSave: true });

    const billingSection = await openSettings(page);
    await expect(billingSection.getByText("Reporting currency: EUR (€)")).toBeVisible();

    await fillBillingCurrency(billingSection, "GBP", "£");
    await billingSection.getByRole("button", { name: "Save" }).click();

    await expect(page.getByText("Billing save rejected")).toBeVisible();

    await page.reload();

    const reloadedBillingSection = getBillingSection(page);
    await expect(reloadedBillingSection).toBeVisible();
    await expect(reloadedBillingSection.getByText("Reporting currency: EUR (€)")).toBeVisible();
    await expect(reloadedBillingSection.getByText("Reporting currency: GBP (£)")).toHaveCount(0);
  });
});
