/* eslint-disable react-refresh/only-export-components */
import type { ReactNode } from "react"
import { ChevronDown, ChevronUp, ChevronsUpDown } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { getStaticMessages } from "@/i18n/staticMessages"
import { TableCell, TableHead, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"
import type { OperatorStatusTier } from "@/shared/design-system"
import type { OperationalSortState } from "./operationalTableState"

type SortableTableHeadProps<TColumn extends string> = {
  align?: "left" | "right"
  children: ReactNode
  className?: string
  onSort: (column: TColumn) => void
  sort: OperationalSortState<TColumn>
  sortKey: TColumn
}

/**
 * The active column shows its real direction; inactive columns show a neutral
 * affordance, so the highlighted column is always the one actually in effect.
 */
export function SortableTableHead<TColumn extends string>({
  align = "left",
  children,
  className,
  onSort,
  sort,
  sortKey,
}: SortableTableHeadProps<TColumn>) {
  const active = sort.column === sortKey
  const ariaSort = active ? (sort.direction === "asc" ? "ascending" : "descending") : "none"
  const DirectionIcon = !active ? ChevronsUpDown : sort.direction === "asc" ? ChevronUp : ChevronDown

  return (
    // 排序命中区是整个表头单元格：24px 高、只占文字宽的按钮既低于 28×28 下限，
    // 也让「点哪里能排序」变成要试出来的事。
    <TableHead
      aria-sort={ariaSort}
      className={cn("p-0", align === "right" && "text-right", className)}
    >
      <Button
        type="button"
        variant="ghost"
        size="sm"
        data-active={active || undefined}
        className={cn(
          "h-[var(--density-table-head-h)] min-h-7 w-full gap-1 rounded-none px-[var(--density-table-cell-px)] text-xs font-medium normal-case text-muted-foreground hover:text-foreground",
          active && "text-foreground",
          align === "right" ? "justify-end" : "justify-start",
        )}
        onClick={() => onSort(sortKey)}
      >
        {children}
        {/* 方向字形永远是实色：淡化后它落进 text-disabled 那一档，
            而那一档按契约不承载信息。生效的那一列用主色标出来。 */}
        <DirectionIcon
          aria-hidden="true"
          className={cn("size-3 shrink-0", active && "text-primary")}
        />
      </Button>
    </TableHead>
  )
}

const STATUS_STRIPE_CLASS: Record<Exclude<OperatorStatusTier, "idle">, string> = {
  healthy: "[&>td:first-child]:before:bg-healthy",
  degraded: "[&>td:first-child]:before:bg-degraded",
  failing: "[&>td:first-child]:before:bg-failing",
}

/**
 * A 2px status bar on the row's left edge, for runtime state only. Idle rows
 * get no stripe, and a non-runtime attribute must never be encoded here.
 * The bar hangs off the row's first cell, never off the `tr`: a pseudo-element
 * on a table-row box gets wrapped in an anonymous table cell, which pushes
 * every real cell one column to the right.
 */
export function operationalRowStripe(tier: OperatorStatusTier | null | undefined): string {
  if (!tier || tier === "idle") return ""
  return cn(
    "[&>td:first-child]:relative [&>td:first-child]:before:absolute [&>td:first-child]:before:inset-y-0 [&>td:first-child]:before:left-0 [&>td:first-child]:before:w-0.5 [&>td:first-child]:before:content-['']",
    STATUS_STRIPE_CLASS[tier],
  )
}

/**
 * 行操作在可 hover 的指针下淡出，其它情况常显。
 * 触屏没有 hover：点一下行不会让操作出现，操作者会以为这一行根本没有操作。
 * 淡出规则见 index.css 的 `.operator-row-actions`：最后一个子节点（溢出菜单触发器）
 * 常驻淡显，整组一起淡没会让一整屏的行看起来根本没有操作入口。
 * 因此调用点要把溢出菜单触发器放在这一组的最后。
 */
export const operationalRowActionsClassName =
  "operator-row-actions flex items-center justify-end gap-0.5"

type OperationalTableSkeletonRowsProps = {
  columns: number
  rows?: number
  /** 播报给读屏的等待文案。默认是共享的「正在加载数据…」。 */
  label?: string
}

/**
 * 骨架行自带活动区：不播报的话，读屏用户在等待期听不到任何东西，
 * 「还在读」与「没有数据」在他那里完全一样。
 */
export function OperationalTableSkeletonRows({
  columns,
  rows = 5,
  label,
}: OperationalTableSkeletonRowsProps) {
  const announcement = label ?? getStaticMessages().operationalTable.loadingFirstPage
  return (
    <>
      <tr>
        <td colSpan={columns} className="p-0">
          <span role="status" aria-live="polite" className="sr-only">
            {announcement}
          </span>
        </td>
      </tr>
      {Array.from({ length: rows }, (_, rowIndex) => (
        <TableRow key={`operational-table-skeleton-${rowIndex}`} aria-hidden="true">
          {Array.from({ length: columns }, (_, columnIndex) => (
            <TableCell key={`operational-table-skeleton-${rowIndex}-${columnIndex}`}>
              <Skeleton className="h-4 w-full" />
            </TableCell>
          ))}
        </TableRow>
      ))}
    </>
  )
}
