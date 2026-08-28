import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
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
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import {
  observe,
  type ObserveActivityItem,
  type ObserveActivityResponse,
} from "@/lib/api/observability";
import { fragmentErrorFrom } from "@/features/observe/useObserveFragments";
import type { ObservePreset } from "@/features/observe/observeSearch";
import { cn } from "@/lib/utils";
import {
  OperatorEmptyState,
  OperatorErrorState,
  OperatorClippedBadge,
  OperatorMissingValue,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
  type OperatorStatusTier,
} from "@/shared/design-system";
import {
  OperationalTableSkeletonRows,
  operationalRowStripe,
} from "@/shared/table/operationalTable";
import { PaginationLiveStatus } from "@/shared/table/paginationControls";
import {
  beginPagedRead,
  commitPagedRead,
  failPagedRead,
  initialPagedListState,
  keepsCommittedRows,
  shouldShowPendingRows,
  type PageReadKind,
  type PagedListState,
} from "@/shared/table/paginationStates";
import { RetryAfterCallout } from "@/features/observe/RetryAfterCallout";
import { nextObserveActivityCursor } from "@/features/observe/observeActivityPagination";

const ACTIVITY_PAGE_SIZE = 20;

/**
 * Compact finalized activity feed: one row per retained finalized ingress
 * request. Route changes, HTTP/stream results and finalized pricing are shown
 * inline; every row has a named "查看请求" action.
 *
 * Times follow the global timezone, and the feed pages through the cursor the
 * backend already returns instead of silently stopping at the first 20 rows.
 * Page turns are replace reads (skeleton rows under a busy table shell); a
 * failed page presents its own retry instead of repainting the old page as
 * the new one.
 */
export function ObserveActivityTable({
  preset,
  queryContext,
}: {
  preset: ObservePreset;
  queryContext: string | null;
}) {
  const { formatNumber, messages } = useLocale();
  const navigate = useNavigate();
  const [fragment, setFragment] = useState<
    PagedListState<ObserveActivityResponse>
  >(() => initialPagedListState());
  // Server-reported 503 backoff, kept beside the paged state: it belongs to
  // this read's failure surface, not to the shared pagination contract.
  const [retryAfterMs, setRetryAfterMs] = useState<number | null>(null);
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  // The last successfully loaded scope decides whether the next read replaces
  // committed rows or starts the list over.
  const loadedKeyRef = useRef<string | null>(null);
  const generationRef = useRef(0);

  const before = cursorStack.at(-1);
  const scopeKey = `${queryContext ?? ""}:${before ?? ""}`;

  // A new window invalidates every outstanding cursor: they were issued by a
  // different signed context and do not address this window's rows.
  const previousQueryContextRef = useRef(queryContext);
  useEffect(() => {
    if (previousQueryContextRef.current !== queryContext) {
      previousQueryContextRef.current = queryContext;
      setCursorStack([]);
    }
  }, [queryContext]);

  useEffect(() => {
    if (!queryContext) return;
    const generation = ++generationRef.current;
    const kind: PageReadKind =
      fragment.data === null || loadedKeyRef.current === null
        ? "initial"
        : loadedKeyRef.current === scopeKey
          ? "refresh"
          : "replace";
    setFragment((current) => beginPagedRead(current, kind));
    let cancelled = false;
    void observe
      .observeActivity(queryContext, { limit: ACTIVITY_PAGE_SIZE, before })
      .then((data) => {
        if (cancelled || generation !== generationRef.current) return;
        loadedKeyRef.current = scopeKey;
        setRetryAfterMs(null);
        setFragment((current) =>
          commitPagedRead(
            current,
            data,
            data.items.length === 0 ? "empty" : "ready",
          ),
        );
      })
      .catch((err: unknown) => {
        if (cancelled || generation !== generationRef.current) return;
        const mapped = fragmentErrorFrom(err);
        setRetryAfterMs(mapped.retryAfterMs);
        setFragment((current) => failPagedRead(current, mapped.error));
      });
    return () => {
      cancelled = true;
    };
    // fragment.data intentionally excluded: the read kind is decided first.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scopeKey, queryContext]);

  const retryRead = useCallback(() => {
    if (!queryContext) return;
    const generation = ++generationRef.current;
    setFragment((current) =>
      beginPagedRead(
        current,
        current.readKind === "replace" ? "replace" : "refresh",
      ),
    );
    void observe
      .observeActivity(queryContext, { limit: ACTIVITY_PAGE_SIZE, before })
      .then((data) => {
        if (generation !== generationRef.current) return;
        loadedKeyRef.current = scopeKey;
        setRetryAfterMs(null);
        setFragment((current) =>
          commitPagedRead(
            current,
            data,
            data.items.length === 0 ? "empty" : "ready",
          ),
        );
      })
      .catch((err: unknown) => {
        if (generation !== generationRef.current) return;
        const mapped = fragmentErrorFrom(err);
        setRetryAfterMs(mapped.retryAfterMs);
        setFragment((current) => failPagedRead(current, mapped.error));
      });
  }, [before, queryContext, scopeKey]);

  const openRequests = useCallback(
    (item: ObserveActivityItem) => {
      void navigate({
        to: "/observe/requests",
        search: {
          view: "ingress_chains",
          ingress_request_id: item.final_ingress_request_id,
          time_range: preset,
        },
      });
    },
    [navigate, preset],
  );

  const tableCopy = messages.operationalTable;

  // The card content is px-0 so the table can reach the card edge; every
  // non-table block carries the gutter itself.
  if (!queryContext || (fragment.phase === "idle" && !fragment.reading)) {
    return (
      <Skeleton
        className="mx-[var(--density-card-pad-x)] h-48 rounded-md"
        aria-busy="true"
      />
    );
  }

  if (fragment.data === null && !fragment.reading && fragment.error !== null) {
    return (
      <div className="flex flex-col gap-2 px-[var(--density-card-pad-x)]">
        {retryAfterMs !== null ? (
          <RetryAfterCallout retryAfterMs={retryAfterMs} />
        ) : null}
        <OperatorErrorState
          testId="activity-load-error"
          title={messages.observe.windowUnavailable}
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={retryRead}
            >
              {messages.routingHealth.retry}
            </Button>
          }
        />
      </div>
    );
  }

  if (fragment.data === null) {
    return (
      <Skeleton
        className="mx-[var(--density-card-pad-x)] h-48 rounded-md"
        aria-busy="true"
      />
    );
  }

  const items = fragment.data.items;
  // The endpoint pages with a `before` usage-event cursor and reports `has_more`; it
  // returns no total, so the footer states the page range rather than
  // inventing a count.
  const nextCursor = nextObserveActivityCursor(items, fragment.data.has_more);
  const showPendingRows = shouldShowPendingRows(fragment);
  const showCommittedRows = keepsCommittedRows(fragment) && items.length > 0;
  const deepLinkedPage = Boolean(before) && cursorStack.length === 0;
  // A failed replace read presents the target-page error instead of the old
  // rows; a failed refresh keeps rows behind the staleness badge.
  const replaceFailureVisible =
    !fragment.reading &&
    fragment.error !== null &&
    fragment.data !== null &&
    !fragment.stale;

  if (items.length === 0 && fragment.phase === "empty" && !fragment.reading) {
    return (
      <div className="flex flex-col gap-2 px-[var(--density-card-pad-x)]">
        {fragment.stale ? (
          <OperatorStalenessBadge
            label={messages.observe.staleDataNote}
            reason={fragment.error ?? undefined}
          />
        ) : null}
        <OperatorEmptyState
          title={messages.observe.noData}
          description={messages.observe.adjustFiltersHint}
        />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <PaginationLiveStatus
        message={
          !fragment.reading
            ? null
            : fragment.data === null
              ? tableCopy.loadingFirstPage
              : deepLinkedPage
                ? tableCopy.loadingTargetPage
                : tableCopy.loadingPage(cursorStack.length + 1)
        }
      />
      {fragment.stale ? (
        <OperatorStalenessBadge
          className="mx-[var(--density-card-pad-x)] self-start"
          label={messages.observe.staleDataNote}
          reason={fragment.error ?? undefined}
        />
      ) : null}

      {/* The card border is this table's border; no second ring around it. */}
      <div data-testid="observe-activity-table" aria-busy={fragment.reading}>
        <div className="overflow-x-auto">
          <Table aria-label={messages.observe.activityTitle}>
            <TableHeader>
              <TableRow>
                <TableHead>{messages.observe.time}</TableHead>
                <TableHead>{messages.observe.activityRoute}</TableHead>
                <TableHead>{messages.observe.result}</TableHead>
                <TableHead className="text-right">
                  {messages.observe.activityAttempts}
                </TableHead>
                <TableHead>
                  {messages.observe.activityExecutionTarget}
                </TableHead>
                <TableHead className="text-right">TTFT</TableHead>
                <TableHead className="text-right">
                  {messages.observe.tokens}
                </TableHead>
                <TableHead className="text-right">
                  {messages.observe.cost}
                </TableHead>
                <TableHead>{messages.observe.pricingStatus}</TableHead>
                <TableHead className="text-right">
                  {messages.routingHealth.actionsColumn}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {showPendingRows ? (
                <OperationalTableSkeletonRows columns={10} rows={5} />
              ) : null}
              {showCommittedRows
                ? items.map((item) => (
                    <ActivityRow
                      key={item.usage_event_id}
                      item={item}
                      onOpenRequest={openRequests}
                    />
                  ))
                : null}
            </TableBody>
          </Table>
        </div>

        {replaceFailureVisible ? (
          <div className="px-[var(--density-card-pad-x)] py-3">
            <OperatorErrorState
              testId="activity-page-error"
              title={
                deepLinkedPage
                  ? messages.observe.windowUnavailable
                  : tableCopy.pageLoadFailed(cursorStack.length + 1)
              }
              description={messages.honesty.readFailedDescription}
              details={fragment.error}
              detailsLabel={messages.honesty.viewDetails}
              action={
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={retryRead}
                >
                  {messages.routingHealth.retry}
                </Button>
              }
            />
          </div>
        ) : (
          <div className="flex items-center justify-between gap-2 border-t border-border bg-inset px-[var(--density-card-pad-x)] py-2">
            <span className="text-xs text-muted-foreground">
              {messages.observe.activityPageRange(
                formatNumber(cursorStack.length + 1),
                formatNumber(items.length),
              )}
            </span>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={fragment.reading || cursorStack.length === 0}
                onClick={() => setCursorStack((stack) => stack.slice(0, -1))}
              >
                {messages.routingHealth.previousPage}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={fragment.reading || !nextCursor}
                onClick={() =>
                  nextCursor &&
                  setCursorStack((stack) => [...stack, nextCursor])
                }
              >
                {messages.routingHealth.nextPage}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

/** The backend's pricing enum never reaches the screen unlabelled. */
function pricingStatusLabel(
  status: string,
  copy: ReturnType<typeof useLocale>["messages"]["observe"],
): string {
  switch (status) {
    case "priced":
      return copy.pricingPriced;
    case "unpriced":
      return copy.pricingUnpriced;
    case "ineligible":
      return copy.pricingIneligible;
    default:
      return copy.pricingUnknown;
  }
}

function pricingStatusIntent(status: string) {
  switch (status) {
    case "priced":
      return "healthy" as const;
    case "unpriced":
      return "degraded" as const;
    case "ineligible":
      return "idle" as const;
    default:
      return "failing" as const;
  }
}

function ActivityRow({
  item,
  onOpenRequest,
}: {
  item: ObserveActivityItem;
  onOpenRequest: (item: ObserveActivityItem) => void;
}) {
  const { formatNumber, messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.observe;

  const tier: OperatorStatusTier =
    item.final_result === "completed"
      ? "healthy"
      : item.final_result === "client_disconnected"
        ? "degraded"
        : "failing";
  const resultText =
    item.final_result === "completed"
      ? `${item.status_code}`
      : item.final_result === "client_disconnected"
        ? copy.clientDisconnected
        : `${item.status_code} · ${item.outcome_detail === "stream_error" ? copy.streamFailures : copy.httpFailedShort}`;

  return (
    <TableRow
      data-testid="activity-row"
      className={cn("group/row", operationalRowStripe(tier))}
    >
      <TableCell className="whitespace-nowrap font-mono tabular-nums">
        {format(item.created_at)}
      </TableCell>
      <TableCell>
        <div className="flex min-w-48 flex-col gap-0.5">
          <span
            className="truncate text-xs"
            data-testid={item.route_changed ? "route-changed" : undefined}
          >
            <span className="text-muted-foreground">
              {copy.entryModelShort}
            </span>{" "}
            {item.ingress_model_label || item.ingress_model_id}
          </span>
          <span className="truncate text-xs">
            <span aria-hidden="true" className="text-muted-foreground">
              →
            </span>{" "}
            <span className="text-muted-foreground">
              {copy.finalModelShort}
            </span>{" "}
            {item.final_target_model_label ?? item.final_target_model_id ?? (
              <OperatorMissingValue reason={copy.finalTargetEvidenceMissing} />
            )}
          </span>
          {!item.routing_evidence_complete ? (
            <OperatorClippedBadge
              label={copy.routingEvidencePartial}
              reason={copy.routingEvidencePartialReason}
            />
          ) : null}
        </div>
      </TableCell>
      <TableCell>
        <OperatorStatusBadge intent={tier} label={resultText} preserveLabel />
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {formatNumber(item.attempt_count)}
      </TableCell>
      <TableCell>
        <div className="flex min-w-40 flex-col gap-0.5 text-xs">
          <span>
            {item.terminal_target_id === null ? (
              <OperatorMissingValue reason={copy.noTerminalTargetEvidence} />
            ) : (
              copy.terminalTargetId(item.terminal_target_id)
            )}
          </span>
          <span className="truncate text-muted-foreground">
            {item.endpoint_label || (
              <OperatorMissingValue reason={copy.noEndpointEvidence} />
            )}
          </span>
        </div>
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {item.ttft_ms === null ? (
          <OperatorMissingValue reason={messages.honesty.noValue} />
        ) : (
          `${formatNumber(item.ttft_ms)} ms`
        )}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {item.total_tokens === null || item.total_tokens === undefined ? (
          <OperatorMissingValue reason={messages.honesty.noValue} />
        ) : (
          formatNumber(item.total_tokens)
        )}
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {item.known_cost_micros === null ? (
          <OperatorMissingValue reason={messages.honesty.noValue} />
        ) : (
          `${item.report_currency_symbol ?? "$"}${(Number(item.known_cost_micros) / 1_000_000).toFixed(4)}`
        )}
      </TableCell>
      <TableCell>
        <OperatorTypeBadge
          intent={pricingStatusIntent(item.final_pricing_status)}
          label={pricingStatusLabel(item.final_pricing_status, copy)}
          preserveLabel
        />
      </TableCell>
      <TableCell className="text-right">
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => onOpenRequest(item)}
          data-testid="activity-open-request"
          className="h-7 text-xs opacity-0 transition-opacity focus-visible:opacity-100 group-hover/row:opacity-100"
        >
          {copy.viewRequest}
        </Button>
      </TableCell>
    </TableRow>
  );
}
