import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5200";
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
page.on("console", (m) => { if (m.text().startsWith("VS:")) console.log("  " + m.text()); });
for (const path of ["/route/models?page=0", "/route/models/18?metrics_scope=bogus"]) {
  console.log("== " + path);
  await page.goto(BASE + path, { waitUntil: "networkidle" }).catch(() => {});
  await page.waitForTimeout(1200);
  console.log("  final: " + page.url());
}
await browser.close();
