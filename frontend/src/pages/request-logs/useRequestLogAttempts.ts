import { useCallback, useEffect, useRef, useState } from "react";

import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  QueryCoverage,
  RequestLogListItem,
  RequestLogListResponse,
} from "@/lib/types";
import type { RequestLogPageState } from "./queryParams";
import {
  buildRequestLogQueryParams,
  EMPTY_REQUEST_LOG_FILTER_OPTIONS,
  requestLogQuerySignature,
  type RequestLogFilterOptions,
} from "./requestLogQuery";

interface RequestLogsLoadFailure {
  message: string;
  stale: boolean;
}

interface UseRequestLogAttemptsParams {
  revision: number;
  state: RequestLogPageState;
  enabled: boolean;
}

export function useRequestLogAttempts({
  revision,
  state,
  enabled,
}: UseRequestLogAttemptsParams) {
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
  const [totalIsExact, setTotalIsExact] = useState(true);
  const [hasMoreRows, setHasMoreRows] = useState(false);
  const [coverage, setCoverage] = useState<QueryCoverage | null>(null);
  const [readKind, setReadKind] = useState<"initial" | "replace" | "refresh">(
    "initial",
  );
  const fetchIdRef = useRef(0);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastRevisionRef = useRef<number | null>(null);
  const loadedSignatureRef = useRef<string | null>(null);

  useEffect(() => {
    if (lastRevisionRef.current === revision) return;
    lastRevisionRef.current = revision;
    fetchIdRef.current += 1;
    if (debounceRef.current !== null) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  }, [revision]);

  const fetchAttempts = useCallback(() => {
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

    api.stats
      .requests(params)
      .then((response: RequestLogListResponse) => {
        if (id !== fetchIdRef.current) return;
        setItems(response.items);
        setTotal(response.total);
        setTotalIsExact(response.total_is_exact);
        setHasMoreRows(response.has_more);
        setCoverage(response.coverage);
        setFilterOptions({
          models: response.filter_options.ingress_models,
          endpoints: response.filter_options.endpoints,
          clients: response.filter_options.clients,
          resolved_target_models: response.filter_options.attempt_target_models,
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
          setTotal(0);
          setHasMoreRows(false);
          setCoverage(null);
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
    debounceRef.current = setTimeout(fetchAttempts, 300);
    return () => {
      if (debounceRef.current !== null) clearTimeout(debounceRef.current);
    };
  }, [enabled, fetchAttempts, revision]);

  useEffect(() => {
    if (enabled) return;
    if (debounceRef.current !== null) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    fetchIdRef.current += 1;
  }, [enabled]);

  const refresh = useCallback(() => {
    if (enabled) fetchAttempts();
  }, [enabled, fetchAttempts]);

  return {
    // Committed metadata remains available to the page coordinator while this
    // row lane is inactive; only the active lane's rows and read controls are
    // masked by `enabled`.
    coverage,
    error: enabled ? (failure?.message ?? null) : null,
    filterOptions,
    filterOptionsLoaded,
    hasMoreRows: enabled ? hasMoreRows : false,
    items: enabled ? items : [],
    lastLoadedAt,
    loading: enabled ? loading : false,
    readKind: enabled ? readKind : "initial",
    refresh,
    stale: enabled ? (failure?.stale ?? false) : false,
    total: enabled ? total : 0,
    totalIsExact: enabled ? totalIsExact : true,
  };
}
