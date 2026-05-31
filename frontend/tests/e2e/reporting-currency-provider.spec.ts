import { expect, test, type Page } from "@playwright/test";

const timestamp = "2026-04-11T00:00:00Z";
const routeReadyTimeout = 15_000;

interface ProfileFixture {
  id: number;
  name: string;
  isActive?: boolean;
  isDefault?: boolean;
}

interface CostingBehavior {
  fail?: boolean;
  onRequest?: () => Promise<void>;
}

type RequestCountsByProfile = Record<string, number>;

function createDeferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((nextResolve) => {
    resolve = nextResolve;
  });

  return { promise, resolve };
}

function createProfile({
  id,
  name,
  isActive = false,
  isDefault = false,
}: ProfileFixture) {
  return {
    id,
    name,
    description: null,
    is_active: isActive,
    is_default: isDefault,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  };
}

function createModelListItem(profileId: number) {
  return {
    id: profileId,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: `GPT-4o mini P${profileId}`,
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

function createUsageSnapshot(profileId: number) {
  const totalRequests = profileId === 2 ? 22 : 11;

  return {
    generated_at: timestamp,
    time_range: {
      preset: "24h",
      start_at: "2026-04-10T00:00:00Z",
      end_at: timestamp,
    },
    currency: { code: "USD", symbol: "$" },
    overview: {
      total_requests: totalRequests,
      success_requests: totalRequests - 1,
      failed_requests: 1,
      success_rate: 90.9,
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
      availability_percentage: 90.9,
      request_count: totalRequests,
      success_count: totalRequests - 1,
      failed_count: 1,
      interval_minutes: 60,
      cells: [],
    },
    request_trends: {
      hourly: [
        {
          key: "all",
          label: "All requests",
          total_requests: totalRequests,
          points: [],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All requests",
          total_requests: totalRequests,
          points: [],
        },
      ],
    },
    token_usage_trends: {
      hourly: [
        {
          key: "all",
          label: "All models",
          total_tokens: 1650,
          points: [],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All models",
          total_tokens: 1650,
          points: [],
        },
      ],
    },
    token_type_breakdown: {
      hourly: [],
      daily: [],
    },
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
    };
}

function createCostingSettings(profileId: number) {
  return {
    report_currency_code: profileId === 2 ? "JPY" : "EUR",
    report_currency_symbol: profileId === 2 ? "¥" : "€",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

function resolveProfileKey(headers: Record<string, string>) {
  return headers["x-profile-id"] ?? "none";
}

function parseProfileId(profileKey: string) {
  const parsed = Number.parseInt(profileKey, 10);
  return Number.isFinite(parsed) ? parsed : 1;
}

function incrementRequestCount(bucket: RequestCountsByProfile, profileKey: string) {
  bucket[profileKey] = (bucket[profileKey] ?? 0) + 1;
}

async function mockReportingCurrencyProtectedRoutes(
  page: Page,
  options: {
    profiles?: Array<ReturnType<typeof createProfile>>;
    locale?: "en" | "zh-CN";
    costingBehaviorByProfileId?: Record<number, CostingBehavior>;
  } = {},
) {
  const profiles =
    options.profiles ??
    [
      createProfile({ id: 1, name: "Default", isActive: true, isDefault: true }),
    ];
  const activeProfile = profiles.find((profile) => profile.is_active) ?? profiles[0] ?? null;
  const requestCounts = {
    costingByProfile: {} as RequestCountsByProfile,
    modelsByProfile: {} as RequestCountsByProfile,
    usageSnapshotByProfile: {} as RequestCountsByProfile,
  };
  let lastCostingProfileKey: string | null = null;

  await page.route("**/*", async (route) => {
    const pathname = new URL(route.request().url()).pathname;

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
        profiles,
        active_profile: activeProfile,
        profile_limits: { max_profiles: 5 },
      });
    }

    if (pathname === "/api/settings/costing") {
      const profileKey = resolveProfileKey(route.request().headers());
      const profileId = parseProfileId(profileKey);
      const behavior = options.costingBehaviorByProfileId?.[profileId];

      incrementRequestCount(requestCounts.costingByProfile, profileKey);
      lastCostingProfileKey = profileKey;

      await behavior?.onRequest?.();

      if (behavior?.fail) {
        return fulfillJson({ detail: "settings unavailable" }, 500);
      }

      return fulfillJson(createCostingSettings(profileId));
    }

    if (pathname === "/api/models") {
      const profileKey = resolveProfileKey(route.request().headers());
      incrementRequestCount(requestCounts.modelsByProfile, profileKey);
      return fulfillJson([createModelListItem(parseProfileId(profileKey))]);
    }

    if (pathname === "/api/stats/usage-snapshot") {
      const profileKey = resolveProfileKey(route.request().headers());
      incrementRequestCount(requestCounts.usageSnapshotByProfile, profileKey);
      return fulfillJson(createUsageSnapshot(parseProfileId(profileKey)));
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.addInitScript(
    (seedLocale: "en" | "zh-CN") => localStorage.setItem("prism.locale", seedLocale),
    options.locale ?? "en",
  );

  return {
    getLastCostingProfileHeader: () => lastCostingProfileKey,
    requestCounts,
  };
}

test.describe("reporting currency provider", () => {
  test("keeps protected descendants behind the route fallback until costing bootstrap resolves", async ({ page }) => {
    const costingGate = createDeferred();
    const { getLastCostingProfileHeader, requestCounts } = await mockReportingCurrencyProtectedRoutes(page, {
      costingBehaviorByProfileId: {
        1: { onRequest: () => costingGate.promise },
      },
    });

    await page.goto("/dashboard?tab=analytics");

    await expect(page.getByText("Loading application...")).toBeVisible();
    await expect.poll(getLastCostingProfileHeader).toBe("1");
    await expect(page.getByTestId("shell-sidebar")).toHaveCount(0);

    await page.waitForTimeout(200);
    expect(requestCounts.modelsByProfile["1"] ?? 0).toBe(0);
    expect(requestCounts.usageSnapshotByProfile["1"] ?? 0).toBe(0);

    costingGate.resolve();

    await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByText("Loading application...")).toHaveCount(0, {
      timeout: routeReadyTimeout,
    });
    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect.poll(() => requestCounts.modelsByProfile["1"] ?? 0).toBeGreaterThan(0);
    await expect.poll(() => requestCounts.usageSnapshotByProfile["1"] ?? 0).toBeGreaterThan(0);
  });

  test("fails open and renders the protected shell when costing bootstrap returns an error", async ({ page }) => {
    const { getLastCostingProfileHeader, requestCounts } = await mockReportingCurrencyProtectedRoutes(page, {
      costingBehaviorByProfileId: {
        1: { fail: true },
      },
    });

    await page.goto("/dashboard?tab=analytics");

    await expect.poll(getLastCostingProfileHeader).toBe("1");
    await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByText("Loading application...")).toHaveCount(0, {
      timeout: routeReadyTimeout,
    });
    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect.poll(() => requestCounts.modelsByProfile["1"] ?? 0).toBeGreaterThan(0);
    await expect.poll(() => requestCounts.usageSnapshotByProfile["1"] ?? 0).toBeGreaterThan(0);
  });

  test("re-engages the route fallback immediately when the selected profile changes", async ({ page }) => {
    const secondProfileCostingGate = createDeferred();
    const { getLastCostingProfileHeader, requestCounts } = await mockReportingCurrencyProtectedRoutes(page, {
      profiles: [
        createProfile({ id: 1, name: "Default", isActive: true, isDefault: true }),
        createProfile({ id: 2, name: "Blue Team" }),
      ],
      costingBehaviorByProfileId: {
        2: { onRequest: () => secondProfileCostingGate.promise },
      },
    });

    await page.goto("/dashboard?tab=analytics");

    await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByText("Loading application...")).toHaveCount(0, {
      timeout: routeReadyTimeout,
    });
    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect.poll(() => requestCounts.modelsByProfile["1"] ?? 0).toBeGreaterThan(0);
    await expect.poll(() => requestCounts.usageSnapshotByProfile["1"] ?? 0).toBeGreaterThan(0);

    await page.getByTestId("shell-profile-switcher").getByRole("button").click();
    await page.getByRole("menuitem", { name: /Blue Team/ }).click();

    await expect(page.getByText("Loading application...")).toBeVisible();
    await expect.poll(getLastCostingProfileHeader).toBe("2");
    await expect(page.getByTestId("shell-sidebar")).toHaveCount(0);
    await expect(page.getByTestId("usage-controls-toolbar")).toHaveCount(0);

    await page.waitForTimeout(200);
    expect(requestCounts.modelsByProfile["2"] ?? 0).toBe(0);
    expect(requestCounts.usageSnapshotByProfile["2"] ?? 0).toBe(0);

    secondProfileCostingGate.resolve();

    await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByText("Loading application...")).toHaveCount(0, {
      timeout: routeReadyTimeout,
    });
    await expect(page.getByTestId("usage-controls-toolbar")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect(page.getByTestId("shell-profile-switcher")).toContainText("Blue Team", {
      timeout: routeReadyTimeout,
    });
    await expect.poll(() => requestCounts.modelsByProfile["2"] ?? 0).toBeGreaterThan(0);
    await expect.poll(() => requestCounts.usageSnapshotByProfile["2"] ?? 0).toBeGreaterThan(0);
  });
});
