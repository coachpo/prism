import { expect, test, type Page, type Route } from "@playwright/test"

const timestamp = "2026-06-11T13:00:00Z"
const evidenceDir = "../.omo/evidence/frontend-rewrite"

type StrategyType = "single" | "fill-first" | "round-robin"
type BanMode = "off" | "temporary" | "until_reset"

function profile() {
  return { id: 71, name: "Selected profile", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: timestamp, deleted_at: null, updated_at: timestamp }
}

function strategy(id: number, name: string, attached_model_count = 0, legacy_strategy_type: StrategyType = "single", ban_mode: BanMode = "off") {
  return { id, profile_id: 71, name, legacy_strategy_type, failure_status_codes: [403, 422, 429, 500, 502, 503, 504, 529], ban_mode, retry_base_delay_ms: 60000, retry_backoff_multiplier: 2, retry_jitter_ratio: 0.2, retry_max_delay_ms: 900000, cycle_retry_attempt_limit: 3, ban_cumulative_retry_attempt_threshold: ban_mode === "off" ? 0 : 6, ban_duration_seconds: ban_mode === "temporary" ? 120 : 0, attached_model_count, created_at: timestamp, updated_at: timestamp }
}

function fulfillJson(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) })
}

async function mockBaseRoutes(page: Page, strategies: unknown[]) {
  const selectedProfile = profile()
  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"))
  await page.route("**/*", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (!pathname.startsWith("/api/")) return route.continue()
    if (pathname === "/api/auth/status") return fulfillJson(route, { auth_enabled: false })
    if (pathname === "/api/profiles/bootstrap") return fulfillJson(route, { profiles: [selectedProfile], active_profile: selectedProfile, profile_limits: { max_profiles: 5 } })
    if (pathname === "/api/settings/costing") return fulfillJson(route, { report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null })
    if (pathname === "/api/settings/timezone") return fulfillJson(route, { timezone_preference: "UTC" })
    if (pathname === "/api/loadbalance/strategies" && request.method() === "GET") return fulfillJson(route, strategies)
    if (pathname === "/api/loadbalance/strategies/defaults" && request.method() === "POST") return fulfillJson(route, { items: strategies, created_count: 0, created_names: [], existing_names: [] })
    if (pathname === "/api/loadbalance/current-state") return fulfillJson(route, { items: [] })
    if (pathname === "/api/loadbalance/events") return fulfillJson(route, { items: [], total: 0, limit: 25, offset: 0 })
    return fulfillJson(route, { error: `Unhandled ${request.method()} ${pathname}` }, 500)
  })
}

test("Task 11 invalid status codes do not send Ban Policy create", async ({ page }) => {
  let createRequests = 0
  await mockBaseRoutes(page, [])
  await page.route("**/api/loadbalance/strategies", async (route) => {
    if (route.request().method() === "POST") {
      createRequests += 1
      return fulfillJson(route, strategy(99, "Should not create"))
    }
    return route.fallback()
  })

  await page.goto("/route/ban-policies")
  await expect(page.getByTestId("ban-policies-feature-page")).toBeVisible()
  await page.getByRole("button", { name: "Add Strategy" }).first().click()
  await page.getByLabel("Name").fill("Invalid status policy")
  await page.getByLabel("Failure Status Codes").fill("99,600")
  await page.getByRole("button", { name: "Save Strategy" }).click()

  await expect(page.getByText("Status code must be a whole number between 100 and 599").first()).toBeVisible()
  expect(createRequests).toBe(0)
  await page.screenshot({ path: `${evidenceDir}/task-11-status-code-error.png`, fullPage: true })
})

test("Task 11 attached-model delete conflict remains visible and blocked", async ({ page }) => {
  const rows = [strategy(41, "Attached policy", 0, "round-robin", "until_reset")]
  await mockBaseRoutes(page, rows)
  await page.route("**/api/loadbalance/strategies/41", async (route) => {
    if (route.request().method() === "DELETE") {
      return fulfillJson(route, { detail: { message: "strategy is attached to models", attached_model_count: 2 } }, 409)
    }
    return route.fallback()
  })

  await page.goto("/route/ban-policies")
  await expect(page.getByTestId("ban-policies-feature-page")).toBeVisible()
  const row = page.getByRole("row", { name: /Attached policy/ })
  await row.getByRole("button", { name: "Delete" }).click()
  await expect(page.getByRole("dialog")).toContainText("Attached policy")
  await page.getByRole("button", { name: "Delete" }).click()
  await expect(page.getByText("This strategy is attached to 2 models and cannot be deleted yet.")).toBeVisible()
  await expect(page.getByRole("button", { name: "Close" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Delete" })).toHaveCount(0)
  await page.screenshot({ path: `${evidenceDir}/task-11-delete-conflict.png`, fullPage: true })
})
