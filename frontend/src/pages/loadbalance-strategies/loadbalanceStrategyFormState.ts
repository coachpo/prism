import type {
  LoadbalanceAutoRecovery,
  LoadbalanceBanMode,
  LoadbalanceRoutingPolicy,
  LoadbalanceStrategy,
  LegacyLoadbalanceStrategyType,
} from "@/lib/types";
import {
  createDefaultAdaptiveRoutingPolicy,
  getDefaultAutoRecovery,
  normalizeFailureStatusCodes,
} from "@/lib/loadbalanceRoutingPolicy";
import { getStaticMessages } from "@/i18n/staticMessages";

type AutoRecoveryDisabledDraft = {
  mode: "disabled";
};

type AutoRecoveryEnabledDraft = {
  mode: "enabled";
  status_codes: number[];
  status_code_input: string;
  cooldown: {
    base_seconds: number;
    failure_threshold: number;
    backoff_multiplier: number;
    max_cooldown_seconds: number;
    jitter_ratio: number;
  };
  ban:
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
};

export type LoadbalanceAutoRecoveryDraft =
  | AutoRecoveryDisabledDraft
  | AutoRecoveryEnabledDraft;

type LegacyLoadbalanceStrategyFormState = {
  name: string;
  strategy_type: "legacy";
  legacy_strategy_type: LegacyLoadbalanceStrategyType;
  auto_recovery: LoadbalanceAutoRecoveryDraft;
};

type AdaptiveLoadbalanceStrategyFormState = {
  name: string;
  strategy_type: "adaptive";
  routing_policy: LoadbalanceRoutingPolicy;
  circuit_breaker_status_code_input: string;
};

export type LoadbalanceStrategyFormState =
  | LegacyLoadbalanceStrategyFormState
  | AdaptiveLoadbalanceStrategyFormState;

export type LoadbalanceStrategyFormPayload =
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

function normalizeInteger(value: number) {
  return Math.trunc(value);
}

type CircuitBreakerValidationState = {
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

function autoRecoveryDraftFromValue(autoRecovery: LoadbalanceAutoRecovery): LoadbalanceAutoRecoveryDraft {
  if (autoRecovery.mode === "disabled") {
    return { mode: "disabled" };
  }

  return {
    mode: "enabled",
    status_codes: normalizeFailureStatusCodes(autoRecovery.status_codes),
    status_code_input: "",
    cooldown: {
      ...autoRecovery.cooldown,
    },
    ban:
      autoRecovery.ban.mode === "off"
        ? { mode: "off" }
        : autoRecovery.ban.mode === "manual"
          ? {
              mode: "manual",
              max_cooldown_strikes_before_ban: autoRecovery.ban.max_cooldown_strikes_before_ban,
            }
          : {
              mode: "temporary",
              max_cooldown_strikes_before_ban: autoRecovery.ban.max_cooldown_strikes_before_ban,
              ban_duration_seconds: autoRecovery.ban.ban_duration_seconds,
            },
  };
}

export function getDefaultAutoRecoveryDraft(
  strategyType: LegacyLoadbalanceStrategyType,
): LoadbalanceAutoRecoveryDraft {
  return autoRecoveryDraftFromValue(getDefaultAutoRecovery(strategyType));
}

export const DEFAULT_LOADBALANCE_STRATEGY_FORM: LoadbalanceStrategyFormState = {
  name: "",
  strategy_type: "legacy",
  legacy_strategy_type: "single",
  auto_recovery: getDefaultAutoRecoveryDraft("single"),
};

function adaptiveFormStateFromRoutingPolicy(
  name: string,
  routingPolicy: LoadbalanceRoutingPolicy,
): AdaptiveLoadbalanceStrategyFormState {
  return {
    name,
    strategy_type: "adaptive",
    routing_policy: { ...routingPolicy },
    circuit_breaker_status_code_input: "",
  };
}

export function loadbalanceStrategyFormStateFromStrategy(
  strategy: LoadbalanceStrategy,
): LoadbalanceStrategyFormState {
  if (strategy.strategy_type === "adaptive") {
    return adaptiveFormStateFromRoutingPolicy(strategy.name, strategy.routing_policy);
  }

  return {
    name: strategy.name,
    strategy_type: "legacy",
    legacy_strategy_type: strategy.legacy_strategy_type,
    auto_recovery: autoRecoveryDraftFromValue(strategy.auto_recovery),
  };
}

export function setLoadbalanceStrategyFamily(
  formState: LoadbalanceStrategyFormState,
  strategyFamily: LoadbalanceStrategyFormState["strategy_type"],
): LoadbalanceStrategyFormState {
  if (strategyFamily === formState.strategy_type) {
    return formState;
  }

  if (strategyFamily === "adaptive") {
    return adaptiveFormStateFromRoutingPolicy(formState.name, createDefaultAdaptiveRoutingPolicy());
  }

  return {
    name: formState.name,
    strategy_type: "legacy",
    legacy_strategy_type: "single",
    auto_recovery: getDefaultAutoRecoveryDraft("single"),
  };
}

export function setLegacyLoadbalanceStrategyType(
  formState: LoadbalanceStrategyFormState,
  strategyType: LegacyLoadbalanceStrategyType,
): LoadbalanceStrategyFormState {
  if (formState.strategy_type !== "legacy" || strategyType === formState.legacy_strategy_type) {
    return formState;
  }

  return {
    ...formState,
    legacy_strategy_type: strategyType,
    auto_recovery:
      formState.auto_recovery.mode === "enabled"
        ? formState.auto_recovery
        : getDefaultAutoRecoveryDraft(strategyType),
  };
}

export function setLoadbalanceStrategyAutoRecoveryMode(
  formState: LoadbalanceStrategyFormState,
  mode: "disabled" | "enabled",
): LoadbalanceStrategyFormState {
  if (formState.strategy_type !== "legacy") {
    return formState;
  }

  if (mode === "disabled") {
    return {
      ...formState,
      auto_recovery: { mode: "disabled" },
    };
  }

  return {
    ...formState,
    auto_recovery:
      formState.auto_recovery.mode === "enabled"
        ? formState.auto_recovery
        : getDefaultAutoRecoveryDraft(formState.legacy_strategy_type),
  };
}

export function setLoadbalanceStrategyBanMode(
  formState: LoadbalanceStrategyFormState,
  mode: LoadbalanceBanMode,
): LoadbalanceStrategyFormState {
  if (formState.strategy_type === "legacy") {
    if (formState.auto_recovery.mode !== "enabled") {
      return formState;
    }

    const currentBan = formState.auto_recovery.ban;

    return {
      ...formState,
      auto_recovery: {
        ...formState.auto_recovery,
        ban:
          mode === "off"
            ? { mode: "off" }
            : mode === "manual"
              ? {
                  mode: "manual",
                  max_cooldown_strikes_before_ban:
                    currentBan.mode === "off"
                      ? 1
                      : Math.max(currentBan.max_cooldown_strikes_before_ban, 1),
                }
              : {
                  mode: "temporary",
                  max_cooldown_strikes_before_ban:
                    currentBan.mode === "off"
                      ? 1
                      : Math.max(currentBan.max_cooldown_strikes_before_ban, 1),
                  ban_duration_seconds:
                    currentBan.mode === "temporary"
                      ? Math.max(currentBan.ban_duration_seconds, 1)
                      : 1,
                },
      },
    };
  }

  const currentCircuitBreaker = formState.routing_policy.circuit_breaker;

  return {
    ...formState,
    routing_policy: {
      ...formState.routing_policy,
      circuit_breaker:
        mode === "off"
          ? {
              ...currentCircuitBreaker,
              ban_mode: "off",
              max_open_strikes_before_ban: 0,
              ban_duration_seconds: 0,
            }
          : mode === "manual"
            ? {
                ...currentCircuitBreaker,
                ban_mode: "manual",
                max_open_strikes_before_ban: Math.max(
                  currentCircuitBreaker.max_open_strikes_before_ban,
                  1,
                ),
                ban_duration_seconds: 0,
              }
            : {
                ...currentCircuitBreaker,
                ban_mode: "temporary",
                max_open_strikes_before_ban: Math.max(
                  currentCircuitBreaker.max_open_strikes_before_ban,
                  1,
                ),
                ban_duration_seconds: Math.max(currentCircuitBreaker.ban_duration_seconds, 1),
              },
    },
  };
}

export function getCircuitBreakerStatusCodeInputError(
  formState: Pick<AutoRecoveryEnabledDraft, "status_codes" | "status_code_input">,
): string | null {
  const messages = getStaticMessages().loadbalanceStrategyValidation;
  const rawValue = (formState.status_code_input ?? "").trim();

  if (!rawValue) {
    return null;
  }

  if (!/^\d+$/.test(rawValue)) {
    return messages.statusCodeIntegerRange;
  }

  const statusCode = Number(rawValue);
  if (statusCode < 100 || statusCode > 599) {
    return messages.statusCodeIntegerRange;
  }

  if (formState.status_codes.includes(statusCode)) {
    return messages.statusCodeExists;
  }

  return null;
}

export function addCircuitBreakerStatusCode(
  formState: LoadbalanceStrategyFormState,
): LoadbalanceStrategyFormState {
  if (formState.strategy_type === "legacy") {
    if (formState.auto_recovery.mode !== "enabled") {
      return formState;
    }

    if (getCircuitBreakerStatusCodeInputError(formState.auto_recovery)) {
      return formState;
    }

    const nextStatusCode = Number(formState.auto_recovery.status_code_input.trim());

    return {
      ...formState,
      auto_recovery: {
        ...formState.auto_recovery,
        status_codes: normalizeFailureStatusCodes([
          ...formState.auto_recovery.status_codes,
          nextStatusCode,
        ]),
        status_code_input: "",
      },
    };
  }

  if (
    getCircuitBreakerStatusCodeInputError({
      status_codes: formState.routing_policy.circuit_breaker.failure_status_codes,
      status_code_input: formState.circuit_breaker_status_code_input,
    })
  ) {
    return formState;
  }

  const nextStatusCode = Number(formState.circuit_breaker_status_code_input.trim());

  return {
    ...formState,
    routing_policy: {
      ...formState.routing_policy,
      circuit_breaker: {
        ...formState.routing_policy.circuit_breaker,
        failure_status_codes: normalizeFailureStatusCodes([
          ...formState.routing_policy.circuit_breaker.failure_status_codes,
          nextStatusCode,
        ]),
      },
    },
    circuit_breaker_status_code_input: "",
  };
}

export function removeCircuitBreakerStatusCode(
  formState: LoadbalanceStrategyFormState,
  statusCodeToRemove: number,
): LoadbalanceStrategyFormState {
  if (formState.strategy_type === "legacy") {
    if (formState.auto_recovery.mode !== "enabled") {
      return formState;
    }

    return {
      ...formState,
      auto_recovery: {
        ...formState.auto_recovery,
        status_codes: formState.auto_recovery.status_codes.filter(
          (statusCode) => statusCode !== statusCodeToRemove,
        ),
      },
    };
  }

  return {
    ...formState,
    routing_policy: {
      ...formState.routing_policy,
      circuit_breaker: {
        ...formState.routing_policy.circuit_breaker,
        failure_status_codes: formState.routing_policy.circuit_breaker.failure_status_codes.filter(
          (statusCode) => statusCode !== statusCodeToRemove,
        ),
      },
    },
  };
}

function autoRecoveryDraftToPayload(autoRecovery: LoadbalanceAutoRecoveryDraft): LoadbalanceAutoRecovery {
  if (autoRecovery.mode === "disabled") {
    return { mode: "disabled" };
  }

  return {
    mode: "enabled",
    status_codes: normalizeFailureStatusCodes(autoRecovery.status_codes),
    cooldown: {
      base_seconds: normalizeInteger(autoRecovery.cooldown.base_seconds),
      failure_threshold: normalizeInteger(autoRecovery.cooldown.failure_threshold),
      backoff_multiplier: autoRecovery.cooldown.backoff_multiplier,
      max_cooldown_seconds: normalizeInteger(autoRecovery.cooldown.max_cooldown_seconds),
      jitter_ratio: autoRecovery.cooldown.jitter_ratio,
    },
    ban:
      autoRecovery.ban.mode === "off"
        ? { mode: "off" }
        : autoRecovery.ban.mode === "manual"
          ? {
              mode: "manual",
              max_cooldown_strikes_before_ban: Math.max(
                normalizeInteger(autoRecovery.ban.max_cooldown_strikes_before_ban),
                1,
              ),
            }
          : {
              mode: "temporary",
              max_cooldown_strikes_before_ban: Math.max(
                normalizeInteger(autoRecovery.ban.max_cooldown_strikes_before_ban),
                1,
              ),
              ban_duration_seconds: Math.max(
                normalizeInteger(autoRecovery.ban.ban_duration_seconds),
                1,
              ),
            },
  };
}

export function toLoadbalanceStrategyPayload(
  formState: LoadbalanceStrategyFormState,
): LoadbalanceStrategyFormPayload {
  if (formState.strategy_type === "adaptive") {
    return {
      name: formState.name.trim(),
      strategy_type: "adaptive",
      routing_policy: { ...formState.routing_policy },
    };
  }

  return {
    name: formState.name.trim(),
    strategy_type: "legacy",
    legacy_strategy_type: formState.legacy_strategy_type,
    auto_recovery: autoRecoveryDraftToPayload(formState.auto_recovery),
  };
}

export function getLoadbalanceStrategyFormValidationError(
  formState: LoadbalanceStrategyFormState,
): string | null {
  const messages = getStaticMessages().loadbalanceStrategyValidation;

  if (!formState.name.trim()) {
    return messages.nameRequired;
  }

  if (formState.strategy_type === "adaptive") {
    return getCircuitBreakerValidationError(formState.routing_policy.circuit_breaker, messages);
  }

  if (formState.auto_recovery.mode === "disabled") {
    return null;
  }

  const autoRecovery = formState.auto_recovery;

  return getCircuitBreakerValidationError(
    {
      failure_status_codes: autoRecovery.status_codes,
      base_open_seconds: autoRecovery.cooldown.base_seconds,
      failure_threshold: autoRecovery.cooldown.failure_threshold,
      backoff_multiplier: autoRecovery.cooldown.backoff_multiplier,
      max_open_seconds: autoRecovery.cooldown.max_cooldown_seconds,
      jitter_ratio: autoRecovery.cooldown.jitter_ratio,
      ban_mode: autoRecovery.ban.mode,
      max_open_strikes_before_ban:
        autoRecovery.ban.mode === "off" ? 0 : autoRecovery.ban.max_cooldown_strikes_before_ban,
      ban_duration_seconds:
        autoRecovery.ban.mode === "temporary" ? autoRecovery.ban.ban_duration_seconds : 0,
    },
    messages,
  );
}

function getCircuitBreakerValidationError(
  circuitBreaker: CircuitBreakerValidationState,
  messages: ReturnType<typeof getStaticMessages>["loadbalanceStrategyValidation"],
): string | null {
  if (circuitBreaker.failure_status_codes.length === 0) {
    return messages.addStatusCode;
  }

  if (
    new Set(circuitBreaker.failure_status_codes).size !== circuitBreaker.failure_status_codes.length
  ) {
    return messages.statusCodesUnique;
  }

  if (
    circuitBreaker.failure_status_codes.some(
      (statusCode) => !Number.isInteger(statusCode) || statusCode < 100 || statusCode > 599,
    )
  ) {
    return messages.statusCodesValidHttp;
  }

  if (!Number.isInteger(circuitBreaker.base_open_seconds)) {
    return messages.baseCooldownIntegerSeconds;
  }
  if (circuitBreaker.base_open_seconds < 0) {
    return messages.baseCooldownMin;
  }

  if (!Number.isInteger(circuitBreaker.failure_threshold)) {
    return messages.failureThresholdInteger;
  }
  if (circuitBreaker.failure_threshold < 1 || circuitBreaker.failure_threshold > 50) {
    return messages.failureThresholdRange;
  }

  if (
    !Number.isFinite(circuitBreaker.backoff_multiplier) ||
    circuitBreaker.backoff_multiplier < 1 ||
    circuitBreaker.backoff_multiplier > 10
  ) {
    return messages.backoffMultiplierRange;
  }

  if (!Number.isInteger(circuitBreaker.max_open_seconds)) {
    return messages.maxCooldownIntegerSeconds;
  }
  if (circuitBreaker.max_open_seconds < 1 || circuitBreaker.max_open_seconds > 86_400) {
    return messages.maxCooldownRange;
  }

  if (
    !Number.isFinite(circuitBreaker.jitter_ratio) ||
    circuitBreaker.jitter_ratio < 0 ||
    circuitBreaker.jitter_ratio > 1
  ) {
    return messages.jitterRatioRange;
  }

  if (circuitBreaker.ban_mode === "off") {
    if (
      circuitBreaker.max_open_strikes_before_ban !== 0 ||
      circuitBreaker.ban_duration_seconds !== 0
    ) {
      return messages.banModeOffZero;
    }

    return null;
  }

  if (!Number.isInteger(circuitBreaker.max_open_strikes_before_ban)) {
    return messages.maxCooldownStrikesInteger;
  }
  if (circuitBreaker.max_open_strikes_before_ban < 1) {
    return messages.maxCooldownStrikesMin;
  }

  if (circuitBreaker.ban_mode === "manual") {
    if (circuitBreaker.ban_duration_seconds !== 0) {
      return messages.banDurationManualDismissZero;
    }

    return null;
  }

  if (!Number.isInteger(circuitBreaker.ban_duration_seconds)) {
    return messages.banDurationIntegerSeconds;
  }

  if (circuitBreaker.ban_duration_seconds < 1) {
    return messages.banDurationTemporaryMin;
  }

  return null;
}

export function getAttachedModelCountFromErrorDetail(detail: unknown): number | null {
  if (!detail || typeof detail !== "object") {
    return null;
  }

  const payload = detail as { detail?: unknown; attached_model_count?: unknown };
  if (typeof payload.attached_model_count === "number") {
    return payload.attached_model_count;
  }

  if (!payload.detail || typeof payload.detail !== "object") {
    return null;
  }

  const nestedDetail = payload.detail as { attached_model_count?: unknown };
  return typeof nestedDetail.attached_model_count === "number"
    ? nestedDetail.attached_model_count
    : null;
}
