import { describe, expect, it } from "vitest"
import {
  buildModelDetailPath,
  buildRequestAuditPath,
  observeSearchSchema,
  prismPathById,
  requestAuditSearchSchema,
  requestLogSearchSchema,
  rewriteRoutePaths,
} from "@/app/index"
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
    expect(prismPathById["route-ban-policies"]).toBe("/route/ban-policies")
    expect(buildModelDetailPath("model/slash")).toBe("/models/model%2Fslash")
    expect(buildRequestAuditPath(123)).toBe("/observe/requests/123/audit")
  })

  it("validates and normalizes target route search params", () => {
    expect(observeSearchSchema.parse({ tab: "routing" })).toEqual({ tab: "overview" })
    expect(observeSearchSchema.parse({ tab: "unknown" })).toEqual({ tab: "overview" })
    expect(requestLogSearchSchema.parse({
      client_rule_id: "123",
      limit: "300",
      cursor: "12",
      request_id: "#bad",
      resolved_target_model_id: "terminal-model",
      status: "success",
      model: "gpt-test",
      selected_request_id: "101",
    })).toMatchObject({
      client_rule_id: "123",
      cursor: 12,
      limit: 300,
      model: "gpt-test",
      resolved_target_model_id: "terminal-model",
      request_id: "",
      selected_request_id: "101",
      status: "success",
      time_range: "24h",
    })
    expect(requestAuditSearchSchema.parse({ audit_id: "201", cursor: "page-2" })).toEqual({
      audit_id: "201",
      cursor: "page-2",
    })
  })

  it("resolves protected and public auth redirects with return state", () => {
    expect(resolveProtectedRedirect({ authEnabled: true, authenticated: false, loading: false }, returnLocation)).toEqual({
      to: "/auth/login",
      search: { redirect: "/observe/requests?request_id=42#audit" },
    })
    expect(resolveProtectedRedirect({ authEnabled: false, authenticated: false, loading: false }, returnLocation)).toBeNull()
    expect(resolvePublicRedirect({ authEnabled: false, authenticated: false, loading: false })).toBe("/observe")
    expect(resolvePublicRedirect({ authEnabled: true, authenticated: true, loading: false })).toBe("/observe")
    expect(resolvePublicRedirect({ authEnabled: true, authenticated: false, loading: false })).toBeNull()
  })
})
