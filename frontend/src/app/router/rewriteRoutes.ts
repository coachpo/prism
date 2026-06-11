import { z } from "zod"
import { modelDetailSearchSchema } from "@/features/models/detail/modelDetailSchemas"

export type PrismRouteScope = "public" | "protected-global" | "protected-selected-profile" | "mixed"

export type PrismRouteId =
  | "observe"
  | "auth-login"
  | "auth-forgot-password"
  | "auth-reset-password"
  | "models"
  | "model-detail"
  | "route-endpoints"
  | "route-ban-policies"
  | "system-settings"
  | "control-proxy-keys"
  | "control-sidecars"
  | "route-pricing"
  | "observe-requests"
  | "observe-request-audit"

const searchStringSchema = z.preprocess((value) => value == null ? "" : String(value), z.string())
const requestIdSearchSchema = z.preprocess((value) => value == null ? "" : String(value), z.string().regex(/^\d+$/)).catch("")
const optionalSearchStringSchema = z.preprocess((value) => value == null ? undefined : String(value), z.string().optional())
const optionalRequestIdSearchSchema = z.preprocess((value) => value == null ? undefined : String(value), z.string().regex(/^\d+$/).optional()).catch(undefined)

export const observeSearchSchema = z.object({
  tab: z.enum(["overview", "analytics", "routing"]).catch("overview"),
})

export const requestLogSearchSchema = z.object({
  cursor: z.coerce.number().int().min(0).catch(0),
  endpoint: searchStringSchema.catch(""),
  endpoint_id: searchStringSchema.catch(""),
  ingress_request_id: searchStringSchema.catch(""),
  limit: z.coerce.number().refine((value) => [100, 300, 500].includes(value)).catch(100),
  model: searchStringSchema.catch(""),
  model_id: searchStringSchema.catch(""),
  offset: z.coerce.number().int().min(0).catch(0),
  request_id: requestIdSearchSchema,
  selected_request_id: requestIdSearchSchema,
  status: z.enum(["all", "client_error", "error"]).catch("all"),
  status_family: z.enum(["all", "4xx", "5xx"]).catch("all"),
  time_range: z.enum(["1h", "6h", "24h", "7d", "30d", "all"]).catch("1h"),
})

export const requestAuditSearchSchema = z.object({
  audit_id: optionalRequestIdSearchSchema,
  cursor: optionalSearchStringSchema.catch(undefined),
})

export const resetPasswordSearchSchema = z.object({
  token: z.string().optional(),
  code: z.string().optional(),
})

export const settingsSearchSchema = z.object({
  tab: z.enum(["profile", "global", "startup"]).catch("profile"),
  section: z.string().optional(),
})

export const emptySearchSchema = z.object({})

export type ObserveSearch = z.input<typeof observeSearchSchema>
export type RequestLogSearch = z.input<typeof requestLogSearchSchema>
export type RequestAuditSearch = z.input<typeof requestAuditSearchSchema>
export type ResetPasswordSearch = z.input<typeof resetPasswordSearchSchema>
export type SettingsSearch = z.input<typeof settingsSearchSchema>
export type ModelDetailRouteSearch = z.input<typeof modelDetailSearchSchema>
interface StaticRouteDefinition {
  readonly id: Exclude<PrismRouteId, "model-detail" | "observe-request-audit">
  readonly path: string
  readonly scope: PrismRouteScope
  readonly searchSchema: typeof emptySearchSchema | typeof observeSearchSchema | typeof requestLogSearchSchema | typeof resetPasswordSearchSchema | typeof settingsSearchSchema
}

export const prismRouteDefinitions = [
  { id: "observe", path: "/observe", scope: "mixed", searchSchema: observeSearchSchema },
  { id: "auth-login", path: "/auth/login", scope: "public", searchSchema: emptySearchSchema },
  { id: "auth-forgot-password", path: "/auth/forgot-password", scope: "public", searchSchema: emptySearchSchema },
  { id: "auth-reset-password", path: "/auth/reset-password", scope: "public", searchSchema: resetPasswordSearchSchema },
  { id: "models", path: "/models", scope: "protected-selected-profile", searchSchema: emptySearchSchema },
  { id: "route-endpoints", path: "/route/endpoints", scope: "protected-selected-profile", searchSchema: emptySearchSchema },
  { id: "route-ban-policies", path: "/route/ban-policies", scope: "protected-selected-profile", searchSchema: emptySearchSchema },
  { id: "system-settings", path: "/system/settings", scope: "mixed", searchSchema: settingsSearchSchema },
  { id: "control-proxy-keys", path: "/control/proxy-keys", scope: "protected-global", searchSchema: emptySearchSchema },
  { id: "control-sidecars", path: "/control/sidecars", scope: "protected-global", searchSchema: emptySearchSchema },
  { id: "route-pricing", path: "/route/pricing", scope: "protected-selected-profile", searchSchema: emptySearchSchema },
  { id: "observe-requests", path: "/observe/requests", scope: "protected-selected-profile", searchSchema: requestLogSearchSchema },
] as const satisfies readonly StaticRouteDefinition[]

export const prismDynamicRouteDefinitions = [
  { id: "model-detail", path: "/models/$modelId", scope: "protected-selected-profile" },
  { id: "observe-request-audit", path: "/observe/requests/$requestId/audit", scope: "protected-selected-profile" },
] as const
export const rewriteCompatibilityRoutePaths = [
  "/dashboard",
  "/login",
  "/forgot-password",
  "/reset-password",
  "/endpoints",
  "/loadbalance-strategies",
  "/settings",
  "/proxy-api-keys",
  "/sidecars",
  "/pricing-templates",
  "/request-logs",
  "/request-logs/$requestId/audit",
] as const

export const rewriteRoutePaths = [
  ...prismRouteDefinitions.map((route) => route.path),
  ...prismDynamicRouteDefinitions.map((route) => route.path),
  ...rewriteCompatibilityRoutePaths,
] as const

export type RewriteRoutePath = (typeof rewriteRoutePaths)[number]

export const prismPathById = {
  observe: "/observe",
  "auth-login": "/auth/login",
  "auth-forgot-password": "/auth/forgot-password",
  "auth-reset-password": "/auth/reset-password",
  models: "/models",
  "model-detail": "/models/$modelId",
  "route-endpoints": "/route/endpoints",
  "route-ban-policies": "/route/ban-policies",
  "system-settings": "/system/settings",
  "control-proxy-keys": "/control/proxy-keys",
  "control-sidecars": "/control/sidecars",
  "route-pricing": "/route/pricing",
  "observe-requests": "/observe/requests",
  "observe-request-audit": "/observe/requests/$requestId/audit",
} as const satisfies Record<PrismRouteId, RewriteRoutePath>

export const legacyRouteRedirects = {
  "/": "/observe",
  "/dashboard": "/observe",
  "/login": "/auth/login",
  "/forgot-password": "/auth/forgot-password",
  "/reset-password": "/auth/reset-password",
  "/endpoints": "/route/endpoints",
  "/loadbalance-strategies": "/route/ban-policies",
  "/settings": "/system/settings",
  "/proxy-api-keys": "/control/proxy-keys",
  "/sidecars": "/control/sidecars",
  "/pricing-templates": "/route/pricing",
  "/request-logs": "/observe/requests",
} as const

export type LegacyRoutePath = keyof typeof legacyRouteRedirects

export function buildModelDetailPath(modelId: string | number): string {
  return `/models/${encodeURIComponent(String(modelId))}`
}

export function buildRequestAuditPath(requestId: string | number): string {
  return `/observe/requests/${encodeURIComponent(String(requestId))}/audit`
}

export function buildLegacyRequestAuditRedirect(requestId: string | number): string {
  return buildRequestAuditPath(requestId)
}

export function getLegacyRedirectPath(pathname: string): string | null {
  if (pathname.startsWith("/request-logs/") && pathname.endsWith("/audit")) {
    const requestId = pathname.slice("/request-logs/".length, -"/audit".length)
    return requestId ? buildLegacyRequestAuditRedirect(decodeURIComponent(requestId)) : null
  }

  return legacyRouteRedirects[pathname as LegacyRoutePath] ?? null
}
