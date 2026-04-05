export const TIME_RANGE_OPTIONS = ["1h", "6h", "24h", "7d", "30d", "all"] as const;
export type TimeRange = (typeof TIME_RANGE_OPTIONS)[number];

export const STATUS_FAMILY_OPTIONS = ["all", "4xx", "5xx"] as const;
export type StatusFamilyFilter = (typeof STATUS_FAMILY_OPTIONS)[number];

export const DETAIL_TAB_OPTIONS = ["overview", "audit"] as const;
export type DetailTab = (typeof DETAIL_TAB_OPTIONS)[number];

export const PAGE_SIZE_OPTIONS = [100, 300, 500] as const;

export const DEFAULTS = {
  limit: 100,
  offset: 0,
  time_range: "1h" as TimeRange,
  status_family: "all" as StatusFamilyFilter,
  detail_tab: "overview" as DetailTab,
} as const;

export interface RequestLogPageState {
  ingress_request_id: string;
  model_id: string;
  endpoint_id: string;
  time_range: TimeRange;
  status_family: StatusFamilyFilter;
  limit: number;
  offset: number;
  request_id: string;
  detail_tab: DetailTab;
}

function parseEnum<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  if (value && (allowed as readonly string[]).includes(value)) return value as T;
  return fallback;
}

function parseIntParam(value: string | null, fallback: number): number {
  if (!value) return fallback;
  const n = parseInt(value, 10);
  return Number.isFinite(n) && n >= 0 ? n : fallback;
}

function parsePageSize(value: string | null): number {
  const fallback = DEFAULTS.limit;
  const parsed = parseIntParam(value, fallback);
  return PAGE_SIZE_OPTIONS.includes(parsed as (typeof PAGE_SIZE_OPTIONS)[number]) ? parsed : fallback;
}

export function parsePageState(params: URLSearchParams): RequestLogPageState {
  return {
    ingress_request_id: params.get("ingress_request_id") ?? "",
    model_id: params.get("model_id") ?? "",
    endpoint_id: params.get("endpoint_id") ?? "",
    time_range: parseEnum(params.get("time_range"), TIME_RANGE_OPTIONS, DEFAULTS.time_range),
    status_family: parseEnum(params.get("status_family"), STATUS_FAMILY_OPTIONS, DEFAULTS.status_family),
    limit: parsePageSize(params.get("limit")),
    offset: parseIntParam(params.get("offset"), DEFAULTS.offset),
    request_id: params.get("request_id") ?? "",
    detail_tab: parseEnum(params.get("detail_tab"), DETAIL_TAB_OPTIONS, DEFAULTS.detail_tab),
  };
}

export function stateToParams(state: RequestLogPageState): URLSearchParams {
  const p = new URLSearchParams();
  if (state.ingress_request_id) p.set("ingress_request_id", state.ingress_request_id);
  if (state.model_id) p.set("model_id", state.model_id);
  if (state.endpoint_id) p.set("endpoint_id", state.endpoint_id);
  if (state.time_range !== DEFAULTS.time_range) p.set("time_range", state.time_range);
  if (state.status_family !== DEFAULTS.status_family) p.set("status_family", state.status_family);
  if (state.limit !== DEFAULTS.limit) p.set("limit", String(state.limit));
  if (state.offset !== DEFAULTS.offset) p.set("offset", String(state.offset));
  if (state.request_id) p.set("request_id", state.request_id);
  if (state.request_id && state.detail_tab !== DEFAULTS.detail_tab) p.set("detail_tab", state.detail_tab);
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
