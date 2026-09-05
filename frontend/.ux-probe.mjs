import { chromium } from "@playwright/test"

const base = "http://127.0.0.1:5201"
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
const errors = []
page.on("pageerror", (e) => errors.push("pageerror: " + e.message))
page.on("console", (m) => { if (m.type() === "error") errors.push("console: " + m.text()) })

async function shot(url, file, wait = 2500) {
  await page.goto(base + url, { waitUntil: "domcontentloaded" })
  await page.waitForTimeout(wait)
  await page.screenshot({ path: file, fullPage: true })
}

await shot("/observe", "/tmp/claude-1001/-home-qing-projects-prism--claude-worktrees-ultracode-model-page-ui-ux-677985/c1f31cf5-c3bf-45f9-8b3b-30245768161d/scratchpad/observe-trend.png", 3500)
console.log("URL after trend:", page.url())
console.log("routing health card:", (await page.getByTestId("routing-health-entry").innerText()).replace(/\n/g, " | "))
const metricGroup = page.getByRole("group", { name: "指标" })
console.log("metric radios:", await metricGroup.getByRole("radio").count(), "enabled:", await metricGroup.getByRole("radio", { disabled: false }).count())
console.log("interval group:", await page.getByRole("group", { name: "时间桶" }).count())
console.log("chart card desc:", await page.locator('[data-slot="card-description"]').allInnerTexts())

await shot("/observe?tab=terminal_targets", "/tmp/claude-1001/-home-qing-projects-prism--claude-worktrees-ultracode-model-page-ui-ux-677985/c1f31cf5-c3bf-45f9-8b3b-30245768161d/scratchpad/observe-tt.png", 3000)
console.log("tt scope group:", await page.getByRole("group", { name: "终端目标统计口径" }).count())
const triggers = page.locator('[data-testid^="tt-endpoint-"]')
const n = await triggers.count()
console.log("endpoint rows:", n)
for (let i = 0; i < Math.min(n, 3); i++) { await triggers.nth(i).click(); await page.waitForTimeout(1200) }
await page.screenshot({ path: "/tmp/claude-1001/-home-qing-projects-prism--claude-worktrees-ultracode-model-page-ui-ux-677985/c1f31cf5-c3bf-45f9-8b3b-30245768161d/scratchpad/observe-tt-open.png", fullPage: true })
console.log("expanded tables:", await page.locator('[data-testid="tt-table"]').count(), "empties:", await page.locator('[data-testid="tt-empty"]').count())
console.log("tt th count:", await page.locator('[data-testid="tt-table"] th').count())

await shot("/observe?tab=trend&scope=final_execution&metric=ttft", "/tmp/claude-1001/-home-qing-projects-prism--claude-worktrees-ultracode-model-page-ui-ux-677985/c1f31cf5-c3bf-45f9-8b3b-30245768161d/scratchpad/observe-rewrite.png", 3000)
console.log("URL after rewrite:", page.url())
console.log("rewrite notice:", await page.getByTestId("observe-scope-rewrite-notice").count(), await page.getByTestId("observe-scope-rewrite-notice").innerText().catch(() => "-"))

await shot("/observe?tab=errors&group_by=ingress_model", "/tmp/claude-1001/-home-qing-projects-prism--claude-worktrees-ultracode-model-page-ui-ux-677985/c1f31cf5-c3bf-45f9-8b3b-30245768161d/scratchpad/observe-errors.png", 3000)
console.log("URL after errors:", page.url())

console.log("ERRORS:", errors.slice(0, 10))
await browser.close()
