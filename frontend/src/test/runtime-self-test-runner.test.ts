import { http, HttpResponse } from "msw";
import { describe, expect, it, vi, afterEach } from "vitest";
import { rewriteTestServer } from "@/test";
import {
  buildSelfTestResult,
  reconcileSelfTestTelemetry,
  runRuntimeSelfTestDirect,
  SelfTestAbortedError,
} from "@/features/runtime-self-test/selfTestRunner";
import type { SelfTestEntryContext } from "@/features/runtime-self-test/selfTestTypes";
import type { FinalizedSummary } from "@/lib/types/request-logs";
import { INGRESS_REQUEST_ID_HEADER } from "@/features/runtime-self-test/selfTestTypes";
import { __setApiBaseForTest, resetEffectiveBackendOriginCache } from "@/features/runtime-self-test/effectiveOrigin";

const RUNTIME_ORIGIN = "http://backend.local:8000";
const RUNTIME_URL = `${RUNTIME_ORIGIN}/v1/chat/completions`;

function context(overrides: Partial<SelfTestEntryContext> = {}): SelfTestEntryContext {
  return {
    source: "generated_secret",
    requestedModelId: "gpt-5.6-luna",
    proxyKey: "pm-1a2b3c4d5e6f7a8b9c0d1e2f9f3e",
    explicitNoKey: false,
    expectedProxyApiKeyId: 42,
    ...overrides,
  };
}

function finalized(overrides: Partial<FinalizedSummary> = {}): FinalizedSummary {
  return {
    request_log_id: "101", final_status_code: 200, final_result: "completed",
    final_target_model: { id: "gpt-5.6-native", label: "Native" },
    terminal_target: { id: 7, label: "Primary", configured: true, owner_model_id: "gpt-5.6-native" },
    endpoint: { id: 3, label: "Primary OpenAI" },
    final_pricing_status: "priced", final_pricing_evidence_trust: "trusted",
    total_cost_user_currency_micros: 1250, report_currency_symbol: "$",
    ...overrides,
  } as FinalizedSummary;
}

afterEach(() => {
  __setApiBaseForTest(undefined);
  resetEffectiveBackendOriginCache();
  vi.restoreAllMocks();
});

describe("runRuntimeSelfTestDirect", () => {
  it("sends a direct runtime request with family header and captures the ingress ID", async () => {
    __setApiBaseForTest(RUNTIME_ORIGIN + "/");
    let receivedHeaders: Record<string, string> = {};
    rewriteTestServer.use(
      http.post(`${RUNTIME_URL}`, ({ request }) => {
        receivedHeaders = Object.fromEntries(request.headers.entries());
        return HttpResponse.json(
          { id: "resp-1", output: ["ok"] },
          { status: 200, headers: { [INGRESS_REQUEST_ID_HEADER]: "ingress-abc-123" } },
        );
      }),
    );
    const result = await runRuntimeSelfTestDirect(
      {
        url: RUNTIME_URL,
        method: "POST",
        headers: { "Content-Type": "application/json", Authorization: "Bearer pm-1a2b3c4d5e6f7a8b9c0d1e2f9f3e" },
        body: "{}",
      },
      context(),
    );
    expect(result.ingressRequestId).toBe("ingress-abc-123");
    expect(result.statusCode).toBe(200);
    expect(receivedHeaders.authorization).toBe("Bearer pm-1a2b3c4d5e6f7a8b9c0d1e2f9f3e");
  });

  it("omits the credential header for an explicit no-key permissive test", async () => {
    __setApiBaseForTest(RUNTIME_ORIGIN + "/");
    let receivedHeaders: Record<string, string> = {};
    rewriteTestServer.use(
      http.post(RUNTIME_URL, ({ request }) => {
        receivedHeaders = Object.fromEntries(request.headers.entries());
        return HttpResponse.json({ id: "resp-2" }, { status: 200, headers: { [INGRESS_REQUEST_ID_HEADER]: "ingress-2" } });
      }),
    );
    await runRuntimeSelfTestDirect(
      { url: RUNTIME_URL, method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" },
      context({ proxyKey: null, explicitNoKey: true }),
    );
    expect(receivedHeaders.Authorization).toBeUndefined();
  });

  it("keeps the HTTP outcome without retaining raw diagnostic bodies", async () => {
    __setApiBaseForTest(RUNTIME_ORIGIN + "/");
    rewriteTestServer.use(
      http.post(RUNTIME_URL, () =>
        HttpResponse.json({ error: { message: "model not found" } }, { status: 404, headers: { [INGRESS_REQUEST_ID_HEADER]: "ingress-3" } }),
      ),
    );
    const result = await runRuntimeSelfTestDirect(
      { url: RUNTIME_URL, method: "POST", headers: {}, body: "{}" },
      context(),
    );
    expect(result.statusCode).toBe(404);
    expect(result).toEqual({ state: "http_error", statusCode: 404, ingressRequestId: "ingress-3" });
  });
});

it("preserves the response ID when receiving the body fails", async () => {
  vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(new ReadableStream({
    start(controller) { controller.error(new TypeError("private read failure")); },
  }), { headers: { [INGRESS_REQUEST_ID_HEADER]: "body-failed" } }));
  const result = await runRuntimeSelfTestDirect({ url: RUNTIME_URL, method: "POST", headers: {}, body: "{}" }, context());
  expect(result).toEqual({ ingressRequestId: "body-failed", statusCode: 200, state: "response_interrupted" });
});

describe("reconcileSelfTestTelemetry", () => {
  it("polls by exact ingress ID and returns the detail when materialized", async () => {
    let calls = 0;
    rewriteTestServer.use(
      http.get("*/api/stats/requests", ({ request }) => {
        expect(new URL(request.url).searchParams.get("view")).toBe("ingress_chains");
        calls += 1;
        if (calls < 2) {
          return HttpResponse.json({ items: [], total: 0, limit: 5, offset: 0, filter_options: { ingress_models: [], attempt_target_models: [], endpoints: [], clients: [] }, caliber: {}, dataset_coverage: {}, samples: {} });
        }
        return HttpResponse.json({
          items: [{ ingress_request_id: "other-request", finalized_evidence_state: "authoritative", finalized_summary: finalized({request_log_id: "unrelated"}) }, { ingress_request_id: "ingress-abc", finalized_evidence_state: "authoritative", finalized_summary: finalized(), retained_rows: [{ request_log_id: "102", is_winner: false, upstream_status_code: 503 }] }],
          total: 1,
          limit: 5,
          offset: 0,
          filter_options: { ingress_models: [], attempt_target_models: [], endpoints: [], clients: [] },
          caliber: {},
          dataset_coverage: {},
          samples: {},
        });
      }),
      http.get("*/api/stats/requests/101", () =>
        HttpResponse.json({
          summary: { request_log_id: "101", status_code: 200, gateway_status_code: 200, attempt_target_model_id: "gpt-5.6-native" },
          request: { ingress_request_id: "ingress-abc", proxy_api_key_id: 42, proxy_api_key_attribution_state: "identified", proxy_api_key_auth_enforced_at_request: false },
          routing: { terminal_target_id: 7, endpoint_id: 3, endpoint_label: "Primary OpenAI" },
          pricing: { pricing_status: "priced", unpriced_reason: null, total_cost_user_currency_micros: 1250, report_currency_symbol: "$" },
        }),
      ),
    );
    const result = await reconcileSelfTestTelemetry("ingress-abc", undefined);
    expect(result.state).toBe("ready");
    expect(result.detail?.request.proxy_api_key_id).toBe(42);
    expect(calls).toBeGreaterThanOrEqual(2);
  });

  it("stops after the bounded attempt budget instead of polling forever", { timeout: 20000 }, async () => {
    let calls = 0;
    rewriteTestServer.use(
      http.get("*/api/stats/requests", () => {
        calls += 1;
        return HttpResponse.json({ items: [], total: 0, limit: 5, offset: 0, filter_options: { ingress_models: [], attempt_target_models: [], endpoints: [], clients: [] }, caliber: {}, dataset_coverage: {}, samples: {} });
      }),
    );
    const result = await reconcileSelfTestTelemetry("ingress-never", undefined);
    expect(result.state).toBe("timed_out");
    expect(calls).toBe(8);
  });

  it("honors abort and throws SelfTestAbortedError", async () => {
    rewriteTestServer.use(
      http.get("*/api/stats/requests", () => HttpResponse.json({ items: [], total: 0, limit: 5, offset: 0, filter_options: { ingress_models: [], attempt_target_models: [], endpoints: [], clients: [] }, caliber: {}, dataset_coverage: {}, samples: {} })),
    );
    const controller = new AbortController();
    const pending = reconcileSelfTestTelemetry("ingress-abort", controller.signal);
    controller.abort();
    await expect(pending).rejects.toBeInstanceOf(SelfTestAbortedError);
  });
});

describe("buildSelfTestResult four-layer projection", () => {
  const telemetryDetail = {
    summary: { request_log_id: "101", status_code: 200, gateway_status_code: 200, attempt_target_model_id: "gpt-5.6-native" },
    request: { proxy_api_key_id: 42, proxy_api_key_attribution_state: "identified", proxy_api_key_auth_enforced_at_request: false },
    routing: { terminal_target_id: 7, endpoint_id: 3, endpoint_label: "Primary OpenAI" },
    pricing: { pricing_status: "priced", unpriced_reason: null, total_cost_user_currency_micros: 1250, report_currency_symbol: "$" },
    costing: { total_cost_user_currency_micros: 1250, report_currency_symbol: "$" },
  };

  it("projects a fully evidenced success: all four layers", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "ingress-x", statusCode: 200, state: "succeeded" },
      context(),
      { detail: telemetryDetail as never, finalized: finalized(), state: "ready" },
    );
    expect(result.direct.state).toBe("succeeded");
    expect(result.credential.attributionState).toBe("identified");
    expect(result.credential.authEnforced).toBe(false);
    expect(result.credential.observedProxyApiKeyId).toBe(42);
    expect(result.routing.state).toBe("resolved");
    expect(result.routing.resolvedModelId).toBe("gpt-5.6-native");
    expect(result.execution.state).toBe("completed");
    expect(result.execution.endpointLabelSnapshot).toBe("Primary OpenAI");
    expect(result.pricing.state).toBe("priced");
    expect(result.pricing.costMicros).toBe(1250);
    expect(result.telemetryState).toBe("ready");
  });

  it("never turns a successful direct response plus unpriced into failure", () => {
    const unpriced = buildSelfTestResult(
      { ingressRequestId: "ingress-y", statusCode: 200, state: "succeeded" },
      context(),
      {
        detail: {
          ...telemetryDetail,
          pricing: { pricing_status: "unpriced", unpriced_reason: "MISSING_PRICE_DATA", total_cost_user_currency_micros: null },
          costing: { total_cost_user_currency_micros: null, report_currency_symbol: null },
        } as never,
        finalized: finalized({ final_pricing_status: "unpriced", final_unpriced_reason: "MISSING_PRICE_DATA", total_cost_user_currency_micros: null }),
        state: "ready",
      },
    );
    expect(unpriced.direct.state).toBe("succeeded");
    expect(unpriced.execution.state).toBe("completed");
    expect(unpriced.pricing.state).toBe("unpriced");
    expect(unpriced.pricing.unpricedReason).toBe("MISSING_PRICE_DATA");
  });

  it("does not infer where an unauthorized request failed without a record", () => {
    const result = buildSelfTestResult({ ingressRequestId: null, statusCode: 401, state: "http_error" }, context(), null);
    expect(result.credential.attributionState).toBe("evidence_pending");
    expect(result.routing.state).toBe("evidence_pending");
    expect(result.execution.state).toBe("evidence_pending");
    expect(result.telemetryState).toBe("not_expected");
  });

  it("keeps telemetry timeout as evidence_pending without rewriting the direct success", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "ingress-z", statusCode: 200, state: "succeeded" },
      context(),
      { detail: null, state: "timed_out" },
    );
    expect(result.direct.state).toBe("succeeded");
    expect(result.credential.attributionState).toBe("evidence_pending");
    expect(result.routing.state).toBe("evidence_pending");
    expect(result.execution.state).toBe("evidence_pending");
    expect(result.pricing.state).toBe("evidence_pending");
    expect(result.telemetryState).toBe("timed_out");
  });

  it("marks permissive no-key success as none attribution, never identified", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "ingress-nk", statusCode: 200, state: "succeeded" },
      context({ proxyKey: null, explicitNoKey: true }),
      null,
    );
    expect(result.direct.state).toBe("succeeded");
    expect(result.credential.attributionState).toBe("none");
    expect(result.credential.observedProxyApiKeyId).toBeNull();
  });

  it.each(["failed", "client_disconnected"] as const)("uses the finalized %s result despite an HTTP 200 response", (final_result) => {
    const result = buildSelfTestResult(
      { ingressRequestId: "interrupted", statusCode: 200, state: "succeeded" }, context(),
      { detail: telemetryDetail as never, finalized: finalized({ final_result }), state: "ready" },
    );
    expect(result.execution.state).toBe("failed");
  });

  it("never presents untrusted finalized prices as known cost", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "untrusted", statusCode: 200, state: "succeeded" }, context(),
      { detail: telemetryDetail as never, finalized: finalized({ final_pricing_evidence_trust: "legacy_untrusted" }), state: "ready" },
    );
    expect(result.pricing.state).toBe("unknown");
  });

  it("exposes expected vs observed key id for generated-secret runs", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "ingress-e", statusCode: 200, state: "succeeded" },
      context({ expectedProxyApiKeyId: 42 }),
      { detail: telemetryDetail as never, finalized: finalized(), state: "ready" },
    );
    expect(result.credential.expectedProxyApiKeyId).toBe(42);
    expect(result.credential.observedProxyApiKeyId).toBe(42);
  });
});
