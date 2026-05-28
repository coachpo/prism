import { expect, test, type Page } from "@playwright/test";

const fixedTimestamp = "2026-04-18T12:00:00Z";
const fixedDate = "2026-04-18";

type DownloadCapture = {
  download: string;
  href: string;
} | null;

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

function createSafeExportBundle() {
  return {
    version: 2 as const,
    bundle_kind: "profile_config" as const,
    exported_at: `${fixedDate}T12:00:00Z`,
    vendor_refs: [],
    endpoints: [
      {
        name: "Default endpoint",
        base_url: "https://safe.example.invalid",
        api_key_secret_ref: null,
        position: 0,
      },
    ],
    pricing_templates: [],
    connections: [],
    loadbalance_strategies: [],
    models: [],
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
    version: 2 as const,
    bundle_kind: "profile_config" as const,
    exported_at: `${fixedDate}T12:00:00Z`,
    vendor_refs: [],
    endpoints: [
      {
        name: "Default endpoint",
        base_url: "https://dangerous.example.invalid",
        api_key_secret_ref: "endpoint-secret-ref",
        position: 0,
      },
    ],
    pricing_templates: [],
    connections: [],
    loadbalance_strategies: [],
    models: [],
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

test("profile safe export uses the redacted route and synthesizes the filename locally", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);

  await page.goto("/settings#backup");
  const backupSection = page.locator("section#backup");
  await expect(backupSection).toBeVisible();

  await backupSection.getByTestId("profile-export-safe").click();

  await expect(page.getByText("Configuration exported successfully")).toBeVisible();
  expect(routes.getSafeExportRequestCount()).toBe(1);
  expect(routes.getDangerousExportRequestCount()).toBe(0);

  const capture = await page.evaluate(
    () => (window as Window & { __downloadCapture?: DownloadCapture }).__downloadCapture ?? null,
  );

  expect(capture).not.toBeNull();
  expect(capture?.download).toBe(`prism-profile-config-v2-${fixedDate}.json`);
  expect(capture?.download).not.toBe("server-safe-name.json");
  expect(capture?.href.startsWith("blob:")).toBe(true);
});

test("profile dangerous export stays disabled until acknowledged and uses the dangerous route", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);

  await page.goto("/settings#backup");
  const backupSection = page.locator("section#backup");
  const dangerousButton = backupSection.getByTestId("profile-export-dangerous");

  await expect(backupSection).toBeVisible();
  await expect(backupSection.getByText("This path returns the full secret-bearing profile bundle, including encrypted secret payload entries and reusable endpoint secret refs. Use it only for disaster recovery.")).toBeVisible();
  await expect(dangerousButton).toBeDisabled();

  await backupSection.getByTestId("profile-export-dangerous-acknowledgement").click();
  await expect(dangerousButton).toBeEnabled();

  await dangerousButton.click();

  await expect(page.getByText("Configuration exported successfully")).toBeVisible();
  expect(routes.getSafeExportRequestCount()).toBe(0);
  expect(routes.getDangerousExportRequestCount()).toBe(1);
  expect(routes.getDangerousConfirmHeaders()).toEqual(["profile-export"]);

  const capture = await page.evaluate(
    () => (window as Window & { __downloadCapture?: DownloadCapture }).__downloadCapture ?? null,
  );

  expect(capture).not.toBeNull();
  expect(capture?.download).toBe(`prism-profile-config-with-secrets-v2-${fixedDate}.json`);
  expect(capture?.download).not.toBe("server-dangerous-name.json");
  expect(capture?.href.startsWith("blob:")).toBe(true);
});
