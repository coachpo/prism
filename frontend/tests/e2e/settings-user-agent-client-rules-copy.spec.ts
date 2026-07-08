import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-27T00:00:00Z";

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

function createUserAgentClientRule(id: number, name: string, pattern: string, isSystem: boolean) {
  return {
    id,
    name,
    pattern,
    enabled: true,
    is_system: isSystem,
    created_at: timestamp,
    updated_at: timestamp,
  };
}

async function mockSettingsRoutes(page: Page) {
  const profile = createProfile();
  const userAgentClientRules = [
    createUserAgentClientRule(1, "Claude Code", "Claude\\sCode", true),
    createUserAgentClientRule(2, "Acme SDK", "acme-sdk", false),
  ];

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
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/auth") {
      return fulfillJson({ auth_enabled: false, username: null, has_password: false, email: null, pending_email: null, email_bound_at: null, email_verification_required: false });
    }
    if (pathname === "/api/settings/log-retention") {
      return fulfillJson({ request_logs_retention_days: 30, statistics_retention_days: 30, audit_logs_retention_days: 30, loadbalance_events_retention_days: 30 });
    }
    if (pathname === "/api/settings/audit") {
      return fulfillJson({ profile_id: profile.id, settings: [] });
    }
    if (pathname === "/api/models") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/header-blocklist-rules") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/user-agent-client-rules") {
      return fulfillJson(userAgentClientRules);
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });
}

test("settings shows user-agent client rule actions", async ({ page }) => {
  await mockSettingsRoutes(page);

  await page.goto("/system/settings?tab=profile&section=audit-configuration#audit-configuration");
  const auditSection = page.locator("section#audit-configuration");
  const card = page.getByTestId("audit-user-agent-client-rules-card");

  await expect(auditSection).toBeVisible();
  await expect(card).toBeVisible();
  await expect(card.getByRole("button", { name: "Add Rule" })).toBeVisible();
});
