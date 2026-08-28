import { useCallback } from "react";
import { Download } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import type { FragmentState } from "@/features/observe/useObserveFragments";
import type { QueryContextResponse, UsageSummaryResponse, DashboardNowResponse, UsageSeriesResponse } from "@/lib/api/observability";

/**
 * Observe JSON export (SPEC §14.4): exports the last successfully accepted,
 * semantically consistent aggregate results of the current selection, with
 * actual coverage, retention range, generation time, window/granularity,
 * HTTP and final-result semantics, data freshness and per-fragment
 * completeness. Stale or partially failed fragments are identifiable in the
 * export and never masquerade as a complete fresh snapshot. Bounded
 * drill-downs that were not loaded are marked as not included — the export
 * never triggers unbounded `all`/Terminal Target/error aggregation.
 */
export function ObserveExportButton({
  preset,
  metric,
  scope,
  groupBy,
  interval,
  queryContextFragment,
  summaryFragment,
  nowFragment,
  seriesFragment,
  costSegmentKey,
}: {
  preset: string;
  metric: string;
  scope: string;
  groupBy: string;
  interval: string;
  queryContextFragment: FragmentState<QueryContextResponse>;
  summaryFragment: FragmentState<UsageSummaryResponse>;
  nowFragment: FragmentState<DashboardNowResponse>;
  seriesFragment: FragmentState<UsageSeriesResponse>;
  costSegmentKey?: string;
}) {
  const { messages } = useLocale();

  const handleExport = useCallback(() => {
    const exportedAt = new Date().toISOString();
    const fragmentState = (fragment: FragmentState<unknown>): string =>
      fragment.phase === "error" ? (fragment.stale ? "stale_error" : "error") : fragment.phase === "ready" ? (fragment.stale ? "stale" : "ready") : "loading";

    const payload = {
      schema: "prism.observe.export.v2",
      exported_at: exportedAt,
      selection: {
        tab: "current",
        preset,
        metric,
        scope,
        group_by: groupBy,
        interval,
        cost_segment_key: costSegmentKey ?? null,
      },
      fragments: {
        query_context: {
          state: fragmentState(queryContextFragment),
          scope: queryContextFragment.data?.scope ?? scope,
          caliber: queryContextFragment.data?.caliber ?? null,
          dataset_coverage: queryContextFragment.data
            ? {
                usage_request_events:
                  queryContextFragment.data.usage_coverage,
                request_logs: queryContextFragment.data.request_coverage,
                loadbalance_events: queryContextFragment.data.event_coverage,
              }
            : null,
          bounds: queryContextFragment.data
            ? {
                requested: queryContextFragment.data.requested_bounds,
                usage: queryContextFragment.data.usage_bounds,
                requests: queryContextFragment.data.request_bounds,
                events: queryContextFragment.data.event_bounds,
              }
            : null,
          data: queryContextFragment.data,
        },
        summary: {
          state: fragmentState(summaryFragment),
          scope: summaryFragment.data?.caliber?.scope ?? "ingress",
          caliber: summaryFragment.data?.caliber ?? null,
          dataset_coverage: summaryFragment.data?.dataset_coverage ?? null,
          bounds: summaryFragment.data?.coverage
            ? {
                from_time: summaryFragment.data.coverage.from_time,
                to_time: summaryFragment.data.coverage.to_time,
              }
            : null,
          data: summaryFragment.data,
        },
        now: {
          state: fragmentState(nowFragment),
          scope: "global_current_state",
          caliber: null,
          dataset_coverage: nowFragment.data?.rolling?.coverage
            ? { usage_request_events: nowFragment.data.rolling.coverage }
            : null,
          bounds: nowFragment.data?.rolling?.coverage
            ? {
                from_time: nowFragment.data.rolling.coverage.from_time,
                to_time: nowFragment.data.rolling.coverage.to_time,
              }
            : null,
          data: nowFragment.data,
        },
        series: {
          state: fragmentState(seriesFragment),
          scope: seriesFragment.data?.caliber?.scope ?? scope,
          caliber: seriesFragment.data?.caliber ?? null,
          dataset_coverage: seriesFragment.data?.dataset_coverage ?? null,
          bounds: seriesFragment.data?.coverage
            ? {
                from_time: seriesFragment.data.coverage.from_time,
                to_time: seriesFragment.data.coverage.to_time,
              }
            : null,
          data: seriesFragment.data,
        },
        // Bounded drill-downs that were not loaded are explicitly excluded.
        error_breakdown: { state: "not_included", data: null },
        activity: { state: "not_included", data: null },
        events: { state: "not_included", data: null },
      },
      freshness: {
        query_context_generated_at: queryContextFragment.data?.generated_at ?? null,
        summary_generated_at: summaryFragment.data?.generated_at ?? null,
        now_generated_at: nowFragment.data?.generated_at ?? null,
        series_generated_at: seriesFragment.data?.generated_at ?? null,
        exported_at: exportedAt,
      },
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `observe-${preset}-${metric}.json`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    URL.revokeObjectURL(url);
  }, [preset, metric, scope, groupBy, interval, costSegmentKey, queryContextFragment, summaryFragment, nowFragment, seriesFragment]);

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={() => void handleExport()}
      data-testid="observe-export-json"
      disabled={queryContextFragment.phase === "loading"}
    >
      <Download className="size-3.5" />
      {messages.observe.exportJson}
    </Button>
  );
}
