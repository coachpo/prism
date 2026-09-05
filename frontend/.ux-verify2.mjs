import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5200";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
for (const path of ["/route/models?page=0", "/system/settings?section=bogus", "/route/models/18?metrics_scope=bogus"]) {
  await page.goto(BASE + path, { waitUntil: "networkidle" }).catch(() => {});
  await page.waitForTimeout(800);
  console.log(JSON.stringify({ requested: path, actual: page.url(), search: await page.evaluate(() => window.location.search) }));
}
await browser.close();
