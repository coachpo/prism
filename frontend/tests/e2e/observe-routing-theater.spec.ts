import { mkdir } from "node:fs/promises"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { expect, test, type Page } from "@playwright/test"
import { createDashboardSnapshot, dashboardAggregateTimestamp } from "./dashboard-aggregate-fixtures"

const evidenceDirectory = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..", "..", ".omo", "evidence", "frontend-rewrite")
const dashboardScreenshotPath = resolve(evidenceDirectory, "task-7-dashboard.png")
const staleScreenshotPath = resolve(evidenceDirectory, "task-7-realtime-stale.png")
const routeReadyTimeout = 15_000

function createProfile(id: number, name: string, isActive = false) {
  return {
    id,
    name,
    description: null,
    is_active: isActive,
    is_default: id === 1,
    is_editable: true,
    version: 1,
    created_at: dashboardAggregateTimestamp,
    deleted_at: null,
    updated_at: dashboardAggregateTimestamp,
  }
}
function createRecentRequestLogItem(id = 301) {
  return {
    id,
    created_at: dashboardAggregateTimestamp,
    model_id: "model-a",
    model_label: "Model A",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    is_proxy_origin: false,
    caller_client_display: "Observe Fixture Row",
    upstream_client_display: "Observe Fixture Row",
    user_agent_overridden: false,
    api_family: "openai" as const,
    vendor_id: null,
    vendor_key: null,
    vendor_name: null,
    endpoint_id: 201,
    endpoint_label: "Endpoint A",
    connection_id: 501,
    ttft_ms: 80,
    completion_duration_ms: 240,
    status_code: 200,
    response_time_ms: 640,
    is_stream: false,
    stream_outcome: "not_streaming" as const,
    stream_error_kind: null,
    reasoning_effort: null,
    output_tokens: 48,
    total_tokens: 120,
    total_cost_user_currency_micros: 250000,
    priced_flag: true,
    unpriced_reason: null,
    report_currency_symbol: "$",
  }
}

function createRequestLogDetail(id = 301) {
  return {
    summary: {
      id,
      created_at: dashboardAggregateTimestamp,
      model_id: "model-a",
      model_label: "Model A",
      resolved_target_model_id: null,
      resolved_target_model_label: null,
      api_family: "openai" as const,
      vendor_id: null,
      vendor_key: null,
      vendor_name: null,
      status_code: 200,
      response_time_ms: 640,
      is_stream: false,
      ttft_ms: 80,
      completion_duration_ms: 240,
    },
    request: {
      request_path: "/v1/responses",
      ingress_request_id: `observe-ingress-${id}`,
      attempt_number: 1,
      provider_correlation_id: `provider-corr-${id}`,
      proxy_api_key_id: null,
      proxy_api_key_name_snapshot: null,
      caller_user_agent: "Observe Fixture Row",
      upstream_user_agent: "Observe Fixture Row",
      caller_client_display: "Observe Fixture Row",
      upstream_client_display: "Observe Fixture Row",
      user_agent_overridden: false,
      error_detail: null,
    },
    routing: {
      profile_id: 1,
      model_id: "model-a",
      resolved_target_model_id: null,
      endpoint_id: 201,
      endpoint_label: "Endpoint A",
      endpoint_base_url: "https://endpoint-a.example",
      endpoint_description: "Primary endpoint",
      connection_id: 501,
      audit_enabled_at_request: true,
      audit_capture_bodies_at_request: true,
    },
    usage: {
      input_tokens: 72,
      output_tokens: 48,
      total_tokens: 120,
      success_flag: true,
      billable_flag: true,
      priced_flag: true,
      unpriced_reason: null,
      cache_read_input_tokens: 0,
      cache_creation_input_tokens: 0,
      reasoning_tokens: 0,
    },
    costing: {
      input_cost_micros: 100000,
      output_cost_micros: 150000,
      cache_read_input_cost_micros: 0,
      cache_creation_input_cost_micros: 0,
      reasoning_cost_micros: 0,
      total_cost_original_micros: 250000,
      total_cost_user_currency_micros: 250000,
      currency_code_original: "USD",
      report_currency_code: "USD",
      report_currency_symbol: "$",
      fx_rate_used: "1",
      fx_rate_source: "manual",
    },
    pricing: {
      pricing_snapshot_unit: "1M tokens",
      pricing_snapshot_input: "0.10",
      pricing_snapshot_output: "0.20",
      pricing_snapshot_cache_read_input: null,
      pricing_snapshot_cache_creation_input: null,
      pricing_snapshot_reasoning: null,
      pricing_config_version_used: 1,
    },
  }
}

async function mockObserveRoutes(page: Page, options: { stale?: boolean } = {}) {
  const profiles = [createProfile(1, "Red Team", true)]
  const dashboardSnapshot = createDashboardSnapshot({
    metricSnapshot: {
      error_rate: options.stale ? 6.5 : 2.4,
      p95_latency: options.stale ? 3200 : 180,
      unpriced_request_count: options.stale ? 2 : 0,
    },
    recentRequests: [createRecentRequestLogItem(301)],
  })
  dashboardSnapshot.health = options.stale
    ? { lag_seconds: 420, stale: true, stale_after_seconds: 300 }
    : { lag_seconds: 0, stale: false, stale_after_seconds: 300 }

  await page.addInitScript(() => localStorage.setItem("prism.locale", "en"))

  await page.route("**/*", async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const { pathname, searchParams } = url

    if (!pathname.startsWith("/api/")) {
      return route.continue()
    }

    const fulfillJson = (body: unknown, status = 200) => route.fulfill({
      status,
      contentType: "application/json",
      body: JSON.stringify(body),
    })

    if (pathname === "/api/auth/status") {
      return fulfillJson({ auth_enabled: false })
    }

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson({ profiles, active_profile: profiles[0], profile_limits: { max_profiles: 5 } })
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson({ report_currency_code: "USD", report_currency_symbol: "$", endpoint_fx_mappings: [], timezone_preference: null })
    }

    if (pathname === "/api/settings/timezone") {
      return fulfillJson({ timezone_preference: "UTC" })
    }

    if (pathname === "/api/stats/dashboard") {
      return fulfillJson(dashboardSnapshot)
    }

    if (pathname === "/api/stats/requests") {
      return fulfillJson({
        items: [createRecentRequestLogItem(301)],
        total: 1,
        limit: Number(searchParams.get("limit") ?? "100"),
        offset: Number(searchParams.get("offset") ?? "0"),
        filter_options: { endpoints: [], models: [] },
      })
    }

    if (pathname === "/api/stats/requests/301") {
      return fulfillJson(createRequestLogDetail(301))
    }

    if (pathname === "/api/models") {
      return fulfillJson([])
    }

    if (pathname === "/api/models/101") {
      return fulfillJson({
        id: 101,
        vendor_id: null,
        vendor: null,
        api_family: "openai",
        model_id: "model-a",
        display_name: "Model A",
        model_type: "native",
        proxy_targets: [],
        loadbalance_strategy_id: null,
        loadbalance_strategy: null,
        is_enabled: true,
        connection_count: 1,
        active_connection_count: 1,
        health_success_rate: 97.6,
        health_total_requests: 42,
        created_at: dashboardAggregateTimestamp,
        updated_at: dashboardAggregateTimestamp,
        connections: [],
      })
    }

    return fulfillJson({}, 404)
  })
}
test.describe("observe routing theater", () => {
  test("renders operator dashboard and drills into typed targets", async ({ page }) => {
    const consoleErrors: string[] = []
    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text())
      }
    })
    page.on("pageerror", (error) => consoleErrors.push(error.message))

    await page.setViewportSize({ width: 1440, height: 980 })
    await mockObserveRoutes(page)
    await page.goto("/observe")

    await expect(page.getByTestId("observe-dashboard")).toBeVisible({ timeout: routeReadyTimeout })
    await expect(page.getByRole("heading", { name: /observe model traffic/i })).toBeVisible()
    await expect(page.getByTestId("observe-routing-theater")).toBeVisible()
    await expect(page.getByTestId("observe-request-stream")).toBeVisible()
    await expect(page.getByTestId("observe-health-strip")).toBeVisible()
    await expect(page.getByTestId("observe-band-spend")).toBeVisible()
    await expect(page.getByTestId("observe-band-latency")).toBeVisible()
    await expect(page.getByTestId("observe-band-success")).toBeVisible()
    await expect(page.getByTestId("routing-diagram-card")).toBeVisible()
    await expect(page.getByTestId("request-stream-row-0")).toContainText("Model A")

    await mkdir(dirname(dashboardScreenshotPath), { recursive: true })
    await page.screenshot({ path: dashboardScreenshotPath, fullPage: true })

    const modelAction = page
      .getByTestId("routing-diagram-node-model-model-101")
      .getByRole("button", { name: "View model details for Model A" })
    await modelAction.click()
    await expect(page).toHaveURL(/\/models\/101$/)

    await page.goto("/observe")
    await expect(page.getByTestId("observe-dashboard")).toBeVisible({ timeout: routeReadyTimeout })
    await page.getByTestId("request-stream-row-0").click()
    await expect(page).toHaveURL(/\/observe\/requests\?request_id=301$/)
    await expect(page.getByTestId("request-log-detail-sheet")).toBeVisible({ timeout: routeReadyTimeout })

    expect(consoleErrors).toEqual([])
  })

  test("keeps realtime stale banner visible without blocking dashboard data", async ({ page }) => {
    const consoleErrors: string[] = []
    page.on("console", (message) => {
      if (message.type() === "error") {
        consoleErrors.push(message.text())
      }
    })
    page.on("pageerror", (error) => consoleErrors.push(error.message))

    await page.setViewportSize({ width: 1440, height: 980 })
    await mockObserveRoutes(page, { stale: true })
    await page.goto("/observe")

    await expect(page.getByTestId("observe-dashboard")).toBeVisible({ timeout: routeReadyTimeout })
    await expect(page.getByTestId("realtime-stale-banner")).toBeVisible()
    await expect(page.getByTestId("observe-routing-theater")).toBeVisible()
    await expect(page.getByTestId("observe-request-stream")).toBeVisible()
    await expect(page.getByTestId("request-stream-row-0")).toContainText("Model A")
    await expect(page.getByTestId("observe-band-latency")).toContainText("3,200ms")

    await mkdir(dirname(staleScreenshotPath), { recursive: true })
    await page.screenshot({ path: staleScreenshotPath, fullPage: true })

    expect(consoleErrors).toEqual([])
  })
})
