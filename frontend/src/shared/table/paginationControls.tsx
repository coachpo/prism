import { ChevronDown, Loader2, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";

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
