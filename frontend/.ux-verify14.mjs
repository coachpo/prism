import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5233";
const browser = await chromium.launch();
for (let i = 0; i < 3; i++) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const costing = [];
  page.on("response", (r) => { if (r.url().includes("/api/settings/costing")) costing.push(r.status()); });
  await page.goto(BASE + "/route/models", { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForFunction(() => !!document.querySelector("[data-slot=sidebar]"), null, { timeout: 25000 }).catch(() => {});
  await page.waitForTimeout(2000);
  console.log("run" + i, "degraded=" + await page.evaluate(() => !!document.querySelector("[data-testid=reporting-currency-degraded]")), "costingResponses=" + JSON.stringify(costing));
  await page.close();
}
await browser.close();
