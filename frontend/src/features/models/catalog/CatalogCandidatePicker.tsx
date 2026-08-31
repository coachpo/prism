import type { ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  OperatorCallout,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorRetryButton,
} from "@/shared/design-system";
import {
  LoadMoreControl,
  PaginationLiveStatus,
} from "@/shared/table/paginationControls";
import type { AppendCandidatePager } from "@/shared/table/useAppendCandidatePager";

/**
 * Source-agnostic candidate picker shell for catalog discovery flows. It
 * composes only the shared operator surfaces and the shared pager contract:
 * loading, replace error + retry, honest empty, caller-supplied candidate
 * rendering, explicit selection, load-more/retry, live status, loaded/total
 * count, and the revision-rollover callout.
 *
 * It lives at the feature level, not the design system, because it knows
 * what a "catalog candidate" interaction is; API DTOs and source policy stay
 * with the adapters, and all visible copy arrives via props.
 */
export interface CatalogCandidatePickerProps<T, Evidence = unknown> {
  pager: AppendCandidatePager<T, Evidence>;
  labels: {
    loading: string;
    loadFailed: string;
    retry: string;
    empty: string;
    emptyDescription?: string;
    loadMore: string;
    loadingMore: string;
    retryLoadMore: string;
    count: (shown: number, total: number) => string;
    liveLoading: string;
    revisionRollover: string;
    revisionRolloverAcknowledge: string;
    /** Accessible name of the results listbox. */
    listboxLabel: string;
  };
  itemKey: (item: T) => string;
  renderCandidate: (item: T) => ReactNode;
  selectedKey: string | null;
  onSelect: (key: string | null) => void;
  disabled?: boolean;
  testIdPrefix?: string;
  footer?: ReactNode;
}

export function CatalogCandidatePicker<T, Evidence = unknown>({
  pager,
  labels,
  itemKey,
  renderCandidate,
  selectedKey,
  onSelect,
  disabled = false,
  testIdPrefix = "catalog-candidate",
  footer,
}: CatalogCandidatePickerProps<T, Evidence>) {
  return (
    <div className="flex flex-col gap-2">
      {pager.revisionRolledOver ? (
        <OperatorCallout
          intent="warning"
          data-testid={`${testIdPrefix}-rollover`}
          description={labels.revisionRollover}
          action={
            pager.replacing ? undefined : (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={pager.onRolloverAcknowledged}
            >
              {labels.revisionRolloverAcknowledge}
            </Button>
            )
          }
        />
      ) : null}
      {pager.replacing ? (
        <OperatorLoadingState title={labels.loading} className="py-3" />
      ) : pager.phase === "error" ? (
        // 替换读取失败时候选集未知，不能降级成“没有匹配”的空结果。
        <OperatorErrorState
          testId={`${testIdPrefix}-error`}
          title={labels.loadFailed}
          description={pager.error ?? undefined}
          action={
            <OperatorRetryButton onClick={pager.onRetry}>
              {labels.retry}
            </OperatorRetryButton>
          }
        />
      ) : pager.items.length === 0 ? (
        <OperatorEmptyState
          testId={`${testIdPrefix}-empty`}
          title={labels.empty}
          description={labels.emptyDescription}
          className="py-4"
        />
      ) : (
        <>
          <div
            role="listbox"
            aria-label={labels.listboxLabel}
            aria-busy={pager.appending || undefined}
            className="max-h-56 overflow-y-auto"
          >
            {pager.items.map((item) => {
              const key = itemKey(item);
              const selected = selectedKey === key;
              return (
                <button
                  key={key}
                  type="button"
                  role="option"
                  aria-selected={selected}
                  disabled={disabled}
                  data-testid={`${testIdPrefix}-option`}
                  onClick={() => onSelect(selected ? null : key)}
                  className={cn(
                    "w-full rounded px-1 py-0.5 text-left hover:bg-muted",
                    selected && "bg-muted",
                  )}
                >
                  {renderCandidate(item)}
                </button>
              );
            })}
          </div>
          <LoadMoreControl
            testId={`${testIdPrefix}-load-more`}
            pending={pager.appending}
            error={pager.appendError}
            hasMore={pager.hasMore}
            labels={{
              loadMore: labels.loadMore,
              loading: labels.loadingMore,
              retry: labels.retryLoadMore,
            }}
            onLoadMore={pager.onLoadMore}
          />
        </>
      )}
      {!pager.replacing && pager.phase !== "error" ? (
        <>
          <PaginationLiveStatus
            message={pager.appending ? labels.liveLoading : null}
          />
          <p className="text-xs text-muted-foreground">
            {labels.count(pager.items.length, pager.total)}
          </p>
        </>
      ) : null}
      {footer}
    </div>
  );
}
