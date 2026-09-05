import { chromium } from "@playwright/test"
const browser = await chromium.launch()
for (const base of ["http://192.168.1.222:8088", "http://127.0.0.1:5201"]) {
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
  await page.goto(base + "/observe?tab=trend", { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(3500)
  console.log(base, await page.evaluate(() => {
    const g = document.querySelector('[data-testid="window-kpi-grid"]')
    return {
      doc: document.documentElement.scrollWidth,
      kpi: g ? { cls: g.className, sw: g.scrollWidth, cw: g.clientWidth } : null,
    }
  }))
  await page.close()
}
await browser.close()
