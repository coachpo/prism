/* eslint-disable react-refresh/only-export-components */
import type { ReactNode } from "react"
import { ChevronDown, ChevronLeft, ChevronRight, ChevronUp, ChevronsUpDown } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { TableCell, TableHead, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"
import type { OperatorStatusTier } from "@/shared/design-system"

export type OperationalSortDirection = "asc" | "desc"

export type OperationalSortState<TColumn extends string = string> = {
  column: TColumn
  direction: OperationalSortDirection
}

export type OperationalSortValue = boolean | number | string | null | undefined

export function compareOperationalValues(
  left: OperationalSortValue,
  right: OperationalSortValue,
  collator: Intl.Collator,
) {
  if (left === right) return 0
  if (left === null || left === undefined) return 1
  if (right === null || right === undefined) return -1

  const leftValue = typeof left === "boolean" ? Number(left) : left
  const rightValue = typeof right === "boolean" ? Number(right) : right

  if (typeof leftValue === "number" && typeof rightValue === "number") {
    return leftValue - rightValue
  }

  return collator.compare(String(leftValue), String(rightValue))
}

export function getNextOperationalSort<TColumn extends string>(
  current: OperationalSortState<TColumn>,
  column: TColumn,
): OperationalSortState<TColumn> {
  if (current.column !== column) return { column, direction: "asc" }
  return { column, direction: current.direction === "asc" ? "desc" : "asc" }
}

export function sortOperationalRows<TItem, TColumn extends string>(
  rows: readonly TItem[],
  sort: OperationalSortState<TColumn>,
  getValue: (row: TItem, column: TColumn) => OperationalSortValue,
  locale: string,
) {
  const collator = new Intl.Collator(locale, { numeric: true, sensitivity: "base" })
  const sorted = [...rows].sort((left, right) =>
    compareOperationalValues(getValue(left, sort.column), getValue(right, sort.column), collator),
  )
  if (sort.direction === "desc") sorted.reverse()
  return sorted
}

export function paginateOperationalRows<TItem>(
  rows: readonly TItem[],
  pageIndex: number,
  pageSize: number,
) {
  const totalPages = Math.max(1, Math.ceil(rows.length / pageSize))
  const currentPageIndex = Math.min(Math.max(pageIndex, 0), totalPages - 1)
  const startIndex = currentPageIndex * pageSize
  const endIndex = Math.min(startIndex + pageSize, rows.length)
  return {
    currentPageIndex,
    endIndex,
    hasNextPage: endIndex < rows.length,
    hasPreviousPage: currentPageIndex > 0,
    pageRows: rows.slice(startIndex, endIndex),
    startIndex,
    totalPages,
  }
}

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

/** Row actions stay hidden until the row is hovered or something inside is focused. */
export const operationalRowActionsClassName =
  "flex items-center justify-end gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/row:opacity-100"

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

type OperationalTablePaginationProps = {
  className?: string
  currentPageIndex: number
  endIndex: number
  formatNumber: (value: number) => string
  hasNextPage: boolean
  hasPreviousPage: boolean
  nextLabel: string
  onNextPage: () => void
  onPreviousPage: () => void
  /** Page-size selector. Omit when the page size is not adjustable. */
  pageSize?: {
    ariaLabel: string
    onChange: (value: number) => void
    options: readonly number[]
    value: number
  }
  previousLabel: string
  resultsLabel: (start: string, end: string, total: string) => string
  startIndex: number
  totalRows: number
  /** `共 N 条`, already localized. Shown on the left beside the range. */
  totalLabel?: (total: string) => string
  zeroLabel: string
  /** Jump straight to a page. Without it only prev/next are rendered. */
  onGoToPage?: (pageIndex: number) => void
  /** Localized `第 N 页`, for the page buttons' accessible names. */
  pageLabel?: (page: string) => string
  /** Total pages, when the caller knows it. Derived otherwise. */
  pageCount?: number
}

/**
 * First page, last page, and a window around the current one; `null` marks an
 * elided run. Keeps the control a fixed width however many pages there are.
 */
function windowedPages(currentPageIndex: number, totalPages: number): Array<number | null> {
  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, index) => index)
  }
  const pages = new Set<number>([0, totalPages - 1])
  for (let offset = -1; offset <= 1; offset += 1) {
    const page = currentPageIndex + offset
    if (page >= 0 && page < totalPages) pages.add(page)
  }
  const ordered = [...pages].sort((left, right) => left - right)
  const withGaps: Array<number | null> = []
  ordered.forEach((page, index) => {
    if (index > 0 && page - ordered[index - 1] > 1) withGaps.push(null)
    withGaps.push(page)
  })
  return withGaps
}

/** `共 N 条` on the left; page controls and page size on the right. */
export function OperationalTablePagination({
  className,
  currentPageIndex,
  endIndex,
  formatNumber,
  hasNextPage,
  hasPreviousPage,
  nextLabel,
  onGoToPage,
  onNextPage,
  onPreviousPage,
  pageCount,
  pageLabel,
  pageSize,
  previousLabel,
  resultsLabel,
  startIndex,
  totalLabel,
  totalRows,
  zeroLabel,
}: OperationalTablePaginationProps) {
  const pageStart = totalRows > 0 ? startIndex + 1 : 0
  const totalPages = pageCount ?? (pageSize ? Math.max(1, Math.ceil(totalRows / pageSize.value)) : 1)
  const pageNumbers = onGoToPage && pageLabel ? windowedPages(currentPageIndex, totalPages) : []

  return (
    <div
      className={cn(
        "flex flex-col gap-3 border-t border-border bg-inset px-[var(--density-card-pad-x)] py-2 sm:flex-row sm:items-center sm:justify-between",
        className,
      )}
    >
      <span className="text-xs text-muted-foreground">
        {totalRows > 0
          ? totalLabel
            ? `${totalLabel(formatNumber(totalRows))} · ${resultsLabel(formatNumber(pageStart), formatNumber(endIndex), formatNumber(totalRows))}`
            : resultsLabel(formatNumber(pageStart), formatNumber(endIndex), formatNumber(totalRows))
          : zeroLabel}
      </span>
      <div className="flex items-center gap-2">
        {pageSize ? (
          <Select
            value={String(pageSize.value)}
            onValueChange={(value) => pageSize.onChange(Number(value))}
          >
            <SelectTrigger size="sm" aria-label={pageSize.ariaLabel} className="h-7 w-20 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {pageSize.options.map((option) => (
                  <SelectItem key={option} value={String(option)}>
                    {formatNumber(option)}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        ) : null}
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="size-7 rounded-md"
            disabled={!hasPreviousPage}
            aria-label={previousLabel}
            onClick={onPreviousPage}
          >
            <ChevronLeft />
          </Button>
          {/* Page numbers, not just prev/next: on a long list the operator
              needs to know where they are and jump, not step. */}
          {pageNumbers.map((page, index) =>
            page === null ? (
              <span key={`gap-${index}`} aria-hidden="true" className="px-1 text-xs text-text-disabled">
                …
              </span>
            ) : (
              <Button
                key={page}
                type="button"
                variant={page === currentPageIndex ? "secondary" : "ghost"}
                size="icon"
                className="size-7 rounded-md font-mono text-xs tabular-nums"
                aria-current={page === currentPageIndex ? "page" : undefined}
                aria-label={pageLabel?.(formatNumber(page + 1))}
                onClick={() => onGoToPage?.(page)}
              >
                {formatNumber(page + 1)}
              </Button>
            ),
          )}
          <Button
            type="button"
            variant="outline"
            size="icon"
            className="size-7 rounded-md"
            disabled={!hasNextPage}
            aria-label={nextLabel}
            onClick={onNextPage}
          >
            <ChevronRight />
          </Button>
        </div>
      </div>
    </div>
  )
}
