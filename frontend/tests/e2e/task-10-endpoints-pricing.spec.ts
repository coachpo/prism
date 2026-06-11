import { expect, test, type Page } from "@playwright/test"

const timestamp = "2026-06-11T12:00:00Z"
const evidenceDir = "../.omo/evidence/frontend-rewrite"

function createProfile() {
  return { id: 71, name: "Selected profile", description: null, is_active: true, is_default: true, is_editable: true, version: 1, created_at: timestamp, deleted_at: null, updated_at: timestamp }
}

function createEndpoint(id: number, name: string, position: number, masked = "sk-...prod") {
  return { id, profile_id: 71, name, base_url: `https://${name.toLowerCase().replace(/ /g, "-")}.example/v1`, has_api_key: true, masked_api_key: masked, position, created_at: timestamp, updated_at: timestamp }
}

function createPricingTemplate(id: number, name: string) {
  return { id, profile_id: 71, name, description: "Fixture pricing", pricing_unit: "PER_1M", pricing_currency_code: "USD", input_price: "1", output_price: "2", cached_input_price: "0", cache_creation_price: "0", reasoning_price: "0", version: 1, created_at: timestamp, updated_at: timestamp }
}

async function mockSharedRoutes(page: Page) {
  const profile = createProfile()
  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"))
  await page.route("**/*", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const { pathname } = url
    if (!pathname.startsWith("/api/")) return route.continue()
    const fulfillJson = (body: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) })
    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false })
    if (pathname === "/api/profiles/bootstrap") return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } })
    if (pathname === "/api/settings/costing") return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null })
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" })
    return fulfillJson({ error: `Unhandled ${request.method()} ${pathname}` }, 500)
  })
}

test("Task 18 endpoint create conflict stays visible in the dialog", async ({ page }) => {
  const endpoints = [createEndpoint(101, "Primary Endpoint", 0)]
  await mockSharedRoutes(page)
  await page.route("**/api/endpoints**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === "/api/endpoints" && request.method() === "GET") return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(endpoints) })
    if (url.pathname === "/api/endpoints" && request.method() === "POST") {
      return route.fulfill({ status: 409, contentType: "application/json", body: JSON.stringify({ detail: { errors: [{ field: "base_url", code: "endpoint_conflict", message: "Base URL already belongs to Primary Endpoint" }] } }) })
    }
    return route.fallback()
  })
  await page.route("**/api/models/by-endpoints", async (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [] }) }))

  await page.goto("/route/endpoints")
  await expect(page.getByTestId("endpoints-feature-page")).toBeVisible()
  await page.getByRole("button", { name: "Add Endpoint" }).click()
  const dialog = page.getByRole("dialog", { name: "Add Endpoint" })
  await dialog.getByLabel("Name").fill("Duplicate endpoint")
  await dialog.getByLabel("Base URL").fill("https://primary-endpoint.example/v1")
  await dialog.getByLabel("API Key").fill("sk-test-secret")
  await dialog.getByRole("button", { name: "Add Endpoint" }).click()

  await expect(dialog.getByTestId("endpoint-form-server-error")).toContainText("base_url (endpoint_conflict): Base URL already belongs to Primary Endpoint")
  await expect(dialog).toBeVisible()
  await page.screenshot({ path: `${evidenceDir}/task-18-server-conflict.png`, fullPage: true })
})

test("Task 10 endpoints filter disables reorder and edit preserves masked secret", async ({ page }) => {
  const endpoints = [
    createEndpoint(101, "Primary Endpoint", 0),
    createEndpoint(102, "Secondary Endpoint", 1),
    createEndpoint(103, "Backup Endpoint", 2),
    createEndpoint(104, "Archive Endpoint", 3),
  ]
  const updateRequests: unknown[] = []
  await mockSharedRoutes(page)
  await page.route("**/api/endpoints**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === "/api/endpoints" && request.method() === "GET") return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(endpoints) })
    if (url.pathname === "/api/endpoints/101" && request.method() === "PUT") {
      updateRequests.push(request.postDataJSON())
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ ...endpoints[0], name: "Primary Endpoint", masked_api_key: "sk-...prod" }) })
    }
    return route.fallback()
  })
  await page.route("**/api/models/by-endpoints", async (route) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [{ endpoint_id: 101, models: [{ id: 1, model_id: "gpt-4o", display_name: "GPT-4o", api_family: "openai", vendor_id: null, vendor: null, model_type: "native", proxy_targets: [], loadbalance_strategy_id: null, loadbalance_strategy: null, is_enabled: true, connection_count: 1, active_connection_count: 1, health_success_rate: null, health_total_requests: 0, created_at: timestamp, updated_at: timestamp, access_targets: [] }] }] }) }))

  await page.goto("/route/endpoints")
  await expect(page.getByTestId("endpoints-feature-page")).toBeVisible()
  await page.getByRole("button", { name: "Edit endpoint Primary Endpoint" }).click()
  await expect(page.getByPlaceholder("sk-...prod")).toBeVisible()
  await page.getByRole("button", { name: "Save Changes" }).click()
  await expect.poll(() => updateRequests.length).toBe(1)
  expect(updateRequests[0]).toEqual({ name: "Primary Endpoint", base_url: "https://primary-endpoint.example/v1" })
  await expect(page.getByText("sk-live-secret")).toHaveCount(0)
  await expect(page.getByText("sk-...prod").first()).toBeVisible()

  await page.getByPlaceholder("Search endpoints...").fill("Secondary")
  await expect(page.getByText("Reordering is disabled while review filters are active.")).toBeVisible()
  await expect(page.getByRole("button", { name: "Drag to reorder endpoint Secondary Endpoint" })).toBeDisabled()
  await page.screenshot({ path: `${evidenceDir}/task-10-endpoint-filter-reorder.png`, fullPage: true })
})

test("Task 10 pricing template delete shows usage blockers", async ({ page }) => {
  const template = createPricingTemplate(301, "Blocked Template")
  await mockSharedRoutes(page)
  await page.route("**/api/pricing-templates**", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === "/api/pricing-templates" && request.method() === "GET") return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify([template]) })
    if (url.pathname === "/api/pricing-templates/301/connections") {
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: [{ connection_id: 901, connection_name: "Primary terminal target", model_config_id: 801, model_id: "gpt-4o", endpoint_id: 101, endpoint_name: "Primary Endpoint" }] }) })
    }
    return route.fallback()
  })

  await page.goto("/route/pricing")
  await expect(page.getByTestId("pricing-feature-page")).toBeVisible()
  await page.getByRole("button", { name: /delete/i }).click()
  await expect(page.getByText(/Cannot delete this template because it is currently used by 1 terminal target/)).toBeVisible()
  await expect(page.getByText("Primary terminal target")).toBeVisible()
  await expect(page.getByRole("button", { name: "Delete" })).toBeDisabled()
  await page.screenshot({ path: `${evidenceDir}/task-10-pricing-delete-blocked.png`, fullPage: true })
})
