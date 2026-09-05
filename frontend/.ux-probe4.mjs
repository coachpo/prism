import { chromium } from "@playwright/test"
const S = "/tmp/claude-1001/-home-qing-projects-prism--claude-worktrees-ultracode-model-page-ui-ux-677985/c1f31cf5-c3bf-45f9-8b3b-30245768161d/scratchpad"
const base = "http://127.0.0.1:5201"
const browser = await chromium.launch()
for (const [name, url] of [["trend", "/observe?tab=trend"], ["tt", "/observe?tab=terminal_targets"], ["errors", "/observe?tab=errors"]]) {
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } })
  await page.goto(base + url, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(3500)
  const info = await page.evaluate(() => {
    const r = document.documentElement
    const ok = r.scrollWidth <= r.clientWidth + 1
    const wide = []
    document.querySelectorAll("*").forEach((el) => {
      const rect = el.getBoundingClientRect()
      if (rect.right > r.clientWidth + 1 && rect.width > 0) {
        wide.push({ tag: el.tagName, cls: (el.className && el.className.baseVal !== undefined ? el.className.baseVal : String(el.className)).slice(0, 70), right: Math.round(rect.right), text: (el.textContent || "").trim().slice(0, 30) })
      }
    })
    return { ok, scrollWidth: r.scrollWidth, clientWidth: r.clientWidth, wide: wide.slice(0, 8) }
  })
  console.log(name, JSON.stringify(info, null, 1))
  await page.screenshot({ path: `${S}/narrow-${name}.png`, fullPage: true })
  await page.close()
}
await browser.close()
