import { expect, test, type Page } from "@playwright/test";

const fixedTimestamp = "2026-04-28T12:00:00Z";
const maskedDatabaseUrl = "postgres://prism:***@db.local/prism?sslpassword=***";
const maskedRuntimeKey = "runtime-secret-********";
const maskedJwtKey = "jwt-signing-********";
const maskedBundleKey = "bundle-key-********";
const maskedSmtpPassword = "smtp-password-********";
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


function createApplyCapabilities() {
  const hotFields = [
    "http.cors_allowed_origins",
    "auth.access_token_ttl_seconds",
    "auth.refresh_token_ttl_seconds",
    "auth.reset_code_ttl_seconds",
    "auth.access_cookie_name",
    "auth.refresh_cookie_name",
    "auth.cookie_secure",
    "mail.enabled",
    "mail.from",
    "mail.reply_to",
    "mail.smtp.host",
    "mail.smtp.port",
    "mail.smtp.mode",
    "mail.smtp.ehlo_hostname",
    "mail.smtp.auth",
    "mail.smtp.username",
    "mail.smtp.password_file",
    "mail.smtp.password",
    "mail.smtp.timeout",
    "mail.smtp.tls_server_name",
    "runtime.buffering_mode",
    "runtime.transport.max_idle_conns",
    "runtime.transport.max_idle_conns_per_host",
    "runtime.transport.max_conns_per_host",
    "runtime.transport.idle_conn_timeout",
    "runtime.transport.request_timeout",
    "runtime.transport.response_header_timeout",
    "runtime.transport.tls_handshake_timeout",
    "runtime.transport.expect_continue_timeout",
    "database.management_admission.m2_max_concurrent",
    "database.management_admission.m3_max_concurrent",
  ];
  const restartFields = [
    ["server.host", "server-host-change"],
    ["server.port", "server-port-change"],
    ["server.docs_enabled", ""],
    ["database.url", "database-url-change"],
    ["database.pools.total_max_conns", ""],
    ["database.pools.management.max_conns", ""],
    ["database.pools.management.min_idle_conns", ""],
    ["database.pools.runtime_execution.max_conns", ""],
    ["database.pools.runtime_execution.min_idle_conns", ""],
    ["database.pools.runtime_telemetry.max_conns", ""],
    ["database.pools.runtime_telemetry.min_idle_conns", ""],
    ["database.pools.runtime_feedback.max_conns", ""],
    ["database.pools.runtime_feedback.min_idle_conns", ""],
    ["database.pools.realtime.max_conns", ""],
    ["database.pools.realtime.min_idle_conns", ""],
    ["database.pools.cache_refresh.max_conns", ""],
    ["database.pools.cache_refresh.min_idle_conns", ""],
    ["database.pools.background_jobs.max_conns", ""],
    ["database.pools.background_jobs.min_idle_conns", ""],
    ["runtime.side_effects.attempt_timeout", ""],
    ["runtime.secretEncryptionKey", ""],
    ["auth.jwtSigningKey", "auth-jwt-signing-key-change"],
    ["stateTransfer.bundleEncryptionKey", "state-transfer-bundle-encryption-key-change"],
  ] as const;
  return Object.fromEntries([
    ...hotFields.map((field) => [field, { mode: "hot_apply" }]),
    ...restartFields.map(([field, confirmation_token]) => [
      field,
      confirmation_token ? { mode: "restart_required", confirmation_token } : { mode: "restart_required" },
    ]),
  ]);
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
    apply_capabilities: createApplyCapabilities(),
    apply_result: undefined as
      | {
        applied_now_fields: string[];
        restart_required_fields: string[];
        unchanged_fields: string[];
        pending_hot_apply_fields: string[];
        failed_hot_apply_fields: string[];
      }
      | undefined,
    planned_changes: undefined as
      | { changed_fields: { field: string; mode: string }[]; restart_required: boolean }
      | undefined,
    values: {
      server: { host: "127.0.0.1", port: 18000, docs_enabled: true },
      database: {
        pools: {
          total_max_conns: 42,
          management: { max_conns: 6, min_idle_conns: 0 },
          runtime_execution: { max_conns: 14, min_idle_conns: 1 },
          runtime_telemetry: { max_conns: 7, min_idle_conns: 0 },
          runtime_feedback: { max_conns: 3, min_idle_conns: 0 },
          realtime: { max_conns: 4, min_idle_conns: 0 },
          cache_refresh: { max_conns: 4, min_idle_conns: 0 },
          background_jobs: { max_conns: 4, min_idle_conns: 0 },
        },
        management_admission: { m2_max_concurrent: 8, m3_max_concurrent: 4 },
      },
      runtime: {
        buffering_mode: "buffered",
        transport: {
          max_idle_conns: 100,
          max_idle_conns_per_host: 10,
          max_conns_per_host: 0,
          idle_conn_timeout: "90s",
          request_timeout: "60s",
          response_header_timeout: "30s",
          tls_handshake_timeout: "10s",
          expect_continue_timeout: "1s",
        },
        side_effects: { attempt_timeout: "10s" },
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
      mail: {
        enabled: true,
        from: "Prism <noreply@example.com>",
        reply_to: "support@example.com",
        smtp: {
          host: "smtp.example.com",
          port: 587,
          mode: "starttls_required",
          ehlo_hostname: "prism.example.com",
          auth: "plain",
          username: "smtp-user",
          password_file: null,
          timeout: "15s",
          tls_server_name: "smtp.example.com",
        },
      },
    },
    secrets: {
      "database.url": { configured: true, editable: true, masked: maskedDatabaseUrl },
      "runtime.secretEncryptionKey": { configured: true, editable: false, masked: maskedRuntimeKey },
      "auth.jwtSigningKey": { configured: true, editable: true, masked: maskedJwtKey },
      "stateTransfer.bundleEncryptionKey": { configured: true, editable: true, masked: maskedBundleKey },
      "mail.smtp.password": { configured: true, editable: true, masked: maskedSmtpPassword },
    },
  };
}

type BootstrapTestResponse = ReturnType<typeof createBootstrapResponse>;
type BootstrapTestUpdatePayload = {
  values: BootstrapTestResponse["values"];
  secret_updates?: Record<string, { action: string; value?: string }>;
  confirmations?: string[];
};

type MockOptions = {
  bootstrapGate?: Promise<void>;
  bootstrapResponse?: BootstrapTestResponse;
  validateFailure?: { status: number; body: unknown };
  validateResponse?: BootstrapTestResponse;
  updateResponse?: (payload: BootstrapTestUpdatePayload, current: BootstrapTestResponse) => BootstrapTestResponse;
};

async function mockSettingsStartupRoutes(page: Page, options: MockOptions = {}) {
  const profile = createProfile();
  let bootstrapResponse = options.bootstrapResponse ?? createBootstrapResponse();
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
      return fulfillJson(options.validateResponse ?? bootstrapResponse);
    }
    if (pathname === "/api/config/bootstrap" && request.method() === "PUT") {
      const payload = request.postDataJSON() as BootstrapTestUpdatePayload;
      updateRequests.push(payload);
      if (options.updateResponse) {
        bootstrapResponse = options.updateResponse(payload, bootstrapResponse);
        return fulfillJson(bootstrapResponse);
      }
      const restartRequired = payload.values.server.port !== bootstrapResponse.values.server.port;
      bootstrapResponse = {
        ...bootstrapResponse,
        file_revision: bootstrapResponse.file_revision + 1,
        document_etag: "etag-8",
        updated_at: "2026-04-28T12:05:00Z",
        restart_required: restartRequired,
        apply_result: {
          applied_now_fields: restartRequired ? [] : ["mail.smtp.password_file"],
          restart_required_fields: restartRequired ? ["server.port"] : [],
          unchanged_fields: [],
          pending_hot_apply_fields: [],
          failed_hot_apply_fields: [],
        },
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

  await expect(page.getByText("Eligible settings apply immediately after save; structural settings are written to config.json and require a Prism restart.")).toBeVisible();
  await expect(page.getByText(maskedDatabaseUrl)).toBeVisible();
  await expect(page.getByText(maskedRuntimeKey)).toBeVisible();
  await expect(page.getByText(maskedJwtKey)).toBeVisible();
  await expect(page.getByText(maskedBundleKey)).toBeVisible();
  await expect(page.getByText(maskedSmtpPassword)).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Database URL" })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "JWT signing key" })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "SMTP password", exact: true })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "Request timeout" })).toHaveValue("60s");
  await expect(page.getByText(forbiddenSecretSentinel)).toHaveCount(0);
});

test("missing PostgreSQL pool lanes hydrate defaults and save canonical pools", async ({ page }) => {
  const response = createBootstrapResponse();
  const pools = response.values.database.pools as Partial<typeof response.values.database.pools>;
  delete pools.runtime_feedback;
  delete pools.cache_refresh;
  const routes = await mockSettingsStartupRoutes(page, { bootstrapResponse: response });

  await page.goto("/settings#startup");

  await expect(page.getByRole("tab", { name: "Startup" })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("spinbutton", { name: "Runtime feedback max conns" })).toHaveValue("3");
  await expect(page.getByRole("spinbutton", { name: "Runtime feedback min idle" })).toHaveValue("0");
  await expect(page.getByRole("spinbutton", { name: "Cache refresh max conns" })).toHaveValue("4");
  await expect(page.getByRole("spinbutton", { name: "Cache refresh min idle" })).toHaveValue("0");

  await page.getByRole("button", { name: "Save startup config" }).click();
  await expect(page.getByText("Saved to config.json and applied immediately.")).toBeVisible();

  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()[0]).toMatchObject({
    values: {
      database: {
        pools: {
          runtime_feedback: { max_conns: 3, min_idle_conns: 0 },
          cache_refresh: { max_conns: 4, min_idle_conns: 0 },
        },
      },
    },
  });
});

test("mail and SMTP startup card renders every safe field", async ({ page }) => {
  await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Mail and SMTP")).toBeVisible();

  await expect(page.getByRole("switch", { name: "Enable auth email delivery" })).toBeChecked();
  await expect(page.getByRole("textbox", { name: "Mail sender" })).toHaveValue("Prism <noreply@example.com>");
  await expect(page.getByRole("textbox", { name: "Reply-to address" })).toHaveValue("support@example.com");
  await expect(page.getByRole("textbox", { name: "SMTP host" })).toHaveValue("smtp.example.com");
  await expect(page.getByRole("spinbutton", { name: "SMTP port" })).toHaveValue("587");
  await expect(page.getByRole("combobox", { name: "SMTP mode" })).toContainText("STARTTLS required");
  await expect(page.getByRole("textbox", { name: "EHLO hostname" })).toHaveValue("prism.example.com");
  await expect(page.getByRole("combobox", { name: "SMTP auth" })).toContainText("Plain username and password");
  await expect(page.getByRole("textbox", { name: "SMTP username" })).toHaveValue("smtp-user");
  await expect(page.getByRole("textbox", { name: "SMTP password file" })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "SMTP password", exact: true })).toHaveValue("");
  await expect(page.getByRole("textbox", { name: "SMTP timeout" })).toHaveValue("15s");
  await expect(page.getByRole("textbox", { name: "TLS server name" })).toHaveValue("smtp.example.com");
});

test("legacy missing mail hydrates as disabled and saves canonical disabled mail", async ({ page }) => {
  const legacyResponse = createBootstrapResponse();
  delete (legacyResponse.values as { mail?: unknown }).mail;
  const routes = await mockSettingsStartupRoutes(page, { bootstrapResponse: legacyResponse });

  await page.goto("/settings#startup");
  await expect(page.getByText("Mail and SMTP")).toBeVisible();
  await expect(page.getByRole("switch", { name: "Enable auth email delivery" })).not.toBeChecked();
  await expect(page.getByRole("textbox", { name: "SMTP host" })).toBeDisabled();

  await page.getByRole("button", { name: "Save startup config" }).click();
  await expect(page.getByText("Saved to config.json and applied immediately.")).toBeVisible();

  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()[0]).toMatchObject({
    values: { mail: { enabled: false, from: null, reply_to: null, smtp: null } },
  });
});

test("enabled blank mail validates on the client without backend validate", async ({ page }) => {
  const legacyResponse = createBootstrapResponse();
  delete (legacyResponse.values as { mail?: unknown }).mail;
  const routes = await mockSettingsStartupRoutes(page, { bootstrapResponse: legacyResponse });

  await page.goto("/settings#startup");
  await expect(page.getByText("Mail and SMTP")).toBeVisible();

  await page.getByRole("switch", { name: "Enable auth email delivery" }).click();
  await page.getByRole("button", { name: "Validate" }).click();

  await expect(page.getByRole("row", { name: /mail\.from.*Mail sender is required when mail is enabled\./i })).toBeVisible();
  await expect(page.getByRole("row", { name: /mail\.smtp\.host.*SMTP host is required when mail is enabled\./i })).toBeVisible();
  await expect(page.getByRole("row", { name: /mail\.smtp\.port.*SMTP port must be an integer from 1 to 65535\./i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(0);
  expect(routes.getUpdateRequests()).toHaveLength(0);
});

test("enabled mail saves password file separately from SMTP password secret updates", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Mail and SMTP")).toBeVisible();

  await page.getByRole("textbox", { name: "SMTP password file" }).fill("/run/secrets/prism-smtp-password");
  await page.getByRole("button", { name: "Save startup config" }).click();
  await expect(page.getByText("Saved to config.json and applied immediately.")).toBeVisible();

  expect(routes.getUpdateRequests()).toHaveLength(1);
  const payload = routes.getUpdateRequests()[0] as {
    values: { mail: { smtp: Record<string, unknown> } };
    secret_updates: Record<string, { action: string; value?: string }>;
  };
  expect(payload.values.mail.smtp.password_file).toBe("/run/secrets/prism-smtp-password");
  expect(payload.values.mail.smtp).not.toHaveProperty("password");
  expect(payload.secret_updates["mail.smtp.password"]).toEqual({ action: "preserve" });
});

test("enabled mail saves inline SMTP password replacement only through secret updates", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Mail and SMTP")).toBeVisible();

  await page.getByRole("textbox", { name: "SMTP password", exact: true }).fill("new-smtp-password");
  await page.getByRole("button", { name: "Save startup config" }).click();
  await expect(page.getByText("Saved to config.json and applied immediately.")).toBeVisible();

  expect(routes.getUpdateRequests()).toHaveLength(1);
  const payload = routes.getUpdateRequests()[0] as {
    values: { mail: { smtp: Record<string, unknown> } };
    secret_updates: Record<string, { action: string; value?: string }>;
  };
  expect(payload.secret_updates["mail.smtp.password"]).toEqual({ action: "replace", value: "new-smtp-password" });
  expect(payload.values.mail.smtp.password_file).toBeNull();
  expect(payload.values.mail.smtp).not.toHaveProperty("password");
});

test("disabling mail after staging SMTP password saves disabled mail without replacement", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Mail and SMTP")).toBeVisible();

  await page.getByRole("textbox", { name: "SMTP password", exact: true }).fill("discarded-smtp-password");
  await page.getByRole("switch", { name: "Enable auth email delivery" }).click();
  await expect(page.getByRole("switch", { name: "Enable auth email delivery" })).not.toBeChecked();
  await expect(page.getByRole("textbox", { name: "SMTP password", exact: true })).toHaveValue("");
  await page.getByRole("button", { name: "Save startup config" }).click();
  await expect(page.getByText("Saved to config.json and applied immediately.")).toBeVisible();

  expect(routes.getUpdateRequests()).toHaveLength(1);
  const payload = routes.getUpdateRequests()[0] as {
    values: { mail: { enabled: boolean; from: unknown; reply_to: unknown; smtp: unknown } };
    secret_updates: Record<string, { action: string; value?: string }>;
  };
  expect(payload.values.mail).toEqual({ enabled: false, from: null, reply_to: null, smtp: null });
  expect(payload.secret_updates["mail.smtp.password"]).toEqual({ action: "preserve" });
});

test("invalid settings hashes normalize and tab switches keep the URL in sync", async ({ page }) => {
  await mockSettingsStartupRoutes(page);

  await page.goto("/settings#bogus");

  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByRole("tab", { name: "Profile" })).toHaveAttribute("aria-selected", "true");

  await page.getByRole("tab", { name: "Global" }).click();
  await expect(page).toHaveURL(/\/settings#authentication$/);
  await expect(page.getByRole("tab", { name: "Global" })).toHaveAttribute("aria-selected", "true");

  await page.getByRole("tab", { name: "Startup" }).click();
  await expect(page).toHaveURL(/\/settings#startup$/);
  await expect(page.getByRole("tab", { name: "Startup" })).toHaveAttribute("aria-selected", "true");

  await page.getByRole("tab", { name: "Profile" }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.getByRole("tab", { name: "Profile" })).toHaveAttribute("aria-selected", "true");
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

test("client validation blocks blank request timeout validate and save", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("textbox", { name: "Request timeout" }).fill("");
  await page.getByRole("button", { name: "Validate" }).click();

  await expect(page.getByRole("row", { name: /runtime\.transport\.request_timeout.*This field is required\./i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(0);

  await page.getByRole("button", { name: "Save startup config" }).click();

  await expect(page.getByRole("row", { name: /runtime\.transport\.request_timeout.*This field is required\./i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(0);
  expect(routes.getUpdateRequests()).toHaveLength(0);
});

test("runtime side-effects timeout renders distinct field", async ({ page }) => {
  await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await expect(page.getByText("Runtime side effects", { exact: true })).toBeVisible();
  await expect(page.getByText("Telemetry enqueue attempts use this timeout separately from upstream provider requests.")).toBeVisible();
  await expect(page.locator('label[for="startup-side-effects-attempt-timeout"]').locator("..").getByText("Restart required")).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Telemetry enqueue attempt timeout" })).toHaveValue("10s");
  await expect(page.getByRole("textbox", { name: "Request timeout" })).toHaveValue("60s");
});

test("blank side-effects timeout blocks client validate and save", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page);

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("textbox", { name: "Telemetry enqueue attempt timeout" }).fill("");
  await page.getByRole("button", { name: "Validate" }).click();

  await expect(page.getByRole("row", { name: /runtime\.side_effects\.attempt_timeout.*Telemetry enqueue attempt timeout is required\./i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(0);

  await page.getByRole("button", { name: "Save startup config" }).click();

  await expect(page.getByRole("row", { name: /runtime\.side_effects\.attempt_timeout.*Telemetry enqueue attempt timeout is required\./i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(0);
  expect(routes.getUpdateRequests()).toHaveLength(0);
});

test("side-effects timeout validate shows restart-required planned row", async ({ page }) => {
  const validateResponse = createBootstrapResponse();
  validateResponse.planned_changes = {
    changed_fields: [{ field: "runtime.side_effects.attempt_timeout", mode: "restart_required" }],
    restart_required: true,
  };
  const routes = await mockSettingsStartupRoutes(page, { validateResponse });

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("textbox", { name: "Telemetry enqueue attempt timeout" }).fill("15s");
  await page.getByRole("button", { name: "Validate" }).click();

  await expect(page.getByRole("row", { name: /Telemetry enqueue attempt timeout.*Will be written for the next restart\./i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getValidateRequests()[0]).toMatchObject({
    values: { runtime: { side_effects: { attempt_timeout: "15s" } } },
  });
  expect(routes.getUpdateRequests()).toHaveLength(0);
});

test("backend validation renders planned hot-apply and restart effects before save", async ({ page }) => {
  const validateResponse = createBootstrapResponse();
  validateResponse.planned_changes = {
    changed_fields: [
      { field: "http.cors_allowed_origins", mode: "hot_apply" },
      { field: "server.docs_enabled", mode: "restart_required" },
    ],
    restart_required: true,
  };
  const routes = await mockSettingsStartupRoutes(page, { validateResponse });

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByLabel("CORS allowed origins").fill("http://localhost:15173, http://127.0.0.1:15173");
  await page.getByRole("switch", { name: "Docs enabled" }).click();
  await page.getByRole("button", { name: "Validate" }).click();

  await expect(page.getByRole("row", { name: /CORS allowed origins.*Will apply immediately after save\./i })).toBeVisible();
  await expect(page.getByRole("row", { name: /Docs enabled.*Will be written for the next restart\./i })).toBeVisible();
  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()).toHaveLength(0);
});

test("hot-only save shows applied-immediately rows without restart alert or dangerous checklist", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page, {
    updateResponse: (payload, current) => ({
      ...current,
      file_revision: current.file_revision + 1,
      document_etag: "etag-hot-8",
      updated_at: "2026-04-28T12:05:00Z",
      restart_required: false,
      apply_result: {
        applied_now_fields: ["runtime.transport.request_timeout"],
        restart_required_fields: [],
        unchanged_fields: [],
        pending_hot_apply_fields: [],
        failed_hot_apply_fields: [],
      },
      values: payload.values,
    }),
  });

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("textbox", { name: "Request timeout" }).fill("45s");
  await expect(page.getByText("1 immediate change staged")).toBeVisible();
  await expect(page.getByText("Dangerous changes staged")).toHaveCount(0);
  await page.getByRole("button", { name: "Save startup config" }).click();

  await expect(page.getByText("Saved to config.json and applied immediately.")).toBeVisible();
  await expect(page.getByRole("row", { name: /Request timeout.*Applied immediately to the running process\./i })).toBeVisible();
  await expect(page.getByRole("row", { name: /Saved for the next Prism restart\./i })).toHaveCount(0);
  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()).toHaveLength(1);
});

test("mixed save shows immediate rows plus restart-required alert", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page, {
    updateResponse: (payload, current) => ({
      ...current,
      file_revision: current.file_revision + 1,
      document_etag: "etag-mixed-8",
      updated_at: "2026-04-28T12:05:00Z",
      restart_required: true,
      apply_result: {
        applied_now_fields: ["http.cors_allowed_origins"],
        restart_required_fields: ["server.port"],
        unchanged_fields: [],
        pending_hot_apply_fields: [],
        failed_hot_apply_fields: [],
      },
      values: payload.values,
    }),
  });

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByLabel("CORS allowed origins").fill("http://localhost:15173, http://127.0.0.1:15173");
  await page.getByRole("spinbutton", { name: "Server port" }).fill("18001");
  await expect(page.getByText("1 immediate and 1 restart change staged")).toBeVisible();
  await page.getByLabel("Server port changes the management and proxy port after restart").check();
  await page.getByRole("button", { name: "Save startup config" }).click();

  await expect(page.getByRole("alertdialog", { name: "Save dangerous startup changes?" })).toBeVisible();
  await page.getByRole("button", { name: "Save and require restart" }).click();

  await expect(page.getByText("Saved to config.json. Eligible settings applied immediately; structural settings require restart.")).toBeVisible();
  await expect(page.getByRole("row", { name: /CORS allowed origins.*Applied immediately to the running process\./i })).toBeVisible();
  await expect(page.getByRole("row", { name: /Server port.*Saved for the next Prism restart\./i })).toBeVisible();
  expect(routes.getUpdateRequests()[0]).toMatchObject({
    confirmations: ["server-port-change"],
  });
});

test("restart fields without backend confirmation tokens do not require dangerous checklist", async ({ page }) => {
  const routes = await mockSettingsStartupRoutes(page, {
    updateResponse: (payload, current) => ({
      ...current,
      file_revision: current.file_revision + 1,
      document_etag: "etag-docs-8",
      updated_at: "2026-04-28T12:05:00Z",
      restart_required: true,
      apply_result: {
        applied_now_fields: [],
        restart_required_fields: ["server.docs_enabled"],
        unchanged_fields: [],
        pending_hot_apply_fields: [],
        failed_hot_apply_fields: [],
      },
      values: payload.values,
    }),
  });

  await page.goto("/settings#startup");
  await expect(page.getByText("Review and save")).toBeVisible();

  await page.getByRole("switch", { name: "Docs enabled" }).click();
  await expect(page.getByText("1 restart change staged")).toBeVisible();
  await expect(page.getByText("Dangerous changes staged")).toHaveCount(0);
  await page.getByRole("button", { name: "Save startup config" }).click();

  await expect(page.getByRole("alertdialog", { name: "Save dangerous startup changes?" })).toHaveCount(0);
  await expect(page.getByText("Saved to config.json. Structural settings require restart.")).toBeVisible();
  await expect(page.getByRole("row", { name: /Docs enabled.*Saved for the next Prism restart\./i })).toBeVisible();
  expect(routes.getUpdateRequests()[0]).toMatchObject({ confirmations: [] });
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

  await expect(page.getByText("Saved to config.json. Structural settings require restart.")).toBeVisible();
  await expect(page.getByText("Restart required").first()).toBeVisible();
  await expect(page.getByRole("row", { name: /Server port.*Saved for the next Prism restart\./i })).toBeVisible();
  await expect(page.getByRole("row", { name: /Applied immediately to the running process\./i })).toHaveCount(0);
  await expect(page.getByText("Saved to config.json and applied immediately.")).toHaveCount(0);
  expect(routes.getValidateRequests()).toHaveLength(1);
  expect(routes.getUpdateRequests()).toHaveLength(1);
  expect(routes.getValidateRequests()[0]).toMatchObject({
    values: { runtime: { transport: { request_timeout: "60s" } } },
  });
  expect(routes.getUpdateRequests()[0]).toMatchObject({
    confirmations: ["server-port-change"],
    values: { server: { port: 18001 }, runtime: { transport: { request_timeout: "60s" } } },
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
