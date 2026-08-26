import type {
  LegacyLoadbalanceStrategyType,
  LoadbalanceBanMode,
  LoadbalanceBanPolicyFields,
  LoadbalanceStrategy,
  LoadbalanceStrategyCreate,
  LoadbalanceStrategyDefaultsResponse,
  LoadbalanceStrategySummary,
  LoadbalanceStrategyUpdate,
  StrategyImpactListResponse,
  StrategyPreviewDraft,
  StrategyPreviewResponse,
  StrategySetDefaultRequest,
  StrategySetDefaultResponse,
} from "../types";
import { normalizeFailureStatusCodes } from "../loadbalanceRoutingPolicy";
import { buildQuery, request } from "./request";

type RawLoadbalanceBanPolicyFields = {
  legacy_strategy_type?: unknown;
  failure_status_codes?: unknown;
  ban_mode?: unknown;
  retry_base_delay_ms?: unknown;
  retry_backoff_multiplier?: unknown;
  retry_jitter_ratio?: unknown;
  retry_max_delay_ms?: unknown;
  cycle_retry_attempt_limit?: unknown;
  ban_cumulative_retry_attempt_threshold?: unknown;
  ban_duration_seconds?: unknown;
};

export type RawLoadbalanceStrategySummary = RawLoadbalanceBanPolicyFields & {
  id: number;
  name: string;
  is_default?: boolean;
};

type RawLoadbalanceStrategy = RawLoadbalanceStrategySummary & {
  profile_id: number;
  attached_model_count: number;
  created_at: string;
  updated_at: string;
};

function unsupportedLoadbalanceStrategy(reason: string): never {
  throw new Error(
    `Unsupported loadbalance strategy contract from management API: ${reason}`,
  );
}

function normalizeInteger(value: unknown, field: string) {
  if (
    typeof value !== "number" ||
    !Number.isFinite(value) ||
    !Number.isInteger(value)
  ) {
    unsupportedLoadbalanceStrategy(field);
  }

  return value;
}

function normalizeNumber(value: unknown, field: string) {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    unsupportedLoadbalanceStrategy(field);
  }

  return value;
}

function normalizeLegacyStrategyType(
  value: unknown,
): LegacyLoadbalanceStrategyType {
  if (value === "single" || value === "fill-first" || value === "round-robin") {
    return value;
  }

  unsupportedLoadbalanceStrategy("legacy_strategy_type");
}

function normalizeBanMode(value: unknown): LoadbalanceBanMode {
  if (value === "off" || value === "temporary" || value === "until_reset") {
    return value;
  }

  unsupportedLoadbalanceStrategy("ban_mode");
}

function normalizeStatusCodes(value: unknown) {
  if (
    !Array.isArray(value) ||
    value.some((statusCode) => typeof statusCode !== "number")
  ) {
    unsupportedLoadbalanceStrategy("failure_status_codes");
  }

  return normalizeFailureStatusCodes(value);
}

const removedRetryAttemptField = ["retry", "max", "attempts"].join("_");

function rejectRemovedLoadbalanceBanPolicyFields(
  strategy: RawLoadbalanceBanPolicyFields,
) {
  if (Object.hasOwn(strategy, removedRetryAttemptField)) {
    unsupportedLoadbalanceStrategy(removedRetryAttemptField);
  }
}

function normalizeLoadbalanceBanPolicyFields(
  strategy: RawLoadbalanceBanPolicyFields,
): LoadbalanceBanPolicyFields {
  rejectRemovedLoadbalanceBanPolicyFields(strategy);

  return {
    legacy_strategy_type: normalizeLegacyStrategyType(
      strategy.legacy_strategy_type,
    ),
    failure_status_codes: normalizeStatusCodes(strategy.failure_status_codes),
    ban_mode: normalizeBanMode(strategy.ban_mode),
    retry_base_delay_ms: normalizeInteger(
      strategy.retry_base_delay_ms,
      "retry_base_delay_ms",
    ),
    retry_backoff_multiplier: normalizeNumber(
      strategy.retry_backoff_multiplier,
      "retry_backoff_multiplier",
    ),
    retry_jitter_ratio: normalizeNumber(
      strategy.retry_jitter_ratio,
      "retry_jitter_ratio",
    ),
    retry_max_delay_ms: normalizeInteger(
      strategy.retry_max_delay_ms,
      "retry_max_delay_ms",
    ),
    cycle_retry_attempt_limit: normalizeInteger(
      strategy.cycle_retry_attempt_limit,
      "cycle_retry_attempt_limit",
    ),
    ban_cumulative_retry_attempt_threshold: normalizeInteger(
      strategy.ban_cumulative_retry_attempt_threshold,
      "ban_cumulative_retry_attempt_threshold",
    ),
    ban_duration_seconds: normalizeInteger(
      strategy.ban_duration_seconds,
      "ban_duration_seconds",
    ),
  };
}

export function normalizeLoadbalanceStrategySummary(
  strategy: RawLoadbalanceStrategySummary | null,
): LoadbalanceStrategySummary | null {
  if (!strategy) {
    return null;
  }

  return {
    id: strategy.id,
    name: strategy.name,
    ...normalizeLoadbalanceBanPolicyFields(strategy),
  };
}

function normalizeLoadbalanceStrategy(
  strategy: RawLoadbalanceStrategy,
): LoadbalanceStrategy {
  return {
    id: strategy.id,
    profile_id: strategy.profile_id,
    name: strategy.name,
    is_default: strategy.is_default === true,
    ...normalizeLoadbalanceBanPolicyFields(strategy),
    attached_model_count: strategy.attached_model_count,
    created_at: strategy.created_at,
    updated_at: strategy.updated_at,
  };
}

export const loadbalanceStrategies = {
  list: () =>
    request<RawLoadbalanceStrategy[]>("/api/loadbalance/strategies").then(
      (strategies) => strategies.map(normalizeLoadbalanceStrategy),
    ),
  createDefaults: () =>
    request<LoadbalanceStrategyDefaultsResponse>(
      "/api/loadbalance/strategies/defaults",
      {
        method: "POST",
      },
    ),
  setDefault: (strategyId: number, expectedDefaultStrategyId: number | null) =>
    request<StrategySetDefaultResponse>(
      `/api/loadbalance/strategies/${strategyId}/default`,
      {
        method: "PUT",
        body: JSON.stringify({
          expected_default_strategy_id: expectedDefaultStrategyId,
        } satisfies StrategySetDefaultRequest),
      },
    ),
  impact: (strategyId: number, params: { limit?: number; cursor?: string }) => {
    const query = buildQuery(params);
    return request<StrategyImpactListResponse>(
      `/api/loadbalance/strategies/${strategyId}/models${query ? `?${query}` : ""}`,
    );
  },
  preview: (data: StrategyPreviewDraft) =>
    request<StrategyPreviewResponse>("/api/loadbalance/strategies/preview", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  get: (id: number) =>
    request<RawLoadbalanceStrategy>(`/api/loadbalance/strategies/${id}`).then(
      normalizeLoadbalanceStrategy,
    ),
  create: (data: LoadbalanceStrategyCreate) =>
    request<RawLoadbalanceStrategy>("/api/loadbalance/strategies", {
      method: "POST",
      body: JSON.stringify(data),
    }).then(normalizeLoadbalanceStrategy),
  update: (id: number, data: LoadbalanceStrategyUpdate) =>
    request<RawLoadbalanceStrategy>(`/api/loadbalance/strategies/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }).then(normalizeLoadbalanceStrategy),
  delete: (id: number) =>
    request<{ deleted: boolean }>(`/api/loadbalance/strategies/${id}`, {
      method: "DELETE",
    }),
};
