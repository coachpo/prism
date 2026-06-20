import { expect, test, type Page } from "@playwright/test"

const timestamp = "2026-06-11T12:00:00Z"
const modelConfigId = 910
const evidenceDir = "../.omo/evidence/frontend-rewrite"

type ReorderMode = "success" | "error"

function createProfile() {
  return {
    id: 71,
    name: "Selected profile",
    description: null,
    is_active: true,
    is_default: true,
    is_editable: true,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  }
}
function createEndpoint() {
  return {
    id: 41,
    profile_id: 71,
    name: "OpenAI Endpoint",
    base_url: "https://api.openai.test/v1",
    has_api_key: true,
    masked_api_key: "sk-...test",
    position: 0,
    created_at: timestamp,
    updated_at: timestamp,
  }
}

function createConnection(id: number, name: string, priority: number) {
  const endpoint = createEndpoint()
  return {
    id,
    profile_id: 71,
    model_config_id: modelConfigId,
    api_family: "openai",
    endpoint_id: endpoint.id,
    endpoint,
    is_active: true,
    priority,
    name,
    auth_type: null,
    custom_headers: null,
    openai_text_capability: "responses_only",
    openai_probe_endpoint_variant: "responses_minimal",
    context_window_tokens: null,
    default_output_token_reserve: 4096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: null,
    context_capability_overrides: {
      context_window_tokens: null,
      default_output_token_reserve: null,
      max_context_utilization: null,
      preferred_context_utilization_threshold: null,
    },
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    health_status: "unknown",
    health_detail: null,
    last_health_check: null,
    created_at: timestamp,
    updated_at: timestamp,
  }
}

function createTarget(id: number, connection: ReturnType<typeof createConnection>, position: number) {
  return {
    id,
    target_type: "connection",
    target_model_id: null,
    connection_id: connection.id,
    position,
    is_enabled: true,
    target_model: null,
    connection: { ...connection, priority: position },
    created_at: timestamp,
    updated_at: timestamp,
  }
}

function createModel(accessTargets: ReturnType<typeof createTarget>[]) {
  return {
    id: modelConfigId,
    profile_id: 71,
    api_family: "openai",
    model_id: "task-9-routed-model",
    display_name: "Task 9 Routed Model",
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: accessTargets,
    is_enabled: true,
    connection_count: accessTargets.length,
    active_connection_count: accessTargets.length,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: timestamp,
    updated_at: timestamp,
  }
}

function createSpendingResponse() {
  return {
    summary: {
      total_cost_micros: 0,
      successful_request_count: 0,
      priced_request_count: 0,
      unpriced_request_count: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_read_input_tokens: 0,
      total_cache_creation_input_tokens: 0,
      total_reasoning_tokens: 0,
      total_tokens: 0,
      avg_cost_per_successful_request_micros: 0,
    },
    groups: [],
    groups_total: 0,
    top_spending_models: [],
    top_spending_endpoints: [],
    unpriced_breakdown: {},
    report_currency_code: "USD",
    report_currency_symbol: "$",
  }
}

async function mockRoutes(page: Page, mode: ReorderMode) {
  const profile = createProfile()
  const primary = createConnection(501, "Primary terminal target", 0)
  const secondary = createConnection(502, "Secondary terminal target", 1)
  const originalTargets = [createTarget(801, primary, 0), createTarget(802, secondary, 1)]
  let targets = originalTargets
  const reorderRequests: Array<{ path: string; payload: unknown; profileHeader: string | null }> = []

  await page.route("**/*", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const pathname = url.pathname
    const method = request.method()

    if (!pathname.startsWith("/api/")) return route.continue()

    const fulfillJson = (body: unknown, status = 200) => route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(body),
    })
    const sortedTargets = () => [...targets]
      .sort((left, right) => left.position - right.position)
      .map((target, position) => ({
        ...target,
        position,
        connection: { ...target.connection, priority: position },
      }))

    if (pathname === "/api/auth/status") return fulfillJson({ auth_enabled: false })
    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles: [profile], active_profile: profile, profile_limits: { max_profiles: 5 } })
    }
    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null })
    }
    if (pathname === "/api/settings/timezone") return fulfillJson({ timezone_preference: "UTC" })
    if (pathname === "/api/endpoints") return fulfillJson([createEndpoint()])
    if (pathname === "/api/loadbalance/strategies") return fulfillJson([])
    if (pathname === "/api/pricing-templates") return fulfillJson([])
    if (pathname === "/api/loadbalance/current-state") return fulfillJson({ items: [] })
    if (pathname === "/api/loadbalance/events") return fulfillJson({ items: [], total: 0, limit: 25, offset: 0 })
    if (pathname === "/api/stats/spending") return fulfillJson(createSpendingResponse())
    if (pathname === "/api/models" && method === "GET") return fulfillJson([createModel(sortedTargets())])
    if (pathname === `/api/models/${modelConfigId}` && method === "GET") return fulfillJson(createModel(sortedTargets()))
    if (pathname === `/api/models/${modelConfigId}/connections` && method === "GET") {
      return fulfillJson(sortedTargets().map((target) => target.connection))
    }

    const positionMatch = pathname.match(new RegExp(`^/api/models/${modelConfigId}/targets/(\\d+)/position$`))
    if (positionMatch && method === "PATCH") {
      const payload = request.postDataJSON() as { to_index?: number }
      reorderRequests.push({
        path: pathname,
        payload,
        profileHeader: request.headers()["x-profile-id"] ?? null,
      })
      if (mode === "error") return fulfillJson({ detail: "Reorder rejected" }, 500)

      const targetId = Number.parseInt(positionMatch[1], 10)
      const moved = targets.find((target) => target.id === targetId)
      targets = targets.filter((target) => target.id !== targetId)
      if (moved && typeof payload.to_index === "number") targets.splice(payload.to_index, 0, moved)
      targets = targets.map((target, position) => ({
        ...target,
        position,
        connection: { ...target.connection, priority: position },
      }))
      return fulfillJson(sortedTargets())
    }

    return fulfillJson({ error: `Unhandled ${method} ${pathname}` }, 500)
  })

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"))
  return { reorderRequests }
}

async function dragPrimaryBelowSecondary(page: Page) {
  await page.getByRole("button", { name: "Move target 1 down" }).click()
}

async function expectConnectionOrder(page: Page, expectedNames: string[]) {
  const cards = page.locator("[data-testid^='access-target-connection:']")
  await expect.poll(async () => {
    const labels = await cards.evaluateAll((elements) => elements.map((element) => {
      const text = element.textContent ?? "";
      if (text.includes("Primary terminal target")) return "Primary terminal target";
      if (text.includes("Secondary terminal target")) return "Secondary terminal target";
      return "";
    }).filter(Boolean))
    return labels
  }).toEqual(expectedNames)
}

test("Task 9 model detail connection reorder succeeds with selected-profile scoping", async ({ page }) => {
  const { reorderRequests } = await mockRoutes(page, "success")

  await page.goto(`/models/${modelConfigId}?tab=connections`)
  await expect(page.getByTestId("model-detail-feature-page")).toBeVisible()
  await expectConnectionOrder(page, ["Primary terminal target", "Secondary terminal target"])
  await dragPrimaryBelowSecondary(page)

  await expect.poll(() => reorderRequests.length).toBe(1)
  expect(reorderRequests[0]).toMatchObject({
    path: `/api/models/${modelConfigId}/targets/801/position`,
    payload: { to_index: 1 },
    profileHeader: "71",
  })
  await expectConnectionOrder(page, ["Secondary terminal target", "Primary terminal target"])
  await page.screenshot({ path: `${evidenceDir}/task-9-reorder-success.png`, fullPage: true })
})

test("Task 9 failed connection reorder rolls back and does not persist after reload", async ({ page }) => {
  const { reorderRequests } = await mockRoutes(page, "error")

  await page.goto(`/models/${modelConfigId}`)
  await expectConnectionOrder(page, ["Primary terminal target", "Secondary terminal target"])
  await dragPrimaryBelowSecondary(page)

  await expect.poll(() => reorderRequests.length).toBe(1)
  await expect(page.getByText("Reorder rejected")).toBeVisible()
  await expectConnectionOrder(page, ["Primary terminal target", "Secondary terminal target"])
  await page.screenshot({ path: `${evidenceDir}/task-9-reorder-error.png`, fullPage: true })

  await page.reload()
  await expectConnectionOrder(page, ["Primary terminal target", "Secondary terminal target"])
})
