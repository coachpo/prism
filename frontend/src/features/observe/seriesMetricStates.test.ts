import { describe, expect, it } from "vitest";

import type { UsageSeriesResponse } from "@/lib/api/observability";
import {
  bucketCacheBasisPartialCoverage,
  bucketCacheReadShare,
  bucketOutputRate,
  bucketOutputRatePartialCoverage,
} from "./seriesMetricStates";

type SeriesPoint = UsageSeriesResponse["series"][number]["points"][number];

function point(overrides: Partial<SeriesPoint> = {}): SeriesPoint {
  return {
    bucket_start: "2026-08-08T00:00:00Z",
    request_count: 10,
    http_success_count: 9,
    http_failed_count: 1,
    failed_count: 1,
    client_disconnected_count: 0,
    ttft_sample_count: 10,
    p50_ttft_ms: null,
    p95_ttft_ms: null,
    total_tokens: null,
    known_cost_micros: null,
    output_rate_sample_count: 0,
    avg_output_rate_tps: null,
    cache_basis_request_count: 0,
    cache_basis_input_tokens: null,
    cache_basis_cache_read_tokens: null,
    cache_basis_cache_creation_tokens: null,
    pricing_reconciliation: {
      pricing_eligible_request_count: 10,
      pricing_ineligible_request_count: 0,
      priced_request_count: 0,
      unpriced_request_count: 0,
      pricing_unknown_request_count: 10,
      unpriced_reason_counts: {
        PRICING_DISABLED: 0,
        MISSING_TOKEN_USAGE: 0,
        STREAM_USAGE_UNAVAILABLE: 0,
        MISSING_PRICE_DATA: 0,
      },
      pricing_coverage_state: "no_trusted_cost",
    },
    ...overrides,
  };
}

describe("bucketOutputRate", () => {
  it("keeps a measured zero as a value, never a no-sample state", () => {
    const rate = bucketOutputRate(
      point({ output_rate_sample_count: 2, avg_output_rate_tps: 0 }),
    );
    expect(rate).toEqual({ kind: "value", tps: 0, samples: 2 });
  });

  it("reports no_sample when the average is missing or no samples exist", () => {
    expect(
      bucketOutputRate(
        point({ output_rate_sample_count: 0, avg_output_rate_tps: null }),
      ),
    ).toEqual({
      kind: "no_sample",
    });
    // A nonzero average without samples cannot happen, but a null average with
    // a stale count must still read as absent rather than crash.
    expect(
      bucketOutputRate(
        point({ output_rate_sample_count: 3, avg_output_rate_tps: null }),
      ),
    ).toEqual({
      kind: "no_sample",
    });
  });

  it("flags partial coverage only when some but not all requests were sampled", () => {
    expect(
      bucketOutputRatePartialCoverage(
        point({ request_count: 5, output_rate_sample_count: 5 }),
      ),
    ).toBe(false);
    expect(
      bucketOutputRatePartialCoverage(
        point({ request_count: 5, output_rate_sample_count: 3 }),
      ),
    ).toBe(true);
    expect(
      bucketOutputRatePartialCoverage(
        point({ request_count: 5, output_rate_sample_count: 0 }),
      ),
    ).toBe(false);
  });
});

describe("bucketCacheReadShare", () => {
  it("computes the share over input + read + creation", () => {
    const share = bucketCacheReadShare(
      point({
        cache_basis_request_count: 4,
        cache_basis_input_tokens: 600,
        cache_basis_cache_read_tokens: 200,
        cache_basis_cache_creation_tokens: 200,
      }),
    );
    expect(share).toEqual({ kind: "value", share: 0.2 });
  });

  it("keeps a measured 0% separate from an unusable basis", () => {
    expect(
      bucketCacheReadShare(
        point({
          cache_basis_request_count: 2,
          cache_basis_input_tokens: 100,
          cache_basis_cache_read_tokens: 0,
          cache_basis_cache_creation_tokens: 0,
        }),
      ),
    ).toEqual({ kind: "value", share: 0 });
    expect(
      bucketCacheReadShare(point({ cache_basis_request_count: 0 })),
    ).toEqual({
      kind: "no_comparable_rows",
    });
    expect(
      bucketCacheReadShare(
        point({
          cache_basis_request_count: 1,
          cache_basis_input_tokens: 0,
          cache_basis_cache_read_tokens: 0,
          cache_basis_cache_creation_tokens: 0,
        }),
      ),
    ).toEqual({ kind: "no_denominator" });
  });

  it("treats null basis sums of comparable rows as zero components, not absence", () => {
    // SQL SUM returns null only for zero rows; a positive basis count with
    // null sums is a contract violation upstream and must not divide.
    const share = bucketCacheReadShare(
      point({
        cache_basis_request_count: 1,
        cache_basis_input_tokens: null,
        cache_basis_cache_read_tokens: null,
        cache_basis_cache_creation_tokens: null,
      }),
    );
    expect(share).toEqual({ kind: "no_denominator" });
  });

  it("flags partial coverage only inside a non-empty basis below the bucket total", () => {
    expect(
      bucketCacheBasisPartialCoverage(
        point({ request_count: 8, cache_basis_request_count: 8 }),
      ),
    ).toBe(false);
    expect(
      bucketCacheBasisPartialCoverage(
        point({ request_count: 8, cache_basis_request_count: 3 }),
      ),
    ).toBe(true);
    expect(
      bucketCacheBasisPartialCoverage(
        point({ request_count: 8, cache_basis_request_count: 0 }),
      ),
    ).toBe(false);
  });
});
