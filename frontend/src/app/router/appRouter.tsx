import { lazy, Suspense, type ReactElement, type ReactNode } from "react"
import { Navigate as CompatNavigate, Route, Routes, useLocation } from "react-router-dom"
import {
  Navigate,
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  createBrowserHistory,
  useNavigate as useTanStackNavigate,
  useParams as useTanStackParams,
  useSearch as useTanStackSearch,
  type RouterHistory,
} from "@tanstack/react-router"
import { AuthProvider } from "@/context/AuthContext"
import { ReportingCurrencyProvider } from "@/context/ReportingCurrencyContext"
import { useAuth } from "@/context/useAuth"
import { Page } from "@/components/layout/page"
import { useLocale } from "@/i18n/useLocale"
import { OperatorLoadingState } from "@/shared/design-system"
import {
  emptySearchSchema,
  observeSearchSchema,
  requestAuditSearchSchema,
  requestLogSearchSchema,
  settingsSearchSchema,
} from "./rewriteRoutes"
import { modelDetailSearchSchema } from "@/features/models/detail/modelDetailSchemas"
import { resolveProtectedRedirect, resolvePublicRedirect } from "./authGates"

const ObservePage = lazy(() => import("@/features/observe/ObservePage"))
const ModelsPage = lazy(() => import("@/features/models/ModelsFeaturePage"))
const ModelDetailFeaturePage = lazy(() => import("@/features/models/detail/ModelDetailFeaturePage"))
const EndpointsPage = lazy(() => import("@/features/endpoints/EndpointsFeaturePage"))
const SettingsFeaturePage = lazy(() => import("@/features/settings/SettingsFeaturePage"))
const PricingTemplatesPage = lazy(() => import("@/features/pricing/PricingFeaturePage"))
const BanPoliciesFeaturePage = lazy(() => import("@/features/loadbalance/BanPoliciesFeaturePage"))
const ProxyApiKeysPage = lazy(() => import("@/features/proxy-keys/ProxyKeysFeaturePage"))
const LoginPage = lazy(() => import("@/pages/LoginPage").then((module) => ({ default: module.LoginPage })))
const RequestLogsPage = lazy(() => import("@/features/request-logs/RequestLogsFeaturePage"))
const RequestLogAuditPage = lazy(() => import("@/features/request-logs/RequestLogAuditFeaturePage"))

export const PUBLIC_AUTH_PATHS = new Set(["/auth/login"])

export function RouteFallback() {
  const { messages } = useLocale()

  return (
    <main className="flex min-h-64 items-center justify-center p-6">
      <OperatorLoadingState
        className="w-full max-w-md"
        title={messages.common.loadingApplication}
      />
    </main>
  )
}

function withRouteSuspense(element: ReactElement) {
  return <Suspense fallback={<RouteFallback />}>{element}</Suspense>
}

function NotFoundRoute() {
  const { messages } = useLocale()

  return (
    <main className="flex min-h-svh items-center justify-center bg-background px-6 text-foreground">
      <div className="max-w-md text-center">
        <h1 className="text-2xl font-semibold tracking-tight">{messages.common.pageNotFound}</h1>
      </div>
    </main>
  )
}

export function RoutedAuthProvider({ children }: { children: ReactNode }) {
  const location = useLocation()
  const bootstrapMode = PUBLIC_AUTH_PATHS.has(location.pathname) ? "public" : "full"

  return <AuthProvider bootstrapMode={bootstrapMode}>{children}</AuthProvider>
}
function ProtectedRoute({ children }: { children: ReactElement }) {
  const { authEnabled, authenticated, loading } = useAuth()

  if (loading) {
    return <RouteFallback />
  }

  const redirect = resolveProtectedRedirect(
    { authEnabled, authenticated, loading },
    {
      pathname: window.location.pathname,
      search: window.location.search,
      hash: window.location.hash,
    },
  )
  if (redirect) {
    return <CompatNavigate to={redirect.to} replace state={redirect.state} />
  }

  return (
    <ReportingCurrencyProvider fallback={<RouteFallback />}>
      <Page>{children}</Page>
    </ReportingCurrencyProvider>
  )
}

function PublicOnlyRoute({ children }: { children: ReactElement }) {
  const { authEnabled, authenticated, loading } = useAuth()

  if (loading) {
    return <RouteFallback />
  }

  const redirect = resolvePublicRedirect({ authEnabled, authenticated, loading })
  if (redirect) {
    return <CompatNavigate to={redirect} replace />
  }

  return children
}
function LegacyRequestAuditCompat() {
  return (
    <Routes>
      <Route path="/observe/requests/:requestId/audit" element={withRouteSuspense(<RequestLogAuditPage />)} />
    </Routes>
  )
}

function PublicLoginRoute() {
  return <PublicOnlyRoute>{withRouteSuspense(<LoginPage />)}</PublicOnlyRoute>
}

function ProtectedObserveRoute() {
  return <ProtectedRoute>{withRouteSuspense(<ObservePage />)}</ProtectedRoute>
}

function ProtectedModelsRoute() {
  return <ProtectedRoute>{withRouteSuspense(<ModelsPage />)}</ProtectedRoute>
}

function ProtectedModelDetailRoute() {
  const { modelId } = useTanStackParams({ from: "/models/$modelId" })
  const search = useTanStackSearch({ from: "/models/$modelId" })
  const navigate = useTanStackNavigate()
  const searchParams = new URLSearchParams()
  if (search.tab && search.tab !== "connections") searchParams.set("tab", search.tab)
  if (search.focus_connection_id) searchParams.set("focus_connection_id", search.focus_connection_id)

  return (
    <ProtectedRoute>
      {withRouteSuspense(
        <ModelDetailFeaturePage
          modelId={modelId}
          tab={search.tab}
          searchParams={searchParams}
          onBack={() => void navigate({ to: "/models" })}
          onNavigateTo={(to) => void navigate({ to })}
          onSearchParamsChange={(nextSearchParams, options) => void navigate({
            to: "/models/$modelId",
            params: { modelId },
            search: {
              tab: nextSearchParams.get("tab") ?? "connections",
              focus_connection_id: nextSearchParams.get("focus_connection_id") ?? undefined,
            },
            replace: options?.replace,
          })}
          onTabChange={(tab) => void navigate({
            to: "/models/$modelId",
            params: { modelId },
            search: { tab },
            replace: true,
          })}
        />,
      )}
    </ProtectedRoute>
  )
}

function ProtectedEndpointsRoute() {
  return <ProtectedRoute>{withRouteSuspense(<EndpointsPage />)}</ProtectedRoute>
}

function ProtectedBanPoliciesRoute() {
  return <ProtectedRoute>{withRouteSuspense(<BanPoliciesFeaturePage />)}</ProtectedRoute>
}

function ProtectedSettingsRoute() {
  return <ProtectedRoute>{withRouteSuspense(<SettingsFeaturePage />)}</ProtectedRoute>
}

function ProtectedProxyKeysRoute() {
  return <ProtectedRoute>{withRouteSuspense(<ProxyApiKeysPage />)}</ProtectedRoute>
}


function ProtectedPricingRoute() {
  return <ProtectedRoute>{withRouteSuspense(<PricingTemplatesPage />)}</ProtectedRoute>
}

function ProtectedRequestLogsRoute() {
  return <ProtectedRoute>{withRouteSuspense(<RequestLogsPage />)}</ProtectedRoute>
}

function ProtectedRequestAuditRoute() {
  return (
    <ProtectedRoute>
      <LegacyRequestAuditCompat />
    </ProtectedRoute>
  )
}

const rootRoute = createRootRoute({ component: Outlet, notFoundComponent: NotFoundRoute })
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: () => <Navigate to="/observe" replace />,
})
const observeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/observe",
  validateSearch: (search) => observeSearchSchema.parse(search),
  component: ProtectedObserveRoute,
})
const authLoginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/auth/login",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: PublicLoginRoute,
})
const modelsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/models",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: ProtectedModelsRoute,
})
const modelDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/models/$modelId",
  validateSearch: (search) => modelDetailSearchSchema.parse(search),
  component: ProtectedModelDetailRoute,
})
const endpointsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/route/endpoints",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: ProtectedEndpointsRoute,
})
const banPoliciesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/route/ban-policies",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: ProtectedBanPoliciesRoute,
})
const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/system/settings",
  validateSearch: (search) => settingsSearchSchema.parse(search),
  component: ProtectedSettingsRoute,
})
const proxyKeysRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/control/proxy-keys",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: ProtectedProxyKeysRoute,
})
const pricingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/route/pricing",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: ProtectedPricingRoute,
})
const requestLogsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/observe/requests",
  validateSearch: (search) => requestLogSearchSchema.parse(search),
  component: ProtectedRequestLogsRoute,
})
const requestAuditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/observe/requests/$requestId/audit",
  validateSearch: (search) => requestAuditSearchSchema.parse(search),
  component: ProtectedRequestAuditRoute,
})
// eslint-disable-next-line react-refresh/only-export-components
export const prismRouteTree = rootRoute.addChildren([
  indexRoute,
  observeRoute,
  authLoginRoute,
  modelsRoute,
  modelDetailRoute,
  endpointsRoute,
  banPoliciesRoute,
  settingsRoute,
  proxyKeysRoute,
  pricingRoute,
  requestLogsRoute,
  requestAuditRoute,
])

function parsePlainSearch(search: string): Record<string, string> {
  const normalizedSearch = search.startsWith("?") ? search.slice(1) : search
  return Object.fromEntries(new URLSearchParams(normalizedSearch))
}

function isDefaultSearchValue(key: string, value: unknown): boolean {
  if ((key === "cursor" || key === "offset") && value === 0) return true
  if (key === "limit" && value === 100) return true
  if ((key === "status" || key === "status_family") && value === "all") return true
  if (key === "time_range" && value === "1h") return true
  return false
}

function stringifyPlainSearch(search: Record<string, unknown>): string {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(search)) {
    if (value == null || value === "" || value === "undefined" || value === "null") continue
    if (isDefaultSearchValue(key, value)) continue
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item != null && item !== "") params.append(key, String(item))
      }
      continue
    }
    params.set(key, String(value))
  }
  const serialized = params.toString()
  return serialized ? `?${serialized}` : ""
}

// eslint-disable-next-line react-refresh/only-export-components
export function createRewriteRouter(options: { history?: RouterHistory } = {}) {
  return createRouter({
    routeTree: prismRouteTree,
    history: options.history ?? createBrowserHistory(),
    parseSearch: parsePlainSearch,
    stringifySearch: stringifyPlainSearch,
  })
}
