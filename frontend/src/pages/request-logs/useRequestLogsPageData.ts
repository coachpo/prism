import type { RequestLogPageState } from "./queryParams";
import {
  EMPTY_REQUEST_LOG_FILTER_OPTIONS,
  type RequestLogFilterOptions,
} from "./requestLogQuery";
import { useRequestLogAttempts } from "./useRequestLogAttempts";
import {
  useRequestLogIngressChains,
} from "./useRequestLogIngressChains";

export type { RequestLogFilterOptions as FilterOptions } from "./requestLogQuery";

interface UseRequestLogsPageDataParams {
  enabled?: boolean;
  revision: number;
  state: RequestLogPageState;
}

/**
 * Page-facing composition only. Attempts and ingress chains keep separate
 * query, replacement, append, cursor, and honest-read state owners; this hook
 * selects the active view and preserves the existing page contract.
 */
export function useRequestLogsPageData({
  revision,
  state,
  enabled = true,
}: UseRequestLogsPageDataParams) {
  const attemptsEnabled = enabled && state.view !== "ingress_chains";
  const chainsEnabled = enabled && state.view === "ingress_chains";
  const attempts = useRequestLogAttempts({
    revision,
    state,
    enabled: attemptsEnabled,
  });
  const chains = useRequestLogIngressChains({
    revision,
    state,
    enabled: chainsEnabled,
  });
  const active = chainsEnabled ? chains : attempts;
  const fallback = chainsEnabled ? attempts : chains;
  const filterOptionsLoaded = enabled &&
    (active.filterOptionsLoaded || fallback.filterOptionsLoaded);
  const filterOptions: RequestLogFilterOptions = !enabled
    ? EMPTY_REQUEST_LOG_FILTER_OPTIONS
    : active.filterOptionsLoaded
      ? active.filterOptions
      : fallback.filterOptionsLoaded
        ? fallback.filterOptions
        : EMPTY_REQUEST_LOG_FILTER_OPTIONS;
  // A successful read (including a stale last-good read) owns its metadata. A
  // non-stale error also owns the failed scope, so its null coverage must not
  // borrow an inactive view's coverage; only a not-yet-committed active lane
  // falls back to the other view.
  const activeHasCommittedRead = enabled &&
    (active.lastLoadedAt !== null ||
      (active.error !== null && !active.stale));
  const coverage = !enabled
    ? null
    : activeHasCommittedRead
      ? active.coverage
      : (fallback.coverage ?? active.coverage);

  return {
    items: active.items,
    total: active.total,
    totalIsExact: active.totalIsExact,
    hasMoreRows: active.hasMoreRows,
    loading: active.loading,
    error: active.error,
    stale: active.stale,
    lastLoadedAt: enabled ? active.lastLoadedAt : null,
    filterOptions,
    filterOptionsLoaded,
    nextChainCursor: chains.nextChainCursor,
    previousChainCursor: chains.previousChainCursor,
    chainPageStart: chains.chainPageStart,
    hasMoreChains: chains.hasMoreChains,
    chains: chains.chains,
    chainPageCounts: chains.chainPageCounts,
    coverage,
    readKind: active.readKind,
    chainRowReads: chains.chainRowReads,
    refresh: active.refresh,
    loadMoreChainRows: chains.loadMoreChainRows,
  };
}
