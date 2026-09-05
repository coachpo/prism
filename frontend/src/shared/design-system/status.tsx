import type { ComponentProps } from "react"

import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import {
  operatorStatusMarkers,
  operatorStatusTiers,
  type OperatorBadgeIntent,
  type OperatorStatusTier,
} from "./foundation"

/**
 * Background at 10% of the tone, outline at 25%. The four runtime tiers are the
 * only tones that also render a shape marker.
 *
 * 底色用不透明混色而不是半透明叠加：徽章会同时落在 panel 与 inset 上，
 * 半透明底会随父容器变化，把 idle/degraded 的对比度拉到 4.3:1 以下。
 */
const OPERATOR_BADGE_TONES: Record<OperatorBadgeIntent, string> = {
  default: "",
  neutral: "border-border bg-panel text-foreground",
  muted: "border-border bg-inset text-muted-foreground",
  accent:
    "border-primary/25 bg-[color-mix(in_srgb,var(--color-primary)_10%,var(--color-panel))] text-primary",
  danger:
    "border-destructive/25 bg-[color-mix(in_srgb,var(--color-destructive)_10%,var(--color-panel))] text-destructive",
  healthy:
    "border-healthy/25 bg-[color-mix(in_srgb,var(--color-healthy)_10%,var(--color-panel))] text-healthy",
  degraded:
    "border-degraded/25 bg-[color-mix(in_srgb,var(--color-degraded)_10%,var(--color-panel))] text-degraded",
  failing:
    "border-failing/25 bg-[color-mix(in_srgb,var(--color-failing)_10%,var(--color-panel))] text-failing",
  idle: "border-idle/25 bg-[color-mix(in_srgb,var(--color-idle)_10%,var(--color-panel))] text-idle",
}

const STATUS_TIERS = new Set<string>(operatorStatusTiers)

function formatStatusLabel(value: string): string {
  return value
    .replace(/[_-]/g, " ")
    .replace(/\b\w/g, (c) => c.toUpperCase());
}

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
      {preserveLabel ? label : formatStatusLabel(label)}
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

/** CJK 出现在等宽里会撕开：混排时中英文字宽不同源，行内基线一格一跳。 */
const CJK_PATTERN = /[\u3000-\u303f\u3400-\u4dbf\u4e00-\u9fff\uff00-\uffef]/

/**
 * Raw values such as HTTP codes, methods, and percentages.
 * 标签里含中文时不套等宽——契约对此是绝对禁令，而这个徽章也被用来渲染
 * 「每轮 3 次」这类混排摘要。
 */
export function OperatorValueBadge({ className, preserveLabel = true, ...props }: OperatorBadgeProps) {
  const mono = !CJK_PATTERN.test(props.label)
  return (
    <OperatorBadgeBase
      className={cn(mono && "font-mono tabular-nums", className)}
      preserveLabel={preserveLabel}
      {...props}
    />
  )
}

export { OPERATOR_BADGE_TONES }
