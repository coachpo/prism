import { chromium } from "@playwright/test"
const S = "/tmp/claude-1001/-home-qing-projects-prism--claude-worktrees-ultracode-model-page-ui-ux-677985/c1f31cf5-c3bf-45f9-8b3b-30245768161d/scratchpad"
const base = "http://127.0.0.1:5201"
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
await page.goto(base + "/observe?tab=trend&scope=route_attempt", { waitUntil: "domcontentloaded" })
await page.waitForTimeout(3500)
// 展开首次配置清单
await page.getByRole("button", { name: /展开配置事实/ }).click().catch(() => {})
await page.waitForTimeout(600)
await page.screenshot({ path: S + "/observe-wide-controls.png", fullPage: true })
const overflow = await page.evaluate(() => {
  const chart = document.querySelector('[data-testid="observe-main-chart"]')
  if (!chart) return null
  const strip = chart.firstElementChild
  return { stripW: strip.scrollWidth, boxW: strip.clientWidth }
})
console.log("control strip overflow:", overflow)
await page.setViewportSize({ width: 390, height: 844 })
await page.waitForTimeout(800)
const narrow = await page.evaluate(() => {
  const r = document.documentElement
  return r.scrollWidth <= r.clientWidth + 1
})
console.log("narrow no horizontal overflow:", narrow)
await page.screenshot({ path: S + "/observe-narrow.png", fullPage: true })
await browser.close()
