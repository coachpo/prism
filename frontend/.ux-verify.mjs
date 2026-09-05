import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5233";
const cases = [
  "/system/settings?section=bogus",
  "/system/settings?scope=bogus",
  "/system/settings?costing_action=bogus",
  "/route/models?view=bogus",
  "/route/models?scope=bogus",
  "/route/models?api_family=bogus",
  "/route/models?flag=bogus",
  "/route/models?sort_by=bogus",
  "/route/models?sort_order=bogus",
  "/route/models?status=bogus",
  "/route/models?page=0",
  "/route/models?page=abc",
  "/route/models?page_size=9999",
  "/route/models/18?metrics_scope=bogus",
  "/route/models/18?endpoint_id=abc",
  "/route/models/18?focus_connection_id=abc",
  "/route/models/18?action=bogus",
  "/observe?tab=bogus",
  "/observe/requests?status=bogus",
  "/route/models/export?bogus=1",
  "/this/route/does/not/exist",
  "/auth/login",
];
const browser = await chromium.launch();
for (const path of cases) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e).slice(0, 100)));
  await page.goto(BASE + path, { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForFunction(() => !!document.querySelector("[data-slot=sidebar]") || !!document.querySelector("[data-slot=card]"), null, { timeout: 20000 }).catch(() => {});
  await page.waitForTimeout(1500);
  const info = await page.evaluate(() => ({
    h1: document.querySelector("h1")?.textContent?.trim() ?? null,
    nav: document.querySelectorAll("nav").length,
    notice: document.querySelector("[data-testid=search-fallback-notice]")?.innerText?.replace(/\s+/g, " ").trim() ?? null,
    routeError: !!document.querySelector("[data-testid=route-error]"),
    notFound: !!document.querySelector("[data-testid=route-not-found]"),
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
  }));
  console.log(JSON.stringify({ path, errors, ...info }));
  await page.close();
}
await browser.close();
