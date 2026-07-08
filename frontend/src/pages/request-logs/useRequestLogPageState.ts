import { useCallback, useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import {
  DEFAULTS,
  normalizeRequestId,
  parsePageState,
  type StatusFamilyFilter,
  stateToParams,
  type RequestLogPageState,
  type TimeRange,
} from "./queryParams";

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
  const setClientRuleId = useCallback((v: string) => update({ client_rule_id: v }), [update]);
  const setResolvedTargetModelId = useCallback((v: string) => update({ resolved_target_model_id: v }), [update]);
  const setStatusCode = useCallback((v: string) => update({ status_code: v }), [update]);
  const setErrorText = useCallback((v: string) => update({ error_text: v }), [update]);
  const setTimeRange = useCallback((v: TimeRange) => update({ time_range: v }), [update]);
  const setStatusFamily = useCallback((v: StatusFamilyFilter) => update({ status_family: v }), [update]);
  const setLimit = useCallback((v: number) => update({ limit: v, offset: DEFAULTS.offset }), [update]);
  const setOffset = useCallback((v: number) => update({ offset: v }, false), [update]);
  const setRequestId = useCallback(
    (value: string) => update({ request_id: normalizeRequestId(value), selected_request_id: "" }, false),
    [update],
  );

  const selectRequest = useCallback(
    (id: number) => update({ selected_request_id: String(id) }, false),
    [update]
  );

  const clearSelectedRequest = useCallback(
    () => update({ selected_request_id: "" }, false),
    [update]
  );

  const clearRequest = useCallback(
    () => update({ request_id: "", selected_request_id: "" }, false),
    [update]
  );

  const clearFilters = useCallback(() => {
    setSearchParams(stateToParams({
      ...parsePageState(new URLSearchParams()),
      request_id: state.request_id,
      selected_request_id: state.selected_request_id,
    }), { replace: true });
  }, [setSearchParams, state.request_id, state.selected_request_id]);

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
    state.client_rule_id ||
    state.resolved_target_model_id ||
    state.status_code ||
    state.error_text ||
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
    setClientRuleId,
    setResolvedTargetModelId,
    setStatusCode,
    setErrorText,
    setTimeRange,
    setStatusFamily,
    setLimit,
    setOffset,
    setRequestId,
    selectRequest,
    clearSelectedRequest,
    clearRequest,
    clearFilters,
    goToNextPage,
    goToPreviousPage,
  };
}

export type RequestLogPageActions = ReturnType<typeof useRequestLogPageState>;
