import { readPreference, serializePreferenceFile, writePreference } from "@/lib/preferences/storage";
// Saved request-log views (Requests SPEC §10.5/R-P2-16): versioned
// localStorage persistence of canonical query states. A saved view stores
// the full canonical RequestLogPageState minus transient pagination and
// selection anchors.
import {
  TOKEN_BOUND_REQUEST_FILTER_DEFAULTS,
  parsePageSearch, stateToSearch,
  type RequestLogPageState,
} from "./queryParams";

const SAVED_VIEWS_STORAGE_KEY = "prism.request-logs.saved-views.v1";
const MAX_SAVED_VIEWS = 20;

export interface SavedRequestLogView {
  id: string;
  name: string;
  createdAt: string;
  updatedAt: string;
  state: Omit<
    RequestLogPageState,
    | "ingress_request_id"
    | "observe_return"
    | "stream_outcome"
    | "stream_error_kind"
    | "chain_cursor"
    | "offset"
    | "request_id"
    | "selected_request_id"
    | "query_context"
    | "final_result"
    | "outcome_detail"
    | "final_status_code"
    | "final_stream_outcome"
    | "final_stream_error_kind"
    | "final_exclude"
    | "final_target_model_id"
    | "final_endpoint_id"
    | "final_terminal_target_id"
    | "final_pricing_status"
    | "final_unpriced_reason"
    | "reporting_currency_epoch"
    | "attempt_trigger"
    | "attempt_result"
  >;
}

const SAFE_KEYS = [
  "ingress_final_result", "confirmed_failover", "model_id", "endpoint_id", "terminal_target_id",
  "client_rule_id", "proxy_api_key_id", "resolved_target_model_id", "api_family", "row_kind",
  "status_code", "error_text", "pricing_status", "unpriced_reason", "pricing_card_role",
  "pricing_selection_state", "time_range", "from_time", "to_time", "cost_segment_key",
  "status_family", "limit", "view", "sort_by", "sort_order", "chain_limit",
] as const;
function pickSafeState(state: RequestLogPageState): SavedRequestLogView["state"] {
  return Object.fromEntries(SAFE_KEYS.map(key => [key, state[key]])) as SavedRequestLogView["state"];
}
export function savedViewStateOf(state: RequestLogPageState): SavedRequestLogView["state"] {
  // Persist only the ordinary query actually represented by the URL/API owner.
  // Inactive conditional values cannot later become active after transfer.
  const ordinary = { ...parsePageSearch({}), ...pickSafeState(state) };
  if (ordinary.view !== "ingress_chains") ordinary.cost_segment_key = "";
  return pickSafeState(parsePageSearch(stateToSearch(ordinary)));
}

export function validateSavedViewState(value: unknown, strict = true): SavedRequestLogView["state"] {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("invalid_state");
  const record = value as Record<string, unknown>;
  if (strict && Object.keys(record).some(key => !(SAFE_KEYS as readonly string[]).includes(key))) throw new Error("unsafe_field");
  const defaults = savedViewStateOf(parsePageSearch({}));
  const state = { ...defaults };
  for (const key of SAFE_KEYS) {
    if (!(key in record)) continue;
    const candidate = record[key];
    if (typeof candidate !== typeof defaults[key] || (typeof candidate === "string" && candidate.length > 2048)) throw new Error("invalid_field");
    Object.assign(state, { [key]: candidate });
  }
  const page = { ...parsePageSearch({}), ...state };
  const normalized = savedViewStateOf(page);
  // Reject unknown enums and malformed ranges rather than silently widening a query.
  for (const key of SAFE_KEYS) {
    if (state[key] !== normalized[key]) {
      const inactiveLegacy =
        (key === "limit" && state.view === "ingress_chains") ||
        (key === "chain_limit" && state.view === "attempts") ||
        (key === "unpriced_reason" && state.pricing_status !== "unpriced") ||
        (key === "cost_segment_key" && state.view === "attempts") ||
        (key === "time_range" && Boolean(state.from_time && state.to_time));
      if (strict || !inactiveLegacy) throw new Error("invalid_field");
      Object.assign(state, { [key]: normalized[key] });
    }
  }
  if (Boolean(state.from_time) !== Boolean(state.to_time) || (state.from_time && (!Number.isFinite(Date.parse(state.from_time)) || !Number.isFinite(Date.parse(state.to_time)) || Date.parse(state.from_time) >= Date.parse(state.to_time)))) throw new Error("invalid_range");
  return state;
}

function createViewId(): string {
  const random = Math.random().toString(36).slice(2, 10);
  return `view-${Date.now().toString(36)}-${random}`;
}

export function loadSavedViewsResult(): { views: SavedRequestLogView[]; persisted: boolean; error: boolean } {
  const stored = readPreference(SAVED_VIEWS_STORAGE_KEY);
  try {
    if (!stored.raw) return { views: [], persisted: stored.persisted, error: false };
    const parsed = JSON.parse(stored.raw);
    if (parsed.version !== 1 || !Array.isArray(parsed.views) || parsed.views.length > MAX_SAVED_VIEWS) throw new Error("invalid_version");
    let invalid = false;
    const views = parsed.views.flatMap((view: SavedRequestLogView) => {
      try {
      if (!view || typeof view.id !== "string" || typeof view.name !== "string" || !view.name.trim() || view.name.length > 80) throw new Error("invalid_view");
      return { id: view.id, name: view.name, createdAt: view.createdAt, updatedAt: view.updatedAt, state: validateSavedViewState(view.state, false) };
      } catch { invalid = true; return []; }
    });
    return { views, persisted: stored.persisted, error: invalid };
  } catch { return { views: [], persisted: stored.persisted, error: true }; }
}
export function loadSavedViews(): SavedRequestLogView[] { return loadSavedViewsResult().views; }
export function requestViewsFileData(views: SavedRequestLogView[]) {
  return { format: "prism.request-views" as const, version: 1 as const, views: views.map(view => ({ name: view.name, state: validateSavedViewState(view.state) })) };
}
function persistViews(views: SavedRequestLogView[]): boolean {
  serializePreferenceFile(requestViewsFileData(views));
  return writePreference(SAVED_VIEWS_STORAGE_KEY, { version: 1, views });
}
export function importSavedViews(views: SavedRequestLogView[]): boolean {
  const existing = loadSavedViews();
  if (existing.length + views.length > MAX_SAVED_VIEWS) throw new Error("limit");
  const names = new Set(existing.map(view => view.name.toLowerCase()));
  for (const view of views) {
    if (!view.name.trim() || view.name.trim().length > 80) throw new Error("invalid_name");
    if (names.has(view.name.trim().toLowerCase())) throw new Error("name_conflict");
    names.add(view.name.trim().toLowerCase());
  }
  return persistViews([...existing, ...views.map(view => ({ ...view, id: createViewId(), name: view.name.trim(), state: validateSavedViewState(view.state) }))]);
}
export function saveRequestLogView(
  name: string,
  state: RequestLogPageState,
): SavedRequestLogView {
  const views = loadSavedViews();
  const trimmedName = name.trim().slice(0, 80);
  const existing = views.find(
    (view) => view.name.toLowerCase() === trimmedName.toLowerCase(),
  );
  const now = new Date().toISOString();
  let view: SavedRequestLogView;
  if (existing) {
    view = {
      ...existing,
      name: trimmedName,
      updatedAt: now,
      state: savedViewStateOf(state),
    };
    const index = views.findIndex((item) => item.id === existing.id);
    views[index] = view;
  } else {
    view = {
      id: createViewId(),
      name: trimmedName,
      createdAt: now,
      updatedAt: now,
      state: savedViewStateOf(state),
    };
    views.push(view);
  }
  // Keep the most recent MAX_SAVED_VIEWS views.
  const trimmed = views.slice(-MAX_SAVED_VIEWS);
  persistViews(trimmed);
  return view;
}

export function deleteRequestLogView(viewId: string): void {
  const views = loadSavedViews().filter((view) => view.id !== viewId);
  persistViews(views);
}

export function applySavedView(
  view: SavedRequestLogView,
  current: RequestLogPageState,
): RequestLogPageState {
  return {
    ...current,
    ...view.state,
    chain_cursor: "",
    offset: 0,
    request_id: "",
    selected_request_id: "",
    ...TOKEN_BOUND_REQUEST_FILTER_DEFAULTS,
    ingress_request_id: "", observe_return: "",
  };
}
