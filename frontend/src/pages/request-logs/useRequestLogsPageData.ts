import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  STATS_FROM_TIME_PARAM,
  type RequestLogFilterModelOption,
  type RequestLogFilterEndpointOption,
  type RequestLogFilterClientOption,
  type RequestLogFilterResolvedTargetModelOption,
  type QueryCoverage,
  type RequestLogListItem,
  type RequestLogListResponse,
  type StatsRequestParams,
} from "@/lib/types";
import type { StreamErrorKind, StreamOutcome } from "@/lib/types";
import type {
  ChainIngressItem,
  ChainResponse,
  RequestLogChainRow,
} from "@/lib/types/request-logs";
import type { RequestLogPageState } from "./queryParams";
import { timeRangeToFromTime } from "./queryParams";

export interface FilterOptions {
  models: RequestLogFilterModelOption[];
  endpoints: RequestLogFilterEndpointOption[];
  clients: RequestLogFilterClientOption[];
  resolved_target_models: RequestLogFilterResolvedTargetModelOption[];
}

const EMPTY_FILTER_OPTIONS: FilterOptions = {
  models: [],
  endpoints: [],
  clients: [],
  resolved_target_models: [],
};

function parseOptionalStatusCode(value: string): number | undefined {
  return /^\d+$/.test(value) ? Number(value) : undefined;
}

// Flatten a chain response into attempt rows for the table. Chain items keep
// their retained rows in order; the page uses request_log_id as the key.
function flattenChainItems(response: ChainResponse): RequestLogListItem[] {
  const rows: RequestLogListItem[] = [];
  for (const chain of response.items) {
    for (const row of chain.retained_rows) {
      // Chain rows carry model_id only; when the source row also carries
      // display labels (fixtures/tests) keep them.
      const withLabels = row as RequestLogChainRow & {
        model_label?: string;
        resolved_target_model_label?: string | null;
        api_family?: string;
        output_tokens?: number | null;
        ttft_ms?: number | null;
        completion_duration_ms?: number | null;
        reasoning_effort?: string | null;
        report_currency_symbol?: string | null;
      };
      rows.push({
        request_log_id: row.request_log_id,
        row_kind: row.row_kind,
        ingress_request_id: row.ingress_request_id,
        attempt_number: row.attempt_number,
        attempt_trigger: row.attempt_trigger,
        attempt_result: row.attempt_result,
        is_winner: row.is_winner,
        created_at: row.created_at,
        model_id: row.model_id,
        model_label: withLabels.model_label ?? row.model_id,
        resolved_target_model_id: row.resolved_target_model_id,
        resolved_target_model_label:
          withLabels.resolved_target_model_label ??
          row.resolved_target_model_id,
        caller_client_display: null,
        upstream_client_display: null,
        user_agent_overridden: false,
        api_family:
          (withLabels.api_family as RequestLogListItem["api_family"]) ??
          "openai",
        endpoint_id: row.endpoint_id,
        endpoint_label:
          row.terminal_target_label ??
          `Terminal Target #${row.terminal_target_id ?? "?"}`,
        terminal_target_id: row.terminal_target_id,
        terminal_target_label: row.terminal_target_label,
        terminal_target_configured: row.terminal_target_configured,
        terminal_target_owner_model_id:
          row.terminal_target_owner_model_id ?? null,
        ttft_ms: withLabels.ttft_ms ?? null,
        completion_duration_ms: withLabels.completion_duration_ms ?? null,
        upstream_status_code: row.upstream_status_code,
        gateway_status_code: row.gateway_status_code,
        legacy_status_code: row.legacy_status_code,
        attempt_duration_ms: row.attempt_duration_ms,
        legacy_duration_ms: row.legacy_duration_ms,
        is_stream: row.stream_outcome !== "not_streaming",
        stream_outcome: row.stream_outcome as StreamOutcome,
        stream_error_kind: row.stream_error_kind as StreamErrorKind | null,
        error_source: row.error_source,
        error_code: row.error_code,
        failure_stage: row.failure_stage,
        failure_detail_preview: row.failure_detail_preview,
        failure_detail_source: row.failure_detail_source,
        failure_detail_preview_truncated: row.failure_detail_preview_truncated,
        failure_detail_redacted: row.failure_detail_redacted,
        reasoning_effort: withLabels.reasoning_effort ?? null,
        output_tokens: withLabels.output_tokens ?? null,
        total_tokens: row.total_tokens,
        total_cost_user_currency_micros: row.total_cost_user_currency_micros,
        pricing_status: row.pricing_status,
        pricing_evidence_trust: row.pricing_evidence_trust,
        unpriced_reason: row.unpriced_reason,
        report_currency_symbol: withLabels.report_currency_symbol ?? null,
        proxy_api_key_id: null,
        proxy_api_key_name_snapshot: null,
        proxy_api_key_attribution_state: "unknown",
        proxy_api_key_auth_enforced_at_request: null,
      });
    }
  }
  return rows;
}

function appendUniqueRequestItems(
  current: RequestLogListItem[],
  incoming: RequestLogListItem[],
): RequestLogListItem[] {
  const seen = new Set(current.map((item) => item.request_log_id));
  return [
    ...current,
    ...incoming.filter((item) => {
      if (seen.has(item.request_log_id)) return false;
      seen.add(item.request_log_id);
      return true;
    }),
  ];
}

function mergeChainRowPage(
  current: ChainIngressItem,
  page: ChainIngressItem,
): ChainIngressItem {
  const seen = new Set(current.retained_rows.map((row) => row.request_log_id));
  const retainedRows = [
    ...current.retained_rows,
    ...page.retained_rows.filter((row) => {
      if (seen.has(row.request_log_id)) return false;
      seen.add(row.request_log_id);
      return true;
    }),
  ];

  return {
    ...page,
    retained_rows: retainedRows,
    retained_rows_loaded_count: retainedRows.length,
  };
}

interface UseRequestLogsPageDataParams {
  enabled?: boolean;
  revision: number;
  state: RequestLogPageState;
}

/**
 * A failed list read. `stale` means the rows still on screen came from the last
 * successful read of this same query, so the page may keep them behind a
 * staleness badge; otherwise nothing on screen describes the current query and
 * the page must replace the table with a failure surface. Either way the hook
 * never leaves a zero total and an empty item list behind, because that reads
 * as "no matching requests" instead of "this read failed".
 */
interface RequestLogsLoadFailure {
  message: string;
  stale: boolean;
}

export function useRequestLogsPageData({
  revision,
  state,
  enabled = true,
}: UseRequestLogsPageDataParams) {
  const messages = getStaticMessages();
  const [items, setItems] = useState<RequestLogListItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(enabled);
  const [failure, setFailure] = useState<RequestLogsLoadFailure | null>(null);
  const [lastLoadedAt, setLastLoadedAt] = useState<string | null>(null);
  const [filterOptions, setFilterOptions] =
    useState<FilterOptions>(EMPTY_FILTER_OPTIONS);
  const [endpointOptionsLoaded, setEndpointOptionsLoaded] = useState(false);
  const [nextChainCursor, setNextChainCursor] = useState<string | null>(null);
  const [hasMoreChains, setHasMoreChains] = useState(false);
  const [totalIsExact, setTotalIsExact] = useState(true);
  const [hasMoreRows, setHasMoreRows] = useState(false);
  const [chains, setChains] = useState<ChainIngressItem[]>([]);
  const [chainPageCounts, setChainPageCounts] = useState({
    ingress: 0,
    attempts: 0,
    rows: 0,
  });
  const [coverage, setCoverage] = useState<QueryCoverage | null>(null);

  const fetchIdRef = useRef(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastRevisionRef = useRef<number | null>(null);
  const endpointOptionsLoadedOnceRef = useRef(false);
  const loadedSignatureRef = useRef<string | null>(null);
  const chainQueryParamsRef = useRef<StatsRequestParams | null>(null);
  const chainCursorHistoryRef = useRef(new Map<string, string>());
  const chainPageStartRef = useRef(new Map<string, number>());
  const chainSignatureRef = useRef<string | null>(null);
  const rowLoadsInFlightRef = useRef(new Set<string>());

  useEffect(() => {
    const revisionChanged = lastRevisionRef.current !== revision;
    if (!revisionChanged) {
      return;
    }

    lastRevisionRef.current = revision;
    fetchIdRef.current += 1;
    if (debounceRef.current !== null) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    endpointOptionsLoadedOnceRef.current = false;
  }, [revision]);

  const fetchData = useCallback(() => {
    const id = ++fetchIdRef.current;
    setLoading(true);
    setFailure(null);

    const fromTime =
      state.from_time && state.to_time
        ? state.from_time
        : timeRangeToFromTime(state.time_range);
    const toTime = state.from_time && state.to_time ? state.to_time : undefined;

    const params: StatsRequestParams = {
      time_range:
        state.from_time && state.to_time ? "custom" : state.time_range,
      ingress_final_result: state.ingress_final_result || undefined,
      confirmed_failover: state.confirmed_failover ? "true" : undefined,
      ingress_request_id: state.ingress_request_id || undefined,
      model_id: state.model_id || undefined,
      proxy_api_key_id: state.proxy_api_key_id
        ? parseInt(state.proxy_api_key_id, 10)
        : undefined,
      client_rule_id: state.client_rule_id
        ? parseInt(state.client_rule_id, 10)
        : undefined,
      resolved_target_model_id: state.resolved_target_model_id || undefined,
      status_family:
        state.status_family === "all" ? undefined : state.status_family,
      status_code: parseOptionalStatusCode(state.status_code),
      error_text: state.error_text || undefined,
      pricing_status:
        state.pricing_status === "all" ? undefined : state.pricing_status,
      unpriced_reason:
        state.pricing_status === "unpriced"
          ? state.unpriced_reason || undefined
          : undefined,
      endpoint_id: state.endpoint_id
        ? parseInt(state.endpoint_id, 10)
        : undefined,
      terminal_target_id: state.terminal_target_id
        ? parseInt(state.terminal_target_id, 10)
        : undefined,
      [STATS_FROM_TIME_PARAM]: fromTime,
      to_time: toTime,
      limit: state.limit,
      offset: state.offset,
      view: state.view || undefined,
      chain_cursor: state.chain_cursor || undefined,
      sort_by: state.sort_by,
      sort_order: state.sort_order,
    };

    // Identifies which query the rows on screen describe, at the URL level
    // rather than the wire level: a relative window resolves to a fresh
    // `from_time` on every fetch, so the resolved bounds count only when the
    // operator pinned them. The chain cursor is excluded on purpose — it
    // advances within the same query scope, while a page turn replaces the
    // retained rows on screen. Any other change (filters, view, sort, page)
    // makes the retained rows describe something else, and a failure has
    // nothing to keep.
    const signature = JSON.stringify({
      ...params,
      chain_cursor: undefined,
      [STATS_FROM_TIME_PARAM]: state.from_time || undefined,
      to_time: state.to_time || undefined,
    });

    // A signed chain cursor only advances one query scope. Keep the small
    // client-side predecessor map for the previous-page control, but discard
    // it whenever filters, sorting, or the view changes.
    if (
      state.view !== "ingress_chains" ||
      chainSignatureRef.current !== signature
    ) {
      chainCursorHistoryRef.current.clear();
      chainPageStartRef.current.clear();
      chainSignatureRef.current =
        state.view === "ingress_chains" ? signature : null;
    }

    const load =
      state.view === "ingress_chains"
        ? api.stats.chains(params)
        : api.stats.requests(params);

    load
      .then((res) => {
        if (id !== fetchIdRef.current) return;
        if (state.view === "ingress_chains") {
          const chain = res as unknown as ChainResponse;
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
          // Outer chain pages replace one another. Only the nested row cursor
          // loads append, and only within their own ingress chain.
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
          // Chain views carry owner-issued source coverage in the response;
          // keep it server-truthful so an incomplete chain window gets the
          // same retention warning and Settings handoff as the flat list.
          setCoverage(chain.source_coverage ?? null);
          if (chain.filter_options) {
            const options = chain.filter_options;
            setFilterOptions((prev) => ({
              ...prev,
              models: options.models,
              endpoints: options.endpoints,
              clients: options.clients,
              resolved_target_models: options.resolved_target_models,
            }));
          }
        } else {
          const list = res as RequestLogListResponse;
          setItems(list.items);
          setChains([]);
          setChainPageCounts({ ingress: 0, attempts: 0, rows: 0 });
          setTotal(list.total);
          setTotalIsExact(list.total_is_exact);
          setHasMoreRows(list.has_more);
          setCoverage(list.coverage);
          setNextChainCursor(null);
          setHasMoreChains(false);
          chainQueryParamsRef.current = null;
          setFilterOptions((prev) => ({
            ...prev,
            models: list.filter_options.models,
            endpoints: list.filter_options.endpoints,
            clients: list.filter_options.clients,
            resolved_target_models: list.filter_options.resolved_target_models,
          }));
        }

        loadedSignatureRef.current = signature;
        setLastLoadedAt(new Date().toISOString());

        if (!endpointOptionsLoadedOnceRef.current) {
          endpointOptionsLoadedOnceRef.current = true;
          setEndpointOptionsLoaded(true);
        }
      })
      .catch((err) => {
        if (id !== fetchIdRef.current) return;
        const stale = loadedSignatureRef.current === signature;
        if (!stale) {
          // Drop everything the failed read would otherwise leave behind: an
          // empty list plus a zero total is the rendering of a genuinely empty
          // result, and the page must not say that about a read that failed.
          setItems([]);
          setTotal(0);
          setChains([]);
          setChainPageCounts({ ingress: 0, attempts: 0, rows: 0 });
          setNextChainCursor(null);
          setHasMoreChains(false);
          setCoverage(null);
          chainQueryParamsRef.current = null;
          loadedSignatureRef.current = null;
          setLastLoadedAt(null);
        }
        setFailure({
          message:
            err instanceof Error
              ? err.message
              : messages.requestLogs.loadFailed,
          stale,
        });
      })
      .finally(() => {
        if (id !== fetchIdRef.current) return;
        setLoading(false);
      });
  }, [
    state.ingress_final_result,
    state.confirmed_failover,
    state.ingress_request_id,
    state.model_id,
    state.proxy_api_key_id,
    state.client_rule_id,
    state.resolved_target_model_id,
    state.status_family,
    state.status_code,
    state.error_text,
    state.pricing_status,
    state.unpriced_reason,
    state.endpoint_id,
    state.terminal_target_id,
    state.time_range,
    state.from_time,
    state.to_time,
    state.limit,
    state.offset,
    state.view,
    state.chain_cursor,
    state.sort_by,
    state.sort_order,
    messages.requestLogs.loadFailed,
  ]);

  useEffect(() => {
    if (enabled) {
      return;
    }

    if (debounceRef.current !== null) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }

    fetchIdRef.current += 1;
  }, [enabled]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    void revision;
    if (debounceRef.current !== null) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(fetchData, 300);
    return () => {
      if (debounceRef.current !== null) clearTimeout(debounceRef.current);
    };
  }, [enabled, fetchData, revision]);

  const refresh = useCallback(() => {
    if (!enabled) {
      return;
    }

    fetchData();
  }, [enabled, fetchData]);

  const loadMoreChainRows = useCallback(
    async (ingressRequestId: string, rowCursor: string) => {
      const baseParams = chainQueryParamsRef.current;
      const normalizedIngress = ingressRequestId.trim();
      const normalizedCursor = rowCursor.trim();
      if (!enabled || !baseParams || !normalizedIngress || !normalizedCursor)
        return;

      const loadKey = `${normalizedIngress}:${normalizedCursor}`;
      if (rowLoadsInFlightRef.current.has(loadKey)) return;
      rowLoadsInFlightRef.current.add(loadKey);
      const querySignature = loadedSignatureRef.current;
      setFailure(null);

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
      } catch (err) {
        if (loadedSignatureRef.current !== querySignature) return;
        setFailure({
          message:
            err instanceof Error
              ? err.message
              : messages.requestLogs.loadFailed,
          stale: true,
        });
      } finally {
        rowLoadsInFlightRef.current.delete(loadKey);
      }
    },
    [enabled, messages.requestLogs.loadFailed],
  );

  const filterOptionsLoaded = endpointOptionsLoaded;
  const currentChainCursor = state.chain_cursor || "";
  const previousChainCursor =
    enabled && state.view === "ingress_chains" && currentChainCursor
      ? (chainCursorHistoryRef.current.get(currentChainCursor) ?? "")
      : null;
  const chainPageStart =
    enabled && state.view === "ingress_chains"
      ? currentChainCursor
        ? (chainPageStartRef.current.get(currentChainCursor) ?? null)
        : 0
      : null;

  return {
    items: enabled ? items : [],
    total: enabled ? total : 0,
    totalIsExact: enabled ? totalIsExact : true,
    hasMoreRows: enabled ? hasMoreRows : false,
    loading: enabled ? loading : false,
    error: enabled ? (failure?.message ?? null) : null,
    /** The read failed but the rows on screen are the last successful ones. */
    stale: enabled ? (failure?.stale ?? false) : false,
    lastLoadedAt: enabled ? lastLoadedAt : null,
    filterOptions: enabled ? filterOptions : EMPTY_FILTER_OPTIONS,
    filterOptionsLoaded: enabled ? filterOptionsLoaded : false,
    nextChainCursor: enabled ? nextChainCursor : null,
    previousChainCursor,
    chainPageStart,
    hasMoreChains: enabled ? hasMoreChains : false,
    chains: enabled ? chains : [],
    chainPageCounts: enabled
      ? chainPageCounts
      : { ingress: 0, attempts: 0, rows: 0 },
    coverage: enabled ? coverage : null,
    refresh,
    loadMoreChainRows,
  };
}
