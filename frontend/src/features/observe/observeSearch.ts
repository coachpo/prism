export const OBSERVE_PRESETS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type ObservePreset = (typeof OBSERVE_PRESETS)[number];

export function isObservePreset(value: string): value is ObservePreset {
  return (OBSERVE_PRESETS as readonly string[]).includes(value);
}

/** Scope-specific metric catalogs. UI order is fixed per scope. */
export const SCOPE_METRICS = {
  ingress: [
    "requests",
    "errors",
    "ttft",
    "output_rate",
    "tokens",
    "cache_read_share",
    "cost",
  ],
  final_execution: [
    "requests",
    "errors",
    "final_attempt_latency",
    "tokens",
    "cache_read_share",
    "cost",
  ],
  route_attempt: ["attempts", "errors", "attempt_latency"],
} as const;

export const SCOPE_DEFAULT_METRIC = {
  ingress: "requests",
  final_execution: "requests",
  route_attempt: "attempts",
} as const;

export const OBSERVE_METRICS = [
  "requests",
  "errors",
  "ttft",
  "output_rate",
  "tokens",
  "cache_read_share",
  "cost",
	"attempts",
	"final_attempt_latency",
	"attempt_latency",
] as const;
export type ObserveMetric = (typeof OBSERVE_METRICS)[number];

export const OBSERVE_GROUPS = [
  "none",
  "ingress_model",
  "final_target_model",
  "attempt_target_model",
  "attempt_trigger",
  "attempt_result",
  "api_family",
  "endpoint",
  "terminal_target",
] as const;
export type ObserveGroupBy = (typeof OBSERVE_GROUPS)[number];

export const OBSERVE_SCOPES = [
  "ingress",
  "final_execution",
  "route_attempt",
] as const;
export type ObserveScope = (typeof OBSERVE_SCOPES)[number];

export function isObserveMetric(value: string): value is ObserveMetric {
  return (OBSERVE_METRICS as readonly string[]).includes(value);
}

export function isObserveGroupBy(value: string): value is ObserveGroupBy {
  return (OBSERVE_GROUPS as readonly string[]).includes(value);
}

export function isObserveScope(value: string): value is ObserveScope {
  return (OBSERVE_SCOPES as readonly string[]).includes(value);
}

export function groupBelongsToScope(
  groupBy: ObserveGroupBy,
  scope: ObserveScope,
): boolean {
  if (groupBy === "none") return true;
  if (groupBy === "api_family") return true;
  if (scope === "ingress") return groupBy === "ingress_model";
  if (scope === "final_execution") {
    return ["final_target_model", "endpoint", "terminal_target"].includes(
      groupBy,
    );
  }
  return [
    "attempt_target_model",
    "attempt_trigger",
    "attempt_result",
    "endpoint",
    "terminal_target",
  ].includes(
    groupBy,
  );
}

export function metricsForScope(scope: ObserveScope): readonly ObserveMetric[] {
  return SCOPE_METRICS[scope] ?? SCOPE_METRICS.ingress;
}

export function defaultMetricForScope(scope: ObserveScope): ObserveMetric {
  return SCOPE_DEFAULT_METRIC[scope] ?? "requests";
}

export function isValidMetricForScope(
  metric: string,
  scope: ObserveScope,
): metric is ObserveMetric {
  return (metricsForScope(scope) as readonly string[]).includes(metric);
}
