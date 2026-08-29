import { ApiError, api } from "@/lib/api"
import { SHELL_ROUTE_METADATA } from "@/components/layout/app-layout/useShellNavigation"
import type {
  ModelRouteReadinessEnvelope,
  ProfileRouteReadiness,
  PricingSetupReadiness,
  ProxySetupReadiness,
  ReadinessAxis,
  RouteScheduleQualifier,
  RouteWitnessRef,
  SetupCoordinatorPhase,
  SetupCoordinatorState,
  SetupFact,
  SetupFactId,
  SetupFetchQuality,
  SetupMatchingWitnessProjection,
} from "@/lib/types"

export const SETUP_GENERATION_RETRY_LIMIT = 2

export type SetupCollapseDecision = "collapse" | "wait-until-blur" | "keep-open" | "expand"

/**
 * The disclosure rule is transition-scoped, not a permanent hidden flag:
 * completion may collapse once, focus inside defers it, manual reopen wins for
 * the current generation, and any hard-item regression expands the card.
 */
export function setupCollapseDecision(input: {
  wasReady: boolean
  isReady: boolean
  focusedInside: boolean
  manuallyReopened: boolean
}): SetupCollapseDecision {
  if (!input.isReady) return "expand"
  if (input.wasReady || input.manuallyReopened) return "keep-open"
  return input.focusedInside ? "wait-until-blur" : "collapse"
}

export interface SetupReadinessSources {
  endpoints: () => Promise<unknown>
  pricing: (generation: string) => Promise<unknown>
  routing: () => Promise<unknown>
  models: () => Promise<unknown>
  proxyKeys: (generation: string) => Promise<unknown>
}

export interface SetupReadinessOptions {
  sources?: SetupReadinessSources
  maxGenerationRetries?: number
}

export interface SetupReadinessSnapshot extends SetupCoordinatorState {
  /** The normalized projection is retained only for the shared action handoff. */
  representative: SetupMatchingWitnessProjection | null
}

type SourceRead = {
  value: unknown
  error: unknown | null
}

const DEFAULT_SOURCES: SetupReadinessSources = {
  endpoints: () => api.endpoints.list(),
  pricing: (generation) => api.pricingTemplates.setupReadiness(generation),
  routing: () => api.loadbalanceStrategies.list(),
  models: () => api.models.routeReadiness(),
  proxyKeys: (generation) => api.settings.auth.proxyKeys.setupReadiness(generation),
}

const FACT_LABELS: Record<SetupFactId, string> = {
  endpoints: "端点",
  pricing: "价格模板",
  routing: "路由策略",
  models: "模型",
  terminal_targets: "终端目标",
  proxy_keys: "代理密钥 / 访问模式",
  runtime_self_test: "验证接入",
}

const FACT_ROUTE_IDS: Record<Exclude<SetupFactId, "runtime_self_test">, string> = {
  endpoints: "endpoints",
  pricing: "pricing-templates",
  routing: "loadbalance-strategies",
  models: "models",
  terminal_targets: "models",
  proxy_keys: "proxy-api-keys",
}

function factHref(id: Exclude<SetupFactId, "runtime_self_test">): string {
  const route = SHELL_ROUTE_METADATA.find((candidate) => candidate.id === FACT_ROUTE_IDS[id])
  return route?.sidebarItem?.to ?? route?.canonicalPath ?? "/observe"
}

export function isPositiveDecimalString(value: unknown): value is string {
  return typeof value === "string" && /^[1-9]\d*$/.test(value)
}

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value)
}

function parseReadinessAxis(value: unknown, allowNotRequired = false): ReadinessAxis | null {
  const validStates = allowNotRequired ? ["ready", "not_ready", "unknown", "not_required"] : ["ready", "not_ready", "unknown"]
  if (!isObject(value) || !validStates.includes(String(value.state))) {
    return null
  }
  const reasonCodes = value.reason_codes
  if (!Array.isArray(reasonCodes) || reasonCodes.some((item) => typeof item !== "string")) {
    return null
  }
  return { state: value.state as ReadinessAxis["state"], reason_codes: reasonCodes }
}

function parseRouteScheduleQualifier(value: unknown): RouteScheduleQualifier | null {
  if (!isObject(value) || typeof value.schedule_limited !== "boolean") return null
  const validCount = (candidate: unknown) =>
    typeof candidate === "number" && Number.isInteger(candidate) && candidate >= 0
  if (!validCount(value.limited_witness_count) || !validCount(value.total_witness_count)) return null
  const limitedWitnessCount = value.limited_witness_count as number
  const totalWitnessCount = value.total_witness_count as number
  if (limitedWitnessCount > totalWitnessCount) return null
  if (value.schedule_limited !== (limitedWitnessCount > 0)) return null
  return {
    schedule_limited: value.schedule_limited,
    limited_witness_count: limitedWitnessCount,
    total_witness_count: totalWitnessCount,
  }
}

function parseRouteWitness(value: unknown): RouteWitnessRef | null {
  if (!isObject(value)) return null
  const stringFields = ["witness_id", "generation", "model_config_id", "model_id", "operation_name", "terminal_target_id", "endpoint_id"] as const
  if (stringFields.some((field) => typeof value[field] !== "string" || value[field].trim() === "")) return null
  if (!["generation", "model_config_id", "terminal_target_id", "endpoint_id"].every((field) => isPositiveDecimalString(value[field]))) return null
  if (value.coverage !== "full" && value.coverage !== "partial" && value.coverage !== "none") return null
  return value as unknown as RouteWitnessRef
}

function parseNullableRouteWitness(value: unknown): RouteWitnessRef | null | undefined {
  if (value === null) return null
  return parseRouteWitness(value) ?? undefined
}

function parseSetupMatchingWitnessProjection(
  value: unknown,
): SetupMatchingWitnessProjection | null | undefined {
  if (value === null) return null
  if (!isObject(value) || !isObject(value.model)) return undefined
  const witness = parseRouteWitness(value.witness)
  const model = value.model
  if (
    !witness ||
    model.kind !== "model" ||
    !isPositiveDecimalString(model.model_config_id) ||
    typeof model.model_id !== "string" ||
    typeof model.name !== "string" ||
    typeof model.name_source !== "string" ||
    (model.deleted !== null && typeof model.deleted !== "boolean")
  ) return undefined
  return {
    witness,
    model: {
      kind: "model",
      model_config_id: model.model_config_id,
      model_id: model.model_id,
      name: model.name,
      name_source: model.name_source,
      deleted: model.deleted,
    },
  }
}

function parseModelRouteReadiness(value: unknown) {
  if (!isObject(value)) return null
  const configuration = parseReadinessAxis(value.configuration)
  const application = parseReadinessAxis(value.application)
  const schedule = parseRouteScheduleQualifier(value.route_schedule)
  const witness = parseNullableRouteWitness(value.representative_witness)
  if (!configuration || !application || !schedule || witness === undefined) return null
  if (!(typeof value.route_witness_count === "number" && Number.isInteger(value.route_witness_count) && value.route_witness_count >= 0)) return null
  return {
    configuration,
    application,
    route_witness_count: value.route_witness_count,
    representative_witness: witness,
    route_schedule: schedule,
  }
}

function parseProfileReadiness(value: unknown): ProfileRouteReadiness | null {
  if (!isObject(value)) return null
  const configuration = parseReadinessAxis(value.configuration)
  const application = parseReadinessAxis(value.application)
  const generation = value.route_witness_generation
  if (!configuration || !application || (generation !== null && !isPositiveDecimalString(generation))) {
    return null
  }
  const nullableCount = (candidate: unknown): candidate is number | null =>
    candidate === null || (typeof candidate === "number" && Number.isInteger(candidate) && candidate >= 0)
  if (!nullableCount(value.configuration_ready_model_count) || !nullableCount(value.route_ready_model_count) || !nullableCount(value.route_witness_count)) {
    return null
  }
  const witness = parseNullableRouteWitness(value.representative_witness)
  const schedule = parseRouteScheduleQualifier(value.route_schedule)
  if (witness === undefined || !schedule) return null
  return {
    route_witness_generation: generation as string | null,
    configuration,
    application,
    configuration_ready_model_count: value.configuration_ready_model_count,
    route_ready_model_count: value.route_ready_model_count,
    route_witness_count: value.route_witness_count,
    representative_witness: witness,
    route_schedule: schedule,
  }
}

export function parseModelReadiness(value: unknown): ModelRouteReadinessEnvelope | null {
  if (!isObject(value) || !Array.isArray(value.items)) return null
  const routeReadiness = parseProfileReadiness(value.route_readiness)
  if (!routeReadiness) return null
  const items = []
  for (const item of value.items) {
    if (!isObject(item)) return null
    const itemReadiness = parseModelRouteReadiness(item.route_readiness)
    if (!itemReadiness) return null
    items.push({ ...item, route_readiness: itemReadiness })
  }
  return { items, route_readiness: routeReadiness }
}

function parseSetupAxis(value: unknown, allowNotRequired = false): ReadinessAxis | null {
  return parseReadinessAxis(value, allowNotRequired)
}

function parsePricingReadiness(value: unknown): PricingSetupReadiness | null {
  if (!isObject(value)) return null
  const configuration = parseSetupAxis(value.configuration)
  const application = parseSetupAxis(value.application)
  if (!configuration || !application || !isPositiveDecimalString(value.evaluated_route_witness_generation)) return null
  if (!["pricing_template_generation", "pricing_reference_generation", "route_witness_count", "applied_witness_count", "cost_ready_witness_count"].every((key) => typeof value[key] === "number" && Number.isInteger(value[key]) && value[key] >= 0)) return null
  if (value.cost_ready !== null && typeof value.cost_ready !== "boolean") return null
  const representative = parseSetupMatchingWitnessProjection(value.representative_matching)
  if (representative === undefined) return null
  return {
    ...(value as unknown as PricingSetupReadiness),
    configuration,
    application,
    representative_matching: representative,
  }
}

function parseProxyReadiness(value: unknown): ProxySetupReadiness | null {
  if (!isObject(value)) return null
  const configuration = parseSetupAxis(value.configuration)
  const application = parseSetupAxis(value.application, true)
  if (!configuration || !application || !isPositiveDecimalString(value.evaluated_route_witness_generation) || typeof value.proxy_key_owner_revision !== "string") return null
  if (!["route_witness_count", "matching_witness_count"].every((key) => typeof value[key] === "number" && Number.isInteger(value[key]) && value[key] >= 0)) return null
  if (value.optional_attribution_witness_count !== null && !(typeof value.optional_attribution_witness_count === "number" && Number.isInteger(value.optional_attribution_witness_count) && value.optional_attribution_witness_count >= 0)) return null
  const matching = parseSetupMatchingWitnessProjection(value.representative_matching)
  const optional = parseSetupMatchingWitnessProjection(value.representative_optional_attribution)
  if (matching === undefined || optional === undefined) return null
  return {
    ...(value as unknown as ProxySetupReadiness),
    configuration,
    application,
    representative_matching: matching,
    representative_optional_attribution: optional,
  }
}

function emptyFact(id: SetupFactId, kind: SetupFact["kind"]): SetupFact {
  return {
    id,
    kind,
    result: null,
    fetch_quality: "loading",
    reason_codes: [],
    label: FACT_LABELS[id],
    href: id === "runtime_self_test" ? null : factHref(id),
    detail: null,
    representative: null,
  }
}

export function createInitialSetupState(): SetupReadinessSnapshot {
  return {
    phase: "loading",
    facts: [
      emptyFact("endpoints", "required"),
      emptyFact("pricing", "recommended"),
      emptyFact("routing", "required"),
      emptyFact("models", "required"),
      emptyFact("terminal_targets", "required"),
      emptyFact("proxy_keys", "conditional"),
      emptyFact("runtime_self_test", "action"),
    ],
    route_configured_count: null,
    route_witness_generation: null,
    error: null,
    last_success_at: null,
    representative: null,
  }
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  return "无法读取配置事实"
}

function isGenerationMismatch(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && (error.code === "route_witness_generation_changed" || error.message.includes("route_witness_generation_changed"))
}

async function read(source: () => Promise<unknown>): Promise<SourceRead> {
  try {
    return { value: await source(), error: null }
  } catch (error) {
    return { value: null, error }
  }
}

function factFromAxis(
  id: SetupFactId,
  kind: SetupFact["kind"],
  axis: ReadinessAxis | null,
  quality: SetupFetchQuality,
  detail: string | null = null,
  representative: SetupMatchingWitnessProjection | null = null,
): SetupFact {
  const result: SetupFactResult = axis?.state === "ready" ? "complete" : axis?.state === "not_required" ? "skipped" : axis?.state === "not_ready" ? "incomplete" : null
  return {
    id,
    kind,
    result: quality === "fresh" ? result : null,
    fetch_quality: quality,
    reason_codes: axis?.reason_codes ?? [],
    label: FACT_LABELS[id],
    href: id === "runtime_self_test" ? null : factHref(id),
    detail,
    representative,
  }
}

type SetupFactResult = SetupFact["result"]

function buildEndpointFact(read: SourceRead): SetupFact {
  if (read.error) return factFromAxis("endpoints", "required", null, "error", errorMessage(read.error))
  if (!Array.isArray(read.value)) return factFromAxis("endpoints", "required", null, "unknown", "端点响应无法验证")
  const axis: ReadinessAxis = read.value.length > 0 ? { state: "ready", reason_codes: [] } : { state: "not_ready", reason_codes: ["no_endpoints"] }
  return factFromAxis("endpoints", "required", axis, "fresh")
}

function buildRoutingFact(read: SourceRead): SetupFact {
  if (read.error) return factFromAxis("routing", "required", null, "error", errorMessage(read.error))
  if (!Array.isArray(read.value)) return factFromAxis("routing", "required", null, "unknown", "路由策略响应无法验证")
  const defaults = read.value.filter((item) => isObject(item) && item.is_default === true).length
  const axis: ReadinessAxis = defaults === 1 ? { state: "ready", reason_codes: [] } : { state: "not_ready", reason_codes: [defaults === 0 ? "no_default_strategy" : "multiple_default_strategies"] }
  return factFromAxis("routing", "required", axis, "fresh")
}

function buildModelFacts(read: SourceRead): { models: SetupFact; terminalTargets: SetupFact; readiness: ProfileRouteReadiness | null } {
  if (read.error) {
    return {
      models: factFromAxis("models", "required", null, "error", errorMessage(read.error)),
      terminalTargets: factFromAxis("terminal_targets", "required", null, "error", errorMessage(read.error)),
      readiness: null,
    }
  }
  const parsed = parseModelReadiness(read.value)
  if (!parsed) {
    return {
      models: factFromAxis("models", "required", null, "unknown", "模型路由快照不完整"),
      terminalTargets: factFromAxis("terminal_targets", "required", null, "unknown", "终端目标路由快照不完整"),
      readiness: null,
    }
  }
  return {
    models: factFromAxis("models", "required", parsed.route_readiness.configuration, "fresh"),
    terminalTargets: factFromAxis(
      "terminal_targets",
      "required",
      parsed.route_readiness.application,
      "fresh",
      parsed.route_readiness.route_schedule.schedule_limited
        ? "已就绪的路由仅在配置时段内可用"
        : null,
    ),
    readiness: parsed.route_readiness,
  }
}

function buildPricingFact(read: SourceRead, generation: string | null): SetupFact {
  if (!generation) return factFromAxis("pricing", "recommended", null, "unknown", "等待模型路由快照")
  if (read.error) return factFromAxis("pricing", "recommended", null, "error", errorMessage(read.error))
  const parsed = parsePricingReadiness(read.value)
  if (!parsed) return factFromAxis("pricing", "recommended", null, "unknown", "价格可信度响应无法验证")
  const axis = parsed.cost_ready === true
    ? { state: "ready", reason_codes: [] } as ReadinessAxis
    : parsed.cost_ready === false
      ? { state: "not_ready", reason_codes: ["cost_not_ready"] } as ReadinessAxis
      : { state: "unknown", reason_codes: ["cost_readiness_unknown"] } as ReadinessAxis
  return factFromAxis("pricing", "recommended", axis, "fresh", null, parsed.representative_matching)
}

function buildProxyFact(read: SourceRead, generation: string | null, authMode: "enabled" | "disabled" | "unknown"): SetupFact {
  if (authMode === "disabled" && read.error === null && generation) {
    const parsed = parseProxyReadiness(read.value)
    if (parsed && parsed.application.state === "not_required") {
      return factFromAxis("proxy_keys", "conditional", { state: "not_required", reason_codes: [] }, "fresh", "当前开放访问，代理密钥不是访问前置", parsed.representative_optional_attribution)
    }
  }
  if (authMode === "disabled" && !generation) {
    return factFromAxis("proxy_keys", "conditional", { state: "not_required", reason_codes: [] }, "fresh", "当前开放访问，代理密钥不是访问前置")
  }
  if (!generation) return factFromAxis("proxy_keys", "conditional", null, "unknown", "等待模型路由快照")
  if (read.error) return factFromAxis("proxy_keys", "conditional", null, "error", errorMessage(read.error))
  const parsed = parseProxyReadiness(read.value)
  if (!parsed) return factFromAxis("proxy_keys", "conditional", null, "unknown", "代理密钥配置响应无法验证")
  return factFromAxis("proxy_keys", "conditional", parsed.configuration.state === "ready" && parsed.application.state === "ready"
    ? { state: "ready", reason_codes: [] }
    : parsed.configuration.state === "unknown" || parsed.application.state === "unknown"
      ? { state: "unknown", reason_codes: ["proxy_readiness_unknown"] }
      : { state: "not_ready", reason_codes: [...parsed.configuration.reason_codes, ...parsed.application.reason_codes] }, "fresh", null, parsed.representative_matching ?? parsed.representative_optional_attribution)
}

function setupPhase(facts: readonly SetupFact[]): SetupCoordinatorPhase {
  const core = facts.filter((fact) => fact.kind === "required")
  if (core.some((fact) => fact.fetch_quality === "loading")) return "loading"
  if (core.some((fact) => fact.fetch_quality === "error")) return "degraded"
  if (core.some((fact) => fact.fetch_quality === "unknown" || fact.fetch_quality === "stale")) return "unknown"
  if (facts.some((fact) => fact.fetch_quality === "error")) return "degraded"
  if (facts.some((fact) => fact.fetch_quality === "unknown" || fact.fetch_quality === "stale")) return "unknown"
  return "fresh"
}

export function finalizeSetupSnapshot(
  facts: readonly SetupFact[],
  readiness: ProfileRouteReadiness | null,
  error: string | null = null,
): SetupReadinessSnapshot {
  const core = facts.filter((fact) => fact.kind === "required")
  const count = core.every((fact) => fact.fetch_quality === "fresh")
    ? core.filter((fact) => fact.result === "complete").length
    : null
  const phase = setupPhase(facts)
  const representative = facts.find((fact) => fact.representative)?.representative ?? null
  return {
    phase,
    facts,
    route_configured_count: count,
    route_witness_generation: readiness?.route_witness_generation ?? null,
    error: error ?? facts.find((fact) => fact.fetch_quality === "error")?.detail ?? null,
    last_success_at: phase === "fresh" ? new Date().toISOString() : null,
    representative,
  }
}

export async function fetchSetupReadiness(
  authMode: "enabled" | "disabled" | "unknown",
  options: SetupReadinessOptions = {},
): Promise<SetupReadinessSnapshot> {
  const sources = options.sources ?? DEFAULT_SOURCES
  const maxRetries = options.maxGenerationRetries ?? SETUP_GENERATION_RETRY_LIMIT
  let attempt = 0
  let lastSnapshot: SetupReadinessSnapshot | null = null

  while (attempt <= maxRetries) {
    const [endpointsRead, routingRead, modelsRead] = await Promise.all([
      read(sources.endpoints),
      read(sources.routing),
      read(sources.models),
    ])
    const modelFacts = buildModelFacts(modelsRead)
    const generation = modelFacts.readiness?.route_witness_generation ?? null
    let pricingRead: SourceRead = { value: null, error: null }
    let proxyRead: SourceRead = { value: null, error: null }
    if (generation) {
      ;[pricingRead, proxyRead] = await Promise.all([
        read(() => sources.pricing(generation)),
        read(() => sources.proxyKeys(generation)),
      ])
    }
    const facts = [
      buildEndpointFact(endpointsRead),
      buildPricingFact(pricingRead, generation),
      buildRoutingFact(routingRead),
      modelFacts.models,
      modelFacts.terminalTargets,
      buildProxyFact(proxyRead, generation, authMode),
      {
        ...emptyFact("runtime_self_test", "action"),
        fetch_quality: "fresh" as const,
        detail: "使用同一条路由快照验证接入",
      },
    ]
    const mismatch = [pricingRead.error, proxyRead.error].some(isGenerationMismatch)
    const snapshot = finalizeSetupSnapshot(facts, modelFacts.readiness)
    if (!mismatch || attempt >= maxRetries) {
      return mismatch
        ? { ...snapshot, phase: "unknown", error: "路由配置在读取期间发生变化，请重试" }
        : snapshot
    }
    lastSnapshot = snapshot
    attempt += 1
  }
  return lastSnapshot ?? createInitialSetupState()
}
