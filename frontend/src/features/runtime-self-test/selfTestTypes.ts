/**
 * Shared runtime self-test types (Proxy Key SPEC §9).
 */

export type SelfTestDirectState =
  | "succeeded"
  | "http_error"
  | "network_error"
  | "cancelled";
export type SelfTestAttributionState =
  | "identified"
  | "none"
  | "unknown"
  | "evidence_pending";
export type SelfTestRoutingState =
  | "resolved"
  | "failed"
  | "not_reached"
  | "evidence_pending";
export type SelfTestExecutionState =
  | "completed"
  | "failed"
  | "not_reached"
  | "evidence_pending";
export type SelfTestPricingState =
  | "priced"
  | "unpriced"
  | "ineligible"
  | "unknown"
  | "evidence_pending";
export type SelfTestTelemetryState =
  | "not_expected"
  | "pending"
  | "ready"
  | "timed_out"
  | "unavailable";

export const INGRESS_REQUEST_ID_HEADER = "X-Prism-Ingress-Request-Id";

export interface RuntimeSelfTestResult {
  ingressRequestId: string | null;
  direct: {
    state: SelfTestDirectState;
    statusCode: number | null;
    safeSummary: string | null;
  };
  credential: {
    authEnforced: boolean | null;
    attributionState: SelfTestAttributionState;
    expectedProxyApiKeyId: number | null;
    observedProxyApiKeyId: number | null;
  };
  routing: {
    state: SelfTestRoutingState;
    requestedModelId: string;
    resolvedModelId: string | null;
  };
  execution: {
    state: SelfTestExecutionState;
    terminalTargetId: number | null;
    endpointId: number | null;
    endpointLabelSnapshot: string | null;
  };
  pricing: {
    state: SelfTestPricingState;
    unpricedReason: string | null;
    costMicros: number | null;
    currency: string | null;
  };
  telemetryState: SelfTestTelemetryState;
}

export interface SelfTestRequestSpec {
  url: string;
  method: "POST";
  headers: Record<string, string>;
  body: string;
}

export type SelfTestEntrySource =
  | "generated_secret"
  | "proxy_key_verify"
  | "model_detail"
  | "endpoint_detail";

export interface SelfTestEntryContext {
  source: SelfTestEntrySource;
  requestedModelId: string;
  /** Raw key from the generated-secret session or an operator paste. */
  proxyKey: string | null;
  /** Present when auth is disabled and the operator chose a no-key test. */
  explicitNoKey: boolean;
  /** Label of the page the test was launched from (Endpoint honesty). */
  launchLabel?: string;
  expectedProxyApiKeyId?: number | null;
}

export const SELF_TEST_POLL_ATTEMPTS = 8;
export const SELF_TEST_POLL_MAX_DELAY_MS = 2000;
export const SELF_TEST_POLL_BASE_DELAY_MS = 250;

export const SELF_TEST_REQUESTS_HANDOFF_PATH =
  "/observe/requests?view=ingress_chains&ingress_request_id=";
