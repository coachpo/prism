// Requests SPEC §4 default columns: pricing state is a first-class column;
// the sheet is a wide-format (no fixed 640px) detail surface.
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  ChevronLeft,
  ChevronRight,
  FileSearch,
} from "lucide-react";
import type { ReactNode } from "react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { RequestLogListItem } from "@/lib/types";
import { OperatorClippedBadge, OperatorEmptyState } from "@/shared/design-system";
import { PaginationLiveStatus } from "@/shared/table/paginationControls";
import {
  getColumns,
  ROW_HEIGHT,
  scopedStatus,
  type ColumnDef,
} from "./columns";
import { PAGE_SIZE_OPTIONS } from "./queryParams";

interface RequestLogsTableProps {
  items: RequestLogListItem[];
  total: number;
  totalIsExact: boolean;
  hasMoreRows: boolean;
  loading: boolean;
  /** True while a replace read has withdrawn the old page for skeletons. */
  replacing: boolean;
  limit: number;
  offset: number;
  activeRequestId: string | null;
  onSelectRequest: (id: string) => void;
  onSetLimit: (limit: number) => void;
  onNextPage: () => void;
  onPreviousPage: () => void;
  formatTimestamp: (iso: string) => string;
  visibleColumns: string[];
  sortBy: string;
  sortOrder: "asc" | "desc";
  onSortChange: (key: string) => void;
  /** True only when the backend's coverage says this window was clipped. */
  retentionClipped: boolean;
  /** Next step for a filtered-to-nothing result; omitted when there is none. */
  emptyAction?: ReactNode;
}

interface ResolvedColumn extends ColumnDef {
  resolvedWidth: number;
}

const OVERSCAN = 10;

// Column key -> backend sort_by key (Requests SPEC §9.3). Chain view
// restricts sorting to created_at server-side; the UI only offers the
// attempt-view sortable keys.
const SORTABLE_COLUMN_KEYS: Record<string, string> = {
  created_at: "created_at",
  status_code: "display_status",
  ttft_ms: "ttft_ms",
  total_tokens: "total_tokens",
  total_cost: "total_cost_user_currency_micros",
};
const SKELETON_ROW_KEYS = [
  "request-log-skeleton-1",
  "request-log-skeleton-2",
  "request-log-skeleton-3",
  "request-log-skeleton-4",
  "request-log-skeleton-5",
  "request-log-skeleton-6",
  "request-log-skeleton-7",
  "request-log-skeleton-8",
];

function getRowTone(row: RequestLogListItem) {
  // A 2px status bar on the left edge, not a tinted row. Tinting every 4xx and
  // 5xx row turns a long list into a wall of color and makes the genuinely
  // unusual rows harder, not easier, to find.
  const status = scopedStatus(row);
  if (status !== null && status >= 500) {
    return {
      row: "border-border bg-panel hover:bg-primary-soft/20",
      stripe: "before:bg-failing",
    };
  }
  if (status !== null && status >= 400) {
    return {
      row: "border-border bg-panel hover:bg-primary-soft/20",
      stripe: "before:bg-degraded",
    };
  }
  return { row: "border-border bg-panel hover:bg-primary-soft/20", stripe: "" };
}

function resolveColumns(
  columns: ColumnDef[],
  containerWidth: number,
): ResolvedColumn[] {
  const baseWidth = columns.reduce((sum, col) => sum + col.width, 0);
  const growWeight = columns.reduce((sum, col) => sum + (col.grow ?? 0), 0);
  const extraWidth = Math.max(0, containerWidth - baseWidth);

  return columns.map((col) => {
    const resolvedWidth = Math.round(
      col.width +
        (growWeight > 0 ? extraWidth * ((col.grow ?? 0) / growWeight) : 0),
    );
    return {
      ...col,
      resolvedWidth,
    };
  });
}

export function RequestLogsTable({
  items,
  total,
  totalIsExact,
  hasMoreRows,
  loading,
  replacing,
  limit,
  offset,
  activeRequestId,
  onSelectRequest,
  onSetLimit,
  onNextPage,
  onPreviousPage,
  formatTimestamp,
  visibleColumns,
  sortBy,
  sortOrder,
  onSortChange,
  retentionClipped,
  emptyAction,
}: RequestLogsTableProps) {
  const { formatNumber, messages } = useLocale();
  const columns = useMemo(() => getColumns(), []);
  const containerRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [containerHeight, setContainerHeight] = useState(500);
  const [containerWidth, setContainerWidth] = useState(0);

  useEffect(() => {
    const element = containerRef.current;
    if (!element) return undefined;

    const syncSize = () => {
      setContainerHeight(element.clientHeight);
      setContainerWidth(element.clientWidth);
    };

    syncSize();
    const observer = new ResizeObserver(syncSize);
    observer.observe(element);

    return () => observer.disconnect();
  }, []);

  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    setScrollTop(el.scrollTop);
  }, []);

  const visibleColumnDefs = useMemo(
    () => columns.filter((column) => visibleColumns.includes(column.key)),
    [columns, visibleColumns],
  );

  const resolvedColumns = useMemo(
    () => resolveColumns(visibleColumnDefs, Math.max(containerWidth - 2, 0)),
    [visibleColumnDefs, containerWidth],
  );

  const totalWidth = useMemo(
    () => resolvedColumns.reduce((sum, col) => sum + col.resolvedWidth, 0),
    [resolvedColumns],
  );

  const totalHeight = items.length * ROW_HEIGHT;
  const startIndex = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
  const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + OVERSCAN * 2;
  const endIndex = Math.min(items.length, startIndex + visibleCount);

  const pageStart = total > 0 ? offset + 1 : 0;
  const pageEnd = total > 0 ? Math.min(offset + limit, total) : 0;
  const hasPrev = offset > 0;
  const hasNext = hasMoreRows;
  // A replace read withdraws the old page: its rows belong to the previous
  // URL and must not masquerade as the target page while it loads.
  const showPendingRows = replacing;

  return (
    <div
      className="operator-table-shell flex min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-panel"
      data-testid="request-logs-table"
    >
      <PaginationLiveStatus
        message={
          showPendingRows ? messages.operationalTable.loadingTargetPage : null
        }
      />
      {/* Adaptive viewport: the table fills the shell instead of a fixed 640px. */}
      <div
        ref={containerRef}
        className="min-h-0 flex-1 overflow-auto scrollbar-thin"
        onScroll={handleScroll}
        aria-busy={loading || undefined}
      >
        <div className="w-full" style={{ minWidth: totalWidth }}>
          <div className="sticky top-0 z-10 flex border-b border-border bg-inset">
            {resolvedColumns.map((col) => {
              const sortKey = SORTABLE_COLUMN_KEYS[col.key];
              const isSorted = sortKey !== undefined && sortBy === sortKey;
              return (
                <div
                  key={col.key}
                  data-testid={col.headerTestId}
                  className={cn(
                    "shrink-0 px-3 py-2.5 text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground",
                    col.align === "right" && "text-right",
                    col.align === "center" && "text-center",
                  )}
                  style={{ width: col.resolvedWidth }}
                >
                  {sortKey ? (
                    <button
                      type="button"
                      aria-label={`${col.label} 排序`}
                      aria-sort={
                        isSorted
                          ? sortOrder === "asc"
                            ? "ascending"
                            : "descending"
                          : "none"
                      }
                      className={cn(
                        "inline-flex items-center gap-1 hover:text-foreground",
                        isSorted && "text-foreground",
                      )}
                      onClick={() => onSortChange(sortKey)}
                    >
                      {col.label}
                      {isSorted ? (
                        sortOrder === "asc" ? (
                          <ArrowUp className="size-3" />
                        ) : (
                          <ArrowDown className="size-3" />
                        )
                      ) : (
                        <ArrowUpDown className="size-3 opacity-40" />
                      )}
                    </button>
                  ) : (
                    col.label
                  )}
                </div>
              );
            })}
          </div>

          {loading && (items.length === 0 || replacing) ? (
            <div className="flex flex-col gap-0">
              {SKELETON_ROW_KEYS.map((key) => (
                <div
                  key={key}
                  className="flex border-b border-border bg-panel"
                  style={{ height: ROW_HEIGHT }}
                >
                  {resolvedColumns.map((col) => (
                    <div
                      key={col.key}
                      className="shrink-0 px-3 py-3"
                      style={{ width: col.resolvedWidth }}
                    >
                      <Skeleton className="h-4 w-full" />
                    </div>
                  ))}
                </div>
              ))}
            </div>
          ) : items.length === 0 && !replacing ? (
            <div
              className="sticky left-0"
              style={{ width: containerWidth || "100%" }}
            >
              <OperatorEmptyState
                className="py-20"
                icon={<FileSearch className="h-6 w-6" />}
                title={
                  retentionClipped
                    ? messages.requestLogs.emptyCoverageClipped
                    : messages.requestLogs.noRequestLogsMatchSlice
                }
                description={
                  retentionClipped ? (
                    <span className="inline-flex flex-col items-center gap-2">
                      <span>
                        {messages.requestLogs.emptyCoverageClippedDescription}
                      </span>
                      <OperatorClippedBadge
                        label={messages.honesty.outsideRetention}
                        reason={messages.honesty.outsideRetentionReason}
                      />
                    </span>
                  ) : (
                    messages.requestLogs.attemptsEmptyDescription
                  )
                }
                action={emptyAction}
              />
            </div>
          ) : (
            <div style={{ height: totalHeight, position: "relative" }}>
              {items.slice(startIndex, endIndex).map((row, i) => {
                const isSelected = activeRequestId === row.request_log_id;
                const tone = getRowTone(row);

                return (
                  <button
                    type="button"
                    key={row.request_log_id}
                    aria-label={messages.requestLogs.requestTitle(
                      row.request_log_id,
                    )}
                    data-testid={`request-log-row-${row.request_log_id}`}
                    className={cn(
                      "absolute left-0 right-0 flex cursor-pointer items-center border-b text-left transition-colors",
                      "before:absolute before:inset-y-0 before:left-0 before:w-0.5 before:content-['']",
                      tone.row,
                      tone.stripe,
                      isSelected && "bg-primary-soft/40 before:bg-primary",
                    )}
                    style={{
                      height: ROW_HEIGHT,
                      top: (startIndex + i) * ROW_HEIGHT,
                    }}
                    onClick={() => onSelectRequest(row.request_log_id)}
                  >
                    {resolvedColumns.map((col: ResolvedColumn) => (
                      <div
                        key={col.key}
                        className={cn(
                          "flex h-full shrink-0 items-center overflow-hidden px-3",
                          col.align === "right" && "justify-end text-right",
                          col.align === "center" &&
                            "justify-center text-center",
                        )}
                        style={{ width: col.resolvedWidth }}
                      >
                        {col.render(row, formatTimestamp)}
                      </div>
                    ))}
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      <div className="flex flex-col gap-3 border-t border-border bg-inset px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>
            {total > 0
              ? totalIsExact
                ? messages.requestLogs.resultsRange(
                    formatNumber(pageStart),
                    formatNumber(pageEnd),
                    formatNumber(total),
                  )
                : messages.requestLogs.resultsRangeAtLeast(
                    formatNumber(pageStart),
                    formatNumber(pageEnd),
                    formatNumber(total),
                  )
              : messages.requestLogs.zeroResults}
          </span>
          <Select
            value={String(limit)}
            onValueChange={(v) => onSetLimit(Number(v))}
          >
            <SelectTrigger
              className="h-8 w-[92px] rounded-md border-border bg-panel text-xs"
              data-testid="request-log-page-size-select"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PAGE_SIZE_OPTIONS.map((s) => (
                <SelectItem key={s} value={String(s)}>
                  {s}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span>{messages.requestLogs.rowsPerPage}</span>
        </div>

        <div className="flex items-center gap-1">
          <Button
            variant="outline"
            size="icon"
            className="size-8 rounded-full"
            aria-label={messages.requestLogs.previousPage}
            disabled={!hasPrev}
            onClick={onPreviousPage}
          >
            <ChevronLeft />
          </Button>
          <Button
            variant="outline"
            size="icon"
            className="size-8 rounded-full"
            aria-label={messages.requestLogs.nextPage}
            disabled={!hasNext}
            onClick={onNextPage}
          >
            <ChevronRight />
          </Button>
        </div>
      </div>
    </div>
  );
}
