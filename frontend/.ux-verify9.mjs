import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5233";
const browser = await chromium.launch();

// permanent 503: gate after the budget, then no background polling.
{
  let hits = 0;
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.route("**/api/auth/status", (r) => { hits += 1; return r.fulfill({ status: 503, contentType: "application/json", body: "{}" }); });
  await page.goto(BASE + "/route/models", { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForTimeout(9000);
  const gate = await page.evaluate(() => document.body.innerText.replace(/\s+/g, " ").slice(0, 200));
  const hitsAtGate = hits;
  await page.waitForTimeout(9000);
  console.log("perm503 gate:", gate);
  console.log("perm503 hits at gate:", hitsAtGate, "-> 9s later:", hits);
  // manual retry re-arms the budget
  await page.getByRole("button", { name: "重试认证状态" }).click().catch((e) => console.log("click err", e.message.slice(0,80)));
  await page.waitForTimeout(9000);
  console.log("perm503 hits after manual retry:", hits);
  await page.close();
}

// transient: recovers on the 2nd attempt, no gate.
{
  let hits = 0;
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.route("**/api/auth/status", (r) => { hits += 1; return hits <= 2 ? r.fulfill({ status: 503, contentType: "application/json", body: "{}" }) : r.continue(); });
  await page.goto(BASE + "/route/models", { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForTimeout(7000);
  console.log("transient:", JSON.stringify(await page.evaluate(() => ({
    sidebar: document.querySelectorAll("[data-slot=sidebar]").length,
    h1: document.querySelector("h1")?.textContent?.trim() ?? null,
  }))), "hits=" + hits);
  await page.close();
}

// 403 stays fail-closed immediately (no auto retry).
{
  let hits = 0;
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  await page.route("**/api/auth/status", (r) => { hits += 1; return r.fulfill({ status: 403, contentType: "application/json", body: "{}" }); });
  await page.goto(BASE + "/route/models", { waitUntil: "domcontentloaded" }).catch(() => {});
  await page.waitForTimeout(3000);
  console.log("403:", (await page.evaluate(() => document.body.innerText.replace(/\s+/g, " ").slice(0, 160))), "hits=" + hits);
  await page.close();
}
await browser.close();
