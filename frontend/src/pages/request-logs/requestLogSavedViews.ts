// Saved request-log views (Requests SPEC §10.5/R-P2-16): versioned
// localStorage persistence of canonical query states. A saved view stores
// the full canonical RequestLogPageState minus transient pagination and
// selection anchors.
import type { RequestLogPageState } from "./queryParams";

const SAVED_VIEWS_STORAGE_KEY = "prism.request-logs.saved-views.v1";
const MAX_SAVED_VIEWS = 20;

export interface SavedRequestLogView {
  id: string;
  name: string;
  createdAt: string;
  updatedAt: string;
  state: Omit<RequestLogPageState, "chain_cursor" | "offset" | "request_id" | "selected_request_id">;
}

export function savedViewStateOf(state: RequestLogPageState): SavedRequestLogView["state"] {
  const { chain_cursor: _chainCursor, offset: _offset, request_id: _requestId, selected_request_id: _selected, ...rest } = state;
  void _chainCursor;
  void _offset;
  void _requestId;
  void _selected;
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
    const parsed = JSON.parse(raw) as { version?: number; views?: SavedRequestLogView[] };
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
    localStorage.setItem(SAVED_VIEWS_STORAGE_KEY, JSON.stringify({ version: 1, views }));
  } catch {
    // localStorage unavailable (private mode / quota): saved views degrade
    // silently to in-memory-only for this session.
  }
}

export function saveRequestLogView(name: string, state: RequestLogPageState): SavedRequestLogView {
  const views = loadSavedViews();
  const trimmedName = name.trim().slice(0, 80);
  const existing = views.find((view) => view.name.toLowerCase() === trimmedName.toLowerCase());
  const now = new Date().toISOString();
  let view: SavedRequestLogView;
  if (existing) {
    view = { ...existing, name: trimmedName, updatedAt: now, state: savedViewStateOf(state) };
    const index = views.findIndex((item) => item.id === existing.id);
    views[index] = view;
  } else {
    view = { id: createViewId(), name: trimmedName, createdAt: now, updatedAt: now, state: savedViewStateOf(state) };
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

export function applySavedView(view: SavedRequestLogView, current: RequestLogPageState): RequestLogPageState {
  return {
    ...current,
    ...view.state,
    chain_cursor: "",
    offset: 0,
    request_id: "",
    selected_request_id: "",
  };
}
