export const TIME_RANGE_OPTIONS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type TimeRange = (typeof TIME_RANGE_OPTIONS)[number];

export const STATUS_FAMILY_OPTIONS = ["all", "2xx", "4xx", "5xx"] as const;
export type StatusFamilyFilter = (typeof STATUS_FAMILY_OPTIONS)[number];

export const STATUS_ALIAS_OPTIONS = ["all", "success", "client_error", "error"] as const;
export type StatusAliasFilter = (typeof STATUS_ALIAS_OPTIONS)[number];

export const PRICING_STATUS_OPTIONS = ["all", "priced", "unpriced", "ineligible", "unknown"] as const;
export type PricingStatusFilter = (typeof PRICING_STATUS_OPTIONS)[number];

export const UNPRICED_REASON_OPTIONS = [
  "PRICING_DISABLED",
  "MISSING_TOKEN_USAGE",
  "STREAM_USAGE_UNAVAILABLE",
  "MISSING_PRICE_DATA",
] as const;
export type UnpricedReasonFilter = (typeof UNPRICED_REASON_OPTIONS)[number];

export const PRICING_CARD_ROLE_OPTIONS = ["standard", "tier_base", "tier_above", "peak", "offpeak"] as const;
export type PricingCardRoleFilter = (typeof PRICING_CARD_ROLE_OPTIONS)[number];
export const PRICING_SELECTION_STATE_OPTIONS = ["not_evaluated", "not_applicable", "selected", "unresolved"] as const;
export type PricingSelectionStateFilter = (typeof PRICING_SELECTION_STATE_OPTIONS)[number];

export const FINAL_RESULT_OPTIONS = ["", "completed", "failed", "client_disconnected"] as const;
export type FinalResultFilter = (typeof FINAL_RESULT_OPTIONS)[number];

export const VIEW_OPTIONS = ["attempts", "ingress_chains"] as const;
export type RequestLogView = (typeof VIEW_OPTIONS)[number];

export const SORT_BY_OPTIONS = ["created_at", "display_status", "ttft_ms", "total_tokens", "total_cost_user_currency_micros"] as const;
export type RequestLogSortBy = (typeof SORT_BY_OPTIONS)[number];

export const PAGE_SIZE_OPTIONS = [100, 300, 500] as const;

// 入口链是游标分页，后端 chain_limit 的上限是 50（超过即 422）。
export const CHAIN_PAGE_SIZE_OPTIONS = [20, 30, 50] as const;

export const DEFAULTS = {
  limit: 100,
  offset: 0,
  ingress_final_result: "",
  confirmed_failover: false,
  // ponytail: fixed 24h default, no per-user persistence for request logs - localStorage only if 24h still annoys.
  time_range: "24h" as TimeRange,
  status_family: "all" as StatusFamilyFilter,
  pricing_status: "all" as PricingStatusFilter,
  unpriced_reason: "",
  pricing_card_role: "",
  pricing_selection_state: "",
  chain_limit: 20,
  view: "ingress_chains" as RequestLogView,
  sort_by: "created_at" as RequestLogSortBy,
  sort_order: "desc" as "asc" | "desc",
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
  ingress_final_result: FinalResultFilter;
  confirmed_failover: boolean;
  ingress_request_id: string;
  model_id: string;
  endpoint_id: string;
  terminal_target_id: string;
  client_rule_id: string;
  proxy_api_key_id: string;
  resolved_target_model_id: string;
  api_family: string;
  row_kind: string;
  status_code: string;
  stream_outcome: string;
  stream_error_kind: string;
  error_text: string;
  pricing_status: PricingStatusFilter;
  unpriced_reason: string;
  pricing_card_role: PricingCardRoleFilter | "";
  pricing_selection_state: PricingSelectionStateFilter | "";
  time_range: TimeRange;
  from_time: string;
  to_time: string;
  observe_return: string;
  query_context: string;
  final_result: string;
  outcome_detail: string;
  final_status_code: string;
  final_stream_outcome: string;
  final_stream_error_kind: string;
  final_exclude: string;
  final_target_model_id: string;
  final_endpoint_id: string;
  final_terminal_target_id: string;
  final_pricing_status: string;
  final_unpriced_reason: string;
  reporting_currency_epoch: string;
  cost_segment_key: string;
  attempt_trigger: string;
  attempt_result: string;
  status_family: StatusFamilyFilter;
  limit: number;
  offset: number;
  request_id: string;
  selected_request_id: string;
  view: RequestLogView;
  sort_by: RequestLogSortBy;
  sort_order: "asc" | "desc";
  chain_cursor: string;
  chain_limit: number;
}

export const TOKEN_BOUND_REQUEST_FILTER_DEFAULTS = {
  query_context: "",
  final_result: "",
  outcome_detail: "",
  final_status_code: "",
  final_stream_outcome: "",
  final_stream_error_kind: "",
  final_exclude: "",
  final_target_model_id: "",
  final_endpoint_id: "",
  final_terminal_target_id: "",
  final_pricing_status: "",
  final_unpriced_reason: "",
  reporting_currency_epoch: "",
  attempt_trigger: "",
  attempt_result: "",
  stream_outcome: "",
  stream_error_kind: "",
} as const satisfies Partial<RequestLogPageState>;

function chainCompatibleState(
  state: RequestLogPageState,
): RequestLogPageState {
  return {
    ...state,
    ...(state.query_context ? TOKEN_BOUND_REQUEST_FILTER_DEFAULTS : {}),
    api_family: "",
    row_kind: "",
    attempt_trigger: "",
    attempt_result: "",
    stream_outcome: "",
    stream_error_kind: "",
  };
}

export function requestLogStateForView(
  state: RequestLogPageState,
  view: RequestLogView,
): RequestLogPageState {
  if (view === "attempts") {
    return { ...state, view, chain_cursor: "", offset: DEFAULTS.offset };
  }
  return {
    ...chainCompatibleState(state),
    view,
    limit: DEFAULTS.limit,
    offset: DEFAULTS.offset,
    chain_cursor: "",
    sort_by: "created_at",
  };
}

export function applyRequestLogStatePatch(
  state: RequestLogPageState,
  patch: Partial<RequestLogPageState>,
  resetPagination = true,
): RequestLogPageState {
  const next = { ...state, ...patch };
  if (resetPagination) {
    if (!("offset" in patch)) next.offset = DEFAULTS.offset;
    if (!("chain_cursor" in patch)) next.chain_cursor = "";
  }
  return next;
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

export function splitRequestFilterValues(value: string): string[] | undefined {
  const values = value
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
  return values.length > 0 ? values : undefined;
}

function parseIntParam(value: unknown, fallback: number): number {
  const normalized = normalizeSearchString(value);
  if (!normalized) return fallback;
  const n = parseInt(normalized, 10);
  return Number.isFinite(n) && n >= 0 ? n : fallback;
}

function parseChainPageSize(value: unknown): number {
  const fallback = DEFAULTS.chain_limit;
  const parsed = parseIntParam(value, fallback);
  return CHAIN_PAGE_SIZE_OPTIONS.includes(
    parsed as (typeof CHAIN_PAGE_SIZE_OPTIONS)[number],
  )
    ? parsed
    : fallback;
}

function parsePageSize(value: unknown): number {
  const fallback = DEFAULTS.limit;
  const parsed = parseIntParam(value, fallback);
  return PAGE_SIZE_OPTIONS.includes(parsed as (typeof PAGE_SIZE_OPTIONS)[number]) ? parsed : fallback;
}

export function normalizeRequestId(value: unknown): string {
  const normalized = normalizeSearchString(value).replace(/^#/, "");
  return isPositiveDecimalInt64(normalized) ? normalized : "";
}

export function isPositiveDecimalInt64(value: string): boolean {
  if (!/^\d+$/.test(value)) return false;
  const canonical = value.replace(/^0+/, "");
  if (!canonical) return false;
  const max = "9223372036854775807";
  return canonical.length < max.length ||
    (canonical.length === max.length && canonical <= max);
}

export function parsePageSearch(search: Record<string, unknown>): RequestLogPageState {
  const statusParam = normalizeSearchString(search.status);
  const statusFamilyParam = normalizeSearchString(search.status_family);
  const cursorOffset = parseIntParam(search.cursor, DEFAULTS.offset);
  const explicitOffset = parseIntParam(search.offset, DEFAULTS.offset);
  const view = parseEnum(search.view, VIEW_OPTIONS, DEFAULTS.view);
  const sortOrder = parseEnum(search.sort_order, ["asc", "desc"], DEFAULTS.sort_order);
  const sortBy =
    view === "ingress_chains"
      ? "created_at"
      : parseEnum(search.sort_by, SORT_BY_OPTIONS, DEFAULTS.sort_by);

  const state: RequestLogPageState = {
    ingress_request_id: normalizeSearchString(search.ingress_request_id),
    model_id: normalizeSearchString(search.ingress_model_id),
    endpoint_id: normalizeSearchString(search.endpoint || search.endpoint_id),
    terminal_target_id: normalizeSearchString(search.terminal_target_id),
    client_rule_id: normalizeSearchString(search.client_rule_id),
    proxy_api_key_id: normalizeSearchString(search.proxy_api_key_id),
    resolved_target_model_id: normalizeSearchString(search.attempt_target_model_id),
    api_family: normalizeSearchString(search.api_family),
    row_kind: normalizeSearchString(search.row_kind),
    status_code: normalizeSearchString(search.status_code),
    stream_outcome: normalizeSearchString(search.stream_outcome),
    stream_error_kind: normalizeSearchString(search.stream_error_kind),
    error_text: normalizeSearchString(search.error_text),
    pricing_status: parseEnum(search.pricing_status, PRICING_STATUS_OPTIONS, DEFAULTS.pricing_status),
    ingress_final_result: parseEnum(search.ingress_final_result, FINAL_RESULT_OPTIONS, ""),
    confirmed_failover: normalizeSearchString(search.confirmed_failover) === "true",
    unpriced_reason: normalizeSearchString(search.unpriced_reason),
    pricing_card_role: parseEnum(search.pricing_card_role, ["", ...PRICING_CARD_ROLE_OPTIONS] as const, DEFAULTS.pricing_card_role),
    pricing_selection_state: parseEnum(search.pricing_selection_state, ["", ...PRICING_SELECTION_STATE_OPTIONS] as const, DEFAULTS.pricing_selection_state),
    time_range: parseEnum(search.time_range, TIME_RANGE_OPTIONS, DEFAULTS.time_range),
    from_time: normalizeSearchString(search.from_time),
    to_time: normalizeSearchString(search.to_time),
    observe_return: normalizeSearchString(search.observe_return),
    query_context: normalizeSearchString(search.query_context),
    final_result: normalizeSearchString(search.final_result),
    outcome_detail: normalizeSearchString(search.outcome_detail),
    final_status_code: normalizeSearchString(search.final_status_code),
    final_stream_outcome: normalizeSearchString(search.final_stream_outcome),
    final_stream_error_kind: normalizeSearchString(
      search.final_stream_error_kind,
    ),
    final_exclude: normalizeSearchString(search.final_exclude),
    final_target_model_id: normalizeSearchString(search.final_target_model_id),
    final_endpoint_id: normalizeSearchString(search.final_endpoint_id),
    final_terminal_target_id: normalizeSearchString(search.final_terminal_target_id),
    final_pricing_status: normalizeSearchString(search.final_pricing_status),
    final_unpriced_reason: normalizeSearchString(search.final_unpriced_reason),
    reporting_currency_epoch: normalizeSearchString(
      search.reporting_currency_epoch,
    ),
    cost_segment_key: normalizeSearchString(search.cost_segment_key),
    attempt_trigger: normalizeSearchString(search.attempt_trigger),
    attempt_result: normalizeSearchString(search.attempt_result),
    status_family: statusParam && statusParam !== "all"
      ? statusAliasToFamily(parseEnum(statusParam, STATUS_ALIAS_OPTIONS, "all"))
      : parseEnum(statusFamilyParam, STATUS_FAMILY_OPTIONS, DEFAULTS.status_family),
    limit: view === "attempts" ? parsePageSize(search.limit) : DEFAULTS.limit,
    chain_limit:
      view === "ingress_chains"
        ? parseChainPageSize(search.chain_limit)
        : DEFAULTS.chain_limit,
    offset:
      view === "attempts"
        ? explicitOffset !== DEFAULTS.offset
          ? explicitOffset
          : cursorOffset
        : DEFAULTS.offset,
    request_id: normalizeRequestId(search.request_id),
    selected_request_id: normalizeRequestId(search.selected_request_id),
    view,
    sort_by: sortBy,
    sort_order: sortOrder,
    chain_cursor:
      view === "ingress_chains"
        ? normalizeSearchString(search.chain_cursor)
        : "",
  };
  return view === "ingress_chains" ? chainCompatibleState(state) : state;
}

export function parsePageState(params: URLSearchParams): RequestLogPageState {
  const search: Record<string, string | string[]> = {};
  params.forEach((value, key) => {
    const current = search[key];
    if (current === undefined) {
      search[key] = value;
    } else if (Array.isArray(current)) {
      current.push(value);
    } else {
      search[key] = [current, value];
    }
  });
  return parsePageSearch(search);
}

export function stateToSearch(state: RequestLogPageState): Record<string, string | number> {
  const search: Record<string, string | number> = {};
  if (state.ingress_final_result) search.ingress_final_result = state.ingress_final_result;
  if (state.confirmed_failover) search.confirmed_failover = "true";
  if (state.ingress_request_id) search.ingress_request_id = state.ingress_request_id;
  if (state.model_id) search.ingress_model_id = state.model_id;
  if (state.endpoint_id) search.endpoint = state.endpoint_id;
  if (state.terminal_target_id) search.terminal_target_id = state.terminal_target_id;
  if (state.client_rule_id) search.client_rule_id = state.client_rule_id;
  if (state.proxy_api_key_id) search.proxy_api_key_id = state.proxy_api_key_id;
  if (state.resolved_target_model_id) search.attempt_target_model_id = state.resolved_target_model_id;
  if (state.api_family) search.api_family = state.api_family;
  if (state.row_kind) search.row_kind = state.row_kind;
  if (state.status_code) search.status_code = state.status_code;
  if (state.stream_outcome) search.stream_outcome = state.stream_outcome;
  if (state.stream_error_kind) search.stream_error_kind = state.stream_error_kind;
  if (state.error_text) search.error_text = state.error_text;
  if (state.pricing_status !== DEFAULTS.pricing_status) search.pricing_status = state.pricing_status;
  if (state.pricing_status === "unpriced" && state.unpriced_reason) search.unpriced_reason = state.unpriced_reason;
  if (state.pricing_card_role) search.pricing_card_role = state.pricing_card_role;
  if (state.pricing_selection_state) search.pricing_selection_state = state.pricing_selection_state;
  if (state.from_time && state.to_time) {
    search.from_time = state.from_time;
    search.to_time = state.to_time;
  } else if (state.time_range !== DEFAULTS.time_range) {
    search.time_range = state.time_range;
  }
  if (state.observe_return) search.observe_return = state.observe_return;
  if (state.query_context) search.query_context = state.query_context;
  if (state.final_result) search.final_result = state.final_result;
  if (state.outcome_detail) search.outcome_detail = state.outcome_detail;
  if (state.final_status_code) search.final_status_code = state.final_status_code;
  if (state.final_stream_outcome)
    search.final_stream_outcome = state.final_stream_outcome;
  if (state.final_stream_error_kind)
    search.final_stream_error_kind = state.final_stream_error_kind;
  if (state.final_exclude) search.final_exclude = state.final_exclude;
  if (state.final_target_model_id) search.final_target_model_id = state.final_target_model_id;
  if (state.final_endpoint_id) search.final_endpoint_id = state.final_endpoint_id;
  if (state.final_terminal_target_id) search.final_terminal_target_id = state.final_terminal_target_id;
  if (state.final_pricing_status) search.final_pricing_status = state.final_pricing_status;
  if (state.final_unpriced_reason) search.final_unpriced_reason = state.final_unpriced_reason;
  if (state.reporting_currency_epoch)
    search.reporting_currency_epoch = state.reporting_currency_epoch;
  if (state.cost_segment_key) search.cost_segment_key = state.cost_segment_key;
  if (state.attempt_trigger) search.attempt_trigger = state.attempt_trigger;
  if (state.attempt_result) search.attempt_result = state.attempt_result;
  if (state.status_family !== DEFAULTS.status_family) search.status = statusFamilyToAlias(state.status_family);
  if (state.view === "attempts" && state.limit !== DEFAULTS.limit)
    search.limit = state.limit;
  if (state.view === "attempts" && state.offset !== DEFAULTS.offset)
    search.cursor = state.offset;
  if (state.request_id) search.request_id = state.request_id;
  if (state.selected_request_id) search.selected_request_id = state.selected_request_id;
  // Keep the investigation unit explicit in portable URLs, including the
  // default ingress-chain view. This prevents a copied request-log URL from
  // silently changing scope when the page default evolves.
  search.view = state.view;
  if (state.view === "attempts" && state.sort_by !== DEFAULTS.sort_by)
    search.sort_by = state.sort_by;
  if (state.sort_order !== DEFAULTS.sort_order) search.sort_order = state.sort_order;
  if (state.view === "ingress_chains" && state.chain_cursor)
    search.chain_cursor = state.chain_cursor;
  if (state.view === "ingress_chains" && state.chain_limit !== DEFAULTS.chain_limit)
    search.chain_limit = state.chain_limit;
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
