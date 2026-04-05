import { lazy, Suspense, type ReactElement } from "react";
import { BrowserRouter, Navigate, Route, Routes, useLocation } from "react-router-dom";
import { AuthProvider } from "@/context/AuthContext";
import { useAuth } from "@/context/useAuth";
import { ProfileProvider } from "@/context/ProfileContext";
import { AppLayout } from "@/components/layout/AppLayout";
import { useLocale } from "@/i18n/useLocale";

const DashboardPage = lazy(() =>
  import("@/pages/DashboardPage").then((module) => ({ default: module.DashboardPage }))
);
const ModelsPage = lazy(() =>
  import("@/pages/ModelsPage").then((module) => ({ default: module.ModelsPage }))
);
const ModelDetailPage = lazy(() =>
  import("@/pages/ModelDetailPage").then((module) => ({ default: module.ModelDetailPage }))
);
const ProxyModelDetailPage = lazy(() =>
  import("@/pages/ProxyModelDetailPage").then((module) => ({ default: module.ProxyModelDetailPage }))
);
const EndpointsPage = lazy(() =>
  import("@/pages/EndpointsPage").then((module) => ({ default: module.EndpointsPage }))
);
const StatisticsPage = lazy(() =>
  import("@/pages/StatisticsPage").then((module) => ({ default: module.StatisticsPage }))
);
const SettingsPage = lazy(() =>
  import("@/pages/SettingsPage").then((module) => ({ default: module.SettingsPage }))
);
const PricingTemplatesPage = lazy(() =>
  import("@/pages/PricingTemplatesPage").then((module) => ({ default: module.PricingTemplatesPage }))
);
const LoadbalanceStrategiesPage = lazy(() =>
  import("@/pages/LoadbalanceStrategiesPage").then((module) => ({ default: module.LoadbalanceStrategiesPage }))
);
const ProxyApiKeysPage = lazy(() =>
  import("@/pages/ProxyApiKeysPage").then((module) => ({ default: module.ProxyApiKeysPage }))
);
const LoginPage = lazy(() =>
  import("@/pages/LoginPage").then((module) => ({ default: module.LoginPage }))
);
const ForgotPasswordPage = lazy(() =>
  import("@/pages/ForgotPasswordPage").then((module) => ({ default: module.ForgotPasswordPage }))
);
const ResetPasswordPage = lazy(() =>
  import("@/pages/ResetPasswordPage").then((module) => ({ default: module.ResetPasswordPage }))
);
const RequestLogsPage = lazy(() =>
  import("@/pages/RequestLogsPage").then((module) => ({ default: module.RequestLogsPage }))
);

const PUBLIC_AUTH_PATHS = new Set(["/login", "/forgot-password", "/reset-password"]);

function RouteFallback() {
  const { messages } = useLocale();

  return (
    <div className="py-10 text-center text-sm text-muted-foreground">
      {messages.common.loadingApplication}
    </div>
  );
}

function withRouteSuspense(element: ReactElement) {
  return <Suspense fallback={<RouteFallback />}>{element}</Suspense>;
}

function ProtectedAppShell() {
  const location = useLocation();
  const { authEnabled, authenticated, loading } = useAuth();

  if (loading) {
    return <RouteFallback />;
  }

  if (authEnabled && !authenticated) {
    return <Navigate to="/login" replace state={{ from: location }} />;
  }

  return (
    <ProfileProvider>
      <AppLayout />
    </ProfileProvider>
  );
}

function PublicOnlyRoute({ children }: { children: ReactElement }) {
  const { authEnabled, authenticated, loading } = useAuth();

  if (loading) {
    return <RouteFallback />;
  }

  if (!authEnabled || authenticated) {
    return <Navigate to="/dashboard" replace />;
  }

  return children;
}

function RoutedAuthProvider({ children }: { children: ReactElement }) {
  const location = useLocation();
  const bootstrapMode = PUBLIC_AUTH_PATHS.has(location.pathname) ? "public" : "full";

  return <AuthProvider bootstrapMode={bootstrapMode}>{children}</AuthProvider>;
}

function App() {
  return (
    <BrowserRouter>
      <RoutedAuthProvider>
        <Routes>
          <Route path="/login" element={<PublicOnlyRoute>{withRouteSuspense(<LoginPage />)}</PublicOnlyRoute>} />
          <Route path="/forgot-password" element={<PublicOnlyRoute>{withRouteSuspense(<ForgotPasswordPage />)}</PublicOnlyRoute>} />
          <Route path="/reset-password" element={<PublicOnlyRoute>{withRouteSuspense(<ResetPasswordPage />)}</PublicOnlyRoute>} />
          <Route element={<ProtectedAppShell />}>
            <Route path="/" element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard" element={withRouteSuspense(<DashboardPage />)} />
            <Route path="/models" element={withRouteSuspense(<ModelsPage />)} />
            <Route path="/models/:id/proxy" element={withRouteSuspense(<ProxyModelDetailPage />)} />
            <Route path="/models/:id" element={withRouteSuspense(<ModelDetailPage />)} />
            <Route path="/endpoints" element={withRouteSuspense(<EndpointsPage />)} />
            <Route path="/loadbalance-strategies" element={withRouteSuspense(<LoadbalanceStrategiesPage />)} />
            <Route path="/statistics" element={withRouteSuspense(<StatisticsPage />)} />
            <Route path="/settings" element={withRouteSuspense(<SettingsPage />)} />
            <Route path="/proxy-api-keys" element={withRouteSuspense(<ProxyApiKeysPage />)} />
            <Route path="/pricing-templates" element={withRouteSuspense(<PricingTemplatesPage />)} />
            <Route path="/request-logs" element={withRouteSuspense(<RequestLogsPage />)} />
          </Route>
        </Routes>
      </RoutedAuthProvider>
    </BrowserRouter>
  );
}

export default App;
