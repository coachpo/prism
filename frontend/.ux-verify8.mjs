import { chromium } from "@playwright/test";
const BASE = "http://127.0.0.1:5233";
const browser = await chromium.launch();

async function open(path, { viewport = { width: 1440, height: 900 }, route } = {}) {
  const page = await browser.newPage({ viewport });
  if (route) await route(page);
  await page.goto(BASE + path, { waitUntil: "domcontentloaded" }).catch(() => {});
  return page;
}

// 1) costing endpoint hung: the console must still render its shell + page.
{
  const page = await open("/route/models", {
    route: async (p) => p.route("**/api/settings/costing", async () => { await new Promise(() => {}); }),
  });
  await page.waitForTimeout(6000);
  console.log("costing-hung:", JSON.stringify(await page.evaluate(() => ({
    sidebar: document.querySelectorAll("[data-slot=sidebar]").length,
    h1: document.querySelector("h1")?.textContent?.trim() ?? null,
    tables: document.querySelectorAll("table").length,
    body: document.body.innerText.replace(/\s+/g, " ").slice(0, 60),
  }))));
  await page.close();
}

// 2) costing endpoint 503: degraded notice, page still renders.
{
  const page = await open("/route/models", {
    route: async (p) => p.route("**/api/settings/costing", (r) => r.fulfill({ status: 503, contentType: "application/json", body: "{}" })),
  });
  await page.waitForTimeout(4000);
  console.log("costing-503:", JSON.stringify(await page.evaluate(() => ({
    sidebar: document.querySelectorAll("[data-slot=sidebar]").length,
    h1: document.querySelector("h1")?.textContent?.trim() ?? null,
    degraded: document.querySelector("[data-testid=reporting-currency-degraded]")?.innerText?.replace(/\s+/g, " ") ?? null,
  }))));
  await page.close();
}

// 3) /api/auth/status 503 forever: the gate must appear only after auto retries.
{
  let hits = 0;
  const page = await open("/route/models", {
    route: async (p) => p.route("**/api/auth/status", (r) => { hits += 1; return r.fulfill({ status: 503, contentType: "application/json", body: "{}" }); }),
  });
  for (const t of [1500, 4000, 9000]) {
    await page.waitForTimeout(t === 1500 ? 1500 : t - 1500);
    console.log(`auth-503 @${t}ms hits=${hits}`, JSON.stringify(await page.evaluate(() => ({
      body: document.body.innerText.replace(/\s+/g, " ").slice(0, 130),
    }))));
  }
  await page.close();
}

// 4) /api/auth/status recovers on the 2nd attempt: no gate at all.
{
  let hits = 0;
  const page = await open("/route/models", {
    route: async (p) => p.route("**/api/auth/status", (r) => {
      hits += 1;
      if (hits === 1) return r.fulfill({ status: 503, contentType: "application/json", body: "{}" });
      return r.continue();
    }),
  });
  await page.waitForTimeout(6000);
  console.log(`auth-503-recovers hits=${hits}`, JSON.stringify(await page.evaluate(() => ({
    sidebar: document.querySelectorAll("[data-slot=sidebar]").length,
    h1: document.querySelector("h1")?.textContent?.trim() ?? null,
  }))));
  await page.close();
}

// 5) login page at 390px.
{
  const page = await open("/auth/login", { viewport: { width: 390, height: 844 } });
  await page.waitForTimeout(2500);
  console.log("login-390:", JSON.stringify(await page.evaluate(() => ({
    title: document.title,
    h1: document.querySelector("h1")?.textContent?.trim() ?? null,
    headings: document.querySelectorAll("h1,h2,h3,h4,h5,h6").length,
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    themeBtnRight: (() => { const b = [...document.querySelectorAll("button")].find((x) => x.getAttribute("aria-label")?.includes("主题") || x.title?.includes("主题")); return b ? Math.round(b.getBoundingClientRect().right) : null; })(),
  }))));
  await page.close();
}

// 6) 404 keeps the shell.
{
  const page = await open("/nope/nope");
  await page.waitForTimeout(2500);
  console.log("404:", JSON.stringify(await page.evaluate(() => ({
    sidebar: document.querySelectorAll("[data-slot=sidebar]").length,
    nav: document.querySelectorAll("nav").length,
    links: document.querySelectorAll("a[href]").length,
    text: document.querySelector("[data-testid=route-not-found]")?.innerText?.replace(/\s+/g, " ") ?? null,
  }))));
  await page.close();
}
await browser.close();
