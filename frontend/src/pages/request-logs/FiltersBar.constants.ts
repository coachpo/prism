import { getStaticMessages } from "@/i18n/staticMessages";

export function getTimeLabel(value: string) {
  const copy = getStaticMessages().requestLogs;
  switch (value) {
    case "1h":
      return copy.lastHour;
    case "6h":
      return copy.last6Hours;
    case "24h":
      return copy.last24Hours;
    case "7d":
      return copy.last7Days;
    case "30d":
      return copy.last30Days;
    case "all":
      return copy.requestLogsAllTime;
    default:
      return value;
  }
}

export function getLatencyLabel(value: string) {
  const copy = getStaticMessages().requestLogs;
  switch (value) {
    case "all":
      return copy.anyLatency;
    case "fast":
      return copy.latencyFast;
    case "normal":
      return copy.latencyNormal;
    case "slow":
      return copy.latencySlow;
    case "very_slow":
      return copy.latencyVerySlow;
    default:
      return value;
  }
}

export function getStatusFamilyLabel(value: string) {
  const copy = getStaticMessages().requestLogs;
  switch (value) {
    case "all":
      return copy.allStatuses;
    case "2xx":
      return copy.twoHundredsOnly;
    case "4xx":
      return copy.fourHundredsOnly;
    case "5xx":
      return copy.fiveHundredsOnly;
    default:
      return value;
  }
}

export function getUnpricedReasonLabel(value: string) {
  const copy = getStaticMessages().requestLogs;
  switch (value) {
    case "PRICING_DISABLED":
      return copy.reasonPricingDisabled;
    case "MISSING_TOKEN_USAGE":
      return copy.reasonMissingTokenUsage;
    case "STREAM_USAGE_UNAVAILABLE":
      return copy.reasonStreamUsageUnavailable;
    case "MISSING_PRICE_DATA":
      return copy.reasonMissingPriceData;
    default:
      return value;
  }
}

/**
 * 折叠面板里生效的条件数。面板关着时这个数字是唯一的提示：
 * 深链带进来的条件不能悄悄改变结果集而界面上一句话都不说。
 */
export function countHiddenRequestLogFilters(state: {
  model_id: string;
  resolved_target_model_id: string;
  endpoint_id: string;
  terminal_target_id: string;
  proxy_api_key_id: string;
  client_rule_id: string;
  status_code: string;
  error_text: string;
  unpriced_reason: string;
  pricing_card_role: string;
  pricing_selection_state: string;
  status_family: string;
  pricing_status: string;
}): number {
  return [
    state.model_id,
    state.resolved_target_model_id,
    state.endpoint_id,
    state.terminal_target_id,
    state.proxy_api_key_id,
    state.client_rule_id,
    state.status_code,
    state.error_text,
    state.unpriced_reason,
    state.pricing_card_role,
    state.pricing_selection_state,
    state.status_family !== "all" ? state.status_family : "",
    state.pricing_status !== "all" ? state.pricing_status : "",
  ].filter(Boolean).length;
}
