import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";

import { api } from "@/lib/api";
import type { CatalogCandidate } from "@/lib/types";

/**
 * Bounded catalog candidate search uses the same page size as the backend
 * default (`backend/internal/httpapi/management/models/catalog_read_routes.go`)
 * so the first read never has to re-request a smaller window.
 */
export const CATALOG_CANDIDATE_PAGE_SIZE = 20;

/** Keystroke debounce for the replace read, unchanged from the first cut. */
const CANDIDATE_QUERY_DEBOUNCE_MS = 250;

/**
 * Honest state of the committed candidate list, named after the shared
 * async-pagination vocabulary in `@/shared/table/paginationStates`:
 *
 * - `loading` a replace read (first read or condition change) is in flight.
 * - `ready`   a replace read committed at least one candidate.
 * - `empty`   a replace read committed zero candidates: a real result.
 * - `error`   a replace read failed, so the list is *unknown*, never empty.
 *
 * An append never changes this state: a failed append keeps the committed
 * candidates and reports itself through `appendError` instead.
 */
export type CandidatePhase = "loading" | "ready" | "empty" | "error";

interface PagerState {
  /** Condition this state describes; anything else is a stale carry-over. */
  key: string;
  /** Monotonic read generation; only the current one may commit. */
  generation: number;
  phase: CandidatePhase;
  items: CatalogCandidate[];
  total: number;
  /** Server cursor for the next segment, or `null` while unknown. */
  nextOffset: number | null;
  error: string | null;
  appendPending: boolean;
  appendError: string | null;
}

interface ReadOwner {
  key: string;
  generation: number;
}

interface AppendOwner extends ReadOwner {
  offset: number;
}

type PagerAction =
  | { type: "replace:start"; key: string; generation: number }
  | {
      type: "replace:ok";
      key: string;
      generation: number;
      items: CatalogCandidate[];
      total: number;
      nextOffset: number;
    }
  | { type: "replace:fail"; key: string; generation: number; error: string }
  | { type: "append:start"; key: string; generation: number }
  | {
      type: "append:ok";
      key: string;
      generation: number;
      items: CatalogCandidate[];
      total: number | null;
      nextOffset: number;
    }
  | { type: "append:fail"; key: string; generation: number; error: string };

function candidateKey(candidate: CatalogCandidate): string {
  return `${candidate.provider_id}/${candidate.model_id}`;
}

function failureMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

function pageItems(
  items: CatalogCandidate[] | null | undefined,
): CatalogCandidate[] {
  return Array.isArray(items) ? items : [];
}

/** Merge an appended segment without ever repeating a visible candidate. */
function mergeSegments(
  loaded: CatalogCandidate[],
  appended: CatalogCandidate[],
): CatalogCandidate[] {
  const seen = new Set(loaded.map(candidateKey));
  const merged = [...loaded];
  for (const candidate of appended) {
    const key = candidateKey(candidate);
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(candidate);
  }
  return merged;
}

/**
 * A read may commit only while it still owns the pager. This is what keeps a
 * late reply from overwriting or mixing into the list the operator sees.
 */
function owns(state: PagerState, action: { key: string; generation: number }) {
  return state.key === action.key && state.generation === action.generation;
}

function reducer(state: PagerState, action: PagerAction): PagerState {
  switch (action.type) {
    case "replace:start":
      return {
        key: action.key,
        generation: action.generation,
        phase: "loading",
        items: [],
        total: 0,
        nextOffset: null,
        error: null,
        appendPending: false,
        appendError: null,
      };
    case "replace:ok":
      if (!owns(state, action)) return state;
      return {
        ...state,
        phase: action.items.length > 0 ? "ready" : "empty",
        items: action.items,
        total: action.total,
        nextOffset: action.nextOffset,
        error: null,
        appendPending: false,
        appendError: null,
      };
    case "replace:fail":
      if (!owns(state, action)) return state;
      // A failed replace is a failure surface, never a degraded empty list.
      return {
        ...state,
        phase: "error",
        items: [],
        total: 0,
        nextOffset: null,
        error: action.error,
        appendPending: false,
        appendError: null,
      };
    case "append:start":
      if (!owns(state, action)) return state;
      return { ...state, appendPending: true, appendError: null };
    case "append:ok": {
      if (!owns(state, action)) return state;
      const items = mergeSegments(state.items, action.items);
      const total = action.total ?? state.total;
      // An empty segment means the server has nothing left to hand out, so the
      // cursor passes the total instead of offering the same read again.
      const nextOffset = action.items.length === 0 ? total : action.nextOffset;
      return {
        ...state,
        items,
        total,
        nextOffset,
        appendPending: false,
        appendError: null,
      };
    }
    case "append:fail":
      if (!owns(state, action)) return state;
      // Loaded candidates stay on screen; the control becomes the retry.
      return {
        ...state,
        appendPending: false,
        appendError: action.error,
      };
    default:
      return state;
  }
}

function initialPagerState(): PagerState {
  return {
    key: "",
    generation: 0,
    phase: "loading",
    items: [],
    total: 0,
    nextOffset: null,
    error: null,
    appendPending: false,
    appendError: null,
  };
}

export interface CatalogCandidatePager {
  items: CatalogCandidate[];
  total: number;
  phase: CandidatePhase;
  /** A replace read is in flight for the current condition. */
  replacing: boolean;
  /** An append read is in flight; the visible list stays untouched. */
  appending: boolean;
  /** Replace failure: the candidate list is unknown, not empty. */
  error: string | null;
  /** Append failure: the loaded candidates still match the condition. */
  appendError: string | null;
  /** Another segment is still available for the current condition. */
  hasMore: boolean;
  onLoadMore: () => void;
  /** Re-issue the failed replace read for the current condition. */
  onRetry: () => void;
}

/**
 * Offset-append pager for the models.dev candidate search.
 *
 * A condition change (`modelConfigId`, query, or an explicit retry) starts a
 * new generation that replaces the candidates from `offset=0`. `loadMore`
 * requests the next segment at the cursor the last *accepted* response
 * reported (`offset + items.length`), deduplicates it into the list, and keeps
 * the count honest until the last segment removes the control.
 */
export function useCatalogCandidates(
  modelConfigId: number | null,
  query: string,
): CatalogCandidatePager {
  const [state, dispatch] = useReducer(reducer, undefined, initialPagerState);
  const [retryToken, setRetryToken] = useState(0);
  const generationRef = useRef(0);
  // Single-flight guard: synchronous, so a fast double click can never issue
  // the same segment twice.
  const appendOwnerRef = useRef<AppendOwner | null>(null);
  // Condition snapshot for the append read, kept in step with the replace
  // effect so a click can never read a stale query.
  const conditionRef = useRef({ modelConfigId, query, key: "", generation: 0 });

  const key = useMemo(
    () => `${modelConfigId}\u0000${query}\u0000${retryToken}`,
    [modelConfigId, query, retryToken],
  );

  useLayoutEffect(() => {
    const generation = (generationRef.current += 1);
    conditionRef.current = { modelConfigId, query, key, generation };
    appendOwnerRef.current = null;
    // Claim the condition before the debounce begins. A semantic A → B → A
    // transition reuses the same key, so delaying this ownership handoff would
    // let an old A response commit against the old A generation.
    dispatch({ type: "replace:start", key, generation });
    if (modelConfigId === null) {
      dispatch({
        type: "replace:ok",
        key,
        generation,
        items: [],
        total: 0,
        nextOffset: 0,
      });
      return;
    }
    const request = {
      modelConfigId,
      query,
      key,
      generation,
      // Empty query stays scoped to the api_family; a search reaches every
      // provider so manual binding can find aggregator offerings.
      scope: query ? ("all" as const) : ("family" as const),
    };
    const handle = setTimeout(() => {
      api.models.catalog
        .candidates(request.modelConfigId, {
          q: request.query || undefined,
          scope: request.scope,
          limit: CATALOG_CANDIDATE_PAGE_SIZE,
          offset: 0,
        })
        .then((response) => {
          const items = pageItems(response.items);
          const offset = Number.isFinite(response.offset) ? response.offset : 0;
          dispatch({
            type: "replace:ok",
            key: request.key,
            generation: request.generation,
            items,
            total: Number.isFinite(response.total)
              ? response.total
              : items.length,
            nextOffset:
              items.length === 0
                ? Number.isFinite(response.total)
                  ? response.total
                  : 0
                : offset + items.length,
          });
        })
        .catch((cause: unknown) => {
          dispatch({
            type: "replace:fail",
            key: request.key,
            generation: request.generation,
            error: failureMessage(cause),
          });
        });
    }, CANDIDATE_QUERY_DEBOUNCE_MS);
    return () => clearTimeout(handle);
  }, [key, modelConfigId, query]);

  const onLoadMore = useCallback(() => {
    if (appendOwnerRef.current) return;
    const condition = conditionRef.current;
    // The click must still belong to the rendered condition; otherwise the
    // cursor it reads describes a list the operator no longer sees.
    if (condition.key !== key || condition.modelConfigId === null) return;
    const offset = state.nextOffset;
    if (offset === null || offset >= state.total) return;
    const owner: AppendOwner = {
      key: condition.key,
      generation: condition.generation,
      offset,
    };
    appendOwnerRef.current = owner;
    dispatch({
      type: "append:start",
      key: owner.key,
      generation: owner.generation,
    });
    api.models.catalog
      .candidates(condition.modelConfigId, {
        q: condition.query || undefined,
        scope: condition.query ? "all" : "family",
        limit: CATALOG_CANDIDATE_PAGE_SIZE,
        offset,
      })
      .then((response) => {
        const items = pageItems(response.items);
        const responseOffset = Number.isFinite(response.offset)
          ? response.offset
          : offset;
        dispatch({
          type: "append:ok",
          key: owner.key,
          generation: owner.generation,
          items,
          total: Number.isFinite(response.total) ? response.total : null,
          nextOffset: responseOffset + items.length,
        });
      })
      .catch((cause: unknown) => {
        dispatch({
          type: "append:fail",
          key: owner.key,
          generation: owner.generation,
          error: failureMessage(cause),
        });
      })
      .finally(() => {
        const currentOwner = appendOwnerRef.current;
        if (
          currentOwner?.key === owner.key &&
          currentOwner.generation === owner.generation &&
          currentOwner.offset === owner.offset
        ) {
          appendOwnerRef.current = null;
        }
      });
  }, [key, state.nextOffset, state.total]);

  const onRetry = useCallback(() => {
    setRetryToken((token) => token + 1);
  }, []);

  const stale = state.key !== key;
  const phase: CandidatePhase = stale ? "loading" : state.phase;
  const items = stale ? [] : state.items;
  const total = stale ? 0 : state.total;
  const nextOffset = stale ? null : state.nextOffset;
  const replacing = phase === "loading";
  const appending = !stale && state.appendPending;
  const hasMore =
    !stale && nextOffset !== null && nextOffset < total && !replacing;

  return {
    items,
    total,
    phase,
    replacing,
    appending,
    error: stale ? null : state.error,
    appendError: stale ? null : state.appendError,
    hasMore,
    onLoadMore,
    onRetry,
  };
}
