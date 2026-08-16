import type { EventSummaryV1 } from "@/lib/types"
import { useLocale } from "@/i18n/useLocale"

// Exhaustive event summary renderer: the six event enums map one-to-one to
// zh-CN catalog keys (SPEC §12). Unknown codes fall back to a localized
// generic line plus the raw code for diagnostics — never backend prose.
export function renderEventSummary(summary: EventSummaryV1, eventSummaryMessages: ReturnType<typeof useLocale>["messages"]["routingHealth"]["eventSummary"]): { label: string; reason: string } {
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

function failureKindLabel(kind: string, copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]["eventSummary"]): string {
  switch (kind) {
    case "transient_http":
      return copy.failureTransientHttp
    case "connect_error":
      return copy.failureConnectError
    case "timeout":
      return copy.failureTimeout
    default:
      return kind
  }
}

function admissionReasonLabel(reason: string, copy: ReturnType<typeof useLocale>["messages"]["routingHealth"]["eventSummary"]): string {
  switch (reason) {
    case "qps_limit":
      return copy.admissionQpsLimit
    case "max_in_flight_stream":
      return copy.admissionMaxInFlightStream
    case "max_in_flight_non_stream":
      return copy.admissionMaxInFlightNonStream
    default:
      return reason
  }
}
