import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { SHELL_ROUTE_METADATA } from "@/components/layout/app-layout/navigationProfileConfig";
import { LocaleProvider } from "@/i18n/LocaleProvider";

const { authState } = vi.hoisted(() => ({
  authState: {
    authEnabled: true,
    authenticated: true,
    loading: false,
  },
}));

vi.mock("@/context/useAuth", () => ({
  useAuth: () => ({
    authEnabled: authState.authEnabled,
    authenticated: authState.authenticated,
    loading: authState.loading,
    login: vi.fn(),
  }),
}));

vi.mock("@/context/AuthContext", () => ({
  AuthProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@/context/ProfileContext", () => ({
  ProfileProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/layout/AppLayout", async () => {
  const { Outlet } = await vi.importActual<typeof import("react-router-dom")>("react-router-dom");

  return {
    AppLayout: () => (
      <div>
        <div>protected-shell</div>
        <Outlet />
      </div>
    ),
  };
});

vi.mock("@/pages/DashboardPage", () => ({
  DashboardPage: () => <div>dashboard-landmark</div>,
}));

vi.mock("@/pages/ModelsPage", () => ({
  ModelsPage: () => <div>models-landmark</div>,
}));

vi.mock("@/pages/ModelDetailPage", () => ({
  ModelDetailPage: () => <div>model-detail-landmark</div>,
}));

vi.mock("@/pages/ProxyModelDetailPage", () => ({
  ProxyModelDetailPage: () => <div>proxy-model-detail-landmark</div>,
}));

vi.mock("@/pages/EndpointsPage", () => ({
  EndpointsPage: () => <div>endpoints-landmark</div>,
}));

vi.mock("@/pages/LoadbalanceStrategiesPage", () => ({
  LoadbalanceStrategiesPage: () => <div>loadbalance-strategies-landmark</div>,
}));

vi.mock("@/pages/StatisticsPage", () => ({
  StatisticsPage: () => <div>statistics-landmark</div>,
}));

vi.mock("@/pages/SettingsPage", () => ({
  SettingsPage: () => <div>settings-landmark</div>,
}));

vi.mock("@/pages/ProxyApiKeysPage", () => ({
  ProxyApiKeysPage: () => <div>proxy-api-keys-landmark</div>,
}));

vi.mock("@/pages/PricingTemplatesPage", () => ({
  PricingTemplatesPage: () => <div>pricing-templates-landmark</div>,
}));

vi.mock("@/pages/RequestLogsPage", () => ({
  RequestLogsPage: () => <div>request-logs-landmark</div>,
}));

async function renderAppRoute(path: string) {
  const AppModule = await import("@/App");

  window.history.pushState({}, "", path);

  return render(
    <LocaleProvider>
      <AppModule.default />
    </LocaleProvider>,
  );
}

describe("App protected route smoke", () => {
  beforeEach(() => {
    authState.authEnabled = true;
    authState.authenticated = true;
    authState.loading = false;
    window.history.replaceState({}, "", "/dashboard");
  });

  it("redirects the root route into the dashboard shell", async () => {
    const view = await renderAppRoute("/");

    expect(await screen.findByText("protected-shell")).toBeInTheDocument();
    expect(await screen.findByText("dashboard-landmark")).toBeInTheDocument();
    expect(window.location.pathname).toBe("/dashboard");

    view.unmount();
  });

  it("redirects /login into the dashboard shell when auth is disabled", async () => {
    authState.authEnabled = false;
    authState.authenticated = false;

    const view = await renderAppRoute("/login");

    expect(await screen.findByText("protected-shell")).toBeInTheDocument();
    expect(await screen.findByText("dashboard-landmark")).toBeInTheDocument();
    expect(window.location.pathname).toBe("/dashboard");

    view.unmount();
  });

  it.each([
    ["/dashboard", "dashboard-landmark"],
    ["/models", "models-landmark"],
    ["/models/5", "model-detail-landmark"],
    ["/models/5/proxy", "proxy-model-detail-landmark"],
    ["/endpoints", "endpoints-landmark"],
    ["/loadbalance-strategies", "loadbalance-strategies-landmark"],
    ["/statistics", "statistics-landmark"],
    ["/settings", "settings-landmark"],
    ["/proxy-api-keys", "proxy-api-keys-landmark"],
    ["/pricing-templates", "pricing-templates-landmark"],
    ["/request-logs", "request-logs-landmark"],
  ])("renders the protected shell and page landmark for %s", async (path, landmark) => {
    const view = await renderAppRoute(path);

    expect(await screen.findByText("protected-shell")).toBeInTheDocument();
    expect(await screen.findByText(landmark)).toBeInTheDocument();
    expect(window.location.pathname).toBe(path);

    view.unmount();
  });

  it("keeps shell route metadata aligned with the protected route surface", () => {
    expect(SHELL_ROUTE_METADATA.map((route) => route.pathPattern)).toEqual([
      "/dashboard",
      "/models",
      "/models/:id/proxy",
      "/models/:id",
      "/endpoints",
      "/loadbalance-strategies",
      "/statistics",
      "/settings",
      "/proxy-api-keys",
      "/pricing-templates",
      "/request-logs",
    ]);
  });
});
