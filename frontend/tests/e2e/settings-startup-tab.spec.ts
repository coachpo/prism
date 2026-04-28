import { expect, test, type Page } from "@playwright/test";

const fixedTimestamp = "2026-04-28T12:00:00Z";
const maskedDatabaseUrl = "postgres://prism:***@db.local/prism?sslpassword=***";
const maskedRuntimeKey = "runtime-secret-********";
const maskedJwtKey = "jwt-signing-********";
const maskedBundleKey = "bundle-key-********";
const forbiddenSecretSentinel = "should-never-render-secret-sentinel";

function createProfile() {
  return {
    id: 1,
    name: "Default",
    description: null,
    is_active: true,
    is_default: true,
    is_editable: true,
    version: 1,
    created_at: fixedTimestamp,
    deleted_at: null,
    updated_at: fixedTimestamp,
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

function createCostingSettings() {
  return {
    report_currency_code: "EUR",
    report_currency_symbol: "€",
    endpoint_fx_mappings: [],
    timezone_preference: null,
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

function createBootstrapResponse() {
  return {
    config_path: "/etc/prism/config.json",
    schema_version: 1,
    file_revision: 7,
    loaded_revision: 7,
    document_etag: "etag-7",
    loaded_document_etag: "etag-7",
    created_at: fixedTimestamp,
    updated_at: fixedTimestamp,
    restart_required: false,
    writable: true,
    values: {
      server: { host: "127.0.0.1", port: 18000, docs_enabled: true },
      database: {
        runtime_pool: { max_conns: 20, min_idle_conns: 2 },
        management_pool: { max_conns: 10, min_idle_conns: 1 },
        management_admission: { m2_max_concurrent: 8, m3_max_concurrent: 4 },
      },
      runtime: {
        buffering_mode: "buffered",
        transport: {
          max_idle_conns: 100,
          max_idle_conns_per_host: 10,
          max_conns_per_host: 0,
          idle_conn_timeout: "90s",
          response_header_timeout: "30s",
          tls_handshake_timeout: "10s",
          expect_continue_timeout: "1s",
        },
      },
      http: { cors_allowed_origins: ["http://localhost:15173"] },
      auth: {
        access_token_ttl_seconds: 900,
        refresh_token_ttl_seconds: 604800,
        reset_code_ttl_seconds: 900,
        access_cookie_name: "prism_access",
        refresh_cookie_name: "prism_refresh",
        cookie_secure: false,
      },
    },
    secrets: {
      "database.url": { configured: true, editable: true, masked: maskedDatabaseUrl },
      "runtime.secretEncryptionKey": { configured: true, editable: false, masked: maskedRuntimeKey },
      "auth.jwtSigningKey": { configured: true, editable: true, masked: maskedJwtKey },
      "stateTransfer.bundleEncryptionKey": { configured: true, editable: true, masked: maskedBundleKey },
    },
  };
}

type MockOptions = {
  bootstrapGate?: Promise<void>;
  validateFailure?: { status: number; body: unknown };
};

async function mockSettingsStartupRoutes(page: Page, options: MockOptions = {}) {
  const profile = createProfile();
  let bootstrapResponse = createBootstrapResponse();
  const validateRequests: unknown[] = [];
  const updateRequests: unknown[] = [];

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
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson(createCostingSettings());
    }
    if (pathname === "/api/settings/auth") {
      return fulfillJson(createAuthSettings());
    }
    if (pathname === "/api/settings/retention") {
      return fulfillJson(createRetentionSettings());
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
    if (pathname === "/api/config/bootstrap" && request.method() === "GET") {
      await options.bootstrapGate;
      return fulfillJson(bootstrapResponse);
    }
    if (pathname === "/api/config/bootstrap/validate" && request.method() === "POST") {
      validateRequests.push(request.postDataJSON());
      if (options.validateFailure) {
        return fulfillJson(options.validateFailure.body, options.validateFailure.status);
      }
      return fulfillJson(bootstrapResponse);
    }
    if (pathname === "/api/config/bootstrap" && request.method() === "PUT") {
      const payload = request.postDataJSON() as { values: typeof bootstrapResponse.values };
      updateRequests.push(payload);
      bootstrapResponse = {
        ...bootstrapResponse,
        file_revision: bootstrapResponse.file_revision + 1,
        document_etag: "etag-8",
        updated_at: "2026-04-28T12:05:00Z",
        restart_required: true,
        values: payload.values,
      };
      return fulfillJson(bootstrapResponse);
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en");
  });

  return {
    getValidateRequests: () => validateRequests,
    getUpdateRequests: () => updateRequests,
  };
}

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

test("settings startup hash opens the tab, shows loading state, warning copy, and masked secrets", async ({ page }) => {
  const gate = deferred();
  await mockSettingsStartupRoutes(page, { bootstrapGate: gate.promise });

  await page.goto("/settings#startup");

  await expect(page.getByRole("tab", { name: "Profile" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Global" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Startup" })).toHaveAttribute("aria-selected", "true");
  await expect(page.locator('[data-slot="skeleton"]').first()).toBeVisible();

  gate.resolve();

  await expect(page.getByText("These settings are loaded when Prism starts. Saving updates config.json; restart Prism for changes to take effect.")).toBeVisible();
  await expect(page.getByText(maskedDatabaseUrl)).toBeVisible();
  await expect(page.getByText(maskedRuntimeKey)).toBeVisible();
  await expect(page.getByText(maskedJwtKey)).toBeVisible();
  await expect(page.getByText(maskedBundleKey)).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Database URL" })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "JWT signing key" })).toHaveValue("");
  await expect(page.getByText(forbiddenSecretSentinel)).toHaveCount(0);
});

test("client validation reports invalid port and CORS without backend validate", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("spinbutton", { name: "Server port" }).fill("0");
  await page.getByLabel("CORS allowed origins").fill("localhost:15173");
  await page.getByRole("button", { name: "Validate" }).click();

  await expect(page.getByText("Server port must be an integer from 1 to 65535.").first()).toBeVisible();
  await expect(page.getByText("CORS origins must be absolute URLs.").first()).toBeVisible();
  await expect(page.getByRole("row", { name: /server\.port/i })).toBeVisible();
  await expect(page.getByRole("row", { name: /http\.cors_allowed_origins/i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(0);
});

test("dangerous port save requires checklist, AlertDialog confirmation, and shows restart-required state", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("spinbutton", { name: "Server port" }).fill("18001");
  await expect(page.getByText("Dangerous changes staged")).toBeVisible();
  await page.getByLabel("Server port changes the management and proxy port after restart").check();
  await page.getByRole("button", { name: "Save startup config" }).click();

  await expect(page.getByRole("alertdialog", { name: "Save dangerous startup changes?" })).toBeVisible();
  await page.getByRole("button", { name: "Save and require restart" }).click();

  await expect(page.getByText("Saved to config.json. Restart Prism for changes to take effect.")).toBeVisible();
  await expect(page.getByText("Restart required").first()).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()[0]).toMatchObject({
    confirmations: ["server-port-change"],
    values: { server: { port: 18001 } },
  });
});

test("backend validation errors map into review output", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page, {
    validateFailure: {
      status: 422,
      body: {
        detail: {
          field: "database.url",
          message: "Database URL cannot be reached",
          required_confirmations: ["database-url-change"],
        },
      },
    },
  });

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("button", { name: "Validate" }).click();

  await expect(page.getByRole("row", { name: /database\.url.*Database URL cannot be reached/i })).toBeVisible();
  await expect(page.getByText("Required confirmations: database-url-change.")).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()).toHaveLength(0);
});
