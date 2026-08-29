import { useCallback, useEffect, useRef, useState } from "react";

import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { QueryCoverage, RequestLogListItem } from "@/lib/types";
import type { ChainIngressItem, ChainResponse } from "@/lib/types/request-logs";
import type { RequestLogPageState } from "./queryParams";
import {
  buildRequestLogQueryParams,
  EMPTY_REQUEST_LOG_FILTER_OPTIONS,
  requestLogQuerySignature,
  type RequestLogFilterOptions,
} from "./requestLogQuery";
import {
  appendUniqueRequestItems,
  flattenChainItems,
  mergeChainRowPage,
} from "./requestLogChainProjection";

interface RequestLogsLoadFailure {
  message: string;
  stale: boolean;
}

export type ChainRowReadState = { pending: boolean; error: string | null };

interface UseRequestLogIngressChainsParams {
  revision: number;
  state: RequestLogPageState;
  enabled: boolean;
}

interface ChainPageCounts {
  ingress: number;
  attempts: number;
  rows: number;
}

export function useRequestLogIngressChains({
  revision,
  state,
  enabled,
}: UseRequestLogIngressChainsParams) {
  const messages = getStaticMessages();
  const [items, setItems] = useState<RequestLogListItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(enabled);
  const [failure, setFailure] = useState<RequestLogsLoadFailure | null>(null);
  const [lastLoadedAt, setLastLoadedAt] = useState<string | null>(null);
  const [filterOptions, setFilterOptions] = useState<RequestLogFilterOptions>(
    EMPTY_REQUEST_LOG_FILTER_OPTIONS,
  );
  const [filterOptionsLoaded, setFilterOptionsLoaded] = useState(false);
  const [nextChainCursor, setNextChainCursor] = useState<string | null>(null);
  const [hasMoreChains, setHasMoreChains] = useState(false);
  const [chains, setChains] = useState<ChainIngressItem[]>([]);
  const [chainPageCounts, setChainPageCounts] = useState<ChainPageCounts>({
    ingress: 0,
    attempts: 0,
    rows: 0,
  });
  const [coverage, setCoverage] = useState<QueryCoverage | null>(null);
  const [readKind, setReadKind] = useState<"initial" | "replace" | "refresh">(
    "initial",
  );
  const [chainRowReads, setChainRowReads] = useState<
    Record<string, ChainRowReadState>
  >({});

  const fetchIdRef = useRef(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastRevisionRef = useRef<number | null>(null);
  const loadedSignatureRef = useRef<string | null>(null);
  const chainSignatureRef = useRef<string | null>(null);
  const chainQueryParamsRef = useRef<ReturnType<typeof buildRequestLogQueryParams> | null>(
    null,
  );
  const chainCursorHistoryRef = useRef(new Map<string, string>());
  const chainPageStartRef = useRef(new Map<string, number>());
  const rowLoadsInFlightRef = useRef(new Set<string>());

  useEffect(() => {
    if (lastRevisionRef.current === revision) return;
    lastRevisionRef.current = revision;
    fetchIdRef.current += 1;
    if (debounceRef.current !== null) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  }, [revision]);

  const fetchChains = useCallback(() => {
    const id = ++fetchIdRef.current;
    setLoading(true);
    setFailure(null);
    const params = buildRequestLogQueryParams(state);
    const signature = requestLogQuerySignature(state, params);
    const previousSignature = loadedSignatureRef.current;
    setReadKind(
      previousSignature === null
        ? "initial"
        : previousSignature === signature
          ? "refresh"
          : "replace",
    );
    if (previousSignature !== signature) setChainRowReads({});
    if (state.view !== "ingress_chains" || chainSignatureRef.current !== signature) {
      chainCursorHistoryRef.current.clear();
      chainPageStartRef.current.clear();
      chainSignatureRef.current =
        state.view === "ingress_chains" ? signature : null;
    }

    api.stats
      .chains(params)
      .then((chain: ChainResponse) => {
        if (id !== fetchIdRef.current) return;
        const currentCursor = state.chain_cursor || "";
        const currentPageStart = currentCursor
          ? (chainPageStartRef.current.get(currentCursor) ?? null)
          : 0;
        if (chain.next_chain_cursor) {
          chainCursorHistoryRef.current.set(
            chain.next_chain_cursor,
            currentCursor,
          );
          if (currentPageStart !== null) {
            chainPageStartRef.current.set(
              chain.next_chain_cursor,
              currentPageStart + chain.page_ingress_count,
            );
          }
        }
        setItems(flattenChainItems(chain));
        setChains(chain.items);
        setTotal(chain.retained_ingress_total);
        setNextChainCursor(chain.next_chain_cursor);
        setHasMoreChains(chain.has_more_chains);
        chainQueryParamsRef.current = {
          ...params,
          chain_cursor: undefined,
          row_cursor: undefined,
        };
        setChainPageCounts({
          ingress: chain.page_ingress_count,
          attempts: chain.page_upstream_attempt_count,
          rows: chain.page_request_log_row_count,
        });
        setCoverage(chain.source_coverage ?? null);
        setFilterOptions({
          models: chain.filter_options.ingress_models,
          endpoints: chain.filter_options.endpoints,
          clients: chain.filter_options.clients,
          resolved_target_models: chain.filter_options.attempt_target_models,
        });
        setFilterOptionsLoaded(true);
        loadedSignatureRef.current = signature;
        setLastLoadedAt(new Date().toISOString());
      })
      .catch((error: unknown) => {
        if (id !== fetchIdRef.current) return;
        const stale = loadedSignatureRef.current === signature;
        if (!stale) {
          setItems([]);
          setChains([]);
          setTotal(0);
          setNextChainCursor(null);
          setHasMoreChains(false);
          setChainPageCounts({ ingress: 0, attempts: 0, rows: 0 });
          setCoverage(null);
          chainQueryParamsRef.current = null;
          loadedSignatureRef.current = null;
          setLastLoadedAt(null);
        }
        setFailure({
          message:
            error instanceof Error
              ? error.message
              : messages.requestLogs.loadFailed,
          stale,
        });
      })
      .finally(() => {
        if (id === fetchIdRef.current) setLoading(false);
      });
  }, [
    messages.requestLogs.loadFailed,
    state,
  ]);

  useEffect(() => {
    if (!enabled) return;
    if (debounceRef.current !== null) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(fetchChains, 300);
    return () => {
      if (debounceRef.current !== null) clearTimeout(debounceRef.current);
    };
  }, [enabled, fetchChains, revision]);

  useEffect(() => {
    if (enabled) return;
    if (debounceRef.current !== null) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    fetchIdRef.current += 1;
  }, [enabled]);

  const refresh = useCallback(() => {
    if (enabled) fetchChains();
  }, [enabled, fetchChains]);

  const loadMoreChainRows = useCallback(
    async (ingressRequestId: string, rowCursor: string) => {
      const baseParams = chainQueryParamsRef.current;
      const normalizedIngress = ingressRequestId.trim();
      const normalizedCursor = rowCursor.trim();
      if (!enabled || !baseParams || !normalizedIngress || !normalizedCursor) {
        return;
      }

      const loadKey = `${normalizedIngress}:${normalizedCursor}`;
      if (rowLoadsInFlightRef.current.has(loadKey)) return;
      rowLoadsInFlightRef.current.add(loadKey);
      const querySignature = loadedSignatureRef.current;
      setChainRowReads((current) => ({
        ...current,
        [normalizedIngress]: { pending: true, error: null },
      }));

      try {
        const response = await api.stats.chains({
          ...baseParams,
          view: "ingress_chains",
          ingress_request_id: normalizedIngress,
          chain_limit: 1,
          chain_cursor: undefined,
          row_cursor: normalizedCursor,
        });
        if (loadedSignatureRef.current !== querySignature) return;

        const page = response.items.find(
          (item) => item.ingress_request_id === normalizedIngress,
        );
        if (
          !page ||
          (!page.retained_rows_page_complete &&
            page.next_row_cursor === normalizedCursor)
        ) {
          throw new Error(messages.requestLogs.loadFailed);
        }

        setChains((current) =>
          current.map((chain) =>
            chain.ingress_request_id === normalizedIngress
              ? mergeChainRowPage(chain, page)
              : chain,
          ),
        );
        setItems((current) =>
          appendUniqueRequestItems(current, flattenChainItems(response)),
        );
        setLastLoadedAt(new Date().toISOString());
        setChainRowReads((current) => ({
          ...current,
          [normalizedIngress]: { pending: false, error: null },
        }));
      } catch (error) {
        if (loadedSignatureRef.current !== querySignature) return;
        setChainRowReads((current) => ({
          ...current,
          [normalizedIngress]: {
            pending: false,
            error:
              error instanceof Error
                ? error.message
                : messages.requestLogs.loadFailed,
          },
        }));
      } finally {
        rowLoadsInFlightRef.current.delete(loadKey);
      }
    },
    [enabled, messages.requestLogs.loadFailed],
  );

  const currentChainCursor = state.chain_cursor || "";
  const previousChainCursor =
    enabled && currentChainCursor
      ? (chainCursorHistoryRef.current.get(currentChainCursor) ?? "")
      : null;
  const chainPageStart = enabled
    ? currentChainCursor
      ? (chainPageStartRef.current.get(currentChainCursor) ?? null)
      : 0
    : null;

  return {
    chainPageCounts: enabled
      ? chainPageCounts
      : { ingress: 0, attempts: 0, rows: 0 },
    chainRowReads: enabled ? chainRowReads : {},
    chains: enabled ? chains : [],
    // Committed metadata remains available to the page coordinator while this
    // chain lane is inactive; rows, loading, errors, and actions remain local.
    coverage,
    error: enabled ? (failure?.message ?? null) : null,
    filterOptions,
    filterOptionsLoaded,
    hasMoreChains: enabled ? hasMoreChains : false,
    hasMoreRows: false,
    items: enabled ? items : [],
    lastLoadedAt,
    loading: enabled ? loading : false,
    loadMoreChainRows,
    nextChainCursor: enabled ? nextChainCursor : null,
    previousChainCursor,
    chainPageStart,
    readKind: enabled ? readKind : "initial",
    refresh,
    stale: enabled ? (failure?.stale ?? false) : false,
    total: enabled ? total : 0,
    totalIsExact: true,
  };
}
