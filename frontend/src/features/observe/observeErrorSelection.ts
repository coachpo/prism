import type { ObserveActivityItem } from "@/lib/api/observability"

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
