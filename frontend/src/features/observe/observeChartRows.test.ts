import { describe, expect, it } from "vitest";
import {
  buildObserveChartRows,
  lastObservedBucket,
  lineMetricDomain,
  observeChartMarks,
} from "./observeChartRows";
import {
  OBSERVE_GROUPS,
  OBSERVE_METRICS,
  type ObserveGroupBy,
} from "./observeSearch";
import type { UsageSeriesResponse } from "@/lib/api/observability";

type SeriesEntry = UsageSeriesResponse["series"][number];
type SeriesPoint = SeriesEntry["points"][number];

const BUCKETS = [
  "2026-08-08T00:00:00Z",
  "2026-08-08T01:00:00Z",
  "2026-08-08T02:00:00Z",
] as const;

const PRICING_RECONCILIATION: SeriesPoint["pricing_reconciliation"] = {
  pricing_eligible_request_count: 1,
  pricing_ineligible_request_count: 0,
  priced_request_count: 1,
  unpriced_request_count: 0,
  pricing_unknown_request_count: 0,
  unpriced_reason_counts: {
    PRICING_DISABLED: 0,
    MISSING_TOKEN_USAGE: 0,
    STREAM_USAGE_UNAVAILABLE: 0,
    MISSING_PRICE_DATA: 0,
  },
  pricing_coverage_state: "complete",
};

function point(bucketStart: string, requestCount: number): SeriesPoint {
  return {
    bucket_start: bucketStart,
    request_count: requestCount,
    http_success_count: requestCount - 1,
    http_failed_count: 1,
    failed_count: 1,
    client_disconnected_count: 0,
    ttft_sample_count: requestCount,
    p50_ttft_ms: 2900,
    p95_ttft_ms: 4900,
    total_tokens: 350,
    known_cost_micros: "10470000",
    output_rate_sample_count: 2,
    avg_output_rate_tps: 120.5,
    cache_basis_request_count: 1,
    cache_basis_input_tokens: 400,
    cache_basis_cache_read_tokens: 100,
    cache_basis_cache_creation_tokens: 0,
    pricing_reconciliation: PRICING_RECONCILIATION,
  };
}

/** The read model sums the window total from the buckets, so the fixture does too. */
function entry(key: string, points: SeriesPoint[]): SeriesEntry {
  const requestCount = points.reduce(
    (total, item) => total + item.request_count,
    0,
  );
  return {
    key,
    entity_id: key,
    label: key,
    configured: true,
    request_count: requestCount,
    points,
  };
}

/**
 * The read model writes a bucket row only where that entity had traffic, so the
 * two grouped series deliberately do not share every bucket.
 */
function seriesFor(groupBy: ObserveGroupBy): SeriesEntry[] {
  if (groupBy === "none") {
    return [entry("total", [point(BUCKETS[0], 500), point(BUCKETS[1], 400)])];
  }
  return [
    entry(`${groupBy}:17`, [point(BUCKETS[0], 500), point(BUCKETS[1], 400)]),
    entry(`${groupBy}:23`, [point(BUCKETS[1], 464), point(BUCKETS[2], 120)]),
  ];
}

describe("observe chart rows", () => {
  it("pins percentage and rate domains without changing TTFT autoscaling", () => {
    expect(lineMetricDomain("cache_read_share")).toEqual([0, 100]);
    expect(lineMetricDomain("output_rate")).toEqual([0, "auto"]);
    expect(lineMetricDomain("ttft")).toBeUndefined();
  });

  it("writes a field for every mark the chart draws, in every metric and group", () => {
    for (const metric of OBSERVE_METRICS) {
      for (const groupBy of OBSERVE_GROUPS) {
        const series = seriesFor(groupBy);
        const rows = buildObserveChartRows(series, metric, groupBy);
        const marks = observeChartMarks(series, metric, groupBy, {
          failed: "失败",
          success: "成功",
        });

        expect(
          marks.length,
          `${metric}/${groupBy} draws nothing`,
        ).toBeGreaterThan(0);
        for (const mark of marks) {
          // A dataKey nobody wrote still renders an empty mark rather than
          // failing, so presence of the field is what has to be asserted.
          const written = rows.filter((row) => Object.hasOwn(row, mark.key));
          expect(
            written.length,
            `${metric}/${groupBy} left ${mark.key} unwritten`,
          ).toBeGreaterThan(0);
        }
      }
    }
  });

  it("spans the union of every series and leaves an entity's absent bucket blank", () => {
    const series = seriesFor("terminal_target");
    const rows = buildObserveChartRows(series, "requests", "terminal_target");

    expect(rows.map((row) => row.bucket)).toEqual([...BUCKETS]);
    // Zero would claim observed silence; a missing field draws no mark at all.
    expect(Object.hasOwn(rows[0], "terminal_target:23")).toBe(false);
    expect(rows[1]["terminal_target:23"]).toBe(464);
    expect(Object.hasOwn(rows[2], "terminal_target:17")).toBe(false);
    // The busiest series stops one bucket early and does not end the window.
    expect(lastObservedBucket(series)).toBe(BUCKETS[2]);
  });

  // A terminal target with no pricing template is a supported configuration,
  // not a defect: those requests are recorded `PRICING_DISABLED` forever. The
  // chart therefore has to keep "cost not computed" apart from "cost was zero",
  // because a null drawn as 0 reads as a priced request that happened to be
  // free.
  it("separates an unpriced bucket from a genuinely zero-cost one", () => {
    const series = [
      entry("total", [
        { ...point(BUCKETS[0], 500), known_cost_micros: null },
        { ...point(BUCKETS[1], 400), known_cost_micros: "0" },
        { ...point(BUCKETS[2], 120), known_cost_micros: "10470000" },
      ]),
    ];
    const rows = buildObserveChartRows(series, "cost", "none");

    // Unpriced stays null: the mark is absent, not a bar sitting on the axis.
    expect(rows[0]["total"]).toBeNull();
    // A priced request that cost nothing is an observation, and keeps its zero.
    expect(rows[1]["total"]).toBe(0);
    expect(rows[2]["total"]).toBe(10.47);
  });

  it("keeps the ungrouped request stack on the success and failed segments", () => {
    const series = seriesFor("none");
    const marks = observeChartMarks(series, "requests", "none", {
      failed: "失败",
      success: "成功",
    });
    const rows = buildObserveChartRows(series, "requests", "none");

    expect(marks.map((mark) => mark.key)).toEqual([
      "total-success",
      "total-failed",
    ]);
    expect(rows[0]["total-success"]).toBe(499);
    expect(rows[0]["total-failed"]).toBe(1);
    expect(Object.hasOwn(rows[0], "total")).toBe(false);
  });

  // Output rate and cache share draw only measured readings. An unsampled
  // bucket keeps an explicit null (a gap), a measured zero keeps its zero, and
  // an unusable cache basis never becomes a plotted 0%.
  it("draws output-rate gaps for unsampled buckets and keeps genuine zeros", () => {
    const series = [
      entry("total", [
        {
          ...point(BUCKETS[0], 500),
          output_rate_sample_count: 0,
          avg_output_rate_tps: null,
        },
        {
          ...point(BUCKETS[1], 400),
          output_rate_sample_count: 3,
          avg_output_rate_tps: 0,
        },
        {
          ...point(BUCKETS[2], 120),
          output_rate_sample_count: 2,
          avg_output_rate_tps: 88.25,
        },
      ]),
    ];
    const rows = buildObserveChartRows(series, "output_rate", "none");

    expect(rows[0]["total"]).toBeNull();
    expect(rows[1]["total"]).toBe(0);
    expect(rows[2]["total"]).toBeCloseTo(88.25);
  });

  it("draws cache-share gaps where the basis is missing or has no denominator", () => {
    const series = [
      entry("total", [
        // No comparable rows in the bucket.
        {
          ...point(BUCKETS[0], 500),
          cache_basis_request_count: 0,
          cache_basis_input_tokens: null,
          cache_basis_cache_read_tokens: null,
          cache_basis_cache_creation_tokens: null,
        },
        // Comparable rows whose components all sum to zero.
        {
          ...point(BUCKETS[1], 400),
          cache_basis_request_count: 2,
          cache_basis_input_tokens: 0,
          cache_basis_cache_read_tokens: 0,
          cache_basis_cache_creation_tokens: 0,
        },
        // A real share, including a genuine 0% when nothing is cached.
        {
          ...point(BUCKETS[2], 120),
          cache_basis_request_count: 2,
          cache_basis_input_tokens: 300,
          cache_basis_cache_read_tokens: 100,
          cache_basis_cache_creation_tokens: 0,
        },
      ]),
    ];
    const rows = buildObserveChartRows(series, "cache_read_share", "none");

    expect(rows[0]["total"]).toBeNull();
    expect(rows[1]["total"]).toBeNull();
    expect(rows[2]["total"]).toBeCloseTo(25);
  });
});
