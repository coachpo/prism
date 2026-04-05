import { ApiError } from "@/lib/api";

export interface AuthBootstrapState {
  authEnabled: boolean;
  authenticated: boolean;
  username: string | null;
}

export type AuthBootstrapMode = "full" | "public";

export interface AuthBootstrapApi {
  publicBootstrap: () => Promise<{
    auth_enabled: boolean;
    authenticated: boolean;
    username: string | null;
  }>;
  refresh: () => Promise<{
    auth_enabled: boolean;
    authenticated: boolean;
    username: string | null;
  }>;
  session: () => Promise<{
    auth_enabled: boolean;
    authenticated: boolean;
    username: string | null;
  }>;
  status: () => Promise<{ auth_enabled: boolean }>;
}

function mapSessionToBootstrapState(session: {
  auth_enabled: boolean;
  authenticated: boolean;
  username: string | null;
}): AuthBootstrapState {
  return {
    authEnabled: session.auth_enabled,
    authenticated: session.authenticated,
    username: session.username,
  };
}

export function createAuthBootstrapLoader(api: AuthBootstrapApi) {
  const authBootstrapPromises = new Map<AuthBootstrapMode, Promise<AuthBootstrapState>>();

  return async function loadAuthBootstrapState(
    mode: AuthBootstrapMode,
    reuseInFlight = false,
  ): Promise<AuthBootstrapState> {
    const inFlight = authBootstrapPromises.get(mode);
    if (reuseInFlight && inFlight) {
      return inFlight;
    }

    const loadPromise = (async (): Promise<AuthBootstrapState> => {
      if (mode === "public") {
        return mapSessionToBootstrapState(await api.publicBootstrap());
      }

      const status = await api.status();
      if (!status.auth_enabled) {
        return {
          authEnabled: false,
          authenticated: false,
          username: null,
        };
      }

      try {
        return mapSessionToBootstrapState(await api.session());
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) {
          try {
            return mapSessionToBootstrapState(await api.refresh());
          } catch (refreshError) {
            if (refreshError instanceof ApiError && refreshError.status === 401) {
              return {
                authEnabled: true,
                authenticated: false,
                username: null,
              };
            }

            throw refreshError;
          }
        }

        throw error;
      }
    })();

    if (reuseInFlight) {
      authBootstrapPromises.set(mode, loadPromise);
      void loadPromise.finally(() => {
        if (authBootstrapPromises.get(mode) === loadPromise) {
          authBootstrapPromises.delete(mode);
        }
      });
    }

    return loadPromise;
  };
}
