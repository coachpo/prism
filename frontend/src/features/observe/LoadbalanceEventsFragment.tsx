import { useCallback, useEffect, useRef, useState } from "react"
import { Link } from "@tanstack/react-router"
import { ArrowDown, ArrowUp, Loader2, RefreshCw, SearchX } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/i18n/useLocale"
import { useTimezone } from "@/hooks/useTimezone"
import { api } from "@/lib/api"
import type { EventsQueryContextPreset, ListEventsParams } from "@/lib/api/observability"
import type { EventsQueryContextResponse, LoadbalanceEventListItem, LoadbalanceEventListResponse, LoadbalanceEventType } from "@/lib/types"
import {
  OperatorCallout,
  OperatorClippedBadge,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorMissingValue,
  OperatorSectionCard,
  OperatorStalenessBadge,
  OperatorTypeBadge,
} from "@/shared/design-system"
import { OperationalTableSkeletonRows } from "@/shared/table/operationalTable"
import { LoadbalanceEventDetailSheet } from "./LoadbalanceEventDetailSheet"
import { renderEventSummary } from "./eventSummary"
import type { FragmentState } from "./RoutingHealthTab"

const EVENTS_PAGE_SIZE = 25

type RoutingHealthSearch = Record<string, unknown>

export function LoadbalanceEventsFragment({
  search,
  onSearchChange,
}: {
  search: RoutingHealthSearch
  onSearchChange: (patch: RoutingHealthSearch, replace?: boolean) => void
}) {
  const { messages } = useLocale()
  const { format: formatTime } = useTimezone()
  const copy = messages.routingHealth
  const [contextState, setContextState] = useState<{
    phase: "idle" | "loading" | "ready" | "error"
    context: EventsQueryContextResponse | null
    error: string | null
  }>({ phase: "idle", context: null, error: null })
  const [fragment, setFragment] = useState<FragmentState<LoadbalanceEventListResponse>>(() => ({
    phase: "idle", data: null, stale: false, lastSuccessfulAt: null, error: null, semanticQueryKey: "observe:events",
  }))
  const [selectedEventId, setSelectedEventId] = useState<string | null>(null)
  // Cursor pagination is forward-only on the wire, so visited cursors are kept
  // here to give a real previous page rather than a "load more" that only ever
  // moves forward.
  const [eventCursorStack, setEventCursorStack] = useState<string[]>([])
  const generation = useRef(0)

  const preset = (search.preset as EventsQueryContextPreset) || "24h"
  const fromTime = (search.from_time as string) || undefined
  const toTime = (search.to_time as string) || undefined
  const eventTypes = normalizeSearchArray(search.event_type) as LoadbalanceEventType[]
  const failureKinds = normalizeSearchArray(search.event_failure_kind) as ListEventsParams["failure_kind"]
  const admissionReasons = normalizeSearchArray(search.event_admission_reason) as ListEventsParams["admission_reason"]
  const eventModelId = (search.event_model_id as string) || undefined
  const eventEndpointId = (search.event_endpoint_id as string) || undefined
  const eventTargetId = (search.event_terminal_target_id as string) || undefined
  const sortOrder = (search.event_sort_order as "desc" | "asc") || "desc"
  const eventCursor = (search.event_cursor as string) || undefined
  const urlEventId = typeof search.event_id === "string" && search.event_id !== "" ? search.event_id : null

  // Direct/share/refresh/back-forward with event_id restores the selection-only
  // detail sheet; removing the param closes it without touching the list cohort.
  useEffect(() => {
    setSelectedEventId(urlEventId)
  }, [urlEventId])

  const windowKey = JSON.stringify({ preset, fromTime, toTime })

  // Issue/refresh the signed query context for the current window.
  const issueContext = useCallback(async () => {
    setContextState((current) => ({ ...current, phase: "loading" }))
    try {
      const response = await api.loadbalance.issueEventsQueryContext(
        preset === "custom"
          ? { requested_preset: "custom", custom_from_time: fromTime, custom_to_time: toTime }
          : { requested_preset: preset },
      )
      setContextState({ phase: "ready", context: response, error: null })
      return response
    } catch (error) {
      setContextState({ phase: "error", context: null, error: error instanceof Error ? error.message : copy.loadFailed })
      return null
    }
  }, [copy.loadFailed, fromTime, preset, toTime])

  const loadEvents = useCallback(async (context: EventsQueryContextResponse, cursorOverride?: string) => {
    const current = ++generation.current
    setFragment((fragment) => ({ ...fragment, phase: fragment.data === null ? "loading" : fragment.phase, stale: fragment.data !== null }))
    try {
      const params: ListEventsParams = {
        query_context: context.query_context,
        sort_order: sortOrder,
        limit: EVENTS_PAGE_SIZE,
        event_type: eventTypes.length > 0 ? eventTypes : undefined,
        failure_kind: failureKinds && failureKinds.length > 0 ? failureKinds : undefined,
        admission_reason: admissionReasons && admissionReasons.length > 0 ? admissionReasons : undefined,
        model_id: eventModelId,
        endpoint_id: eventEndpointId ? Number(eventEndpointId) : undefined,
        terminal_target_id: eventTargetId ? Number(eventTargetId) : undefined,
        cursor: cursorOverride ?? eventCursor,
      }
      const response = await api.loadbalance.listEvents(params)
      if (current !== generation.current) return
      let phase: FragmentState<LoadbalanceEventListResponse>["phase"] = "ready"
      if (response.items.length === 0) {
        phase = response.coverage.complete ? "empty" : "partial"
      }
      setFragment({ phase, data: response, stale: false, lastSuccessfulAt: new Date().toISOString(), error: null, semanticQueryKey: JSON.stringify(params) })
    } catch (error) {
      if (current !== generation.current) return
      setFragment((fragment) => ({ ...fragment, phase: "error", stale: fragment.data !== null, error: error instanceof Error ? error.message : copy.loadFailed }))
    }
  }, [admissionReasons, eventCursor, eventEndpointId, eventModelId, eventTargetId, eventTypes, failureKinds, copy.loadFailed, sortOrder])

  // Rebuild the context when the window changes; then load the first page.
  useEffect(() => {
    let cancelled = false
    setContextState((current) => ({ ...current, phase: "loading" }))
    const run = async () => {
      const context = await issueContext()
      if (cancelled || !context) return
      setFragment({ phase: "loading", data: null, stale: false, lastSuccessfulAt: null, error: null, semanticQueryKey: "observe:events" })
      onSearchChange({ event_cursor: undefined })
      await loadEvents(context)
    }
    void run()
    return () => { cancelled = true }
    // windowKey drives context rebuilds; the other filters only reload pages.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [windowKey])

  // Filter/sort/direction changes reload from the first page with the current
  // (already valid) context.
  useEffect(() => {
    if (contextState.phase !== "ready" || !contextState.context) return
    void loadEvents(contextState.context)
    onSearchChange({ event_cursor: undefined })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify({ eventTypes, failureKinds, admissionReasons, eventModelId, eventEndpointId, eventTargetId, sortOrder })])

  // Any filter change resets paging: a cursor issued under a different filter
  // set does not address the same rows.
  const updateSearch = useCallback((patch: RoutingHealthSearch, replace = true) => {
    setEventCursorStack([])
    onSearchChange(patch, replace)
  }, [onSearchChange])

  const items = fragment.data?.items ?? []
  return (
    <OperatorSectionCard
      data-testid="routing-health-events"
      title={copy.eventsTitle}
      description={`${copy.eventsDescription} ${copy.eventsWindowIndependentNote}`}
      contentClassName="flex flex-col gap-4"
      actions={
        <div className="flex items-center gap-2">
          {fragment.stale && fragment.lastSuccessfulAt ? (
            <OperatorStalenessBadge
              label={messages.honesty.lastSuccessful(formatTime(fragment.lastSuccessfulAt))}
              reason={fragment.error ?? undefined}
            />
          ) : null}
          <Select
            value={preset}
            onValueChange={(value) => updateSearch({ preset: value, from_time: undefined, to_time: undefined, event_cursor: undefined, event_id: undefined })}
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
            onClick={() => { if (contextState.context) void loadEvents(contextState.context) }}
            disabled={fragment.phase === "loading" || contextState.phase !== "ready"}
            aria-label={copy.refresh}
          >
            {fragment.phase === "loading" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <RefreshCw data-icon="inline-start" />}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={eventTypes.length === 1 ? eventTypes[0] : "all"}
            onValueChange={(value) => updateSearch({ event_type: value === "all" ? undefined : [value], event_cursor: undefined })}
          >
            <SelectTrigger className="w-44" aria-label={copy.eventTypeFilterLabel}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.eventTypeFilterAll}</SelectItem>
                <SelectItem value="retry_scheduled">{copy.eventSummary.retryScheduled}</SelectItem>
                <SelectItem value="retry_exhausted">{copy.eventSummary.retryExhausted}</SelectItem>
                <SelectItem value="banned">{copy.eventSummary.banned}</SelectItem>
                <SelectItem value="unbanned">{copy.eventSummary.unbanned}</SelectItem>
                <SelectItem value="recovered">{copy.eventSummary.recovered}</SelectItem>
                <SelectItem value="admission_rejected">{copy.eventSummary.admissionRejected}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            value={failureKinds && failureKinds.length === 1 ? failureKinds[0] : "all"}
            onValueChange={(value) => updateSearch({ event_failure_kind: value === "all" ? undefined : [value], event_cursor: undefined })}
          >
            <SelectTrigger className="w-40" aria-label={copy.failureKindFilterLabel}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.eventTypeFilterAll}</SelectItem>
                <SelectItem value="transient_http">{copy.eventSummary.failureTransientHttp}</SelectItem>
                <SelectItem value="connect_error">{copy.eventSummary.failureConnectError}</SelectItem>
                <SelectItem value="timeout">{copy.eventSummary.failureTimeout}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Select
            value={admissionReasons && admissionReasons.length === 1 ? admissionReasons[0] : "all"}
            onValueChange={(value) => updateSearch({ event_admission_reason: value === "all" ? undefined : [value], event_cursor: undefined })}
          >
            <SelectTrigger className="w-44" aria-label={copy.admissionFilterLabel}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="all">{copy.eventTypeFilterAll}</SelectItem>
                <SelectItem value="qps_limit">{copy.eventSummary.admissionQpsLimit}</SelectItem>
                <SelectItem value="max_in_flight_stream">{copy.eventSummary.admissionMaxInFlightStream}</SelectItem>
                <SelectItem value="max_in_flight_non_stream">{copy.eventSummary.admissionMaxInFlightNonStream}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
          <Input
            className="w-44"
            placeholder={copy.modelFilterSubmitPlaceholder}
            defaultValue={eventModelId ?? ""}
            aria-label={copy.modelFilterLabel}
            onBlur={(event) => updateSearch({ event_model_id: event.target.value.trim() || undefined, event_cursor: undefined })}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                updateSearch({ event_model_id: event.currentTarget.value.trim() || undefined, event_cursor: undefined })
              }
            }}
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => updateSearch({
              event_sort_order: sortOrder === "desc" ? "asc" : "desc",
              event_cursor: undefined,
              event_id: undefined,
            })}
            aria-label={copy.sortToggleLabel}
          >
            {sortOrder === "desc" ? <ArrowDown data-icon="inline-start" /> : <ArrowUp data-icon="inline-start" />}
            {sortOrder === "desc" ? copy.sortNewestFirst : copy.sortOldestFirst}
          </Button>
        </div>
      </div>

      {contextState.phase === "error" ? (
        <OperatorErrorState
          testId="events-context-error"
          title={copy.loadFailed}
          description={messages.honesty.readFailedDescription}
          details={contextState.error}
          detailsLabel={messages.honesty.viewDetails}
          action={<Button type="button" variant="outline" size="sm" onClick={() => void issueContext()}><RefreshCw data-icon="inline-start" />{copy.retry}</Button>}
        />
      ) : null}

      {fragment.phase === "error" && fragment.data === null ? (
        <OperatorErrorState
          testId="events-load-error"
          title={copy.loadFailed}
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
          action={<Button type="button" variant="outline" size="sm" onClick={() => contextState.context && void loadEvents(contextState.context)}><RefreshCw data-icon="inline-start" />{copy.retry}</Button>}
        />
      ) : null}

      {fragment.phase === "empty" ? (
        <OperatorEmptyState icon={<SearchX />} title={copy.eventsEmptyTitle} description={copy.eventsEmptyDescription} />
      ) : null}

      {fragment.phase === "partial" ? (
        <OperatorClippedBadge label={copy.coverageIncompleteTitle} reason={copy.coverageIncompleteDescription} className="self-start" />
      ) : null}

      {fragment.data?.coverage.complete === false && fragment.data.coverage.gaps.length > 0 ? (
        <OperatorCallout intent="muted" title={copy.sourceCoverageTitle}>
          <p>{copy.sourceCoverageDescription}</p>
          <Link className="mt-2 inline-flex text-sm font-medium text-primary underline-offset-4 hover:underline" to="/system/settings?scope=instance&section=retention">
            {copy.retentionCoverageLink}
          </Link>
        </OperatorCallout>
      ) : null}

      {items.length > 0 || fragment.phase === "loading" || contextState.phase === "loading" ? (
        <div className="overflow-hidden rounded-md border border-border">
          <div className="overflow-x-auto">
            <Table aria-label={copy.eventsTitle}>
              <TableHeader>
                <TableRow>
                  <TableHead>{copy.timeColumn}</TableHead>
                  <TableHead>{copy.eventColumn}</TableHead>
                  <TableHead>{copy.modelColumn}</TableHead>
                  <TableHead>{copy.targetColumn}</TableHead>
                  <TableHead>{copy.windowColumn}</TableHead>
                  <TableHead className="text-right">{copy.actionsColumn}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? <OperationalTableSkeletonRows columns={6} rows={5} /> : null}
                {items.map((item) => (
                  <EventRow
                    key={item.event_id}
                    item={item}
                    formatTime={formatTime}
                    copy={copy}
                    onOpenDetail={() => { setSelectedEventId(item.event_id); updateSearch({ event_id: item.event_id }, false) }}
                  />
                ))}
              </TableBody>
            </Table>
          </div>
        </div>
      ) : null}

      {eventCursorStack.length > 0 || fragment.data?.has_more ? (
        <div className="flex items-center justify-end gap-1">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={eventCursorStack.length === 0}
            onClick={() => {
              const nextStack = eventCursorStack.slice(0, -1)
              setEventCursorStack(nextStack)
              onSearchChange({ event_cursor: nextStack.at(-1), event_id: undefined })
            }}
          >
            {copy.previousPage}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={!fragment.data?.has_more || !fragment.data?.next_cursor}
            onClick={() => {
              const next = fragment.data?.next_cursor
              if (!next) return
              setEventCursorStack((stack) => [...stack, next])
              onSearchChange({ event_cursor: next, event_id: undefined })
            }}
          >
            {copy.nextPage}
          </Button>
        </div>
      ) : null}

      <LoadbalanceEventDetailSheet
        eventId={selectedEventId}
        queryContext={contextState.context?.query_context ?? null}
        sourceSearch={search}
        onClose={() => { setSelectedEventId(null); updateSearch({ event_id: undefined }) }}
        onRetryContext={() => { void issueContext().then((context) => { if (context) void loadEvents(context) }) }}
      />
    </OperatorSectionCard>
  )
}

function EventRow({ item, formatTime, copy, onOpenDetail }: {
  item: LoadbalanceEventListItem
  formatTime: (value: string, options?: Intl.DateTimeFormatOptions) => string
  copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]
  onOpenDetail: () => void
}) {
  const summary = renderEventSummary(item.summary, copy.eventSummary)
  const modelLabel = item.model.label || item.model.model_id || null
  const targetLabel = item.terminal_target.label || `#${item.terminal_target.id ?? "?"}`
  const endpointLabel = item.endpoint.label || (item.endpoint.id != null ? `#${item.endpoint.id}` : null)
  const windowTimeValue = item.event_type === "banned" && item.banned_until_at
    ? item.banned_until_at
    : item.next_retry_at ?? item.last_success_at ?? null
  return (
    <TableRow data-testid={`event-row-${item.event_id}`}>
      <TableCell className="whitespace-nowrap font-mono tabular-nums">{formatTime(item.created_at)}</TableCell>
      <TableCell>
        <div className="flex flex-col gap-1">
          <span className="font-medium">{summary.label}</span>
          <span className="text-xs text-muted-foreground">{summary.reason}</span>
          <div className="flex flex-wrap gap-1">
            <OperatorTypeBadge label={eventTypeLabel(item.event_type, copy)} preserveLabel />
            {item.failure_kind ? (
              <OperatorTypeBadge label={failureKindLabel(item.failure_kind, copy)} intent="degraded" preserveLabel />
            ) : null}
            {item.admission_reason ? (
              <OperatorTypeBadge label={admissionReasonLabel(item.admission_reason, copy)} intent="degraded" preserveLabel />
            ) : null}
          </div>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-col">
          <span>{modelLabel ?? <OperatorMissingValue />}</span>
          <span className="font-mono text-xs text-muted-foreground">{item.model.model_id ?? ""}</span>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-col">
          <span>{targetLabel}</span>
          <span className="text-xs text-muted-foreground">{endpointLabel ?? <OperatorMissingValue />}</span>
        </div>
      </TableCell>
      <TableCell className="whitespace-nowrap font-mono tabular-nums">
        {windowTimeValue ? formatTime(windowTimeValue) : <OperatorMissingValue />}
      </TableCell>
      <TableCell className="text-right">
        <Button type="button" variant="ghost" size="sm" onClick={onOpenDetail}>
          {copy.viewDetail}
        </Button>
      </TableCell>
    </TableRow>
  )
}

type RoutingHealthCopy = ReturnType<typeof useLocale>["messages"]["routingHealth"]

/** Enum keys never reach the screen; every one passes through here first. */
function eventTypeLabel(value: string, copy: RoutingHealthCopy): string {
  switch (value) {
    case "retry_scheduled":
      return copy.eventSummary.retryScheduled
    case "retry_exhausted":
      return copy.eventSummary.retryExhausted
    case "banned":
      return copy.eventSummary.banned
    case "unbanned":
      return copy.eventSummary.unbanned
    case "recovered":
      return copy.eventSummary.recovered
    case "admission_rejected":
      return copy.eventSummary.admissionRejected
    default:
      return copy.eventSummary.unknownEvent
  }
}

function failureKindLabel(value: string, copy: RoutingHealthCopy): string {
  switch (value) {
    case "transient_http":
      return copy.eventSummary.failureTransientHttp
    case "connect_error":
      return copy.eventSummary.failureConnectError
    case "timeout":
      return copy.eventSummary.failureTimeout
    default:
      return copy.eventSummary.unknownEvent
  }
}

function admissionReasonLabel(value: string, copy: RoutingHealthCopy): string {
  switch (value) {
    case "qps_limit":
      return copy.eventSummary.admissionQpsLimit
    case "max_in_flight_stream":
      return copy.eventSummary.admissionMaxInFlightStream
    case "max_in_flight_non_stream":
      return copy.eventSummary.admissionMaxInFlightNonStream
    default:
      return copy.eventSummary.unknownEvent
  }
}

function normalizeSearchArray(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((item): item is string => typeof item === "string")
  }
  if (typeof value === "string") {
    return [value]
  }
  return []
}
