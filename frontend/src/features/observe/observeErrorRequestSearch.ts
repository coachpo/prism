import type { ObserveErrorSelection } from "@/features/observe/observeErrorSelection";
import type { ObserveScope } from "@/features/observe/observeSearch";
import type { UsageErrorsResponse } from "@/lib/api/observability";

/**
 * Projects the backend-authored error cohort into the canonical Requests
 * attempts view. Comma joining preserves repeated OR values and `__null__`;
 * separate keys remain an AND conjunction.
 */
export function buildRequestsSearch(
  selection: ObserveErrorSelection,
  requestsContext: UsageErrorsResponse["requests_context"] | null,
  queryContext: string | null,
  _scope: ObserveScope,
): Record<string, string> {
  void _scope;
  const search: Record<string, string> = {
    view: "attempts",
    query_context: requestsContext?.query_context ?? queryContext ?? "",
  };
  for (const [key, values] of Object.entries(selection.requestFilters)) {
    search[key] = values.join(",");
  }
  return search;
}
