import { http, HttpResponse } from "msw"
import { afterEach, describe, expect, it } from "vitest"
import { setApiProfileId } from "@/lib/api"
import { isProfileScopedManagementRoute } from "@/lib/api/profileScope"
import { rewriteQueryKeys } from "@/shared"
import { rewriteTestServer } from "@/test"

afterEach(() => {
  setApiProfileId(null)
})

describe("runtime-bypass contracts", () => {
  it("keeps runtime routes out of management profile-scope matching", () => {
    expect(isProfileScopedManagementRoute("/v1/chat/completions")).toBe(false)
    expect(isProfileScopedManagementRoute("/v1/responses")).toBe(false)
    expect(isProfileScopedManagementRoute("/v1beta/models/gemini:generateContent")).toBe(false)
    expect(isProfileScopedManagementRoute("/api/models")).toBe(true)
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

  it("bypasses the management client and selected-profile header for runtime fetches", async () => {
    const observedProfileHeaders: Array<string | null> = []
    setApiProfileId(42)

    rewriteTestServer.use(
      http.post("/v1/chat/completions", ({ request }) => {
        observedProfileHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json({ id: "chatcmpl-test", choices: [] })
      }),
      http.post("/v1beta/models/gemini:generateContent", ({ request }) => {
        observedProfileHeaders.push(request.headers.get("X-Profile-Id"))
        return HttpResponse.json({ candidates: [] })
      }),
    )

    await fetch("/v1/chat/completions", { method: "POST" })
    await fetch("/v1beta/models/gemini:generateContent", { method: "POST" })

    expect(observedProfileHeaders).toEqual([null, null])
  })
})
