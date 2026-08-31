// Bucket-level honest states for the output-rate and cache-read-share main
// chart metrics. The backend ships raw components per bucket (sample count +
// per-request mean; cache-basis count + disjoint token sums); this module
// projects them into the distinguishable states so neither a fabricated zero
// nor a collapsed ratio can reach the screen. A failed read never reaches
// here — it renders as the fragment error surface upstream.

import type { UsageSeriesResponse } from "@/lib/api/observability";

type SeriesPoint = UsageSeriesResponse["series"][number]["points"][number];

/** One bucket's output-rate reading. */
export type BucketOutputRate =
 | {
    kind: "value" /** Measured tok/s; 0 is a genuine measured zero. */;
    tps: number;
    samples: number;
   }
 | { kind: "no_sample" };

/**
 * Output rate keeps the backend-authoritative measured-evidence caliber: only
 * requests whose persisted evidence is state=measured (completed, progressive
 * visible text/tool-output delivery with at least two events across at least
 * 50ms) enter the per-request mean. The backend computes the rate; the UI
 * never recomputes it. Without samples the average is missing — never a zero
 * stand-in.
 */
export function bucketOutputRate(point: SeriesPoint): BucketOutputRate {
 if (
  point.avg_output_rate_tps === null ||
  point.output_rate_sample_count === 0
 ) {
  return { kind: "no_sample" };
 }
 return {
  kind: "value",
  tps: point.avg_output_rate_tps,
  samples: point.output_rate_sample_count,
 };
}

/** One bucket's cache-read-share reading. */
export type BucketCacheReadShare =
 | {
    kind: "value" /** Read share in [0,1]; 0 is a genuine measured zero. */;
    share: number;
   }
 | { kind: "no_comparable_rows" }
 | { kind: "no_denominator" };

/**
 * Projects one bucket's cache basis with the window card's state order: a
 * bucket exists only where its series had traffic, so the empty-window state
 * cannot occur here — an absent bucket stays an absent row, not a projected
 * state. A zero denominator yields no ratio.
 */
export function bucketCacheReadShare(point: SeriesPoint): BucketCacheReadShare {
 if (point.cache_basis_request_count === 0)
  return { kind: "no_comparable_rows" };
 const denominator =
  (point.cache_basis_input_tokens ?? 0) +
  (point.cache_basis_cache_read_tokens ?? 0) +
  (point.cache_basis_cache_creation_tokens ?? 0);
 if (denominator <= 0) return { kind: "no_denominator" };
 return {
  kind: "value",
  share: (point.cache_basis_cache_read_tokens ?? 0) / denominator,
 };
}

/**
 * Partial cache-basis coverage inside one bucket: at least one comparable row
 * but not every request in the bucket is comparable.
 */
export function bucketCacheBasisPartialCoverage(point: SeriesPoint): boolean {
 return (
  point.cache_basis_request_count > 0 &&
  point.cache_basis_request_count < point.request_count
 );
}

/**
 * Partial output-rate coverage inside one bucket: some requests were sampled,
 * others had no measurable rate.
 */
export function bucketOutputRatePartialCoverage(point: SeriesPoint): boolean {
 return (
  point.output_rate_sample_count > 0 &&
  point.output_rate_sample_count < point.request_count
 );
}
