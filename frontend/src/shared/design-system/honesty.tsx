import type { ComponentProps, ReactNode } from "react"
import { useId } from "react"

import { getStaticMessages } from "@/i18n/staticMessages"
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

/**
 * An absent value. Never `0`, never blank.
 *
 * 破折号本身是 `aria-hidden` 的，没有 reason 时读屏听到的是一个空单元格——
 * 与「这一格没渲染出来」无法区分。所以缺省也要有一句具名兜底文案；
 * 调用点仍应尽量给出具体原因（无流量 / 无样本 / 无可信成本是三件事）。
 */
export function OperatorMissingValue({
  className,
  reason,
  ...props
}: OperatorMissingValueProps) {
  const spoken = reason ?? getStaticMessages().honesty.noValue
  return (
    <span
      data-slot="missing-value"
      title={reason}
      className={cn("font-mono tabular-nums text-muted-foreground", className)}
      {...props}
    >
      <span aria-hidden="true">—</span>
      <span className="sr-only">{spoken}</span>
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
    <BadgeWithReason
      badgeClassName={cn("font-normal", className)}
      dataSlot="staleness-badge"
      label={`◐ ${label}`}
      reason={reason}
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
    <BadgeWithReason
      badgeClassName={cn("font-normal", className)}
      dataSlot="clipped-badge"
      label={`⋯ ${label}`}
      reason={reason}
      {...props}
    />
  )
}

/**
 * 「保留期外」「部分覆盖」这四个字本身不说明缺了什么，唯一能回答
 * 「这个数到底可不可信」的是 reason。只挂在 `title` 上等于对键盘与读屏
 * 用户不存在——所以 reason 同时进 sr-only 节点并用 `aria-describedby` 关联。
 */
function BadgeWithReason({
  badgeClassName,
  dataSlot,
  label,
  reason,
  ...props
}: Omit<ComponentProps<typeof OperatorTypeBadge>, "intent" | "label"> & {
  badgeClassName?: string
  dataSlot: string
  label: string
  reason?: string
}) {
  const reasonId = useId()
  if (!reason) {
    return (
      <OperatorTypeBadge
        data-slot={dataSlot}
        intent="degraded"
        label={label}
        preserveLabel
        className={badgeClassName}
        {...props}
      />
    )
  }
  return (
    <span className="inline-flex items-center">
      <OperatorTypeBadge
        data-slot={dataSlot}
        intent="degraded"
        label={label}
        preserveLabel
        title={reason}
        aria-describedby={reasonId}
        className={badgeClassName}
        {...props}
      />
      <span id={reasonId} className="sr-only">
        {reason}
      </span>
    </span>
  )
}
