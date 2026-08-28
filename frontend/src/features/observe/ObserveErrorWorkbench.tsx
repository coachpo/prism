import { useCallback, useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import {
  observe,
  type ObserveActivityItem,
  type ObserveActivityResponse,
  type UsageErrorsResponse,
} from "@/lib/api/observability";
import {
  OperatorClippedBadge,
  OperatorCallout,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorRetryButton,
  OperatorStatusBadge,
  OperatorTypeBadge,
} from "@/shared/design-system";
import {
  fragmentErrorFrom,
  type FragmentState,
} from "@/features/observe/useObserveFragments";
import { ObserveErrorPanel } from "@/features/observe/ObserveErrorPanel";
import type { ObserveErrorSelection } from "@/features/observe/observeErrorSelection";
import type {
  ObserveGroupBy,
  ObserveScope,
} from "@/features/observe/observeSearch";

const STREAM_PAGE_SIZE = 50;

/**
 * The anomaly workbench: the error ranking and the request stream side by
 * side. Picking a ranking entry filters the stream in place, so diagnosing
 * "which requests are these" no longer means leaving the dashboard and losing
 * the ranking that prompted the question.
 *
 * The stream matches within the page it has loaded, not across the whole
 * window, and says so — the full set stays one click away through the
 * backend-built filter conjunction.
 */
export function ObserveErrorWorkbench({
  queryContext,
  scope,
  groupBy,
}: {
  groupBy: ObserveGroupBy;
  queryContext: string | null;
  scope: ObserveScope;
}) {
  const { messages } = useLocale();
  const copy = messages.observe;
  const contextKey = `${queryContext ?? ""}:${scope}:${groupBy}`;
  const [selectionSnapshot, setSelectionSnapshot] = useState<{
    key: string;
    value: ObserveErrorSelection | null;
  }>(() => ({ key: contextKey, value: null }));
  const [requestsContextSnapshot, setRequestsContextSnapshot] = useState<{
    key: string;
    value: UsageErrorsResponse["requests_context"] | null;
  }>(() => ({ key: contextKey, value: null }));
  const selection =
    selectionSnapshot.key === contextKey ? selectionSnapshot.value : null;
  const requestsContext =
    requestsContextSnapshot.key === contextKey
      ? requestsContextSnapshot.value
      : null;

  const handleSelection = useCallback(
    (next: ObserveErrorSelection | null) => {
      setSelectionSnapshot({ key: contextKey, value: next });
    },
    [contextKey],
  );

  const handleContextResolved = useCallback(
    (context: UsageErrorsResponse["requests_context"]) => {
      setRequestsContextSnapshot({ key: contextKey, value: context });
    },
    [contextKey],
  );

  return (
    <div className="grid min-w-0 gap-[var(--density-card-gap)] xl:grid-cols-[minmax(0,22rem)_minmax(0,1fr)]">
      <ObserveErrorPanel
        groupBy={groupBy}
        queryContext={queryContext}
        onContextResolved={handleContextResolved}
        onSelect={handleSelection}
        selectedKey={selection?.key ?? null}
      />

      <OperatorInsetPanel
        className="min-w-0"
        title={copy.workbenchStreamTitle}
        description={copy.workbenchScopeBasis}
        actions={
          selection ? (
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => handleSelection(null)}
              >
                {copy.workbenchClearSelection}
              </Button>
              <Button asChild type="button" variant="outline" size="sm">
                <Link
                  to="/observe/requests"
                  search={buildRequestsSearch(
                    selection,
                    requestsContext,
                    queryContext,
                    scope,
                  )}
                >
                  {copy.workbenchOpenInRequests}
                </Link>
              </Button>
            </div>
          ) : null
        }
      >
        {selection && scope === "ingress" ? (
          <MatchingStream queryContext={queryContext} selection={selection} />
        ) : selection ? (
          <OperatorCallout intent="info">
            {copy.scopedErrorsOpenRequestsHint}
          </OperatorCallout>
        ) : (
          <p className="text-xs text-muted-foreground">
            {copy.workbenchNoSelection}
          </p>
        )}
      </OperatorInsetPanel>
    </div>
  );
}

/**
 * The deep link keeps the backend's `request_filters` and `requests_context`
 * verbatim — the same conjunction the ranking used to navigate with.
 */
function buildRequestsSearch(
  selection: ObserveErrorSelection,
  requestsContext: UsageErrorsResponse["requests_context"] | null,
  queryContext: string | null,
  _scope: ObserveScope,
): Record<string, string> {
  void _scope;
  const search: Record<string, string> = {
    view: "attempts",
    query_context: requestsContext?.query_context ?? queryContext ?? "",
  };
  for (const [key, values] of Object.entries(selection.requestFilters)) {
    search[key] = values.join(",");
  }
  return search;
}

function MatchingStream({
  queryContext,
  selection,
}: {
  queryContext: string | null;
  selection: ObserveErrorSelection;
}) {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.observe;
  const [fragment, setFragment] = useState<
    FragmentState<ObserveActivityResponse>
  >({
    phase: "loading",
    data: null,
    stale: false,
    error: null,
    retryAfterMs: null,
  });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!queryContext) return;
    let cancelled = false;
    void observe
      .observeActivity(queryContext, { limit: STREAM_PAGE_SIZE })
      .then((data) => {
        if (!cancelled)
          setFragment({
            phase: "ready",
            data,
            stale: false,
            error: null,
            retryAfterMs: null,
          });
      })
      .catch((error: unknown) => {
        if (cancelled) return;
        const mapped = fragmentErrorFrom(error);
        setFragment((previous) => ({
          ...previous,
          phase: "error",
          stale: previous.data !== null,
          error: mapped.error,
          retryAfterMs: mapped.retryAfterMs,
        }));
      });
    return () => {
      cancelled = true;
    };
  }, [attempt, queryContext]);

  if (fragment.phase === "loading")
    return <Skeleton className="h-40 rounded-md" />;
  if (fragment.phase === "error" && fragment.data === null) {
    return (
      <OperatorErrorState
        title={copy.windowUnavailable}
        description={fragment.error ?? undefined}
        action={
          <OperatorRetryButton
            onClick={() => setAttempt((current) => current + 1)}
          >
            {messages.common.retry}
          </OperatorRetryButton>
        }
      />
    );
  }
  if (fragment.data === null) {
    return (
      <p className="text-xs text-muted-foreground">{copy.windowUnavailable}</p>
    );
  }

  const scanned = fragment.data.items;
  const matched = scanned.filter((item) => selection.match(item));

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <OperatorTypeBadge
          intent="accent"
          preserveLabel
          label={selection.label}
        />
        <span className="font-mono tabular-nums">
          {copy.workbenchMatchedCount(
            formatNumber(matched.length),
            formatNumber(scanned.length),
          )}
        </span>
        {/* The page boundary is a real clip, so it is labelled as one. */}
        <OperatorClippedBadge
          label={copy.workbenchStreamTitle}
          reason={copy.workbenchScopeBasis}
        />
      </div>

      {matched.length === 0 ? (
        <OperatorEmptyState title={copy.workbenchNoMatches} className="py-6" />
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{messages.requestLogs.timeRange}</TableHead>
                <TableHead>{messages.modelDetail.modelIdLabel}</TableHead>
                <TableHead>{messages.common.status}</TableHead>
                <TableHead className="text-right">
                  {messages.requestLogs.tokens}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {matched.map((item) => (
                <StreamRow
                  key={item.usage_event_id}
                  formatTime={formatTime}
                  item={item}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function StreamRow({
  formatTime,
  item,
}: {
  formatTime: (value: string) => string;
  item: ObserveActivityItem;
}) {
  const { formatNumber, messages } = useLocale();
  return (
    <TableRow>
      <TableCell className="font-mono text-xs tabular-nums">
        {formatTime(item.created_at)}
      </TableCell>
      <TableCell className="text-xs">
        {item.ingress_model_label || item.ingress_model_id}
      </TableCell>
      <TableCell>
        <OperatorStatusBadge
          intent={
            item.final_result === "completed"
              ? "healthy"
              : item.final_result === "failed"
                ? "failing"
                : "degraded"
          }
          preserveLabel
          label={`${item.status_code}`}
        />
      </TableCell>
      <TableCell className="text-right font-mono text-xs tabular-nums">
        {item.total_tokens === null
          ? messages.honesty.noValue
          : formatNumber(item.total_tokens)}
      </TableCell>
    </TableRow>
  );
}
