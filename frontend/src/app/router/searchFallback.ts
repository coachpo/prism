// A search schema's `.catch()` keeps an illegal parameter from taking the page
// down, but on its own it trades a visible broken link for an invisible one:
// the router drops the parameter, canonicalizes the address bar, and the page
// renders its default view as if that were what the link asked for. This module
// records what was dropped so the page can say so.

/** Longest raw value echoed back in the notice. */
const REJECTED_SEARCH_VALUE_MAX_LENGTH = 40;

type SearchFallbackRecord = {
  pathname: string;
  rejected: string[];
};

let record: SearchFallbackRecord | null = null;

/**
 * Called from `validateSearch`, where the raw query values are still available.
 * A key that carried a value in the URL and came back `undefined` did not
 * survive validation.
 *
 * The record is deliberately write-only here: after validation the router
 * rewrites the address bar without the illegal parameters and validates the
 * cleaned search again, and that second pass must not erase the trace. Leaving
 * the route clears it (see `retireSearchFallbackUnless`).
 */
export function recordSearchFallback(
  rawSearch: Record<string, unknown>,
  parsed: Record<string, unknown>,
  keys: readonly string[],
): void {
  const rejected: string[] = [];
  for (const key of keys) {
    const rawValue = rawSearch[key];
    if (typeof rawValue !== "string" || rawValue.trim() === "") continue;
    if (parsed[key] !== undefined) continue;
    rejected.push(
      rawValue.length > REJECTED_SEARCH_VALUE_MAX_LENGTH
        ? `${key}=${rawValue.slice(0, REJECTED_SEARCH_VALUE_MAX_LENGTH)}…`
        : `${key}=${rawValue}`,
    );
  }
  if (rejected.length === 0) {
    return;
  }
  // An illegal value only ever arrives with the address the browser is already
  // on — a bookmark, a hand-edited URL, a pushState — so the location is the
  // page the operator is looking at.
  record = { pathname: window.location.pathname, rejected };
}

const NO_REJECTED_KEYS: readonly string[] = [];

/**
 * Reading never consumes the record. The shell remounts several times on a cold
 * load — auth bootstrap, then the lazy page chunk — and a one-shot read loses
 * the notice in whichever remount happens to come after it.
 */
export function readSearchFallback(pathname: string): readonly string[] {
  return record?.pathname === pathname ? record.rejected : NO_REJECTED_KEYS;
}

/**
 * Retires the record once the operator is on a different page, so returning to
 * the same route later starts clean. Leaving is the only thing that clears it:
 * the router validates the cleaned search again right after canonicalizing the
 * address bar, and that pass legitimately finds nothing wrong.
 */
export function retireSearchFallbackUnless(pathname: string): void {
  if (record && record.pathname !== pathname) {
    record = null;
  }
}
