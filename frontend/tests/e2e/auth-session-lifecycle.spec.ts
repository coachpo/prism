import { expect, test, type BrowserContext, type Page, type Route } from "@playwright/test";
import {
  createDashboardRecentActivityResponse,
  createEmptyDashboardSnapshot,
} from "./dashboard-aggregate-fixtures";

const timestamp = "2026-04-28T12:00:00Z";
const clientReadyTimeoutMs = 45_000;

// Full-regression hosts can delay client-side login-form readiness beyond the
// default 30-second test timeout.
test.setTimeout(60_000);

type AuthState = {
  authEnabled: boolean;
  authenticated: boolean;
  email: string | null;
  emailBoundAt: string | null;
  passwordVersion: number;
  pendingEmail: string | null;
  username: string | null;
};

function createAuthSettings(state: AuthState) {
  return {
    auth_enabled: state.authEnabled,
    username: state.username,
    email: state.email,
    email_bound_at: state.emailBoundAt,
    pending_email: state.pendingEmail,
    email_verification_required: false,
    has_password: true,
    proxy_key_limit: 10,
  };
}

async function fulfillJson(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function installAuthLifecycleRoutes(context: BrowserContext) {
  const authState: AuthState = {
    authEnabled: true,
    authenticated: false,
    email: "admin@example.com",
    emailBoundAt: timestamp,
    passwordVersion: 1,
    pendingEmail: null,
    username: "admin",
  };

  await context.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      await route.continue();
      return;
    }

    if (pathname === "/api/auth/status") {
      await fulfillJson(route, {
        state: authState.authEnabled ? "enabled" : "disabled",
        transition_state: null,
        login_available: authState.authEnabled,
        effective_generation: "1",
        retry_after_seconds: null,
      });
      return;
    }

    if (pathname === "/api/auth/public-bootstrap") {
      await fulfillJson(route, {
        authenticated: authState.authEnabled ? authState.authenticated : false,
        auth_enabled: authState.authEnabled,
        username: authState.authEnabled && authState.authenticated ? authState.username : null,
      });
      return;
    }

    if (pathname === "/api/auth/session") {
      if (!authState.authEnabled || !authState.authenticated) {
        await fulfillJson(route, { detail: "Authentication required" }, 401);
        return;
      }

      await fulfillJson(route, {
        authenticated: true,
        auth_enabled: true,
        username: authState.username,
        subject_key: "auth:subject:1",
      });
      return;
    }

    if (pathname === "/api/auth/refresh") {
      if (!authState.authEnabled || !authState.authenticated) {
        await fulfillJson(route, { detail: "Invalid refresh token" }, 401);
        return;
      }

      await fulfillJson(route, {
        authenticated: true,
        auth_enabled: true,
        username: authState.username,
        subject_key: "auth:subject:1",
      });
      return;
    }

    if (pathname === "/api/auth/login" && request.method() === "POST") {
      const payload = request.postDataJSON() as { username: string };
      authState.authenticated = true;
      authState.username = payload.username;
      await fulfillJson(route, {
        authenticated: true,
        auth_enabled: true,
        username: authState.username,
        subject_key: "auth:subject:1",
      });
      return;
    }

    if (pathname === "/api/auth/logout" && request.method() === "POST") {
      authState.authenticated = false;
      await route.fulfill({ status: 204, headers: { "Cache-Control": "private, no-store" } });
      return;
    }

    if (pathname === "/api/settings/costing") {
      await fulfillJson(route, {
        report_currency_code: "USD",
        report_currency_symbol: "$",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
      return;
    }

    if (pathname === "/api/settings/log-retention") {
      await fulfillJson(route, {
        request_logs_retention_days: 30,
        statistics_retention_days: 30,
        audit_logs_retention_days: 30,
        loadbalance_events_retention_days: 30,
      });
      return;
    }

    if (pathname === "/api/settings/timezone") {
      await fulfillJson(route, { timezone_preference: null, effective_timezone: "UTC" });
      return;
    }

    if (pathname === "/api/models") {
      if (new URL(request.url()).searchParams.get("include") === "route_readiness") {
        await fulfillJson(route, {
          items: [],
          route_readiness: {
            route_witness_generation: null,
            configuration: { state: "unknown", reason_codes: ["test_fixture"] },
            application: { state: "unknown", reason_codes: ["test_fixture"] },
            configuration_ready_model_count: null,
            route_ready_model_count: null,
            route_witness_count: null,
            representative_witness: null,
          },
        });
      } else {
        await fulfillJson(route, []);
      }
      return;
    }

    if (pathname === "/api/endpoints") {
      await fulfillJson(route, []);
      return;
    }

    if (pathname === "/api/loadbalance/strategies") {
      await fulfillJson(route, []);
      return;
    }

    if (pathname === "/api/stats/dashboard") {
      await fulfillJson(route, createEmptyDashboardSnapshot());
      return;
    }

    if (pathname === "/api/stats/dashboard/recent-activity") {
      await fulfillJson(route, createDashboardRecentActivityResponse([]));
      return;
    }

    if (pathname === "/api/stats/query-context") {
      await fulfillJson(route, {
        query_context: "signed-token",
        usage_bounds: { from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z" },
        usage_coverage: { requested_preset: "24h", from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z", source: "raw", complete: true, gaps: [] },
        event_bounds: { from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z" },
        request_bounds: { from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z" },
        generated_at: "2026-08-09T00:00:00Z",
      });
      return;
    }

    if (pathname === "/api/stats/usage-summary") {
      await fulfillJson(route, {
        generated_at: "2026-08-09T00:00:00Z",
        coverage: { requested_preset: "24h", from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z", source: "raw", complete: true, gaps: [] },
        cost_segments: [],
        request_count: 0,
        http_success_count: 0,
        http_failed_count: 0,
        http_success_rate: null,
        completed_count: 0,
        stream_error_count: 0,
        client_disconnected_count: 0,
        failed_count: 0,
        ttft_sample_count: 0,
        p50_ttft_ms: null,
        p95_ttft_ms: null,
        output_rate_sample_count: 0,
        avg_output_rate_tps: null,
        total_tokens: null,
        cache_basis_request_count: 0,
        cache_basis_input_tokens: null,
        cache_basis_cache_read_tokens: null,
        cache_basis_cache_creation_tokens: null,
        pricing_reconciliation: { pricing_eligible_request_count: 0, pricing_ineligible_request_count: 0, priced_request_count: 0, unpriced_request_count: 0, pricing_unknown_request_count: 0, unpriced_reason_counts: { PRICING_DISABLED: 0, MISSING_TOKEN_USAGE: 0, STREAM_USAGE_UNAVAILABLE: 0, MISSING_PRICE_DATA: 0 }, pricing_coverage_state: "no_eligible" },
        window_average_rpm: null,
        window_average_tpm: null,
      });
      return;
    }

    if (pathname === "/api/stats/usage-series") {
      await fulfillJson(route, { generated_at: "2026-08-09T00:00:00Z", coverage: { requested_preset: "24h", from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z", source: "raw", complete: true, gaps: [] }, metric: "requests", group_by: "none", selection_basis: "request_count", interval: "1h", series_limit: 6, truncated: false, series: [] });
      return;
    }

    if (pathname === "/api/stats/dashboard/now") {
      await fulfillJson(route, { generated_at: "2026-08-09T00:00:00Z", health: { stale: false, cache_lag_ms: null }, rolling: { window_minutes: 30, coverage: { requested_preset: "rolling", from_time: "2026-08-09T00:00:00Z", to_time: "2026-08-09T00:30:00Z", source: "raw", complete: true, gaps: [] }, request_count: 0, token_sample_count: 0, token_coverage_complete: true, token_count: null, rpm: null, tpm: null }, enabled_model_count: 0 });
      return;
    }

    if (pathname === "/api/stats/observe-activity") {
      await fulfillJson(route, { generated_at: "2026-08-09T00:00:00Z", coverage: { requested_preset: "24h", from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z", source: "raw", complete: true, gaps: [] }, items: [], has_more: false });
      return;
    }

if (pathname === "/api/stats/usage-errors") {
      await fulfillJson(route, { generated_at: "2026-08-09T00:00:00Z", coverage: { requested_preset: "24h", from_time: "2026-08-08T00:00:00Z", to_time: "2026-08-09T00:00:00Z", source: "raw", complete: true, gaps: [] }, requests_context: { view: "ingress_chains", query_context: "signed-token", final_from_time: "2026-08-08T00:00:00Z", final_to_time: "2026-08-09T00:00:00Z", base_request_filters: {} }, summary: { request_count: 0, http_error_count: 0, stream_error_count: 0, failed_count: 0, client_disconnected_count: 0, diagnostic_stream_anomaly_count: 0 }, timeline: [], http_statuses: [], stream_outcomes: [], groups: [], other: { http_statuses: { count: 0, denominator: 0, percentage: null, request_filters: null }, stream_outcomes: { count: 0, denominator: 0, percentage: null, request_filters: null }, groups: { count: 0, denominator: 0, percentage: null, request_filters: null } } });
      return;
    }

    if (pathname === "/api/loadbalance/incidents") {
      await fulfillJson(route, { active_bans: [], recent_events: [], generated_at: timestamp });
      return;
    }

    if (pathname === "/api/stats/summary") {
      await fulfillJson(route, {
        total_requests: 0,
        success_count: 0,
        error_count: 0,
        avg_response_time_ms: 0,
        total_tokens: 0,
        items: [],
      });
      return;
    }

    if (pathname === "/api/stats/spending") {
      await fulfillJson(route, { summary: { total_cost_micros: 0 }, items: [] });
      return;
    }

    if (pathname === "/api/stats/throughput") {
      await fulfillJson(route, { average_rpm: 0, total_requests: 0, buckets: [] });
      return;
    }

    if (pathname === "/api/stats/requests") {
      await fulfillJson(route, { items: [], total: 0, page: 1, page_size: 20 });
      return;
    }

    if (pathname === "/api/stats/connection-success-rates") {
      await fulfillJson(route, []);
      return;
    }

    if (pathname === "/api/settings/auth" && request.method() === "GET") {
      await fulfillJson(route, createAuthSettings(authState));
      return;
    }

    if (pathname === "/api/settings/auth" && request.method() === "PUT") {
      const payload = request.postDataJSON() as {
        auth_enabled: boolean;
        password?: string | null;
        username?: string | null;
      };

      const nextUsername = payload.username?.trim() || null;
      const usernameChanged = nextUsername !== authState.username;
      const passwordChanged = Boolean(payload.password);

      authState.authEnabled = payload.auth_enabled;
      authState.username = nextUsername;

      if (!payload.auth_enabled || usernameChanged || passwordChanged) {
        authState.authenticated = false;
        if (passwordChanged) {
          authState.passwordVersion += 1;
        }
      }

      await fulfillJson(route, createAuthSettings(authState));
      return;
    }

    if (pathname === "/api/settings/auth/proxy-keys") {
      await fulfillJson(route, []);
      return;
    }


    if (pathname === "/api/config/header-blocklist-rules") {
      await fulfillJson(route, []);
      return;
    }

    if (pathname === "/api/config/user-agent-client-rules") {
      await fulfillJson(route, []);
      return;
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });
}

async function installSetupReadinessRoutes(page: Page, mode: "fresh" | "unknown" | "degraded") {
  await page.route("**/api/endpoints", async (route) => {
    if (mode === "degraded") {
      await fulfillJson(route, { code: "upstream_unavailable", detail: "端点读取暂不可用" }, 503);
      return;
    }
    await fulfillJson(route, mode === "fresh" ? [{ id: 1, name: "setup-endpoint" }] : []);
  });
  await page.route("**/api/loadbalance/strategies", async (route) => {
    await fulfillJson(route, mode === "fresh" ? [{
      id: 1,
      profile_id: 1,
      name: "default",
      is_default: true,
      attached_model_count: 0,
      created_at: "2026-08-09T00:00:00Z",
      updated_at: "2026-08-09T00:00:00Z",
      legacy_strategy_type: "fill-first",
      failure_status_codes: [408, 429, 500, 502, 503, 504],
      ban_mode: "off",
      retry_base_delay_ms: 100,
      retry_backoff_multiplier: 2,
      retry_jitter_ratio: 0,
      retry_max_delay_ms: 1000,
      cycle_retry_attempt_limit: 0,
      ban_cumulative_retry_attempt_threshold: 0,
      ban_duration_seconds: 0,
    }] : []);
  });
  await page.route("**/api/models*", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname !== "/api/models") {
      await route.continue();
      return;
    }
    if (url.searchParams.get("include") !== "route_readiness") {
      await route.continue();
      return;
    }
    if (mode === "unknown") {
      await fulfillJson(route, { items: [] });
      return;
    }
    await fulfillJson(route, {
      items: [],
      route_readiness: {
        route_witness_generation: "7",
        configuration: { state: "ready", reason_codes: [] },
        application: { state: "ready", reason_codes: [] },
        configuration_ready_model_count: 1,
        route_ready_model_count: 1,
        route_witness_count: 1,
        representative_witness: null,
        route_schedule: {
          schedule_limited: false,
          limited_witness_count: 0,
          total_witness_count: 1,
        },
      },
    });
  });
  await page.route("**/api/pricing-templates*", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("include") !== "setup_readiness") {
      await route.continue();
      return;
    }
    await fulfillJson(route, {
      evaluated_route_witness_generation: "7",
      pricing_template_generation: 1,
      pricing_reference_generation: 1,
      configuration: { state: "ready", reason_codes: [] },
      application: { state: "ready", reason_codes: [] },
      route_witness_count: 1,
      applied_witness_count: 1,
      cost_ready_witness_count: 1,
      cost_ready: true,
      representative_matching: null,
    });
  });
  await page.route("**/api/settings/auth/proxy-keys*", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("include") !== "setup_readiness") {
      await route.continue();
      return;
    }
    await fulfillJson(route, {
      evaluated_route_witness_generation: "7",
      proxy_key_owner_revision: "1:",
      configuration: { state: "ready", reason_codes: [] },
      application: { state: "ready", reason_codes: [] },
      route_witness_count: 1,
      matching_witness_count: 1,
      optional_attribution_witness_count: null,
      representative_matching: null,
      representative_optional_attribution: null,
    });
  });
}

async function openLoginForm(page: Page) {
  await page.goto("/auth/login", { waitUntil: "domcontentloaded", timeout: clientReadyTimeoutMs });
  const usernameInput = page.getByLabel("用户名");
  await expect(usernameInput).toBeVisible({ timeout: clientReadyTimeoutMs });
  return usernameInput;
}

async function loginToProxyKeys(page: Page) {
  const usernameInput = await openLoginForm(page);
  await usernameInput.fill("admin");
  await page.getByLabel("密码", { exact: true }).fill("password123");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByTestId("observe-page")).toBeVisible();

  await page.goto("/system/proxy-keys");
  await expect(page).toHaveURL(/\/system\/proxy-keys$/);
  await expect(page.getByRole("heading", { name: "代理密钥" })).toBeVisible();
}


test.describe("auth session lifecycle", () => {
  test.beforeEach(async ({ context }) => {
    await installAuthLifecycleRoutes(context);
  });

  test("renders fresh setup facts as 4/4 and collapses once until manually reopened", async ({ page }) => {
    await loginToProxyKeys(page);
    await installSetupReadinessRoutes(page, "fresh");
    await page.goto("/observe");
    await expect(page.getByTestId("setup-card-summary")).toBeVisible();
    await expect(page.getByRole("button", { name: "展开配置事实" })).toBeVisible();
    await page.getByRole("button", { name: "展开配置事实" }).click();
    await expect(page.getByText("路由配置 4 / 4")).toBeVisible();
    await expect(page.getByRole("link", { name: "验证接入" })).toBeVisible();
  });

  test("keeps setup unknown when the analyzer snapshot is unavailable", async ({ page }) => {
    await loginToProxyKeys(page);
    await installSetupReadinessRoutes(page, "unknown");
    await page.goto("/observe");
    await expect(page.getByText("配置状态暂不可确认")).toBeVisible();
    await expect(page.getByText("路由配置 0 / 4")).toHaveCount(0);
  });

  test("surfaces a retention coverage gap instead of presenting the window as complete", async ({ page }) => {
    await loginToProxyKeys(page);
    await page.route("**/api/stats/usage-summary**", async (route) => {
      await fulfillJson(route, {
        generated_at: "2026-08-09T00:00:00Z",
        coverage: {
          requested_preset: "24h",
          from_time: "2026-08-08T00:00:00Z",
          to_time: "2026-08-09T00:00:00Z",
          source: "raw",
          complete: false,
          gaps: [{ from_time: "2026-08-08T04:00:00Z", to_time: "2026-08-08T05:00:00Z", reason: "retention" }],
        },
        cost_segments: [],
        request_count: 0,
        http_success_count: 0,
        http_failed_count: 0,
        http_success_rate: null,
        completed_count: 0,
        stream_error_count: 0,
        client_disconnected_count: 0,
        failed_count: 0,
        ttft_sample_count: 0,
        p50_ttft_ms: null,
        p95_ttft_ms: null,
        output_rate_sample_count: 0,
        avg_output_rate_tps: null,
        total_tokens: null,
        cache_basis_request_count: 0,
        cache_basis_input_tokens: null,
        cache_basis_cache_read_tokens: null,
        cache_basis_cache_creation_tokens: null,
        pricing_reconciliation: { pricing_eligible_request_count: 0, pricing_ineligible_request_count: 0, priced_request_count: 0, unpriced_request_count: 0, pricing_unknown_request_count: 0, unpriced_reason_counts: { PRICING_DISABLED: 0, MISSING_TOKEN_USAGE: 0, STREAM_USAGE_UNAVAILABLE: 0, MISSING_PRICE_DATA: 0 }, pricing_coverage_state: "no_eligible" },
        window_average_rpm: null,
        window_average_tpm: null,
      });
    });
    await page.goto("/observe");
    await expect(page.getByText("事件来源覆盖不完整，不能确认没有事件；筛选结果可能缺失。")).toBeVisible();
  });

  test("shows degraded setup reads without turning the failure into 0/4", async ({ page }) => {
    await loginToProxyKeys(page);
    await installSetupReadinessRoutes(page, "degraded");
    await page.goto("/observe");
    await expect(page.getByText("部分配置读取失败")).toBeVisible({
      timeout: clientReadyTimeoutMs,
    });
    await expect(page.getByText("路由配置 0 / 4")).toHaveCount(0);
  });

  test("shows the open-access explainer when auth is disabled", async ({ page }) => {
    await page.route("**/api/auth/status", (route) =>
      fulfillJson(route, { state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null }),
    );
    await page.goto("/auth/login");
    await expect(page.getByText("本实例未启用身份验证")).toBeVisible();
    await expect(page.getByRole("button", { name: "进入控制台" })).toBeVisible();
  });

  test("fails closed when a disabled instance returns 401 and the fixed probe is also unauthorized", async ({ page }) => {
    await page.route("**/api/auth/status", (route) =>
      fulfillJson(route, { state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null }),
    );
    await page.route("**/api/auth/public-bootstrap", (route) =>
      fulfillJson(route, { authenticated: false, auth_enabled: false, username: null }),
    );
    await page.route("**/api/models*", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname !== "/api/models") {
        await route.continue();
        return;
      }
      await fulfillJson(route, { detail: "Authentication required" }, 401);
    });

    await page.goto("/observe");
    await expect(page.getByText("暂时无法确认登录状态")).toBeVisible();
    await expect(page.getByText("受保护的数据已暂停显示，以避免给出不完整的结论。")).toBeVisible();
  });

  test("renders the lockout countdown on a typed 429 login response", async ({ page }) => {
    await page.route("**/api/auth/login", (route) =>
      route.fulfill({
        status: 429,
        headers: { "Content-Type": "application/json", "Retry-After": "873" },
        body: JSON.stringify({
          code: "auth_login_locked",
          detail: "登录尝试过多，请稍后重试",
          params: {},
          details: { retry_at: new Date(Date.now() + 873_000).toISOString(), retry_after_seconds: 873 },
          request_id: "req-1",
        }),
      }),
    );
    const usernameInput = await openLoginForm(page);
    await usernameInput.fill("admin");
    await page.getByLabel("密码", { exact: true }).fill("wrong");
    await page.getByRole("button", { name: "登录" }).click();
    await expect(page.getByText(/登录尝试过多/)).toBeVisible();
    // The locked state is card-level: the form collapses and the submit button
    // is replaced by a disabled button carrying the countdown.
    await expect(page.getByTestId("login-locked-countdown")).toHaveText(/^\d{2}:\d{2}:\d{2}$/);
    await expect(page.getByRole("button", { name: /^锁定中/ })).toBeDisabled();
    await expect(page.getByRole("button", { name: "登录", exact: true })).toHaveCount(0);
  });

  test("propagates session expiry to a second tab without waiting for the 12-minute timer", async ({
    context,
    page,
  }) => {
    await loginToProxyKeys(page);
    const secondPage = await context.newPage();
    await secondPage.goto("/observe");
    await expect(secondPage.getByTestId("observe-page")).toBeVisible();

    // Simulate the cross-tab expiry broadcast (SPEC §11) with the real
    // shared session generation. Headless Playwright does not reliably
    // dispatch cross-page storage events, so the event is delivered
    // directly to the receiving document (browser-native delivery is
    // exercised by the disabling/rotation tests below via reload).
    const broadcastPayload = await page.evaluate(() => {
      const generation = localStorage.getItem("prism.authSessionGeneration") ?? "unknown";
      return JSON.stringify({
        event_id: crypto.randomUUID(),
        origin_tab_id: "other-tab",
        sequence: 1,
        session_generation_id: generation,
        kind: "session_expired",
      });
    });
    await secondPage.evaluate((raw) => {
      window.dispatchEvent(new StorageEvent("storage", { key: "prism.authStateVersion", newValue: raw }));
    }, broadcastPayload);

    await expect(secondPage.getByText("会话已过期")).toBeVisible();
    await expect(page.getByText("会话已过期")).toBeVisible();
    await secondPage.getByRole("button", { name: "重新登录" }).click();
    await expect(secondPage).toHaveURL(/\/auth\/login/);
  });

  test("logs in to a protected shell route and logs out cleanly", async ({ page }) => {
    await loginToProxyKeys(page);

    await page.getByRole("button", { name: /admin/i }).click();
    await page.getByRole("menuitem", { name: "退出登录" }).click();

    await expect(page).toHaveURL(/\/auth\/login(?:\?.*)?$/);
    await expect(page.getByLabel("用户名")).toBeVisible();
  });

  test("disabling auth in one tab clears stale session identity in another tab without breaking the shell", async ({
    context,
    page,
  }) => {
    await loginToProxyKeys(page);

    const controlPage = await context.newPage();
    await controlPage.goto("/system/proxy-keys");
    await expect(controlPage.getByRole("heading", { name: "代理密钥" })).toBeVisible();

    await controlPage.evaluate(async () => {
      await fetch("/api/settings/auth", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ auth_enabled: false, username: "admin", password: null }),
      });
      localStorage.setItem(
        "prism.authStateVersion",
        JSON.stringify({ event_id: crypto.randomUUID(), origin_tab_id: "control", sequence: 1, session_generation_id: "current-gen", kind: "auth_changed", target_generation: "2" }),
      );
    });
    await controlPage.reload();

    await expect(controlPage.getByText("身份验证已禁用")).toBeVisible();
    await expect(page.getByText("身份验证已禁用")).toBeVisible();
    await expect(page).toHaveURL(/\/system\/proxy-keys$/);
  });

  test("changing operator credentials forces stale tabs back to login", async ({ context, page }) => {
    await loginToProxyKeys(page);

    const controlPage = await context.newPage();
    await controlPage.goto("/system/proxy-keys");
    await expect(controlPage.getByRole("heading", { name: "代理密钥" })).toBeVisible();

    await controlPage.evaluate(async () => {
      await fetch("/api/settings/auth", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          auth_enabled: true,
          username: "admin-rotated",
          password: "new-password-123",
        }),
      });
      localStorage.setItem(
        "prism.authStateVersion",
        JSON.stringify({ event_id: crypto.randomUUID(), origin_tab_id: "control", sequence: 1, session_generation_id: "current-gen", kind: "auth_changed", target_generation: "3" }),
      );
    });
    await controlPage.reload();

    await expect(controlPage).toHaveURL(/\/auth\/login(?:\?.*)?$/);
    await expect(page).toHaveURL(/\/auth\/login(?:\?.*)?$/);
    await expect(page.getByLabel("用户名")).toBeVisible();
  });
});
