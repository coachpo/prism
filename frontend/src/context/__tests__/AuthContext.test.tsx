import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createAuthBootstrapLoader } from "../auth/bootstrap";
import { runPassiveSessionRefresh } from "../auth/refresh";
import { AuthProvider } from "../AuthContext";
import { useAuth } from "../useAuth";

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const api = vi.hoisted(() => ({
  auth: {
    login: vi.fn(),
    logout: vi.fn(),
    publicBootstrap: vi.fn(),
    refresh: vi.fn(),
    session: vi.fn(),
    status: vi.fn(),
  },
}));

vi.mock("@/lib/api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/api")>("@/lib/api");
  return {
    ...actual,
    api,
  };
});

function AuthProbe() {
  const { authenticated, loading, login, logout, username } = useAuth();

  return (
    <div>
      <div>{loading ? "loading" : authenticated ? username ?? "authenticated" : "guest"}</div>
      <button type="button" onClick={() => void login("alice", "secret", "session")}>
        Login
      </button>
      <button type="button" onClick={() => void logout()}>
        Logout
      </button>
    </div>
  );
}

describe("AuthProvider", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.auth.status.mockResolvedValue({ auth_enabled: true });
    api.auth.publicBootstrap.mockResolvedValue({ auth_enabled: true, authenticated: false, username: null });
    api.auth.session.mockResolvedValue({ auth_enabled: true, authenticated: false, username: null });
    api.auth.login.mockResolvedValue({ auth_enabled: true, authenticated: true, username: "alice" });
    api.auth.logout.mockResolvedValue({ auth_enabled: true, authenticated: false, username: null });
    api.auth.refresh.mockResolvedValue({ auth_enabled: true, authenticated: false, username: null });
  });

  afterEach(() => {
    cleanup();
  });

  it("ignores stale bootstrap responses after login wins the race", async () => {
    const deferredSession = createDeferred<{ auth_enabled: boolean; authenticated: boolean; username: string | null }>();
    api.auth.session.mockImplementationOnce(() => deferredSession.promise);

    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(api.auth.status).toHaveBeenCalledTimes(1);
      expect(api.auth.session).toHaveBeenCalledTimes(1);
    });

    fireEvent.click(screen.getByRole("button", { name: "Login" }));

    await waitFor(() => {
      expect(screen.getByText("alice")).toBeInTheDocument();
    });

    deferredSession.resolve({ auth_enabled: true, authenticated: false, username: null });

    await waitFor(() => {
      expect(screen.getByText("alice")).toBeInTheDocument();
    });

    expect(api.auth.login).toHaveBeenCalledTimes(1);
  });

  it("ignores stale visibility refresh responses after logout wins the race", async () => {
    const deferredRefresh = createDeferred<{
      auth_enabled: boolean;
      authenticated: boolean;
      username: string | null;
    }>();

    api.auth.session.mockResolvedValueOnce({
      auth_enabled: true,
      authenticated: true,
      username: "alice",
    });
    api.auth.refresh.mockImplementationOnce(() => deferredRefresh.promise);

    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => "visible",
    });

    render(
      <AuthProvider>
        <AuthProbe />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("alice")).toBeInTheDocument();
    });

    fireEvent(document, new Event("visibilitychange"));

    await waitFor(() => {
      expect(api.auth.refresh).toHaveBeenCalledTimes(1);
    });

    fireEvent.click(screen.getByRole("button", { name: "Logout" }));

    await waitFor(() => {
      expect(screen.getByText("guest")).toBeInTheDocument();
    });

    deferredRefresh.resolve({
      auth_enabled: true,
      authenticated: true,
      username: "alice",
    });

    await waitFor(() => {
      expect(screen.getByText("guest")).toBeInTheDocument();
    });

    expect(api.auth.logout).toHaveBeenCalledTimes(1);
  });

  it("uses the public bootstrap path in public mode", async () => {
    api.auth.publicBootstrap.mockResolvedValueOnce({
      auth_enabled: true,
      authenticated: true,
      username: "alice",
    });

    render(
      <AuthProvider bootstrapMode="public">
        <AuthProbe />
      </AuthProvider>,
    );

    await waitFor(() => {
      expect(screen.getByText("alice")).toBeInTheDocument();
    });

    expect(api.auth.publicBootstrap).toHaveBeenCalledTimes(1);
    expect(api.auth.status).not.toHaveBeenCalled();
    expect(api.auth.session).not.toHaveBeenCalled();
  });

  it("reuses the in-flight bootstrap request for the same mode", async () => {
    const publicBootstrap = vi.fn().mockResolvedValue({
      auth_enabled: true,
      authenticated: false,
      username: null,
    });
    const loadBootstrapState = createAuthBootstrapLoader({
      publicBootstrap,
      refresh: vi.fn(),
      session: vi.fn(),
      status: vi.fn(),
    });

    const [first, second] = await Promise.all([
      loadBootstrapState("public", true),
      loadBootstrapState("public", true),
    ]);

    expect(first).toEqual(second);
    expect(publicBootstrap).toHaveBeenCalledTimes(1);
  });

  it("does not apply a passive refresh result after the auth version changes", async () => {
    const deferredRefresh = createDeferred<{
      auth_enabled: boolean;
      authenticated: boolean;
      username: string | null;
    }>();
    const applySessionState = vi.fn();
    const authState = { version: 1, mutationInFlight: false };

    const refreshPromise = runPassiveSessionRefresh({
      applySessionState,
      getAuthStateVersion: () => authState.version,
      isMutationInFlight: () => authState.mutationInFlight,
      refreshSession: () => deferredRefresh.promise,
      requestVersion: authState.version,
    });

    authState.version = 2;
    deferredRefresh.resolve({
      auth_enabled: true,
      authenticated: true,
      username: "alice",
    });

    await refreshPromise;

    expect(applySessionState).not.toHaveBeenCalled();
  });

  it("runs the extracted login mutation workflow with the existing session payload", async () => {
    const { createAuthMutations } = await import("../auth/mutations");
    const setLoading = vi.fn();
    const applySessionState = vi.fn();
    const mutationSteps: string[] = [];
    const mutations = createAuthMutations({
      loginRequest: api.auth.login,
      logoutRequest: api.auth.logout,
      setLoading,
      applySessionState,
      beginMutation: () => mutationSteps.push("begin"),
      endMutation: () => mutationSteps.push("end"),
    });

    await mutations.login("alice", "secret", "session");

    expect(api.auth.login).toHaveBeenCalledWith({
      username: "alice",
      password: "secret",
      session_duration: "session",
    });
    expect(setLoading).toHaveBeenCalledWith(false);
    expect(applySessionState).toHaveBeenCalledWith({
      auth_enabled: true,
      authenticated: true,
      username: "alice",
    });
    expect(mutationSteps).toEqual(["begin", "end"]);
  });

  it("runs the extracted logout mutation workflow with the existing session payload", async () => {
    const { createAuthMutations } = await import("../auth/mutations");
    const setLoading = vi.fn();
    const applySessionState = vi.fn();
    const mutationSteps: string[] = [];
    const mutations = createAuthMutations({
      loginRequest: api.auth.login,
      logoutRequest: api.auth.logout,
      setLoading,
      applySessionState,
      beginMutation: () => mutationSteps.push("begin"),
      endMutation: () => mutationSteps.push("end"),
    });

    await mutations.logout();

    expect(api.auth.logout).toHaveBeenCalledTimes(1);
    expect(setLoading).toHaveBeenCalledWith(false);
    expect(applySessionState).toHaveBeenCalledWith({
      auth_enabled: true,
      authenticated: false,
      username: null,
    });
    expect(mutationSteps).toEqual(["begin", "end"]);
  });
});
