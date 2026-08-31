import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
} from "react";

/**
 * One committed page of candidate reads. `revision` is the source-qualified
 * catalog revision the page was computed from (the adapter qualifies it, e.g.
 * `models.dev:<etag>` or `pi.dev:<sha256>`); a bare revision must never be
 * compared across sources.
 */
export interface CandidatePage<T, Evidence = undefined> {
  items: T[];
  total: number;
  offset: number;
  revision: string;
  /** Source-owned evidence committed under the same generation as the rows. */
  evidence?: Evidence;
}

export type AppendCandidatePagerSource = "models.dev" | "pi.dev";

export interface AppendCandidatePagerOptions<T, Evidence = undefined> {
  sourceKey: AppendCandidatePagerSource;
  /** When false the pager idles and exposes an empty, non-error state. */
  enabled: boolean;
  /**
   * Semantic condition key (committed query + model scoping). A change starts
   * a new generation that replaces the list from offset 0. Debouncing and
   * "search" buttons are the adapter's job; this controller only reacts.
   */
  conditionKey: string;
  /**
   * One committed page reader. The adapter owns the page size, the wire
   * shape, and the source-qualified revision mapping; the controller derives
   * cursors from the server-echoed offset, so it never needs the size itself.
   * Must be stable (useCallback) across renders for the same condition.
   */
  fetchPage: (
    offset: number,
    signal: AbortSignal,
  ) => Promise<CandidatePage<T, Evidence>>;
  itemKey: (item: T) => string;
}

export type CandidatePhase = "loading" | "ready" | "empty" | "error";

export interface AppendCandidatePager<T, Evidence = undefined> {
  items: T[];
  total: number;
  revision: string | null;
  /** Evidence from the accepted page; late generations cannot mutate it. */
  evidence?: Evidence | null;
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
  /** True after a committed page arrived whose revision differs from the list's. */
  revisionRolledOver: boolean;
  onLoadMore: () => void;
  /** Re-issue the failed replace read for the current condition. */
  onRetry: () => void;
  /** Dismiss the revision-rollover notice after the re-read completes. */
  onRolloverAcknowledged: () => void;
}

interface PagerState<T, Evidence> {
  /** Condition this state describes; anything else is a stale carry-over. */
  key: string;
  /** Monotonic read generation; only the current one may commit. */
  generation: number;
  phase: CandidatePhase;
  items: T[];
  total: number;
  /** Source-qualified revision the committed pages belong to. */
  revision: string;
  evidence: Evidence | null;
  /** Server cursor for the next segment, or `null` while unknown. */
  nextOffset: number | null;
  error: string | null;
  appendPending: boolean;
  appendError: string | null;
  revisionRolledOver: boolean;
}

interface ReadOwner {
  key: string;
  generation: number;
}

interface AppendOwner extends ReadOwner {
  offset: number;
}

type PagerAction<T, Evidence> =
  | { type: "replace:start"; key: string; generation: number }
  | {
      type: "replace:ok";
      key: string;
      generation: number;
      page: CandidatePage<T, Evidence>;
    }
  | { type: "replace:fail"; key: string; generation: number; error: string }
  | { type: "append:start"; key: string; generation: number }
  | {
      type: "append:ok";
      key: string;
      generation: number;
      page: CandidatePage<T, Evidence>;
    }
  | { type: "append:fail"; key: string; generation: number; error: string }
  | { type: "rollover:acknowledge"; key: string; generation: number };

function failureMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

function pageItems<T>(items: T[] | null | undefined): T[] {
  return Array.isArray(items) ? items : [];
}

/** Merge an appended segment without ever repeating a visible item. */
function mergeSegments<T>(
  loaded: T[],
  appended: T[],
  itemKey: (item: T) => string,
): T[] {
  const seen = new Set(loaded.map(itemKey));
  const merged = [...loaded];
  for (const item of appended) {
    const key = itemKey(item);
    if (seen.has(key)) continue;
    seen.add(key);
    merged.push(item);
  }
  return merged;
}

/**
 * A read may commit only while it still owns the pager. This is what keeps a
 * late reply from overwriting or mixing into the list the operator sees.
 */
function owns<T, Evidence>(
  state: PagerState<T, Evidence>,
  action: { key: string; generation: number },
) {
  return state.key === action.key && state.generation === action.generation;
}

function reducer<T, Evidence>(
  itemKey: (item: T) => string,
): (
  state: PagerState<T, Evidence>,
  action: PagerAction<T, Evidence>,
) => PagerState<T, Evidence> {
  return (state, action) => {
    switch (action.type) {
      case "replace:start":
        return {
          key: action.key,
          generation: action.generation,
          phase: "loading",
          items: [],
          total: 0,
          revision: "",
          evidence: null,
          nextOffset: null,
          error: null,
          appendPending: false,
          appendError: null,
          revisionRolledOver: false,
        };
      case "replace:ok":
        if (!owns(state, action)) return state;
        return {
          ...state,
          phase: action.page.items.length > 0 ? "ready" : "empty",
          items: pageItems(action.page.items),
          total: action.page.total,
          revision: action.page.revision,
          evidence: action.page.evidence ?? null,
          nextOffset:
            action.page.items.length === 0
              ? action.page.total
              : action.page.offset + action.page.items.length,
          error: null,
          appendPending: false,
          appendError: null,
          revisionRolledOver: state.revisionRolledOver,
        };
      case "replace:fail":
        if (!owns(state, action)) return state;
        // A failed replace is a failure surface, never a degraded empty list.
        return {
          ...state,
          phase: "error",
          items: [],
          total: 0,
          revision: "",
          nextOffset: null,
          error: action.error,
          appendPending: false,
          appendError: null,
          revisionRolledOver: state.revisionRolledOver,
        };
      case "append:start":
        if (!owns(state, action)) return state;
        return { ...state, appendPending: true, appendError: null };
      case "append:ok": {
        if (!owns(state, action)) return state;
        // A committed page from a different revision must never be stitched
        // onto the current list: the whole group is withdrawn and the pager
        // restarts from offset 0 of the new revision.
        if (action.page.revision !== state.revision) {
          return {
            ...state,
            items: [],
            total: action.page.total,
            revision: action.page.revision,
            evidence: action.page.evidence ?? null,
            phase: "loading",
            nextOffset: 0,
            appendPending: false,
            appendError: null,
            revisionRolledOver: true,
          };
        }
        const items = mergeSegments(
          state.items,
          pageItems(action.page.items),
          itemKey,
        );
        // An empty segment means the server has nothing left to hand out, so
        // the cursor passes the total instead of offering the same read again.
        const nextOffset =
          action.page.items.length === 0
            ? action.page.total
            : action.page.offset + action.page.items.length;
        return {
          ...state,
          items,
          total: action.page.total,
          evidence:
            action.page.evidence === undefined
              ? state.evidence
              : action.page.evidence,
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
      case "rollover:acknowledge":
        if (!owns(state, action)) return state;
        // Recovery owns the rollover flag while offset 0 is being re-read.
        // Clearing it here would tear down the effect and abort the only read
        // capable of leaving the loading phase.
        if (state.phase === "loading") return state;
        return { ...state, revisionRolledOver: false };
      default:
        return state;
    }
  };
}

function initialPagerState<T, Evidence>(): PagerState<T, Evidence> {
  return {
    key: "",
    generation: 0,
    phase: "loading",
    items: [],
    total: 0,
    revision: "",
    evidence: null,
    nextOffset: null,
    error: null,
    appendPending: false,
    appendError: null,
    revisionRolledOver: false,
  };
}

/**
 * Source-agnostic append pager for catalog candidate searches.
 *
 * The controller owns only the paging lifecycle: initial/replace/append,
 * semantic-key + monotonic generation ownership, AbortController and
 * late-response discard, single-flight appends per offset, item dedupe,
 * hasMore/retry, the source-qualified committed snapshot revision, and the
 * revision-rollover reset (a page from a different revision withdraws the
 * group and restarts from offset 0).
 *
 * It deliberately does NOT own: debouncing or search buttons (the adapter
 * commits `conditionKey` only when a query is submitted), source matching
 * policy, Pi exact candidates / final-API gates / one-hit selection, or any
 * visible copy, route state, or API DTO.
 */
export function useAppendCandidatePager<T, Evidence = undefined>(
  options: AppendCandidatePagerOptions<T, Evidence>,
): AppendCandidatePager<T, Evidence> {
  const { sourceKey, enabled, conditionKey, fetchPage, itemKey } = options;
  const [state, dispatch] = useReducer(
    reducer<T, Evidence>(itemKey),
    undefined,
    initialPagerState<T, Evidence>,
  );
  const [retryToken, setRetryToken] = useReducer(
    (token: number) => token + 1,
    0,
  );
  const generationRef = useRef(0);
  // Single-flight guard: synchronous, so a fast double click can never issue
  // the same segment twice.
  const appendOwnerRef = useRef<AppendOwner | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  // Condition snapshot for the append read, kept in step with the replace
  // effect so a click can never read a stale condition.
  const conditionRef = useRef({
    conditionKey,
    enabled,
    key: "",
    generation: 0,
  });

  const key = useMemo(
    () => `${sourceKey}\u0000${conditionKey}\u0000${retryToken}`,
    [sourceKey, conditionKey, retryToken],
  );

  useLayoutEffect(() => {
    const generation = (generationRef.current += 1);
    conditionRef.current = { conditionKey, enabled, key, generation };
    appendOwnerRef.current = null;
    // Claim the condition before the read begins. A semantic A → B → A
    // transition reuses the same key, so delaying this ownership handoff
    // would let an old A response commit against the old A generation.
    dispatch({ type: "replace:start", key, generation });
    if (!enabled) {
      dispatch({
        type: "replace:ok",
        key,
        generation,
        page: { items: [], total: 0, offset: 0, revision: "" },
      });
      return;
    }
    const controller = new AbortController();
    abortRef.current = controller;
    fetchPage(0, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        dispatch({ type: "replace:ok", key, generation, page });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        dispatch({
          type: "replace:fail",
          key,
          generation,
          error: failureMessage(cause),
        });
      });
    return () => {
      controller.abort();
    };
  }, [key, conditionKey, enabled, fetchPage]);

  const onLoadMore = useCallback(() => {
    if (appendOwnerRef.current) return;
    const condition = conditionRef.current;
    // The click must still belong to the rendered condition; otherwise the
    // cursor it reads describes a list the operator no longer sees.
    if (condition.key !== key || !condition.enabled) return;
    const offset = state.nextOffset;
    if (offset === null || offset >= state.total || state.phase === "loading")
      return;
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
    const controller = new AbortController();
    fetchPage(offset, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        dispatch({
          type: "append:ok",
          key: owner.key,
          generation: owner.generation,
          page,
        });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
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
  }, [key, state.nextOffset, state.total, state.phase, fetchPage]);

  const onRetry = useCallback(() => {
    setRetryToken();
  }, []);

  const onRolloverAcknowledged = useCallback(() => {
    dispatch({
      type: "rollover:acknowledge",
      key: conditionRef.current.key,
      generation: conditionRef.current.generation,
    });
  }, []);

  // Revision rollover: an append answered from a different source-qualified
  // revision withdraws the mixed group and the controller re-reads offset 0
  // of the new revision under the same generation ownership.
  const rolledOver = state.key === key && state.revisionRolledOver;
  const rolledOverKey = state.key;
  const rolledOverGeneration = state.generation;
  useEffect(() => {
    if (!rolledOver) return;
    const controller = new AbortController();
    fetchPage(0, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        dispatch({
          type: "replace:ok",
          key: rolledOverKey,
          generation: rolledOverGeneration,
          page,
        });
      })
      .catch((cause: unknown) => {
        if (controller.signal.aborted) return;
        dispatch({
          type: "replace:fail",
          key: rolledOverKey,
          generation: rolledOverGeneration,
          error: failureMessage(cause),
        });
      });
    return () => {
      controller.abort();
    };
  }, [rolledOver, rolledOverKey, rolledOverGeneration, fetchPage]);

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
    revision: stale ? null : state.revision || null,
    evidence: stale ? null : state.evidence,
    phase,
    replacing,
    appending,
    error: stale ? null : state.error,
    appendError: stale ? null : state.appendError,
    hasMore,
    revisionRolledOver: !stale && state.revisionRolledOver,
    onLoadMore,
    onRetry,
    onRolloverAcknowledged,
  };
}
