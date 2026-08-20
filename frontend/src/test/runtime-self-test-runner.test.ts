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
import { INGRESS_REQUEST_ID_HEADER } from "@/features/runtime-self-test/selfTestTypes";
import {
  __setApiBaseForTest,
  resetEffectiveBackendOriginCache,
} from "@/features/runtime-self-test/effectiveOrigin";

const RUNTIME_ORIGIN = "http://backend.local:8000";
const RUNTIME_URL = `${RUNTIME_ORIGIN}/v1/chat/completions`;
const fixtureProxyValue = "test-proxy-value";

function context(
  overrides: Partial<SelfTestEntryContext> = {},
): SelfTestEntryContext {
  return {
    source: "generated_secret",
    requestedModelId: "gpt-5.6-luna",
    proxyKey: fixtureProxyValue,
    explicitNoKey: false,
    expectedProxyApiKeyId: 42,
    ...overrides,
  };
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
          {
            status: 200,
            headers: { [INGRESS_REQUEST_ID_HEADER]: "ingress-abc-123" },
          },
        );
      }),
    );
    const result = await runRuntimeSelfTestDirect(
      {
        url: RUNTIME_URL,
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${fixtureProxyValue}`,
        },
        body: "{}",
      },
      context(),
    );
    expect(result.ingressRequestId).toBe("ingress-abc-123");
    expect(result.statusCode).toBe(200);
    expect(receivedHeaders.authorization).toBe(`Bearer ${fixtureProxyValue}`);
  });

  it("omits the credential header for an explicit no-key permissive test", async () => {
    __setApiBaseForTest(RUNTIME_ORIGIN + "/");
    let receivedHeaders: Record<string, string> = {};
    rewriteTestServer.use(
      http.post(RUNTIME_URL, ({ request }) => {
        receivedHeaders = Object.fromEntries(request.headers.entries());
        return HttpResponse.json(
          { id: "resp-2" },
          {
            status: 200,
            headers: { [INGRESS_REQUEST_ID_HEADER]: "ingress-2" },
          },
        );
      }),
    );
    await runRuntimeSelfTestDirect(
      {
        url: RUNTIME_URL,
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      },
      context({ proxyKey: null, explicitNoKey: true }),
    );
    expect(receivedHeaders.Authorization).toBeUndefined();
  });

  it("summarizes error bodies safely and bounds the summary size", async () => {
    __setApiBaseForTest(RUNTIME_ORIGIN + "/");
    rewriteTestServer.use(
      http.post(RUNTIME_URL, () =>
        HttpResponse.json(
          { error: { message: "model not found" } },
          {
            status: 404,
            headers: { [INGRESS_REQUEST_ID_HEADER]: "ingress-3" },
          },
        ),
      ),
    );
    const result = await runRuntimeSelfTestDirect(
      { url: RUNTIME_URL, method: "POST", headers: {}, body: "{}" },
      context(),
    );
    expect(result.statusCode).toBe(404);
    expect(result.safeSummary).toBe("model not found");
  });
});

describe("reconcileSelfTestTelemetry", () => {
  it("polls by exact ingress ID and returns the detail when materialized", async () => {
    let calls = 0;
    rewriteTestServer.use(
      http.get("*/api/stats/requests", () => {
        calls += 1;
        if (calls < 2) {
          return HttpResponse.json({
            items: [],
            total: 0,
            limit: 5,
            offset: 0,
            filter_options: {},
          });
        }
        return HttpResponse.json({
          items: [{ request_log_id: "101" }],
          total: 1,
          limit: 5,
          offset: 0,
          filter_options: {},
        });
      }),
      http.get("*/api/stats/requests/101", () =>
        HttpResponse.json({
          summary: {
            id: 101,
            status_code: 200,
            gateway_status_code: 200,
            resolved_target_model_id: "gpt-5.6-native",
          },
          request: {
            proxy_api_key_id: 42,
            proxy_api_key_attribution_state: "identified",
            proxy_api_key_auth_enforced_at_request: false,
          },
          routing: {
            terminal_target_id: 7,
            endpoint_id: 3,
            endpoint_label: "Primary OpenAI",
          },
          pricing: {
            pricing_status: "priced",
            unpriced_reason: null,
            total_cost_user_currency_micros: 1250,
            report_currency_symbol: "$",
          },
        }),
      ),
    );
    const result = await reconcileSelfTestTelemetry("ingress-abc", undefined);
    expect(result.state).toBe("ready");
    expect(result.detail?.request.proxy_api_key_id).toBe(42);
    expect(calls).toBeGreaterThanOrEqual(2);
  });

  it("stops after the bounded attempt budget instead of polling forever", {
    timeout: 20000,
  }, async () => {
    let calls = 0;
    rewriteTestServer.use(
      http.get("*/api/stats/requests", () => {
        calls += 1;
        return HttpResponse.json({
          items: [],
          total: 0,
          limit: 5,
          offset: 0,
          filter_options: {},
        });
      }),
    );
    const result = await reconcileSelfTestTelemetry("ingress-never", undefined);
    expect(result.state).toBe("timed_out");
    expect(calls).toBe(8);
  });

  it("honors abort and throws SelfTestAbortedError", async () => {
    rewriteTestServer.use(
      http.get("*/api/stats/requests", () =>
        HttpResponse.json({
          items: [],
          total: 0,
          limit: 5,
          offset: 0,
          filter_options: {},
        }),
      ),
    );
    const controller = new AbortController();
    const pending = reconcileSelfTestTelemetry(
      "ingress-abort",
      controller.signal,
    );
    controller.abort();
    await expect(pending).rejects.toBeInstanceOf(SelfTestAbortedError);
  });
});

describe("buildSelfTestResult four-layer projection", () => {
  const telemetryDetail = {
    summary: {
      id: 101,
      status_code: 200,
      gateway_status_code: 200,
      resolved_target_model_id: "gpt-5.6-native",
    },
    request: {
      proxy_api_key_id: 42,
      proxy_api_key_attribution_state: "identified",
      proxy_api_key_auth_enforced_at_request: false,
    },
    routing: {
      terminal_target_id: 7,
      endpoint_id: 3,
      endpoint_label: "Primary OpenAI",
    },
    pricing: {
      pricing_status: "priced",
      unpriced_reason: null,
      total_cost_user_currency_micros: 1250,
      report_currency_symbol: "$",
    },
    costing: {
      total_cost_user_currency_micros: 1250,
      report_currency_symbol: "$",
    },
  };

  it("projects a fully evidenced success: all four layers", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "ingress-x", statusCode: 200, safeSummary: null },
      context(),
      { detail: telemetryDetail as never, state: "ready" },
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
      { ingressRequestId: "ingress-y", statusCode: 200, safeSummary: null },
      context(),
      {
        detail: {
          ...telemetryDetail,
          pricing: {
            pricing_status: "unpriced",
            unpriced_reason: "MISSING_PRICE_DATA",
            total_cost_user_currency_micros: null,
          },
          costing: {
            total_cost_user_currency_micros: null,
            report_currency_symbol: null,
          },
        } as never,
        state: "ready",
      },
    );
    expect(unpriced.direct.state).toBe("succeeded");
    expect(unpriced.execution.state).toBe("completed");
    expect(unpriced.pricing.state).toBe("unpriced");
    expect(unpriced.pricing.unpricedReason).toBe("MISSING_PRICE_DATA");
  });

  it("maps 401 to credential failure with routing/execution not reached", () => {
    const result = buildSelfTestResult(
      {
        ingressRequestId: null,
        statusCode: 401,
        safeSummary: "Proxy API key required",
      },
      context(),
      null,
    );
    expect(result.credential.attributionState).toBe("none");
    expect(result.routing.state).toBe("not_reached");
    expect(result.execution.state).toBe("not_reached");
    expect(result.telemetryState).toBe("not_expected");
  });

  it("keeps telemetry timeout as evidence_pending without rewriting the direct success", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "ingress-z", statusCode: 200, safeSummary: null },
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
      { ingressRequestId: "ingress-nk", statusCode: 200, safeSummary: null },
      context({ proxyKey: null, explicitNoKey: true }),
      null,
    );
    expect(result.direct.state).toBe("succeeded");
    expect(result.credential.attributionState).toBe("none");
    expect(result.credential.observedProxyApiKeyId).toBeNull();
  });

  it("exposes expected vs observed key id for generated-secret runs", () => {
    const result = buildSelfTestResult(
      { ingressRequestId: "ingress-e", statusCode: 200, safeSummary: null },
      context({ expectedProxyApiKeyId: 42 }),
      { detail: telemetryDetail as never, state: "ready" },
    );
    expect(result.credential.expectedProxyApiKeyId).toBe(42);
    expect(result.credential.observedProxyApiKeyId).toBe(42);
  });
});
