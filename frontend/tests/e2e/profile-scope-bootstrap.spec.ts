import { expect, test, type Page, type Request } from "@playwright/test";

const timestamp = "2026-04-28T12:00:00Z";
const PROFILE_STORAGE_KEY = "prism.selectedProfileId";
const profileScopedDescription =
  "Runtime traffic keeps following the active profile until you activate another one.";

type ProfileFixture = {
  id: number;
  isActive?: boolean;
  isDefault?: boolean;
  name: string;
};

type HeaderCapture = {
  bootstrap: Array<string | null>;
  costing: Array<string | null>;
  models: Array<string | null>;
  retention: Array<string | null>;
};

function createProfile({ id, name, isActive = false, isDefault = false }: ProfileFixture) {
  return {
    id,
    name,
    description: `${name} profile`,
    is_active: isActive,
    is_default: isDefault,
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

function createRetentionSettings() {
  return {
    request_logs_retention_days: 30,
    statistics_retention_days: 30,
    audit_logs_retention_days: 30,
    loadbalance_events_retention_days: 30,
  };
}

function getProfileScopedDescription(profileName: string, profileId: number) {
  return `Changes here manage ${profileName} (#${profileId}). ${profileScopedDescription}`;
}

function getLastCapturedHeader(values: Array<string | null>) {
  return values.length > 0 ? values[values.length - 1] : null;
}

async function readProfileHeader(request: Request) {
  const headers = await request.allHeaders();
  return headers["x-profile-id"] ?? null;
}

async function seedBrowserStorage(page: Page, storedProfileId: string | null) {
  await page.addInitScript(
    ({ locale, profileStorageKey, selectedProfileId }) => {
      window.localStorage.setItem(locale.key, locale.value);

      if (selectedProfileId === null) {
        window.localStorage.removeItem(profileStorageKey);
        return;
      }

      window.localStorage.setItem(profileStorageKey, selectedProfileId);
    },
    {
      locale: { key: "prism.locale", value: "en" },
      profileStorageKey: PROFILE_STORAGE_KEY,
      selectedProfileId: storedProfileId,
    },
  );
}

async function mockProfileScopedSettingsRoutes(page: Page) {
  const profiles = [
    createProfile({ id: 1, name: "Default", isDefault: true }),
    createProfile({ id: 2, name: "Blue Team", isActive: true }),
    createProfile({ id: 3, name: "Green Team" }),
  ];
  const activeProfile = profiles[1];
  const capturedHeaders: HeaderCapture = {
    bootstrap: [],
    costing: [],
    models: [],
    retention: [],
  };

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
      capturedHeaders.bootstrap.push(await readProfileHeader(request));
      return fulfillJson({
        profiles,
        active_profile: activeProfile,
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/costing") {
      const profileHeader = await readProfileHeader(request);
      const profileId = Number.parseInt(profileHeader ?? "1", 10);
      capturedHeaders.costing.push(profileHeader);
      return fulfillJson(createCostingSettings(Number.isFinite(profileId) ? profileId : 1));
    }

    if (pathname === "/api/settings/auth") {
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

    if (pathname === "/api/settings/log-retention") {
      capturedHeaders.retention.push(await readProfileHeader(request));
      return fulfillJson(createRetentionSettings());
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: null, effective_timezone: "Europe/Helsinki" });
    }

    if (pathname === "/api/models") {
      capturedHeaders.models.push(await readProfileHeader(request));
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

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  return { activeProfile, capturedHeaders };
}

async function expectActiveProfileBootstrap(page: Page, capturedHeaders: HeaderCapture) {
  await expect.poll(() => capturedHeaders.costing.length).toBeGreaterThan(0);
  await expect.poll(() => capturedHeaders.models.length).toBeGreaterThan(0);

  expect(capturedHeaders.bootstrap).toEqual([null]);
  expect(capturedHeaders.costing[0]).toBe("2");
  expect(capturedHeaders.models[0]).toBe("2");

  await expect(page.getByTestId("shell-profile-switcher")).toContainText("Blue Team");
  await expect(page.getByText(getProfileScopedDescription("Blue Team", 2))).toBeVisible();
  expect(
    await page.evaluate((profileStorageKey) => window.localStorage.getItem(profileStorageKey), PROFILE_STORAGE_KEY),
  ).toBe("2");
}

async function expectGlobalRetentionUnscoped(page: Page, capturedHeaders: HeaderCapture) {
  const previousRetentionRequests = capturedHeaders.retention.length;

  await page.goto("/settings#retention-deletion");

  await expect.poll(() => capturedHeaders.retention.length).toBeGreaterThan(previousRetentionRequests);
  expect(capturedHeaders.retention.every((value) => value === null)).toBe(true);
}

test.describe("profile scope bootstrap", () => {
  test("cold start without stored selection prefers the active profile and keeps mismatch copy coherent", async ({
    page,
  }) => {
    const { capturedHeaders } = await mockProfileScopedSettingsRoutes(page);
    await seedBrowserStorage(page, null);

    await page.goto("/settings");

    await expectActiveProfileBootstrap(page, capturedHeaders);

    await page.getByTestId("shell-profile-switcher").getByRole("button").click();
    await page.getByRole("menuitem", { name: /Default/ }).click();

    await expect.poll(() => getLastCapturedHeader(capturedHeaders.costing)).toBe("1");
    await expect(page.getByTestId("shell-profile-switcher")).toContainText("Default");
    await expect(page.getByText("Default · Runtime: Blue Team")).toBeVisible();
    await expect(page.getByRole("button", { name: "Activate" })).toBeVisible();
    await expect(page.getByText(getProfileScopedDescription("Default", 1))).toBeVisible();
    expect(
      await page.evaluate((profileStorageKey) => window.localStorage.getItem(profileStorageKey), PROFILE_STORAGE_KEY),
    ).toBe("1");

    await expectGlobalRetentionUnscoped(page, capturedHeaders);
  });

  test("cold start with stale stored selection falls back to the active profile before the default profile", async ({
    page,
  }) => {
    const { capturedHeaders } = await mockProfileScopedSettingsRoutes(page);
    await seedBrowserStorage(page, "999");

    await page.goto("/settings");

    await expectActiveProfileBootstrap(page, capturedHeaders);
    expect(capturedHeaders.costing).not.toContain("1");
    expect(capturedHeaders.models).not.toContain("1");

    await expectGlobalRetentionUnscoped(page, capturedHeaders);
  });
});
