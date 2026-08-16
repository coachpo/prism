import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Loader2, RefreshCw } from "lucide-react"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useLocale } from "@/i18n/useLocale"
import { LoadbalanceEventsFragment } from "./LoadbalanceEventsFragment"
import { useTimezone } from "@/hooks/useTimezone"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { GlobalCurrentStateItem, GlobalCurrentStateResponse } from "@/lib/types"
import {
  OperatorCallout,
  OperatorClippedBadge,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorMissingValue,
  OperatorSectionCard,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
  type OperatorStatusTier,
} from "@/shared/design-system"
import { OperationalTableSkeletonRows, operationalRowStripe } from "@/shared/table/operationalTable"

// ---------------------------------------------------------------------------
// Fragment state machine (SPEC §9.2): every resource owns its own
// idle|loading|ready|empty|partial|error phase with last-good stale support.
// ---------------------------------------------------------------------------
export type FragmentPhase = "idle" | "loading" | "ready" | "empty" | "partial" | "error"

export interface FragmentState<T> {
  phase: FragmentPhase
  data: T | null
  stale: boolean
  lastSuccessfulAt: string | null
  error: string | null
  semanticQueryKey: string
}

function initialFragment<T>(key: string): FragmentState<T> {
  return { phase: "idle", data: null, stale: false, lastSuccessfulAt: null, error: null, semanticQueryKey: key }
}

type RoutingHealthSearch = Record<string, unknown>

export type RoutingHealthSearchUpdater = (patch: RoutingHealthSearch, replace?: boolean) => void

/**
 * Two sibling cards, never nested. Current state and the event timeline run on
 * two independent time bases, and each card states its own in its header so
 * the two can never be read as one window.
 */
export function RoutingHealthTab({
  search,
  onSearchChange,
}: {
  search: RoutingHealthSearch
  onSearchChange: (patch: RoutingHealthSearch, replace?: boolean) => void
}) {
  const { messages } = useLocale()
  const copy = messages.routingHealth
  return (
    <div className="flex flex-col gap-[var(--density-page-gap)]" data-testid="routing-health-tab">
      <GlobalCurrentStateFragment search={search} onSearchChange={onSearchChange} />
      <LoadbalanceEventsFragment search={search} onSearchChange={onSearchChange} />
      <p className="text-xs text-muted-foreground">{copy.sourceBoundaryNote}</p>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Global Current State (tokenless; the event window never reaches it).
// ---------------------------------------------------------------------------
function GlobalCurrentStateFragment({
  search,
  onSearchChange,
}: {
  search: RoutingHealthSearch
  onSearchChange: (patch: RoutingHealthSearch, replace?: boolean) => void
}) {
  const { messages, formatNumber } = useLocale()
  const { format: formatTime } = useTimezone()
  const copy = messages.routingHealth
  const [fragment, setFragment] = useState<FragmentState<GlobalCurrentStateResponse>>(() => initialFragment<GlobalCurrentStateResponse>("observe:runtime"))
  const [resettingTargetId, setResettingTargetId] = useState<number | null>(null)
  const [resetError, setResetError] = useState<string | null>(null)
  const [resetNotice, setResetNotice] = useState<string | null>(null)
  const [confirmTarget, setConfirmTarget] = useState<GlobalCurrentStateItem | null>(null)
  const generation = useRef(0)

  const modelId = (search.runtime_model_id as string) || undefined
  const states = useMemo(() => Array.isArray(search.runtime_state) ? search.runtime_state as string[] : search.runtime_state ? [search.runtime_state as string] : [], [search.runtime_state])
  const endpointId = (search.runtime_endpoint_id as string) || undefined
  const targetId = (search.runtime_terminal_target_id as string) || undefined
  const cursor = (search.runtime_cursor as string) || undefined

  // Submitted rather than live: the filter applies on Enter or blur, matching
  // how the event card's filters behave.
  const [modelDraft, setModelDraft] = useState(modelId ?? "")
  useEffect(() => setModelDraft(modelId ?? ""), [modelId])

  // Cursor pagination is forward-only on the wire, so the visited cursors are
  // kept here to make a real previous page instead of a "load more".
  const [cursorStack, setCursorStack] = useState<string[]>([])

  const semanticKey = JSON.stringify({ modelId, states: [...states].sort(), endpointId, targetId, cursor })

  const load = useCallback(async () => {
    const current = ++generation.current
    setFragment((fragment) => ({ ...fragment, phase: fragment.data === null ? "loading" : fragment.phase, stale: fragment.data !== null }))
    try {
      const response = await api.loadbalance.listCurrentState({
        model_id: modelId,
        state: states.length > 0 ? states as never : undefined,
        endpoint_id: endpointId ? Number(endpointId) : undefined,
        terminal_target_id: targetId ? Number(targetId) : undefined,
        limit: 50,
        cursor,
      })
      if (current !== generation.current) return
      const phase = response.items.length === 0 && response.completeness.state === "ready" ? "empty" : "ready"
      setFragment({ phase, data: response, stale: false, lastSuccessfulAt: new Date().toISOString(), error: null, semanticQueryKey: semanticKey })
    } catch (error) {
      if (current !== generation.current) return
      setFragment((fragment) => ({ ...fragment, phase: "error", stale: fragment.data !== null, error: error instanceof Error ? error.message : copy.loadFailed }))
    }
  }, [copy.loadFailed, cursor, endpointId, modelId, semanticKey, states, targetId])

  useEffect(() => { void load() }, [load])

  const updateSearch = useCallback((patch: RoutingHealthSearch) => {
    setCursorStack([])
    onSearchChange({ ...patch, runtime_cursor: undefined })
  }, [onSearchChange])

  const submitModelFilter = useCallback(() => {
    const next = modelDraft.trim() || undefined
    if (next === modelId) return
    updateSearch({ runtime_model_id: next })
  }, [modelDraft, modelId, updateSearch])

  const resetTarget = async (targetIdValue: number) => {
    setResettingTargetId(targetIdValue)
    setResetError(null)
    setResetNotice(null)
    try {
      const response = await api.loadbalance.resetCurrentState(targetIdValue)
      // A 2xx response is a confirmed success, including cleared=false; the
      // returned post-reset snapshot calibrates the row before revalidating.
      setFragment((current) => {
        if (!current.data || !response.state) return current
        const snapshot = response.state
        return {
          ...current,
          data: {
            ...current.data,
            items: current.data.items.map((item) => {
              if (item.terminal_target.id !== targetIdValue) return item
              // Calibrate the matching row with the authoritative post-reset
              // snapshot (connection_id-based snapshot maps to the row).
              return {
                ...item,
                observation_state: "observed" as const,
                state: snapshot.state,
                available: snapshot.state === "available",
                cycle_retry_attempts: snapshot.cycle_retry_attempts,
                cumulative_retry_attempts: snapshot.cumulative_retry_attempts,
                next_retry_at: snapshot.next_retry_at,
                last_retry_delay_ms: snapshot.last_retry_delay_ms,
                ban_mode: snapshot.ban_mode,
                banned_until_at: snapshot.banned_until_at,
                last_failure_kind: snapshot.last_failure_kind,
                updated_at: snapshot.updated_at,
              }
            }),
          },
        }
      })
      if (!response.cleared) {
        setResetNotice(copy.resetNothingToClear)
      }
      await load()
    } catch (error) {
      // The original row and state stay untouched on failure.
      setResetError(error instanceof Error ? error.message : copy.resetFailed)
    } finally {
      setResettingTargetId(null)
    }
  }

  const completeness = fragment.data?.completeness
  const rows = fragment.data?.items ?? []
  const bannedCount = rows.filter((item) => item.observation_state === "observed" && item.state === "banned").length
  const retryWaitCount = rows.filter((item) => item.observation_state === "observed" && item.state === "retry_wait").length
  const unobservedCount = rows.filter((item) => item.observation_state !== "observed").length

  return (
    <OperatorSectionCard
      data-testid="routing-health-current-state"
      title={copy.currentStateTitle}
      description={
        <>
          <span>{copy.currentStateDescription}</span>
          {rows.length > 0 ? (
            <span className="ml-1 text-foreground">
              {copy.currentStateSummary(
                formatNumber(rows.length),
                formatNumber(bannedCount),
                formatNumber(retryWaitCount),
              )}
              {unobservedCount > 0 ? ` · ${copy.currentStateSummaryUnobserved(formatNumber(unobservedCount))}` : ""}
            </span>
          ) : null}
        </>
      }
      contentClassName="flex flex-col gap-3"
      actions={
        <div className="flex items-center gap-2">
          {fragment.stale && fragment.lastSuccessfulAt ? (
            <OperatorStalenessBadge
              label={messages.honesty.lastSuccessful(formatTime(fragment.lastSuccessfulAt))}
              reason={fragment.error ?? undefined}
            />
          ) : null}
          <Button type="button" variant="outline" size="sm" onClick={() => void load()} disabled={fragment.phase === "loading"}>
            {fragment.phase === "loading" ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <RefreshCw data-icon="inline-start" />}
            {copy.refresh}
          </Button>
        </div>
      }
    >
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end">
        <FieldGroup className="flex-1">
          <Field>
            <FieldLabel htmlFor="runtime-model-filter">{copy.modelFilterLabel}</FieldLabel>
            <Input
              id="runtime-model-filter"
              value={modelDraft}
              placeholder={copy.modelFilterSubmitPlaceholder}
              onChange={(event) => setModelDraft(event.target.value)}
              onBlur={submitModelFilter}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault()
                  submitModelFilter()
                }
              }}
            />
          </Field>
        </FieldGroup>
        <FieldGroup className="flex-1">
          <Field>
            <FieldLabel htmlFor="runtime-state-filter">{copy.stateFilterLabel}</FieldLabel>
            <Select
              value={states.length === 1 ? states[0] : "all"}
              onValueChange={(value) => updateSearch({ runtime_state: value === "all" ? undefined : value })}
            >
              <SelectTrigger id="runtime-state-filter" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value="all">{copy.stateFilterAll}</SelectItem>
                  <SelectItem value="available">{copy.stateAvailable}</SelectItem>
                  <SelectItem value="retry_wait">{copy.stateRetryWait}</SelectItem>
                  <SelectItem value="banned">{copy.stateBanned}</SelectItem>
                  <SelectItem value="unobserved">{copy.stateUnobserved}</SelectItem>
                </SelectGroup>
              </SelectContent>
            </Select>
          </Field>
        </FieldGroup>
      </div>

      {/* A failed read that has no previous data keeps the card, replaces the
          body, and never degrades into "there is nothing here". */}
      {fragment.phase === "error" && fragment.data === null ? (
        <OperatorErrorState
          testId="runtime-load-error"
          title={copy.loadFailed}
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
          action={<Button type="button" variant="outline" size="sm" onClick={() => void load()}><RefreshCw data-icon="inline-start" />{copy.retry}</Button>}
        />
      ) : null}

      {completeness ? (
        <div className="flex flex-wrap items-center gap-2 text-xs" data-testid="runtime-completeness">
          <OperatorTypeBadge label={completenessLabel(completeness.state, copy)} intent={completeness.state === "ready" ? "muted" : "degraded"} preserveLabel />
          <span className="text-muted-foreground">
            {copy.completenessCounts(formatNumber(completeness.observed_target_count), formatNumber(completeness.configured_target_count))}
          </span>
          {completeness.state === "partial" || completeness.state === "unobserved" ? (
            <OperatorClippedBadge label={copy.coverageIncompleteTitle} reason={copy.completenessPartialNote} />
          ) : null}
        </div>
      ) : null}

      {resetError ? <OperatorCallout intent="danger" title={copy.resetFailed} role="alert">{resetError}</OperatorCallout> : null}
      {resetNotice ? <OperatorCallout intent="info" title={copy.resetNothingToClear}>{copy.resetNothingToClearDescription}</OperatorCallout> : null}

      {fragment.phase === "empty" ? (
        <OperatorEmptyState title={copy.currentStateEmpty} description={copy.currentStateEmptyDescription} />
      ) : null}

      {/* The card supplies the outer border; the table only needs to scroll. */}
      {rows.length > 0 || fragment.phase === "loading" ? (
        <div className="overflow-x-auto">
          <Table aria-label={copy.currentStateTitle}>
            <TableHeader>
              <TableRow>
                <TableHead>{copy.modelColumn}</TableHead>
                <TableHead>{copy.targetColumn}</TableHead>
                <TableHead>{copy.stateColumn}</TableHead>
                <TableHead className="text-right">{copy.attemptsColumn}</TableHead>
                <TableHead>{copy.nextRetryColumn}</TableHead>
                <TableHead>{copy.banUntilColumn}</TableHead>
                <TableHead className="text-right">{copy.actionsColumn}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {fragment.phase === "loading" && rows.length === 0 ? (
                <OperationalTableSkeletonRows columns={7} rows={5} />
              ) : (
                rows.map((item) => (
                  <CurrentStateRow
                    key={item.terminal_target.id}
                    item={item}
                    resetting={resettingTargetId === item.terminal_target.id}
                    onRequestReset={() => setConfirmTarget(item)}
                    formatTime={formatTime}
                    formatNumber={formatNumber}
                    copy={copy}
                    missingLabel={messages.honesty.noValue}
                  />
                ))
              )}
            </TableBody>
          </Table>
        </div>
      ) : null}

      {cursorStack.length > 0 || fragment.data?.has_more ? (
        <div className="flex items-center justify-end gap-1">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={cursorStack.length === 0}
            onClick={() => {
              const nextStack = cursorStack.slice(0, -1)
              setCursorStack(nextStack)
              onSearchChange({ runtime_cursor: nextStack.at(-1) })
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
              setCursorStack((stack) => [...stack, next])
              onSearchChange({ runtime_cursor: next })
            }}
          >
            {copy.nextPage}
          </Button>
        </div>
      ) : null}

      <AlertDialog open={confirmTarget !== null} onOpenChange={(open) => !open && setConfirmTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{copy.resetCooldownConfirmTitle}</AlertDialogTitle>
            <AlertDialogDescription>
              {copy.resetCooldownConfirmDescription(confirmTarget?.terminal_target.label ?? "")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{copy.cancel}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const target = confirmTarget
                setConfirmTarget(null)
                if (target) void resetTarget(target.terminal_target.id)
              }}
            >
              {copy.resetCooldownConfirmAction}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </OperatorSectionCard>
  )
}

/** Runtime state maps onto the four tiers; an unobserved row is `idle`. */
function stateTier(item: GlobalCurrentStateItem): OperatorStatusTier {
  if (item.observation_state !== "observed" || item.state === null) return "idle"
  if (item.state === "banned") return "failing"
  if (item.state === "retry_wait") return "degraded"
  return "healthy"
}

function CurrentStateRow({ item, resetting, onRequestReset, formatTime, formatNumber, copy, missingLabel }: {
  item: GlobalCurrentStateItem
  resetting: boolean
  onRequestReset: () => void
  formatTime: (value: string, options?: Intl.DateTimeFormatOptions) => string
  formatNumber: (value: number) => string
  copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]
  missingLabel: string
}) {
  const observed = item.observation_state === "observed"
  const hasAttemptCounters = item.cycle_retry_attempts !== null && item.cumulative_retry_attempts !== null
  const tier = stateTier(item)
  const hasCooldown = observed && (item.state === "retry_wait" || item.state === "banned")
  const disabledReason = !observed
    ? copy.resetCooldownDisabledUnobserved
    : !hasCooldown
      ? copy.resetCooldownDisabledNoCooldown
      : null

  return (
    <TableRow
      data-testid={`runtime-row-${item.terminal_target.id}`}
      className={cn("group/row", operationalRowStripe(tier))}
    >
      <TableCell>
        <div className="flex flex-col">
          <span className="font-medium">{item.model.label}</span>
          <span className="font-mono text-xs text-muted-foreground">{item.model.id}</span>
        </div>
      </TableCell>
      <TableCell>
        <div className="flex flex-col">
          <span>{item.terminal_target.label}</span>
          <span className="font-mono text-xs text-muted-foreground">#{item.terminal_target.id}</span>
        </div>
      </TableCell>
      <TableCell>
        <OperatorStatusBadge intent={tier} label={stateLabel(observed ? item.state : null, copy)} preserveLabel />
      </TableCell>
      <TableCell className="text-right font-mono tabular-nums">
        {observed && hasAttemptCounters
          ? `${formatNumber(item.cycle_retry_attempts!)} / ${formatNumber(item.cumulative_retry_attempts!)}`
          : <OperatorMissingValue reason={observed ? missingLabel : copy.stateUnobserved} />}
      </TableCell>
      <TableCell className="font-mono tabular-nums">
        {observed && item.next_retry_at ? formatTime(item.next_retry_at) : <OperatorMissingValue reason={observed ? missingLabel : copy.stateUnobserved} />}
      </TableCell>
      <TableCell className="font-mono tabular-nums">
        {observed && item.banned_until_at ? formatTime(item.banned_until_at) : <OperatorMissingValue reason={observed ? missingLabel : copy.stateUnobserved} />}
      </TableCell>
      <TableCell className="text-right">
        {disabledReason ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span tabIndex={0}>
                <Button type="button" variant="outline" size="sm" disabled aria-describedby={`reset-reason-${item.terminal_target.id}`}>
                  {copy.resetCooldown}
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent id={`reset-reason-${item.terminal_target.id}`}>{disabledReason}</TooltipContent>
          </Tooltip>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={resetting}
            aria-busy={resetting}
            onClick={onRequestReset}
            className="opacity-0 transition-opacity focus-visible:opacity-100 group-hover/row:opacity-100"
          >
            {resetting ? <Loader2 data-icon="inline-start" className="animate-spin" /> : null}
            {copy.resetCooldown}
          </Button>
        )}
      </TableCell>
    </TableRow>
  )
}

function completenessLabel(state: string, copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]): string {
  switch (state) {
    case "ready":
      return copy.completenessReady
    case "no_config":
      return copy.completenessNoConfig
    case "partial":
      return copy.completenessPartial
    default:
      return copy.completenessUnobserved
  }
}

function stateLabel(state: string | null, copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]): string {
  switch (state) {
    case "available":
      return copy.stateAvailable
    case "retry_wait":
      return copy.stateRetryWait
    case "banned":
      return copy.stateBanned
    default:
      return copy.stateUnobserved
  }
}
