import { chromium } from "@playwright/test"
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
await page.goto("http://192.168.1.222:8088/observe?tab=trend", { waitUntil: "domcontentloaded" })
await page.waitForTimeout(4000)
console.log(await page.evaluate(() => {
  const r = document.documentElement
  return { ok: r.scrollWidth <= r.clientWidth + 1, scrollWidth: r.scrollWidth, clientWidth: r.clientWidth }
}))
await browser.close()
