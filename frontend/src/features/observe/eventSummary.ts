import type { EventSummaryV1 } from "@/lib/types"
import { useLocale } from "@/i18n/useLocale"

type EventSummaryCopy = ReturnType<typeof useLocale>["messages"]["routingHealth"]["eventSummary"]

// Exhaustive event summary renderer: the six event enums map one-to-one to
// zh-CN catalog keys (SPEC §12). Unknown codes fall back to a localized
// generic line plus the raw code for diagnostics — never backend prose.
export function renderEventSummary(summary: EventSummaryV1, eventSummaryMessages: EventSummaryCopy): { label: string; reason: string } {
  const copy = eventSummaryMessages
  const params = summary.params ?? {}
  const evidence = params.evidence_state === "legacy_incomplete" ? copy.legacyIncompleteSuffix : ""

  switch (summary.code) {
    case "loadbalance.retry_scheduled":
      return {
        label: `${copy.retryScheduled}${evidence}`,
        reason: params.failure_kind
          ? copy.retryScheduledReason(failureKindLabel(params.failure_kind, copy), formatRetryCounter(params.cycle_retry_attempts), formatRetryCounter(params.cumulative_retry_attempts))
          : copy.legacyReason,
      }
    case "loadbalance.retry_exhausted":
      return {
        label: `${copy.retryExhausted}${evidence}`,
        reason: params.failure_kind
          ? copy.retryExhaustedReason(failureKindLabel(params.failure_kind, copy), formatRetryCounter(params.cycle_retry_attempts), formatRetryCounter(params.policy_cycle_retry_attempt_limit))
          : copy.legacyReason,
      }
    case "loadbalance.banned":
      return {
        label: `${copy.banned}${evidence}`,
        reason: params.failure_kind
          ? copy.bannedReason(failureKindLabel(params.failure_kind, copy), formatRetryCounter(params.cumulative_retry_attempts), formatRetryCounter(params.policy_ban_cumulative_retry_attempt_threshold))
          : copy.legacyReason,
      }
    case "loadbalance.unbanned":
      return {
        label: `${copy.unbanned}${evidence}`,
        reason: copy.unbannedReason,
      }
    case "loadbalance.recovered":
      return {
        label: `${copy.recovered}${evidence}`,
        reason: copy.recoveredReason,
      }
    case "loadbalance.admission_rejected":
      return {
        label: `${copy.admissionRejected}${evidence}`,
        reason: params.admission_reason ? copy.admissionReason(admissionReasonLabel(params.admission_reason, copy)) : copy.legacyReason,
      }
    default:
      return {
        label: copy.unknownEvent,
        reason: `${copy.unknownEventCode}${summary.code ?? "unknown"}`,
      }
  }
}

function formatRetryCounter(value: number | null | undefined): number | string {
  return value == null ? "—" : value
}

// 事件列表与事件详情抽屉共用这三份字典：同一个事实在两个界面里必须是同一个
// 名字，未知取值落到具名兜底，原始枚举键一律不上屏。
export function eventTypeLabel(value: string, copy: EventSummaryCopy): string {
  switch (value) {
    case "retry_scheduled":
      return copy.retryScheduled
    case "retry_exhausted":
      return copy.retryExhausted
    case "banned":
      return copy.banned
    case "unbanned":
      return copy.unbanned
    case "recovered":
      return copy.recovered
    case "admission_rejected":
      return copy.admissionRejected
    default:
      return copy.unknownEvent
  }
}

export function failureKindLabel(kind: string, copy: EventSummaryCopy): string {
  switch (kind) {
    case "transient_http":
      return copy.failureTransientHttp
    case "connect_error":
      return copy.failureConnectError
    case "timeout":
      return copy.failureTimeout
    default:
      return copy.unknownFailureKind
  }
}

export function admissionReasonLabel(reason: string, copy: EventSummaryCopy): string {
  switch (reason) {
    case "qps_limit":
      return copy.admissionQpsLimit
    case "max_in_flight_stream":
      return copy.admissionMaxInFlightStream
    case "max_in_flight_non_stream":
      return copy.admissionMaxInFlightNonStream
    default:
      return copy.unknownAdmissionReason
  }
}
