import type { ComponentProps } from "react"

import { Badge } from "@/components/ui/badge"
import { cn, formatLabel } from "@/lib/utils"
import type { OperatorStatusIntent } from "./foundation"

const OPERATOR_BADGE_TONES: Record<OperatorStatusIntent, string> = {
  default: "",
  neutral: "border-outline-variant bg-surface text-foreground",
  muted: "border-outline-variant bg-surface-container-low text-muted-foreground",
  accent: "border-primary/25 bg-primary/10 text-primary",
  info: "border-info/25 bg-info/10 text-info",
  success: "border-success/25 bg-success/10 text-success",
  warning: "border-warning/30 bg-warning/10 text-warning",
  danger: "border-destructive/30 bg-destructive/10 text-destructive",
  healthy: "border-healthy/25 bg-healthy/10 text-healthy",
  downgrade: "border-downgrade/30 bg-downgrade/10 text-downgrade",
  unhealthy: "border-unhealthy/30 bg-unhealthy/10 text-unhealthy",
}

export type OperatorBadgeIntent = OperatorStatusIntent | "blue"

type OperatorBadgeProps = Omit<ComponentProps<typeof Badge>, "children"> & {
  label: string
  intent?: OperatorBadgeIntent
  preserveLabel?: boolean
}

function resolveBadgeIntent(intent: OperatorBadgeIntent) {
  return intent === "blue" ? "info" : intent
}

function OperatorBadgeBase({
  className,
  intent = "default",
  label,
  preserveLabel = false,
  ...props
}: OperatorBadgeProps) {
  const resolvedIntent = resolveBadgeIntent(intent)

  return (
    <Badge
      variant="outline"
      className={cn("shrink-0 text-[10px]", OPERATOR_BADGE_TONES[resolvedIntent], className)}
      {...props}
    >
      {preserveLabel ? label : formatLabel(label)}
    </Badge>
  )
}

export function OperatorStatusBadge(props: OperatorBadgeProps) {
  return <OperatorBadgeBase {...props} />
}

export function OperatorTypeBadge(props: OperatorBadgeProps) {
  return <OperatorBadgeBase {...props} />
}

export function OperatorValueBadge({ className, preserveLabel = true, ...props }: OperatorBadgeProps) {
  return (
    <OperatorBadgeBase
      className={cn("font-mono", className)}
      preserveLabel={preserveLabel}
      {...props}
    />
  )
}

export { OPERATOR_BADGE_TONES }
