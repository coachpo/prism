import {
  STATS_FROM_TIME_PARAM,
  type RequestLogFilterClientOption,
  type RequestLogFilterEndpointOption,
  type RequestLogFilterModelOption,
  type RequestLogFilterResolvedTargetModelOption,
  type StatsRequestParams,
} from "@/lib/types";
import {
  timeRangeToFromTime,
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

export function buildRequestLogQueryParams(
  state: RequestLogPageState,
): StatsRequestParams {
  const fromTime =
    state.from_time && state.to_time
      ? state.from_time
      : timeRangeToFromTime(state.time_range);
  const toTime = state.from_time && state.to_time ? state.to_time : undefined;

  return {
    time_range:
      state.from_time && state.to_time ? "custom" : state.time_range,
    ingress_final_result: state.ingress_final_result || undefined,
    confirmed_failover: state.confirmed_failover ? "true" : undefined,
    query_context: state.query_context || undefined,
    final_result:
      (state.final_result as StatsRequestParams["final_result"]) || undefined,
    final_target_model_id: state.final_target_model_id || undefined,
    final_endpoint_id: state.final_endpoint_id
      ? parseInt(state.final_endpoint_id, 10)
      : undefined,
    final_terminal_target_id: state.final_terminal_target_id
      ? parseInt(state.final_terminal_target_id, 10)
      : undefined,
    final_pricing_status:
      (state.final_pricing_status as StatsRequestParams["final_pricing_status"]) || undefined,
    final_unpriced_reason: state.final_unpriced_reason || undefined,
    ingress_request_id: state.ingress_request_id || undefined,
    ingress_model_id: state.model_id || undefined,
    proxy_api_key_id: state.proxy_api_key_id
      ? parseInt(state.proxy_api_key_id, 10)
      : undefined,
    client_rule_id: state.client_rule_id
      ? parseInt(state.client_rule_id, 10)
      : undefined,
    attempt_target_model_id: state.resolved_target_model_id || undefined,
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
    pricing_card_role: state.pricing_card_role || undefined,
    pricing_selection_state: state.pricing_selection_state || undefined,
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
}

export function requestLogQuerySignature(
  state: RequestLogPageState,
  params: StatsRequestParams,
) {
  return JSON.stringify({
    ...params,
    chain_cursor: undefined,
    [STATS_FROM_TIME_PARAM]: state.from_time || undefined,
    to_time: state.to_time || undefined,
  });
}

function parseOptionalStatusCode(value: string): number | undefined {
  return /^\d+$/.test(value) ? Number(value) : undefined;
}
