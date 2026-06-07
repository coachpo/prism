import { readFile } from "node:fs/promises";
import { expect, test, type Download, type Page } from "@playwright/test";

const fixedTimestamp = "2026-04-18T12:00:00Z";
const fixedDate = "2026-04-18";
const liveAuthoringCapabilityDefaults = {
  context_window_tokens: null,
  default_output_token_reserve: 4_096,
  max_context_utilization: 0.9,
};

type DownloadCapture = {
  download: string;
  href: string;
} | null;

const appReadyTimeout = 30_000;

async function gotoBackupSection(page: Page) {
  await page.goto("/settings");
  await expect(page.getByTestId("shell-sidebar")).toBeVisible({ timeout: appReadyTimeout });
  await expect(page.getByRole("tab", { name: "Profile" })).toBeVisible({ timeout: appReadyTimeout });
  await expect(page.getByText("Loading application...")).toHaveCount(0, { timeout: appReadyTimeout });

  await page.evaluate(() => {
    window.location.hash = "backup";
  });
  await expect(page).toHaveURL(/\/settings#backup$/);

  const backupSection = page.locator("section#backup");
  await expect(backupSection).toBeVisible({ timeout: appReadyTimeout });
  return backupSection;
}

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
    request_logs_retention_days: 30,
    statistics_retention_days: 30,
    audit_logs_retention_days: 30,
    loadbalance_events_retention_days: 30,
  };
}

function createModelListItem() {
  return {
    id: 1,
    profile_id: 1,
    vendor_id: null,
    vendor: null,
    api_family: "openai",
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini",
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    ...liveAuthoringCapabilityDefaults,
    access_targets: [],
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: fixedTimestamp,
    updated_at: fixedTimestamp,
  };
}

function createExportVendorRef() {
  return {
    key: "openai",
    name_hint: "OpenAI",
    description_hint: "Primary vendor",
    icon_key_hint: "openai",
  };
}

function createExportLoadbalanceStrategy(name = "Default export routing") {
  return {
    name,
    legacy_strategy_type: "single" as const,
    failure_status_codes: [429, 500],
    ban_mode: "off" as const,
    retry_base_delay_ms: 1000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 8000,
    cycle_retry_attempt_limit: 3,
    ban_cumulative_retry_attempt_threshold: 0,
    ban_duration_seconds: 0,
  };
}

function createExportConnection(overrides = {}) {
  return {
    ref: "default-connection",
    endpoint_name: "Default endpoint",
    api_family: "openai" as const,
    ...liveAuthoringCapabilityDefaults,
    pricing_template_name: null,
    is_active: true,
    name: "Default connection",
    auth_type: "openai" as const,
    custom_headers: null,
    openai_probe_endpoint_variant: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    ...overrides,
  };
}

function createExportModel(overrides = {}) {
  return {
    vendor_key: "openai",
    api_family: "openai" as const,
    model_id: "gpt-4o-mini",
    display_name: "GPT-4o mini",
    loadbalance_strategy_name: "Default export routing",
    ...liveAuthoringCapabilityDefaults,
    is_enabled: true,
    access_targets: [
      {
        position: 0,
        is_enabled: true,
        target_type: "connection" as const,
        connection_ref: "default-connection",
      },
    ],
    ...overrides,
  };
}

async function readDownloadedBundle(download: Download) {
  const downloadPath = await download.path();
  if (downloadPath === null) {
    throw new Error("Expected Playwright to persist the downloaded export bundle");
  }

  return JSON.parse(await readFile(downloadPath, "utf8"));
}

function createSafeExportBundle() {
  return {
    version: 3 as const,
    bundle_kind: "profile_config" as const,
    exported_at: `${fixedDate}T12:00:00Z`,
    vendor_refs: [createExportVendorRef()],
    endpoints: [
      {
        name: "Default endpoint",
        base_url: "https://safe.example.invalid",
        api_key_secret_ref: null,
        position: 0,
      },
    ],
    pricing_templates: [],
    connections: [createExportConnection()],
    loadbalance_strategies: [createExportLoadbalanceStrategy()],
    models: [createExportModel()],
    profile_settings: {
      timezone_preference: null,
      report_currency_code: "EUR",
      report_currency_symbol: "€",
      endpoint_fx_mappings: [],
    },
    header_blocklist_rules: [],
    user_agent_client_rules: [],
    secret_payload: {
      kind: "encrypted" as const,
      cipher: "fernet-v1" as const,
      key_id: "bundle-key-id",
      entries: [],
    },
  };
}

function createDangerousExportBundle() {
  return {
    version: 3 as const,
    bundle_kind: "profile_config" as const,
    exported_at: `${fixedDate}T12:00:00Z`,
    vendor_refs: [createExportVendorRef()],
    endpoints: [
      {
        name: "Default endpoint",
        base_url: "https://dangerous.example.invalid",
        api_key_secret_ref: "endpoint-secret-ref",
        position: 0,
      },
    ],
    pricing_templates: [],
    connections: [
      createExportConnection({
        ref: "dangerous-connection",
        context_window_tokens: 200_000,
        default_output_token_reserve: 8_192,
        max_context_utilization: 0.92,
        name: "Dangerous connection",
      }),
    ],
    loadbalance_strategies: [createExportLoadbalanceStrategy("Dangerous export routing")],
    models: [
      createExportModel({
        model_id: "gpt-4.1",
        display_name: "GPT-4.1",
        loadbalance_strategy_name: "Dangerous export routing",
        context_window_tokens: 262_144,
        default_output_token_reserve: 12_288,
        max_context_utilization: 0.95,
        access_targets: [
          {
            position: 0,
            is_enabled: true,
            target_type: "connection" as const,
            connection_ref: "dangerous-connection",
          },
        ],
      }),
    ],
    profile_settings: {
      timezone_preference: null,
      report_currency_code: "EUR",
      report_currency_symbol: "€",
      endpoint_fx_mappings: [],
    },
    header_blocklist_rules: [],
    user_agent_client_rules: [],
    secret_payload: {
      kind: "encrypted" as const,
      cipher: "fernet-v1" as const,
      key_id: "bundle-key-id",
      entries: [{ ref: "endpoint-secret-ref", ciphertext: "ciphertext" }],
    },
  };
}

async function mockSettingsRoutes(page: Page) {
  const profile = createProfile();
  let safeExportRequestCount = 0;
  let dangerousExportRequestCount = 0;
  const dangerousConfirmHeaders: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200, headers?: Record<string, string>) =>
      route.fulfill({
        status,
        contentType: "application/json",
        headers,
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
    if (pathname === "/api/settings/log-retention") {
      return fulfillJson(createRetentionSettings());
    }
    if (pathname === "/api/models") {
      return fulfillJson([createModelListItem()]);
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
    if (pathname === "/api/config/profile/export" && request.method() === "GET") {
      safeExportRequestCount += 1;
      return fulfillJson(createSafeExportBundle(), 200, {
        "Content-Disposition": 'attachment; filename="server-safe-name.json"',
      });
    }
    if (pathname === "/api/config/profile/export/with-secrets" && request.method() === "POST") {
      const dangerousConfirmHeader = (await request.allHeaders())["x-prism-dangerous-confirm"] ?? "";
      dangerousConfirmHeaders.push(dangerousConfirmHeader);
      if (dangerousConfirmHeader !== "profile-export") {
        return fulfillJson({ error: "missing dangerous confirm header" }, 400);
      }
      dangerousExportRequestCount += 1;
      return fulfillJson(createDangerousExportBundle(), 200, {
        "Content-Disposition": 'attachment; filename="server-dangerous-name.json"',
      });
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  await page.addInitScript((isoTimestamp) => {
    localStorage.setItem("prism.locale", "en");

    const fixedTime = new Date(isoTimestamp).valueOf();
    const RealDate = Date;

    class MockDate extends RealDate {
      constructor(value?: string | number | Date) {
        if (value === undefined) {
          super(fixedTime);
          return;
        }

        super(value);
      }

      static now() {
        return fixedTime;
      }
    }

    MockDate.parse = RealDate.parse;
    MockDate.UTC = RealDate.UTC;
    Object.setPrototypeOf(MockDate, RealDate);

    const windowWithDownloadCapture = window as Window & { __downloadCapture?: DownloadCapture };
    windowWithDownloadCapture.__downloadCapture = null;
    window.Date = MockDate as DateConstructor;

    const nativeAnchorClick = HTMLAnchorElement.prototype.click;
    HTMLAnchorElement.prototype.click = function clickWithDownloadCapture(this: HTMLAnchorElement) {
      const href = this.getAttribute("href") ?? this.href ?? "";
      if (this.hasAttribute("download") || href.startsWith("blob:")) {
        windowWithDownloadCapture.__downloadCapture = { download: this.download, href };
      }
      return nativeAnchorClick.call(this);
    };
  }, fixedTimestamp);

  return {
    getDangerousConfirmHeaders: () => dangerousConfirmHeaders,
    getDangerousExportRequestCount: () => dangerousExportRequestCount,
    getSafeExportRequestCount: () => safeExportRequestCount,
  };
}

test("context-capability-authoring: config export safe export uses the redacted route and synthesizes the filename locally", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);
  const expectedBundle = createSafeExportBundle();

  const backupSection = await gotoBackupSection(page);

  const downloadPromise = page.waitForEvent("download");
  await backupSection.getByTestId("profile-export-safe").click();
  const download = await downloadPromise;
  const suggestedFilename = download.suggestedFilename();
  const downloadedBundle = await readDownloadedBundle(download);

  await expect(page.getByText("Configuration exported successfully")).toBeVisible();
  expect(routes.getSafeExportRequestCount()).toBe(1);
  expect(routes.getDangerousExportRequestCount()).toBe(0);
  expect(downloadedBundle).toEqual(expectedBundle);

  const capture = await page.evaluate(
    () => (window as Window & { __downloadCapture?: DownloadCapture }).__downloadCapture ?? null,
  );

  expect(suggestedFilename).toBe(`prism-profile-config-v3-${fixedDate}.json`);
  expect(suggestedFilename).not.toBe("server-safe-name.json");
  expect(capture).not.toBeNull();
  expect(capture?.download).toBe(suggestedFilename);
  expect(capture?.href.startsWith("blob:")).toBe(true);
});

test("context-capability-authoring: config export dangerous export stays disabled until acknowledged and uses the dangerous route", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);
  const expectedBundle = createDangerousExportBundle();

  const backupSection = await gotoBackupSection(page);
  const dangerousButton = backupSection.getByTestId("profile-export-dangerous");
  await expect(backupSection.getByText("This path returns the full secret-bearing profile bundle, including encrypted secret payload entries and reusable endpoint secret refs. Use it only for disaster recovery.")).toBeVisible();
  await expect(dangerousButton).toBeDisabled();

  await backupSection.getByTestId("profile-export-dangerous-acknowledgement").click();
  await expect(dangerousButton).toBeEnabled();

  const downloadPromise = page.waitForEvent("download");
  await dangerousButton.click();
  const download = await downloadPromise;
  const suggestedFilename = download.suggestedFilename();
  const downloadedBundle = await readDownloadedBundle(download);

  await expect(page.getByText("Configuration exported successfully")).toBeVisible();
  expect(routes.getSafeExportRequestCount()).toBe(0);
  expect(routes.getDangerousExportRequestCount()).toBe(1);
  expect(routes.getDangerousConfirmHeaders()).toEqual(["profile-export"]);
  expect(downloadedBundle).toEqual(expectedBundle);

  const capture = await page.evaluate(
    () => (window as Window & { __downloadCapture?: DownloadCapture }).__downloadCapture ?? null,
  );

  expect(suggestedFilename).toBe(`prism-profile-config-with-secrets-v3-${fixedDate}.json`);
  expect(suggestedFilename).not.toBe("server-dangerous-name.json");
  expect(capture).not.toBeNull();
  expect(capture?.download).toBe(suggestedFilename);
  expect(capture?.href.startsWith("blob:")).toBe(true);
});
