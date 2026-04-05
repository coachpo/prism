import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  Endpoint,
  ModelConfigListItem,
  RequestLogListItem,
} from "@/lib/types";
import type { RequestLogPageState } from "./queryParams";
import { timeRangeToFromTime } from "./queryParams";

export interface FilterOptions {
  models: ModelConfigListItem[];
  endpoints: Endpoint[];
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
  const [filterOptions, setFilterOptions] = useState<FilterOptions>({
    models: [],
    endpoints: [],
  });
  const [filterOptionsLoaded, setFilterOptionsLoaded] = useState(false);

  const fetchIdRef = useRef(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const bootstrapFilterOptions = useCallback(async () => {
    const [modelsResult, endpointsResult] =
      await Promise.allSettled([
        api.models.list(),
        api.endpoints.list(),
      ]);

    const models = modelsResult.status === "fulfilled" ? modelsResult.value : [];
    const endpoints = endpointsResult.status === "fulfilled" ? endpointsResult.value : [];

    setFilterOptions({ models, endpoints });
    setFilterOptionsLoaded(true);
  }, []);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    void revision;
    const bootstrapId = setTimeout(() => {
      void bootstrapFilterOptions();
    }, 0);

    return () => {
      clearTimeout(bootstrapId);
    };
  }, [bootstrapFilterOptions, enabled, revision]);

  const fetchData = useCallback(() => {
    const id = ++fetchIdRef.current;
    setLoading(true);
    setError(null);

    const fromTime = timeRangeToFromTime(state.time_range);

    const params = {
      ingress_request_id: state.ingress_request_id || undefined,
      model_id: state.model_id || undefined,
      status_family: state.status_family === "all" ? undefined : state.status_family,
      endpoint_id: state.endpoint_id ? parseInt(state.endpoint_id, 10) : undefined,
      from_time: fromTime,
      limit: state.limit,
      offset: state.offset,
    };

    api.stats
      .requests(params)
      .then((res) => {
        if (id !== fetchIdRef.current) return;
        setItems(res.items);
        setTotal(res.total);
      })
      .catch((err) => {
        if (id !== fetchIdRef.current) return;
        setError(err instanceof Error ? err.message : messages.requestLogs.loadFailed);
        setItems([]);
        setTotal(0);
      })
      .finally(() => {
        if (id !== fetchIdRef.current) return;
        setLoading(false);
      });
  }, [
    state.ingress_request_id,
    state.model_id,
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
    setItems([]);
    setTotal(0);
    setError(null);
    setLoading(false);
    setFilterOptions({ models: [], endpoints: [] });
    setFilterOptionsLoaded(false);
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

  return {
    items,
    total,
    loading,
    error,
    filterOptions,
    filterOptionsLoaded,
    refresh,
  };
}
