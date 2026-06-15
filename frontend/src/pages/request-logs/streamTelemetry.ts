import type { Messages } from "@/i18n/messages/en";
import type { StreamOutcome } from "@/lib/types";
import type { OperatorBadgeIntent } from "@/shared/design-system";

export function isStreamUsageUnavailableReason(reason: string | null | undefined): boolean {
  return reason === "STREAM_USAGE_UNAVAILABLE";
}

export function hasStreamTelemetryOutcome(outcome: StreamOutcome | null | undefined): boolean {
  return outcome !== null && outcome !== undefined;
}

export function isHistoricalUnknownStreamRow(
  _isStream: boolean,
  outcome: StreamOutcome | null | undefined,
): boolean {
  return outcome === "unknown";
}

export function getStreamOutcomeLabel(
  outcome: StreamOutcome | null | undefined,
  messages: Messages["requestLogs"],
): string {
  switch (outcome) {
    case "provider_incomplete":
      return messages.streamProviderIncomplete;
    case "client_disconnected":
      return messages.streamInterruptedClient;
    case "upstream_read_error":
      return messages.streamInterruptedUpstream;
    case "upstream_ended_without_terminal":
      return messages.streamEndedWithoutTerminal;
    case "unknown":
      return messages.streamUnknown;
    case "completed":
    case null:
    case undefined:
      return messages.streaming;
    case "not_streaming":
      return messages.nonStreaming;
  }
}

export function getStreamOutcomeIntent(outcome: StreamOutcome | null | undefined): OperatorBadgeIntent {
  switch (outcome) {
    case "completed":
      return "blue";
    case "provider_incomplete":
    case "upstream_ended_without_terminal":
      return "warning";
    case "client_disconnected":
    case "upstream_read_error":
      return "danger";
    case "unknown":
      return "muted";
    case "not_streaming":
    case null:
    case undefined:
      return "blue";
  }
}

export function shouldShowStreamStatus(outcome: StreamOutcome | null | undefined): boolean {
  return outcome !== null && outcome !== undefined && outcome !== "completed" && outcome !== "not_streaming";
}
