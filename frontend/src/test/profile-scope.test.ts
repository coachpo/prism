import { QueryClient } from "@tanstack/react-query"
import { http, HttpResponse } from "msw"
import { describe, expect, it } from "vitest"
import { api } from "@/lib/api"
import {
  getRewriteMutationInvalidationKeys,
  invalidateRewriteMutationScope,
  rewriteQueryKeys,
} from "@/shared"
import { rewriteTestServer } from "@/test"

describe("profile-scope api and query contracts", () => {
  it("attaches X-Profile-Id only to profile-scoped API calls", async () => {
    const observedHeaders: Array<string | null> = []

    rewriteTestServer.use(
      http.get("/api/models", ({ request }) => {
        observedHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json([])
      }),
      http.get("/api/settings/costing", ({ request }) => {
        observedHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json({ report_currency_code: "USD", report_currency_symbol: "$" })
      }),
      http.get("/api/auth/status", ({ request }) => {
        observedHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json({ auth_enabled: false, authenticated: true })
      }),
    )

    await api.models.list()
    await api.settings.costing.get()
    await api.auth.status()

    expect(observedHeaders).toEqual(["1", "1", null])
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

    expect(rewriteQueryKeys.global.settingsAuth()).toEqual(["rewrite", "global", "settings", "auth"])
    expect(rewriteQueryKeys.global.settingsAuth()).not.toContain("42")
  })

  it("invalidates selected-profile cache without touching global cache", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const profileModelsKey = rewriteQueryKeys.selectedProfile(42).models()
    const settingsAuthKey = rewriteQueryKeys.global.settingsAuth()

    queryClient.setQueryData(profileModelsKey, [])
    queryClient.setQueryData(settingsAuthKey, [])

    expect(getRewriteMutationInvalidationKeys({ scope: "selected-profile", profileId: 42 })).toEqual([
      rewriteQueryKeys.selectedProfile(42).all,
    ])

    await invalidateRewriteMutationScope(queryClient, { scope: "selected-profile", profileId: 42 })

    expect(queryClient.getQueryState(profileModelsKey)?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(settingsAuthKey)?.isInvalidated).toBe(false)
    queryClient.clear()
  })
})
