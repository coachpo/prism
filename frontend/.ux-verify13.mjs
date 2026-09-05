import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5233";
const browser = await chromium.launch();
// Break the lazy chunk for the models page so the route component throws.
{
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.route("**/ModelsFeaturePage*", (r) => r.abort());
  await page.goto(BASE + "/route/models", { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForTimeout(4000);
  console.log("chunk-abort:", JSON.stringify(await page.evaluate(() => ({
    sidebar: document.querySelectorAll("[data-slot=sidebar]").length,
    nav: document.querySelectorAll("nav").length,
    links: document.querySelectorAll("a[href]").length,
    routeError: document.querySelector("[data-testid=route-error]")?.innerText?.replace(/\s+/g, " ").slice(0, 220) ?? null,
    body: document.body.innerText.replace(/\s+/g, " ").slice(0, 160),
  }))));
  await page.close();
}
await browser.close();
