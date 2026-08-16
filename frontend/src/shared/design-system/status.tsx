import type { ComponentProps } from "react"

import { Badge } from "@/components/ui/badge"
import { cn, formatLabel } from "@/lib/utils"
import {
  operatorStatusMarkers,
  operatorStatusTiers,
  type OperatorBadgeIntent,
  type OperatorStatusTier,
} from "./foundation"

/**
 * Background at 10% of the tone, outline at 25%. The four runtime tiers are the
 * only tones that also render a shape marker.
 */
const OPERATOR_BADGE_TONES: Record<OperatorBadgeIntent, string> = {
  default: "",
  neutral: "border-border bg-panel text-foreground",
  muted: "border-border bg-inset text-muted-foreground",
  accent: "border-primary/25 bg-primary/10 text-primary",
  danger: "border-destructive/25 bg-destructive/10 text-destructive",
  healthy: "border-healthy/25 bg-healthy/10 text-healthy",
  degraded: "border-degraded/25 bg-degraded/10 text-degraded",
  failing: "border-failing/25 bg-failing/10 text-failing",
  idle: "border-idle/25 bg-idle/10 text-idle",
}

const STATUS_TIERS = new Set<string>(operatorStatusTiers)

function isStatusTier(intent: OperatorBadgeIntent): intent is OperatorStatusTier {
  return STATUS_TIERS.has(intent)
}

export type { OperatorBadgeIntent }

type OperatorBadgeProps = Omit<ComponentProps<typeof Badge>, "children"> & {
  label: string
  intent?: OperatorBadgeIntent
  preserveLabel?: boolean
}

/** 20px tall, 4px radius, 6px horizontal padding, 12px label. */
const BADGE_GEOMETRY = "h-5 shrink-0 gap-1 rounded-[4px] px-1.5 text-xs font-medium"

function OperatorBadgeBase({
  className,
  intent = "default",
  label,
  marker,
  preserveLabel = false,
  ...props
}: OperatorBadgeProps & { marker?: boolean }) {
  return (
    <Badge
      variant="outline"
      className={cn(BADGE_GEOMETRY, OPERATOR_BADGE_TONES[intent], className)}
      {...props}
    >
      {marker && isStatusTier(intent) ? (
        <span aria-hidden="true" className="text-[8px] leading-none">
          {operatorStatusMarkers[intent]}
        </span>
      ) : null}
      {preserveLabel ? label : formatLabel(label)}
    </Badge>
  )
}

/** Runtime state. Always color plus shape plus text. */
export function OperatorStatusBadge(props: OperatorBadgeProps) {
  return <OperatorBadgeBase marker {...props} />
}

/** Categories and classifications. Not runtime state, so no shape marker. */
export function OperatorTypeBadge(props: OperatorBadgeProps) {
  return <OperatorBadgeBase {...props} />
}

/** Raw values such as HTTP codes, methods, and percentages. */
export function OperatorValueBadge({ className, preserveLabel = true, ...props }: OperatorBadgeProps) {
  return (
    <OperatorBadgeBase
      className={cn("font-mono tabular-nums", className)}
      preserveLabel={preserveLabel}
      {...props}
    />
  )
}

export { OPERATOR_BADGE_TONES }
