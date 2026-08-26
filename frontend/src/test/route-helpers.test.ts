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
    expect(prismPathById["models"]).toBe("/route/models")
    expect(rewriteRoutePaths).toContain("/observe/routing-health")
    expect(buildModelDetailPath("model/slash")).toBe("/route/models/model%2Fslash")
    expect(buildRequestAuditPath("9007199254740997")).toBe(
      "/observe/requests/9007199254740997/audit",
    )
  })

  it("validates and normalizes target route search params", () => {
    expect(observeSearchSchema.parse({ tab: "routing" })).toMatchObject({ tab: "overview" })
    expect(observeSearchSchema.parse({ tab: "unknown" })).toMatchObject({ tab: "overview" })
    expect(observeSearchSchema.parse({ tab: "events", event_type: "banned", runtime_state: "banned" })).toMatchObject({
      tab: "events",
      event_type: ["banned"],
      runtime_state: ["banned"],
    })
    expect(observeSearchSchema.parse({ tab: "events", event_sort_order: "asc" }).event_sort_order).toBe("asc")
    expect(observeSearchSchema.parse({ tab: "events", preset: "7d" }).preset).toBe("7d")
    expect(observeSearchSchema.parse({ metric: "output_rate" }).metric).toBe("output_rate")
    expect(observeSearchSchema.parse({ metric: "cache_read_share" }).metric).toBe("cache_read_share")
    expect(observeSearchSchema.parse({ metric: "unknown" }).metric).toBe("requests")
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
    const anonymous = { phase: { kind: "ANONYMOUS", session_epoch: 1 } as const, authEnabled: true, authenticated: false, loading: false }
    const disabled = { phase: { kind: "AUTH_DISABLED", session_epoch: 1 } as const, authEnabled: false, authenticated: false, loading: false }
    const authenticatedState = { phase: { kind: "AUTHENTICATED", session_epoch: 1, subject_key: "sub:1", username: "admin" } as const, authEnabled: true, authenticated: true, loading: false }
    expect(resolveProtectedRedirect(anonymous, returnLocation)).toEqual({
      to: "/auth/login",
      search: { redirect: "/observe/requests?request_id=42#audit" },
    })
    expect(resolveProtectedRedirect(disabled, returnLocation)).toBeNull()
    expect(resolvePublicRedirect(disabled)).toBeNull()
    expect(resolvePublicRedirect(authenticatedState)).toBe("/observe")
    expect(resolvePublicRedirect(anonymous)).toBeNull()
  })
})
