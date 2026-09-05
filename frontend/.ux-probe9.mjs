import { chromium } from "@playwright/test"
const browser = await chromium.launch()
for (const url of ["/observe?tab=trend", "/observe?tab=trend", "/observe?tab=terminal_targets"]) {
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
  await page.goto("http://127.0.0.1:5201" + url, { waitUntil: "networkidle" }).catch(() => {})
  await page.waitForTimeout(5000)
  const info = await page.evaluate(() => {
    const g = document.querySelector('[data-testid="window-kpi-grid"]')
    const main = document.querySelector('[data-testid="observe-page"]')
    return {
      doc: document.documentElement.scrollWidth,
      kpi: g ? { sw: g.scrollWidth, cw: g.clientWidth } : null,
      page: main ? { sw: main.scrollWidth, cw: main.clientWidth } : null,
    }
  })
  console.log(url, JSON.stringify(info))
  await page.close()
}
await browser.close()
