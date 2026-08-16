import type { AuthenticatedSession, AuthTransitionProblemDetails } from "@/lib/types/auth";

// Refresh outcome classifier (Auth/Session/Landing SPEC §5.2): HTTP
// classification preserves the refresh response status/body; it never
// collapses failures into a boolean.

export type RefreshOutcome =
  | { kind: "REFRESHED"; session: AuthenticatedSession }
  | { kind: "EXPIRED"; evidence: "refresh_401" | "refresh_200_unauthenticated" }
  | { kind: "AUTH_DISABLED" }
  | {
      kind: "AUTH_TRANSITION_FAIL_CLOSED";
      transition_state: "enabling_fail_closed" | "rollback_required";
      effective_generation: string;
      retry_after_seconds?: number;
    }
  | {
      kind: "AUTH_UNAVAILABLE";
      reason: "network" | "timeout" | "forbidden" | "rate_limited" | "server" | "invalid_payload" | "unexpected_response";
      retry_after_seconds?: number;
    };

export type RefreshProblemEnvelope = Record<string, unknown>;

export type RefreshRawBody = Record<string, unknown>;

// classifyRefreshOutcome maps a raw refresh response to the typed outcome.
// The order is exhaustive and mutually exclusive; anything not listed
// exactly becomes unexpected_response.
export function classifyRefreshOutcome(
  status: number | null,
  body: RefreshRawBody | RefreshProblemEnvelope | null,
  networkError: boolean,
  timedOut: boolean,
  retryAfterSeconds?: number,
): RefreshOutcome {
  const details = body?.details as AuthTransitionProblemDetails | undefined;
  if (networkError) {
    return { kind: "AUTH_UNAVAILABLE", reason: "network", retry_after_seconds: retryAfterSeconds };
  }
  if (timedOut) {
    return { kind: "AUTH_UNAVAILABLE", reason: "timeout", retry_after_seconds: retryAfterSeconds };
  }
  if (status === null) {
    return { kind: "AUTH_UNAVAILABLE", reason: "unexpected_response" };
  }
  if (status === 503) {
    const code = body?.code as string | undefined;
    if (
      code === "auth_transition_in_progress" &&
      details &&
      details.transition_state === "enabling_fail_closed" &&
      details.recovery === "confirm_public_status"
    ) {
      return {
        kind: "AUTH_TRANSITION_FAIL_CLOSED",
        transition_state: "enabling_fail_closed",
        effective_generation: details.effective_generation,
        retry_after_seconds: details.retry_after_seconds ?? undefined,
      };
    }
    if (
      code === "auth_transition_recovery_required" &&
      details &&
      details.transition_state === "rollback_required" &&
      details.recovery === "confirm_public_status"
    ) {
      return {
        kind: "AUTH_TRANSITION_FAIL_CLOSED",
        transition_state: "rollback_required",
        effective_generation: details.effective_generation,
        retry_after_seconds: details.retry_after_seconds ?? undefined,
      };
    }
    return { kind: "AUTH_UNAVAILABLE", reason: "server", retry_after_seconds: retryAfterSeconds };
  }
  if (status === 401) {
    return { kind: "EXPIRED", evidence: "refresh_401" };
  }
  if (status === 403) {
    return { kind: "AUTH_UNAVAILABLE", reason: "forbidden", retry_after_seconds: retryAfterSeconds };
  }
  if (status === 429) {
    return { kind: "AUTH_UNAVAILABLE", reason: "rate_limited", retry_after_seconds: retryAfterSeconds };
  }
  if (status >= 500 && status <= 599) {
    return { kind: "AUTH_UNAVAILABLE", reason: "server", retry_after_seconds: retryAfterSeconds };
  }
  if (status !== 200) {
    return { kind: "AUTH_UNAVAILABLE", reason: "unexpected_response" };
  }
  const authenticated = body?.authenticated as unknown;
  const authEnabled = body?.auth_enabled as unknown;
  if (authEnabled === false && authenticated === false) {
    if ((body?.subject_key as unknown) !== undefined && (body?.subject_key as unknown) !== null) {
      return { kind: "AUTH_UNAVAILABLE", reason: "invalid_payload" };
    }
    return { kind: "AUTH_DISABLED" };
  }
  if (authEnabled === true && authenticated === false) {
    if ((body?.subject_key as unknown) !== undefined && (body?.subject_key as unknown) !== null) {
      return { kind: "AUTH_UNAVAILABLE", reason: "invalid_payload" };
    }
    return { kind: "EXPIRED", evidence: "refresh_200_unauthenticated" };
  }
  if (authEnabled === true && authenticated === true) {
    const subjectKey = body?.subject_key as unknown;
    if (typeof subjectKey !== "string" || subjectKey.trim().length === 0) {
      return { kind: "AUTH_UNAVAILABLE", reason: "invalid_payload" };
    }
    const username = body?.username as unknown;
    if (username !== null && username !== undefined && typeof username !== "string") {
      return { kind: "AUTH_UNAVAILABLE", reason: "invalid_payload" };
    }
    return {
      kind: "REFRESHED",
      session: {
        authenticated: true,
        auth_enabled: true,
        username: typeof username === "string" ? username : null,
        subject_key: subjectKey as string,
      },
    };
  }
  // Anything else (including auth_enabled=false, authenticated=true and
  // missing booleans) is a protocol inconsistency.
  return { kind: "AUTH_UNAVAILABLE", reason: "unexpected_response" };
}
