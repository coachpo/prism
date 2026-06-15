import type { ReactNode } from "react"

import { Card, CardContent } from "@/components/ui/card"
import { cn } from "@/lib/utils"

export type OperatorMetricCardProps = {
  label: ReactNode
  value: ReactNode
  detail?: ReactNode
  icon?: ReactNode
  trend?: { value: string; positive?: boolean }
  className?: string
  onClick?: () => void
}

export function OperatorMetricCard({
  className,
  detail,
  icon,
  label,
  onClick,
  trend,
  value,
}: OperatorMetricCardProps) {
  return (
    <Card
      data-slot="metric-card"
      className={cn("operator-section-surface overflow-hidden transition-colors duration-150", onClick && "cursor-pointer hover:border-primary/30 hover:bg-surface-container-low", className)}
      onClick={onClick}
    >
      <CardContent className="overflow-hidden p-[var(--density-metric-pad)]">
        <div className="flex min-w-0 items-start justify-between gap-3 overflow-hidden">
          <div className="flex min-w-0 flex-1 flex-col gap-2 overflow-hidden">
            <div
              data-slot="metric-label"
              className="flex min-w-0 flex-wrap items-center gap-2 overflow-hidden text-sm font-medium text-muted-foreground [&_[data-slot=badge]]:max-w-full [&_[data-slot=badge]]:truncate"
            >
              {label}
            </div>
            <div className="flex min-w-0 flex-wrap items-baseline gap-2">
              <span
                data-slot="metric-value"
                className="min-w-0 break-words text-2xl font-bold leading-tight tracking-tight"
              >
                {value}
              </span>
              {trend ? (
                <span
                  className={cn(
                    "max-w-full break-words text-xs font-medium",
                    trend.positive ? "text-success" : "text-destructive",
                  )}
                >
                  {trend.value}
                </span>
              ) : null}
            </div>
            {detail ? (
              <div data-slot="metric-detail" className="text-xs text-muted-foreground">
                {detail}
              </div>
            ) : null}
          </div>
          {icon ? (
            <div
              data-slot="icon"
              className="flex size-[var(--density-control-h)] shrink-0 items-center justify-center rounded-lg bg-primary-container text-on-primary-container"
            >
              {icon}
            </div>
          ) : null}
        </div>
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
      className={cn("rounded-md border border-outline-variant bg-surface-container-low p-3 transition-colors duration-300", className)}
    >
      <p data-slot="metric-label" className="text-xs text-muted-foreground">
        {label}
      </p>
      <div data-slot="metric-value" className={cn("mt-1 text-lg font-semibold tabular-nums", valueClassName)}>
        {value}
      </div>
      {detail ? (
        <div data-slot="metric-detail" className="mt-1 text-xs text-muted-foreground">
          {detail}
        </div>
      ) : null}
    </div>
  )
}
