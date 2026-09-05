/* eslint-disable react-refresh/only-export-components */
import type { ReactNode } from "react"
import { ChevronDown, ChevronUp, ChevronsUpDown } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
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
    <TableHead aria-sort={ariaSort} className={cn(align === "right" && "text-right", className)}>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        data-active={active || undefined}
        className={cn(
          "h-6 gap-1 px-1 text-xs font-medium normal-case text-muted-foreground hover:text-foreground",
          active && "text-foreground",
          align === "right" && "ml-auto",
        )}
        onClick={() => onSort(sortKey)}
      >
        {children}
        <DirectionIcon aria-hidden="true" className={cn("size-3", !active && "opacity-50")} />
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
 */
export const operationalRowActionsClassName =
  "flex items-center justify-end gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/row:opacity-100 [@media(hover:none)]:opacity-100"

type OperationalTableSkeletonRowsProps = {
  columns: number
  rows?: number
}

export function OperationalTableSkeletonRows({ columns, rows = 5 }: OperationalTableSkeletonRowsProps) {
  return Array.from({ length: rows }, (_, rowIndex) => (
    <TableRow key={`operational-table-skeleton-${rowIndex}`}>
      {Array.from({ length: columns }, (_, columnIndex) => (
        <TableCell key={`operational-table-skeleton-${rowIndex}-${columnIndex}`}>
          <Skeleton className="h-4 w-full" />
        </TableCell>
      ))}
    </TableRow>
  ))
}
