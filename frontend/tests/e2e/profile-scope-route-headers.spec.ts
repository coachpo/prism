import { expect, test, type Page, type Request } from "@playwright/test";

const timestamp = "2026-04-28T12:00:00Z";
const PROFILE_STORAGE_KEY = "prism.selectedProfileId";

type HeaderProbeCapture = {
  bootstrap: Array<string | null>;
  settingsAuth: Array<string | null>;
  settingsCosting: Array<string | null>;
  statsSummary: Array<string | null>;
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

function createStatsSummary(profileId: number) {
  const totalRequests = profileId === 2 ? 42 : 13;

  return {
    total_requests: totalRequests,
    success_requests: totalRequests - 1,
    failed_requests: 1,
    success_rate: 97.6,
    total_tokens: 1650,
    input_tokens: 900,
    output_tokens: 600,
    cached_tokens: 100,
    reasoning_tokens: 50,
    average_rpm: 0.5,
    average_tpm: 68.8,
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
    statsSummary: [],
  };

  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const { pathname, searchParams } = url;

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

    if (pathname === "/api/stats/summary") {
      const profileHeader = await readProfileHeader(request);
      const profileId = Number.parseInt(profileHeader ?? "1", 10);
      capturedHeaders.statsSummary.push(profileHeader);
      return fulfillJson(createStatsSummary(Number.isFinite(profileId) ? profileId : 1));
    }

    if (pathname === "/api/stats/usage-snapshot") {
      return fulfillJson({
        generated_at: timestamp,
        time_range: {
          preset: "24h",
          start_at: "2026-04-27T12:00:00Z",
          end_at: timestamp,
        },
        currency: { code: "USD", symbol: "$" },
        overview: {
          total_requests: 42,
          success_requests: 41,
          failed_requests: 1,
          success_rate: 97.6,
          total_tokens: 1650,
          input_tokens: 900,
          output_tokens: 600,
          cached_tokens: 100,
          reasoning_tokens: 50,
          average_rpm: 0.5,
          average_tpm: 68.8,
          total_cost_micros: 250000,
        },
        service_health: {
          availability_percentage: 97.6,
          request_count: 42,
          success_count: 41,
          failed_count: 1,
          interval_minutes: 60,
          cells: [],
        },
        request_trends: {
          hourly: [{ key: "all", label: "All requests", total_requests: 42, points: [] }],
          daily: [{ key: "all", label: "All requests", total_requests: 42, points: [] }],
        },
        token_usage_trends: { hourly: [], daily: [] },
        token_type_breakdown: { hourly: [], daily: [] },
        cost_overview: {
          total_cost_micros: 250000,
          priced_request_count: 9,
          unpriced_request_count: 2,
          hourly: [],
          daily: [],
        },
        endpoint_statistics: [],
        model_statistics: [],
        proxy_api_key_statistics: [],
      });
    }

    if (pathname === "/api/stats/requests") {
      const limit = Number.parseInt(searchParams.get("limit") ?? "1", 10);
      const offset = Number.parseInt(searchParams.get("offset") ?? "0", 10);
      return fulfillJson({
        items: [],
        total: 0,
        limit,
        offset,
        filter_options: { endpoints: [] },
      });
    }

    if (pathname === "/api/stats/spending") {
      return fulfillJson({
        summary: {
          total_cost_micros: 250000,
          successful_request_count: 11,
          priced_request_count: 9,
          unpriced_request_count: 2,
        },
        top_spending_models: [],
      });
    }

    if (pathname === "/api/stats/throughput") {
      return fulfillJson({ average_rpm: 0.5, total_requests: 42 });
    }

    if (pathname === "/api/stats/api-family") {
      return fulfillJson({ groups: [] });
    }

    if (pathname === "/api/stats/connection-success-rates") {
      return fulfillJson([]);
    }

    if (pathname === "/api/connections/by-models") {
      return fulfillJson([]);
    }

    if (pathname === "/api/routing-diagram") {
      return fulfillJson({ nodes: [], links: [] });
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

  await expect.poll(() => capturedHeaders.statsSummary.length).toBeGreaterThan(0);

  expect(capturedHeaders.bootstrap.every((value) => value === null)).toBe(true);
  expect(capturedHeaders.statsSummary.every((value) => value === "2")).toBe(true);
});
