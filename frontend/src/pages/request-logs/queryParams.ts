export const TIME_RANGE_OPTIONS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type TimeRange = (typeof TIME_RANGE_OPTIONS)[number];

export const STATUS_FAMILY_OPTIONS = ["all", "4xx", "5xx"] as const;
export type StatusFamilyFilter = (typeof STATUS_FAMILY_OPTIONS)[number];

export const STATUS_ALIAS_OPTIONS = ["all", "client_error", "error"] as const;
export type StatusAliasFilter = (typeof STATUS_ALIAS_OPTIONS)[number];

export const PAGE_SIZE_OPTIONS = [100, 300, 500] as const;

export const DEFAULTS = {
  limit: 100,
  offset: 0,
  time_range: "1h" as TimeRange,
  status_family: "all" as StatusFamilyFilter,
} as const;

export function statusAliasToFamily(value: StatusAliasFilter | string | null): StatusFamilyFilter {
  if (value === "client_error") return "4xx";
  if (value === "error") return "5xx";
  return "all";
}

export function statusFamilyToAlias(value: StatusFamilyFilter): StatusAliasFilter {
  if (value === "4xx") return "client_error";
  if (value === "5xx") return "error";
  return "all";
}

export interface RequestLogPageState {
  ingress_request_id: string;
  model_id: string;
  endpoint_id: string;
  time_range: TimeRange;
  status_family: StatusFamilyFilter;
  limit: number;
  offset: number;
  request_id: string;
  selected_request_id: string;
}

function parseEnum<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  if (value && (allowed as readonly string[]).includes(value)) return value as T;
  return fallback;
}

function normalizeSearchString(value: string | null): string {
  if (!value) {
    return "";
  }

  const trimmed = value.trim();
  if (!trimmed || trimmed === "undefined" || trimmed === "null") {
    return "";
  }

  if (trimmed.length >= 2 && trimmed.startsWith('"') && trimmed.endsWith('"')) {
    return trimmed.slice(1, -1);
  }

  return trimmed;
}

function parseIntParam(value: string | null, fallback: number): number {
  const normalized = normalizeSearchString(value);
  if (!normalized) return fallback;
  const n = parseInt(normalized, 10);
  return Number.isFinite(n) && n >= 0 ? n : fallback;
}

function parsePageSize(value: string | null): number {
  const fallback = DEFAULTS.limit;
  const parsed = parseIntParam(value, fallback);
  return PAGE_SIZE_OPTIONS.includes(parsed as (typeof PAGE_SIZE_OPTIONS)[number]) ? parsed : fallback;
}

export function normalizeRequestId(value: string | null): string {
  const normalized = normalizeSearchString(value).replace(/^#/, "");
  return /^\d+$/.test(normalized) ? normalized : "";
}

export function parsePageState(params: URLSearchParams): RequestLogPageState {
  const statusParam = params.get("status");
  const statusFamilyParam = params.get("status_family");

  return {
    ingress_request_id: normalizeSearchString(params.get("ingress_request_id")),
    model_id: normalizeSearchString(params.get("model") ?? params.get("model_id")),
    endpoint_id: normalizeSearchString(params.get("endpoint") ?? params.get("endpoint_id")),
    time_range: parseEnum(normalizeSearchString(params.get("time_range")), TIME_RANGE_OPTIONS, DEFAULTS.time_range),
    status_family: statusParam
      ? statusAliasToFamily(parseEnum(statusParam, STATUS_ALIAS_OPTIONS, "all"))
      : parseEnum(statusFamilyParam, STATUS_FAMILY_OPTIONS, DEFAULTS.status_family),
    limit: parsePageSize(params.get("limit")),
    offset: parseIntParam(params.get("offset") ?? params.get("cursor"), DEFAULTS.offset),
    request_id: normalizeRequestId(params.get("request_id")),
    selected_request_id: normalizeRequestId(params.get("selected_request_id")),
  };
}

export function stateToParams(state: RequestLogPageState): URLSearchParams {
  const p = new URLSearchParams();
  if (state.ingress_request_id) p.set("ingress_request_id", state.ingress_request_id);
  if (state.model_id) p.set("model", state.model_id);
  if (state.endpoint_id) p.set("endpoint", state.endpoint_id);
  if (state.time_range !== DEFAULTS.time_range) p.set("time_range", state.time_range);
  if (state.status_family !== DEFAULTS.status_family) p.set("status", statusFamilyToAlias(state.status_family));
  if (state.limit !== DEFAULTS.limit) p.set("limit", String(state.limit));
  if (state.offset !== DEFAULTS.offset) p.set("cursor", String(state.offset));
  if (state.request_id) p.set("request_id", state.request_id);
  if (state.selected_request_id) p.set("selected_request_id", state.selected_request_id);
  return p;
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
