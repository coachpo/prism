/**
 * Shared honest-state contract for every truly async paged list (SPEC:
 * frontend/DESIGN.md Honesty Contract). One small vocabulary covers all four
 * read kinds so页面级、展开行和弹窗列表 render the same states site-wide:
 *
 * - `initial`  first read of a list; nothing on screen yet → full skeleton.
 * - `replace`  a page turn or deep-linked cursor; the rows on screen belong to
 *              the *previous* URL/scope, so they are swapped for skeleton rows
 *              while the target page loads and never repainted as the new page.
 * - `refresh`  same-scope re-read; the matching rows stay visible untouched,
 *              and only a failure marks them stale behind the staleness badge.
 * - `append`   "load more"; old items stay put and the new page attaches below
 *              with its own inline pending/error/retry affordance.
 *
 * Superseded responses (a newer read started, or the request was aborted) are
 * discarded by the caller before committing; nothing here may paint a response
 * that lost its race.
 */

/** How the in-flight (or last failed) read was issued. */
export type PageReadKind = "initial" | "replace" | "refresh" | "append";

export type PagedListPhase = "idle" | "ready" | "empty" | "partial";

export interface PagedListState<T> {
 /**
  * Display phase of the **committed** page. Independent of `reading`: a
  * replace read keeps the previous phase visible until the new page commits
  * or the read fails.
  */
 phase: PagedListPhase;
 data: T | null;
 readKind: PageReadKind;
 /** True while any read is in flight. */
 reading: boolean;
 /**
  * The kept rows no longer answer the current query because their read
  * failed. Set only on failure, only when kept data exists, and only for
  * kinds whose data survives (`refresh`/`append`); a failed `replace` hides
  * the old rows behind the target-page error instead.
  */
 stale: boolean;
 lastSuccessfulAt: string | null;
 error: string | null;
}

export function initialPagedListState<T>(
 data: T | null = null,
): PagedListState<T> {
 return {
  phase: "idle",
  data,
  readKind: "initial",
  reading: false,
  stale: false,
  lastSuccessfulAt: null,
  error: null,
 };
}

/** Mark a read as in flight. Kept data stays until the read resolves.
 *
 * A `replace` withdraws the old rows (they describe the previous scope), so a
 * staleness badge carried over from an earlier failed refresh goes with them;
 * `refresh`/`append` keep rows on screen and keep any existing staleness
 * visible until the read resolves.
 */
export function beginPagedRead<T>(
 state: PagedListState<T>,
 kind: PageReadKind,
): PagedListState<T> {
 const withdrawsOldRows = kind === "initial" || kind === "replace";
 return {
  ...state,
  readKind: kind,
  reading: true,
  error: null,
  stale: withdrawsOldRows ? false : state.stale,
 };
}

/**
 * Atomically commit a successful read. The previous page never bleeds into the
 * new one: `data` swaps in one setState and `stale` clears.
 *
 * `phase` is the caller-derived display phase of the committed page (`ready`,
 * `empty`, or `partial` for clipped coverage), so response-specific semantics
 * stay with the feature.
 */
export function commitPagedRead<T>(
 state: PagedListState<T>,
 data: T,
 phase: PagedListPhase,
 semanticQueryKey?: string,
): PagedListState<T> {
 void semanticQueryKey;
 return {
  ...state,
  phase,
  data,
  reading: false,
  stale: false,
  lastSuccessfulAt: new Date().toISOString(),
  error: null,
 };
}

/**
 * Commit a failed read. The last good page is never deleted here; whether it
 * still describes the current query follows from the failed read's kind:
 * `refresh`/`append` failures leave matching-but-stale data on screen, while
 * `initial`/`replace` failures present the target-page error surface instead
 * of rows that would masquerade as the page that failed to load.
 */
export function failPagedRead<T>(
 state: PagedListState<T>,
 message: string,
): PagedListState<T> {
 const keptDataMatchesQuery =
  state.data !== null &&
  (state.readKind === "refresh" || state.readKind === "append");
 return {
  ...state,
  reading: false,
  stale: keptDataMatchesQuery || (state.stale && state.data !== null),
  error: message,
 };
}

/**
 * Whether the table body should show skeleton rows instead of committed rows.
 * `initial` always; `replace` only while the replacement is in flight; never
 * for `refresh`/`append`, which keep real rows visible.
 */
export function shouldShowPendingRows<T>(state: PagedListState<T>): boolean {
 if (!state.reading) return false;
 if (state.data === null) return true;
 return state.readKind === "replace";
}

/** Whether the committed rows may stay on screen during the in-flight read. */
export function keepsCommittedRows<T>(state: PagedListState<T>): boolean {
 if (!state.reading) return true;
 if (state.data === null) return false;
 return state.readKind !== "replace";
}
