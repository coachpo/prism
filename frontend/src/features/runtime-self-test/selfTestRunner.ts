/**
 * Shared runtime self-test runner (Proxy Key SPEC §9.2–§9.4).
 *
 * The browser sends one direct fetch to the exact runtime operation URL:
 * no management proxy route, no target pin, no bypass query parameter. The
 * response header carries the server-generated ingress ID; telemetry
 * reconciliation polls the ordinary request lookup by that exact ID with a
 * bounded backoff and offers an explicit recheck instead of polling forever.
 */

import { api } from "@/lib/api";
import type { RequestLogDetailV2 } from "@/lib/types/request-logs-v2";
import {
  INGRESS_REQUEST_ID_HEADER,
  SELF_TEST_POLL_ATTEMPTS,
  type RuntimeSelfTestResult,
  type SelfTestEntryContext,
  type SelfTestRequestSpec,
} from "./selfTestTypes";

const SAFE_SUMMARY_MAX_BYTES = 64 * 1024;

function truncateSafeSummary(raw: string): string {
  const encoder = new TextEncoder();
  const bytes = encoder.encode(raw);
  if (bytes.length <= SAFE_SUMMARY_MAX_BYTES) {
    return raw;
  }
  const truncated = bytes.subarray(0, SAFE_SUMMARY_MAX_BYTES);
  return new TextDecoder().decode(truncated) + "\n…[truncated]";
}

function summarizeBody(raw: string): string {
  const trimmed = raw.trim();
  if (trimmed === "") {
    return "";
  }
  try {
    const parsed = JSON.parse(trimmed) as Record<string, unknown>;
    const error = parsed.error as Record<string, unknown> | undefined;
    const message = parsed.message ?? error?.message ?? parsed.detail;
    if (typeof message === "string" && message.trim() !== "") {
      return message;
    }
  } catch {
    // Not JSON: fall through to raw bounded text.
  }
  return truncateSafeSummary(trimmed);
}

export class SelfTestAbortedError extends Error {
  constructor() {
    super("Self-test wait cancelled");
    this.name = "SelfTestAbortedError";
  }
}

export async function runRuntimeSelfTestDirect(
  spec: SelfTestRequestSpec,
  context: SelfTestEntryContext,
  signal?: AbortSignal,
): Promise<{ ingressRequestId: string | null; statusCode: number | null; safeSummary: string | null }> {
  const headers: Record<string, string> = { ...spec.headers };
  if (context.explicitNoKey || !context.proxyKey) {
    // No-key permissive test: omit the credential header entirely rather than
    // sending an empty value. The curl builder already carries the family
    // credential when a key is present; it is never re-injected here.
    delete headers.Authorization;
    delete headers["X-API-Key"];
    delete headers["X-Goog-Api-Key"];
  }
  const response = await fetch(spec.url, {
    method: "POST",
    headers,
    body: spec.body,
    signal,
    cache: "no-store",
  });
  const ingressRequestId = response.headers.get(INGRESS_REQUEST_ID_HEADER);
  const rawBody = await response.text();
  return {
    ingressRequestId: ingressRequestId && ingressRequestId.trim() !== "" ? ingressRequestId.trim() : null,
    statusCode: response.status,
    safeSummary: summarizeBody(rawBody) || null,
  };
}

export function delay(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new SelfTestAbortedError());
      return;
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(new SelfTestAbortedError());
    };
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

export interface TelemetryReconciliationResult {
  detail: RequestLogDetailV2 | null;
  state: "ready" | "timed_out" | "not_expected";
}

/**
 * Polls the ordinary request lookup by exact ingress ID with a bounded
 * backoff (at most SELF_TEST_POLL_ATTEMPTS within ~10s, exponential delay
 * capped at SELF_TEST_POLL_MAX_DELAY_MS). A timeout only downgrades evidence
 * state; it never rewrites a successful direct provider response as failure.
 */
export async function reconcileSelfTestTelemetry(
  ingressRequestId: string,
  signal?: AbortSignal,
): Promise<TelemetryReconciliationResult> {
  // Exponential delay capped at 2s: 8 attempts stay within the ~10s budget
  // (250+500+1000+1500+2000+2000+2000 = 9250ms of waiting).
  const delays = [250, 500, 1000, 1500, 2000, 2000, 2000];
  for (let attempt = 0; attempt < SELF_TEST_POLL_ATTEMPTS; attempt += 1) {
    if (signal?.aborted) {
      throw new SelfTestAbortedError();
    }
    const response = await api.stats.requests({
      ingress_request_id: ingressRequestId,
      limit: 5,
      offset: 0,
    });
    const item = response.items[0] ?? null;
    if (item) {
      const detail = await api.stats.requestDetail(Number(item.request_log_id));
      return { detail, state: "ready" };
    }
    if (attempt < delays.length) {
      await delay(delays[attempt], signal);
    }
  }
  return { detail: null, state: "timed_out" };
}

/**
 * Projects the four-layer result from the direct HTTP outcome and the
 * telemetry evidence. No aggregate green "connected" state is allowed when a
 * layer is unknown or failed.
 */
export function buildSelfTestResult(
  direct: { ingressRequestId: string | null; statusCode: number | null; safeSummary: string | null },
  context: SelfTestEntryContext,
  telemetry: TelemetryReconciliationResult | null,
): RuntimeSelfTestResult {
  const directState =
    direct.statusCode === null ? "network_error" : direct.statusCode >= 200 && direct.statusCode < 300 ? "succeeded" : "http_error";

  const credential: RuntimeSelfTestResult["credential"] = {
    authEnforced: null,
    attributionState: "evidence_pending",
    expectedProxyApiKeyId: context.expectedProxyApiKeyId ?? null,
    observedProxyApiKeyId: null,
  };

  const detail = telemetry?.state === "ready" ? telemetry.detail : null;
  const telemetryState: RuntimeSelfTestResult["telemetryState"] =
    telemetry === null ? "not_expected" : telemetry.state === "ready" ? "ready" : "timed_out";

  if (detail) {
    credential.observedProxyApiKeyId = detail.request.proxy_api_key_id ?? null;
    const attribution = detail.request.proxy_api_key_attribution_state;
    credential.attributionState =
      attribution === "identified" || attribution === "none" || attribution === "unknown" ? attribution : "unknown";
    if (detail.request.proxy_api_key_auth_enforced_at_request !== null && detail.request.proxy_api_key_auth_enforced_at_request !== undefined) {
      credential.authEnforced = detail.request.proxy_api_key_auth_enforced_at_request;
    }
  } else if (direct.statusCode === 401) {
    credential.attributionState = "none";
  } else if (direct.statusCode !== null && direct.statusCode >= 200 && direct.statusCode < 300 && !context.proxyKey) {
    // Permissive no-key run: execution may have succeeded, but attribution
    // evidence stays pending/absent; never claim identified.
    credential.attributionState = "none";
  }

  const routing: RuntimeSelfTestResult["routing"] = {
    state: detail ? "resolved" : direct.statusCode === 401 ? "not_reached" : "evidence_pending",
    requestedModelId: context.requestedModelId,
    resolvedModelId: detail?.summary.resolved_target_model_id ?? null,
  };

  const detailStatus = detail
    ? detail.summary.gateway_status_code ?? detail.summary.upstream_status_code ?? detail.summary.legacy_status_code
    : null;

  const execution: RuntimeSelfTestResult["execution"] = {
    state: detail
      ? detailStatus !== null && detailStatus >= 200 && detailStatus < 300
        ? "completed"
        : "failed"
      : direct.statusCode === 401
        ? "not_reached"
        : "evidence_pending",
    terminalTargetId: detail?.routing.terminal_target_id ?? null,
    endpointId: detail?.routing.endpoint_id ?? null,
    endpointLabelSnapshot: detail?.routing.endpoint_label ?? null,
  };

  const pricing: RuntimeSelfTestResult["pricing"] = {
    state: detail
      ? detail.pricing.pricing_status === "priced"
        ? "priced"
        : detail.pricing.pricing_status === "unpriced"
          ? "unpriced"
          : "unknown"
      : "evidence_pending",
    unpricedReason: detail?.pricing.unpriced_reason ?? null,
    costMicros: detail?.pricing.total_cost_user_currency_micros ?? null,
    currency: detail?.pricing.report_currency_symbol ?? null,
  };

  return {
    ingressRequestId: direct.ingressRequestId,
    direct: {
      state: directState,
      statusCode: direct.statusCode,
      safeSummary: direct.safeSummary,
    },
    credential,
    routing,
    execution,
    pricing,
    telemetryState,
  };
}
