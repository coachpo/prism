import { AlertCircle, Loader2, RotateCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/i18n/useLocale"
import type { LoadbalanceCurrentStateItem } from "@/lib/types"
import { OperatorCallout, OperatorStatusBadge } from "@/shared/design-system"

function formatDateTime(isoString: string | null | undefined) {
  if (!isoString) return ""
  const date = new Date(isoString)
  if (Number.isNaN(date.getTime())) return isoString
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "numeric",
    minute: "numeric",
    second: "numeric",
    hour12: false,
  }).format(date)
}

function formatDurationMS(milliseconds: number | null | undefined) {
  if (milliseconds == null) return "—"
  if (milliseconds < 1000) return `${milliseconds} ms`
  return `${(milliseconds / 1000).toFixed(2)} s`
}

interface TerminalTargetRuntimeSummaryProps {
  connectionId: number
  state: LoadbalanceCurrentStateItem | null | undefined
  resetting: boolean
  onResetCooldown?: (connectionId: number) => void
  onRefresh?: () => void
  refreshError?: string | null
}

const STALE_SNAPSHOT_AFTER_MS = 60_000

function isSnapshotStale(item: LoadbalanceCurrentStateItem | null | undefined) {
  if (!item) return false
  const updatedAt = new Date(item.updated_at).getTime()
  if (!Number.isFinite(updatedAt)) return false
  return Date.now() - updatedAt > STALE_SNAPSHOT_AFTER_MS
}

// TerminalTargetRuntimeSummary renders the process-local Ban Policy
// observation for one connection. It never claims probe health or upstream
// availability: `available` means only "no current cooldown limit".
export function TerminalTargetRuntimeSummary({
  connectionId,
  state,
  resetting,
  onResetCooldown,
  onRefresh,
  refreshError,
}: TerminalTargetRuntimeSummaryProps) {
  const { formatRelativeTimeFromNow, messages } = useLocale()
  const copy = messages.routing
  const stale = isSnapshotStale(state)

  if (state == null) {
    return (
      <div className="flex items-center justify-between gap-3 rounded-md border border-dashed px-3 py-2">
        <p className="text-xs text-muted-foreground">{copy.noRuntimeObservation}</p>
        {onRefresh ? (
          <Button type="button" variant="ghost" size="sm" onClick={onRefresh}>
            <RotateCw data-icon="inline-start" />
            {copy.refresh}
          </Button>
        ) : null}
      </div>
    )
  }

  const showReset = state.state === "retry_wait" || state.state === "banned"
  const bannedUntil = formatDateTime(state.banned_until_at)
  const stateLabel =
    state.state === "retry_wait"
      ? copy.retryWaitUntil(formatDateTime(state.next_retry_at))
      : state.state === "banned"
        ? bannedUntil ? copy.bannedUntil(bannedUntil) : copy.banned
        : copy.noCooldown

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-inset px-3 py-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <OperatorStatusBadge
            intent={state.state === "available" ? "healthy" : state.state === "banned" ? "failing" : "degraded"}
            label={stateLabel}
            preserveLabel
          />
          {stale ? (
            <span className="inline-flex items-center gap-1 text-xs text-degraded">
              <AlertCircle data-icon="inline-start" />
              {copy.stateStale}
            </span>
          ) : null}
          {refreshError ? (
            <span className="inline-flex items-center gap-1 text-xs text-destructive" role="alert">
              <AlertCircle data-icon="inline-start" />
              {refreshError}
            </span>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          {onRefresh ? (
            <Button type="button" variant="ghost" size="icon-sm" aria-label={copy.refresh} onClick={onRefresh}>
              <RotateCw />
            </Button>
          ) : null}
          {showReset && onResetCooldown ? (
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={resetting}
              aria-busy={resetting}
              onClick={() => onResetCooldown(connectionId)}
            >
              {resetting ? <Loader2 data-icon="inline-start" className="animate-spin" /> : <RotateCw data-icon="inline-start" />}
              {copy.resetCooldown}
            </Button>
          ) : null}
        </div>
      </div>

      <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs text-muted-foreground sm:grid-cols-2">
        {state.last_success_at ? (
          <div className="flex flex-wrap items-center gap-1">
            <dt>{copy.lastSuccessAt("")}:</dt>
            <dd>
              <time dateTime={state.last_success_at} title={formatDateTime(state.last_success_at)}>
                {formatRelativeTimeFromNow(state.last_success_at)}
              </time>
            </dd>
          </div>
        ) : null}
        {state.last_success_response_headers_latency_ms != null ? (
          <div className="flex items-center gap-1">
            <dt>{copy.lastSuccessLatency("")}:</dt>
            <dd className="font-mono tabular-nums">{formatDurationMS(state.last_success_response_headers_latency_ms)}</dd>
          </div>
        ) : null}
        <div className="flex items-center gap-1">
          <dt>{copy.inFlight("", "")}:</dt>
          <dd className="font-mono tabular-nums">
            {state.in_flight_non_stream} / {state.in_flight_stream}
          </dd>
        </div>
      </dl>

      {showReset ? (
        <details className="text-xs text-muted-foreground">
          <summary className="cursor-pointer font-medium text-foreground">{copy.resetCooldownDescription}</summary>
          <p className="pt-2">{copy.resetCooldownDescriptionDetails}</p>
        </details>
      ) : null}
    </div>
  )
}

export function TerminalTargetRuntimeError({ message, onRetry }: { message: string; onRetry?: () => void }) {
  const { messages } = useLocale()
  return (
    <OperatorCallout intent="danger" description={message} role="alert">
      {onRetry ? (
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          <RotateCw data-icon="inline-start" />
          {messages.routing.retry}
        </Button>
      ) : null}
    </OperatorCallout>
  )
}
