import { useCallback, useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import {
  DEFAULTS,
  parsePageState,
  type StatusFamilyFilter,
  stateToParams,
  type DetailTab,
  type RequestLogPageState,
  type TimeRange,
} from "./queryParams";

function normalizeRequestId(value: string) {
  return value.trim().replace(/^#/, "");
}

export function useRequestLogPageState() {
  const [searchParams, setSearchParams] = useSearchParams();
  const state = useMemo(() => parsePageState(searchParams), [searchParams]);

  useEffect(() => {
    const canonicalParams = stateToParams(state);
    if (canonicalParams.toString() !== searchParams.toString()) {
      setSearchParams(canonicalParams, { replace: true });
    }
  }, [searchParams, setSearchParams, state]);

  const update = useCallback(
    (patch: Partial<RequestLogPageState>, resetOffset = true) => {
      setSearchParams(
        (prev) => {
          const current = parsePageState(prev);
          const next = { ...current, ...patch };
          if (resetOffset && !("offset" in patch)) next.offset = DEFAULTS.offset;
          return stateToParams(next);
        },
        { replace: true }
      );
    },
    [setSearchParams]
  );

  const setIngressRequestId = useCallback((v: string) => update({ ingress_request_id: v }), [update]);
  const setModelId = useCallback((v: string) => update({ model_id: v }), [update]);
  const setEndpointId = useCallback((v: string) => update({ endpoint_id: v }), [update]);
  const setTimeRange = useCallback((v: TimeRange) => update({ time_range: v }), [update]);
  const setStatusFamily = useCallback((v: StatusFamilyFilter) => update({ status_family: v }), [update]);
  const setLimit = useCallback((v: number) => update({ limit: v, offset: DEFAULTS.offset }), [update]);
  const setOffset = useCallback((v: number) => update({ offset: v }, false), [update]);
  const setRequestId = useCallback(
    (value: string) =>
      update(
        {
          request_id: normalizeRequestId(value),
          detail_tab: DEFAULTS.detail_tab,
        },
        false,
      ),
    [update],
  );

  const selectRequest = useCallback(
    (id: number, tab: DetailTab = "overview") => update({ request_id: String(id), detail_tab: tab }, false),
    [update]
  );

  const clearRequest = useCallback(
    () => update({ request_id: "", detail_tab: DEFAULTS.detail_tab }, false),
    [update]
  );

  const setDetailTab = useCallback(
    (tab: DetailTab) => update({ detail_tab: tab }, false),
    [update]
  );

  const clearFilters = useCallback(() => {
    setSearchParams(stateToParams({
      ...parsePageState(new URLSearchParams()),
      request_id: state.request_id,
      detail_tab: state.detail_tab,
    }), { replace: true });
  }, [setSearchParams, state.request_id, state.detail_tab]);

  const goToNextPage = useCallback(
    (total: number) => {
      if (state.offset + state.limit < total) setOffset(state.offset + state.limit);
    },
    [state.offset, state.limit, setOffset]
  );

  const goToPreviousPage = useCallback(() => {
    if (state.offset > 0) setOffset(Math.max(0, state.offset - state.limit));
  }, [state.offset, state.limit, setOffset]);

  const isExactMode = state.request_id !== "";

  const hasActiveFilters = !!(
    state.ingress_request_id ||
    state.model_id ||
    state.endpoint_id ||
    state.time_range !== DEFAULTS.time_range ||
    state.status_family !== DEFAULTS.status_family
  );

  return {
    state,
    isExactMode,
    hasActiveFilters,
    setIngressRequestId,
    setModelId,
    setEndpointId,
    setTimeRange,
    setStatusFamily,
    setLimit,
    setOffset,
    setRequestId,
    selectRequest,
    clearRequest,
    setDetailTab,
    clearFilters,
    goToNextPage,
    goToPreviousPage,
  };
}

export type RequestLogPageActions = ReturnType<typeof useRequestLogPageState>;
