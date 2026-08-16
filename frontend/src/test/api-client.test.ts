import { http, HttpResponse } from "msw"
import { beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api"
import { authSessionCoordinator } from "@/context/auth/coordinatorInstance"
import { rewriteTestServer } from "@/test"

const TEST_SUBJECT = "auth:subject:test"

function establishAuthenticatedSession() {
  // The coordinator is process-local: each test re-establishes an
  // authenticated phase so the eligible-401 path is exercised.
  authSessionCoordinator.applyLoginSuccess({
    authenticated: true,
    auth_enabled: true,
    username: "admin",
    subject_key: TEST_SUBJECT,
  })
}

describe("api client contracts", () => {
  beforeEach(() => {
    establishAuthenticatedSession()
  })

  it("retries one eligible api request after auth refresh and preserves credentials", async () => {
    let modelRequests = 0
    let refreshRequests = 0
    const credentials: RequestCredentials[] = []

    rewriteTestServer.use(
      http.get("/api/models", ({ request }) => {
        modelRequests += 1
        credentials.push(request.credentials)
        if (modelRequests === 1) {
          return HttpResponse.json({ code: "auth_not_authenticated", detail: "expired" }, { status: 401 })
        }
        return HttpResponse.json([])
      }),
      http.post("/api/auth/refresh", ({ request }) => {
        refreshRequests += 1
        credentials.push(request.credentials)
        return HttpResponse.json({
          authenticated: true,
          auth_enabled: true,
          username: "admin",
          subject_key: TEST_SUBJECT,
        })
      }),
    )

    await expect(api.models.list()).resolves.toEqual([])

    expect(modelRequests).toBe(2)
    expect(refreshRequests).toBe(1)
    expect(credentials).toEqual(["include", "include", "include"])
  })

  it("does not loop refresh retries when the retried api request remains unauthorized", async () => {
    let modelRequests = 0
    let refreshRequests = 0

    rewriteTestServer.use(
      http.get("/api/models", () => {
        modelRequests += 1
        return HttpResponse.json({ code: "auth_not_authenticated", detail: "still expired" }, { status: 401 })
      }),
      http.post("/api/auth/refresh", () => {
        refreshRequests += 1
        return HttpResponse.json({
          authenticated: true,
          auth_enabled: true,
          username: "admin",
          subject_key: TEST_SUBJECT,
        })
      }),
    )

    await expect(api.models.list()).rejects.toMatchObject({
      message: "still expired",
      status: 401,
    })

    expect(modelRequests).toBe(2)
    expect(refreshRequests).toBe(1)
  })

  it("never replays after a definitive expired refresh", async () => {
    let modelRequests = 0
    let refreshRequests = 0

    rewriteTestServer.use(
      http.get("/api/models", () => {
        modelRequests += 1
        return HttpResponse.json({ code: "auth_not_authenticated", detail: "expired" }, { status: 401 })
      }),
      http.post("/api/auth/refresh", () => {
        refreshRequests += 1
        return HttpResponse.json({ authenticated: false, auth_enabled: true, username: null })
      }),
    )

    await expect(api.models.list()).rejects.toMatchObject({ name: "AuthPhaseChangedError" })
    expect(modelRequests).toBe(1)
    expect(refreshRequests).toBe(1)
    expect(authSessionCoordinator.getPhase().kind).toBe("SESSION_EXPIRED")
  })

  it("routes protected CSV exports through the auth coordinator", async () => {
    let exportRequests = 0
    let refreshRequests = 0

    rewriteTestServer.use(
      http.get("/api/stats/requests/export", () => {
        exportRequests += 1
        if (exportRequests === 1) {
          return HttpResponse.json({ code: "auth_not_authenticated", detail: "expired" }, { status: 401 })
        }
        return new HttpResponse("request_id,endpoint_label\n1,primary\n", {
          headers: { "Content-Type": "text/csv" },
        })
      }),
      http.post("/api/auth/refresh", () => {
        refreshRequests += 1
        return HttpResponse.json({
          authenticated: true,
          auth_enabled: true,
          username: "admin",
          subject_key: TEST_SUBJECT,
        })
      }),
    )

    const blob = await api.stats.exportCsv()
    await expect(blob.text()).resolves.toContain("request_id,endpoint_label")
    expect(exportRequests).toBe(2)
    expect(refreshRequests).toBe(1)
  })
})
