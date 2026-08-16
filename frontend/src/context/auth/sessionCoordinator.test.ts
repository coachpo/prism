import { describe, expect, it } from "vitest";
import { AuthSessionCoordinator, AuthPhaseChangedError } from "@/context/auth/sessionCoordinator";
import { classifyRefreshOutcome } from "@/context/auth/refreshOutcome";
import { isAuthExemptPath, isPublicAuthOperationStatusPath, parsePublicAuthOperationID } from "@/context/auth/authExempt";

function makeCoordinator(overrides?: {
  refresh?: AuthSessionCoordinator["ensurePassiveFlight"] extends never ? never : () => Promise<{ status: number | null; body: unknown; networkError: boolean; timedOut: boolean; retryAfterSeconds?: number }>;
  disabledAccessProbe?: () => Promise<"confirmed" | "unauthorized" | "unavailable">;
}) {
  return new AuthSessionCoordinator({
    refresh:
      overrides?.refresh ??
      (async () => ({
        status: 200,
        body: { authenticated: true, auth_enabled: true, username: "admin", subject_key: "sub:1" },
        networkError: false,
        timedOut: false,
      })),
    disabledAccessProbe: overrides?.disabledAccessProbe,
  });
}

describe("refresh outcome classifier", () => {
  it("classifies the exhaustive refresh matrix without fallthrough", () => {
    expect(classifyRefreshOutcome(200, { authenticated: true, auth_enabled: true, subject_key: "s:1", username: "a" }, false, false).kind).toBe("REFRESHED");
    expect(classifyRefreshOutcome(401, null, false, false)).toEqual({ kind: "EXPIRED", evidence: "refresh_401" });
    expect(classifyRefreshOutcome(200, { authenticated: false, auth_enabled: true }, false, false)).toEqual({ kind: "EXPIRED", evidence: "refresh_200_unauthenticated" });
    expect(classifyRefreshOutcome(200, { authenticated: false, auth_enabled: false }, false, false)).toEqual({ kind: "AUTH_DISABLED" });
    expect(classifyRefreshOutcome(200, { authenticated: false, auth_enabled: false, subject_key: "s:1" }, false, false).kind).toBe("AUTH_UNAVAILABLE");
    expect(classifyRefreshOutcome(503, { code: "auth_transition_in_progress", details: { transition_state: "enabling_fail_closed", effective_generation: "2", recovery: "confirm_public_status", retry_after_seconds: null } }, false, false).kind).toBe("AUTH_TRANSITION_FAIL_CLOSED");
    expect(classifyRefreshOutcome(503, { code: "auth_transition_recovery_required", details: { transition_state: "rollback_required", effective_generation: "2", recovery: "confirm_public_status", retry_after_seconds: null } }, false, false).kind).toBe("AUTH_TRANSITION_FAIL_CLOSED");
    expect(classifyRefreshOutcome(503, { code: "auth_transition_in_progress", details: { transition_state: "rollback_required", effective_generation: "2", recovery: "confirm_public_status" } }, false, false).kind).toBe("AUTH_UNAVAILABLE");
    expect(classifyRefreshOutcome(403, null, false, false)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "forbidden" });
    expect(classifyRefreshOutcome(429, null, false, false)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "rate_limited" });
    expect(classifyRefreshOutcome(500, null, false, false)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "server" });
    expect(classifyRefreshOutcome(204, null, false, false)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "unexpected_response" });
    expect(classifyRefreshOutcome(null, null, true, false)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "network" });
    expect(classifyRefreshOutcome(null, null, false, true)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "timeout" });
    expect(classifyRefreshOutcome(200, { authenticated: true, auth_enabled: true }, false, false)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "invalid_payload" });
    expect(classifyRefreshOutcome(200, { authenticated: false, auth_enabled: true, subject_key: "x" }, false, false).kind).toBe("AUTH_UNAVAILABLE");
    expect(classifyRefreshOutcome(200, { authenticated: true, auth_enabled: false }, false, false)).toEqual({ kind: "AUTH_UNAVAILABLE", reason: "unexpected_response" });
  });
});

describe("session coordinator singleflight", () => {
  it("emits AUTH_RECOVERY_STARTED exactly once for concurrent eligible 401s and settles with the same subject", async () => {
    const coordinator = makeCoordinator();
    coordinator.applyLoginSuccess({ authenticated: true, auth_enabled: true, username: "admin", subject_key: "sub:1" });
    const events: string[] = [];
    coordinator.subscribe(() => events.push(coordinator.getPhase().kind));

    const first = coordinator.ensureRecoveryFlight();
    const second = coordinator.ensureRecoveryFlight();
    expect(first.created).toBe(true);
    expect(second.created).toBe(false);
    expect(coordinator.getPhase().kind).toBe("REFRESHING");

    const outcome = await first.promise;
    expect(outcome.kind).toBe("REFRESHED");
    expect(coordinator.getPhase().kind).toBe("AUTHENTICATED");
    expect(events.filter((kind) => kind === "REFRESHING")).toHaveLength(1);
  });

  it("advances the epoch on definitive expiry and drops late events", async () => {
    const coordinator = makeCoordinator({
      refresh: async () => ({ status: 401, body: null, networkError: false, timedOut: false }),
    });
    coordinator.applyLoginSuccess({ authenticated: true, auth_enabled: true, username: "admin", subject_key: "sub:1" });
    const epochBefore = coordinator.getEpoch();
    await coordinator.ensureRecoveryFlight().promise;
    expect(coordinator.getPhase().kind).toBe("SESSION_EXPIRED");
    expect(coordinator.getEpoch()).toBe(epochBefore + 1);
    // Late dispatch from the old epoch is ignored.
    coordinator.dispatch({ type: "AUTH_RECOVERY_STARTED", observed_epoch: epochBefore });
    expect(coordinator.getPhase().kind).toBe("SESSION_EXPIRED");
  });

  it("promotes an in-flight passive flight to recovery without a second refresh", async () => {
    let refreshCalls = 0;
    let resolveRefresh: (value: { status: number | null; body: unknown; networkError: boolean; timedOut: boolean }) => void = () => undefined;
    const coordinator = makeCoordinator({
      refresh: () => {
        refreshCalls += 1;
        return new Promise((resolve) => {
          resolveRefresh = resolve as never;
        });
      },
    });
    coordinator.applyLoginSuccess({ authenticated: true, auth_enabled: true, username: "admin", subject_key: "sub:1" });

    const passive = coordinator.ensurePassiveFlight();
    expect(passive.created).toBe(true);
    const recovery = coordinator.ensureRecoveryFlight();
    expect(recovery.created).toBe(false);
    expect(coordinator.getPhase().kind).toBe("REFRESHING");

    resolveRefresh({ status: 200, body: { authenticated: true, auth_enabled: true, username: "admin", subject_key: "sub:1" }, networkError: false, timedOut: false });
    const outcome = await passive.promise;
    expect(outcome.kind).toBe("REFRESHED");
    expect(refreshCalls).toBe(1);
    expect(coordinator.getPhase().kind).toBe("AUTHENTICATED");
  });

  it("enters AUTH_DISABLED_VERIFYING on a disabled-401 inconsistency and clears with the probe", async () => {
    const coordinator = makeCoordinator({
      disabledAccessProbe: async () => "confirmed" as const,
    });
    coordinator.applyBootstrapStatus({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null }, null, true);
    expect(coordinator.getPhase().kind).toBe("AUTH_DISABLED");

    const epochBefore = coordinator.getEpoch();
    coordinator.dispatch({ type: "AUTH_INCONSISTENT", observed_epoch: coordinator.getEpoch(), request_path: "/api/models" });
    expect(coordinator.getPhase().kind).toBe("AUTH_UNAVAILABLE");
    coordinator.beginDisabledVerification("1");
    expect(coordinator.getPhase().kind).toBe("AUTH_DISABLED_VERIFYING");
    expect(coordinator.getEpoch()).toBe(epochBefore + 1);

    // The probe resolves asynchronously; wait for the confirmation.
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(coordinator.getPhase().kind).toBe("AUTH_DISABLED");
  });

  it("exhausts the disabled probe incident on failure and keeps the breaker closed", async () => {
    const coordinator = makeCoordinator({
      disabledAccessProbe: async () => "unauthorized" as const,
    });
    coordinator.applyBootstrapStatus({ state: "disabled", transition_state: null, login_available: false, effective_generation: "1", retry_after_seconds: null }, null, true);
    coordinator.dispatch({ type: "AUTH_INCONSISTENT", observed_epoch: coordinator.getEpoch(), request_path: "/api/models" });
    coordinator.beginDisabledVerification("1");
    await new Promise((resolve) => setTimeout(resolve, 10));
    const phase = coordinator.getPhase();
    expect(phase.kind).toBe("AUTH_UNAVAILABLE");
    if (phase.kind === "AUTH_UNAVAILABLE") {
      expect(phase.reason).toBe("disabled_but_unauthorized");
      expect(phase.incident?.state).toBe("exhausted");
    }
  });
});

describe("auth exempt matcher", () => {
  it("matches permanent exempt paths exactly", () => {
    expect(isAuthExemptPath("/api/auth/status")).toBe(true);
    expect(isAuthExemptPath("/api/auth/public-bootstrap")).toBe(true);
    expect(isAuthExemptPath("/api/auth/session")).toBe(true);
    expect(isAuthExemptPath("/api/auth/login")).toBe(true);
    expect(isAuthExemptPath("/api/auth/logout")).toBe(true);
    expect(isAuthExemptPath("/api/auth/refresh")).toBe(true);
    expect(isAuthExemptPath("/api/auth/operations/11111111-1111-4111-8111-111111111111/status")).toBe(false);
    expect(isAuthExemptPath("/api/models")).toBe(false);
  });

  it("matches the public operation status route exactly", () => {
    const origin = window.location.origin;
    expect(isPublicAuthOperationStatusPath("GET", "/api/auth/operations/11111111-1111-4111-8111-111111111111/status", "", origin)).toBe(true);
    expect(isPublicAuthOperationStatusPath("POST", "/api/auth/operations/11111111-1111-4111-8111-111111111111/status", "", origin)).toBe(false);
    expect(isPublicAuthOperationStatusPath("GET", "/api/auth/operations/11111111-1111-4111-8111-111111111111/status?x=1", "x=1", origin)).toBe(false);
    expect(isPublicAuthOperationStatusPath("GET", "/api/auth/operations/not-a-uuid/status", "", origin)).toBe(false);
    expect(isPublicAuthOperationStatusPath("GET", "/api/auth/operations/11111111-1111-4111-8111-111111111111/status/", "", origin)).toBe(false);
    expect(parsePublicAuthOperationID("/api/auth/operations/11111111-1111-4111-8111-111111111111/status")).toBe("11111111-1111-4111-8111-111111111111");
    expect(parsePublicAuthOperationID("/api/auth/operations/bogus/status")).toBeNull();
  });
});

describe("auth phase changed error", () => {
  it("is a silent internal error type", () => {
    const error = new AuthPhaseChangedError();
    expect(error.name).toBe("AuthPhaseChangedError");
  });
});

describe("cross-tab auth generation fence", () => {
  it("adopts and clears the target generation for enabled and disabled bootstrap", () => {
    const coordinator = makeCoordinator();
    coordinator.beginCrossTabBootstrap("disabled-target");
    expect(coordinator.getPendingAuthGeneration()).toBe("disabled-target");
    coordinator.applyBootstrapStatus(
      { state: "disabled", transition_state: null, login_available: false, effective_generation: "4", retry_after_seconds: null },
      null,
      true,
    );
    expect(coordinator.getSessionGenerationId()).toBe("disabled-target");
    expect(coordinator.getPendingAuthGeneration()).toBeNull();
    expect(coordinator.getPhase().kind).toBe("AUTH_DISABLED");

    coordinator.beginCrossTabBootstrap("enabled-target");
    coordinator.applyBootstrapStatus(
      { state: "enabled", transition_state: null, login_available: true, effective_generation: "5", retry_after_seconds: null },
      { authenticated: true, auth_enabled: true, username: "admin", subject_key: "sub:1" },
      true,
    );
    expect(coordinator.getSessionGenerationId()).toBe("enabled-target");
    expect(coordinator.getPendingAuthGeneration()).toBeNull();
    expect(coordinator.getPhase().kind).toBe("AUTHENTICATED");
  });
});
