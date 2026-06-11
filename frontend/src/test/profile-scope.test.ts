import { QueryClient } from "@tanstack/react-query"
import { http, HttpResponse } from "msw"
import { afterEach, describe, expect, it } from "vitest"
import { api, setApiProfileId } from "@/lib/api"
import {
  getRewriteMutationInvalidationKeys,
  invalidateRewriteMutationScope,
  rewriteQueryKeys,
} from "@/shared"
import { rewriteTestServer } from "@/test"

afterEach(() => {
  setApiProfileId(null)
})

describe("profile-scope api and query contracts", () => {
  it("attaches X-Profile-Id only to profile-scoped API calls", async () => {
    const observedHeaders: Array<string | null> = []
    setApiProfileId(42)

    rewriteTestServer.use(
      http.get("/api/models", ({ request }) => {
        observedHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json([])
      }),
      http.get("/api/settings/costing", ({ request }) => {
        observedHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json({ report_currency_code: "USD", report_currency_symbol: "$" })
      }),
      http.get("/api/profiles", ({ request }) => {
        observedHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json([])
      }),
    )

    await api.models.list()
    await api.settings.costing.get()
    await api.profiles.list()

    expect(observedHeaders).toEqual(["42", "42", null])
  })

  it("puts selected profile IDs in scoped keys and omits them from global keys", () => {
    expect(rewriteQueryKeys.selectedProfile(42).models()).toEqual([
      "rewrite",
      "selected-profile",
      "42",
      "models",
    ])
    expect(rewriteQueryKeys.selectedProfile(7).models()).toEqual([
      "rewrite",
      "selected-profile",
      "7",
      "models",
    ])

    expect(rewriteQueryKeys.global.sidecars()).toEqual(["rewrite", "global", "sidecars"])
    expect(rewriteQueryKeys.global.vendors()).toEqual(["rewrite", "global", "vendors"])
    expect(rewriteQueryKeys.global.sidecars()).not.toContain("42")
  })

  it("invalidates selected-profile cache without touching global cache", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const profileModelsKey = rewriteQueryKeys.selectedProfile(42).models()
    const sidecarsKey = rewriteQueryKeys.global.sidecars()

    queryClient.setQueryData(profileModelsKey, [])
    queryClient.setQueryData(sidecarsKey, { items: [] })

    expect(getRewriteMutationInvalidationKeys({ scope: "selected-profile", profileId: 42 })).toEqual([
      rewriteQueryKeys.selectedProfile(42).all,
    ])

    await invalidateRewriteMutationScope(queryClient, { scope: "selected-profile", profileId: 42 })

    expect(queryClient.getQueryState(profileModelsKey)?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(sidecarsKey)?.isInvalidated).toBe(false)
    queryClient.clear()
  })
})
