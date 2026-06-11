import { describe, expect, it } from "vitest"
import {
  buildModelDetailPath,
  buildRequestAuditPath,
  getLegacyRedirectPath,
  legacyRouteRedirects,
  observeSearchSchema,
  prismPathById,
  requestAuditSearchSchema,
  requestLogSearchSchema,
  rewriteRoutePaths,
} from "@/app"
import { resolveProtectedRedirect, resolvePublicRedirect } from "@/app/router/authGates"

const returnLocation = {
  pathname: "/observe/requests",
  search: "?request_id=42",
  hash: "#audit",
}

describe("rewrite route helpers", () => {
  it("keeps the required target route map and typed builders", () => {
    expect(rewriteRoutePaths).toContain("/observe")
    expect(rewriteRoutePaths).toContain("/auth/login")
    expect(rewriteRoutePaths).toContain("/observe/requests/$requestId/audit")
    expect(rewriteRoutePaths).toContain("/sidecars")
    expect(prismPathById["route-ban-policies"]).toBe("/route/ban-policies")
    expect(buildModelDetailPath("model/slash")).toBe("/models/model%2Fslash")
    expect(buildRequestAuditPath(123)).toBe("/observe/requests/123/audit")
  })

  it("maps legacy paths to target routes without changing selected-profile scope semantics", () => {
    expect(legacyRouteRedirects["/dashboard"]).toBe("/observe")
    expect(legacyRouteRedirects["/endpoints"]).toBe("/route/endpoints")
    expect(legacyRouteRedirects["/proxy-api-keys"]).toBe("/control/proxy-keys")
    expect(getLegacyRedirectPath("/request-logs/321/audit")).toBe("/observe/requests/321/audit")
  })

  it("validates and normalizes target route search params", () => {
    expect(observeSearchSchema.parse({ tab: "routing" })).toEqual({ tab: "routing" })
    expect(observeSearchSchema.parse({ tab: "unknown" })).toEqual({ tab: "overview" })
    expect(requestLogSearchSchema.parse({ limit: "300", cursor: "12", request_id: "#bad", status: "error", model: "gpt-test", selected_request_id: "101" })).toMatchObject({
      cursor: 12,
      limit: 300,
      model: "gpt-test",
      request_id: "",
      selected_request_id: "101",
      status: "error",
      time_range: "1h",
    })
    expect(requestAuditSearchSchema.parse({ audit_id: "201", cursor: "page-2" })).toEqual({
      audit_id: "201",
      cursor: "page-2",
    })
  })

  it("resolves protected and public auth redirects with return state", () => {
    expect(resolveProtectedRedirect({ authEnabled: true, authenticated: false, loading: false }, returnLocation)).toEqual({
      to: "/auth/login",
      state: { from: returnLocation },
    })
    expect(resolveProtectedRedirect({ authEnabled: false, authenticated: false, loading: false }, returnLocation)).toBeNull()
    expect(resolvePublicRedirect({ authEnabled: false, authenticated: false, loading: false })).toBe("/observe")
    expect(resolvePublicRedirect({ authEnabled: true, authenticated: true, loading: false })).toBe("/observe")
    expect(resolvePublicRedirect({ authEnabled: true, authenticated: false, loading: false })).toBeNull()
  })
})
