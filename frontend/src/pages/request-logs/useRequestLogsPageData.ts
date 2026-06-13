import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  STATS_FROM_TIME_PARAM,
  type RequestLogFilterModelOption,
  type RequestLogFilterEndpointOption,
  type RequestLogFilterClientOption,
  type RequestLogFilterResolvedTargetModelOption,
  type RequestLogListItem,
} from "@/lib/types";
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

    const fromTime = timeRangeToFromTime(state.time_range);

    const params = {
      ingress_request_id: state.ingress_request_id || undefined,
      model_id: state.model_id || undefined,
      client_rule_id: state.client_rule_id ? parseInt(state.client_rule_id, 10) : undefined,
      resolved_target_model_id: state.resolved_target_model_id || undefined,
      status_family: state.status_family === "all" ? undefined : state.status_family,
      endpoint_id: state.endpoint_id ? parseInt(state.endpoint_id, 10) : undefined,
      [STATS_FROM_TIME_PARAM]: fromTime,
      limit: state.limit,
      offset: state.offset,
    };

    api.stats
      .requests(params)
      .then((res) => {
        if (id !== fetchIdRef.current) return;
        setItems(res.items);
        setTotal(res.total);
        setFilterOptions((prev) => ({
          ...prev,
          models: res.filter_options.models,
          endpoints: res.filter_options.endpoints,
          clients: res.filter_options.clients,
          resolved_target_models: res.filter_options.resolved_target_models,
        }));

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
    state.ingress_request_id,
    state.model_id,
    state.client_rule_id,
    state.resolved_target_model_id,
    state.status_family,
    state.endpoint_id,
    state.time_range,
    state.limit,
    state.offset,
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
    refresh,
  };
}
