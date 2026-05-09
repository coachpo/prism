import { expect, test, type Page } from "@playwright/test";

const fixedTimestamp = "2026-04-27T12:00:00Z";
const fixedDate = "2026-04-27";

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

function createVendor(id: number, key: string, name: string, description: string | null = null) {
  return {
    id,
    key,
    name,
    description,
    icon_key: key,
    is_readonly: false,
    audit_enabled: true,
    audit_capture_bodies: false,
    created_at: fixedTimestamp,
    updated_at: fixedTimestamp,
  };
}

function buildVendorImportBundle() {
  return {
    version: 1,
    bundle_kind: "vendor_catalog" as const,
    vendors: [
      {
        key: "openai",
        name: "OpenAI Updated",
        description: "Updated vendor",
        icon_key: "openai",
        audit_enabled: true,
        audit_capture_bodies: false,
      },
      {
        key: "anthropic",
        name: "Anthropic",
        description: "New vendor",
        icon_key: "anthropic",
        audit_enabled: true,
        audit_capture_bodies: false,
      },
    ],
  };
}

async function mockSettingsRoutes(page: Page) {
  const profile = createProfile();
  const previewTokenBindings = new Map<string, string>();
  let vendors = [createVendor(1, "openai", "OpenAI", "Primary vendor")];
  let exportRequestCount = 0;
  const importedPayloads: unknown[] = [];
  const previewPayloads: unknown[] = [];
  const appliedPreviewTokens: string[] = [];

  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;

    if (!pathname.startsWith("/api/")) {
      return route.continue();
    }

    const fulfillJson = (body: unknown, status = 200, headers?: Record<string, string>) =>
      route.fulfill({ status, contentType: "application/json", headers, body: JSON.stringify(body) });

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false });
    }
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } });
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], timezone_preference: null });
    }
    if (pathname === "/api/settings/auth") {
      return fulfillJson({ auth_enabled: false, username: null, has_password: false, email: null, pending_email: null, email_bound_at: null, email_verification_required: false });
    }
    if (pathname === "/api/settings/log-retention") {
      return fulfillJson({ request_logs_retention_days: 30, statistics_retention_days: 30, audit_logs_retention_days: 30, loadbalance_events_retention_days: 30 });
    }
    if (pathname === "/api/models") {
      return fulfillJson([]);
    }
    if (pathname === "/api/vendors" && request.method() === "GET") {
      return fulfillJson(vendors);
    }
    if (pathname === "/api/config/header-blocklist-rules") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/user-agent-client-rules") {
      return fulfillJson([]);
    }
    if (pathname === "/api/config/vendors/export" && request.method() === "GET") {
      exportRequestCount += 1;
      return fulfillJson(
        {
          version: 1,
          bundle_kind: "vendor_catalog",
          exported_at: `${fixedDate}T12:00:00Z`,
          vendors: [
            {
              key: "openai",
              name: "OpenAI",
              description: "Primary vendor",
              icon_key: "openai",
              audit_enabled: true,
              audit_capture_bodies: false,
            },
          ],
        },
        200,
        { "Content-Disposition": 'attachment; filename="ignored-server-name.json"' },
      );
    }
    if (pathname === "/api/config/vendors/import/preview" && request.method() === "POST") {
      const payload = request.postDataJSON();
      const previewToken = `vendor-preview-token-${previewPayloads.length + 1}`;
      previewPayloads.push(payload);
      previewTokenBindings.set(previewToken, JSON.stringify(payload));
      return fulfillJson({
        ready: true,
        version: 1,
        bundle_kind: "vendor_catalog",
        preview_token: previewToken,
        bundle_fingerprint: `vendor-fingerprint-${previewPayloads.length}`,
        mutation_scope: {
          target: "global_vendor_catalog",
          create_count: 1,
          update_count: 1,
          unchanged_count: 0,
        },
        untouched_scope: {
          profiles: true,
          profile_scoped_config: true,
          request_logs: true,
        },
        create_count: 1,
        update_count: 1,
        blocking_errors: [],
        warnings: ["Readonly vendors will be skipped."],
      });
    }
    if (pathname === "/api/config/vendors/import" && request.method() === "POST") {
      const previewToken = (await request.allHeaders())["x-prism-preview-token"] ?? "";
      const payload = request.postDataJSON();
      if (!previewToken) {
        return fulfillJson({ error: "missing preview token" }, 400);
      }
      if (previewTokenBindings.get(previewToken) !== JSON.stringify(payload)) {
        return fulfillJson({ error: "stale preview token" }, 409);
      }
      appliedPreviewTokens.push(previewToken);
      importedPayloads.push(payload);
      vendors = [
        createVendor(1, "openai", "OpenAI Updated", "Updated vendor"),
        createVendor(2, "anthropic", "Anthropic", "New vendor"),
      ];
      return fulfillJson({ created_count: 1, updated_count: 1 });
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
    getAppliedPreviewTokens: () => appliedPreviewTokens,
    getExportRequestCount: () => exportRequestCount,
    getImportedPayloads: () => importedPayloads,
    getPreviewPayloads: () => previewPayloads,
  };
}

test("global settings exposes vendor catalog export plus an explicit preview before apply", async ({ page }) => {
  const routes = await mockSettingsRoutes(page);
  const importBundle = buildVendorImportBundle();

  await page.goto("/settings");
  await page.getByRole("tab", { name: "Global" }).click();
  await expect(page.getByText("Vendor Catalog Transport")).toBeVisible();

  await page.getByRole("button", { name: "Export Vendor Catalog" }).click();
  await expect(page.getByText("Vendor catalog exported successfully")).toBeVisible();
  expect(routes.getExportRequestCount()).toBe(1);

  const capture = await page.evaluate(
    () => (window as Window & { __downloadCapture?: DownloadCapture }).__downloadCapture ?? null,
  );
  expect(capture).not.toBeNull();
  expect(capture?.download).toBe(`prism-vendor-catalog-v1-${fixedDate}.json`);

  await page.getByTestId("vendor-catalog-import-file").setInputFiles({
    name: "vendors.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(importBundle)),
  });

  const applyButton = page.getByTestId("vendor-catalog-apply");
  await expect(page.getByText("Loaded vendors.json: 2 vendor rows.")).toBeVisible();
  await expect(page.getByText("Run preview to bind a fresh token for the currently loaded vendor bundle before applying it.")).toBeVisible();
  await expect(applyButton).toBeDisabled();
  expect(routes.getPreviewPayloads()).toEqual([]);
  expect(routes.getImportedPayloads()).toEqual([]);

  await page.getByTestId("vendor-catalog-preview").click();

  await expect(page.getByText("Preview ready for apply")).toBeVisible();
  await expect(page.getByText("Apply is bound to the currently loaded bundle: vendors.json.")).toBeVisible();
  await expect(page.getByText("Mutation scope")).toBeVisible();
  await expect(page.getByText("Untouched scope")).toBeVisible();
  await expect(page.getByText("Readonly vendors will be skipped.")).toBeVisible();
  await expect(applyButton).toBeEnabled();

  await applyButton.click();

  await expect(page.getByText("Imported 1 vendors and updated 1 vendors")).toBeVisible();
  await expect(page.getByText("OpenAI Updated")).toBeVisible();
  await expect(page.getByRole("row", { name: /Anthropic anthropic New vendor Edit Delete/i })).toBeVisible();

  expect(routes.getPreviewPayloads()).toEqual([importBundle]);
  expect(routes.getImportedPayloads()).toEqual([importBundle]);
  expect(routes.getAppliedPreviewTokens()).toEqual(["vendor-preview-token-1"]);
});
