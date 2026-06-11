export type RewriteQueryScope = "selected-profile" | "global" | "runtime-bypass"

export type RewriteProfileId = number | string

const root = ["rewrite"] as const

function profileRoot(profileId: RewriteProfileId) {
  return [...root, "selected-profile", String(profileId)] as const
}

const globalRoot = [...root, "global"] as const
const runtimeBypassRoot = [...root, "runtime-bypass"] as const

export const rewriteQueryKeys = {
  all: root,
  contract: () => [...root, "contract"] as const,
  route: (path: string) => [...root, "route", path] as const,
  selectedProfile: (profileId: RewriteProfileId) => {
    const profile = profileRoot(profileId)

    return {
      all: profile,
      models: () => [...profile, "models"] as const,
      model: (modelConfigId: number) => [...profile, "models", modelConfigId] as const,
      modelTargets: (modelConfigId: number) =>
        [...profile, "models", modelConfigId, "targets"] as const,
      modelConnections: (modelConfigId: number) =>
        [...profile, "models", modelConfigId, "connections"] as const,
      endpoints: () => [...profile, "endpoints"] as const,
      endpoint: (endpointId: number) => [...profile, "endpoints", endpointId] as const,
      connections: () => [...profile, "connections"] as const,
      pricingTemplates: () => [...profile, "pricing-templates"] as const,
      loadbalanceStrategies: () => [...profile, "loadbalance", "strategies"] as const,
      loadbalanceEvents: () => [...profile, "loadbalance", "events"] as const,
      stats: () => [...profile, "stats"] as const,
      requestLogs: (search: unknown) => [...profile, "stats", "request-logs", search] as const,
      requestLog: (requestId: number | string) => [...profile, "stats", "request-logs", String(requestId)] as const,
      audit: () => [...profile, "audit"] as const,
      requestAudit: (requestId: number | string, search: unknown) => [...profile, "audit", "request-log", String(requestId), search] as const,
      costing: () => [...profile, "settings", "costing"] as const,
      timezone: () => [...profile, "settings", "timezone"] as const,
      profileConfig: () => [...profile, "config", "profile"] as const,
      configRules: () => [...profile, "config", "rules"] as const,
    }
  },
  global: {
    all: globalRoot,
    auth: () => [...globalRoot, "auth"] as const,
    profiles: () => [...globalRoot, "profiles"] as const,
    activeProfile: () => [...globalRoot, "profiles", "active"] as const,
    vendors: () => [...globalRoot, "vendors"] as const,
    vendor: (vendorId: number) => [...globalRoot, "vendors", vendorId] as const,
    vendorCatalog: () => [...globalRoot, "config", "vendors"] as const,
    sidecars: () => [...globalRoot, "sidecars"] as const,
    sidecar: (sidecarId: number) => [...globalRoot, "sidecars", sidecarId] as const,
    settingsAuth: () => [...globalRoot, "settings", "auth"] as const,
    proxyApiKeys: () => [...globalRoot, "settings", "auth", "proxy-keys"] as const,
    logRetention: () => [...globalRoot, "settings", "log-retention"] as const,
  },
  runtimeBypass: {
    all: runtimeBypassRoot,
    operation: (path: `/v1${string}` | `/v1beta${string}`) =>
      [...runtimeBypassRoot, "operation", path] as const,
  },
}

export type RewriteQueryKey = readonly unknown[]
