import { chromium } from "@playwright/test"
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
await page.goto("http://127.0.0.1:5201/observe?tab=trend", { waitUntil: "domcontentloaded" })
await page.waitForTimeout(3500)
console.log(JSON.stringify(await page.evaluate(() => {
  const out = []
  document.querySelectorAll("*").forEach((el) => {
    const cs = getComputedStyle(el)
    if (el.scrollWidth > el.clientWidth + 1 && cs.overflowX === "visible" && el.clientWidth > 0) {
      out.push({
        tag: el.tagName,
        cls: String(el.className.baseVal ?? el.className).slice(0, 90),
        sw: el.scrollWidth, cw: el.clientWidth,
        text: (el.textContent || "").trim().slice(0, 40),
      })
    }
  })
  return out.slice(0, 20)
}, null), null, 1))
await browser.close()
