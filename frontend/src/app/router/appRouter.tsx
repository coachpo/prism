import { lazy, Suspense, type ReactElement, type ReactNode, useEffect, useRef } from "react"
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
  authLoginSearchSchema,
  modelsListSearchSchema,
  observeSearchSchema,
  requestAuditSearchSchema,
  requestLogSearchSchema,
  routingHealthSearchSchema,
  settingsSearchSchema,
} from "./rewriteRoutes"
import { modelDetailSearchSchema } from "@/features/models/detail/modelDetailSchemas"
import { needsGlobalAccessLayer, resolveProtectedRedirect, resolvePublicRedirect } from "./authGates"
import { GlobalAccessLayer } from "./GlobalAccessLayer"

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
const RequestLogAuditFeaturePage = lazy(() => import("@/features/request-logs/RequestLogAuditFeaturePage"))
const RoutingHealthFeaturePage = lazy(() => import("@/features/routing-health/RoutingHealthFeaturePage"))

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
  const bootstrapMode = PUBLIC_AUTH_PATHS.has(window.location.pathname) ? "public" : "full"

  return <AuthProvider bootstrapMode={bootstrapMode}>{children}</AuthProvider>
}
function ProtectedRoute({ children }: { children: ReactElement }) {
  const { authEnabled, authenticated, loading, phase } = useAuth()

  if (loading) {
    return <RouteFallback />
  }

  if (needsGlobalAccessLayer(phase)) {
    return <GlobalAccessLayer />
  }

  const redirect = resolveProtectedRedirect(
    { phase, authEnabled, authenticated, loading },
    {
      pathname: window.location.pathname,
      search: window.location.search,
      hash: window.location.hash,
    },
  )
  if (redirect) {
    return <Navigate to={redirect.to} replace search={redirect.search} />
  }

  return (
    <ReportingCurrencyProvider fallback={<RouteFallback />}>
      <Page>{children}</Page>
    </ReportingCurrencyProvider>
  )
}

function PublicOnlyRoute({ children }: { children: ReactElement }) {
  const { authEnabled, authenticated, loading, phase } = useAuth()

  if (loading) {
    return <RouteFallback />
  }

  if (needsGlobalAccessLayer(phase)) {
    return <GlobalAccessLayer />
  }

  const redirect = resolvePublicRedirect({ phase, authEnabled, authenticated, loading })
  if (redirect) {
    return <Navigate to={redirect} replace />
  }

  return children
}

function PublicLoginRoute() {
  return <PublicOnlyRoute>{withRouteSuspense(<LoginPage />)}</PublicOnlyRoute>
}

const ROUTING_HEALTH_SEARCH_KEYS = [
  "preset",
  "from_time",
  "to_time",
  "event_type",
  "event_failure_kind",
  "event_admission_reason",
  "event_model_id",
  "event_endpoint_id",
  "event_terminal_target_id",
  "event_sort_order",
  "event_id",
  "event_cursor",
  "runtime_state",
  "runtime_model_id",
  "runtime_endpoint_id",
  "runtime_terminal_target_id",
  "runtime_cursor",
] as const

function pickRoutingHealthSearch(search: Record<string, unknown>): Record<string, unknown> {
  const picked: Record<string, unknown> = {}
  for (const key of ROUTING_HEALTH_SEARCH_KEYS) {
    if (search[key] !== undefined) picked[key] = search[key]
  }
  return picked
}

function ProtectedObserveRoute() {
  const search = useTanStackSearch({ from: "/observe" })

  // The events tab became its own page. Old `?tab=events` deep links carry the
  // whole event window and filter set across unchanged; the chart-only keys
  // (metric, group_by, interval, cost_segment_key) belong to the dashboard and
  // are dropped.
  if (search.tab === "events") {
    return <Navigate to="/observe/routing-health" replace search={pickRoutingHealthSearch(search)} />
  }

  return <ProtectedRoute>{withRouteSuspense(<ObservePage />)}</ProtectedRoute>
}

function ProtectedModelsRoute() {
  return <ProtectedRoute>{withRouteSuspense(<ModelsPage />)}</ProtectedRoute>
}

function ProtectedModelDetailRoute() {
  const { modelId } = useTanStackParams({ from: "/route/models/$modelId" })
  const search = useTanStackSearch({ from: "/route/models/$modelId" })
  const navigate = useTanStackNavigate()
  // Canonicalize dead/unsupported search state (old ?tab=connections|events)
  // with a replace so the URL settles on /models/:id. One-shot legal params
  // (action/endpoint_id/focus_connection_id) are preserved. The ref guard
  // makes the rewrite fire once per raw query even while the history commit
  // is still in flight, avoiding a navigate/re-render loop.
  const canonicalizedRawSearchRef = useRef<string | null>(null)
  useEffect(() => {
    const rawSearch = window.location.search
    if (canonicalizedRawSearchRef.current === rawSearch) {
      return
    }
    const raw = new URLSearchParams(rawSearch)
    const supported = new Set(["action", "endpoint_id", "focus_connection_id"])
    if (Array.from(raw.keys()).some((key) => !supported.has(key))) {
      canonicalizedRawSearchRef.current = rawSearch
      void navigate({
        to: "/route/models/$modelId",
        params: { modelId },
        search: {
          ...(search.action ? { action: search.action } : {}),
          ...(search.endpoint_id ? { endpoint_id: search.endpoint_id } : {}),
          ...(search.focus_connection_id ? { focus_connection_id: search.focus_connection_id } : {}),
        },
        replace: true,
        // A URL tidy-up on an already-rendered page, so it must not scroll.
        resetScroll: false,
      })
    }
  }, [modelId, navigate, search.action, search.endpoint_id, search.focus_connection_id])
  const searchParams = new URLSearchParams()
  if (search.action) searchParams.set("action", search.action)
  if (search.endpoint_id) searchParams.set("endpoint_id", search.endpoint_id)
  if (search.focus_connection_id) searchParams.set("focus_connection_id", search.focus_connection_id)
  return (
    <ProtectedRoute>
      {withRouteSuspense(
        <ModelDetailFeaturePage
          modelId={modelId}
          searchParams={searchParams}
          onNavigateTo={(to) => void navigate({ to })}
          onSearchParamsChange={(nextSearchParams, options) => void navigate({
            to: "/route/models/$modelId",
            params: { modelId },
            search: {
              action: nextSearchParams.get("action") === "create-terminal-target" ? "create-terminal-target" : undefined,
              endpoint_id: nextSearchParams.get("endpoint_id") ?? undefined,
              focus_connection_id: nextSearchParams.get("focus_connection_id") ?? undefined,
            },
            replace: options?.replace,
            // The detail page consumes its one-shot params by writing them back
            // out; `focus_connection_id` in particular is cleared while the page
            // is smooth-scrolling to that card, and a scroll reset here would
            // fight it.
            resetScroll: false,
          })}
        />,
      )}
    </ProtectedRoute>
  )
}

function ProtectedRoutingHealthRoute() {
  return <ProtectedRoute>{withRouteSuspense(<RoutingHealthFeaturePage />)}</ProtectedRoute>
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

function LegacyProxyKeysRoute() {
  return <Navigate to="/system/proxy-keys" replace />
}

// Models moved under /route so the configuration chain reads down the sidebar
// in dependency order. Both legacy paths keep working.
function LegacyModelsRoute() {
  return <Navigate to="/route/models" replace />
}

function LegacyModelDetailRoute() {
  const { modelId } = useTanStackParams({ from: "/models/$modelId" })
  const search = useTanStackSearch({ from: "/models/$modelId" })
  return (
    <Navigate
      to="/route/models/$modelId"
      params={{ modelId }}
      search={{
        action: search.action,
        endpoint_id: search.endpoint_id,
        focus_connection_id: search.focus_connection_id,
      }}
      replace
    />
  )
}


function ProtectedPricingRoute() {
  return <ProtectedRoute>{withRouteSuspense(<PricingTemplatesPage />)}</ProtectedRoute>
}

function ProtectedRequestLogsRoute() {
  return <ProtectedRoute>{withRouteSuspense(<RequestLogsPage />)}</ProtectedRoute>
}

function ProtectedRequestAuditRoute() {
  const { requestId } = useTanStackParams({ from: "/observe/requests/$requestId/audit" })
  const search = useTanStackSearch({ from: "/observe/requests/$requestId/audit" })
  const searchParams = new URLSearchParams()
  if (search.audit_id) searchParams.set("audit_id", search.audit_id)
  if (search.cursor) searchParams.set("cursor", search.cursor)

  return (
    <ProtectedRoute>
      {withRouteSuspense(
        <RequestLogAuditFeaturePage
          requestIdParam={requestId}
          searchParams={searchParams}
        />,
      )}
    </ProtectedRoute>
  )
}

const rootRoute = createRootRoute({ component: Outlet, notFoundComponent: NotFoundRoute })
function RootLandingGate() {
  const { phase } = useAuth()
  switch (phase.kind) {
    case "AUTHENTICATED":
    case "AUTH_DISABLED":
      return <Navigate to="/observe" replace />
    case "ANONYMOUS":
      return <Navigate to="/auth/login" replace search={{ redirect: "/observe" }} />
    default:
      // BOOTSTRAPPING / AUTH_DISABLED_VERIFYING / REFRESHING / LOGGING_OUT /
      // AUTH_TRANSITION_FAIL_CLOSED / AUTH_UNAVAILABLE / SESSION_EXPIRED render
      // the global access layer once; no protected page is mounted.
      return <GlobalAccessLayer />
  }
}

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: RootLandingGate,
})
// eslint-disable-next-line react-refresh/only-export-components
export const observeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/observe",
  validateSearch: (search) => observeSearchSchema.parse(search),
  component: ProtectedObserveRoute,
})
const authLoginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/auth/login",
  validateSearch: (search) => authLoginSearchSchema.parse(search),
  component: PublicLoginRoute,
})
const modelsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/route/models",
  validateSearch: (search) => modelsListSearchSchema.parse(search),
  component: ProtectedModelsRoute,
})
const modelDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/route/models/$modelId",
  // `tab` was removed from the model-detail surface. Keep the shared parser
  // tolerant for direct callers, but do not re-emit its default in the
  // canonical URL.
  validateSearch: (search) => {
    const canonicalSearch = modelDetailSearchSchema.parse(search)
    return { ...canonicalSearch, tab: undefined }
  },
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
  path: "/system/proxy-keys",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: ProtectedProxyKeysRoute,
})
const routingHealthRouteInternal = createRoute({
  getParentRoute: () => rootRoute,
  path: "/observe/routing-health",
  validateSearch: (search) => routingHealthSearchSchema.parse(search),
  component: ProtectedRoutingHealthRoute,
})
const legacyModelsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/models",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: LegacyModelsRoute,
})
const legacyModelDetailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/models/$modelId",
  validateSearch: (search) => modelDetailSearchSchema.parse(search),
  component: LegacyModelDetailRoute,
})
const legacyProxyKeysRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/control/proxy-keys",
  validateSearch: (search) => emptySearchSchema.parse(search),
  component: LegacyProxyKeysRoute,
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
export const routingHealthRoute = routingHealthRouteInternal
// eslint-disable-next-line react-refresh/only-export-components
export const prismRouteTree = rootRoute.addChildren([
  indexRoute,
  observeRoute,
  routingHealthRouteInternal,
  authLoginRoute,
  modelsRoute,
  modelDetailRoute,
  legacyModelsRoute,
  legacyModelDetailRoute,
  endpointsRoute,
  banPoliciesRoute,
  settingsRoute,
  proxyKeysRoute,
  legacyProxyKeysRoute,
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
  if (key === "pricing_status" && value === "all") return true
  if ((key === "status" || key === "status_family") && value === "all") return true
  if (key === "time_range" && value === "24h") return true
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
