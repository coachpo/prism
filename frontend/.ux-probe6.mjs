import { chromium } from "@playwright/test"
const browser = await chromium.launch()
for (const url of ["/route/endpoints", "/system/settings", "/observe/routing-health", "/observe/requests"]) {
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
  await page.goto("http://127.0.0.1:5201" + url, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(3000)
  console.log(url, await page.evaluate(() => {
    const r = document.documentElement
    return { scrollWidth: r.scrollWidth, clientWidth: r.clientWidth }
  }))
  await page.close()
}
await browser.close()
