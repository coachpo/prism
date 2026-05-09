import { expect, test, type BrowserContext, type Page, type Route } from "@playwright/test";

const timestamp = "2026-04-28T12:00:00Z";

type AuthState = {
  authEnabled: boolean;
  authenticated: boolean;
  email: string | null;
  emailBoundAt: string | null;
  passwordVersion: number;
  pendingEmail: string | null;
  username: string | null;
};

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

async function seedLocale(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });
}

async function fulfillJson(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function installAuthLifecycleRoutes(context: BrowserContext) {
  const profile = createProfile();
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
      await fulfillJson(route, { auth_enabled: authState.authEnabled });
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
      });
      return;
    }

    if (pathname === "/api/auth/logout" && request.method() === "POST") {
      authState.authenticated = false;
      await fulfillJson(route, {
        authenticated: false,
        auth_enabled: authState.authEnabled,
        username: null,
      });
      return;
    }

    if (pathname === "/api/profiles/bootstrap") {
      await fulfillJson(route, {
        profiles: [profile],
        active_profile: profile,
        profile_limits: { max_profiles: 5 },
      });
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
      await fulfillJson(route, []);
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

    if (pathname === "/api/vendors") {
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

async function loginToProxyKeys(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Username").fill("admin");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();

  await page.goto("/proxy-api-keys");
  await expect(page).toHaveURL(/\/proxy-api-keys$/);
  await expect(page.getByRole("heading", { name: "Proxy API Keys" })).toBeVisible();
}

test.describe("auth session lifecycle", () => {
  test.beforeEach(async ({ context, page }) => {
    await installAuthLifecycleRoutes(context);
    await seedLocale(page);
  });

  test("logs in to a protected shell route and logs out cleanly", async ({ page }) => {
    await loginToProxyKeys(page);

    await page.getByRole("button", { name: /admin/i }).click();
    await page.getByRole("menuitem", { name: "Sign out" }).click();

    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByLabel("Username")).toBeVisible();
  });

  test("disabling auth in one tab clears stale session identity in another tab without breaking the shell", async ({
    context,
    page,
  }) => {
    await loginToProxyKeys(page);

    const controlPage = await context.newPage();
    await seedLocale(controlPage);
    await controlPage.goto("/proxy-api-keys");
    await expect(controlPage.getByRole("heading", { name: "Proxy API Keys" })).toBeVisible();

    await controlPage.evaluate(async () => {
      await fetch("/api/settings/auth", {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ auth_enabled: false, username: "admin", password: null }),
      });
      localStorage.setItem("prism.authStateVersion", String(Date.now()));
    });
    await controlPage.reload();

    await expect(controlPage.getByText("Authentication disabled")).toBeVisible();
    await expect(page.getByText("Authentication disabled")).toBeVisible();
    await expect(page).toHaveURL(/\/proxy-api-keys$/);
  });

  test("changing operator credentials forces stale tabs back to login", async ({ context, page }) => {
    await loginToProxyKeys(page);

    const controlPage = await context.newPage();
    await seedLocale(controlPage);
    await controlPage.goto("/proxy-api-keys");
    await expect(controlPage.getByRole("heading", { name: "Proxy API Keys" })).toBeVisible();

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
      localStorage.setItem("prism.authStateVersion", String(Date.now()));
    });
    await controlPage.reload();

    await expect(controlPage).toHaveURL(/\/login$/);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.getByLabel("Username")).toBeVisible();
  });
});
