import { describe, expect, it } from "vitest";
import { cacheBasisPartialCoverage, windowCacheReadShare, type CacheReadShareWindowInput } from "./cacheReadShare";

function input(overrides: Partial<CacheReadShareWindowInput>): CacheReadShareWindowInput {
  return {
    requestCount: 10,
    basisRequestCount: 10,
    basisInputTokens: 2000,
    basisCacheReadTokens: 18000,
    basisCacheCreationTokens: 0,
    ...overrides,
  };
}

describe("windowCacheReadShare", () => {
  it("computes the share against the disjoint denominator", () => {
    expect(windowCacheReadShare(input({}))).toEqual({ kind: "value", share: 18000 / 20000 });
  });

  it("reports a genuine zero when no token was served from cache", () => {
    // Cold start: cache_read is 0 while cache_creation carries the prompt;
    // the share must be 0, not read + creation over the total.
    expect(
      windowCacheReadShare(input({ basisCacheReadTokens: 0, basisCacheCreationTokens: 18000, basisInputTokens: 2000 })),
    ).toEqual({ kind: "value", share: 0 });
  });

  it("distinguishes no comparable rows", () => {
    expect(windowCacheReadShare(input({ basisRequestCount: 0, basisInputTokens: null, basisCacheReadTokens: null, basisCacheCreationTokens: null }))).toEqual({ kind: "no_comparable_rows" });
  });

  it("distinguishes an empty window", () => {
    expect(windowCacheReadShare(input({ requestCount: 0, basisRequestCount: 0, basisInputTokens: null, basisCacheReadTokens: null, basisCacheCreationTokens: null }))).toEqual({ kind: "empty_window" });
  });

  it("reports no ratio when the denominator is zero", () => {
    // Eligible rows exist but all measured components are zero: 0/0 is not a
    // percentage and must not be rendered as 0.0%.
    expect(
      windowCacheReadShare(input({ basisRequestCount: 3, basisInputTokens: 0, basisCacheReadTokens: 0, basisCacheCreationTokens: 0 })),
    ).toEqual({ kind: "no_denominator" });
  });

  it("keeps sharing the value under partial coverage", () => {
    // Fewer comparable rows than window requests still yields a share; the
    // partial flag travels separately so the badge can attach.
    const partial = input({ basisRequestCount: 4, basisInputTokens: 800, basisCacheReadTokens: 400, requestCount: 6 });
    expect(windowCacheReadShare(partial)).toEqual({ kind: "value", share: 400 / 1200 });
    expect(cacheBasisPartialCoverage(partial)).toBe(true);
    expect(cacheBasisPartialCoverage(input({ basisRequestCount: 6, requestCount: 6 }))).toBe(false);
    expect(cacheBasisPartialCoverage(input({ basisRequestCount: 0, requestCount: 6, basisInputTokens: null, basisCacheReadTokens: null, basisCacheCreationTokens: null }))).toBe(false);
  });
});