// Window-level cache-read share read model. The backend aggregates over a
// single cache_basis_eligible predicate (disjoint input/cache_read/cache_
// creation semantics); this module projects that aggregate into the distinct
// states the KPI card must present: a real value, a genuine zero, no
// comparable rows, an empty window, and a zero denominator. None of them may
// be shown as a zero standing in for an absent value. A failed read is
// handled by the fragment error surface, never here.

export interface CacheReadShareWindowInput {
  /** Window request total from the usage-summary fragment. */
  requestCount: number;
  /** Rows that passed the backend cache_basis_eligible predicate. */
  basisRequestCount: number;
  /** SUM(input_tokens) over eligible rows (null when none exist). */
  basisInputTokens: number | null;
  /** SUM(cache_read_input_tokens) over eligible rows. */
  basisCacheReadTokens: number | null;
  /** SUM(COALESCE(cache_creation_input_tokens, 0)) over eligible rows. */
  basisCacheCreationTokens: number | null;
}

export type WindowCacheReadShare =
  | { kind: "value"; share: number }
  | { kind: "empty_window" }
  | { kind: "no_comparable_rows" }
  | { kind: "no_denominator" };

/** Projects the window aggregate onto the distinguishable states. SUM never
 *  distinguishes "no rows" from "all rows null", so the state order reads
 *  request_count first, then the eligible count, then the denominator. A
 *  zero denominator yields no ratio — never a zero-value stand-in. */
export function windowCacheReadShare(input: CacheReadShareWindowInput): WindowCacheReadShare {
  if (input.requestCount === 0) return { kind: "empty_window" };
  if (input.basisRequestCount === 0) return { kind: "no_comparable_rows" };
  const denominator =
    (input.basisInputTokens ?? 0) +
    (input.basisCacheReadTokens ?? 0) +
    (input.basisCacheCreationTokens ?? 0);
  if (denominator <= 0) return { kind: "no_denominator" };
  return { kind: "value", share: (input.basisCacheReadTokens ?? 0) / denominator };
}

/** Partial cache-basis coverage: at least one comparable row exists but not
 *  every window request is comparable. The card attaches an
 *  OperatorClippedBadge while still rendering the share. */
export function cacheBasisPartialCoverage(input: CacheReadShareWindowInput): boolean {
  return input.basisRequestCount > 0 && input.basisRequestCount < input.requestCount;
}