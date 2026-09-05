import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5233";
const browser = await chromium.launch();
for (const path of ["/route/models?sort_by=bogus", "/route/models?page=abc", "/route/models?sort_by=bogus", "/route/models?page=abc"]) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.goto(BASE + path, { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForFunction(() => !!document.querySelector("[data-slot=sidebar]"), null, { timeout: 25000 }).catch(() => {});
  await page.waitForTimeout(1500);
  console.log(path, await page.evaluate(() => !!document.querySelector("[data-testid=search-fallback-notice]")));
  await page.close();
}
// login at 390
{
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  await page.goto(BASE + "/auth/login", { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForTimeout(2500);
  console.log("login390:", JSON.stringify(await page.evaluate(() => {
    const over = [...document.querySelectorAll("*")].filter((el) => el.getBoundingClientRect().right > document.documentElement.clientWidth + 0.5).map((el) => el.tagName + "." + (el.className?.toString?.().slice(0, 40) ?? ""));
    return {
      title: document.title,
      h1: document.querySelector("h1")?.textContent?.trim() ?? null,
      h1Style: (() => { const h = document.querySelector("h1"); if (!h) return null; const s = getComputedStyle(h); return `${s.fontSize}/${s.lineHeight}/${s.fontWeight}`; })(),
      headings: document.querySelectorAll("h1,h2,h3,h4,h5,h6").length,
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      overflowers: over.slice(0, 5),
    };
  })));
  await page.close();
}
await browser.close();
