export type ReadinessState = "ready" | "not_ready" | "unknown" | "not_required"

export interface ReadinessAxis {
  state: ReadinessState
  reason_codes: string[]
}

export interface RouteWitnessRef {
  witness_id: string
  generation: string
  model_config_id: string
  model_id: string
  operation_name: string
  terminal_target_id: string
  endpoint_id: string
  coverage: "full" | "partial" | "none"
}

export interface RouteScheduleQualifier {
  schedule_limited: boolean
  limited_witness_count: number
  total_witness_count: number
}

export interface ModelEntityRef {
  kind: "model"
  model_config_id: string
  model_id: string
  name: string
  name_source: string
  deleted: boolean | null
}

export interface SetupMatchingWitnessProjection {
  witness: RouteWitnessRef
  model: ModelEntityRef
}

export interface ModelRouteReadinessSummary {
  configuration: ReadinessAxis
  application: ReadinessAxis
  route_witness_count: number
  representative_witness: RouteWitnessRef | null
  route_schedule: RouteScheduleQualifier
}

export interface ProfileRouteReadiness {
  route_witness_generation: string | null
  configuration: ReadinessAxis
  application: ReadinessAxis
  configuration_ready_model_count: number | null
  route_ready_model_count: number | null
  route_witness_count: number | null
  representative_witness: RouteWitnessRef | null
  route_schedule: RouteScheduleQualifier
}

export interface ModelRouteReadinessEnvelope<TModel = unknown> {
  items: TModel[]
  route_readiness: ProfileRouteReadiness
}

export interface PricingSetupReadiness {
  evaluated_route_witness_generation: string
  pricing_template_generation: number
  pricing_reference_generation: number
  configuration: ReadinessAxis
  application: ReadinessAxis
  route_witness_count: number
  applied_witness_count: number
  cost_ready_witness_count: number
  cost_ready: boolean | null
  representative_matching: SetupMatchingWitnessProjection | null
}

export interface ProxySetupReadiness {
  evaluated_route_witness_generation: string
  proxy_key_owner_revision: string
  configuration: ReadinessAxis
  application: ReadinessAxis
  route_witness_count: number
  matching_witness_count: number
  optional_attribution_witness_count: number | null
  representative_matching: SetupMatchingWitnessProjection | null
  representative_optional_attribution: SetupMatchingWitnessProjection | null
}

export type SetupFactId =
  | "endpoints"
  | "pricing"
  | "routing"
  | "models"
  | "terminal_targets"
  | "proxy_keys"
  | "runtime_self_test"

export type SetupFactKind = "required" | "recommended" | "conditional" | "action"
export type SetupFactResult = "complete" | "incomplete" | "skipped" | null
export type SetupFetchQuality = "loading" | "fresh" | "stale" | "unknown" | "error"

export interface SetupFact {
  id: SetupFactId
  kind: SetupFactKind
  result: SetupFactResult
  fetch_quality: SetupFetchQuality
  reason_codes: string[]
  label: string
  href: string | null
  detail: string | null
  representative: SetupMatchingWitnessProjection | null
}

export type SetupCoordinatorPhase = "loading" | "fresh" | "degraded" | "unknown" | "error"

export interface SetupCoordinatorState {
  phase: SetupCoordinatorPhase
  facts: readonly SetupFact[]
  route_configured_count: number | null
  route_witness_generation: string | null
  error: string | null
  last_success_at: string | null
}
