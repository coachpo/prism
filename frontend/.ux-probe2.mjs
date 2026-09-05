import { chromium } from "@playwright/test"
const base = "http://127.0.0.1:5201"
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
for (const url of ["/observe?tab=errors&group_by=ingress_model", "/observe?tab=trend&group_by=final_target_model", "/observe?tab=errors&group_by=ingress_model&scope=final_execution"]) {
  await page.goto(base + url, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(4000)
  console.log(url, "=>", page.url())
}
await browser.close()
