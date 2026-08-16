import type { ComponentProps, ReactNode } from "react"

import { cn } from "@/lib/utils"
import { OperatorTypeBadge } from "./status"

/**
 * The Honesty Contract's four situations, as components.
 *
 * Genuinely zero renders as an ordinary `0`; nothing here is needed for it.
 * Absent renders as `OperatorMissingValue`. Read failure belongs to
 * `OperatorErrorState`, never to an empty state. Clipped or truncated data
 * renders normally and carries `OperatorClippedBadge`.
 *
 * None of these ship copy: every label arrives from the caller's `messages`.
 */

export type OperatorMissingValueProps = ComponentProps<"span"> & {
  /**
   * Localized reason the value is absent, surfaced as a tooltip and to screen
   * readers. Without it the dash reads as "no value" and nothing more.
   */
  reason?: string
}

/** An absent value. Never `0`, never blank. */
export function OperatorMissingValue({
  className,
  reason,
  ...props
}: OperatorMissingValueProps) {
  return (
    <span
      data-slot="missing-value"
      title={reason}
      className={cn("font-mono tabular-nums text-muted-foreground", className)}
      {...props}
    >
      <span aria-hidden="true">—</span>
      {reason ? <span className="sr-only">{reason}</span> : null}
    </span>
  )
}

/**
 * Render `value` when it is present, otherwise the em dash. Keeps callers from
 * reaching for `?? 0`, which the contract treats as a defect.
 */
export function OperatorValue<T>({
  children,
  className,
  reason,
  value,
}: {
  children: (value: NonNullable<T>) => ReactNode
  className?: string
  reason?: string
  value: T
}) {
  if (value === null || value === undefined) {
    return <OperatorMissingValue className={className} reason={reason} />
  }
  return <>{children(value as NonNullable<T>)}</>
}

/**
 * The one staleness badge design in the product. A failed refresh keeps the
 * last successful data on screen and attaches this, carrying the last success
 * time; the failure reason rides along as the tooltip.
 */
export function OperatorStalenessBadge({
  className,
  label,
  reason,
  ...props
}: Omit<ComponentProps<typeof OperatorTypeBadge>, "intent" | "label"> & {
  /** Localized, e.g. `数据为 14:28:03 的上次成功刷新`. */
  label: string
  /** Localized failure reason. */
  reason?: string
}) {
  return (
    <OperatorTypeBadge
      data-slot="staleness-badge"
      intent="degraded"
      label={`◐ ${label}`}
      preserveLabel
      title={reason}
      className={cn("font-normal", className)}
      {...props}
    />
  )
}

/**
 * Data that is real but incomplete: outside the retention cutoff, a truncated
 * payload, coverage that cannot confirm absence. The value still renders — the
 * badge says what is missing from it.
 */
export function OperatorClippedBadge({
  className,
  label,
  reason,
  ...props
}: Omit<ComponentProps<typeof OperatorTypeBadge>, "intent" | "label"> & {
  /** Localized, e.g. `保留期外`, `载荷已截断`, `覆盖不完整`. */
  label: string
  reason?: string
}) {
  return (
    <OperatorTypeBadge
      data-slot="clipped-badge"
      intent="degraded"
      label={`⋯ ${label}`}
      preserveLabel
      title={reason}
      className={cn("font-normal", className)}
      {...props}
    />
  )
}
