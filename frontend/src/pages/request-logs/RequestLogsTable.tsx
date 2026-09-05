// Requests SPEC §4 default columns: pricing state is a first-class column;
// the sheet is a wide-format (no fixed 640px) detail surface.
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { ArrowDown, ArrowUp, ArrowUpDown, FileSearch } from "lucide-react";
import type { ReactNode } from "react";
import { useLocale } from "@/i18n/useLocale";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import type { RequestLogListItem } from "@/lib/types";
import { OperatorClippedBadge, OperatorEmptyState } from "@/shared/design-system";
import {
  OperationalTablePagination,
  PaginationLiveStatus,
} from "@/shared/table/paginationControls";
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
  const copy = messages.requestLogs;
  const tableCopy = messages.operationalTable;
  const headingId = useId();
  const rowHintId = useId();
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

  const pageEnd = total > 0 ? Math.min(offset + limit, total) : 0;
  const hasPrev = offset > 0;
  const hasNext = hasMoreRows;
  // A replace read withdraws the old page: its rows belong to the previous
  // URL and must not masquerade as the target page while it loads.
  const showPendingRows = replacing;

  return (
    // 结果区块由一个真 h2 命名；壳上必须有高度上限，否则内层的 flex-1 永远
    // 撑到内容高，容器不滚动，sticky 表头等于没写。
    <section
      aria-labelledby={headingId}
      className="operator-table-shell flex max-h-[calc(100dvh-22rem)] min-h-0 flex-col overflow-hidden rounded-lg border border-border bg-panel"
      data-testid="request-logs-table"
    >
      <h2 id={headingId} className="sr-only">
        {copy.viewAttempts}
      </h2>
      <span id={rowHintId} className="sr-only">
        {copy.rowOpensDetailHint}
      </span>
      <PaginationLiveStatus
        message={showPendingRows ? tableCopy.loadingTargetPage : null}
      />
      {/* Adaptive viewport: the table fills the shell instead of a fixed 640px. */}
      <div
        ref={containerRef}
        className="min-h-0 flex-1 overflow-auto scrollbar-thin"
        onScroll={handleScroll}
        aria-busy={loading || undefined}
      >
        {/* 虚拟列表也要有表格语义：没有 table/row/columnheader/cell 时，
            读屏在这个视图里每行只听得到一句「请求 #102752 按钮」，
            时间、状态码、延迟、模型、令牌、费用全部丢失。 */}
        <div
          role="table"
          aria-label={copy.viewAttempts}
          aria-rowcount={items.length + 1}
          className="w-full"
          style={{ minWidth: totalWidth }}
        >
          <div
            role="row"
            className="sticky top-0 z-10 flex border-b border-border bg-inset"
          >
            {resolvedColumns.map((col) => {
              const sortKey = SORTABLE_COLUMN_KEYS[col.key];
              const isSorted = sortKey !== undefined && sortBy === sortKey;
              return (
                <div
                  key={col.key}
                  role="columnheader"
                  aria-sort={
                    sortKey === undefined
                      ? undefined
                      : isSorted
                        ? sortOrder === "asc"
                          ? "ascending"
                          : "descending"
                        : "none"
                  }
                  data-testid={col.headerTestId}
                  className={cn(
                    "shrink-0 text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground",
                    !sortKey && "px-3 py-2.5",
                    col.align === "right" && "text-right",
                    col.align === "center" && "text-center",
                  )}
                  style={{ width: col.resolvedWidth }}
                >
                  {sortKey ? (
                    // 命中区铺满整个表头单元格：16.5px 高的按钮既低于 28×28
                    // 下限，也让「点哪里能排序」变成要试出来的事。
                    <button
                      type="button"
                      aria-label={copy.sortByColumnAria(col.label)}
                      className={cn(
                        "flex h-full min-h-8 w-full items-center gap-1 px-3 py-2.5 hover:text-foreground",
                        col.align === "right" && "justify-end",
                        col.align === "center" && "justify-center",
                        isSorted && "text-foreground",
                      )}
                      onClick={() => onSortChange(sortKey)}
                    >
                      {col.label}
                      {isSorted ? (
                        sortOrder === "asc" ? (
                          <ArrowUp className="size-3 shrink-0 text-primary" />
                        ) : (
                          <ArrowDown className="size-3 shrink-0 text-primary" />
                        )
                      ) : (
                        <ArrowUpDown className="size-3 shrink-0" />
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
            <div role="rowgroup" className="flex flex-col gap-0">
              <span role="status" aria-live="polite" className="sr-only">
                {tableCopy.loadingFirstPage}
              </span>
              {SKELETON_ROW_KEYS.map((key) => (
                <div
                  key={key}
                  aria-hidden="true"
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
            <div
              role="rowgroup"
              style={{ height: totalHeight, position: "relative" }}
            >
              {items.slice(startIndex, endIndex).map((row, i) => {
                const isSelected = activeRequestId === row.request_log_id;
                const tone = getRowTone(row);

                return (
                  // 行的可访问名就是它的可见内容：整行挂一个 aria-label 会把
                  // 时间／状态／延迟／模型／令牌／费用全部抹掉，只剩一句请求号。
                  <button
                    type="button"
                    role="row"
                    aria-rowindex={startIndex + i + 2}
                    aria-describedby={rowHintId}
                    key={row.request_log_id}
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
                        role="cell"
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

      {/* 分页行走共享实现：「共 N 条」在左、页控件与页大小在右。
          总数不精确时换用「共超过 N 条」，不把估计值写成确数。 */}
      <OperationalTablePagination
        currentPageIndex={Math.floor(offset / Math.max(limit, 1))}
        startIndex={offset}
        endIndex={pageEnd}
        totalRows={total}
        formatNumber={formatNumber}
        hasPreviousPage={hasPrev}
        hasNextPage={hasNext}
        onPreviousPage={onPreviousPage}
        onNextPage={onNextPage}
        previousLabel={tableCopy.previousPage}
        nextLabel={tableCopy.nextPage}
        resultsLabel={
          totalIsExact ? tableCopy.resultsRange : tableCopy.resultsRangeAtLeast
        }
        totalLabel={totalIsExact ? tableCopy.totalRows : tableCopy.totalRowsAtLeast}
        zeroLabel={tableCopy.zeroResults}
        pageSize={{
          ariaLabel: tableCopy.pageSize,
          onChange: onSetLimit,
          options: PAGE_SIZE_OPTIONS,
          value: limit,
        }}
      />
    </section>
  );
}
