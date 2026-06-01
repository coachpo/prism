import type {
  LoadbalanceBanMode,
  LoadbalanceStrategySummary,
  LegacyLoadbalanceStrategyType,
} from "./types";

export const LOADBALANCE_LEGACY_STRATEGY_TYPES = [
  "single",
  "fill-first",
  "round-robin",
  "cheapest_eligible_context",
] as const;
export const LOADBALANCE_BAN_MODES = ["off", "temporary", "until_reset"] as const;
export const DEFAULT_FAILURE_STATUS_CODES = [403, 422, 429, 500, 502, 503, 504, 529];

export const DEFAULT_BAN_POLICY_FIELDS = {
  failure_status_codes: [...DEFAULT_FAILURE_STATUS_CODES],
  ban_mode: "off" as LoadbalanceBanMode,
  retry_base_delay_ms: 60_000,
  retry_backoff_multiplier: 2,
  retry_jitter_ratio: 0.2,
  retry_max_delay_ms: 900_000,
  cycle_retry_attempt_limit: 3,
  ban_cumulative_retry_attempt_threshold: 0,
  ban_duration_seconds: 0,
};

type LegacyStrategyCopy = {
  cheapestEligibleContextLabel: string;
  cheapestEligibleContextSummary: string;
  fillFirstLabel: string;
  fillFirstSummary: string;
  legacyFamilyLabel: string;
  roundRobinLabel: string;
  roundRobinSummary: string;
  singleLabel: string;
  singleSummary: string;
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

export function isLegacyLoadbalanceStrategyType(
  value: string,
): value is LegacyLoadbalanceStrategyType {
  return LOADBALANCE_LEGACY_STRATEGY_TYPES.includes(value as LegacyLoadbalanceStrategyType);
}

export function isLoadbalanceBanMode(value: string): value is LoadbalanceBanMode {
  return LOADBALANCE_BAN_MODES.includes(value as LoadbalanceBanMode);
}

export function getLegacyLoadbalanceStrategyLabel(
  strategyType: LegacyLoadbalanceStrategyType,
  copy: LegacyStrategyCopy,
) {
  switch (strategyType) {
    case "single":
      return copy.singleLabel;
    case "fill-first":
      return copy.fillFirstLabel;
    case "round-robin":
      return copy.roundRobinLabel;
    case "cheapest_eligible_context":
      return copy.cheapestEligibleContextLabel;
  }
}

export function getLegacyLoadbalanceStrategySummary(
  strategyType: LegacyLoadbalanceStrategyType,
  copy: LegacyStrategyCopy,
) {
  switch (strategyType) {
    case "single":
      return copy.singleSummary;
    case "fill-first":
      return copy.fillFirstSummary;
    case "round-robin":
      return copy.roundRobinSummary;
    case "cheapest_eligible_context":
      return copy.cheapestEligibleContextSummary;
  }
}

export function getLoadbalanceStrategyTypeLabel(
  strategy: Pick<LoadbalanceStrategySummary, "legacy_strategy_type">,
  copy: LegacyStrategyCopy,
) {
  return getLegacyLoadbalanceStrategyLabel(strategy.legacy_strategy_type, copy);
}

export function getLoadbalanceStrategyDetailLabel(
  strategy: Pick<LoadbalanceStrategySummary, "legacy_strategy_type">,
  copy: LegacyStrategyCopy,
) {
  return `${copy.legacyFamilyLabel} • ${getLoadbalanceStrategyTypeLabel(strategy, copy)}`;
}
