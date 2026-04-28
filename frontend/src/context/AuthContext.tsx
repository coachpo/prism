import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ApiError, api } from "@/lib/api";
import type { SessionResponse } from "@/lib/types";
import {
  createAuthBootstrapLoader,
  type AuthBootstrapMode,
  type AuthBootstrapState,
} from "./auth/bootstrap";
import { createAuthMutations } from "./auth/mutations";
import {
  PROACTIVE_REFRESH_MS,
  runPassiveSessionRefresh as runPassiveSessionRefreshHelper,
  shouldRefreshOnVisibilityChange,
  shouldRunProactiveRefresh,
} from "./auth/refresh";
import { AUTH_STATE_BROADCAST_KEY } from "./auth/broadcast";
import { AuthContext, type AuthContextValue } from "./auth-context";

const loadAuthBootstrapState = createAuthBootstrapLoader({
  publicBootstrap: () => api.auth.publicBootstrap(),
  refresh: () => api.auth.refresh(),
  session: () => api.auth.session(),
  status: () => api.auth.status(),
});

export function AuthProvider({
  bootstrapMode = "full",
  children,
}: {
  bootstrapMode?: AuthBootstrapMode;
  children: ReactNode;
}) {
  const [authEnabled, setAuthEnabled] = useState(false);
  const [authenticated, setAuthenticated] = useState(false);
  const [username, setUsername] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const refreshTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const authStateVersionRef = useRef(0);
  const authMutationInFlightRef = useRef(false);
  const authEnabledRef = useRef(authEnabled);
  const authenticatedRef = useRef(authenticated);

  const applySessionState = useCallback((session: SessionResponse) => {
    authEnabledRef.current = session.auth_enabled;
    authenticatedRef.current = session.authenticated;
    setAuthEnabled(session.auth_enabled);
    setAuthenticated(session.authenticated);
    setUsername(session.username);
  }, []);

  const applyBootstrapState = useCallback((state: AuthBootstrapState) => {
    authEnabledRef.current = state.authEnabled;
    authenticatedRef.current = state.authenticated;
    setAuthEnabled(state.authEnabled);
    setAuthenticated(state.authenticated);
    setUsername(state.username);
  }, []);

  const runPassiveSessionRefresh = useCallback(async () => {
    const requestVersion = authStateVersionRef.current;

    await runPassiveSessionRefreshHelper({
      applySessionState,
      getAuthStateVersion: () => authStateVersionRef.current,
      isMutationInFlight: () => authMutationInFlightRef.current,
      refreshSession: () => api.auth.refresh(),
      requestVersion,
      resolveRefreshFailure: async (error) => {
        if (!(error instanceof ApiError) || error.status !== 401) {
          return null;
        }

        const status = await api.auth.status();
        return {
          auth_enabled: status.auth_enabled,
          authenticated: false,
          username: null,
        };
      },
    });
  }, [applySessionState]);

  const startRefreshTimer = useCallback(() => {
    if (refreshTimerRef.current) {
      clearInterval(refreshTimerRef.current);
    }
    refreshTimerRef.current = setInterval(() => {
      void runPassiveSessionRefresh();
    }, PROACTIVE_REFRESH_MS);
  }, [runPassiveSessionRefresh]);

  const stopRefreshTimer = useCallback(() => {
    if (refreshTimerRef.current) {
      clearInterval(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
  }, []);

  const beginAuthMutation = useCallback(() => {
    authMutationInFlightRef.current = true;
    authStateVersionRef.current += 1;
  }, []);

  const endAuthMutation = useCallback(() => {
    authMutationInFlightRef.current = false;
  }, []);

  const runAuthBootstrap = useCallback(async (reuseInFlight = false) => {
    const requestVersion = ++authStateVersionRef.current;
    setLoading(true);
    try {
      const state = await loadAuthBootstrapState(bootstrapMode, reuseInFlight);
      if (requestVersion !== authStateVersionRef.current) {
        return;
      }
      applyBootstrapState(state);
    } finally {
      if (requestVersion === authStateVersionRef.current) {
        setLoading(false);
      }
    }
  }, [applyBootstrapState, bootstrapMode]);

  const refreshAuth = useCallback(async () => {
    await runAuthBootstrap(false);
  }, [runAuthBootstrap]);

  // Start/stop proactive refresh timer based on auth state
  useEffect(() => {
    if (shouldRunProactiveRefresh(authenticated, authEnabled)) {
      startRefreshTimer();
    } else {
      stopRefreshTimer();
    }
    return () => stopRefreshTimer();
  }, [authenticated, authEnabled, startRefreshTimer, stopRefreshTimer]);

  // Refresh session when user returns to the tab after being away
  useEffect(() => {
    function handleVisibilityChange() {
      if (
        shouldRefreshOnVisibilityChange(
          document.visibilityState,
          authenticatedRef.current,
          authEnabledRef.current,
        )
      ) {
        void runPassiveSessionRefresh();
      }
    }
    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => document.removeEventListener("visibilitychange", handleVisibilityChange);
  }, [runPassiveSessionRefresh]);

  useEffect(() => {
    void runAuthBootstrap(true);
  }, [runAuthBootstrap]);

  useEffect(() => {
    function handleStorage(event: StorageEvent) {
      if (event.key !== AUTH_STATE_BROADCAST_KEY) {
        return;
      }

      void runAuthBootstrap(false);
    }

    window.addEventListener("storage", handleStorage);
    return () => window.removeEventListener("storage", handleStorage);
  }, [runAuthBootstrap]);

  const authMutations = useMemo(
    () =>
      createAuthMutations({
        loginRequest: api.auth.login,
        logoutRequest: api.auth.logout,
        setLoading,
        applySessionState,
        beginMutation: beginAuthMutation,
        endMutation: endAuthMutation,
      }),
    [applySessionState, beginAuthMutation, endAuthMutation]
  );

  const value = useMemo<AuthContextValue>(
    () => ({
      authEnabled,
      authenticated,
      loading,
      username,
      refreshAuth,
      login: authMutations.login,
      logout: authMutations.logout,
    }),
    [authEnabled, authenticated, loading, username, refreshAuth, authMutations]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
