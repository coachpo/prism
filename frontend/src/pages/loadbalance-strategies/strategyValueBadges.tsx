import type { LoadbalanceStrategy } from "@/lib/types"
import { getStaticMessages } from "@/i18n/staticMessages"

/**
 * Milliseconds are a wire unit, not a reading unit. `60000ms` and `900000ms`
 * sitting in one sentence force the operator to count zeros to tell a first
 * retry from a cap, so every duration is rendered the way a person says it.
 */
export function formatDurationMs(milliseconds: number): string {
  if (milliseconds < 1000) return `${milliseconds}ms`
  const seconds = milliseconds / 1000
  if (seconds < 60) return `${trimZero(seconds)}s`
  const minutes = seconds / 60
  if (minutes < 60) return `${trimZero(minutes)}min`
  return `${trimZero(minutes / 60)}h`
}

export function formatDurationSeconds(seconds: number): string {
  return formatDurationMs(seconds * 1000)
}

function trimZero(value: number): string {
  return Number.isInteger(value) ? String(value) : value.toFixed(1)
}

export type StrategyValueBadge = { key: string; label: string }

/** The retry policy, one fact per badge. */
export function retryBadges(strategy: LoadbalanceStrategy): StrategyValueBadge[] {
  const copy = getStaticMessages().routingStrategyTable
  return [
    { key: "first", label: copy.retryFirstDelay(formatDurationMs(strategy.retry_base_delay_ms)) },
    { key: "multiplier", label: copy.retryMultiplier(String(strategy.retry_backoff_multiplier)) },
    { key: "jitter", label: copy.retryJitter(`${Math.round(strategy.retry_jitter_ratio * 100)}%`) },
    { key: "max", label: copy.retryMaxDelay(formatDurationMs(strategy.retry_max_delay_ms)) },
    { key: "cycle", label: copy.retryPerCycle(String(strategy.cycle_retry_attempt_limit)) },
  ]
}

/** The ban policy, one fact per badge. Ban off yields no badges. */
export function banBadges(strategy: LoadbalanceStrategy): StrategyValueBadge[] {
  const copy = getStaticMessages().routingStrategyTable
  if (strategy.ban_mode === "off") return []
  const badges: StrategyValueBadge[] = [
    { key: "threshold", label: copy.banThreshold(String(strategy.ban_cumulative_retry_attempt_threshold)) },
  ]
  badges.push(
    strategy.ban_mode === "until_reset"
      ? { key: "duration", label: copy.banUntilResetShort }
      : { key: "duration", label: copy.banDuration(formatDurationSeconds(strategy.ban_duration_seconds)) },
  )
  return badges
}

export function failureStatusCodeLabel(strategy: LoadbalanceStrategy): string {
  const copy = getStaticMessages().routingStrategyTable
  if (strategy.failure_status_codes.length === 0) return copy.failureStatusCodesNone
  return copy.failureStatusCodes(strategy.failure_status_codes.join(", "))
}
