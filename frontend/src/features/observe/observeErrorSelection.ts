import type { ObserveActivityItem, UsageErrorsResponse } from "@/lib/api/observability"

/**
 * What the operator picked in the error ranking.
 *
 * `requestFilters` is the backend-built conjunction, carried through verbatim
 * so the "open in request logs" deep link stays byte-identical to what the
 * ranking would have navigated to before.
 */
export type ObserveErrorSelection = {
  key: string
  label: string
  requestFilters: Record<string, string[]>
  match: (item: ObserveActivityItem) => boolean
}

export function httpStatusSelection(statusCode: number, label: string, requestFilters: Record<string, string[]>): ObserveErrorSelection {
  return {
    key: `http:${statusCode}`,
    label,
    requestFilters,
    match: (item) => item.status_code === statusCode,
  }
}

export function streamOutcomeSelection(outcome: string, label: string, requestFilters: Record<string, string[]>): ObserveErrorSelection {
  return {
    key: `stream:${outcome}`,
    label,
    requestFilters,
    match: (item) => item.stream_outcome === outcome,
  }
}

export function streamErrorKindSelection(
  outcome: string,
  kind: string | null,
  label: string,
  requestFilters: Record<string, string[]>,
): ObserveErrorSelection {
  return {
    key: `kind:${outcome}:${kind ?? "__null__"}`,
    label,
    requestFilters,
    match: (item) => item.stream_outcome === outcome && item.stream_error_kind === kind,
  }
}

/** Keeps the operator's category, but refreshes its server-authored filter conjunction. */
export function resolveErrorSelection(previous: ObserveErrorSelection, data: UsageErrorsResponse): ObserveErrorSelection | null {
  for (const status of data.http_statuses) {
    if (previous.key === `http:${status.status_code}`) return { ...previous, requestFilters: status.request_filters };
  }
  for (const outcome of data.stream_outcomes) {
    if (previous.key === `stream:${outcome.stream_outcome}`) return { ...previous, requestFilters: outcome.request_filters };
    for (const kind of outcome.error_kinds) {
      if (previous.key === `kind:${outcome.stream_outcome}:${kind.stream_error_kind ?? "__null__"}`) return { ...previous, requestFilters: kind.request_filters };
    }
  }
  return null;
}
