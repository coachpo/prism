import type {
  LoadbalanceBanMode,
  LoadbalanceStrategy,
  LegacyLoadbalanceStrategyType,
} from "@/lib/types";
import {
  DEFAULT_BAN_POLICY_FIELDS,
  normalizeFailureStatusCodes,
} from "@/lib/loadbalanceRoutingPolicy";
import { getStaticMessages } from "@/i18n/staticMessages";

export type LoadbalanceStrategyFormState = {
  name: string;
  legacy_strategy_type: LegacyLoadbalanceStrategyType;
  failure_status_codes: number[];
  status_code_input: string;
  ban_mode: LoadbalanceBanMode;
  retry_base_delay_ms: number;
  retry_backoff_multiplier: number;
  retry_jitter_ratio: number;
  retry_max_delay_ms: number;
  retry_max_attempts: number;
  ban_duration_seconds: number;
};

export type LoadbalanceStrategyFormPayload = Omit<
  LoadbalanceStrategyFormState,
  "status_code_input"
>;

type BanPolicyValidationState = Omit<
  LoadbalanceStrategyFormState,
  "name" | "legacy_strategy_type" | "status_code_input"
>;

function normalizeInteger(value: number) {
  return Math.trunc(value);
}

function createDefaultBanPolicyFields() {
  return {
    ...DEFAULT_BAN_POLICY_FIELDS,
    failure_status_codes: [...DEFAULT_BAN_POLICY_FIELDS.failure_status_codes],
  };
}

export const DEFAULT_LOADBALANCE_STRATEGY_FORM: LoadbalanceStrategyFormState = {
  name: "",
  legacy_strategy_type: "single",
  status_code_input: "",
  ...createDefaultBanPolicyFields(),
};

export function loadbalanceStrategyFormStateFromStrategy(
  strategy: LoadbalanceStrategy,
): LoadbalanceStrategyFormState {
  return {
    name: strategy.name,
    legacy_strategy_type: strategy.legacy_strategy_type,
    failure_status_codes: normalizeFailureStatusCodes(strategy.failure_status_codes),
    status_code_input: "",
    ban_mode: strategy.ban_mode,
    retry_base_delay_ms: strategy.retry_base_delay_ms,
    retry_backoff_multiplier: strategy.retry_backoff_multiplier,
    retry_jitter_ratio: strategy.retry_jitter_ratio,
    retry_max_delay_ms: strategy.retry_max_delay_ms,
    retry_max_attempts: strategy.retry_max_attempts,
    ban_duration_seconds: strategy.ban_duration_seconds,
  };
}

export function setLegacyLoadbalanceStrategyType(
  formState: LoadbalanceStrategyFormState,
  strategyType: LegacyLoadbalanceStrategyType,
): LoadbalanceStrategyFormState {
  if (strategyType === formState.legacy_strategy_type) {
    return formState;
  }

  return {
    ...formState,
    legacy_strategy_type: strategyType,
  };
}

export function setLoadbalanceStrategyBanMode(
  formState: LoadbalanceStrategyFormState,
  mode: LoadbalanceBanMode,
): LoadbalanceStrategyFormState {
  if (mode === formState.ban_mode) {
    return formState;
  }

  return {
    ...formState,
    ban_mode: mode,
    ban_duration_seconds:
      mode === "temporary" ? Math.max(formState.ban_duration_seconds, 1) : 0,
  };
}

export function getCircuitBreakerStatusCodeInputError(
  formState: Pick<LoadbalanceStrategyFormState, "failure_status_codes" | "status_code_input">,
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

  if (formState.failure_status_codes.includes(statusCode)) {
    return messages.statusCodeExists;
  }

  return null;
}

export function addCircuitBreakerStatusCode(
  formState: LoadbalanceStrategyFormState,
): LoadbalanceStrategyFormState {
  if (getCircuitBreakerStatusCodeInputError(formState)) {
    return formState;
  }

  const nextStatusCode = Number(formState.status_code_input.trim());

  return {
    ...formState,
    failure_status_codes: normalizeFailureStatusCodes([
      ...formState.failure_status_codes,
      nextStatusCode,
    ]),
    status_code_input: "",
  };
}

export function removeCircuitBreakerStatusCode(
  formState: LoadbalanceStrategyFormState,
  statusCodeToRemove: number,
): LoadbalanceStrategyFormState {
  return {
    ...formState,
    failure_status_codes: formState.failure_status_codes.filter(
      (statusCode) => statusCode !== statusCodeToRemove,
    ),
  };
}

export function toLoadbalanceStrategyPayload(
  formState: LoadbalanceStrategyFormState,
): LoadbalanceStrategyFormPayload {
  const banDurationSeconds =
    formState.ban_mode === "temporary"
      ? Math.max(normalizeInteger(formState.ban_duration_seconds), 1)
      : 0;

  return {
    name: formState.name.trim(),
    legacy_strategy_type: formState.legacy_strategy_type,
    failure_status_codes: normalizeFailureStatusCodes(formState.failure_status_codes),
    ban_mode: formState.ban_mode,
    retry_base_delay_ms: normalizeInteger(formState.retry_base_delay_ms),
    retry_backoff_multiplier: formState.retry_backoff_multiplier,
    retry_jitter_ratio: formState.retry_jitter_ratio,
    retry_max_delay_ms: normalizeInteger(formState.retry_max_delay_ms),
    retry_max_attempts: normalizeInteger(formState.retry_max_attempts),
    ban_duration_seconds: banDurationSeconds,
  };
}

export function getLoadbalanceStrategyFormValidationError(
  formState: LoadbalanceStrategyFormState,
): string | null {
  const messages = getStaticMessages().loadbalanceStrategyValidation;

  if (!formState.name.trim()) {
    return messages.nameRequired;
  }

  return getBanPolicyValidationError(formState, messages);
}

function getBanPolicyValidationError(
  banPolicy: BanPolicyValidationState,
  messages: ReturnType<typeof getStaticMessages>["loadbalanceStrategyValidation"],
): string | null {
  if (banPolicy.failure_status_codes.length === 0) {
    return messages.addStatusCode;
  }

  if (new Set(banPolicy.failure_status_codes).size !== banPolicy.failure_status_codes.length) {
    return messages.statusCodesUnique;
  }

  if (
    banPolicy.failure_status_codes.some(
      (statusCode) => !Number.isInteger(statusCode) || statusCode < 100 || statusCode > 599,
    )
  ) {
    return messages.statusCodesValidHttp;
  }

  if (!Number.isInteger(banPolicy.retry_base_delay_ms)) {
    return messages.retryBaseDelayIntegerMs;
  }
  if (banPolicy.retry_base_delay_ms < 0 || banPolicy.retry_base_delay_ms > 86_400_000) {
    return messages.retryBaseDelayRange;
  }

  if (
    !Number.isFinite(banPolicy.retry_backoff_multiplier) ||
    banPolicy.retry_backoff_multiplier < 1 ||
    banPolicy.retry_backoff_multiplier > 10
  ) {
    return messages.backoffMultiplierRange;
  }

  if (
    !Number.isFinite(banPolicy.retry_jitter_ratio) ||
    banPolicy.retry_jitter_ratio < 0 ||
    banPolicy.retry_jitter_ratio > 1
  ) {
    return messages.retryJitterRatioRange;
  }

  if (!Number.isInteger(banPolicy.retry_max_delay_ms)) {
    return messages.retryMaxDelayIntegerMs;
  }
  if (banPolicy.retry_max_delay_ms < 1 || banPolicy.retry_max_delay_ms > 86_400_000) {
    return messages.retryMaxDelayRange;
  }

  if (!Number.isInteger(banPolicy.retry_max_attempts)) {
    return messages.retryMaxAttemptsInteger;
  }
  if (banPolicy.retry_max_attempts < 1 || banPolicy.retry_max_attempts > 50) {
    return messages.retryMaxAttemptsRange;
  }

  if (!Number.isInteger(banPolicy.ban_duration_seconds)) {
    return messages.banDurationIntegerSeconds;
  }

  if (banPolicy.ban_mode === "temporary") {
    return banPolicy.ban_duration_seconds < 1 ? messages.banDurationTemporaryMin : null;
  }

  if (banPolicy.ban_duration_seconds !== 0) {
    return messages.banDurationManualDismissZero;
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
