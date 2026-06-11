import { z } from "zod"
import { DEFAULT_BAN_POLICY_FIELDS, normalizeFailureStatusCodes } from "@/lib/loadbalanceRoutingPolicy"
import type { LoadbalanceStrategy, LoadbalanceStrategyCreate, LoadbalanceStrategyUpdate, LegacyLoadbalanceStrategyType, LoadbalanceBanMode } from "@/lib/types"

export const banPolicyRoutingTypes = ["single", "fill-first", "round-robin", "cheapest_eligible_context"] as const
export const banPolicyModes = ["off", "temporary", "until_reset"] as const

export type BanPolicyFormValues = {
  name: string
  legacy_strategy_type: LegacyLoadbalanceStrategyType
  failure_status_codes_input: string
  ban_mode: LoadbalanceBanMode
  retry_base_delay_ms: number
  retry_backoff_multiplier: number
  retry_jitter_ratio: number
  retry_max_delay_ms: number
  cycle_retry_attempt_limit: number
  ban_cumulative_retry_attempt_threshold: number
  ban_duration_seconds: number
}

function parseFailureStatusCodes(value: string): number[] {
  return normalizeFailureStatusCodes(value.split(/[\s,]+/).map((token) => Number(token.trim())).filter(Number.isFinite))
}

const statusCodeTokenSchema = z.string().superRefine((value, context) => {
  const tokens = value.split(/[\s,]+/).map((token) => token.trim()).filter(Boolean)
  if (tokens.length === 0) {
    context.addIssue({ code: "custom", message: "Add at least one failure status code" })
    return
  }
  const seen = new Set<number>()
  for (const token of tokens) {
    if (!/^\d+$/.test(token)) {
      context.addIssue({ code: "custom", message: "Status code must be a whole number between 100 and 599" })
      continue
    }
    const code = Number(token)
    if (code < 100 || code > 599) {
      context.addIssue({ code: "custom", message: "Status code must be a whole number between 100 and 599" })
      continue
    }
    if (seen.has(code)) {
      context.addIssue({ code: "custom", message: "Failure status codes must be unique" })
    }
    seen.add(code)
  }
})

export const banPolicyFormSchema = z.object({
  name: z.string().trim().min(1, "Name is required"),
  legacy_strategy_type: z.enum(banPolicyRoutingTypes),
  failure_status_codes_input: statusCodeTokenSchema,
  ban_mode: z.enum(banPolicyModes),
  retry_base_delay_ms: z.coerce.number().int("Retry base delay must be a whole number of milliseconds").min(0, "Retry base delay must be between 0 and 86400000 milliseconds").max(86_400_000, "Retry base delay must be between 0 and 86400000 milliseconds"),
  retry_backoff_multiplier: z.coerce.number().min(1, "Backoff multiplier must be between 1 and 10").max(10, "Backoff multiplier must be between 1 and 10"),
  retry_jitter_ratio: z.coerce.number().min(0, "Retry jitter ratio must be between 0 and 1").max(1, "Retry jitter ratio must be between 0 and 1"),
  retry_max_delay_ms: z.coerce.number().int("Retry max delay must be a whole number of milliseconds").min(1, "Retry max delay must be between 1 and 86400000 milliseconds").max(86_400_000, "Retry max delay must be between 1 and 86400000 milliseconds"),
  cycle_retry_attempt_limit: z.coerce.number().int("Cycle retry attempt limit must be a whole number").min(1, "Cycle retry attempt limit must be between 1 and 50").max(50, "Cycle retry attempt limit must be between 1 and 50"),
  ban_cumulative_retry_attempt_threshold: z.coerce.number().int("Ban cumulative retry attempt threshold must be a whole number").min(0),
  ban_duration_seconds: z.coerce.number().int("Ban duration must be a whole number of seconds").min(0),
}).superRefine((value, context) => {
  if (value.ban_mode === "off" && value.ban_cumulative_retry_attempt_threshold !== 0) {
    context.addIssue({ code: "custom", path: ["ban_cumulative_retry_attempt_threshold"], message: "Ban mode Off requires cumulative threshold 0" })
  }
  if (value.ban_mode !== "off") {
    if (value.ban_cumulative_retry_attempt_threshold < 1 || value.ban_cumulative_retry_attempt_threshold > 500) {
      context.addIssue({ code: "custom", path: ["ban_cumulative_retry_attempt_threshold"], message: "Ban cumulative retry attempt threshold must be between 1 and 500 when banning is enabled" })
    }
    if (value.ban_cumulative_retry_attempt_threshold < value.cycle_retry_attempt_limit) {
      context.addIssue({ code: "custom", path: ["ban_cumulative_retry_attempt_threshold"], message: "Ban cumulative retry attempt threshold must be greater than or equal to the cycle retry attempt limit" })
    }
  }
  if (value.ban_mode === "temporary" && value.ban_duration_seconds < 1) {
    context.addIssue({ code: "custom", path: ["ban_duration_seconds"], message: "Ban duration must be at least 1 second for temporary bans" })
  }
  if (value.ban_mode !== "temporary" && value.ban_duration_seconds !== 0) {
    context.addIssue({ code: "custom", path: ["ban_duration_seconds"], message: "Ban duration must be 0 seconds for off or until-reset bans" })
  }
})

export type ParsedBanPolicyForm = z.output<typeof banPolicyFormSchema>

export const DEFAULT_BAN_POLICY_FORM_VALUES: BanPolicyFormValues = {
  name: "",
  legacy_strategy_type: "single",
  failure_status_codes_input: DEFAULT_BAN_POLICY_FIELDS.failure_status_codes.join(", "),
  ban_mode: DEFAULT_BAN_POLICY_FIELDS.ban_mode,
  retry_base_delay_ms: DEFAULT_BAN_POLICY_FIELDS.retry_base_delay_ms,
  retry_backoff_multiplier: DEFAULT_BAN_POLICY_FIELDS.retry_backoff_multiplier,
  retry_jitter_ratio: DEFAULT_BAN_POLICY_FIELDS.retry_jitter_ratio,
  retry_max_delay_ms: DEFAULT_BAN_POLICY_FIELDS.retry_max_delay_ms,
  cycle_retry_attempt_limit: DEFAULT_BAN_POLICY_FIELDS.cycle_retry_attempt_limit,
  ban_cumulative_retry_attempt_threshold: DEFAULT_BAN_POLICY_FIELDS.ban_cumulative_retry_attempt_threshold,
  ban_duration_seconds: DEFAULT_BAN_POLICY_FIELDS.ban_duration_seconds,
}

export function banPolicyFormValuesFromStrategy(strategy: LoadbalanceStrategy): BanPolicyFormValues {
  return {
    name: strategy.name,
    legacy_strategy_type: strategy.legacy_strategy_type,
    failure_status_codes_input: normalizeFailureStatusCodes(strategy.failure_status_codes).join(", "),
    ban_mode: strategy.ban_mode,
    retry_base_delay_ms: strategy.retry_base_delay_ms,
    retry_backoff_multiplier: strategy.retry_backoff_multiplier,
    retry_jitter_ratio: strategy.retry_jitter_ratio,
    retry_max_delay_ms: strategy.retry_max_delay_ms,
    cycle_retry_attempt_limit: strategy.cycle_retry_attempt_limit,
    ban_cumulative_retry_attempt_threshold: strategy.ban_cumulative_retry_attempt_threshold,
    ban_duration_seconds: strategy.ban_duration_seconds,
  }
}

export function buildBanPolicyPayload(values: BanPolicyFormValues): LoadbalanceStrategyCreate {
  const parsed = banPolicyFormSchema.parse(values)
  return {
    name: parsed.name,
    legacy_strategy_type: parsed.legacy_strategy_type,
    failure_status_codes: parseFailureStatusCodes(parsed.failure_status_codes_input),
    ban_mode: parsed.ban_mode,
    retry_base_delay_ms: parsed.retry_base_delay_ms,
    retry_backoff_multiplier: parsed.retry_backoff_multiplier,
    retry_jitter_ratio: parsed.retry_jitter_ratio,
    retry_max_delay_ms: parsed.retry_max_delay_ms,
    cycle_retry_attempt_limit: parsed.cycle_retry_attempt_limit,
    ban_cumulative_retry_attempt_threshold: parsed.ban_cumulative_retry_attempt_threshold,
    ban_duration_seconds: parsed.ban_mode === "temporary" ? parsed.ban_duration_seconds : 0,
  }
}

export function buildBanPolicyUpdatePayload(values: BanPolicyFormValues): LoadbalanceStrategyUpdate {
  return buildBanPolicyPayload(values)
}

export function getAttachedModelCountFromDeleteDetail(detail: unknown): number | null {
  if (!detail || typeof detail !== "object") return null
  const payload = detail as { attached_model_count?: unknown; detail?: unknown }
  if (typeof payload.attached_model_count === "number") return payload.attached_model_count
  if (!payload.detail || typeof payload.detail !== "object") return null
  const nested = payload.detail as { attached_model_count?: unknown }
  return typeof nested.attached_model_count === "number" ? nested.attached_model_count : null
}
