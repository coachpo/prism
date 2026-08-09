export const TIME_RANGE_OPTIONS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type TimeRange = (typeof TIME_RANGE_OPTIONS)[number];

export const STATUS_FAMILY_OPTIONS = ["all", "2xx", "4xx", "5xx"] as const;
export type StatusFamilyFilter = (typeof STATUS_FAMILY_OPTIONS)[number];

export const STATUS_ALIAS_OPTIONS = ["all", "success", "client_error", "error"] as const;
export type StatusAliasFilter = (typeof STATUS_ALIAS_OPTIONS)[number];

export const PRICED_OPTIONS = ["all", "true", "false"] as const;
export type PricedFilter = (typeof PRICED_OPTIONS)[number];

export const UNPRICED_REASON_OPTIONS = [
  "PRICING_DISABLED",
  "MISSING_TOKEN_USAGE",
  "STREAM_USAGE_UNAVAILABLE",
  "MISSING_PRICE_DATA",
] as const;
export type UnpricedReasonFilter = (typeof UNPRICED_REASON_OPTIONS)[number];

export const PAGE_SIZE_OPTIONS = [100, 300, 500] as const;

export const DEFAULTS = {
  limit: 100,
  offset: 0,
  // ponytail: fixed 24h default, no per-user persistence for request logs - localStorage only if 24h still annoys.
  time_range: "24h" as TimeRange,
  status_family: "all" as StatusFamilyFilter,
  priced: "all" as PricedFilter,
  unpriced_reason: "",
} as const;

export function statusAliasToFamily(value: StatusAliasFilter | string | null): StatusFamilyFilter {
  if (value === "success") return "2xx";
  if (value === "client_error") return "4xx";
  if (value === "error") return "5xx";
  return "all";
}

export function statusFamilyToAlias(value: StatusFamilyFilter): StatusAliasFilter {
  if (value === "2xx") return "success";
  if (value === "4xx") return "client_error";
  if (value === "5xx") return "error";
  return "all";
}

export interface RequestLogPageState {
  ingress_request_id: string;
  model_id: string;
  endpoint_id: string;
  client_rule_id: string;
  proxy_api_key_id: string;
  view: "attempts" | "ingress_chains" | "";
  resolved_target_model_id: string;
  status_code: string;
  error_text: string;
  priced: PricedFilter;
  unpriced_reason: string;
  time_range: TimeRange;
  status_family: StatusFamilyFilter;
  limit: number;
  offset: number;
  request_id: string;
  selected_request_id: string;
}

function parseEnum<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
  const normalized = normalizeSearchString(value);
  if (normalized && (allowed as readonly string[]).includes(normalized)) return normalized as T;
  return fallback;
}

function normalizeSearchString(value: unknown): string {
  if (value == null || value === "") {
    return "";
  }

  const trimmed = String(value).trim();
  if (!trimmed || trimmed === "undefined" || trimmed === "null") {
    return "";
  }

  if (trimmed.length >= 2 && trimmed.startsWith('"') && trimmed.endsWith('"')) {
    return trimmed.slice(1, -1);
  }

  return trimmed;
}

function parseIntParam(value: unknown, fallback: number): number {
  const normalized = normalizeSearchString(value);
  if (!normalized) return fallback;
  const n = parseInt(normalized, 10);
  return Number.isFinite(n) && n >= 0 ? n : fallback;
}

function parsePageSize(value: unknown): number {
  const fallback = DEFAULTS.limit;
  const parsed = parseIntParam(value, fallback);
  return PAGE_SIZE_OPTIONS.includes(parsed as (typeof PAGE_SIZE_OPTIONS)[number]) ? parsed : fallback;
}

export function normalizeRequestId(value: unknown): string {
  const normalized = normalizeSearchString(value).replace(/^#/, "");
  return /^\d+$/.test(normalized) ? normalized : "";
}

export function parsePageSearch(search: Record<string, unknown>): RequestLogPageState {
  const statusParam = normalizeSearchString(search.status);
  const statusFamilyParam = normalizeSearchString(search.status_family);
  const cursorOffset = parseIntParam(search.cursor, DEFAULTS.offset);
  const explicitOffset = parseIntParam(search.offset, DEFAULTS.offset);

  return {
    ingress_request_id: normalizeSearchString(search.ingress_request_id),
    model_id: normalizeSearchString(search.model || search.model_id),
    endpoint_id: normalizeSearchString(search.endpoint || search.endpoint_id),
    client_rule_id: normalizeSearchString(search.client_rule_id),
    proxy_api_key_id: normalizeSearchString(search.proxy_api_key_id),
    view: parseEnum(search.view, ["attempts", "ingress_chains"], "") as "" | "attempts" | "ingress_chains",
    resolved_target_model_id: normalizeSearchString(search.resolved_target_model_id),
    status_code: normalizeSearchString(search.status_code),
    error_text: normalizeSearchString(search.error_text),
    priced: parseEnum(search.priced, PRICED_OPTIONS, DEFAULTS.priced),
    unpriced_reason: normalizeSearchString(search.unpriced_reason),
    time_range: parseEnum(search.time_range, TIME_RANGE_OPTIONS, DEFAULTS.time_range),
    status_family: statusParam && statusParam !== "all"
      ? statusAliasToFamily(parseEnum(statusParam, STATUS_ALIAS_OPTIONS, "all"))
      : parseEnum(statusFamilyParam, STATUS_FAMILY_OPTIONS, DEFAULTS.status_family),
    limit: parsePageSize(search.limit),
    offset: explicitOffset !== DEFAULTS.offset ? explicitOffset : cursorOffset,
    request_id: normalizeRequestId(search.request_id),
    selected_request_id: normalizeRequestId(search.selected_request_id),
  };
}

export function parsePageState(params: URLSearchParams): RequestLogPageState {
  return parsePageSearch(Object.fromEntries(params));
}

export function stateToSearch(state: RequestLogPageState): Record<string, string | number> {
  const search: Record<string, string | number> = {};
  if (state.ingress_request_id) search.ingress_request_id = state.ingress_request_id;
  if (state.proxy_api_key_id) search.proxy_api_key_id = state.proxy_api_key_id;
  if (state.view) search.view = state.view;
  if (state.model_id) search.model = state.model_id;
  if (state.endpoint_id) search.endpoint = state.endpoint_id;
  if (state.client_rule_id) search.client_rule_id = state.client_rule_id;
  if (state.resolved_target_model_id) search.resolved_target_model_id = state.resolved_target_model_id;
  if (state.status_code) search.status_code = state.status_code;
  if (state.error_text) search.error_text = state.error_text;
  if (state.priced !== DEFAULTS.priced) search.priced = state.priced;
  if (state.priced === "false" && state.unpriced_reason) search.unpriced_reason = state.unpriced_reason;
  if (state.time_range !== DEFAULTS.time_range) search.time_range = state.time_range;
  if (state.status_family !== DEFAULTS.status_family) search.status = statusFamilyToAlias(state.status_family);
  if (state.limit !== DEFAULTS.limit) search.limit = state.limit;
  if (state.offset !== DEFAULTS.offset) search.cursor = state.offset;
  if (state.request_id) search.request_id = state.request_id;
  if (state.selected_request_id) search.selected_request_id = state.selected_request_id;
  return search;
}

export function stateToParams(state: RequestLogPageState): URLSearchParams {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(stateToSearch(state))) {
    params.set(key, String(value));
  }
  return params;
}

export function timeRangeToFromTime(range: TimeRange): string | undefined {
  if (range === "all") return undefined;
  const now = Date.now();
  const ms: Record<string, number> = {
    "1h": 3600000,
    "6h": 21600000,
    "24h": 86400000,
    "7d": 604800000,
    "30d": 2592000000,
  };
  return new Date(now - (ms[range] ?? 86400000)).toISOString();
}
