// Saved request-log views (Requests SPEC §10.5/R-P2-16): versioned
// localStorage persistence of canonical query states. A saved view stores
// the full canonical RequestLogPageState minus transient pagination and
// selection anchors.
import {
  TOKEN_BOUND_REQUEST_FILTER_DEFAULTS,
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

export function savedViewStateOf(
  state: RequestLogPageState,
): SavedRequestLogView["state"] {
  const {
    chain_cursor: _chainCursor,
    offset: _offset,
    request_id: _requestId,
    selected_request_id: _selected,
    query_context: _queryContext,
    final_result: _finalResult,
    outcome_detail: _outcomeDetail,
    final_status_code: _finalStatusCode,
    final_stream_outcome: _finalStreamOutcome,
    final_stream_error_kind: _finalStreamErrorKind,
    final_exclude: _finalExclude,
    final_target_model_id: _finalModel,
    final_endpoint_id: _finalEndpoint,
    final_terminal_target_id: _finalTarget,
    final_pricing_status: _finalPricingStatus,
    final_unpriced_reason: _finalUnpricedReason,
    reporting_currency_epoch: _reportingCurrencyEpoch,
    attempt_trigger: _attemptTrigger,
    attempt_result: _attemptResult,
    ...rest
  } = state;
  void _chainCursor;
  void _offset;
  void _requestId;
  void _selected;
  void _queryContext;
  void _finalResult;
  void _outcomeDetail;
  void _finalStatusCode;
  void _finalStreamOutcome;
  void _finalStreamErrorKind;
  void _finalExclude;
  void _finalModel;
  void _finalEndpoint;
  void _finalTarget;
  void _finalPricingStatus;
  void _finalUnpricedReason;
  void _reportingCurrencyEpoch;
  void _attemptTrigger;
  void _attemptResult;
  return rest;
}

function createViewId(): string {
  const random = Math.random().toString(36).slice(2, 10);
  return `view-${Date.now().toString(36)}-${random}`;
}

export function loadSavedViews(): SavedRequestLogView[] {
  try {
    const raw = localStorage.getItem(SAVED_VIEWS_STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as {
      version?: number;
      views?: SavedRequestLogView[];
    };
    if (parsed.version !== 1 || !Array.isArray(parsed.views)) return [];
    return parsed.views.filter(
      (view) =>
        view &&
        typeof view.id === "string" &&
        typeof view.name === "string" &&
        view.state &&
        typeof view.state === "object" &&
        typeof (view.state as Record<string, unknown>).model_id === "string",
    );
  } catch {
    return [];
  }
}

function persistViews(views: SavedRequestLogView[]): void {
  try {
    localStorage.setItem(
      SAVED_VIEWS_STORAGE_KEY,
      JSON.stringify({ version: 1, views }),
    );
  } catch {
    // localStorage unavailable (private mode / quota): saved views degrade
    // silently to in-memory-only for this session.
  }
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
  };
}
