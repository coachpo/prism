import type { SessionResponse } from "../types";
import { getStaticMessages } from "@/i18n/staticMessages";
import { isProfileScopedManagementRoute } from "./profileScope";
import { authSessionCoordinator } from "@/context/auth/coordinatorInstance";
import { isAuthExemptPath } from "@/context/auth/authExempt";
import { AuthPhaseChangedError, StaleSessionEpochError } from "@/context/auth/sessionCoordinator";

const rawApiBase = import.meta.env.VITE_API_BASE;
const API_BASE =
  typeof rawApiBase === "string" && rawApiBase.trim().length > 0
    ? rawApiBase.trim().replace(/\/+$/, "")
    : "";

// ponytail: profile pinned to Default(1).
const currentProfileId = 1;

export function getApiProfileId(): number {
  return currentProfileId;
}

export class ApiError extends Error {
  readonly status: number;
  readonly detail: unknown;
  readonly code?: string;
  readonly details?: unknown;
  /** Parsed Retry-After (seconds) when the server sent the header. */
  readonly retryAfterMs: number | null;

  constructor(message: string, status: number, detail: unknown, retryAfterMs: number | null = null, code?: string, details?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.detail = detail;
    this.retryAfterMs = retryAfterMs;
    this.code = code;
    this.details = details;
  }
}

/** Parses RFC 9110 Retry-After: HTTP-date or delay-seconds. */
export function parseRetryAfter(value: string | null, now: Date = new Date()): number | null {
  if (!value) {
    return null;
  }
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return null;
  }
  if (/^\d+$/.test(trimmed)) {
    return Number(trimmed) * 1000;
  }
  const parsed = new Date(trimmed);
  if (Number.isNaN(parsed.getTime())) {
    return null;
  }
  return Math.max(0, parsed.getTime() - now.getTime());
}

function extractErrorMessage(body: unknown): string {
  if (!body || typeof body !== "object") {
    return getStaticMessages().common.requestFailed;
  }
  const detail = (body as { detail?: unknown }).detail;
  if (typeof detail === "string" && detail.trim().length > 0) {
    return detail;
  }
  if (Array.isArray(detail) && detail.length > 0) {
    return detail.map((item) => JSON.stringify(item)).join(", ");
  }
  if (detail && typeof detail === "object") {
    const maybeMessage = (detail as { message?: unknown }).message;
    if (typeof maybeMessage === "string" && maybeMessage.trim().length > 0) {
      return maybeMessage;
    }
  }
  return getStaticMessages().common.requestFailed;
}

function shouldAttachProfileHeader(path: string): boolean {
  return isProfileScopedManagementRoute(path);
}

function buildHeaders(path: string, init?: RequestInit): Record<string, string> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init?.headers as Record<string, string>),
  };

  if (shouldAttachProfileHeader(path)) {
    headers["X-Profile-Id"] = String(currentProfileId);
  }

  return headers;
}

export interface RequestOptions {
  allowAuthRefresh?: boolean;
  /** Internal: set on the single replay of an eligible pre-handler 401. */
  authReplayAttempted?: boolean;
  /** Internal: return a successful response as a Blob for download endpoints. */
  responseType?: "json" | "blob";
  /** Internal: how many overload replays this request has already spent. */
  overloadRetryAttempt?: number;
}

/**
 * Management admission is a counter, not a queue: one request over the line is
 * rejected immediately with 503 and `Retry-After`, even though the server is
 * healthy a second later. Without a replay here every such moment turns a
 * transient into a panel that stays failed until the operator clicks refresh.
 * Bounded and idempotent-only, so a replay can never repeat a mutation or
 * become the load that keeps the server over its limit.
 */
const OVERLOAD_RETRY_LIMIT = 2;
const OVERLOAD_RETRY_MIN_MS = 250;
const OVERLOAD_RETRY_MAX_MS = 2000;

function isIdempotentRead(init?: RequestInit): boolean {
  const method = (init?.method ?? "GET").toUpperCase();
  return method === "GET" || method === "HEAD";
}

/** Milliseconds to wait before replaying, or null when this 503 is terminal. */
export function overloadRetryDelayMs(
  retryAfterMs: number | null,
  attempt: number,
  jitter: number = Math.random(),
): number | null {
  // No Retry-After means the server never said this was transient. Admission
  // overload always sends one; an unqualified 503 is somebody else's outage
  // and replaying it is a guess, not a recovery.
  if (retryAfterMs === null || attempt >= OVERLOAD_RETRY_LIMIT) {
    return null;
  }
  // The server's own floor is a full second, so the clamp only ever matters
  // for an absurd header.
  const base = Math.min(Math.max(retryAfterMs, OVERLOAD_RETRY_MIN_MS), OVERLOAD_RETRY_MAX_MS);
  // Spread the replays: a page fans out several reads at once, and they all
  // hit the ceiling in the same instant.
  return Math.round(base * (1 + jitter * 0.5));
}

/** Resolves after `ms`, or as soon as the request is abandoned. */
function waitBeforeReplay(ms: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) {
    return Promise.resolve();
  }
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    function onAbort() {
      clearTimeout(timer);
      resolve();
    }
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

export async function request<T>(
  path: string,
  init?: RequestInit,
  options?: RequestOptions,
): Promise<T> {
  const headers = buildHeaders(path, init);
  const allowAuthRefresh = options?.allowAuthRefresh ?? true;
  const authReplayAttempted = options?.authReplayAttempted ?? false;
  const responseType = options?.responseType ?? "json";
  const overloadRetryAttempt = options?.overloadRetryAttempt ?? 0;

  const epochSignal = authSessionCoordinator.epochSignal();
  const mergedSignal = mergeSignals(epochSignal, init?.signal);

  let res: Response
  try {
    res = await fetch(`${API_BASE}${path}`, {
      ...init,
      credentials: "include",
      headers,
      signal: mergedSignal,
    })
  } catch (error) {
    // Epoch invalidation aborts every old protected request. Convert that
    // browser-level AbortError into the silent typed boundary so late work
    // cannot leak an unhandled rejection or a page-level network error.
    if (epochSignal?.aborted) {
      throw new StaleSessionEpochError()
    }
    throw error
  }

  try {
    // Stale epoch: the response belongs to a previous auth generation and
    // must never populate UI, cache or side effects. The body read is also
    // inside this guard because abort may happen after headers arrive.
    if (epochSignal?.aborted) {
      throw new StaleSessionEpochError();
    }

    if (res.status === 401 && allowAuthRefresh && !authReplayAttempted) {
      if (isAuthExemptPath(path)) {
        // Auth-exempt routes are handled by their own flows; the 401 is a
        // regular (typed) error here.
        return await buildError<T>(res);
      }
      const outcome = await handleEligible401(path);
      if (outcome === "replay") {
        return request<T>(path, init, { allowAuthRefresh: false, authReplayAttempted: true, responseType });
      }
      // The phase/epoch was committed synchronously; waiters end silently.
      throw new AuthPhaseChangedError();
    }

    if (res.status === 503 && isIdempotentRead(init)) {
      const delayMs = overloadRetryDelayMs(parseRetryAfter(res.headers.get("Retry-After")), overloadRetryAttempt);
      if (delayMs !== null) {
        await waitBeforeReplay(delayMs, mergedSignal);
        return request<T>(path, init, {
          allowAuthRefresh,
          authReplayAttempted,
          responseType,
          overloadRetryAttempt: overloadRetryAttempt + 1,
        });
      }
    }

    if (responseType === "blob") {
      if (!res.ok) {
        return await buildError<T>(res);
      }
      return (await res.blob()) as T;
    }
    return await buildError<T>(res);
  } catch (error) {
    if (error instanceof AuthPhaseChangedError || error instanceof StaleSessionEpochError) {
      throw error;
    }
    if (epochSignal?.aborted) {
      throw new StaleSessionEpochError();
    }
    throw error;
  }
}

// handleEligible401 runs the coordinator's singleflight policy for a typed
// pre-handler auth 401 and returns "replay" only when the refresh succeeded
// with the same subject and the epoch still matches.
async function handleEligible401(path: string): Promise<"replay" | "settled"> {
  const phase = authSessionCoordinator.getPhase();
  const observedEpoch = authSessionCoordinator.getEpoch();

  if (phase.kind === "AUTH_DISABLED") {
    // Auth-disabled 401 is a system inconsistency (SPEC §5.4): no refresh,
    // no login redirect. The coordinator enters AUTH_DISABLED_VERIFYING and
    // runs the single fixed GET /api/models probe; every waiter settles
    // silently through the phase commit.
    const effectiveGeneration = authSessionCoordinator.getPhase().session_epoch.toString();
    authSessionCoordinator.dispatch({ type: "AUTH_INCONSISTENT", observed_epoch: observedEpoch, request_path: path });
    authSessionCoordinator.beginDisabledVerification(effectiveGeneration);
    return "settled";
  }

  if (phase.kind === "AUTHENTICATED" || phase.kind === "REFRESHING") {
    const { promise } = authSessionCoordinator.ensureRecoveryFlight();
    const outcome = await promise;
    if (outcome.kind === "REFRESHED" && authSessionCoordinator.getEpoch() === observedEpoch) {
      const currentPhase = authSessionCoordinator.getPhase();
      if (currentPhase.kind === "AUTHENTICATED" && currentPhase.subject_key === outcome.session.subject_key) {
        return "replay";
      }
    }
    return "settled";
  }

  // Any other phase receiving a protected-management 401 is a protocol/owner
  // violation: fail closed globally with public-auth-status recovery.
  authSessionCoordinator.dispatch({
    type: "AUTH_UNAVAILABLE",
    observed_epoch: observedEpoch,
    reason: "unexpected_auth_401",
    recovery_kind: "public_auth_status",
  });
  return "settled";
}

function mergeSignals(epochSignal: AbortSignal | null, callerSignal?: AbortSignal | null): AbortSignal | undefined {
  if (!epochSignal && !callerSignal) {
    return undefined;
  }
  if (epochSignal && !callerSignal) {
    return epochSignal;
  }
  if (!epochSignal && callerSignal) {
    return callerSignal;
  }
  const controller = new AbortController();
  const onAbort = () => controller.abort();
  epochSignal?.addEventListener("abort", onAbort, { once: true });
  callerSignal?.addEventListener("abort", onAbort, { once: true });
  return controller.signal;
}

async function buildError<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let body: unknown = null;
    try {
      body = await res.json();
    } catch {
      body = null;
    }
    const envelope = extractProblemEnvelope(body);
    throw new ApiError(
      envelope?.detail ?? extractErrorMessage(body),
      res.status,
      body,
      parseRetryAfter(res.headers.get("Retry-After")),
      envelope?.code,
      envelope?.details,
    );
  }

  if (res.status === 204 || res.status === 205) {
    return undefined as T;
  }

  const text = await res.text();
  if (text.length === 0) {
    return undefined as T;
  }

  return JSON.parse(text) as T;
}

function extractProblemEnvelope(body: unknown): { code?: string; detail?: string; details?: unknown } | null {
  if (!body || typeof body !== "object") {
    return null;
  }
  const candidate = body as { code?: unknown; detail?: unknown; details?: unknown };
  if (typeof candidate.code !== "string") {
    return null;
  }
  return {
    code: candidate.code,
    detail: typeof candidate.detail === "string" ? candidate.detail : undefined,
    details: candidate.details,
  };
}

export function buildQuery(
  params?: Record<string, string | number | boolean | null | undefined>
) {
  const qs = new URLSearchParams();
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== "") {
        qs.set(key, String(value));
      }
    });
  }
  return qs.toString();
}

// Re-exported for consumers that need the session payload type.
export type { SessionResponse };
