import { ChevronDown, ChevronLeft, ChevronRight, Loader2, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
      Select,
      SelectContent,
      SelectGroup,
      SelectItem,
      SelectTrigger,
      SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

/**
 * Polite live region for async pagination progress. The message is Chinese UI
 * copy supplied by the caller through messages; it is announced without any
 * visual footprint so a replace/append read is perceivable without eyes on it.
 */
export function PaginationLiveStatus({ message }: { message: string | null }) {
      return (
            <span aria-live="polite" role="status" className="sr-only">
                  {message ?? ""}
            </span>
      );
}

type LoadMoreControlLabels = {
      loadMore: string;
      loading: string;
      retry: string;
};

type LoadMoreControlProps = {
      /** An append read is in flight; old items stay visible above this control. */
      pending: boolean;
      /**
       * The last append read failed. Old items remain untouched and this control
       * becomes the local retry — an append failure must never blank the list or
       * surface only as a page-wide banner.
       */
      error: string | null;
      hasMore: boolean;
      labels: LoadMoreControlLabels;
      onLoadMore: () => void;
      testId?: string;
};

/**
 * The shared "加载更多" control for append-style lists. One control owns all
 * three honest states of the next page: idle (load), in-flight (spinner,
 * disabled), failed (inline retryable error). Scoped single-flight is the
 * caller's job; this control just refuses to double-fire while pending.
 */
export function LoadMoreControl({
      error,
      hasMore,
      labels,
      onLoadMore,
      pending,
      testId,
}: LoadMoreControlProps) {
      if (!hasMore && !error && !pending) return null;
      const actionable = hasMore || Boolean(error);
      return (
            <div className="flex flex-col items-start gap-1.5">
                  <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        data-testid={testId}
                        aria-busy={pending}
                        disabled={!actionable || pending}
                        onClick={() => {
                              if (!pending) onLoadMore();
                        }}
                  >
                        {pending ? (
                              <Loader2
                                    data-icon="inline-start"
                                    className="animate-spin"
                              />
                        ) : error ? (
                              <RefreshCw data-icon="inline-start" />
                        ) : (
                              <ChevronDown data-icon="inline-start" />
                        )}
                        {pending
                              ? labels.loading
                              : error
                                ? labels.retry
                                : labels.loadMore}
                  </Button>
                  {error && !pending ? (
                        <p
                              role="alert"
                              className="text-xs text-failing"
                              data-testid={
                                    testId ? `${testId}-error` : undefined
                              }
                        >
                              {error}
                        </p>
                  ) : null}
            </div>
      );
}

type OperationalTablePaginationProps = {
      className?: string;
      currentPageIndex: number;
      endIndex: number;
      formatNumber: (value: number) => string;
      hasNextPage: boolean;
      hasPreviousPage: boolean;
      nextLabel: string;
      onNextPage: () => void;
      onPreviousPage: () => void;
      /** Page-size selector. Omit when the page size is not adjustable. */
      pageSize?: {
            ariaLabel: string;
            onChange: (value: number) => void;
            options: readonly number[];
            value: number;
      };
      previousLabel: string;
      resultsLabel: (start: string, end: string, total: string) => string;
      startIndex: number;
      totalRows: number;
      /** `共 N 条`, already localized. Shown on the left beside the range. */
      totalLabel?: (total: string) => string;
      zeroLabel: string;
      /** Jump straight to a page. Without it only prev/next are rendered. */
      onGoToPage?: (pageIndex: number) => void;
      /** Localized `第 N 页`, for the page buttons' accessible names. */
      pageLabel?: (page: string) => string;
      /** Total pages, when the caller knows it. Derived otherwise. */
      pageCount?: number;
};

/**
 * First page, last page, and a window around the current one; `null` marks an
 * elided run. Keeps the control a fixed width however many pages there are.
 */
function windowedPages(currentPageIndex: number, totalPages: number): Array<number | null> {
      if (totalPages <= 7) {
            return Array.from({ length: totalPages }, (_, index) => index);
      }
      const pages = new Set<number>([0, totalPages - 1]);
      for (let offset = -1; offset <= 1; offset += 1) {
            const page = currentPageIndex + offset;
            if (page >= 0 && page < totalPages) pages.add(page);
      }
      const ordered = [...pages].sort((left, right) => left - right);
      const withGaps: Array<number | null> = [];
      ordered.forEach((page, index) => {
            if (index > 0 && page - ordered[index - 1] > 1) withGaps.push(null);
            withGaps.push(page);
      });
      return withGaps;
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
      const pageStart = totalRows > 0 ? startIndex + 1 : 0;
      const totalPages = pageCount ?? (pageSize ? Math.max(1, Math.ceil(totalRows / pageSize.value)) : 1);
      const pageNumbers = onGoToPage && pageLabel ? windowedPages(currentPageIndex, totalPages) : [];

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
      );
}
