import {
  STATS_FROM_TIME_PARAM,
  type RequestLogFilterClientOption,
  type RequestLogFilterEndpointOption,
  type RequestLogFilterModelOption,
  type RequestLogFilterResolvedTargetModelOption,
  type StatsRequestParams,
} from "@/lib/types";
import {
  splitRequestFilterValues,
  type RequestLogPageState,
} from "./queryParams";

export interface RequestLogFilterOptions {
  models: RequestLogFilterModelOption[];
  endpoints: RequestLogFilterEndpointOption[];
  clients: RequestLogFilterClientOption[];
  resolved_target_models: RequestLogFilterResolvedTargetModelOption[];
}

export const EMPTY_REQUEST_LOG_FILTER_OPTIONS: RequestLogFilterOptions = {
  models: [],
  endpoints: [],
  clients: [],
  resolved_target_models: [],
};

export function buildRequestLogTimeParams(
  state: RequestLogPageState,
): Partial<StatsRequestParams> {
  const hasQueryContext = Boolean(state.query_context);
  const isCustom = Boolean(state.from_time && state.to_time);
  // Preset only sends time_range; custom only sends paired boundaries;
  // signed query_context window is authoritative and suppresses browser time.
  if (hasQueryContext) return {};
  if (isCustom) {
    return {
      time_range: "custom",
      [STATS_FROM_TIME_PARAM]: state.from_time,
      to_time: state.to_time,
    };
  }
  if (state.ingress_request_id && state.time_range === "24h") return {};
  return { time_range: state.time_range };
}

export function buildRequestLogFilterParams(
  state: RequestLogPageState,
): Partial<StatsRequestParams> {
  const finalResults = splitRequestFilterValues(state.final_result) as
    | StatsRequestParams["final_result"]
    | undefined;
  const attemptTriggers = splitRequestFilterValues(state.attempt_trigger) as
    | StatsRequestParams["attempt_trigger"]
    | undefined;
  const attemptResults = splitRequestFilterValues(state.attempt_result) as
    | StatsRequestParams["attempt_result"]
    | undefined;

  return {
    ingress_final_result: state.ingress_final_result || undefined,
    confirmed_failover: state.confirmed_failover ? "true" : undefined,
    query_context: state.query_context || undefined,
    final_result: finalResults,
    outcome_detail: splitRequestFilterValues(state.outcome_detail),
    final_status_code: splitRequestFilterValues(state.final_status_code),
    final_stream_outcome: splitRequestFilterValues(
      state.final_stream_outcome,
    ),
    final_stream_error_kind: splitRequestFilterValues(
      state.final_stream_error_kind,
    ),
    final_exclude: splitRequestFilterValues(state.final_exclude),
    final_target_model_id: splitRequestFilterValues(
      state.final_target_model_id,
    ),
    final_endpoint_id: splitRequestFilterValues(state.final_endpoint_id),
    final_terminal_target_id: splitRequestFilterValues(
      state.final_terminal_target_id,
    ),
    final_pricing_status:
      (state.final_pricing_status as StatsRequestParams["final_pricing_status"]) ||
      undefined,
    final_unpriced_reason: splitRequestFilterValues(
      state.final_unpriced_reason,
    ),
    reporting_currency_epoch: state.reporting_currency_epoch || undefined,
    cost_segment_key:
      state.view === "ingress_chains"
        ? state.cost_segment_key || undefined
        : undefined,
    attempt_trigger: attemptTriggers,
    attempt_result: attemptResults,
    ingress_request_id: state.ingress_request_id || undefined,
    ingress_model_id: state.model_id || undefined,
    proxy_api_key_id: state.proxy_api_key_id || undefined,
    client_rule_id: state.client_rule_id || undefined,
    attempt_target_model_id: splitRequestFilterValues(
      state.resolved_target_model_id,
    ),
    api_family: splitRequestFilterValues(state.api_family),
    row_kind: splitRequestFilterValues(
      state.row_kind,
    ) as StatsRequestParams["row_kind"],
    status_family:
      state.status_family === "all" ? undefined : state.status_family,
    status_code: splitRequestFilterValues(state.status_code),
    stream_outcome: splitRequestFilterValues(state.stream_outcome),
    stream_error_kind: splitRequestFilterValues(state.stream_error_kind),
    error_text: state.error_text || undefined,
    pricing_status:
      state.pricing_status === "all" ? undefined : state.pricing_status,
    unpriced_reason:
      state.pricing_status === "unpriced"
        ? state.unpriced_reason || undefined
        : undefined,
    pricing_card_role: state.pricing_card_role || undefined,
    pricing_selection_state: state.pricing_selection_state || undefined,
    endpoint_id: splitRequestFilterValues(state.endpoint_id),
    terminal_target_id: splitRequestFilterValues(state.terminal_target_id),
  };
}

export function buildRequestLogQueryParams(
  state: RequestLogPageState,
): StatsRequestParams {
  const isChainView = state.view === "ingress_chains";
  const paginationFields: Partial<StatsRequestParams> = isChainView
    ? {
        chain_cursor: state.chain_cursor || undefined,
        chain_limit: state.chain_limit,
      }
    : {
        limit: state.limit,
        offset: state.offset,
      };
  return {
    ...buildRequestLogTimeParams(state),
    ...buildRequestLogFilterParams(state),
    ...paginationFields,
    view: state.view || undefined,
    sort_by: state.sort_by,
    sort_order: state.sort_order,
  } as StatsRequestParams;
}

export function requestLogQuerySignature(
  _state: RequestLogPageState,
  params: StatsRequestParams,
) {
  return JSON.stringify({
    ...params,
    chain_cursor: undefined,
  });
}
