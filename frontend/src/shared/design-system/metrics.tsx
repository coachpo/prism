import type { ReactNode } from "react"

import { Card, CardContent } from "@/components/ui/card"
import { cn } from "@/lib/utils"

/**
 * Direction is what the number did; tone is whether that is good here. They
 * are separate on purpose: spend rising is `degraded`, success rate rising is
 * `healthy`, and the arrow carries the direction so color is never alone.
 */
export type OperatorKpiDelta = {
  direction: "up" | "down"
  label: string
  tone: "healthy" | "degraded" | "failing" | "neutral"
}

const DELTA_TONE_CLASS: Record<OperatorKpiDelta["tone"], string> = {
  healthy: "text-healthy",
  degraded: "text-degraded",
  failing: "text-failing",
  neutral: "text-muted-foreground",
}

export type OperatorKpiCardProps = {
  className?: string
  /** Basis, comparison, or scope note under the value. */
  detail?: ReactNode
  /** Staleness, clipped, or coverage badges belonging to this number. */
  badges?: ReactNode
  delta?: OperatorKpiDelta
  label: ReactNode
  onClick?: () => void
  /**
   * The number itself. Pass `OperatorMissingValue` when it is absent and an
   * error surface when the read failed — never a zero standing in for either.
   */
  value: ReactNode
}

export function OperatorKpiCard({
  badges,
  className,
  delta,
  detail,
  label,
  onClick,
  value,
}: OperatorKpiCardProps) {
  const interactive = Boolean(onClick)

  return (
    <Card
      data-slot="kpi-card"
      className={cn(
        "operator-section-surface min-w-0 gap-0 rounded-lg py-0 transition-colors",
        interactive && "cursor-pointer hover:border-primary/40 hover:bg-primary-soft/25",
        className,
      )}
      onClick={onClick}
      {...(interactive
        ? {
            role: "button" as const,
            tabIndex: 0,
            onKeyDown: (event: React.KeyboardEvent) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault()
                onClick?.()
              }
            },
          }
        : {})}
    >
      <CardContent className="flex min-w-0 flex-col gap-1 p-[var(--density-metric-pad)]">
        <div
          data-slot="metric-label"
          className="flex min-w-0 items-center gap-1.5 text-[11px] font-medium tracking-[0.04em] text-muted-foreground"
        >
          {label}
        </div>
        <div className="flex min-w-0 flex-wrap items-baseline gap-2">
          <span
            data-slot="metric-value"
            className="min-w-0 font-mono text-[1.625rem] font-semibold leading-[1.875rem] tabular-nums"
          >
            {value}
          </span>
          {delta ? (
            <span
              data-slot="metric-delta"
              className={cn("inline-flex items-center gap-0.5 text-xs font-medium", DELTA_TONE_CLASS[delta.tone])}
            >
              <span aria-hidden="true">{delta.direction === "up" ? "▲" : "▼"}</span>
              {delta.label}
            </span>
          ) : null}
        </div>
        {detail ? (
          <div data-slot="metric-detail" className="text-xs text-muted-foreground">
            {detail}
          </div>
        ) : null}
        {badges ? <div className="flex flex-wrap items-center gap-1 pt-0.5">{badges}</div> : null}
      </CardContent>
    </Card>
  )
}

export type OperatorMetricTileProps = {
  className?: string
  detail?: ReactNode
  label: ReactNode
  value: ReactNode
  valueClassName?: string
}

/** The inline variant, for dense metadata strips inside an existing card. */
export function OperatorMetricTile({
  className,
  detail,
  label,
  value,
  valueClassName,
}: OperatorMetricTileProps) {
  return (
    <div
      data-slot="compact-metric-tile"
      className={cn("rounded-md border border-border bg-inset p-2.5", className)}
    >
      <p data-slot="metric-label" className="text-[11px] font-medium tracking-[0.04em] text-muted-foreground">
        {label}
      </p>
      <div
        data-slot="metric-value"
        className={cn("mt-0.5 font-mono text-base font-semibold tabular-nums", valueClassName)}
      >
        {value}
      </div>
      {detail ? (
        <div data-slot="metric-detail" className="mt-0.5 text-xs text-muted-foreground">
          {detail}
        </div>
      ) : null}
    </div>
  )
}
