/* eslint-disable react-refresh/only-export-components */
import type { ReactNode } from "react"
import { ArrowUpDown, ChevronLeft, ChevronRight } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { TableCell, TableHead, TableRow } from "@/components/ui/table"
import { cn } from "@/lib/utils"

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

  return (
    <TableHead aria-sort={ariaSort} className={cn(align === "right" && "text-right", className)}>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className={cn(
          "h-7 gap-1 px-1 text-xs font-medium uppercase tracking-[0.14em] text-muted-foreground hover:text-foreground",
          align === "right" && "ml-auto",
        )}
        onClick={() => onSort(sortKey)}
      >
        {children}
        <ArrowUpDown data-icon="inline-end" />
      </Button>
    </TableHead>
  )
}

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
  previousLabel: string
  resultsLabel: (start: string, end: string, total: string) => string
  startIndex: number
  totalRows: number
  zeroLabel: string
}

export function OperationalTablePagination({
  className,
  currentPageIndex,
  endIndex,
  formatNumber,
  hasNextPage,
  hasPreviousPage,
  nextLabel,
  onNextPage,
  onPreviousPage,
  previousLabel,
  resultsLabel,
  startIndex,
  totalRows,
  zeroLabel,
}: OperationalTablePaginationProps) {
  const pageStart = totalRows > 0 ? startIndex + 1 : 0

  return (
    <div className={cn("flex flex-col gap-3 border-t border-border/70 bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between", className)}>
      <span className="text-xs text-muted-foreground">
        {totalRows > 0
          ? resultsLabel(formatNumber(pageStart), formatNumber(endIndex), formatNumber(totalRows))
          : zeroLabel}
      </span>
      <div className="flex items-center gap-1" aria-label={`Page ${formatNumber(currentPageIndex + 1)}`}>
        <Button type="button" variant="outline" size="icon" className="size-8 rounded-full" disabled={!hasPreviousPage} aria-label={previousLabel} onClick={onPreviousPage}>
          <ChevronLeft />
        </Button>
        <Button type="button" variant="outline" size="icon" className="size-8 rounded-full" disabled={!hasNextPage} aria-label={nextLabel} onClick={onNextPage}>
          <ChevronRight />
        </Button>
      </div>
    </div>
  )
}
