import type { AuthenticatedSession } from "@/lib/types/auth";
import { classifyRefreshOutcome, type RefreshOutcome } from "./refreshOutcome";
import { loadSharedSessionGeneration, persistSharedSessionGeneration, randomSessionGenerationId } from "./crossTab";

// Process-local auth session coordinator (SPEC §4–§5). It owns the single
// phase/epoch store, the typed event bridge, the singleflight refresh and
// the cross-tab pending-generation fence. It is constructed before the
// QueryClient, router and any protected client; the React AuthProvider is
// only a render subscriber and never the sole commit seam.

export type EnforcedAuthTransition = {
  transition_state: "disabling_enforced";
  effective_generation: string;
  operation_id?: string;
  retry_after_seconds?: number;
};

export type FailClosedAuthTransitionState = "enabling_fail_closed" | "rollback_required";

export type AuthPhase =
  | { kind: "BOOTSTRAPPING"; session_epoch: number }
  | { kind: "AUTH_DISABLED"; session_epoch: number }
  | { kind: "AUTH_DISABLED_VERIFYING"; session_epoch: number; effective_generation: string; incident_id: string }
  | { kind: "ANONYMOUS"; session_epoch: number; transition?: EnforcedAuthTransition }
  | { kind: "AUTHENTICATED"; session_epoch: number; subject_key: string; username: string; transition?: EnforcedAuthTransition }
  | { kind: "REFRESHING"; session_epoch: number; subject_key: string; username: string; transition?: EnforcedAuthTransition }
  | { kind: "LOGGING_OUT"; session_epoch: number; state: "pending" | "unconfirmed"; retry_after_seconds?: number }
  | {
      kind: "AUTH_TRANSITION_FAIL_CLOSED";
      session_epoch: number;
      transition_state: FailClosedAuthTransitionState;
      effective_generation: string;
      operation_id?: string;
      retry_after_seconds?: number;
    }
  | {
      kind: "AUTH_UNAVAILABLE";
      session_epoch: number;
      reason: AuthUnavailableReason;
      recovery_kind: AuthRecoveryKind;
      retry_after_seconds?: number;
      incident?: DisabledProbeIncident;
      /** Automatic retries already spent before the gate closed. */
      auto_retry_attempts?: number;
      /** Epoch millis of the last authoritative auth answer, or null when there was none. */
      last_confirmed_at?: number | null;
    }
  | { kind: "SESSION_EXPIRED"; session_epoch: number; return_to: string };

export type AuthUnavailableReason =
  | "bootstrap_failed"
  | "network"
  | "timeout"
  | "forbidden"
  | "rate_limited"
  | "server"
  | "invalid_payload"
  | "unexpected_response"
  | "unexpected_auth_401"
  | "auth_coordinator_unavailable"
  | "disabled_but_unauthorized";

export type AuthRecoveryKind =
  | "public_bootstrap"
  | "public_auth_status"
  | "public_auth_operation_status"
  | "session_refresh"
  | "session_logout";

export type DisabledProbeIncident = {
  kind: "disabled_401_probe";
  state: "exhausted";
  effective_generation: string;
  incident_id: string;
};

export type PendingLogoutIntent = {
  intent_id: string;
  origin_session_generation_id: string;
  state: "request_pending" | "confirmation_needed" | "confirming" | "unconfirmed";
};

export type AuthSessionCoordinatorState = {
  phase: AuthPhase;
  pending_logout_intent: PendingLogoutIntent | null;
};

export type AuthClientEvent =
  | { type: "AUTH_RECOVERY_STARTED"; observed_epoch: number }
  | { type: "AUTH_RECOVERY_SUCCEEDED"; observed_epoch: number; session: AuthenticatedSession }
  | { type: "AUTH_SESSION_IDENTITY_CHANGED"; observed_epoch: number; session: AuthenticatedSession; evidence: "refresh_subject_changed" }
  | { type: "SESSION_EXPIRED"; observed_epoch: number; evidence: string; request_path: string }
  | { type: "AUTH_DISABLED"; observed_epoch: number }
  | { type: "AUTH_UNAVAILABLE"; observed_epoch: number; reason: AuthUnavailableReason; recovery_kind: AuthRecoveryKind; retry_after_seconds?: number }
  | { type: "AUTH_INCONSISTENT"; observed_epoch: number; request_path: string }
  | {
      type: "AUTH_TRANSITION_DETECTED";
      observed_epoch: number;
      transition_state: FailClosedAuthTransitionState;
      effective_generation: string;
      retry_after_seconds?: number;
      evidence: "ordinary_management_503" | "refresh_503" | "login_503";
    };

// AuthPhaseChangedError is the silent internal result for waiters that must
// never surface as page errors or toasts.
export class AuthPhaseChangedError extends Error {
  constructor(message = "auth phase changed") {
    super(message);
    this.name = "AuthPhaseChangedError";
  }
}

export class StaleSessionEpochError extends Error {
  constructor(message = "stale session epoch") {
    super(message);
    this.name = "StaleSessionEpochError";
  }
}

type RefreshFlight = {
  mode: "passive" | "recovery";
  startedEventSent: boolean;
  promise: Promise<RefreshOutcome>;
  observedEpoch: number;
};

export type AuthCoordinatorOptions = {
  refresh: () => Promise<{ status: number | null; body: unknown; networkError: boolean; timedOut: boolean; retryAfterSeconds?: number }>;
  publicBootstrap?: () => Promise<import("@/lib/types/auth").PublicAuthStatus>;
  now?: () => number;
  /** Runs the single fixed GET /api/models disabled-access probe (SPEC §5.4). */
  disabledAccessProbe?: () => Promise<"confirmed" | "unauthorized" | "unavailable">;
};

const DISABLED_PROBE_TIMEOUT_MS = 10_000;
const MAX_DISABLED_PROBE_WAITERS = 64;

// AUTH_UNAVAILABLE has two very different causes and they must not share one
// recovery policy. An authorization answer (403, an illegal payload, a protocol
// inconsistency) is definitive, so fail-closed is right. A 5xx, a dropped
// connection, a timeout or a rate limit is the backend hiccupping: turning the
// whole console into a full-screen gate on the first failure — and requiring a
// human click to leave it — costs far more than the fault does. Transient
// reasons back off and retry themselves; only an exhausted retry budget closes
// the gate. `request.ts` replays an idempotent 503 that carried Retry-After;
// this layer covers the unqualified failures that one deliberately leaves.
const TRANSIENT_AUTH_UNAVAILABLE_REASONS: ReadonlySet<AuthUnavailableReason> = new Set([
  "bootstrap_failed",
  "network",
  "timeout",
  "rate_limited",
  "server",
]);
const AUTH_UNAVAILABLE_MAX_AUTO_RETRIES = 3;
const AUTH_UNAVAILABLE_RETRY_BASE_MS = 1_000;
/** Ceiling for both the backoff and an honoured Retry-After: somebody is waiting behind the gate. */
const AUTH_UNAVAILABLE_RETRY_CEILING_MS = 30_000;

// The server's own Retry-After wins when it sent one; otherwise the delay
// doubles per attempt (1s, 2s, 4s).
export function authUnavailableRetryDelayMs(attempt: number, retryAfterSeconds?: number): number {
  const base =
    retryAfterSeconds !== undefined && retryAfterSeconds >= 0
      ? retryAfterSeconds * 1000
      : AUTH_UNAVAILABLE_RETRY_BASE_MS * 2 ** attempt;
  return Math.min(base, AUTH_UNAVAILABLE_RETRY_CEILING_MS);
}

// AuthSessionCoordinator is the single process-local auth session store.
export class AuthSessionCoordinator {
  private state: AuthSessionCoordinatorState;
  private epochController: AbortController | null = null;
  private flight: RefreshFlight | null = null;
  private listeners = new Set<() => void>();
  private epoch: number;
  private sessionGenerationId = loadSharedSessionGeneration();
  private pendingAuthGeneration: string | null = null;
  private pendingGenerationExpiry: AuthClientEvent | null = null;
  private options: AuthCoordinatorOptions;
  private readonly coordinatorReady: boolean;
  private autoRetryAttempts = 0;
  private autoRetryTimer: ReturnType<typeof setTimeout> | null = null;
  private lastConfirmedAt: number | null = null;
  private recoveryRunner: (() => void) | null = null;

  constructor(options: AuthCoordinatorOptions) {
    this.options = options;
    this.coordinatorReady = true;
    this.epoch = 1;
    this.state = { phase: { kind: "BOOTSTRAPPING", session_epoch: this.epoch }, pending_logout_intent: null };
    this.epochController = new AbortController();
  }

  // ---- external store surface ----

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  getState(): AuthSessionCoordinatorState {
    return this.state;
  }

  getPhase(): AuthPhase {
    return this.state.phase;
  }

  getEpoch(): number {
    return this.epoch;
  }

  getSessionGenerationId(): string {
    return this.sessionGenerationId;
  }

  getPendingAuthGeneration(): string | null {
    return this.pendingAuthGeneration;
  }

  isReady(): boolean {
    return this.coordinatorReady;
  }

  // ---- epoch management ----

  epochSignal(): AbortSignal | null {
    return this.epochController?.signal ?? null;
  }

  isCurrentEpoch(observedEpoch: number): boolean {
    return observedEpoch === this.epoch;
  }

  private advanceEpoch(): number {
    this.epoch += 1;
    this.epochController?.abort();
    this.epochController = new AbortController();
    this.flight = null;
    this.cancelAutoRecoveryTimer();
    return this.epoch;
  }

  // ---- automatic recovery for transient auth failures ----

  // setRecoveryRunner registers the public bootstrap read the coordinator may
  // re-run on its own. Without one (unit tests, teardown) every
  // AUTH_UNAVAILABLE stays immediately fail-closed.
  setRecoveryRunner(run: (() => void) | null): void {
    this.recoveryRunner = run;
  }

  /** Returns the retry budget, so the next transient failure heals itself again. */
  cancelAutoRecovery(): void {
    this.cancelAutoRecoveryTimer();
    this.autoRetryAttempts = 0;
  }

  // An epoch change abandons a scheduled retry but not the budget: once the
  // gate is up the breaker stays open, and the console must not keep polling
  // /api/auth/status behind it forever.
  private cancelAutoRecoveryTimer(): void {
    if (this.autoRetryTimer !== null) {
      clearTimeout(this.autoRetryTimer);
      this.autoRetryTimer = null;
    }
  }

  // scheduleAutoRecovery returns true when the failure was absorbed into a
  // backoff instead of the gate. The phase is deliberately left alone: the
  // operator keeps seeing the awaiting surface they were already on, not a
  // blocker that appears and disappears on every retry.
  private scheduleAutoRecovery(reason: AuthUnavailableReason, retryAfterSeconds?: number): boolean {
    const run = this.recoveryRunner;
    if (!run || !TRANSIENT_AUTH_UNAVAILABLE_REASONS.has(reason)) {
      return false;
    }
    if (this.autoRetryTimer !== null) {
      return true;
    }
    if (this.autoRetryAttempts >= AUTH_UNAVAILABLE_MAX_AUTO_RETRIES) {
      return false;
    }
    const delayMs = authUnavailableRetryDelayMs(this.autoRetryAttempts, retryAfterSeconds);
    this.autoRetryAttempts += 1;
    this.autoRetryTimer = setTimeout(() => {
      this.autoRetryTimer = null;
      run();
    }, delayMs);
    return true;
  }

  private now(): number {
    return this.options.now?.() ?? Date.now();
  }

  // ---- phase commits ----

  private commit(next: AuthPhase): void {
    // An authoritative answer is what the gate later reports as the last
    // confirmation, and it also returns the retry budget.
    if (next.kind === "AUTHENTICATED" || next.kind === "ANONYMOUS" || next.kind === "AUTH_DISABLED") {
      this.lastConfirmedAt = this.now();
      this.cancelAutoRecovery();
    }
    this.state = { ...this.state, phase: next };
    for (const listener of this.listeners) {
      listener();
    }
  }

  // ---- event bridge ----

  // dispatch synchronously commits the event to the single process-local
  // store; the React provider only subscribes and re-renders afterwards.
  dispatch(event: AuthClientEvent): void {
    if (event.observed_epoch !== this.epoch) {
      // Late events from a previous epoch are silently dropped.
      return;
    }
    switch (event.type) {
      case "AUTH_RECOVERY_STARTED": {
        const phase = this.state.phase;
        if (phase.kind === "AUTHENTICATED" || phase.kind === "REFRESHING") {
          this.commit({
            kind: "REFRESHING",
            session_epoch: this.epoch,
            subject_key: phase.subject_key,
            username: phase.username,
            transition: phase.transition,
          });
        }
        break;
      }
      case "AUTH_RECOVERY_SUCCEEDED": {
        const phase = this.state.phase;
        if (phase.kind === "REFRESHING" && phase.subject_key === event.session.subject_key) {
          this.commit({
            kind: "AUTHENTICATED",
            session_epoch: this.epoch,
            subject_key: event.session.subject_key,
            username: event.session.username ?? phase.username,
            transition: phase.transition,
          });
        }
        break;
      }
      case "AUTH_SESSION_IDENTITY_CHANGED": {
        this.advanceEpoch();
        this.sessionGenerationId = randomSessionGenerationId();
        persistSharedSessionGeneration(this.sessionGenerationId);
        this.commit({
          kind: "AUTHENTICATED",
          session_epoch: this.epoch,
          subject_key: event.session.subject_key,
          username: event.session.username ?? "",
        });
        break;
      }
      case "SESSION_EXPIRED": {
        this.advanceEpoch();
        this.commit({ kind: "SESSION_EXPIRED", session_epoch: this.epoch, return_to: event.request_path });
        break;
      }
      case "AUTH_DISABLED": {
        this.advanceEpoch();
        this.commit({ kind: "AUTH_DISABLED", session_epoch: this.epoch });
        break;
      }
      case "AUTH_UNAVAILABLE": {
        if (this.scheduleAutoRecovery(event.reason, event.retry_after_seconds)) {
          break;
        }
        const spentRetries = this.autoRetryAttempts;
        const lastConfirmedAt = this.lastConfirmedAt;
        this.advanceEpoch();
        this.commit({
          kind: "AUTH_UNAVAILABLE",
          session_epoch: this.epoch,
          reason: event.reason,
          recovery_kind: event.recovery_kind,
          retry_after_seconds: event.retry_after_seconds,
          auto_retry_attempts: spentRetries,
          last_confirmed_at: lastConfirmedAt,
        });
        break;
      }
      case "AUTH_INCONSISTENT": {
        const lastConfirmedAt = this.lastConfirmedAt;
        this.advanceEpoch();
        this.commit({
          kind: "AUTH_UNAVAILABLE",
          session_epoch: this.epoch,
          reason: "disabled_but_unauthorized",
          recovery_kind: "public_auth_status",
          auto_retry_attempts: 0,
          last_confirmed_at: lastConfirmedAt,
        });
        break;
      }
      case "AUTH_TRANSITION_DETECTED": {
        this.advanceEpoch();
        this.commit({
          kind: "AUTH_TRANSITION_FAIL_CLOSED",
          session_epoch: this.epoch,
          transition_state: event.transition_state,
          effective_generation: event.effective_generation,
          retry_after_seconds: event.retry_after_seconds,
        });
        break;
      }
    }
  }

  // ---- singleflight refresh ----

  // ensurePassiveFlight returns the current-epoch flight for a background
  // (timer/visibility) refresh without emitting recovery events. A protected
  // 401 later promotes the same flight through ensureRecoveryFlight.
  ensurePassiveFlight(): { promise: Promise<RefreshOutcome>; created: boolean } {
    if (this.flight && this.flight.observedEpoch === this.epoch) {
      return { promise: this.flight.promise, created: false };
    }
    const observedEpoch = this.epoch;
    const flight: RefreshFlight = {
      mode: "passive",
      startedEventSent: false,
      observedEpoch,
      promise: (async () => {
        const outcome = await this.runRefresh(observedEpoch);
        this.settleFlight(observedEpoch, outcome);
        return outcome;
      })(),
    };
    this.flight = flight;
    return { promise: flight.promise, created: true };
  }

  // ensureRecoveryFlight returns the current-epoch flight, promoting a
  // passive flight to recovery exactly once and emitting
  // AUTH_RECOVERY_STARTED synchronously before the refresh is sent.
  ensureRecoveryFlight(): { promise: Promise<RefreshOutcome>; created: boolean } {
    if (this.flight && this.flight.observedEpoch === this.epoch) {
      if (this.flight.mode === "passive" && !this.flight.startedEventSent) {
        this.flight.mode = "recovery";
        this.flight.startedEventSent = true;
        this.dispatch({ type: "AUTH_RECOVERY_STARTED", observed_epoch: this.epoch });
      }
      return { promise: this.flight.promise, created: false };
    }
    const observedEpoch = this.epoch;
    const flight: RefreshFlight = {
      mode: "recovery",
      startedEventSent: true,
      observedEpoch,
      promise: (async () => {
        const outcome = await this.runRefresh(observedEpoch);
        this.settleFlight(observedEpoch, outcome);
        return outcome;
      })(),
    };
    this.flight = flight;
    this.dispatch({ type: "AUTH_RECOVERY_STARTED", observed_epoch: this.epoch });
    return { promise: flight.promise, created: true };
  }

  private async runRefresh(observedEpoch: number): Promise<RefreshOutcome> {
    const result = await this.options.refresh();
    if (!this.isCurrentEpoch(observedEpoch)) {
      return { kind: "AUTH_UNAVAILABLE", reason: "unexpected_response" };
    }
    const body = (result.body ?? {}) as { code?: string; details?: unknown; authenticated?: unknown; auth_enabled?: unknown; username?: unknown; subject_key?: unknown };
    return classifyRefreshOutcome(result.status, body as never, result.networkError, result.timedOut, result.retryAfterSeconds);
  }

  // settleFlight applies the singleflight settlement policy (SPEC §5.3).
  private settleFlight(observedEpoch: number, outcome: RefreshOutcome): void {
    if (!this.isCurrentEpoch(observedEpoch)) {
      return;
    }
    const flight = this.flight;
    if (!flight || flight.observedEpoch !== observedEpoch) {
      return;
    }
    const promoted = flight.mode === "recovery";
    switch (outcome.kind) {
      case "REFRESHED": {
        const phase = this.state.phase;
        const currentSubject = phase.kind === "AUTHENTICATED" || phase.kind === "REFRESHING" ? phase.subject_key : null;
        if (currentSubject !== null && currentSubject === outcome.session.subject_key) {
          if (promoted) {
            this.dispatch({ type: "AUTH_RECOVERY_SUCCEEDED", observed_epoch: observedEpoch, session: outcome.session });
          } else if (phase.kind === "AUTHENTICATED" && outcome.session.username !== null && outcome.session.username !== phase.username) {
            // Passive same-subject success may atomically update the display
            // username without advancing the epoch.
            this.commit({
              ...phase,
              username: outcome.session.username,
            });
          }
          this.flight = null;
          return;
        }
        if (promoted) {
          this.dispatch({
            type: "AUTH_SESSION_IDENTITY_CHANGED",
            observed_epoch: observedEpoch,
            session: outcome.session,
            evidence: "refresh_subject_changed",
          });
        }
        this.flight = null;
        return;
      }
      case "EXPIRED": {
        this.dispatch({
          type: "SESSION_EXPIRED",
          observed_epoch: observedEpoch,
          evidence: outcome.evidence,
          request_path: typeof window !== "undefined" ? window.location.pathname + window.location.search + window.location.hash : "/",
        });
        this.flight = null;
        return;
      }
      case "AUTH_DISABLED": {
        this.dispatch({ type: "AUTH_DISABLED", observed_epoch: observedEpoch });
        this.flight = null;
        return;
      }
      case "AUTH_TRANSITION_FAIL_CLOSED": {
        this.dispatch({
          type: "AUTH_TRANSITION_DETECTED",
          observed_epoch: observedEpoch,
          transition_state: outcome.transition_state,
          effective_generation: outcome.effective_generation,
          retry_after_seconds: outcome.retry_after_seconds,
          evidence: "refresh_503",
        });
        this.flight = null;
        return;
      }
      case "AUTH_UNAVAILABLE": {
        if (promoted) {
          this.dispatch({
            type: "AUTH_UNAVAILABLE",
            observed_epoch: observedEpoch,
            reason: outcome.reason,
            recovery_kind: "session_refresh",
            retry_after_seconds: outcome.retry_after_seconds,
          });
        }
        this.flight = null;
        return;
      }
    }
  }

  // ---- bootstrap integration ----

  // applyBootstrapStatus consumes the tagged public status union and
  // commits the corresponding phase (SPEC §4.2).
  applyBootstrapStatus(status: import("@/lib/types/auth").PublicAuthStatus, session: AuthenticatedSession | null, isPublicMode: boolean): void {
    void isPublicMode;
    if (!this.coordinatorReady) {
      return;
    }
    switch (status.state) {
      case "disabled": {
        if (status.transition_state !== null) {
          this.dispatch({
            type: "AUTH_UNAVAILABLE",
            observed_epoch: this.epoch,
            reason: "invalid_payload",
            recovery_kind: "public_bootstrap",
          });
          return;
        }
        const pendingGeneration = this.pendingAuthGeneration;
        this.clearCrossTabFence();
        this.advanceEpoch();
        this.sessionGenerationId = pendingGeneration ?? randomSessionGenerationId();
        persistSharedSessionGeneration(this.sessionGenerationId);
        this.commit({ kind: "AUTH_DISABLED", session_epoch: this.epoch });
        return;
      }
      case "enabled": {
        const transition: EnforcedAuthTransition | undefined =
          status.transition_state === "disabling_enforced"
            ? {
                transition_state: "disabling_enforced",
                effective_generation: status.effective_generation,
                retry_after_seconds: status.retry_after_seconds ?? undefined,
              }
            : undefined;
        if (session && session.subject_key) {
          const pendingGeneration = this.pendingAuthGeneration;
          this.clearCrossTabFence();
          this.advanceEpoch();
          // Bootstrap confirming the same session does not rotate the shared
          // generation (SPEC §4.3): only a genuinely different identity does,
          // and that only happens through explicit login or identity events.
          if (pendingGeneration) {
            this.sessionGenerationId = pendingGeneration;
            persistSharedSessionGeneration(this.sessionGenerationId);
          }
          this.commit({
            kind: "AUTHENTICATED",
            session_epoch: this.epoch,
            subject_key: session.subject_key,
            username: session.username ?? "",
            transition,
          });
          return;
        }
        const pendingGeneration = this.pendingAuthGeneration;
        this.clearCrossTabFence();
        this.advanceEpoch();
        this.sessionGenerationId = pendingGeneration ?? randomSessionGenerationId();
        persistSharedSessionGeneration(this.sessionGenerationId);
        this.commit({ kind: "ANONYMOUS", session_epoch: this.epoch, transition });
        return;
      }
      case "transition_fail_closed": {
        this.dispatch({
          type: "AUTH_TRANSITION_DETECTED",
          observed_epoch: this.epoch,
          transition_state: status.transition_state,
          effective_generation: status.effective_generation,
          retry_after_seconds: status.retry_after_seconds ?? undefined,
          evidence: "ordinary_management_503",
        });
        return;
      }
    }
  }

  // ---- login / logout integration ----

  applyLoginSuccess(session: AuthenticatedSession): void {
    this.advanceEpoch();
    this.sessionGenerationId = randomSessionGenerationId();
    persistSharedSessionGeneration(this.sessionGenerationId);
    this.commit({
      kind: "AUTHENTICATED",
      session_epoch: this.epoch,
      subject_key: session.subject_key,
      username: session.username ?? "",
    });
  }

  startLogout(intentId: string, originGeneration: string): void {
    this.state = {
      ...this.state,
      pending_logout_intent: {
        intent_id: intentId,
        origin_session_generation_id: originGeneration,
        state: "request_pending",
      },
    };
    this.advanceEpoch();
    this.commit({ kind: "LOGGING_OUT", session_epoch: this.epoch, state: "pending" });
  }

  confirmLogoutAnonymous(): void {
    this.advanceEpoch();
    this.sessionGenerationId = randomSessionGenerationId();
    persistSharedSessionGeneration(this.sessionGenerationId);
    this.state = { ...this.state, pending_logout_intent: null };
    this.commit({ kind: "ANONYMOUS", session_epoch: this.epoch });
  }

  confirmLogoutDisabled(): void {
    this.advanceEpoch();
    this.sessionGenerationId = randomSessionGenerationId();
    persistSharedSessionGeneration(this.sessionGenerationId);
    this.state = { ...this.state, pending_logout_intent: null };
    this.commit({ kind: "AUTH_DISABLED", session_epoch: this.epoch });
  }

  markLogoutUnconfirmed(retryAfterSeconds?: number): void {
    this.advanceEpoch();
    this.commit({
      kind: "LOGGING_OUT",
      session_epoch: this.epoch,
      state: "unconfirmed",
      retry_after_seconds: retryAfterSeconds,
    });
  }

  hasPendingLogoutIntent(): boolean {
    return this.state.pending_logout_intent !== null;
  }

  // ---- cross-tab fence ----

  beginCrossTabBootstrap(pendingGeneration: string): void {
    this.pendingAuthGeneration = pendingGeneration;
    this.pendingGenerationExpiry = null;
    this.advanceEpoch();
    this.commit({ kind: "BOOTSTRAPPING", session_epoch: this.epoch });
  }

  clearCrossTabFence(): void {
    this.pendingAuthGeneration = null;
    this.pendingGenerationExpiry = null;
  }

  queuePendingGenerationExpiry(event: AuthClientEvent): void {
    if (this.pendingAuthGeneration && !this.pendingGenerationExpiry) {
      this.pendingGenerationExpiry = event;
    }
  }

  settlePendingGenerationExpiry(): void {
    const queued = this.pendingGenerationExpiry;
    this.clearCrossTabFence();
    if (queued && queued.observed_epoch === this.epoch) {
      this.dispatch(queued);
    }
  }

  dropPendingGenerationExpiry(): void {
    this.pendingGenerationExpiry = null;
  }

  // ---- disabled-401 probe breaker ----

  // beginDisabledVerification enters AUTH_DISABLED_VERIFYING and runs the
  // single coordinator probe. Only a legal 2xx strict Models payload clears
  // the breaker; every other outcome exhausts the incident and keeps the
  // breaker closed until an explicit retry or a new authoritative
  // generation.
  beginDisabledVerification(effectiveGeneration: string): void {
    if (this.state.phase.kind === "AUTH_DISABLED_VERIFYING") {
      return;
    }
    const incidentId = randomIncidentId();
    // The inconsistency event already advanced the epoch; entering the
    // verifying phase is the same incident and must not advance again.
    this.commit({
      kind: "AUTH_DISABLED_VERIFYING",
      session_epoch: this.epoch,
      effective_generation: effectiveGeneration,
      incident_id: incidentId,
    });
    void this.runDisabledAccessProbe(effectiveGeneration, incidentId);
  }

  private async runDisabledAccessProbe(effectiveGeneration: string, incidentId: string): Promise<void> {
    if (!this.options.disabledAccessProbe) {
      this.enterDisabledProbeExhausted(effectiveGeneration, incidentId);
      return;
    }
    try {
      const outcome = await this.options.disabledAccessProbe();
      if (this.state.phase.kind !== "AUTH_DISABLED_VERIFYING") {
        return;
      }
      if (outcome === "confirmed") {
        this.confirmDisabledAccess();
        return;
      }
      this.enterDisabledProbeExhausted(effectiveGeneration, incidentId);
    } catch {
      this.enterDisabledProbeExhausted(effectiveGeneration, incidentId);
    }
  }

  enterDisabledVerifying(generation: string, incidentId: string): void {
    this.advanceEpoch();
    this.commit({
      kind: "AUTH_DISABLED_VERIFYING",
      session_epoch: this.epoch,
      effective_generation: generation,
      incident_id: incidentId,
    });
  }

  confirmDisabledAccess(): void {
    this.advanceEpoch();
    this.commit({ kind: "AUTH_DISABLED", session_epoch: this.epoch });
  }

  enterDisabledProbeExhausted(generation: string, incidentId: string): void {
    const lastConfirmedAt = this.lastConfirmedAt;
    this.advanceEpoch();
    this.commit({
      kind: "AUTH_UNAVAILABLE",
      session_epoch: this.epoch,
      reason: "disabled_but_unauthorized",
      recovery_kind: "public_auth_status",
      incident: { kind: "disabled_401_probe", state: "exhausted", effective_generation: generation, incident_id: incidentId },
      auto_retry_attempts: 0,
      last_confirmed_at: lastConfirmedAt,
    });
  }

  enterSessionExpired(returnTo: string): void {
    this.advanceEpoch();
    this.commit({ kind: "SESSION_EXPIRED", session_epoch: this.epoch, return_to: returnTo });
  }

  // ---- constants exposed for the probe machinery ----

  static readonly DISABLED_PROBE_TIMEOUT_MS = DISABLED_PROBE_TIMEOUT_MS;
  static readonly MAX_DISABLED_PROBE_WAITERS = MAX_DISABLED_PROBE_WAITERS;
}

function randomIncidentId(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `incident-${Date.now().toString(36)}`;
}
