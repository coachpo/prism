import { http, HttpResponse } from "msw"
import { afterEach, describe, expect, it } from "vitest"
import { api, ApiError, setApiProfileId } from "@/lib/api"
import { rewriteTestServer } from "@/test"

afterEach(() => {
  setApiProfileId(null)
})

describe("api client contracts", () => {
  it("retries one eligible api request after auth refresh and preserves credentials", async () => {
    let modelRequests = 0
    let refreshRequests = 0
    const credentials: RequestCredentials[] = []

    rewriteTestServer.use(
      http.get("/api/models", ({ request }) => {
        modelRequests += 1
        credentials.push(request.credentials)
        if (modelRequests === 1) {
          return HttpResponse.json({ detail: "expired" }, { status: 401 })
        }
        return HttpResponse.json([])
      }),
      http.post("/api/auth/refresh", ({ request }) => {
        refreshRequests += 1
        credentials.push(request.credentials)
        return HttpResponse.json({ authenticated: true })
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
        return HttpResponse.json({ detail: "still expired" }, { status: 401 })
      }),
      http.post("/api/auth/refresh", () => {
        refreshRequests += 1
        return HttpResponse.json({ authenticated: true })
      }),
    )

    await expect(api.models.list()).rejects.toMatchObject({
      message: "still expired",
      status: 401,
    })

    expect(modelRequests).toBe(2)
    expect(refreshRequests).toBe(1)
  })

  it("extracts detail messages into ApiError instances", async () => {
    rewriteTestServer.use(
      http.get("/api/vendors", () =>
        HttpResponse.json({ detail: { message: "Vendor catalog failed" } }, { status: 422 }),
      ),
    )

    try {
      await api.vendors.list()
      throw new Error("api.vendors.list should reject")
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError)
      expect(error).toMatchObject({
        detail: { detail: { message: "Vendor catalog failed" } },
        message: "Vendor catalog failed",
        status: 422,
      })
    }
  })

  it("keeps sidecars and vendor catalog global even when a profile is selected", async () => {
    const observedProfileHeaders: Array<string | null> = []
    setApiProfileId(42)

    rewriteTestServer.use(
      http.get("/api/sidecars", ({ request }) => {
        observedProfileHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json({ items: [] })
      }),
      http.get("/api/config/vendors/export", ({ request }) => {
        observedProfileHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json({ version: 1, bundle_kind: "vendor_catalog", vendors: [] })
      }),
    )

    await api.sidecars.list()
    await api.config.vendors.export()

    expect(observedProfileHeaders).toEqual([null, null])
  })
})
