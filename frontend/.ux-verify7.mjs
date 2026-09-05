import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5222";
const cases = ["/route/models?page=abc", "/route/models?page=0", "/system/settings?costing_action=bogus", "/route/models/18?metrics_scope=bogus", "/route/models/18?action=bogus", "/route/models?view=bogus"];
const browser = await chromium.launch();
const tally = {};
for (let i = 0; i < 4; i++) {
  for (const path of cases) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    await page.goto(BASE + path, { waitUntil: "domcontentloaded" }).catch(() => {});
    await page.waitForFunction(() => !!document.querySelector("[data-slot=sidebar]"), null, { timeout: 15000 }).catch(() => {});
    await page.waitForTimeout(1200);
    const ok = await page.evaluate(() => !!document.querySelector("[data-testid=search-fallback-notice]"));
    tally[path] = (tally[path] ?? 0) + (ok ? 1 : 0);
    await page.close();
  }
}
console.log(JSON.stringify(tally, null, 1));
await browser.close();
