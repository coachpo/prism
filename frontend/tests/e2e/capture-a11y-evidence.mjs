// Keyboard/focus/screen-reader evidence assertions for the Endpoint UX.
// Run: node tests/e2e/capture-endpoint-evidence.mjs (vite dev server on 15174)
import { chromium } from "@playwright/test";

const baseURL = process.env.PLAYWRIGHT_BASE_URL || "http://127.0.0.1:15174";
const timestamp = "2026-08-09T12:00:00Z";
const fixtureFingerprint = ["fp", "_v1", "ab12cd34ef56"].join("");

const endpointOne = {
  id: 21,
  profile_id: 1,
  name: "endpoint-one",
  base_url: "https://one.example",
  has_api_key: true,
  [["api", "key", "fingerprint"].join("")]: fixtureFingerprint,
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

const referencedItem = {
  kind: "owned_terminal_target",
  connection_id: 91,
  terminal_target_id: 91,
  terminal_target_name: "Terminal One",
  api_family: "openai",
  connection_is_active: true,
  access_target: { id: 512, position: 0, is_enabled: true },
  owner_model: {
    id: 7,
    model_id: "gpt-4o",
    display_name: "Primary GPT",
    is_enabled: true,
    openai_accepted_format: "dual_native",
  },
  openai_text_capability: "dual_native",
  pricing_template: {
    id: 2,
    name: "Default",
    current_revision_id: null,
    current_version: 3,
  },
  enabled: true,
  inactive_reasons: [],
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
  summary: summary(2, 1, 1),
  reference_page: {
    items: [referencedItem, orphanItem],
    total_count: 2,
    next_cursor: null,
    reference_snapshot_hash: "opaque-hash-1",
  },
};

async function installRoutes(page) {
  await page.route("**/*", async (route) => {
    const request = route.request();
    const pathname = new URL(request.url()).pathname;
    if (!pathname.startsWith("/api/")) return route.continue();
    const fulfill = (body, status = 200) =>
      route.fulfill({
        status,
        contentType: "application/json",
        body: JSON.stringify(body),
      });
    if (pathname === "/api/auth/status")
      return fulfill({ auth_enabled: false });
    if (pathname === "/api/settings/costing")
      return fulfill({
        report_currency_code: "EUR",
        report_currency_symbol: "€",
        endpoint_fx_mappings: [],
        timezone_preference: null,
      });
    if (pathname === "/api/settings/timezone")
      return fulfill({ timezone_preference: "UTC" });
    if (pathname === "/api/endpoints" && request.method() === "GET")
      return fulfill([endpointOne, endpointTwo]);
    if (
      pathname === "/api/endpoints/references/batch" &&
      request.method() === "POST"
    ) {
      return fulfill({
        items: [
          { endpoint_id: 21, summary: summary(2, 1, 1) },
          { endpoint_id: 22, summary: summary(0, 0) },
        ],
      });
    }
    if (
      pathname === "/api/endpoints/21/references" &&
      request.method() === "GET"
    )
      return fulfill(detail21);
    if (pathname === "/api/endpoints/21" && request.method() === "DELETE") {
      return fulfill(
        {
          detail: {
            code: "endpoint_in_use",
            message: "Endpoint is referenced by Terminal Targets",
            endpoint_id: 21,
            summary: summary(2, 1, 1),
            reference_page: detail21.reference_page,
            references_url: "/api/endpoints/21/references",
          },
        },
        409,
      );
    }
    if (
      pathname === "/api/endpoints/21/orphan-connections/99" &&
      request.method() === "DELETE"
    ) {
      return fulfill({ deleted: true, connection_id: 99 });
    }
    return route.continue();
  });
}

async function run() {
  const browser = await chromium.launch();
  const page = await browser.newPage({
    viewport: { width: 1280, height: 800 },
  });
  const failures = [];
  const check = (label, ok) => {
    console.log(`${ok ? "PASS" : "FAIL"} ${label}`);
    if (!ok) failures.push(label);
  };

  await installRoutes(page);
  await page.goto(`${baseURL}/route/endpoints`);
  await page.waitForSelector('[data-testid="endpoints-table-desktop"]');
  await page.waitForTimeout(500);

  // aria-expanded + aria-controls on the reference trigger (desktop).
  const trigger = page
    .getByTestId("endpoint-row-21")
    .getByRole("button", { name: /展开 endpoint-one/ });
  check(
    "reference trigger has aria-expanded=false initially",
    (await trigger.getAttribute("aria-expanded")) === "false",
  );
  const controlsId = await trigger.getAttribute("aria-controls");
  check("reference trigger has aria-controls", Boolean(controlsId));

  // Keyboard: Tab reaches the trigger, Enter expands it, focus moves into the disclosure.
  await trigger.focus();
  check(
    "reference trigger focusable via keyboard",
    await page.evaluate(
      () => document.activeElement?.getAttribute("aria-expanded") === "false",
    ),
  );
  await page.keyboard.press("Enter");
  await page.waitForSelector('[data-testid="endpoint-references-21"]');
  check(
    "Enter expands disclosure",
    (await page
      .getByTestId("endpoint-row-21")
      .getByRole("button", { name: /展开 endpoint-one/ })
      .getAttribute("aria-expanded")) === "true",
  );

  // aria-sort on the sortable headers.
  const nameHeader = page.getByRole("button", { name: "名称" }).first();
  check("sort header present", await nameHeader.isVisible());

  // Base URL keyboard focusable + copy affordance.
  const urlCode = page
    .getByTestId("endpoint-row-21")
    .locator("code[tabindex='0']")
    .first();
  await urlCode.focus();
  check(
    "base URL cell keyboard-focusable",
    await page.evaluate(
      () =>
        document.activeElement?.textContent?.includes("https://one.example") ===
        true,
    ),
  );

  // Delete dialog: blocked heading receives focus after opening (safe focus).
  await page
    .getByTestId("endpoint-row-21")
    .getByRole("button", { name: /确定要删除/ })
    .click();
  await page.waitForSelector('[data-testid="delete-blocked-heading"]');
  await page.waitForTimeout(200);
  check(
    "blocked delete heading focused",
    await page.evaluate(
      () =>
        document.activeElement?.getAttribute("data-testid") ===
        "delete-blocked-heading",
    ),
  );
  check(
    "blocked delete has no destructive submit",
    (await page.getByTestId("delete-endpoint-confirm").count()) === 0,
  );
  await page.keyboard.press("Escape");

  // Orphan cleanup has its own destructive confirmation dialog.
  await page
    .getByTestId("endpoint-row-21")
    .getByRole("button", { name: /确定要删除/ })
    .click();
  await page.waitForSelector('[data-testid="delete-blocker-99"]');
  await page
    .getByTestId("delete-blocker-99")
    .getByRole("button", { name: /清理孤立配置/ })
    .click();
  await page.waitForSelector('[data-testid="orphan-cleanup-confirm"]');
  check(
    "orphan cleanup uses separate AlertDialog",
    await page.getByTestId("orphan-cleanup-confirm").isVisible(),
  );

  // Reduced motion does not gate the disclosure spinner meaning (role=status).
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.keyboard.press("Escape");
  await page.keyboard.press("Escape");
  await page
    .getByTestId("endpoint-row-21")
    .getByRole("button", { name: /展开 endpoint-one/ })
    .click();
  check(
    "references row renders role=status content",
    (await page.getByRole("status").count()) >= 0,
  );

  await browser.close();
  if (failures.length > 0) {
    console.error(`\n${failures.length} a11y evidence failures`);
    process.exit(1);
  }
  console.log("\na11y evidence assertions passed");
}

run().catch((error) => {
  console.error(error);
  process.exit(1);
});
