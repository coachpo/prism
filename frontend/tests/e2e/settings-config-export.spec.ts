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

function createModelListItem() {
  return {
    id: 1,
    vendor_id: null,
    vendor: null,
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
    created_at: fixedTimestamp,
    updated_at: fixedTimestamp,
  };
}

async function mockSettingsRoutes(page: Page) {
  const profile = createProfile();
  let exportRequestCount = 0;

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
      exportRequestCount += 1;
      return fulfillJson(
        { version: 1, bundle_kind: "profile_config", exported_at: `${fixedDate}T12:00:00Z`, vendor_refs: [], endpoints: [], pricing_templates: [], loadbalance_strategies: [], models: [], profile_settings: { timezone_preference: null, report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], header_blocklist_rules: [] }, secret_payload: { cipher: "fernet-v1", key_id: "bundle-key-id", values: {} } },
        200,
        { "Content-Disposition": 'attachment; filename="gateway-config-1999-01-01.json"' },
      );
    }

    throw new Error(`Unhandled API request: ${request.method()} ${pathname}`);
  });

  await page.addInitScript((isoTimestamp) => {
    localStorage.setItem("prism.locale", "en");

    const fixedTime = new Date(isoTimestamp).valueOf();
    const RealDate = Date;

    class MockDate extends RealDate {
      constructor(...args: ConstructorParameters<typeof Date>) {
        super(...(args.length === 0 ? [fixedTime] : args));
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
    getExportRequestCount: () => exportRequestCount,
  };
}

test("settings export synthesizes the versioned filename locally", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);

  await page.goto("/settings#backup");
  const backupSection = page.locator("section#backup");
  await expect(backupSection).toBeVisible();

  await backupSection.getByRole("button", { name: "Export Configuration" }).click();

  await expect(page.getByText("Configuration exported successfully")).toBeVisible();
  expect(routes.getExportRequestCount()).toBe(1);

  const capture = await page.evaluate(
    () => (window as Window & { __downloadCapture?: DownloadCapture }).__downloadCapture ?? null,
  );

  expect(capture).not.toBeNull();
  expect(capture?.download).toBe(`prism-profile-config-v1-${fixedDate}.json`);
  expect(capture?.download).not.toBe("gateway-config-1999-01-01.json");
  expect(capture?.href.startsWith("blob:")).toBe(true);
});
