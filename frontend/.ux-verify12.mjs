import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5233";
const browser = await chromium.launch();
for (let i = 0; i < 3; i++) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const logs = [];
  page.on("console", (m) => { const t = m.text(); if (t.startsWith("SF:")) logs.push(t.slice(0, 140)); });
  await page.goto(BASE + "/route/models?page=abc", { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForFunction(() => !!document.querySelector("[data-slot=sidebar]"), null, { timeout: 25000 }).catch(() => {});
  await page.waitForTimeout(1500);
  const ok = await page.evaluate(() => !!document.querySelector("[data-testid=search-fallback-notice]"));
  console.log("run" + i + " notice=" + ok);
  if (!ok) logs.forEach((l) => console.log("   " + l));
  await page.close();
}
await browser.close();
