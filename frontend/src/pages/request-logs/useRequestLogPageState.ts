import { useCallback, useEffect, useMemo } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import {
  DEFAULTS,
  normalizeRequestId,
  parsePageSearch,
  parsePageState,
  type PricedFilter,
  type StatusFamilyFilter,
  stateToParams,
  stateToSearch,
  type RequestLogPageState,
  type TimeRange,
} from "./queryParams";

export function useRequestLogPageState() {
  const location = useRouterState({ select: (routerState) => routerState.location });
  const navigate = useNavigate();
  const state = useMemo(() => parsePageSearch(location.search), [location.search]);

  useEffect(() => {
    if (location.pathname !== "/observe/requests") {
      return;
    }

    const canonicalParams = stateToParams(state);
    if (canonicalParams.toString() !== new URLSearchParams(location.searchStr).toString()) {
      void navigate({ to: "/observe/requests", search: () => stateToSearch(state), replace: true });
    }
  }, [location.pathname, location.searchStr, navigate, state]);

  const update = useCallback(
    (patch: Partial<RequestLogPageState>, resetOffset = true) => {
      const next = { ...state, ...patch };
      if (resetOffset && !("offset" in patch)) next.offset = DEFAULTS.offset;
      void navigate({ to: "/observe/requests", search: () => stateToSearch(next), replace: true });
    },
    [navigate, state]
  );

  const setIngressRequestId = useCallback((v: string) => update({ ingress_request_id: v }), [update]);
  const setModelId = useCallback((v: string) => update({ model_id: v }), [update]);
  const setEndpointId = useCallback((v: string) => update({ endpoint_id: v }), [update]);
  const setClientRuleId = useCallback((v: string) => update({ client_rule_id: v }), [update]);
  const setResolvedTargetModelId = useCallback((v: string) => update({ resolved_target_model_id: v }), [update]);
  const setStatusCode = useCallback((v: string) => update({ status_code: v }), [update]);
  const setErrorText = useCallback((v: string) => update({ error_text: v }), [update]);
  const setPriced = useCallback((v: PricedFilter) => update({ priced: v, unpriced_reason: v === "false" ? state.unpriced_reason : "" }), [state.unpriced_reason, update]);
  const setUnpricedReason = useCallback((v: string) => update({ unpriced_reason: v }), [update]);
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
    if (!state.request_id && !state.selected_request_id) {
      void navigate({ to: "/observe/requests", search: {}, replace: true });
      return;
    }

    void navigate({ to: "/observe/requests", search: stateToSearch({
      ...parsePageState(new URLSearchParams()),
      request_id: state.request_id,
      selected_request_id: state.selected_request_id,
    }), replace: true });
  }, [navigate, state.request_id, state.selected_request_id]);

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
    state.priced !== DEFAULTS.priced ||
    state.unpriced_reason ||
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
    setPriced,
    setUnpricedReason,
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
