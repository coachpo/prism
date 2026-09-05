import { chromium } from "@playwright/test";

const BASE = "http://127.0.0.1:5200";

const cases = [
  "/system/settings?section=bogus",
  "/system/settings?scope=bogus",
  "/system/settings?costing_action=bogus",
  "/route/models?view=bogus",
  "/route/models?sort_by=bogus",
  "/route/models?status=bogus",
  "/route/models?page=0",
  "/route/models?page=abc",
  "/route/models?page_size=9999",
  "/route/models/18?metrics_scope=bogus",
  "/route/models/18?endpoint_id=abc",
  "/route/models/18?focus_connection_id=abc",
  "/this/route/does/not/exist",
  "/auth/login",
];

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
for (const path of cases) {
  await page.goto(BASE + path, { waitUntil: "networkidle" }).catch(() => {});
  await page.waitForTimeout(700);
  const info = await page.evaluate(() => ({
    title: document.title,
    h1: document.querySelector("h1")?.textContent?.trim() ?? null,
    headings: document.querySelectorAll("h1,h2,h3,h4,h5,h6").length,
    nav: document.querySelectorAll("nav").length,
    sidebar: document.querySelectorAll("[data-slot=sidebar]").length,
    links: document.querySelectorAll("a[href]").length,
    notice: document.querySelector("[data-testid=search-fallback-notice]")?.innerText?.replace(/\s+/g, " ").trim() ?? null,
    routeError: document.querySelector("[data-testid=route-error]") ? true : false,
    notFound: document.querySelector("[data-testid=route-not-found]")?.innerText?.replace(/\s+/g, " ").trim() ?? null,
    currencyDegraded: document.querySelector("[data-testid=reporting-currency-degraded]") ? true : false,
    bodyHead: document.body.innerText.replace(/\s+/g, " ").slice(0, 160),
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
  }));
  console.log(JSON.stringify({ path, ...info }, null, 1));
}
await browser.close();
