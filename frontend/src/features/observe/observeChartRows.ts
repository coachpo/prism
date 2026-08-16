import type { ObserveGroupBy, ObserveMetric } from "@/features/observe/observeSearch";
import type { UsageSeriesResponse } from "@/lib/api/observability";

type UsageSeriesEntry = UsageSeriesResponse["series"][number];

export type ObserveChartRow = Record<string, unknown> & { bucket: string };

/**
 * One drawn mark. `key` is at once the recharts `dataKey`, the legend toggle
 * id, and the field `buildObserveChartRows` writes, so a bar can never end up
 * bound to a field nobody set.
 */
export type ObserveChartMark = {
  /** Spectrum position; the legend swatch and the mark read the same one. */
  colorIndex: number;
  key: string;
  label: string;
};

/**
 * Bucket starts are fixed-width UTC RFC3339, so a plain string sort is
 * chronological.
 */
function orderedBuckets(series: readonly UsageSeriesEntry[]): string[] {
  const bucketStarts = new Set<string>();
  for (const item of series) {
    for (const point of item.points) bucketStarts.add(point.bucket_start);
  }
  return Array.from(bucketStarts).sort();
}

/**
 * The window's last observed bucket across every series. The first series is
 * only the busiest one; it can stop before the window does.
 */
export function lastObservedBucket(series: readonly UsageSeriesEntry[]): string | undefined {
  return orderedBuckets(series).at(-1);
}

/** Requests split into a success/failed stack only while nothing is grouped. */
export function isStackedRequestChart(metric: ObserveMetric, groupBy: ObserveGroupBy): boolean {
  return metric === "requests" && groupBy === "none";
}

/**
 * The marks the chart draws, in order. Stack segments are localized by the
 * caller because everything else here is a server-supplied series label.
 */
export function observeChartMarks(
  series: readonly UsageSeriesEntry[],
  metric: ObserveMetric,
  groupBy: ObserveGroupBy,
  stackLabels: { failed: string; success: string },
): ObserveChartMark[] {
  if (isStackedRequestChart(metric, groupBy)) {
    const ungrouped = series[0];
    if (!ungrouped) return [];
    // Success and failure are a fixed pair, not two entries of the spectrum:
    // green and pink regardless of where the one series sits in it.
    return [
      { colorIndex: 2, key: `${ungrouped.key}-success`, label: stackLabels.success },
      { colorIndex: 4, key: `${ungrouped.key}-failed`, label: stackLabels.failed },
    ];
  }
  if (metric === "ttft") {
    return series.flatMap((item, index) => [
      { colorIndex: index * 2, key: `${item.key}-p50`, label: `${item.label} P50` },
      { colorIndex: index * 2 + 1, key: `${item.key}-p95`, label: `${item.label} P95` },
    ]);
  }
  return series.map((item, index) => ({ colorIndex: index, key: item.key, label: item.label }));
}

/**
 * One row per bucket, one field per mark. The bucket axis is the union of every
 * series, not the first one: the read model writes a bucket row only where that
 * entity had traffic, so the top-by-request-count series does not know the
 * whole window.
 */
export function buildObserveChartRows(
  series: readonly UsageSeriesEntry[],
  metric: ObserveMetric,
  groupBy: ObserveGroupBy,
): ObserveChartRow[] {
  if (series.length === 0) return [];
  const stacked = isStackedRequestChart(metric, groupBy);
  const rows: ObserveChartRow[] = orderedBuckets(series).map((bucket) => ({ bucket }));
  for (const item of series) {
    const pointsByBucket = new Map(item.points.map((point) => [point.bucket_start, point]));
    for (const row of rows) {
      const point = pointsByBucket.get(row.bucket);
      // A bucket this entity has no row for stays unwritten, so the mark is a
      // gap. Filling it with 0 would claim observed silence.
      if (!point) continue;
      const key = item.key;
      if (metric === "requests") {
        if (stacked) {
          row[`${key}-success`] = point.http_success_count;
          row[`${key}-failed`] = point.http_failed_count;
        } else {
          // Grouped requests draw one bar per series key. `request_count` is
          // the total the table's 窗口合计 column sums, and the read model
          // derives success from it, so the two views cannot disagree.
          row[key] = point.request_count;
        }
      } else if (metric === "errors") {
        row[key] = point.failed_count + point.client_disconnected_count;
      } else if (metric === "ttft") {
        row[`${key}-p50`] = point.p50_ttft_ms;
        row[`${key}-p95`] = point.p95_ttft_ms;
      } else if (metric === "tokens") {
        row[key] = point.total_tokens;
      } else if (metric === "cost") {
        row[key] = point.known_cost_micros === null ? null : Number(point.known_cost_micros) / 1_000_000;
      }
    }
  }
  return rows;
}
