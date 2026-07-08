import { expect, test, type Page } from "@playwright/test";
import {
  createDashboardRecentActivityResponse,
  createDashboardSnapshot,
} from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-11T00:00:00Z";
const routeReadyTimeout = 15_000;

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

function createModelListItem() {
  return {
    id: 1,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini P1",
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

function createUsageSnapshot() {
  return {
    generated_at: timestamp,
    time_range: {
      preset: "24h",
      start_at: "2026-04-10T00:00:00Z",
      end_at: timestamp,
    },
    currency: { code: "USD", symbol: "$" },
    overview: {
      total_requests: 11,
      success_requests: 10,
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
    request_trends: {
      hourly: [
        {
          key: "all",
          label: "All requests",
          total_requests: 11,
          points: [],
        },
      ],
      daily: [
        {
          key: "all",
          label: "All requests",
          total_requests: 11,
          points: [],
        },
      ],
    },
    latency_trends: {
      hourly: [],
      daily: [],
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

function createCostingSettings() {
  return {
    report_currency_code: "EUR",
    report_currency_symbol: "€",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  };
}

function resolveProfileKey(headers: Record<string, string>) {
  return headers["x-profile-id"] ?? "none";
}

function incrementRequestCount(bucket: RequestCountsByProfile, profileKey: string) {
  bucket[profileKey] = (bucket[profileKey] ?? 0) + 1;
}

async function mockReportingCurrencyProtectedRoutes(
  page: Page,
  options: {
    costingBehaviorByProfileId?: Record<number, CostingBehavior>;
  } = {},
) {
  const requestCounts = {
    costingByProfile: {} as RequestCountsByProfile,
    modelsByProfile: {} as RequestCountsByProfile,
    usageSnapshotByProfile: {} as RequestCountsByProfile,
    dashboardByProfile: {} as RequestCountsByProfile,
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

    if (pathname === "/api/settings/costing") {
      const profileKey = resolveProfileKey(route.request().headers());
      const behavior = options.costingBehaviorByProfileId?.[1];

      incrementRequestCount(requestCounts.costingByProfile, profileKey);
      lastCostingProfileKey = profileKey;

      await behavior?.onRequest?.();

      if (behavior?.fail) {
        return fulfillJson({ detail: "settings unavailable" }, 500);
      }

      return fulfillJson(createCostingSettings());
    }

    if (pathname === "/api/models") {
      const profileKey = resolveProfileKey(route.request().headers());
      incrementRequestCount(requestCounts.modelsByProfile, profileKey);
      return fulfillJson([createModelListItem()]);
    }

    if (pathname === "/api/stats/usage-snapshot") {
      const profileKey = resolveProfileKey(route.request().headers());
      incrementRequestCount(requestCounts.usageSnapshotByProfile, profileKey);
      return fulfillJson(createUsageSnapshot());
    }

    if (pathname === "/api/stats/dashboard") {
      const profileKey = resolveProfileKey(route.request().headers());
      incrementRequestCount(requestCounts.dashboardByProfile, profileKey);
      return fulfillJson(createDashboardSnapshot());
    }

    if (pathname === "/api/stats/dashboard/recent-activity") {
      return fulfillJson(createDashboardRecentActivityResponse([]));
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

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

    await page.goto("/observe?tab=analytics");

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
    await expect(page.getByTestId("observe-dashboard")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect.poll(() => requestCounts.usageSnapshotByProfile["1"] ?? 0).toBeGreaterThan(0);
  });

  test("fails open and renders the protected shell when costing bootstrap returns an error", async ({ page }) => {
    const { getLastCostingProfileHeader, requestCounts } = await mockReportingCurrencyProtectedRoutes(page, {
      costingBehaviorByProfileId: {
        1: { fail: true },
      },
    });

    await page.goto("/observe?tab=analytics");

    await expect.poll(getLastCostingProfileHeader).toBe("1");
    await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByText("Loading application...")).toHaveCount(0, {
      timeout: routeReadyTimeout,
    });
    await expect(page.getByTestId("observe-dashboard")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect.poll(() => requestCounts.usageSnapshotByProfile["1"] ?? 0).toBeGreaterThan(0);
  });

  test("keeps the frozen Default profile pinned while the shell boots", async ({ page }) => {
    const { getLastCostingProfileHeader, requestCounts } = await mockReportingCurrencyProtectedRoutes(page);

    await page.goto("/observe?tab=analytics");

    await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
    await expect(page.getByText("Loading application...")).toHaveCount(0, {
      timeout: routeReadyTimeout,
    });
    await expect(page.getByTestId("observe-dashboard")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect.poll(getLastCostingProfileHeader).toBe("1");
    await expect.poll(() => requestCounts.usageSnapshotByProfile["1"] ?? 0).toBeGreaterThan(0);
    expect(requestCounts.usageSnapshotByProfile["2"] ?? 0).toBe(0);
  });
});
