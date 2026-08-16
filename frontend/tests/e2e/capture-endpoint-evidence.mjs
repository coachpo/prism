// Screenshot evidence capture for the Endpoint management UX upgrade.
// Run: node tests/e2e/capture-endpoint-evidence.mjs (with vite dev server on 15174)
// Produces artifacts under /Users/qingli/Documents/proj/prism/artifacts/evidence/
import { chromium } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";

const evidenceDir = "/Users/qingli/Documents/proj/prism/artifacts/evidence";
mkdirSync(evidenceDir, { recursive: true });

const baseURL = process.env.PLAYWRIGHT_BASE_URL || "http://127.0.0.1:15174";
const timestamp = "2026-08-09T12:00:00Z";

const endpointOne = {
  id: 21,
  profile_id: 1,
  name: "endpoint-one",
  base_url: "https://one.example",
  has_api_key: true,
  api_key_fingerprint: "fp_v1_ab12cd34ef56",
  api_key_updated_at: timestamp,
  config_revision: 1,
  created_at: timestamp,
  updated_at: timestamp,
};
const endpointTwo = {
  id: 22,
  profile_id: 1,
  name: "endpoint-two",
  base_url: "https://two.example",
  has_api_key: false,
  api_key_fingerprint: null,
  api_key_updated_at: null,
  config_revision: 1,
  created_at: timestamp,
  updated_at: timestamp,
};
const endpointThree = {
  id: 23,
  profile_id: 1,
  name: "endpoint-three",
  base_url: "https://three.example",
  has_api_key: true,
  api_key_fingerprint: "fp_v1_998877665544",
  api_key_updated_at: timestamp,
  config_revision: 1,
  created_at: timestamp,
  updated_at: timestamp,
};
const endpointFour = {
  id: 24,
  profile_id: 1,
  name: "endpoint-four",
  base_url: "https://four.example",
  has_api_key: false,
  api_key_fingerprint: null,
  api_key_updated_at: null,
  config_revision: 1,
  created_at: timestamp,
  updated_at: timestamp,
};

const referencedItem = {
  kind: "owned_terminal_target",
  connection_id: 91,
  terminal_target_id: 91,
  terminal_target_name: "Terminal One",
  api_family: "openai",
  connection_is_active: true,
  access_target: { id: 512, position: 0, is_enabled: true },
  owner_model: { id: 7, model_id: "gpt-4o", display_name: "Primary GPT", is_enabled: true, openai_accepted_format: "dual_native" },
  openai_text_capability: "dual_native",
  pricing_template: { id: 2, name: "Default", current_revision_id: null, current_version: 3 },
  enabled: true,
  inactive_reasons: [],
};
const disabledItem = {
  ...referencedItem,
  connection_id: 92,
  terminal_target_id: 92,
  terminal_target_name: "Terminal Two",
  access_target: { id: 513, position: 1, is_enabled: false },
  enabled: false,
  inactive_reasons: ["access_target_disabled"],
};
const orphanItem = {
  kind: "orphan_connection",
  connection_id: 99,
  terminal_target_id: 99,
  terminal_target_name: null,
  api_family: "openai",
  connection_is_active: false,
  access_target: null,
  owner_model: null,
  openai_text_capability: "dual_native",
  pricing_template: null,
  enabled: false,
  inactive_reasons: ["orphaned"],
};

const summary = (direct, enabled, orphan = 0) => ({
  direct_reference_count: direct,
  referencing_model_count: direct > 0 ? 1 : 0,
  enabled_reference_count: enabled,
  orphan_reference_count: orphan,
});

const detail21 = {
  endpoint_id: 21,
  summary: summary(3, 1, 1),
  reference_page: {
    items: [referencedItem, disabledItem, orphanItem],
    total_count: 3,
    next_cursor: null,
    reference_snapshot_hash: "opaque-hash-1",
  },
};

async function installRoutes(page, mode) {
  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) return route.continue();
    const fulfill = (body, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
    if (pathname === "/api/auth/status") return fulfill({ auth_enabled: false });
    if (pathname === "/api/settings/costing") return fulfill({ report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], timezone_preference: null });
    if (pathname === "/api/settings/timezone") return fulfill({ timezone_preference: "UTC" });
    if (pathname === "/api/endpoints" && request.method() === "GET") return fulfill([endpointOne, endpointTwo, endpointThree, endpointFour]);
    if (pathname === "/api/endpoints/references/batch" && request.method() === "POST") {
      if (mode === "references-error") {
        return fulfill({ detail: "upstream unavailable" }, 503);
      }
      return fulfill({
        items: [
          { endpoint_id: 21, summary: summary(3, 1, 1) },
          { endpoint_id: 22, summary: summary(0, 0) },
          { endpoint_id: 23, summary: summary(1, 0) },
          { endpoint_id: 24, summary: summary(0, 0) },
        ],
      });
    }
    if (pathname === "/api/endpoints/21/references" && request.method() === "GET") {
      if (mode === "references-error") return fulfill({ detail: "upstream unavailable" }, 503);
      return fulfill(detail21);
    }
    if (pathname === "/api/endpoints/21" && request.method() === "PUT") {
      const body = request.postDataJSON ? request.postDataJSON() : {};
      return fulfill({ ...endpointOne, name: body.name ?? endpointOne.name, base_url: body.base_url ?? endpointOne.base_url });
    }
    if (pathname === "/api/endpoints/21" && request.method() === "DELETE") {
      return fulfill({ detail: { code: "endpoint_in_use", message: "Endpoint is referenced by Terminal Targets", endpoint_id: 21, summary: summary(3, 1, 1), reference_page: detail21.reference_page, references_url: "/api/endpoints/21/references" } }, 409);
    }
    if (pathname === "/api/endpoints/21/orphan-connections/99" && request.method() === "DELETE") {
      return fulfill({ deleted: true, connection_id: 99 });
    }
    if (pathname === "/api/endpoints/21/verify" && request.method() === "POST") {
      return fulfill({ endpoint_id: 21, api_family: "openai", config_revision: 1, api_key_fingerprint: "fp_v1_ab12cd34ef56", is_current: true, outcome: "verified", probe_path: "/v1/models", upstream_status: 200, duration_ms: 120, error_summary: null });
    }
    return route.continue();
  });
}

async function capture() {
  const browser = await chromium.launch();
  const viewports = [
    { name: "1680", width: 1680, height: 1050 },
    { name: "1200", width: 1200, height: 800 },
    { name: "390", width: 390, height: 844 },
  ];

  for (const viewport of viewports) {
    const page = await browser.newPage({ viewport: { width: viewport.width, height: viewport.height } });
    await installRoutes(page, "ready");

    // Ready table
    await page.goto(`${baseURL}/route/endpoints`);
    if (viewport.width < 640) {
      await page.waitForSelector('[data-testid="endpoints-mobile-cards"]');
    } else {
      await page.waitForSelector('[data-testid="endpoints-table-desktop"]');
    }
    await page.waitForTimeout(600);
    await page.screenshot({ path: `${evidenceDir}/ux-ep-list-${viewport.name}.png`, fullPage: false });

    // Expanded references (owned + disabled + orphan)
    if (viewport.width < 640) {
      await page.getByTestId("endpoint-mobile-card-21").getByRole("button", { name: /展开 endpoint-one/ }).click();
    } else {
      await page.getByTestId("endpoint-row-21").getByRole("button", { name: /展开 endpoint-one/ }).click();
    }
    await page.waitForTimeout(600);
    await page.screenshot({ path: `${evidenceDir}/ux-ep-references-${viewport.name}.png`, fullPage: false });

    // Blocked delete dialog
    if (viewport.width < 640) {
      await page.getByTestId("endpoint-mobile-card-21").getByRole("button", { name: /确定要删除/ }).click();
    } else {
      await page.getByTestId("endpoint-row-21").getByRole("button", { name: /确定要删除/ }).click();
    }
    await page.waitForSelector('[data-testid="delete-blocked-heading"]');
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${evidenceDir}/ux-ep-delete-blocked-${viewport.name}.png`, fullPage: false });
    await page.keyboard.press("Escape");

    // Verify success dialog
    if (viewport.width < 640) {
      await page.getByTestId("endpoint-mobile-card-21").getByRole("button", { name: /编辑端点/ }).click();
    } else {
      await page.getByTestId("endpoint-row-21").getByRole("button", { name: /编辑端点/ }).click();
    }
    await page.getByRole("button", { name: /保存并验证/ }).click();
    await page.getByTestId("verify-section").getByRole("combobox").click();
    await page.getByRole("option", { name: "OpenAI" }).click();
    await page.getByTestId("endpoint-save-only").click();
    await page.waitForSelector('[data-testid="verify-result"]');
    await page.waitForTimeout(400);
    await page.screenshot({ path: `${evidenceDir}/ux-ep-verify-${viewport.name}.png`, fullPage: false });
    await page.keyboard.press("Escape");

    // Fail-closed references 503
    const errorPage = await browser.newPage({ viewport: { width: viewport.width, height: viewport.height } });
    await installRoutes(errorPage, "references-error");
    await errorPage.goto(`${baseURL}/route/endpoints`);
    if (viewport.width < 640) {
      await errorPage.waitForSelector('[data-testid="endpoints-mobile-cards"]');
    } else {
      await errorPage.waitForSelector('[data-testid="endpoints-table-desktop"]');
    }
    await errorPage.waitForTimeout(600);
    await errorPage.screenshot({ path: `${evidenceDir}/ux-ep-references-error-${viewport.name}.png`, fullPage: false });
    await errorPage.close();

    await page.close();
  }

  await browser.close();
  console.log("evidence captured under", evidenceDir);
}

capture().catch((error) => {
  console.error(error);
  process.exit(1);
});
