import { expect, test, type Page } from "@playwright/test";
import {
  createDashboardRecentActivityResponse,
  createDashboardSnapshot,
} from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-18T00:00:00Z";
const routeReadyTimeout = 15_000;
const SIDEBAR_COLLAPSED_STORAGE_KEY = "prism.sidebarCollapsed";

interface CostingBehavior {
  fail?: boolean;
  onRequest?: () => Promise<void>;
}

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

function createUsageSnapshot() {
  return {
    generated_at: timestamp,
    time_range: {
      preset: "24h",
      start_at: "2026-04-17T00:00:00Z",
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
      hourly: [{ key: "all", label: "All requests", total_requests: 11, points: [] }],
      daily: [{ key: "all", label: "All requests", total_requests: 11, points: [] }],
    },
    latency_trends: {
      hourly: [],
      daily: [],
    },
    token_usage_trends: {
      hourly: [{ key: "all", label: "All models", total_tokens: 1650, points: [] }],
      daily: [{ key: "all", label: "All models", total_tokens: 1650, points: [] }],
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
    report_currency_code: "USD",
    report_currency_symbol: "$",
    endpoint_fx_mappings: [],
    timezone_preference: null,
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

function createRequestLogDetail() {
  return {
    summary: {
      id: 101,
      created_at: timestamp,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
      status_code: 502,
      response_time_ms: 125,
      is_stream: false,
    },
    request: {
      request_path: "/v1/responses",
      ingress_request_id: "ingress-101",
      attempt_number: 1,
      provider_correlation_id: "provider-corr-101",
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Prism QA Browser",
      upstream_user_agent: "Prism QA Browser",
      caller_client_display: "Prism QA Browser",
      upstream_client_display: "Prism QA Browser",
      user_agent_overridden: false,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      model_id: "gpt-4o-mini",
      resolved_target_model_id: null,
      api_family: "openai",
      endpoint_id: 1,
      connection_id: null,
      endpoint_base_url: "https://api.example.test",
      endpoint_description: "Primary endpoint",
      audit_enabled_at_request: false,
    },
    usage: {
      input_tokens: 12,
      output_tokens: 8,
      total_tokens: 20,
      success_flag: false,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 1000,
      output_cost_micros: 2000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 3000,
      total_cost_user_currency_micros: 3000,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: "1",
      fx_rate_source: "manual",
    },
    pricing: {
      pricing_snapshot_unit: "1M tokens",
      pricing_snapshot_input: "0.10",
      pricing_snapshot_output: "0.20",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: 1,
    },
  };
}

async function readSidebarCollapsed(page: Page) {
  return page.evaluate(
    (storageKey) => window.localStorage.getItem(storageKey),
    SIDEBAR_COLLAPSED_STORAGE_KEY,
  );
}

async function expectShellChrome(
  page: Page,
  options: { current: string; parent?: string },
) {
  await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByText("正在加载应用...")).toHaveCount(0, {
    timeout: routeReadyTimeout,
  });
  await expect(page.getByTestId("shell-breadcrumb")).toBeVisible({ timeout: routeReadyTimeout });
  await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText(options.current, {
    timeout: routeReadyTimeout,
  });

  if (options.parent) {
    await expect(page.getByTestId("shell-breadcrumb")).toContainText(options.parent, {
      timeout: routeReadyTimeout,
    });
  }

}

async function mockProtectedShellRoutes(
  page: Page,
  options: { costingBehavior?: CostingBehavior } = {},
) {
  const requestCounts = {
    costing: 0,
    models: 0,
    usageSnapshot: 0,
    dashboard: 0,
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

    if (pathname === "/api/settings/costing") {
      requestCounts.costing += 1;
      await options.costingBehavior?.onRequest?.();

      if (options.costingBehavior?.fail) {
        return fulfillJson({ detail: "settings unavailable" }, 500);
      }

      return fulfillJson(createCostingSettings());
    }

    if (pathname === "/api/models") {
      requestCounts.models += 1;
      return fulfillJson([createModelListItem()]);
    }

    if (pathname === "/api/stats/usage-snapshot") {
      requestCounts.usageSnapshot += 1;
      return fulfillJson(createUsageSnapshot());
    }

    if (pathname === "/api/stats/dashboard") {
      requestCounts.dashboard += 1;
      return fulfillJson(createDashboardSnapshot());
    }

    if (pathname === "/api/stats/dashboard/recent-activity") {
      return fulfillJson(createDashboardRecentActivityResponse([]));
    }

    if (pathname === "/api/settings/auth") {
      return fulfillJson(createAuthSettings());
    }


    if (pathname === "/api/config/header-blocklist-rules") {
      return fulfillJson([]);
    }

    if (pathname === "/api/config/user-agent-client-rules") {
      return fulfillJson([]);
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" });
    }

    if (pathname === "/api/stats/requests/101") {
      return fulfillJson(createRequestLogDetail());
    }

    return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
  });

  await page.addInitScript((storageKey) => {
    window.localStorage.removeItem(storageKey);
  }, SIDEBAR_COLLAPSED_STORAGE_KEY);

  return { requestCounts };
}

test.describe("protected shell sidebar regression", () => {
  test("keeps protected shell chrome behind the fallback until costing bootstrap resolves", async ({ page }) => {
    const costingGate = createDeferred();
    const { requestCounts } = await mockProtectedShellRoutes(page, {
      costingBehavior: { onRequest: () => costingGate.promise },
    });

    await page.goto("/observe?tab=analytics");

    await expect(page.getByText("正在加载应用...")).toBeVisible();
    await expect(page.getByTestId("shell-sidebar")).toHaveCount(0);
    await expect(page.getByTestId("shell-breadcrumb")).toHaveCount(0);
    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveCount(0);

    await page.waitForTimeout(200);
    expect(requestCounts.models).toBe(0);
    expect(requestCounts.usageSnapshot).toBe(0);

    costingGate.resolve();

    await expectShellChrome(page, { current: "仪表盘" });
    await expect(page.getByTestId("observe-dashboard")).toBeVisible();
    await expect.poll(() => requestCounts.usageSnapshot).toBeGreaterThan(0);
  });

  test("renders the dashboard shell and persists desktop collapse state", async ({ page }) => {
    await mockProtectedShellRoutes(page);

    await page.goto("/observe?tab=analytics");

    await expectShellChrome(page, { current: "仪表盘" });
    await expect(page.getByTestId("observe-dashboard")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expect.poll(() => readSidebarCollapsed(page), { timeout: routeReadyTimeout }).toBe("false");

    const sidebarToggle = page.getByRole("button", { name: "Toggle Sidebar" });

    await sidebarToggle.click();
    await expect.poll(() => readSidebarCollapsed(page), { timeout: routeReadyTimeout }).toBe("true");
    await expect(sidebarToggle).toBeVisible();

    await sidebarToggle.click();
    await expect.poll(() => readSidebarCollapsed(page), { timeout: routeReadyTimeout }).toBe("false");
    await expect(sidebarToggle).toBeVisible();
  });

  test("renders settings hash breadcrumbs with the section leaf as current", async ({ page }) => {
    await mockProtectedShellRoutes(page);

    await page.goto("/system/settings#authentication");

    await expectShellChrome(page, { parent: "设置", current: "身份验证" });
  });

  test("renders request-log detail breadcrumbs while the detail sheet is open", async ({ page }) => {
    await mockProtectedShellRoutes(page);

    await page.goto("/observe/requests?request_id=101");

    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible({
      timeout: routeReadyTimeout,
    });
    await expectShellChrome(page, { parent: "请求日志", current: "#101" });
  });

  test("opens and closes the mobile drawer around route navigation", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await mockProtectedShellRoutes(page);

    await page.goto("/observe?tab=analytics");

    const sidebarToggle = page.getByRole("button", { name: "Toggle Sidebar" });

    await expect(page.getByTestId("shell-breadcrumb")).toBeVisible();
    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("仪表盘");
    await expect(page.getByTestId("shell-sidebar")).toHaveCount(0);

    await sidebarToggle.click();
    await expect(page.getByTestId("shell-sidebar")).toBeVisible();

    await page.getByTestId("shell-sidebar").getByRole("link", { name: "设置" }).click();
    await expect(page).toHaveURL(/\/system\/settings$/);
    await expect(page.getByTestId("shell-sidebar")).toHaveCount(0);
    await expect(page.getByTestId("shell-breadcrumb-current")).toHaveText("设置");

    await sidebarToggle.click();
    await expect(page.getByTestId("shell-sidebar")).toBeVisible();

    await page.locator('[data-slot="sheet-overlay"]').click({ position: { x: 380, y: 20 } });
    await expect(page.getByTestId("shell-sidebar")).toHaveCount(0);
  });
});
