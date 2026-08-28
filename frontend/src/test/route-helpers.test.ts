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
import { modelsListSearchSchema } from "@/app/router/rewriteRoutes"
import { groupBelongsToScope } from "@/features/observe/observeSearch"

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
    expect(prismPathById["model-export"]).toBe("/route/models/export")
    expect(rewriteRoutePaths).toContain("/route/models/export")
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
    expect(observeSearchSchema.parse({ scope: "final_execution", group_by: "final_target_model" })).toMatchObject({
      scope: "final_execution",
      group_by: "final_target_model",
    })
    expect(observeSearchSchema.parse({ scope: "route_attempt", group_by: "attempt_target_model" })).toMatchObject({
      scope: "route_attempt",
      group_by: "attempt_target_model",
    })
    expect(observeSearchSchema.parse({ scope: "final_execution", group_by: "api_family" })).toMatchObject({
      scope: "final_execution",
      group_by: "api_family",
    })
    expect(observeSearchSchema.parse({ scope: "unknown", group_by: "model" })).toMatchObject({
      scope: "ingress",
      group_by: "none",
    })
    expect(modelsListSearchSchema.parse({ scope: "final_execution" })).toEqual({ scope: "final_execution" })
    expect(modelsListSearchSchema.parse({ scope: "route_attempt" })).toEqual({ scope: "route_attempt" })
    expect(requestLogSearchSchema.parse({
      client_rule_id: "123",
      limit: "300",
      cursor: "12",
      request_id: "#bad",
      attempt_target_model_id: "terminal-model",
      status: "success",
      ingress_model_id: "gpt-test",
      selected_request_id: "101",
    })).toMatchObject({
      client_rule_id: "123",
      cursor: 12,
      limit: 300,
      ingress_model_id: "gpt-test",
      attempt_target_model_id: "terminal-model",
      request_id: "",
      selected_request_id: "101",
      status: "success",
      time_range: "24h",
    })
    expect(requestLogSearchSchema.parse({
      request_id: "0",
      selected_request_id: "9223372036854775807",
    })).toMatchObject({
      request_id: "",
      selected_request_id: "9223372036854775807",
    })
    expect(requestLogSearchSchema.parse({
      request_id: "9223372036854775808",
    }).request_id).toBe("")
    expect(requestLogSearchSchema.parse({
      query_context: "signed-context",
      confirmed_failover: "true",
      final_result: "failed,client_disconnected",
      outcome_detail: "http_error,stream_error",
      final_status_code: "429,503",
      final_stream_outcome: "stream_error,client_disconnected",
      final_stream_error_kind: "__null__,protocol_error",
      final_exclude: "stream_error_kind,__null__,protocol_error",
      final_target_model_id: "winner-a,__null__",
      final_endpoint_id: "7,__null__,9",
      final_terminal_target_id: "11,12,__null__",
      final_pricing_status: "unpriced",
      final_unpriced_reason: "MISSING_PRICE_DATA,STREAM_USAGE_UNAVAILABLE",
      reporting_currency_epoch: "3",
      cost_segment_key: "e.3",
      api_family: "openai,__null__",
      row_kind: "upstream",
      attempt_trigger: "initial,failover,__null__",
      attempt_result: "http_error,transport_error,__null__",
    })).toMatchObject({
      query_context: "signed-context",
      confirmed_failover: "true",
      final_result: "failed,client_disconnected",
      outcome_detail: "http_error,stream_error",
      final_status_code: "429,503",
      final_stream_outcome: "stream_error,client_disconnected",
      final_stream_error_kind: "__null__,protocol_error",
      final_exclude: "stream_error_kind,__null__,protocol_error",
      final_target_model_id: "winner-a,__null__",
      final_endpoint_id: "7,__null__,9",
      final_terminal_target_id: "11,12,__null__",
      final_pricing_status: "unpriced",
      final_unpriced_reason: "MISSING_PRICE_DATA,STREAM_USAGE_UNAVAILABLE",
      reporting_currency_epoch: "3",
      cost_segment_key: "e.3",
      api_family: "openai,__null__",
      row_kind: "upstream",
      attempt_trigger: "initial,failover,__null__",
      attempt_result: "http_error,transport_error,__null__",
    })
    expect(requestAuditSearchSchema.parse({ audit_id: "201", cursor: "page-2" })).toEqual({
      audit_id: "201",
      cursor: "page-2",
    })
  })

  it.each([
    ["ingress_model", "ingress", true],
    ["final_target_model", "final_execution", true],
    ["attempt_target_model", "route_attempt", true],
    ["attempt_trigger", "route_attempt", true],
    ["attempt_result", "route_attempt", true],
    ["api_family", "ingress", true],
    ["api_family", "final_execution", true],
    ["api_family", "route_attempt", true],
    ["terminal_target", "final_execution", true],
    ["terminal_target", "route_attempt", true],
    ["final_target_model", "ingress", false],
    ["attempt_target_model", "final_execution", false],
  ] as const)("maps %s to %s with allowed=%s", (groupBy, scope, allowed) => {
    expect(groupBelongsToScope(groupBy, scope)).toBe(allowed)
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
