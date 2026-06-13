import { expect, test, type Page, type Route } from "@playwright/test"
import { mkdirSync, writeFileSync } from "node:fs"
import path from "node:path"

const timestamp = "2026-04-28T12:00:00Z"
const evidenceDir = path.resolve(process.cwd(), "../.omo/evidence/frontend-rewrite")
const profileConflictEvidencePath = path.join(evidenceDir, "task-6-profile-conflict.png")
const refreshEvidencePath = path.join(evidenceDir, "task-6-refresh.txt")

type ProfileFixture = ReturnType<typeof createProfile>

function ensureEvidenceDir() {
  mkdirSync(evidenceDir, { recursive: true })
}

function createProfile(id: number, name: string, flags: { active?: boolean; default?: boolean } = {}) {
  return {
    id,
    name,
    description: `${name} profile`,
    is_active: Boolean(flags.active),
    is_default: Boolean(flags.default),
    is_editable: !flags.default,
    version: 1,
    created_at: timestamp,
    deleted_at: null,
    updated_at: timestamp,
  }
}

function createCostingSettings() {
  return {
    report_currency_code: "USD",
    report_currency_symbol: "$",
    endpoint_fx_mappings: [],
    timezone_preference: null,
  }
}
async function fulfillJson(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  })
}

async function seedLocale(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("prism.locale", "en")
    localStorage.removeItem("prism.selectedProfileId")
  })
}

async function installTask6Routes(
  page: Page,
  options: {
    firstModelsRequestUnauthorized?: boolean
    onActivationConflict?: (payload: unknown) => void
    onRefresh?: () => void
  } = {},
) {
  let profiles: ProfileFixture[] = [
    createProfile(1, "Default", { active: true, default: true }),
    createProfile(2, "Blue Team"),
    createProfile(3, "Red Team"),
  ]
  let modelRequests = 0

  await page.route("**/*", async (route) => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname

    if (!pathname.startsWith("/api/")) {
      return route.continue()
    }

    if (pathname === "/api/auth/status") {
      return fulfillJson(route, { auth_enabled: true })
    }

    if (pathname === "/api/auth/session" || pathname === "/api/auth/refresh") {
      if (pathname === "/api/auth/refresh") {
        options.onRefresh?.()
      }
      return fulfillJson(route, {
        authenticated: true,
        auth_enabled: true,
        username: "admin",
      })
    }

    if (pathname === "/api/profiles/bootstrap") {
      return fulfillJson(route, {
        profiles,
        active_profile: profiles.find((profile) => profile.is_active) ?? null,
        profile_limits: { max_profiles: 5 },
      })
    }

    if (pathname === "/api/profiles") {
      return fulfillJson(route, profiles)
    }

    const activationMatch = pathname.match(/^\/api\/profiles\/(\d+)\/activate$/)
    if (activationMatch && request.method() === "POST") {
      const payload = request.postDataJSON()
      options.onActivationConflict?.(payload)
      profiles = [
        createProfile(1, "Default", { default: true }),
        createProfile(2, "Blue Team"),
        createProfile(3, "Red Team", { active: true }),
      ]
      return fulfillJson(route, { detail: "stale active profile" }, 409)
    }

    if (pathname === "/api/settings/costing") {
      return fulfillJson(route, createCostingSettings())
    }

    if (pathname === "/api/loadbalance/strategies") {
      return fulfillJson(route, [])
    }

    if (pathname === "/api/models") {
      modelRequests += 1
      if (options.firstModelsRequestUnauthorized && modelRequests === 1) {
        return fulfillJson(route, { detail: "expired" }, 401)
      }
      return fulfillJson(route, [])
    }

    return fulfillJson(route, {}, 404)
  })

  return {
    getModelRequests: () => modelRequests,
  }
}

test.describe("task 6 auth and profile smoke", () => {
  test.beforeEach(async ({ page }) => {
    ensureEvidenceDir()
    await seedLocale(page)
  })

  test("refreshes stale active profile state after activation conflict", async ({ page }) => {
    const activationPayloads: unknown[] = []
    await installTask6Routes(page, {
      onActivationConflict: (payload) => activationPayloads.push(payload),
    })

    await page.goto("/models")
    await expect(page.getByRole("heading", { name: "Models", exact: true })).toBeVisible()

    await page.getByTestId("shell-profile-switcher").getByRole("button").click()
    await page.getByRole("menuitem", { name: /Blue Team/ }).click()
    await expect(page.getByText("Blue Team · Runtime: Default")).toBeVisible()

    await page.getByRole("button", { name: "Activate" }).click()
    await expect(page.getByRole("dialog", { name: /Blue Team/ })).toBeVisible()
    await page.getByRole("dialog").getByRole("button", { name: "Activate" }).click()

    await expect(page.getByRole("dialog").getByText("Activation conflict detected. Active profile changed elsewhere, profile state was refreshed.")).toBeVisible()
    await expect(page.getByText("Blue Team · Runtime: Red Team")).toBeVisible()
    expect(activationPayloads).toEqual([{ expected_active_profile_id: 1 }])

    await page.screenshot({ path: profileConflictEvidencePath, fullPage: true })
  })

  test("loads a protected route after one auth refresh retry", async ({ page }) => {
    let refreshRequests = 0
    const { getModelRequests } = await installTask6Routes(page, {
      firstModelsRequestUnauthorized: true,
      onRefresh: () => {
        refreshRequests += 1
      },
    })

    await page.goto("/models")
    await expect(page.getByRole("heading", { name: "Models", exact: true })).toBeVisible()
    await expect.poll(getModelRequests).toBe(2)
    expect(refreshRequests).toBe(1)

    writeFileSync(
      refreshEvidencePath,
      [
        "Task 6 session refresh evidence",
        `model_requests=${getModelRequests()}`,
        `auth_refresh_requests=${refreshRequests}`,
        "first_model_request_status=401",
        "retried_model_request_status=200",
      ].join("\n") + "\n",
    )
  })
})
