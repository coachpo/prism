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
