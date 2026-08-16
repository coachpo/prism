import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { observe, type ObserveActivityItem, type ObserveActivityResponse } from "@/lib/api/observability";
import { fragmentErrorFrom, type FragmentState } from "@/features/observe/useObserveFragments";
import { cn } from "@/lib/utils";
import {
  OperatorEmptyState,
  OperatorErrorState,
  OperatorMissingValue,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
  type OperatorStatusTier,
} from "@/shared/design-system";
import { operationalRowStripe } from "@/shared/table/operationalTable";
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
 */
export function ObserveActivityTable({ queryContext }: { queryContext: string | null }) {
  const { formatNumber, messages } = useLocale();
  const navigate = useNavigate();
  const [fragment, setFragment] = useState<FragmentState<ObserveActivityResponse>>({
    phase: "loading",
    data: null,
    stale: false,
    error: null,
    retryAfterMs: null,
  });
  const [cursorStack, setCursorStack] = useState<string[]>([]);

  const before = cursorStack.at(-1);

  useEffect(() => {
    if (!queryContext) return;
    let cancelled = false;
    void observe
      .observeActivity(queryContext, { limit: ACTIVITY_PAGE_SIZE, before })
      .then((data) => {
        if (!cancelled) setFragment({ phase: "ready", data, stale: false, error: null, retryAfterMs: null });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        const mapped = fragmentErrorFrom(err);
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
  }, [before, queryContext]);

  const openRequests = useCallback(
    (item: ObserveActivityItem) => {
      void navigate({
        to: "/observe/requests",
        search: {
          view: "ingress_chains",
          query_context: queryContext ?? "",
          final_ingress_request_id: item.final_ingress_request_id,
        },
      });
    },
    [navigate, queryContext],
  );

  // The card content is px-0 so the table can reach the card edge; every
  // non-table block carries the gutter itself.
  if (!queryContext || (fragment.phase === "loading" && fragment.data === null)) {
    return <Skeleton className="mx-[var(--density-card-pad-x)] h-48 rounded-md" aria-busy="true" />;
  }

  if (fragment.data === null) {
    return (
      <div className="flex flex-col gap-2 px-[var(--density-card-pad-x)]">
        {fragment.retryAfterMs !== null ? <RetryAfterCallout retryAfterMs={fragment.retryAfterMs} /> : null}
        <OperatorErrorState
          testId="activity-load-error"
          title={messages.observe.windowUnavailable}
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
        />
      </div>
    );
  }

  const items = fragment.data.items;
  // The endpoint pages with a `before` usage-event cursor and reports `has_more`; it
  // returns no total, so the footer states the page range rather than
  // inventing a count.
  const nextCursor = nextObserveActivityCursor(items, fragment.data.has_more);

  return (
    <div className="flex flex-col gap-2">
      {fragment.stale ? (
        <OperatorStalenessBadge
          className="mx-[var(--density-card-pad-x)] self-start"
          label={messages.observe.staleDataNote}
          reason={fragment.error ?? undefined}
        />
      ) : null}

      {items.length === 0 ? (
        <OperatorEmptyState
          className="mx-[var(--density-card-pad-x)]"
          title={messages.observe.noData}
          description={messages.observe.adjustFiltersHint}
        />
      ) : (
        // The card border is this table's border; no second ring around it.
        <div data-testid="observe-activity-table">
          <div className="overflow-x-auto">
            <Table aria-label={messages.observe.activityTitle}>
              <TableHeader>
                <TableRow>
                  <TableHead>{messages.observe.time}</TableHead>
                  <TableHead>{messages.observe.requestedModel}</TableHead>
                  <TableHead>{messages.observe.result}</TableHead>
                  <TableHead>{messages.observe.endpoint}</TableHead>
                  <TableHead className="text-right">TTFT</TableHead>
                  <TableHead className="text-right">{messages.observe.tokens}</TableHead>
                  <TableHead className="text-right">{messages.observe.cost}</TableHead>
                  <TableHead>{messages.observe.pricingStatus}</TableHead>
                  <TableHead className="text-right">{messages.routingHealth.actionsColumn}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <ActivityRow key={item.usage_event_id} item={item} onOpenRequest={openRequests} />
                ))}
              </TableBody>
            </Table>
          </div>
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
                disabled={cursorStack.length === 0}
                onClick={() => setCursorStack((stack) => stack.slice(0, -1))}
              >
                {messages.routingHealth.previousPage}
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={!nextCursor}
                onClick={() => nextCursor && setCursorStack((stack) => [...stack, nextCursor])}
              >
                {messages.routingHealth.nextPage}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/** The backend's pricing enum never reaches the screen unlabelled. */
function pricingStatusLabel(status: string, copy: ReturnType<typeof useLocale>["messages"]["observe"]): string {
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
    item.final_result === "completed" ? "healthy" : item.final_result === "client_disconnected" ? "degraded" : "failing";
  const resultText =
    item.final_result === "completed"
      ? `${item.status_code}`
      : item.final_result === "client_disconnected"
        ? copy.clientDisconnected
        : `${item.status_code} · ${item.outcome_detail === "stream_error" ? copy.streamFailures : copy.httpFailedShort}`;

  return (
    <TableRow data-testid="activity-row" className={cn("group/row", operationalRowStripe(tier))}>
      <TableCell className="whitespace-nowrap font-mono tabular-nums">{format(item.created_at)}</TableCell>
      <TableCell>
        <div>{item.model_label}</div>
        {item.route_changed ? (
          <div className="text-xs text-muted-foreground" data-testid="route-changed">
            {copy.routeChanged} →{" "}
            {item.resolved_target_model_label ?? <OperatorMissingValue reason={messages.honesty.noValue} />}
          </div>
        ) : null}
      </TableCell>
      <TableCell>
        <OperatorStatusBadge intent={tier} label={resultText} preserveLabel />
      </TableCell>
      <TableCell>{item.endpoint_label}</TableCell>
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
