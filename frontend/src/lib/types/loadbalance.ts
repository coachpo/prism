export type LoadbalanceEventType =
  | "opened"
  | "extended"
  | "max_cooldown_strike"
  | "banned"
  | "recovered"
  | "not_opened";

export type LoadbalanceStrategyFamily = "legacy" | "adaptive";
export type LegacyLoadbalanceStrategyType = "single" | "fill-first" | "round-robin";
export type AdaptiveRoutingObjective = "maximize_availability" | "minimize_latency";

export type LoadbalanceBanMode = "off" | "temporary" | "manual";

export type LoadbalanceFailureKind =
  | "transient_http"
  | "connect_error"
  | "timeout";

export interface LoadbalanceRoutingPolicy {
  kind: "adaptive";
  routing_objective: AdaptiveRoutingObjective;
  deadline_budget_ms: number;
  hedge: {
    enabled: boolean;
    delay_ms: number;
    max_additional_attempts: number;
  };
  circuit_breaker: {
    failure_status_codes: number[];
    base_open_seconds: number;
    failure_threshold: number;
    backoff_multiplier: number;
    max_open_seconds: number;
    jitter_ratio: number;
    ban_mode: LoadbalanceBanMode;
    max_open_strikes_before_ban: number;
    ban_duration_seconds: number;
  };
  admission: {
    respect_qps_limit: boolean;
    respect_in_flight_limits: boolean;
  };
}

export type LoadbalanceCurrentStateValue =
  | "counting"
  | "blocked"
  | "banned";

export interface LoadbalanceAutoRecoveryCooldown {
  base_seconds: number;
  failure_threshold: number;
  backoff_multiplier: number;
  max_cooldown_seconds: number;
  jitter_ratio: number;
}

export type LoadbalanceAutoRecoveryBan =
  | {
      mode: "off";
    }
  | {
      mode: "manual";
      max_cooldown_strikes_before_ban: number;
    }
  | {
      mode: "temporary";
      max_cooldown_strikes_before_ban: number;
      ban_duration_seconds: number;
    };

export interface LoadbalanceAutoRecoveryEnabled {
  mode: "enabled";
  status_codes: number[];
  cooldown: LoadbalanceAutoRecoveryCooldown;
  ban: LoadbalanceAutoRecoveryBan;
}

export type LoadbalanceAutoRecovery =
  | {
      mode: "disabled";
    }
  | LoadbalanceAutoRecoveryEnabled;

export interface LegacyLoadbalanceStrategySummary {
  id: number;
  name: string;
  strategy_type: "legacy";
  legacy_strategy_type: LegacyLoadbalanceStrategyType;
  auto_recovery: LoadbalanceAutoRecovery;
}

export interface AdaptiveLoadbalanceStrategySummary {
  id: number;
  name: string;
  strategy_type: "adaptive";
  routing_policy: LoadbalanceRoutingPolicy;
}

export type LoadbalanceStrategySummary =
  | LegacyLoadbalanceStrategySummary
  | AdaptiveLoadbalanceStrategySummary;

export type LoadbalanceStrategy =
  | (LegacyLoadbalanceStrategySummary & {
      profile_id: number;
      attached_model_count: number;
      created_at: string;
      updated_at: string;
    })
  | (AdaptiveLoadbalanceStrategySummary & {
      profile_id: number;
      attached_model_count: number;
      created_at: string;
      updated_at: string;
    });

export type LoadbalanceStrategyCreate =
  | {
      name: string;
      strategy_type: "legacy";
      legacy_strategy_type: LegacyLoadbalanceStrategyType;
      auto_recovery: LoadbalanceAutoRecovery;
    }
  | {
      name: string;
      strategy_type: "adaptive";
      routing_policy: LoadbalanceRoutingPolicy;
    };

export type LoadbalanceStrategyUpdate = LoadbalanceStrategyCreate;

export interface LoadbalanceCurrentStateItem {
  connection_id: number;
  consecutive_failures: number;
  last_failure_kind: LoadbalanceFailureKind | null;
  last_cooldown_seconds: number;
  blocked_until_at: string | null;
  max_cooldown_strikes: number;
  ban_mode: LoadbalanceBanMode;
  banned_until_at: string | null;
  state: LoadbalanceCurrentStateValue;
  created_at: string;
  updated_at: string;
}

export interface LoadbalanceCurrentStateListResponse {
  items: LoadbalanceCurrentStateItem[];
}

export interface LoadbalanceCurrentStateResetResponse {
  connection_id: number;
  cleared: boolean;
}

export interface LoadbalanceEventSummary {
  event: string;
  reason: string;
  operation: string;
  cooldown: string;
}

export interface LoadbalanceEvent {
  id: number;
  profile_id: number;
  connection_id: number;
  event_type: LoadbalanceEventType;
  failure_kind: LoadbalanceFailureKind | null;
  consecutive_failures: number;
  cooldown_seconds: number;
  blocked_until_mono: number | null;
  model_id: string | null;
  endpoint_id: number | null;
  vendor_id: number | null;
  summary: LoadbalanceEventSummary;
  created_at: string;
}

export interface LoadbalanceEventDetail extends LoadbalanceEvent {
  failure_threshold: number | null;
  backoff_multiplier: number | null;
  max_cooldown_seconds: number | null;
  max_cooldown_strikes: number | null;
  ban_mode: LoadbalanceBanMode | null;
  banned_until_at: string | null;
}

export interface LoadbalanceEventListResponse {
  items: LoadbalanceEvent[];
  total: number;
  limit: number;
  offset: number;
}

export interface LoadbalanceEventDeleteResponse {
  accepted: boolean;
}
