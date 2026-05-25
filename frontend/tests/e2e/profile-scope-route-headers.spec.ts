import { expect, test, type Page, type Request } from "@playwright/test";
import { createDashboardSnapshot } from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-28T12:00:00Z";
const PROFILE_STORAGE_KEY = "prism.selectedProfileId";

type HeaderProbeCapture = {
  bootstrap: Array<string | null>;
  settingsAuth: Array<string | null>;
  settingsCosting: Array<string | null>;
  statsDashboard: Array<string | null>;
};

function createProfile(id: number, name: string, options?: { isActive?: boolean; isDefault?: boolean }) {
  return {
    id,
    name,
    description: `${name} profile`,
    is_active: options?.isActive ?? false,
    is_default: options?.isDefault ?? false,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  };
}

function createCostingSettings(profileId: number) {
  return {
    report_currency_code: profileId === 2 ? "USD" : "EUR",
    report_currency_symbol: profileId === 2 ? "$" : "€",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

async function readProfileHeader(request: Request) {
  const headers = await request.allHeaders();
  return headers["x-profile-id"] ?? null;
}

async function seedBrowserStorage(page: Page) {
  await page.addInitScript(
    ({ locale, profileStorageKey }) => {
      window.localStorage.setItem(locale.key, locale.value);
      window.localStorage.removeItem(profileStorageKey);
    },
    {
      locale: { key: "prism.locale", value: "en" },
      profileStorageKey: PROFILE_STORAGE_KEY,
    },
  );
}

async function mockHeaderProbeRoutes(page: Page) {
  const profiles = [
    createProfile(1, "Default", { isDefault: true }),
    createProfile(2, "Blue Team", { isActive: true }),
  ];
  const capturedHeaders: HeaderProbeCapture = {
    bootstrap: [],
    settingsAuth: [],
    settingsCosting: [],
    statsDashboard: [],
  };

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname } = url;

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
      capturedHeaders.bootstrap.push(await readProfileHeader(request));
      return fulfillJson({
        profiles,
        active_profile: profiles[1],
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/auth") {
      capturedHeaders.settingsAuth.push(await readProfileHeader(request));
      return fulfillJson({
        auth_enabled: false,
        username: null,
        has_password: false,
        email: null,
        pending_email: null,
        email_bound_at: null,
        email_verification_required: false,
      });
    }

    if (pathname === "/api/settings/costing") {
      const profileHeader = await readProfileHeader(request);
      const profileId = Number.parseInt(profileHeader ?? "1", 10);
      capturedHeaders.settingsCosting.push(profileHeader);
      return fulfillJson(createCostingSettings(Number.isFinite(profileId) ? profileId : 1));
    }

    if (pathname === "/api/settings/log-retention") {
      return fulfillJson({
        request_logs_retention_days: 30,
        statistics_retention_days: 30,
        audit_logs_retention_days: 30,
        loadbalance_events_retention_days: 30,
      });
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: null, effective_timezone: "Europe/Helsinki" });
    }

    if (pathname === "/api/models") {
      return fulfillJson([]);
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

    if (pathname === "/api/stats/dashboard") {
      const profileHeader = await readProfileHeader(request);
      const profileId = Number.parseInt(profileHeader ?? "1", 10);
      capturedHeaders.statsDashboard.push(profileHeader);
      return fulfillJson(createDashboardSnapshot({
        metricSnapshot: {
          total_requests: Number.isFinite(profileId) && profileId === 2 ? 42 : 13,
        },
      }));
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  return capturedHeaders;
}

test("browser requests omit X-Profile-Id on global routes and include it on scoped management routes", async ({
  page,
}) => {
  const capturedHeaders = await mockHeaderProbeRoutes(page);
  await seedBrowserStorage(page);

  await page.goto("/settings");

  await expect.poll(() => capturedHeaders.settingsCosting.length).toBeGreaterThan(0);

  expect(capturedHeaders.bootstrap.every((value) => value === null)).toBe(true);
  expect(capturedHeaders.settingsCosting.every((value) => value === "2")).toBe(true);

  await page.goto("/settings#authentication");

  await expect.poll(() => capturedHeaders.settingsAuth.length).toBeGreaterThan(0);
  expect(capturedHeaders.settingsAuth.every((value) => value === null)).toBe(true);

  await page.goto("/dashboard?tab=overview");

  await expect.poll(() => capturedHeaders.statsDashboard.length).toBeGreaterThan(0);

  expect(capturedHeaders.bootstrap.every((value) => value === null)).toBe(true);
  expect(capturedHeaders.statsDashboard.every((value) => value === "2")).toBe(true);
});
