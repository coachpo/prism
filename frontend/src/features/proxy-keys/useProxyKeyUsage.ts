import { useCallback, useMemo } from "react";
import { useQueries, useQueryClient } from "@tanstack/react-query";

import { api } from "@/lib/api";
import { rewriteQueryKeys } from "@/shared/api/queryKeys";

const USAGE_WINDOW = "7d" as const;

/**
 * Per-key request counts for the ledger's 7-day column.
 *
 * The column has its own read, so it also has its own failure: a failed count
 * renders as a failure, never as `0` and never as a blank cell. `total` stays
 * `null` unless the server actually returned one.
 */
export type ProxyKeyUsageEntry = {
  failed: boolean;
  /** True while no cached value exists yet — the cell shows a Skeleton. */
  loading: boolean;
  /**
   * True while a background re-read is in flight over an already-cached
   * value: the number stays on screen and the cell reports refreshing.
   */
  refreshing: boolean;
  /** Server-reported request count. `null` means the read did not produce one. */
  total: number | null;
  /** `false` when the window could not be fully covered; `null` when unknown. */
  coverageComplete: boolean | null;
};

export type ProxyKeyUsageState = {
  entries: Map<number, ProxyKeyUsageEntry>;
  /** Some key in the visible set failed its usage read. */
  hasFailure: boolean;
  loading: boolean;
  refetch: () => void;
};

/**
 * Reads usage only for the keys currently on screen. The ledger holds up to the
 * server-side key limit, and counting every one of them on every render would
 * cost far more than the column is worth.
 */
export function useProxyKeyUsage(
  keyIds: readonly number[],
): ProxyKeyUsageState {
  const queryClient = useQueryClient();
  const stableIds = useMemo(
    () => [...keyIds].sort((left, right) => left - right),
    [keyIds],
  );

  const results = useQueries({
    queries: stableIds.map((keyId) => ({
      queryKey: rewriteQueryKeys.global.proxyApiKeyUsage(keyId, USAGE_WINDOW),
      queryFn: () =>
        api.stats.requests({
          proxy_api_key_id: keyId,
          time_range: USAGE_WINDOW,
          limit: 1,
        }),
      staleTime: 60_000,
    })),
  });

  const entries = useMemo(() => {
    const next = new Map<number, ProxyKeyUsageEntry>();
    stableIds.forEach((keyId, index) => {
      const result = results[index];
      if (!result) {
        return;
      }
      next.set(keyId, {
        failed: Boolean(result.error),
        loading: result.isPending,
        // A background refetch over a cached value keeps the number on
        // screen; only then does the cell show its refreshing feedback.
        refreshing: Boolean(result.isFetching && !result.isPending),
        total: result.data ? result.data.total : null,
        coverageComplete: result.data ? result.data.coverage.complete : null,
      });
    });
    return next;
  }, [results, stableIds]);

  const hasFailure = useMemo(
    () => results.some((result) => Boolean(result.error)),
    [results],
  );
  const loading = useMemo(
    () => results.some((result) => result.isPending),
    [results],
  );

  const refetch = useCallback(() => {
    stableIds.forEach((keyId) => {
      void queryClient.invalidateQueries({
        queryKey: rewriteQueryKeys.global.proxyApiKeyUsage(keyId, USAGE_WINDOW),
      });
    });
  }, [queryClient, stableIds]);

  return { entries, hasFailure, loading, refetch };
}
