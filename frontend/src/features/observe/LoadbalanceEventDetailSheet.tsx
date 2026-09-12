import { useEffect, useState, type ReactNode } from "react"
import { ExternalLink, RefreshCw } from "lucide-react"
import { Link } from "@tanstack/react-router"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetFooter, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { requestServiceLabel } from "@/pages/request-logs/requestFailurePresentation"
import { useLocale } from "@/i18n/useLocale"
import { useTimezone } from "@/hooks/useTimezone"
import { api } from "@/lib/api"
import type { LoadbalanceEventDetail } from "@/lib/types"
import { OperatorCallout, OperatorLoadingState, OperatorMissingValue, OperatorTypeBadge } from "@/shared/design-system"
import { encodeObserveReturn, type ObserveReturnPayload } from "@/lib/observeReturn"
import { admissionReasonLabel, eventTypeLabel, failureKindLabel, renderEventSummary } from "./eventSummary"

type DetailState =
  | { phase: "idle" }
  | { phase: "loading" }
  | { phase: "ready"; data: LoadbalanceEventDetail }
  | { phase: "error" }

export function LoadbalanceEventDetailSheet({
  eventId,
  queryContext,
  onClose,
  onRetryContext,
  sourceSearch = {},
}: {
  eventId: string | null
  queryContext: string | null
  onClose: () => void
  onRetryContext: () => void
  sourceSearch?: Record<string, unknown>
}) {
  const { messages } = useLocale()
  const { format: formatTime } = useTimezone()
  const copy = messages.routingHealth
  const [state, setState] = useState<DetailState>({ phase: "idle" })

  useEffect(() => {
    if (!eventId || !queryContext) {
      return
    }
    let cancelled = false
    const load = async () => {
      setState({ phase: "loading" })
      try {
        const data = await api.loadbalance.getEvent(eventId, queryContext)
        if (cancelled) return
        setState({ phase: "ready", data })
      } catch {
        if (cancelled) return
        setState({ phase: "error" })
      }
    }
    void load()
    return () => { cancelled = true }
  }, [eventId, queryContext])

  // When the sheet closes the previous detail must not linger as "ready".
  const openEventId = eventId
  const [lastOpenEventId, setLastOpenEventId] = useState<string | null>(null)
  if (openEventId !== lastOpenEventId) {
    setLastOpenEventId(openEventId)
    if (openEventId === null || !queryContext) {
      setState({ phase: "idle" })
    }
  }

  const eventSummaryMessages = messages.routingHealth.eventSummary
  const summary = state.phase === "ready" ? renderEventSummary(state.data.summary, eventSummaryMessages) : null

  return (
    <Sheet open={eventId !== null} onOpenChange={(open) => { if (!open) onClose() }}>
      <SheetContent side="right" size="md" className="flex flex-col">
        <SheetHeader>
          <SheetTitle>{copy.detailTitle}</SheetTitle>
          <SheetDescription>
            {state.phase === "ready" && state.data ? (
              <span className="flex flex-wrap items-center gap-2">
                {/* 分诊时打开详情就是为了确认列表里那条到底发生了什么：
                    两边必须是同一份字典，未知取值落具名兜底而不是枚举键。 */}
                <OperatorTypeBadge label={eventTypeLabel(state.data.event_type, eventSummaryMessages)} preserveLabel />
                {state.data.failure_kind ? <OperatorTypeBadge label={failureKindLabel(state.data.failure_kind, eventSummaryMessages)} preserveLabel /> : null}
                {state.data.admission_reason ? <OperatorTypeBadge label={admissionReasonLabel(state.data.admission_reason, eventSummaryMessages)} preserveLabel /> : null}
              </span>
            ) : state.phase === "error" ? copy.detailFailed : copy.detailLoadingDescription}
          </SheetDescription>
        </SheetHeader>
        {state.phase === "loading" ? (
          <div className="flex-1 overflow-y-auto p-6">
            <OperatorLoadingState title={copy.detailLoading} description={copy.detailLoadingDescription} />
          </div>
        ) : null}

        {state.phase === "error" ? (
          <div className="flex flex-1 flex-col gap-3 overflow-y-auto p-6">
            <OperatorCallout intent="danger" title={copy.detailFailed}>
              {copy.detailFailedRecovery}
            </OperatorCallout>
            <div className="flex gap-2">
              <Button type="button" variant="outline" size="sm" onClick={onRetryContext}>
                <RefreshCw data-icon="inline-start" />
                {copy.retryContext}
              </Button>
            </div>
          </div>
        ) : null}

        {state.phase === "ready" && state.data ? (
          <EventDetailBody detail={state.data} summaryLabel={summary?.label ?? ""} summaryReason={summary?.reason ?? ""} formatTime={formatTime} copy={copy} onClose={onClose} sourceSearch={sourceSearch} />
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function EventDetailBody({ detail, summaryLabel, summaryReason, formatTime, copy, onClose, sourceSearch }: {
  detail: LoadbalanceEventDetail
  summaryLabel: string
  summaryReason: string
  formatTime: (value: string, options?: Intl.DateTimeFormatOptions) => string
  copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]
  onClose: () => void
  sourceSearch: Record<string, unknown>
}) {
  const { messages } = useLocale()
  const banDisabled = detail.ban_mode === "off"
  const handoffAvailable = detail.request_context_filters != null && detail.request_context_unavailable_reason == null
  const requestSearch = detail.request_context_filters
    ? {
        from_time: detail.request_context_filters.from_time,
        to_time: detail.request_context_filters.to_time,
        model_id: detail.request_context_filters.model_id ?? undefined,
        endpoint_id: detail.request_context_filters.endpoint_id != null ? String(detail.request_context_filters.endpoint_id) : undefined,
        terminal_target_id: detail.request_context_filters.terminal_target_id != null ? String(detail.request_context_filters.terminal_target_id) : undefined,
      }
    : null
  // The validated return token restores the source event context (window,
  // filters, sort, cursor and the opened event) when returning from Requests.
  const observeReturnPayload: ObserveReturnPayload = {
    v: 1,
    event_id: detail.event_id,
    preset: (sourceSearch.preset as ObserveReturnPayload["preset"]) || "24h",
    from_time: typeof sourceSearch.from_time === "string" ? sourceSearch.from_time : undefined,
    to_time: typeof sourceSearch.to_time === "string" ? sourceSearch.to_time : undefined,
    event_type: savedFilter(sourceSearch.event_type),
    event_failure_kind: savedFilter(sourceSearch.event_failure_kind),
    event_admission_reason: savedFilter(sourceSearch.event_admission_reason),
    event_model_id: typeof sourceSearch.event_model_id === "string" ? sourceSearch.event_model_id : undefined,
    event_endpoint_id: typeof sourceSearch.event_endpoint_id === "string" ? sourceSearch.event_endpoint_id : undefined,
    event_terminal_target_id: typeof sourceSearch.event_terminal_target_id === "string" ? sourceSearch.event_terminal_target_id : undefined,
    event_sort_order: sourceSearch.event_sort_order === "asc" ? "asc" : "desc",
    event_cursor: typeof sourceSearch.event_cursor === "string" ? sourceSearch.event_cursor : undefined,
    runtime_model_id: typeof sourceSearch.runtime_model_id === "string" ? sourceSearch.runtime_model_id : undefined,
    runtime_endpoint_id: typeof sourceSearch.runtime_endpoint_id === "string" ? sourceSearch.runtime_endpoint_id : undefined,
    runtime_terminal_target_id: typeof sourceSearch.runtime_terminal_target_id === "string" ? sourceSearch.runtime_terminal_target_id : undefined,
    runtime_cursor: typeof sourceSearch.runtime_cursor === "string" ? sourceSearch.runtime_cursor : undefined,
    runtime_state: savedFilter(sourceSearch.runtime_state),
  }
  const observeReturn = encodeObserveReturn(observeReturnPayload)

  return (
    <>
      <div className="flex flex-1 flex-col gap-5 overflow-y-auto p-6">
        <section className="flex flex-col gap-2 rounded-lg border border-border p-4" aria-labelledby="detail-what-happened">
          <h3 id="detail-what-happened" className="text-sm font-medium">{copy.detailWhatHappened}</h3>
          <p className="text-sm font-medium">{summaryLabel}</p>
          <p className="text-sm text-foreground/70">{summaryReason}</p>
          <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-2">
            <DetailField label={copy.timeField} numeric value={formatTime(detail.created_at)} />
            <DetailField label={copy.cycleAttemptsField} numeric value={String(detail.cycle_retry_attempts)} />
            <DetailField label={copy.cumulativeAttemptsField} numeric value={String(detail.cumulative_retry_attempts)} />
            <DetailField
              label={copy.nextRetryField}
              numeric
              value={detail.next_retry_at ? formatTime(detail.next_retry_at) : <OperatorMissingValue reason={copy.nextRetryMissingReason} />}
            />
            <DetailField label={copy.lastDelayField} numeric value={`${detail.last_retry_delay_ms} ms`} />
          </dl>
        </section>

        <section className="flex flex-col gap-2 rounded-lg border border-border p-4" aria-labelledby="detail-objects">
          <h3 id="detail-objects" className="text-sm font-medium">{copy.detailObjects}</h3>
          <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-2">
            <DetailField
              label={copy.modelField}
              value={detail.model.label || detail.model.model_id || messages.routingHealth.unattributed}
            />
            <DetailField
              label={copy.endpointField}
              value={requestServiceLabel(detail.endpoint.label, copy.unnamedService)}
            />
            <DetailField
              label={copy.targetField}
              value={requestServiceLabel(detail.terminal_target.label, copy.unnamedModelService)}
            />
          </dl>
          <div className="flex flex-wrap gap-2">
            <ObjectLink
              enabled={detail.model.configured === true && detail.model.model_config_id != null}
              label={copy.openModel}
              to={`/route/models/${detail.model.model_config_id}`}
            />
            <ObjectLink
              enabled={detail.terminal_target.configured === true && detail.terminal_target.owner_model_config_id != null}
              label={copy.openTarget}
              to={`/route/models/${detail.terminal_target.owner_model_config_id}?focus_connection_id=${detail.terminal_target.id}`}
            />
          </div>
        </section>

        <section className="flex flex-col gap-2 rounded-lg border border-border p-4" aria-labelledby="detail-snapshot">
          <h3 id="detail-snapshot" className="text-sm font-medium">{copy.detailSnapshot}</h3>
          {/* 「封禁模式 off」和「阈值读不到」不是一回事：模式走标签字典，
              每个缺失的破折号各自带上它自己的原因。 */}
          <dl className="grid grid-cols-1 gap-2 text-sm sm:grid-cols-2">
            <DetailField
              label={copy.banModeField}
              value={detail.ban_mode ? banModeLabel(detail.ban_mode, copy) : <OperatorMissingValue reason={copy.banModeMissingReason} />}
            />
            <DetailField
              label={copy.banUntilField}
              numeric
              value={detail.banned_until_at ? formatTime(detail.banned_until_at) : <OperatorMissingValue reason={banDisabled ? copy.banUntilOffReason : copy.banUntilNoneReason} />}
            />
            <DetailField
              label={copy.lastSuccessField}
              numeric
              value={detail.last_success_at ? formatTime(detail.last_success_at) : <OperatorMissingValue reason={copy.lastSuccessMissingReason} />}
            />
            <DetailField
              label={copy.policyCycleLimitField}
              numeric
              value={detail.policy_cycle_retry_attempt_limit != null ? String(detail.policy_cycle_retry_attempt_limit) : <OperatorMissingValue reason={copy.policyCycleLimitMissingReason} />}
            />
            <DetailField
              label={copy.policyBanThresholdField}
              numeric
              value={banDisabled ? copy.banModeOff : detail.policy_ban_cumulative_retry_attempt_threshold != null ? String(detail.policy_ban_cumulative_retry_attempt_threshold) : <OperatorMissingValue reason={copy.policyBanThresholdMissingReason} />}
            />
          </dl>
        </section>

        <section className="flex flex-col gap-2 rounded-lg border border-border p-4" aria-labelledby="detail-requests">
          <h3 id="detail-requests" className="text-sm font-medium">{copy.detailRequests}</h3>
          {handoffAvailable && requestSearch ? (
            <div className="flex flex-col gap-2">
              <Link
                to="/observe/requests"
                search={{ ...requestSearch, observe_return: observeReturn }}
                className="inline-flex w-fit items-center gap-2 rounded-lg border border-border bg-panel px-3 py-2 text-sm font-medium hover:bg-inset"
              >
                <ExternalLink data-icon="inline-start" />
                {copy.investigateRequestsCta}
              </Link>
              <p className="text-xs text-muted-foreground">{copy.investigateRequestsNote}</p>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{copy.investigateRequestsUnavailable}</p>
          )}
        </section>
      </div>
      <SheetFooter>
        <Button type="button" variant="outline" onClick={onClose}>{messages.common.close}</Button>
      </SheetFooter>
    </>
  )
}

/**
 * 抽屉正是拿来和列表逐字比对的地方：时间戳、计数与延迟必须与表格同族同宽
 * （表格侧是 font-mono tabular-nums 13px），否则两边对不齐也读不出量级。
 * 中文名称与散文不进等宽——混排会撕裂字形。
 */
function DetailField({ label, numeric = false, value }: { label: string; numeric?: boolean; value: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={numeric ? "font-mono text-[0.8125rem] font-medium tabular-nums" : "font-medium"}>{value}</dd>
    </div>
  )
}

function banModeLabel(mode: string, copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]): string {
  switch (mode) {
    case "off":
      return copy.banModeOff
    case "temporary":
      return copy.banModeTemporary
    case "until_reset":
      return copy.banModeUntilReset
    default:
      return copy.banModeUnknown
  }
}

function ObjectLink({ enabled, label, to }: { enabled: boolean; label: string; to: string }) {
  const { messages } = useLocale()
  if (!enabled) {
    return <span className="text-xs text-foreground/40">{messages.routingHealth.linkUnavailable}</span>
  }
  return (
    <Link to={to} className="inline-flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs font-medium hover:bg-inset">
      <ExternalLink data-icon="inline-start" />
      {label}
    </Link>
  )
}

function savedFilter(value: unknown): string | string[] | undefined {
  if (typeof value === "string") return value
  return Array.isArray(value) && value.every(item => typeof item === "string") ? value : undefined
}
