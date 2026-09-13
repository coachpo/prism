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

describe("api profile scope and query-key contracts", () => {
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

  it("puts pinned profile IDs in scoped keys and omits them from global keys", () => {
    expect(rewriteQueryKeys.selectedProfile(1).models()).toEqual([
      "rewrite",
      "selected-profile",
      "1",
      "models",
    ])

    expect(rewriteQueryKeys.global.settingsAuth()).toEqual(["rewrite", "global", "settings", "auth"])
  })

  it("uses runtime-bypass query keys that do not include selected-profile scope", () => {
    expect(rewriteQueryKeys.runtimeBypass.operation("/v1/chat/completions")).toEqual([
      "rewrite",
      "runtime-bypass",
      "operation",
      "/v1/chat/completions",
    ])
    expect(rewriteQueryKeys.runtimeBypass.operation("/v1beta/models/gemini:generateContent")).not.toContain(
      "selected-profile",
    )
  })

  it("invalidates Default-profile cache without touching global cache", async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const profileModelsKey = rewriteQueryKeys.selectedProfile(1).models()
    const settingsAuthKey = rewriteQueryKeys.global.settingsAuth()

    queryClient.setQueryData(profileModelsKey, [])
    queryClient.setQueryData(settingsAuthKey, [])

    expect(getRewriteMutationInvalidationKeys({ scope: "selected-profile", profileId: 1 })).toEqual([
      rewriteQueryKeys.selectedProfile(1).all,
    ])

    await invalidateRewriteMutationScope(queryClient, { scope: "selected-profile", profileId: 1 })

    expect(queryClient.getQueryState(profileModelsKey)?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(settingsAuthKey)?.isInvalidated).toBe(false)
    queryClient.clear()
  })
})
