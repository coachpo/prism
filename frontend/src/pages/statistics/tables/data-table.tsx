import { useMemo, useState } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import {
  DataTableColumnHeader,
  type DataTableSortDirection,
} from "./data-table-column-header";

export interface DataTableColumn<TItem> {
  cell: (item: TItem) => React.ReactNode;
  className?: string;
  header: string;
  headerClassName?: string;
  id: string;
  sortValue?: (item: TItem) => number | string | null;
}

interface DataTableProps<TItem> {
  columns: DataTableColumn<TItem>[];
  emptyState: React.ReactNode;
  getRowId: (item: TItem) => string;
  initialSort?: {
    columnId: string;
    direction: DataTableSortDirection;
  };
  items: TItem[];
  rowClassName?: (item: TItem) => string | undefined;
  testId: string;
}

function compareValues(
  left: number | string | null,
  right: number | string | null,
  collator: Intl.Collator,
) {
  if (left === null && right === null) {
    return 0;
  }

  if (left === null) {
    return 1;
  }

  if (right === null) {
    return -1;
  }

  if (typeof left === "number" && typeof right === "number") {
    return left - right;
  }

  return collator.compare(String(left), String(right));
}

export function DataTable<TItem>({
  columns,
  emptyState,
  getRowId,
  initialSort,
  items,
  rowClassName,
  testId,
}: DataTableProps<TItem>) {
  const { locale } = useLocale();
  const [sort, setSort] = useState(initialSort ?? null);

  const collator = useMemo(
    () => new Intl.Collator(locale, { numeric: true, sensitivity: "base" }),
    [locale],
  );

  const sortedItems = useMemo(() => {
    if (!sort) {
      return items;
    }

    const activeColumn = columns.find((column) => column.id === sort.columnId);
    if (!activeColumn?.sortValue) {
      return items;
    }

    const getSortValue = activeColumn.sortValue!;

    const nextItems = [...items].sort((left, right) =>
      compareValues(getSortValue(left), getSortValue(right), collator),
    );

    if (sort.direction === "desc") {
      nextItems.reverse();
    }

    return nextItems;
  }, [collator, columns, items, sort]);

  const toggleSort = (column: DataTableColumn<TItem>) => {
    if (!column.sortValue) {
      return;
    }

    setSort((current) => {
      if (!current || current.columnId !== column.id) {
        return { columnId: column.id, direction: "asc" };
      }

      return {
        columnId: column.id,
        direction: current.direction === "asc" ? "desc" : "asc",
      };
    });
  };

  return (
    <div
      className="overflow-hidden rounded-xl border border-border/60 bg-background/80"
      data-testid={testId}
    >
      {items.length === 0 ? (
        <div className="p-2">{emptyState}</div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              {columns.map((column) => {
                const isSorted = sort?.columnId === column.id;
                return (
                  <TableHead
                    aria-sort={
                      isSorted
                        ? sort?.direction === "asc"
                          ? "ascending"
                          : "descending"
                        : column.sortValue
                          ? "none"
                          : undefined
                    }
                    className={cn(column.headerClassName)}
                    key={column.id}
                  >
                    <DataTableColumnHeader
                      active={Boolean(isSorted)}
                      className={column.headerClassName}
                      direction={sort?.direction ?? "asc"}
                      label={column.header}
                      onToggle={column.sortValue ? () => toggleSort(column) : undefined}
                    />
                  </TableHead>
                );
              })}
            </TableRow>
          </TableHeader>

          <TableBody>
            {sortedItems.map((item) => (
              <TableRow className={rowClassName?.(item)} key={getRowId(item)}>
                {columns.map((column) => (
                  <TableCell className={cn(column.className)} key={column.id}>
                    {column.cell(item)}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
