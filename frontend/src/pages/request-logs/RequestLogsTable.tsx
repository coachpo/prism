import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, FileSearch } from "lucide-react";
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
import {
  getColumns,
  ROW_HEIGHT,
  type ColumnDef,
  type RequestLogEndpointResolver,
  type RequestLogModelResolver,
} from "./columns";
import { PAGE_SIZE_OPTIONS } from "./queryParams";

interface RequestLogsTableProps {
  items: RequestLogListItem[];
  total: number;
  loading: boolean;
  limit: number;
  offset: number;
  activeRequestId: number | null;
  onSelectRequest: (id: number) => void;
  onSetLimit: (limit: number) => void;
  onNextPage: () => void;
  onPreviousPage: () => void;
  formatTimestamp: (iso: string) => string;
  resolveModelLabel: RequestLogModelResolver;
  resolveEndpointLabel: RequestLogEndpointResolver;
}

interface ResolvedColumn extends ColumnDef {
  resolvedWidth: number;
}

const OVERSCAN = 10;
const TABLE_VIEWPORT_HEIGHT = 640;
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

function getRowTone(row: RequestLogListItem, isSelected: boolean) {
  if (isSelected) {
    return {
      row: "border-sky-500/20 bg-sky-500/[0.08] hover:bg-sky-500/[0.12]",
    };
  }

  if (row.status_code >= 500) {
    return {
      row: "border-red-500/15 bg-red-500/[0.06] hover:bg-red-500/[0.10]",
    };
  }

  if (row.status_code >= 400 || row.response_time_ms >= 5000) {
    return {
      row: "border-amber-500/15 bg-amber-500/[0.05] hover:bg-amber-500/[0.09]",
    };
  }

  return {
    row: "border-border/50 bg-card hover:bg-muted/40",
  };
}

function resolveColumns(columns: ColumnDef[], containerWidth: number): ResolvedColumn[] {
  const baseWidth = columns.reduce((sum, col) => sum + col.width, 0);
  const growWeight = columns.reduce((sum, col) => sum + (col.grow ?? 0), 0);
  const extraWidth = Math.max(0, containerWidth - baseWidth);

  return columns.map((col) => {
    const resolvedWidth = Math.round(col.width + (growWeight > 0 ? extraWidth * ((col.grow ?? 0) / growWeight) : 0));
    return {
      ...col,
      resolvedWidth,
    };
  });
}

export function RequestLogsTable({
  items,
  total,
  loading,
  limit,
  offset,
  activeRequestId,
  onSelectRequest,
  onSetLimit,
  onNextPage,
  onPreviousPage,
  formatTimestamp,
  resolveModelLabel,
  resolveEndpointLabel,
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

  const resolvedColumns = useMemo(
    () => resolveColumns(columns, Math.max(containerWidth - 2, 0)),
    [columns, containerWidth]
  );

  const totalWidth = useMemo(
    () => resolvedColumns.reduce((sum, col) => sum + col.resolvedWidth, 0),
    [resolvedColumns]
  );

  const totalHeight = items.length * ROW_HEIGHT;
  const startIndex = Math.max(0, Math.floor(scrollTop / ROW_HEIGHT) - OVERSCAN);
  const visibleCount = Math.ceil(containerHeight / ROW_HEIGHT) + OVERSCAN * 2;
  const endIndex = Math.min(items.length, startIndex + visibleCount);

  const pageStart = total > 0 ? offset + 1 : 0;
  const pageEnd = total > 0 ? Math.min(offset + limit, total) : 0;
  const hasPrev = offset > 0;
  const hasNext = offset + limit < total;

  return (
    <div className="overflow-hidden rounded-xl border border-border/70 bg-card shadow-sm" data-testid="request-logs-table">
      <div
        ref={containerRef}
        className="overflow-auto scrollbar-thin"
        style={{ height: TABLE_VIEWPORT_HEIGHT }}
        onScroll={handleScroll}
      >
        <div className="w-full" style={{ minWidth: totalWidth }}>
          <div className="sticky top-0 z-10 flex border-b border-border/70 bg-background/92 backdrop-blur-md">
            {resolvedColumns.map((col) => (
              <div
                key={col.key}
                data-testid={col.headerTestId}
                className={cn(
                  "shrink-0 px-3 py-2.5 text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground",
                  col.align === "right" && "text-right",
                  col.align === "center" && "text-center"
                )}
                style={{ width: col.resolvedWidth }}
              >
                {col.label}
              </div>
            ))}
          </div>

          {loading && items.length === 0 ? (
            <div className="space-y-0">
              {SKELETON_ROW_KEYS.map((key) => (
                <div key={key} className="flex border-b border-border/40 bg-card/70" style={{ height: ROW_HEIGHT }}>
                  {resolvedColumns.map((col) => (
                    <div key={col.key} className="shrink-0 px-3 py-3" style={{ width: col.resolvedWidth }}>
                      <Skeleton className="h-4 w-full" />
                    </div>
                  ))}
                </div>
              ))}
            </div>
          ) : items.length === 0 ? (
            <div className="flex flex-col items-center justify-center gap-3 py-20 text-center">
              <div className="flex h-12 w-12 items-center justify-center rounded-full border border-border/70 bg-muted/30">
                <FileSearch className="h-5 w-5 text-muted-foreground" />
              </div>
              <div className="space-y-1">
                <p className="text-sm font-medium">{messages.requestLogs.noRequestLogsMatchSlice}</p>
                <p className="text-xs text-muted-foreground">
                  {messages.statistics.adjustFiltersOrTimeRange}
                </p>
              </div>
            </div>
          ) : (
            <div style={{ height: totalHeight, position: "relative" }}>
              {items.slice(startIndex, endIndex).map((row, i) => {
                const isSelected = activeRequestId === row.id;
                const tone = getRowTone(row, isSelected);

                return (
                  <button
                    type="button"
                    key={row.id}
                    className={cn(
                      "absolute left-0 right-0 flex cursor-pointer items-center border-b border-l-2 text-left transition-colors",
                      tone.row,
                      isSelected ? "border-l-primary" : "border-l-transparent"
                    )}
                    style={{
                      height: ROW_HEIGHT,
                      top: (startIndex + i) * ROW_HEIGHT,
                    }}
                    onClick={() => onSelectRequest(row.id)}
                  >
                    {resolvedColumns.map((col: ResolvedColumn) => (
                      <div
                        key={col.key}
                        className={cn(
                          "flex h-full shrink-0 items-center overflow-hidden px-3",
                          col.align === "right" && "justify-end text-right",
                          col.align === "center" && "justify-center text-center"
                         )}
                         style={{ width: col.resolvedWidth }}
                       >
                         {col.render(row, formatTimestamp, resolveModelLabel, resolveEndpointLabel)}
                       </div>
                     ))}
                   </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      <div className="flex flex-col gap-3 border-t border-border/70 bg-muted/20 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>
            {total > 0
              ? messages.requestLogs.resultsRange(
                  formatNumber(pageStart),
                  formatNumber(pageEnd),
                  formatNumber(total),
                )
              : messages.requestLogs.zeroResults}
          </span>
          <Select value={String(limit)} onValueChange={(v) => onSetLimit(Number(v))}>
            <SelectTrigger
              className="h-8 w-[92px] rounded-full border-border/70 bg-background text-xs"
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
          <Button variant="outline" size="icon" className="h-8 w-8 rounded-full" disabled={!hasPrev} onClick={onPreviousPage}>
            <ChevronLeft className="h-4 w-4" />
          </Button>
          <Button variant="outline" size="icon" className="h-8 w-8 rounded-full" disabled={!hasNext} onClick={onNextPage}>
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
