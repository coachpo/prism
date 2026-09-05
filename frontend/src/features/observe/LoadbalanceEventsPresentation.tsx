import { Link } from "@tanstack/react-router";
import { Loader2, RefreshCw, SearchX } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import {
  OperatorCallout,
  OperatorClippedBadge,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorHelpHint,
  OperatorSectionCard,
  OperatorStalenessBadge,
} from "@/shared/design-system";
import {
  OperationalTableSkeletonRows,
  SortableTableHead,
} from "@/shared/table/operationalTable";
import { PaginationLiveStatus } from "@/shared/table/paginationControls";
import {
  keepsCommittedRows,
  shouldShowPendingRows,
} from "@/shared/table/paginationStates";
import { LoadbalanceEventDetailSheet } from "./LoadbalanceEventDetailSheet";
import { LoadbalanceEventRow } from "./LoadbalanceEventRow";
import type { RoutingHealthSearch } from "./routingHealthSearch";
import type { RoutingHealthContextState } from "./useRoutingHealthQueryContext";
import {
  EVENTS_PAGE_SIZE_OPTIONS,
  type useRoutingHealthEventPage,
} from "./useRoutingHealthEventPage";

type EventPageState = ReturnType<typeof useRoutingHealthEventPage>;

export function LoadbalanceEventsPresentation({
  contextState,
  eventPage,
  preset,
  search,
}: {
  contextState: RoutingHealthContextState;
  eventPage: EventPageState;
  preset: string;
  search: RoutingHealthSearch;
}) {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.routingHealth;
  const tableCopy = messages.operationalTable;
  const {
    admissionReasons,
    closeEventDetail,
    eventCursor,
    eventCursorStack,
    eventModelId,
    eventTypes,
    failureKinds,
    fragment,
    goNextPage,
    goPreviousPage,
    openEventDetail,
    pageSize,
    refresh,
    retryRead,
    selectedEventId,
    setPageSize,
    sortOrder,
    updateSearch,
    retryQueryContext,
  } = eventPage;
  const items = fragment.data?.items ?? [];
  const showPendingRows = shouldShowPendingRows(fragment);
  const showCommittedRows = keepsCommittedRows(fragment) && items.length > 0;
  const showTableShell = showPendingRows || showCommittedRows;
  const knownPageNumber = eventCursor ? eventCursorStack.length + 1 : 1;
  const deepLinkedPage = Boolean(eventCursor) && eventCursorStack.length === 0;
  const liveMessage = !fragment.reading
    ? null
    : fragment.data === null
      ? tableCopy.loadingFirstPage
      : deepLinkedPage
        ? tableCopy.loadingTargetPage
        : tableCopy.loadingPage(knownPageNumber);
  const replaceFailureVisible =
    !fragment.reading &&
    fragment.error !== null &&
    fragment.data !== null &&
    !fragment.stale;
  // 游标分页拿不到总数，页脚只报本页区间和还有没有下一页。深链直接落在某个
  // 游标上时连偏移量都不知道，那就只说本页几条，不去推算一个假的起始序号。
  const pageRangeStart = eventCursorStack.length * pageSize + 1;
  const pageRangeLabel =
    items.length === 0
      ? null
      : deepLinkedPage
        ? copy.pageRangeUnknown(formatNumber(items.length))
        : copy.pageRange(
            formatNumber(pageRangeStart),
            formatNumber(pageRangeStart + items.length - 1),
          );

  return (
    <OperatorSectionCard
      data-testid="routing-health-events"
      title={copy.eventsTitle}
      description={`${copy.eventsDescription} ${copy.eventsWindowIndependentNote}`}
      contentClassName="flex flex-col gap-4"
      actions={
        <div className="flex items-center gap-2">
          <PaginationLiveStatus message={liveMessage} />
          {fragment.stale && fragment.lastSuccessfulAt ? (
            <OperatorStalenessBadge
              label={messages.honesty.lastSuccessful(
                formatTime(fragment.lastSuccessfulAt),
              )}
              reason={fragment.error ?? undefined}
            />
          ) : null}
          <Select
            value={preset}
            onValueChange={(value) =>
              updateSearch({
                preset: value,
                from_time: undefined,
                to_time: undefined,
                event_cursor: undefined,
                event_id: undefined,
              })
            }
          >
            <SelectTrigger className="w-36" aria-label={copy.timeRangeLabel}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="1h">{copy.range1h}</SelectItem>
                <SelectItem value="6h">{copy.range6h}</SelectItem>
                <SelectItem value="24h">{copy.range24h}</SelectItem>
                <SelectItem value="7d">{copy.range7d}</SelectItem>
                <SelectItem value="30d">{copy.range30d}</SelectItem>
                <SelectItem value="all">{copy.rangeAll}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={refresh}
            disabled={fragment.reading || contextState.phase !== "ready"}
            aria-label={copy.refresh}
          >
            {fragment.reading ? (
              <Loader2 data-icon="inline-start" className="animate-spin" />
            ) : (
              <RefreshCw data-icon="inline-start" />
            )}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={eventTypes.length === 1 ? eventTypes[0] : "all"}
            onValueChange={(value) =>
              updateSearch({
                event_type: value === "all" ? undefined : [value],
                event_cursor: undefined,
              })
            }
          >
            <SelectTrigger
              className="w-44"
              aria-label={copy.eventTypeFilterLabel}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.eventTypeFilterAll}</SelectItem>
                <SelectItem value="retry_scheduled">
                  {copy.eventSummary.retryScheduled}
                </SelectItem>
                <SelectItem value="retry_exhausted">
                  {copy.eventSummary.retryExhausted}
                </SelectItem>
                <SelectItem value="banned">
                  {copy.eventSummary.banned}
                </SelectItem>
                <SelectItem value="unbanned">
                  {copy.eventSummary.unbanned}
                </SelectItem>
                <SelectItem value="recovered">
                  {copy.eventSummary.recovered}
                </SelectItem>
                <SelectItem value="admission_rejected">
                  {copy.eventSummary.admissionRejected}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            value={
              failureKinds && failureKinds.length === 1
                ? failureKinds[0]
                : "all"
            }
            onValueChange={(value) =>
              updateSearch({
                event_failure_kind: value === "all" ? undefined : [value],
                event_cursor: undefined,
              })
            }
          >
            <SelectTrigger
              className="w-40"
              aria-label={copy.failureKindFilterLabel}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.eventTypeFilterAll}</SelectItem>
                <SelectItem value="transient_http">
                  {copy.eventSummary.failureTransientHttp}
                </SelectItem>
                <SelectItem value="connect_error">
                  {copy.eventSummary.failureConnectError}
                </SelectItem>
                <SelectItem value="timeout">
                  {copy.eventSummary.failureTimeout}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            value={
              admissionReasons && admissionReasons.length === 1
                ? admissionReasons[0]
                : "all"
            }
            onValueChange={(value) =>
              updateSearch({
                event_admission_reason: value === "all" ? undefined : [value],
                event_cursor: undefined,
              })
            }
          >
            <SelectTrigger
              className="w-44"
              aria-label={copy.admissionFilterLabel}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.eventTypeFilterAll}</SelectItem>
                <SelectItem value="qps_limit">
                  {copy.eventSummary.admissionQpsLimit}
                </SelectItem>
                <SelectItem value="max_in_flight_stream">
                  {copy.eventSummary.admissionMaxInFlightStream}
                </SelectItem>
                <SelectItem value="max_in_flight_non_stream">
                  {copy.eventSummary.admissionMaxInFlightNonStream}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Input
            className="w-44"
            placeholder={copy.modelFilterSubmitPlaceholder}
            defaultValue={eventModelId ?? ""}
            aria-label={copy.modelFilterLabel}
            onBlur={(event) =>
              updateSearch({
                event_model_id: event.target.value.trim() || undefined,
                event_cursor: undefined,
              })
            }
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                updateSearch({
                  event_model_id: event.currentTarget.value.trim() || undefined,
                  event_cursor: undefined,
                });
              }
            }}
          />
        </div>
      </div>

      {contextState.phase === "error" ? (
        <OperatorErrorState
          testId="events-context-error"
          title={copy.loadFailed}
          description={messages.honesty.readFailedDescription}
          details={contextState.error}
          detailsLabel={messages.honesty.viewDetails}
          action={
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={retryQueryContext}
            >
              <RefreshCw data-icon="inline-start" />
              {copy.retry}
            </Button>
          }
        />
      ) : null}

      {!fragment.reading &&
      fragment.error !== null &&
      fragment.data === null ? (
        <OperatorErrorState
          testId="events-load-error"
          title={copy.loadFailed}
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
              <RefreshCw data-icon="inline-start" />
              {copy.retry}
            </Button>
          }
        />
      ) : null}

      {replaceFailureVisible ? (
        <OperatorErrorState
          testId="events-page-error"
          title={
            deepLinkedPage
              ? copy.loadFailed
              : tableCopy.pageLoadFailed(knownPageNumber)
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
              <RefreshCw data-icon="inline-start" />
              {copy.retry}
            </Button>
          }
        />
      ) : null}

      {fragment.phase === "empty" && !fragment.reading ? (
        <OperatorEmptyState
          icon={<SearchX />}
          title={copy.eventsEmptyTitle}
          description={copy.eventsEmptyDescription}
        />
      ) : null}

      {fragment.phase === "partial" && !fragment.reading ? (
        <OperatorClippedBadge
          label={copy.coverageIncompleteTitle}
          reason={copy.coverageIncompleteDescription}
          className="self-start"
        />
      ) : null}

      {keepsCommittedRows(fragment) &&
      fragment.data?.coverage.complete === false &&
      fragment.data.coverage.gaps.length > 0 ? (
        <OperatorCallout intent="muted" title={copy.sourceCoverageTitle}>
          <p>{copy.sourceCoverageDescription}</p>
          <Link
            className="mt-2 inline-flex text-sm font-medium text-primary underline-offset-4 hover:underline"
            to="/system/settings?scope=instance&section=retention"
          >
            {copy.retentionCoverageLink}
          </Link>
        </OperatorCallout>
      ) : null}

      {showTableShell ? (
        <div aria-busy={fragment.reading}>
          {/* sticky 表头只对最近的滚动祖先生效：高度上限必须落在表格自己的
              滚动容器上，横竖两轴同源，否则滚过十行就没有列义了。 */}
          <Table
            aria-label={copy.eventsTitle}
            scrollAreaClassName="max-h-[calc(100dvh-22rem)]"
          >
            <TableHeader>
              <TableRow>
                {/* 排序开关原来在筛选行最右，离它重新排序的那一列约 800px，
                    列头上既没有 aria-sort 也没有方向字形。 */}
                <SortableTableHead
                  className="sticky left-0 z-20 bg-inset shadow-[inset_-1px_0_0_0_var(--color-border)]"
                  sortKey="created_at"
                  sort={{ column: "created_at", direction: sortOrder }}
                  onSort={() =>
                    updateSearch({
                      event_sort_order: sortOrder === "desc" ? "asc" : "desc",
                      event_cursor: undefined,
                      event_id: undefined,
                    })
                  }
                >
                  {copy.timeColumn}
                </SortableTableHead>
                <TableHead>{copy.eventColumn}</TableHead>
                <TableHead>{copy.modelColumn}</TableHead>
                <TableHead>{copy.targetColumn}</TableHead>
                <TableHead>
                  <span className="inline-flex items-center gap-1">
                    {copy.windowColumn}
                    <OperatorHelpHint label={copy.windowColumnHint} />
                  </span>
                </TableHead>
                <TableHead className="sticky right-0 z-20 bg-inset text-right shadow-[inset_1px_0_0_0_var(--color-border)]">
                  {copy.actionsColumn}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {showPendingRows ? (
                <OperationalTableSkeletonRows columns={6} rows={5} />
              ) : null}
              {showCommittedRows
                ? items.map((item) => (
                    <LoadbalanceEventRow
                      key={item.event_id}
                      item={item}
                      formatTime={formatTime}
                      copy={copy}
                      onOpenDetail={() => openEventDetail(item.event_id)}
                    />
                  ))
                : null}
            </TableBody>
          </Table>
        </div>
      ) : null}

      {/* 只有一页时也要出页脚：那一句区间就是这张表的「共 N 条」，
          否则「25 条是全部还是第一页」只能靠点下一页去试。 */}
      {showCommittedRows ||
      eventCursorStack.length > 0 ||
      fragment.data?.has_more ||
      fragment.reading ? (
        <div className="flex flex-col gap-2 border-t border-border pt-2 sm:flex-row sm:items-center sm:justify-between">
          <span className="text-xs text-muted-foreground">
            {pageRangeLabel
              ? `${pageRangeLabel} · ${fragment.data?.has_more ? copy.pageHasMore : copy.pageAtEnd}`
              : null}
          </span>
          <div className="flex items-center gap-2">
            <Select
              value={String(pageSize)}
              onValueChange={(value) => setPageSize(Number(value))}
            >
              <SelectTrigger
                size="sm"
                aria-label={tableCopy.pageSize}
                className="w-20 text-xs"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {EVENTS_PAGE_SIZE_OPTIONS.map((option) => (
                    <SelectItem key={option} value={String(option)}>
                      {formatNumber(option)}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={fragment.reading || eventCursorStack.length === 0}
              onClick={goPreviousPage}
            >
              {copy.previousPage}
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={
                fragment.reading ||
                !fragment.data?.has_more ||
                !fragment.data?.next_cursor
              }
              onClick={goNextPage}
            >
              {copy.nextPage}
            </Button>
          </div>
        </div>
      ) : null}

      <LoadbalanceEventDetailSheet
        eventId={selectedEventId}
        queryContext={contextState.context?.query_context ?? null}
        sourceSearch={search}
        onClose={closeEventDetail}
        onRetryContext={() => void eventPage.retryContext()}
      />
    </OperatorSectionCard>
  );
}
