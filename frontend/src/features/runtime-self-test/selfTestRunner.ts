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
import type { FinalizedSummary, RequestLogDetail } from "@/lib/types/request-logs";
import {
  INGRESS_REQUEST_ID_HEADER,
  SELF_TEST_POLL_ATTEMPTS,
  type RuntimeSelfTestResult,
  type SelfTestEntryContext,
  type SelfTestRequestSpec,
  type SelfTestDirectResponse,
} from "./selfTestTypes";

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
): Promise<SelfTestDirectResponse> {
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
  const direct: SelfTestDirectResponse = {
    ingressRequestId: ingressRequestId && ingressRequestId.trim() !== "" ? ingressRequestId.trim() : null,
    statusCode: response.status,
    state: response.ok ? "succeeded" : "http_error",
  };
  // Finish receiving the response without retaining diagnostic bodies. A read
  // failure must preserve the already received ID for exact record recovery.
  const reader = response.body?.getReader();
  try {
    if (reader) while (!(await reader.read()).done) { /* discard body chunks */ }
  } catch (error) {
    if (signal?.aborted) throw error;
    return { ...direct, state: "response_interrupted" };
  } finally {
    reader?.releaseLock();
  }
  return direct;
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

export type TelemetryReconciliationResult =
  | { state: "ready"; detail: RequestLogDetail; finalized: FinalizedSummary }
  | { state: "timed_out" | "not_expected"; detail: null };

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
  try {
    for (let attempt = 0; attempt < SELF_TEST_POLL_ATTEMPTS; attempt += 1) {
      if (signal?.aborted) {
        throw new SelfTestAbortedError();
      }
      const response = await api.stats.chains({
        view: "ingress_chains",
        ingress_request_id: ingressRequestId,
        chain_limit: 1,
        chain_row_limit: 1,
      }, signal);
      const item = response.items.find((candidate) => candidate.ingress_request_id === ingressRequestId);
      const finalized = item?.finalized_evidence_state === "authoritative" ? item.finalized_summary : null;
      if (finalized?.request_log_id) {
        const detail = await api.stats.requestDetail(finalized.request_log_id, signal);
        if (detail.request.ingress_request_id === ingressRequestId && detail.summary.request_log_id === finalized.request_log_id) {
          return { detail, finalized, state: "ready" };
        }
      }
      if (attempt < delays.length) {
        await delay(delays[attempt], signal);
      }
    }
  } catch (error) {
    if (signal?.aborted) throw new SelfTestAbortedError();
    throw error;
  }
  return { detail: null, state: "timed_out" };
}

/**
 * Projects the four-layer result from the direct HTTP outcome and the
 * telemetry evidence. No aggregate green "connected" state is allowed when a
 * layer is unknown or failed.
 */
export function buildSelfTestResult(
  direct: SelfTestDirectResponse,
  context: SelfTestEntryContext,
  telemetry: TelemetryReconciliationResult | null,
): RuntimeSelfTestResult {
  const credential: RuntimeSelfTestResult["credential"] = {
    authEnforced: null,
    attributionState: "evidence_pending",
    expectedProxyApiKeyId: context.expectedProxyApiKeyId ?? null,
    observedProxyApiKeyId: null,
  };

  const detail = telemetry?.state === "ready" ? telemetry.detail : null;
  const finalized = telemetry?.state === "ready" ? telemetry.finalized : null;
  const telemetryState: RuntimeSelfTestResult["telemetryState"] =
    telemetry === null ? direct.ingressRequestId ? "pending" : "not_expected" : telemetry.state === "ready" ? "ready" : "timed_out";

  if (detail) {
    credential.observedProxyApiKeyId = detail.request.proxy_api_key_id ?? null;
    const attribution = detail.request.proxy_api_key_attribution_state;
    credential.attributionState =
      attribution === "identified" || attribution === "none" || attribution === "unknown" ? attribution : "unknown";
    if (detail.request.proxy_api_key_auth_enforced_at_request !== null && detail.request.proxy_api_key_auth_enforced_at_request !== undefined) {
      credential.authEnforced = detail.request.proxy_api_key_auth_enforced_at_request;
    }
  } else if (context.explicitNoKey || !context.proxyKey) {
    credential.attributionState = "none";
  }

  const routing: RuntimeSelfTestResult["routing"] = {
    state: finalized ? finalized.final_target_model ? "resolved" : "failed" : "evidence_pending",
    requestedModelId: context.requestedModelId,
    resolvedModelId: finalized?.final_target_model?.id ?? null,
  };

  const execution: RuntimeSelfTestResult["execution"] = {
    state: finalized?.final_result === "completed" ? "completed"
      : finalized?.final_result === "failed" || finalized?.final_result === "client_disconnected" ? "failed" : "evidence_pending",
    terminalTargetId: finalized?.terminal_target?.id ?? null,
    endpointId: finalized?.endpoint?.id ?? null,
    endpointLabelSnapshot: finalized?.endpoint?.label ?? null,
  };

  const pricing: RuntimeSelfTestResult["pricing"] = {
    state: finalized
      ? finalized.final_pricing_status === "priced" && finalized.final_pricing_evidence_trust === "trusted"
        ? "priced"
        : finalized.final_pricing_status === "unpriced"
          ? "unpriced"
          : finalized.final_pricing_status === "ineligible" ? "ineligible" : "unknown"
      : "evidence_pending",
    unpricedReason: finalized?.final_unpriced_reason ?? null,
    costMicros: finalized?.total_cost_user_currency_micros ?? null,
    currency: finalized?.report_currency_symbol || finalized?.report_currency_code || null,
  };

  return {
    ingressRequestId: direct.ingressRequestId,
    direct: {
      state: direct.state,
      statusCode: direct.statusCode,
    },
    credential,
    routing,
    execution,
    pricing,
    telemetryState,
  };
}
