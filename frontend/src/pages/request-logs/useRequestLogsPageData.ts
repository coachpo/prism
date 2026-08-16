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
import type { ChainIngressItem, ChainResponse, RequestLogRowV2 } from "@/lib/types/request-logs-v2";
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
      const withLabels = row as RequestLogRowV2 & { model_label?: string; resolved_target_model_label?: string | null; api_family?: string; output_tokens?: number | null; ttft_ms?: number | null; completion_duration_ms?: number | null; reasoning_effort?: string | null; report_currency_symbol?: string | null };
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
        resolved_target_model_label: withLabels.resolved_target_model_label ?? row.resolved_target_model_id,
        caller_client_display: null,
        upstream_client_display: null,
        user_agent_overridden: false,
        api_family: (withLabels.api_family as RequestLogListItem["api_family"]) ?? "openai",
        endpoint_id: row.endpoint_id,
        endpoint_label: row.terminal_target_label ?? `Terminal Target #${row.terminal_target_id ?? "?"}`,
        terminal_target_id: row.terminal_target_id,
        terminal_target_label: row.terminal_target_label,
        terminal_target_configured: row.terminal_target_configured,
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
        total_cost_user_currency_micros: row.total_cost_user_currency_micros !== null ? Number(row.total_cost_user_currency_micros) : null,
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

interface UseRequestLogsPageDataParams {
  enabled?: boolean;
  revision: number;
  state: RequestLogPageState;
}

export function useRequestLogsPageData({ revision, state, enabled = true }: UseRequestLogsPageDataParams) {
  const messages = getStaticMessages();
  const [items, setItems] = useState<RequestLogListItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | null>(null);
  const [filterOptions, setFilterOptions] = useState<FilterOptions>(EMPTY_FILTER_OPTIONS);
  const [endpointOptionsLoaded, setEndpointOptionsLoaded] = useState(false);
  const [nextChainCursor, setNextChainCursor] = useState<string | null>(null);
  const [hasMoreChains, setHasMoreChains] = useState(false);
  const [chains, setChains] = useState<ChainIngressItem[]>([]);
  const [chainPageCounts, setChainPageCounts] = useState({ ingress: 0, attempts: 0, rows: 0 });
  const [coverage, setCoverage] = useState<QueryCoverage | null>(null);

  const fetchIdRef = useRef(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastRevisionRef = useRef<number | null>(null);
  const endpointOptionsLoadedOnceRef = useRef(false);

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
    setError(null);

    const fromTime = state.from_time && state.to_time ? state.from_time : timeRangeToFromTime(state.time_range);
    const toTime = state.from_time && state.to_time ? state.to_time : undefined;

    const params: StatsRequestParams = {
      time_range: state.from_time && state.to_time ? "custom" : state.time_range,
      ingress_final_result: state.ingress_final_result || undefined,
      confirmed_failover: state.confirmed_failover ? "true" : undefined,
      ingress_request_id: state.ingress_request_id || undefined,
      model_id: state.model_id || undefined,
      proxy_api_key_id: state.proxy_api_key_id ? parseInt(state.proxy_api_key_id, 10) : undefined,
      client_rule_id: state.client_rule_id ? parseInt(state.client_rule_id, 10) : undefined,
      resolved_target_model_id: state.resolved_target_model_id || undefined,
      status_family: state.status_family === "all" ? undefined : state.status_family,
      status_code: parseOptionalStatusCode(state.status_code),
      error_text: state.error_text || undefined,
      pricing_status: state.pricing_status === "all" ? undefined : state.pricing_status,
      unpriced_reason: state.pricing_status === "unpriced" ? state.unpriced_reason || undefined : undefined,
      endpoint_id: state.endpoint_id ? parseInt(state.endpoint_id, 10) : undefined,
      terminal_target_id: state.terminal_target_id ? parseInt(state.terminal_target_id, 10) : undefined,
      [STATS_FROM_TIME_PARAM]: fromTime,
      to_time: toTime,
      limit: state.limit,
      offset: state.offset,
      view: state.view || undefined,
      chain_cursor: state.chain_cursor || undefined,
      sort_by: state.sort_by,
      sort_order: state.sort_order,
    };

    const load = state.view === "ingress_chains"
      ? api.stats.chains(params)
      : api.stats.requests(params);

    load
      .then((res) => {
        if (id !== fetchIdRef.current) return;
        if (state.view === "ingress_chains") {
          const chain = res as unknown as ChainResponse;
          setItems(flattenChainItems(chain));
          setChains((current) => state.chain_cursor ? [...current, ...chain.items] : chain.items);
          setTotal(chain.retained_ingress_total);
          setNextChainCursor(chain.next_chain_cursor);
          setHasMoreChains(chain.has_more_chains);
          setChainPageCounts((current) => state.chain_cursor ? {
            ingress: current.ingress + chain.page_ingress_count,
            attempts: current.attempts + chain.page_upstream_attempt_count,
            rows: current.rows + chain.page_request_log_row_count,
          } : {
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
          setCoverage(list.coverage);
          setNextChainCursor(null);
          setHasMoreChains(false);
          setFilterOptions((prev) => ({
            ...prev,
            models: list.filter_options.models,
            endpoints: list.filter_options.endpoints,
            clients: list.filter_options.clients,
            resolved_target_models: list.filter_options.resolved_target_models,
          }));
        }

        if (!endpointOptionsLoadedOnceRef.current) {
          endpointOptionsLoadedOnceRef.current = true;
          setEndpointOptionsLoaded(true);
        }
      })
      .catch((err) => {
        if (id !== fetchIdRef.current) return;
        setError(err instanceof Error ? err.message : messages.requestLogs.loadFailed);
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

  const filterOptionsLoaded = endpointOptionsLoaded;

  return {
    items: enabled ? items : [],
    total: enabled ? total : 0,
    loading: enabled ? loading : false,
    error: enabled ? error : null,
    filterOptions: enabled ? filterOptions : EMPTY_FILTER_OPTIONS,
    filterOptionsLoaded: enabled ? filterOptionsLoaded : false,
    nextChainCursor: enabled ? nextChainCursor : null,
    hasMoreChains: enabled ? hasMoreChains : false,
    chains: enabled ? chains : [],
    chainPageCounts: enabled ? chainPageCounts : { ingress: 0, attempts: 0, rows: 0 },
    coverage: enabled ? coverage : null,
    refresh,
  };
}
