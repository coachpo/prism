import { useCallback, useEffect, useMemo } from "react";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import {
  DEFAULTS,
  normalizeRequestId,
  parsePageSearch,
  parsePageState,
  requestLogStateForView,
  type PricingStatusFilter,
  type PricingCardRoleFilter,
  type PricingSelectionStateFilter,
  type RequestLogView,
  type StatusFamilyFilter,
  stateToParams,
  stateToSearch,
  type RequestLogPageState,
  type TimeRange,
} from "./queryParams";

/**
 * Every write below targets the page the operator is already on — filters,
 * sort, paging, row selection and the canonical-URL rewrite are all in-page
 * state, not navigation. Each therefore opts out of the router's default
 * scroll reset, which would otherwise throw the operator back to the top of a
 * long log page on every filter keystroke and every page turn.
 */
export function useRequestLogPageState() {
  const location = useRouterState({
    select: (routerState) => routerState.location,
  });
  const navigate = useNavigate();
  const state = useMemo(
    () => parsePageSearch(location.search),
    [location.search],
  );

  useEffect(() => {
    if (location.pathname !== "/observe/requests") {
      return;
    }

    const canonicalParams = stateToParams(state);
    if (
      canonicalParams.toString() !==
      new URLSearchParams(location.searchStr).toString()
    ) {
      void navigate({
        to: "/observe/requests",
        search: () => stateToSearch(state),
        replace: true,
        resetScroll: false,
      });
    }
  }, [location.pathname, location.searchStr, navigate, state]);

  const update = useCallback(
    (patch: Partial<RequestLogPageState>, resetOffset = true) => {
      const next = { ...state, ...patch };
      if (resetOffset) {
        if (!("offset" in patch)) next.offset = DEFAULTS.offset;
        if (!("chain_cursor" in patch)) next.chain_cursor = "";
      }
      void navigate({
        to: "/observe/requests",
        search: () => stateToSearch(next),
        replace: true,
        resetScroll: false,
      });
    },
    [navigate, state],
  );

  const setIngressRequestId = useCallback(
    (v: string) => update({ ingress_request_id: v }),
    [update],
  );
  const setModelId = useCallback(
    (v: string) => update({ model_id: v }),
    [update],
  );
  const setEndpointId = useCallback(
    (v: string) => update({ endpoint_id: v }),
    [update],
  );
  const setTerminalTargetId = useCallback(
    (v: string) => update({ terminal_target_id: v }),
    [update],
  );
  const setClientRuleId = useCallback(
    (v: string) => update({ client_rule_id: v }),
    [update],
  );
  const setProxyApiKeyId = useCallback(
    (v: string) => update({ proxy_api_key_id: v }),
    [update],
  );
  const setResolvedTargetModelId = useCallback(
    (v: string) => update({ resolved_target_model_id: v }),
    [update],
  );
  const setStatusCode = useCallback(
    (v: string) => update({ status_code: v }),
    [update],
  );
  const setErrorText = useCallback(
    (v: string) => update({ error_text: v }),
    [update],
  );
  const setPricingStatus = useCallback(
    (v: PricingStatusFilter) =>
      update({
        pricing_status: v,
        unpriced_reason: v === "unpriced" ? state.unpriced_reason : "",
      }),
    [state.unpriced_reason, update],
  );
  const setUnpricedReason = useCallback(
    (v: string) => update({ unpriced_reason: v }),
    [update],
  );
  const setPricingCardRole = useCallback(
    (v: PricingCardRoleFilter | "") => update({ pricing_card_role: v }),
    [update],
  );
  const setPricingSelectionState = useCallback(
    (v: PricingSelectionStateFilter | "") =>
      update({ pricing_selection_state: v }),
    [update],
  );
  const setView = useCallback(
    (v: RequestLogView) => update(requestLogStateForView(state, v), false),
    [state, update],
  );
  const setChainCursor = useCallback(
    (v: string) => update({ chain_cursor: v }, false),
    [update],
  );
  const setSort = useCallback(
    (sortBy: string, sortOrder: "asc" | "desc") =>
      update(
        {
          sort_by: sortBy as RequestLogPageState["sort_by"],
          sort_order: sortOrder,
          chain_cursor: "",
          offset: DEFAULTS.offset,
        },
        false,
      ),
    [update],
  );
  const setIngressFinalResult = useCallback(
    (v: string) =>
      update(
        {
          ingress_final_result:
            v as RequestLogPageState["ingress_final_result"],
          confirmed_failover: false,
        },
        false,
      ),
    [update],
  );
  const setConfirmedFailover = useCallback(
    (v: boolean) =>
      update({ confirmed_failover: v, ingress_final_result: "" }, false),
    [update],
  );
  const clearTriage = useCallback(
    () =>
      update(
        {
          ingress_final_result: "",
          confirmed_failover: false,
          pricing_status: DEFAULTS.pricing_status,
          unpriced_reason: "",
          pricing_card_role: "",
          pricing_selection_state: "",
        },
        false,
      ),
    [update],
  );
  const replaceState = useCallback(
    (next: RequestLogPageState) => {
      void navigate({
        to: "/observe/requests",
        search: () => stateToSearch(next),
        replace: true,
        resetScroll: false,
      });
    },
    [navigate],
  );
  const setTimeRange = useCallback(
    (v: TimeRange) => update({ time_range: v }),
    [update],
  );
  const setStatusFamily = useCallback(
    (v: StatusFamilyFilter) => update({ status_family: v }),
    [update],
  );
  const setLimit = useCallback(
    (v: number) => update({ limit: v, offset: DEFAULTS.offset }),
    [update],
  );
  const setOffset = useCallback(
    (v: number) => update({ offset: v }, false),
    [update],
  );
  const setRequestId = useCallback(
    (value: string) =>
      update(
        { request_id: normalizeRequestId(value), selected_request_id: "" },
        false,
      ),
    [update],
  );

  const selectRequest = useCallback(
    (id: string) => update({ selected_request_id: id }, false),
    [update],
  );

  const clearSelectedRequest = useCallback(
    () => update({ selected_request_id: "" }, false),
    [update],
  );

  const clearRequest = useCallback(
    () => update({ request_id: "", selected_request_id: "" }, false),
    [update],
  );

  const clearFilters = useCallback(() => {
    if (!state.request_id && !state.selected_request_id) {
      void navigate({
        to: "/observe/requests",
        search: {},
        replace: true,
        resetScroll: false,
      });
      return;
    }

    void navigate({
      to: "/observe/requests",
      search: stateToSearch({
        ...parsePageState(new URLSearchParams()),
        request_id: state.request_id,
        selected_request_id: state.selected_request_id,
      }),
      replace: true,
      resetScroll: false,
    });
  }, [navigate, state.request_id, state.selected_request_id]);

  const goToNextPage = useCallback(
    (total: number) => {
      if (state.offset + state.limit < total)
        setOffset(state.offset + state.limit);
    },
    [state.offset, state.limit, setOffset],
  );

  const goToPreviousPage = useCallback(() => {
    if (state.offset > 0) setOffset(Math.max(0, state.offset - state.limit));
  }, [state.offset, state.limit, setOffset]);

  const isExactMode = state.request_id !== "";

  const hasActiveFilters = !!(
    state.ingress_request_id ||
    state.query_context ||
    state.final_result ||
    state.outcome_detail ||
    state.final_status_code ||
    state.final_stream_outcome ||
    state.final_stream_error_kind ||
    state.final_target_model_id ||
    state.final_endpoint_id ||
    state.final_terminal_target_id ||
    state.final_pricing_status ||
    state.final_unpriced_reason ||
    state.reporting_currency_epoch ||
    state.cost_segment_key ||
    state.api_family ||
    state.row_kind ||
    state.attempt_trigger ||
    state.attempt_result ||
    state.model_id ||
    state.endpoint_id ||
    state.terminal_target_id ||
    state.client_rule_id ||
    state.proxy_api_key_id ||
    state.resolved_target_model_id ||
    state.status_code ||
    state.error_text ||
    state.pricing_status !== DEFAULTS.pricing_status ||
    state.ingress_final_result !== DEFAULTS.ingress_final_result ||
    state.confirmed_failover !== DEFAULTS.confirmed_failover ||
    state.unpriced_reason ||
    state.pricing_card_role ||
    state.pricing_selection_state ||
    state.time_range !== DEFAULTS.time_range ||
    state.status_family !== DEFAULTS.status_family ||
    state.view !== DEFAULTS.view
  );

  return {
    state,
    isExactMode,
    hasActiveFilters,
    setIngressRequestId,
    setModelId,
    setEndpointId,
    setTerminalTargetId,
    setClientRuleId,
    setProxyApiKeyId,
    setResolvedTargetModelId,
    setStatusCode,
    setErrorText,
    setPricingStatus,
    setUnpricedReason,
    setPricingCardRole,
    setPricingSelectionState,
    setView,
    setChainCursor,
    setIngressFinalResult,
    setConfirmedFailover,
    clearTriage,
    replaceState,
    setSort,
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
