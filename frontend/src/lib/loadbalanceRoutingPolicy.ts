import type {
  AdaptiveRoutingObjective,
  LoadbalanceAutoRecovery,
  LoadbalanceAutoRecoveryEnabled,
  LoadbalanceRoutingPolicy,
  LoadbalanceStrategy,
  LoadbalanceStrategyFamily,
  LegacyLoadbalanceStrategyType,
} from "./types";

export const LOADBALANCE_STRATEGY_FAMILIES = ["legacy", "adaptive"] as const;
export const LOADBALANCE_LEGACY_STRATEGY_TYPES = ["single", "fill-first", "round-robin"] as const;
export const LOADBALANCE_ADAPTIVE_ROUTING_OBJECTIVES = ["maximize_availability", "minimize_latency"] as const;
export const DEFAULT_FAILURE_STATUS_CODES = [403, 422, 429, 500, 502, 503, 504, 529];

type AdaptiveRoutingCopy = {
  maximizeAvailabilityLabel: string;
  maximizeAvailabilitySummary: string;
  minimizeLatencyLabel: string;
  minimizeLatencySummary: string;
};

export function normalizeFailureStatusCodes(statusCodes: readonly number[]): number[] {
  return Array.from(
    new Set(
      statusCodes
        .filter((statusCode) => Number.isFinite(statusCode))
        .map((statusCode) => Math.trunc(statusCode)),
    ),
  ).sort((left, right) => left - right);
}

export function createDefaultEnabledAutoRecovery(): LoadbalanceAutoRecoveryEnabled {
  return {
    mode: "enabled",
    status_codes: [...DEFAULT_FAILURE_STATUS_CODES],
    cooldown: {
      base_seconds: 60,
      failure_threshold: 2,
      backoff_multiplier: 2,
      max_cooldown_seconds: 900,
      jitter_ratio: 0.2,
    },
    ban: {
      mode: "off",
    },
  };
}

export function createDefaultAdaptiveRoutingPolicy(
  routingObjective: AdaptiveRoutingObjective = "minimize_latency",
): LoadbalanceRoutingPolicy {
  return {
    kind: "adaptive",
    routing_objective: routingObjective,
    deadline_budget_ms: 30_000,
    hedge: {
      enabled: false,
      delay_ms: 1_500,
      max_additional_attempts: 1,
    },
    circuit_breaker: {
      failure_status_codes: [...DEFAULT_FAILURE_STATUS_CODES],
      base_open_seconds: 60,
      failure_threshold: 2,
      backoff_multiplier: 2,
      max_open_seconds: 900,
      jitter_ratio: 0.2,
      ban_mode: "off",
      max_open_strikes_before_ban: 0,
      ban_duration_seconds: 0,
    },
    admission: {
      respect_qps_limit: true,
      respect_in_flight_limits: true,
    },
  };
}

export function getDefaultAutoRecovery(
  _strategyType: LegacyLoadbalanceStrategyType,
): LoadbalanceAutoRecovery {
  return createDefaultEnabledAutoRecovery();
}

export function isAutoRecoveryEnabled(
  autoRecovery: LoadbalanceAutoRecovery,
): autoRecovery is LoadbalanceAutoRecoveryEnabled {
  return autoRecovery.mode === "enabled";
}

export function isLegacyLoadbalanceStrategy(
  strategy: Pick<LoadbalanceStrategy, "strategy_type">,
): strategy is Extract<LoadbalanceStrategy, { strategy_type: "legacy" }> {
  return strategy.strategy_type === "legacy";
}

export function isAdaptiveLoadbalanceStrategy(
  strategy: Pick<LoadbalanceStrategy, "strategy_type">,
): strategy is Extract<LoadbalanceStrategy, { strategy_type: "adaptive" }> {
  return strategy.strategy_type === "adaptive";
}

export function isLoadbalanceStrategyFamily(value: string): value is LoadbalanceStrategyFamily {
  return LOADBALANCE_STRATEGY_FAMILIES.includes(value as LoadbalanceStrategyFamily);
}

export function isLegacyLoadbalanceStrategyType(
  value: string,
): value is LegacyLoadbalanceStrategyType {
  return LOADBALANCE_LEGACY_STRATEGY_TYPES.includes(value as LegacyLoadbalanceStrategyType);
}

export function getAdaptiveRoutingObjectiveLabel(
  routingObjective: AdaptiveRoutingObjective,
  copy: AdaptiveRoutingCopy,
) {
  return routingObjective === "minimize_latency"
    ? copy.minimizeLatencyLabel
    : copy.maximizeAvailabilityLabel;
}

export function getAdaptiveRoutingObjectiveSummary(
  routingObjective: AdaptiveRoutingObjective,
  copy: AdaptiveRoutingCopy,
) {
  return routingObjective === "minimize_latency"
    ? copy.minimizeLatencySummary
    : copy.maximizeAvailabilitySummary;
}
