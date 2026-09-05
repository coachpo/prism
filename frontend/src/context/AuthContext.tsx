import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ApiError, api } from "@/lib/api";
import type { AuthenticatedSession, PublicAuthStatus } from "@/lib/types";
import { authSessionCoordinator } from "@/context/auth/coordinatorInstance";
import { AuthPhaseChangedError, StaleSessionEpochError } from "@/context/auth/sessionCoordinator";
import {
  AUTH_STATE_BROADCAST_KEY,
  BroadcastDedupe,
  parseBroadcastPayload,
  sessionExpiredEventForCrossTab,
  broadcastAuthStateChange,
} from "@/context/auth/crossTab";
import type { AuthUnavailableReason } from "@/context/auth/sessionCoordinator";
import { AuthContext, type AuthContextValue } from "./auth-context";
import {
  PROACTIVE_REFRESH_MS,
  shouldRefreshOnVisibilityChange,
  shouldRunProactiveRefresh,
} from "@/context/auth/refresh";

// The bootstrap read fails for two different kinds of reason and the
// coordinator's recovery policy depends on telling them apart: 403 is an
// answer, a 5xx or a dead connection is not. `bootstrap_failed` stays the
// catch-all for anything that is neither.
function classifyBootstrapFailure(error: unknown): AuthUnavailableReason {
  if (error instanceof ApiError) {
    if (error.status === 403) return "forbidden";
    if (error.status === 429) return "rate_limited";
    if (error.status >= 500) return "server";
    return "bootstrap_failed";
  }
  // fetch rejects with a TypeError when the request never reached the server.
  if (error instanceof TypeError) return "network";
  return "bootstrap_failed";
}

// The React provider is a render subscriber of the process-local session
// coordinator; it never commits phases itself.
export function AuthProvider({
  bootstrapMode = "full",
  children,
}: {
  bootstrapMode?: "full" | "public";
  children: ReactNode;
}) {
  const [snapshot, setSnapshot] = useState(() => authSessionCoordinator.getState());
  const refreshTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const authMutationInFlightRef = useRef(false);
  const dedupeRef = useRef(new BroadcastDedupe());

  // Subscribe to the coordinator store (render-only subscriber).
  useEffect(() => {
    const unsubscribe = authSessionCoordinator.subscribe(() => {
      setSnapshot(authSessionCoordinator.getState());
    });
    return unsubscribe;
  }, []);

  const phase = snapshot.phase;

  // Derive the legacy boolean surface for existing callers: auth is enabled
  // whenever the instance enforces operator auth (every phase except the
  // confirmed disabled mode and the bootstrapping window).
  const authEnabled = phase.kind !== "AUTH_DISABLED" && phase.kind !== "BOOTSTRAPPING";
  const authenticated = phase.kind === "AUTHENTICATED" || phase.kind === "REFRESHING";
  const loading = phase.kind === "BOOTSTRAPPING" || phase.kind === "AUTH_DISABLED_VERIFYING";
  const username = phase.kind === "AUTHENTICATED" || phase.kind === "REFRESHING" ? phase.username : null;

  // Bootstrap: read the tagged public status and the session, then commit
  // through the coordinator.
  const runAuthBootstrap = useCallback(
    async (reuseInFlight = false) => {
      void reuseInFlight;
      const status = await api.auth.status();
      let session: AuthenticatedSession | null = null;
      if (status.state === "enabled") {
        try {
          const sessionResponse = await api.auth.session();
          if (sessionResponse.authenticated) {
            session = sessionResponse;
          }
        } catch (error) {
          if (error instanceof ApiError && error.status === 401) {
            // No valid session: anonymous is fine.
          } else if (error instanceof AuthPhaseChangedError) {
            return;
          } else {
            throw error;
          }
        }
      }
      authSessionCoordinator.applyBootstrapStatus(status as PublicAuthStatus, session, bootstrapMode === "public");
    },
    [bootstrapMode],
  );

  // A failed bootstrap is reported with the reason it actually failed for:
  // the coordinator retries a transient backend fault on its own and only
  // fails closed on an authorization answer.
  const reportBootstrapFailure = useCallback((error: unknown) => {
    if (error instanceof AuthPhaseChangedError || error instanceof StaleSessionEpochError) {
      return;
    }
    authSessionCoordinator.dispatch({
      type: "AUTH_UNAVAILABLE",
      observed_epoch: authSessionCoordinator.getEpoch(),
      reason: classifyBootstrapFailure(error),
      recovery_kind: "public_bootstrap",
      retry_after_seconds:
        error instanceof ApiError && error.retryAfterMs !== null
          ? Math.max(0, Math.round(error.retryAfterMs / 1000))
          : undefined,
    });
  }, []);

  // Run the initial bootstrap once.
  useEffect(() => {
    let active = true;
    void runAuthBootstrap(true).catch((error: unknown) => {
      if (!active) {
        return;
      }
      reportBootstrapFailure(error);
    });
    return () => {
      active = false;
    };
  }, [reportBootstrapFailure, runAuthBootstrap]);

  // Let the coordinator re-run the bootstrap read itself while it is backing
  // off, so a 503 on /api/auth/status no longer costs a manual click.
  useEffect(() => {
    authSessionCoordinator.setRecoveryRunner(() => {
      void runAuthBootstrap(false).catch(reportBootstrapFailure);
    });
    return () => authSessionCoordinator.setRecoveryRunner(null);
  }, [reportBootstrapFailure, runAuthBootstrap]);

  const refreshAuth = useCallback(async () => {
    await runAuthBootstrap(false);
  }, [runAuthBootstrap]);

  // Passive refresh shares the coordinator's singleflight policy: a
  // background refresh never races a protected-401 recovery and may be
  // promoted to recovery by an eligible 401.
  const runPassiveSessionRefresh = useCallback(async () => {
    if (authMutationInFlightRef.current) {
      return;
    }
    const phaseBefore = authSessionCoordinator.getPhase();
    if (phaseBefore.kind !== "AUTHENTICATED" && phaseBefore.kind !== "REFRESHING") {
      return;
    }
    await authSessionCoordinator.ensurePassiveFlight().promise;
  }, []);

  // Start/stop proactive refresh timer based on auth state.
  useEffect(() => {
    if (shouldRunProactiveRefresh(authenticated, authEnabled)) {
      if (refreshTimerRef.current) {
        clearInterval(refreshTimerRef.current);
      }
      refreshTimerRef.current = setInterval(() => {
        void runPassiveSessionRefresh();
      }, PROACTIVE_REFRESH_MS);
    } else {
      if (refreshTimerRef.current) {
        clearInterval(refreshTimerRef.current);
        refreshTimerRef.current = null;
      }
    }
    return () => {
      if (refreshTimerRef.current) {
        clearInterval(refreshTimerRef.current);
        refreshTimerRef.current = null;
      }
    };
  }, [authenticated, authEnabled, runPassiveSessionRefresh]);

  // Refresh session when the user returns to the tab.
  useEffect(() => {
    function handleVisibilityChange() {
      if (shouldRefreshOnVisibilityChange(document.visibilityState, authenticated, authEnabled)) {
        void runPassiveSessionRefresh();
      }
    }
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => document.removeEventListener("visibilitychange", handleVisibilityChange);
  }, [authenticated, authEnabled, runPassiveSessionRefresh]);

  // Cross-tab auth broadcast: validate, dedupe, then run the coordinator
  // fence/expiry semantics (SPEC §11).
  useEffect(() => {
    function handleStorage(event: StorageEvent) {
      if (event.key !== AUTH_STATE_BROADCAST_KEY) {
        return;
      }
      const payload = parseBroadcastPayload(event.newValue);
      if (!payload) {
        return;
      }
      const dedupe = dedupeRef.current;
      if (dedupe.seen(payload.event_id) || dedupe.outOfOrder(payload.origin_tab_id, payload.sequence)) {
        return;
      }
      if (payload.kind === "session_expired") {
        if (authSessionCoordinator.hasPendingLogoutIntent()) {
          // Logout intent in progress: the expiry settles the logout-aware
          // confirmation, never an expired blocker.
          authSessionCoordinator.markLogoutUnconfirmed();
          return;
        }
        const currentGeneration = authSessionCoordinator.getSessionGenerationId();
        if (payload.session_generation_id !== currentGeneration) {
          return;
        }
        if (dedupe.terminalConsumed(currentGeneration, payload.event_id)) {
          return;
        }
        authSessionCoordinator.dispatch(sessionExpiredEventForCrossTab(authSessionCoordinator.getEpoch()));
        return;
      }
      if (payload.kind === "auth_changed") {
        const target = payload.target_generation ?? payload.session_generation_id;
        authSessionCoordinator.beginCrossTabBootstrap(target);
        void runAuthBootstrap(false);
      }
    }

    window.addEventListener("storage", handleStorage);
    return () => window.removeEventListener("storage", handleStorage);
  }, [runAuthBootstrap]);

  // Broadcast local definitive transitions to other tabs.
  useEffect(() => {
    if (phase.kind === "SESSION_EXPIRED") {
      broadcastAuthStateChange(authSessionCoordinator.getSessionGenerationId(), "session_expired");
    }
  }, [phase.kind]);

  const login = useCallback(
    async (usernameValue: string, password: string, sessionDuration: import("@/lib/types/auth").LoginSessionDuration) => {
      authMutationInFlightRef.current = true;
      try {
        const session = await api.auth.login({ username: usernameValue, password, session_duration: sessionDuration });
        if (!session.authenticated) {
          throw new ApiError("登录未完成", 200, session);
        }
        authSessionCoordinator.applyLoginSuccess(session);
      } finally {
        authMutationInFlightRef.current = false;
      }
    },
    [],
  );

  const logout = useCallback(async () => {
    authMutationInFlightRef.current = true;
    const intentId = crypto?.randomUUID?.() ?? `logout-${Date.now().toString(36)}`;
    try {
      authSessionCoordinator.startLogout(intentId, authSessionCoordinator.getSessionGenerationId());
      try {
        await api.auth.logout();
        authSessionCoordinator.confirmLogoutAnonymous();
      } catch (error) {
        if (error instanceof ApiError && error.status === 400 && error.code === "auth_not_enabled") {
          await runAuthBootstrap(false);
          if (authSessionCoordinator.getPhase().kind === "AUTH_DISABLED") {
            authSessionCoordinator.confirmLogoutDisabled();
          }
          return;
        }
        const retryAfterSeconds = error instanceof ApiError && error.retryAfterMs !== null
          ? Math.max(0, Math.round(error.retryAfterMs / 1000))
          : undefined;
        authSessionCoordinator.markLogoutUnconfirmed(retryAfterSeconds);
      }
    } finally {
      authMutationInFlightRef.current = false;
    }
  }, [runAuthBootstrap]);

  const retryLogout = useCallback(async () => {
    authMutationInFlightRef.current = true;
    try {
      try {
        await api.auth.logout();
        authSessionCoordinator.confirmLogoutAnonymous();
      } catch (error) {
        if (error instanceof ApiError && error.status === 400 && error.code === "auth_not_enabled") {
          await runAuthBootstrap(false);
          if (authSessionCoordinator.getPhase().kind === "AUTH_DISABLED") {
            authSessionCoordinator.confirmLogoutDisabled();
          }
          return;
        }
        authSessionCoordinator.markLogoutUnconfirmed();
      }
    } finally {
      authMutationInFlightRef.current = false;
    }
  }, [runAuthBootstrap]);

  const value = useMemo<AuthContextValue>(
    () => ({
      authEnabled,
      authenticated,
      loading,
      username,
      phase,
      refreshAuth,
      login,
      logout,
      retryLogout,
      retryRecovery: async () => {
        // A manual retry that fails must land back on the coordinator: the
        // gate would otherwise keep reporting the previous attempt forever.
        await runAuthBootstrap(false).catch(reportBootstrapFailure);
      },
    }),
    [authEnabled, authenticated, loading, username, phase, refreshAuth, login, logout, retryLogout, reportBootstrapFailure, runAuthBootstrap],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
