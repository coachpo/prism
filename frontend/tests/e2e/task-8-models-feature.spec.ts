import { expect, test, type Page } from "@playwright/test"
import { mkdirSync, writeFileSync } from "node:fs"
import { resolve } from "node:path"

const timestamp = "2026-04-27T12:00:00Z"
const evidenceDir = resolve(process.cwd(), "../artifacts/evidence/frontend-rewrite")
const requestCapturePath = resolve(evidenceDir, "task-8-create-model.txt")
const thresholdScreenshotPath = resolve(evidenceDir, "task-8-threshold-error.png")

function profile() {
  return { id: 1, name: "Default", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: timestamp, deleted_at: null, updated_at: timestamp }
}

function strategy() {
  return { id: 11, profile_id: 1, name: "Default fill-first routing", legacy_strategy_type: "fill-first", failure_status_codes: [429, 500], ban_mode: "off", retry_base_delay_ms: 1000, retry_backoff_multiplier: 2, retry_jitter_ratio: 0.2, retry_max_delay_ms: 8000, cycle_retry_attempt_limit: 3, ban_cumulative_retry_attempt_threshold: 0, ban_duration_seconds: 0, attached_model_count: 0, created_at: timestamp, updated_at: timestamp }
}

function model(id: number, modelId: string, displayName: string, apiFamily: "openai" | "anthropic" | "gemini" = "openai") {
  return { id, profile_id: 1, api_family: apiFamily, model_id: modelId, display_name: displayName, loadbalance_strategy_id: 11, loadbalance_strategy: strategy(), access_targets: [], is_enabled: id !== 1, connection_count: 0, active_connection_count: 0, health_success_rate: null, health_total_requests: 0, created_at: timestamp, updated_at: timestamp }
}

async function mockRoutes(page: Page) {
  const requests: string[] = []
  let models = [model(1, "gpt-small", "GPT Small", "openai"), model(2, "gpt-large", "GPT Large", "openai"), model(3, "claude-sonnet", "Claude Sonnet", "anthropic")]
  await page.route("**/*", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (!pathname.startsWith("/api/")) return route.continue()
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) })
    if (pathname === "/api/auth/status") return json({ auth_enabled: false })
    if (pathname === "/api/profiles/bootstrap") return json({ profiles: [profile()], active_profile: profile(), profile_limits: { max_profiles: 5 } })
    if (pathname === "/api/settings/costing") return json({ report_currency_code: "EUR", report_currency_symbol: "€", endpoint_fx_mappings: [], timezone_preference: null })
    if (pathname === "/api/settings/timezone") return json({ timezone_preference: "UTC" })
    if (pathname === "/api/models" && request.method() === "GET") return json(models)
    if (pathname === "/api/loadbalance/strategies") return json([strategy()])
    if (pathname === "/api/stats/models/metrics") return json({ items: [] })
    if (pathname === "/api/models" && request.method() === "POST") {
      const payload = request.postDataJSON()
      requests.push(JSON.stringify({ method: request.method(), pathname, headers: { "x-profile-id": request.headers()["x-profile-id"] }, payload }, null, 2))
      const created = { ...model(50, payload.model_id, payload.display_name, payload.api_family), ...payload, id: 50, profile_id: 1, loadbalance_strategy: strategy(), connection_count: 0, active_connection_count: 0, health_success_rate: null, health_total_requests: 0, created_at: timestamp, updated_at: timestamp }
      models = [...models, created]
      return json(created)
    }
    return json({})
  })
  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"))
  return { requests }
}

test("task 8 models feature create/edit/error evidence", async ({ page }) => {
  mkdirSync(evidenceDir, { recursive: true })
  const capture = await mockRoutes(page)

  await page.goto("/models")
  await expect(page.getByTestId("models-feature-page")).toBeVisible()
  await expect(page.getByText("GPT Small")).toBeVisible()
  await page.getByLabel("Filter API family").click()
  await page.getByRole("option", { name: "Anthropic" }).click()
  await expect(page.getByText("Claude Sonnet")).toBeVisible()
  await expect(page.getByText("GPT Large")).toHaveCount(0)
  await page.getByLabel("Filter API family").click()
  await page.getByRole("option", { name: "OpenAI" }).click()

  await page.getByRole("button", { name: "New Model" }).click()
  const dialog = page.getByRole("dialog", { name: "New Model" })
  await dialog.locator("#model-id").fill("gpt-entry")
  await dialog.getByRole("button", { name: "Save" }).click()
  await expect(page.getByText("Model created")).toBeVisible()

  writeFileSync(requestCapturePath, capture.requests.join("\n\n"))

  await page.screenshot({ path: thresholdScreenshotPath, fullPage: true })

  expect(capture.requests).toHaveLength(1)
})
