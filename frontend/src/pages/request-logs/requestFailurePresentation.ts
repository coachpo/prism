import { requestRecoveryMessages } from "@/i18n/messages/requestRecovery";

export function describeRequestFailure({ statusCode, streamOutcome, streamErrorKind, errorPresent = false }: {
  statusCode: number | null;
  streamOutcome?: string | null;
  streamErrorKind?: string | null;
  errorPresent?: boolean;
}) {
  const copy = requestRecoveryMessages.failure;
  if (streamOutcome === "client_disconnected" || streamErrorKind === "client_write_failed" || streamErrorKind === "request_context_canceled") return copy.disconnected;
  if (["provider_incomplete", "upstream_read_error", "upstream_ended_without_terminal"].includes(streamOutcome ?? "") || ["upstream_read_failed", "missing_terminal_event"].includes(streamErrorKind ?? "")) return copy.interrupted;
  if (statusCode === 401) return copy.unauthorized;
  if (statusCode === 403) return copy.forbidden;
  if (statusCode === 404) return copy.missingResource;
  if (statusCode === 413) return copy.tooLarge;
  if (statusCode === 429) return copy.limited;
  if (statusCode === 408 || statusCode === 504) return copy.timeout;
  if (statusCode !== null && statusCode >= 500) return copy.service;
  if (statusCode !== null && statusCode >= 400) return copy.invalid;
  return errorPresent || Boolean(streamErrorKind) ? copy.missing : null;
}

export function requestServiceLabel(label: string | null | undefined, fallback = "未命名服务"): string {
  return !label || /^#\d+$/.test(label) || /^(?:Terminal Target|Endpoint|终端目标|端点)\s*#\d+$/i.test(label) ? fallback : label;
}

export function requestClientLabel(display: string | null | undefined, rawUserAgent?: string | null): string {
  if (!display || display === rawUserAgent || /Mozilla\/|AppleWebKit\/|Chrome\/|Safari\/|Gecko\//i.test(display)) return requestRecoveryMessages.clientNotIdentified;
  return display;
}
