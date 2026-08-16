import { useMemo, useState } from "react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { OBSERVE_GROUPS, OBSERVE_METRICS, type ObserveGroupBy, type ObserveMetric } from "@/features/observe/observeSearch";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import type { FragmentState } from "@/features/observe/useObserveFragments";
import { cn } from "@/lib/utils";
import {
  OperatorEmptyState,
  OperatorErrorState,
  OperatorMissingValue,
  OperatorStalenessBadge,
} from "@/shared/design-system";

/**
 * Series take the spectrum in order through `var(--chart-N)`; hard-coded hex
 * is a defect. Adjacent series alternate solid and dashed so the set stays
 * separable without relying on hue.
 */
const SERIES_TOKENS = [
  "var(--chart-1)",
  "var(--chart-2)",
  "var(--chart-3)",
  "var(--chart-4)",
  "var(--chart-5)",
  "var(--chart-6)",
] as const;

function seriesStroke(index: number): string {
  return SERIES_TOKENS[index % SERIES_TOKENS.length];
}

function seriesDash(index: number): string | undefined {
  return index % 2 === 1 ? "5 3" : undefined;
}

type ChartRow = Record<string, unknown> & { bucket: string };

export function ObserveMainChart({
  fragment,
  metric,
  groupBy,
  onMetricChange,
  onGroupByChange,
}: {
  fragment: FragmentState<UsageSeriesResponse>;
  metric: ObserveMetric;
  groupBy: ObserveGroupBy;
  onMetricChange: (metric: ObserveMetric) => void;
  onGroupByChange: (groupBy: ObserveGroupBy) => void;
}) {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime, timezone } = useTimezone();
  const copy = messages.observe;
  const [mode, setMode] = useState<"chart" | "table">("chart");
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());

  const series = useMemo(() => fragment.data?.series ?? [], [fragment.data]);
  const chartData = useMemo<ChartRow[]>(() => {
    if (series.length === 0) return [];
    const rows: ChartRow[] = series[0].points.map((point) => ({ bucket: point.bucket_start }));
    for (const item of series) {
      const pointsByBucket = new Map(item.points.map((point) => [point.bucket_start, point]));
      for (const row of rows) {
        const point = pointsByBucket.get(row.bucket);
        if (!point) continue;
        const key = item.key;
        if (metric === "requests") {
          row[`${key}-success`] = point.http_success_count;
          row[`${key}-failed`] = point.http_failed_count;
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
  }, [series, metric]);

  const showStacked = metric === "requests" && groupBy === "none";
  const unit = copy.metricUnit(metric);
  const timezoneLabel = timezone ?? "";

  const legendEntries = useMemo(() => {
    if (showStacked) {
      return [
        { color: "var(--chart-3)", key: "total-success", label: copy.httpSuccessShort },
        { color: "var(--chart-5)", key: "total-failed", label: copy.httpFailedShort },
      ];
    }
    if (metric === "ttft") {
      return series.flatMap((item, index) => [
        { color: seriesStroke(index * 2), key: `${item.key}-p50`, label: `${item.label} P50` },
        { color: seriesStroke(index * 2 + 1), key: `${item.key}-p95`, label: `${item.label} P95` },
      ]);
    }
    return series.map((item, index) => ({
      color: seriesStroke(index),
      key: item.key,
      label: item.label,
    }));
  }, [copy.httpFailedShort, copy.httpSuccessShort, metric, series, showStacked]);

  const axisTick = { fill: "var(--chart-axis)", fontSize: 11 };
  const formatBucket = (value: string) => formatTime(value, { hour: "2-digit", minute: "2-digit" });

  return (
    <section className="flex flex-col gap-3" data-testid="observe-main-chart">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Segmented
          ariaLabel={copy.metricLabel}
          options={OBSERVE_METRICS.map((value) => ({ label: copy.metricName(value), value }))}
          value={metric}
          onChange={(value) => onMetricChange(value as ObserveMetric)}
        />
        <div className="flex items-center gap-2">
          <Segmented
            ariaLabel={copy.groupLabel}
            options={OBSERVE_GROUPS.map((value) => ({ label: copy.groupName(value), value }))}
            value={groupBy}
            onChange={(value) => onGroupByChange(value as ObserveGroupBy)}
          />
          <Segmented
            ariaLabel={copy.chartTableSwitcherLabel}
            options={[
              { label: copy.chartView, value: "chart" },
              { label: copy.tableView, value: "table" },
            ]}
            value={mode}
            onChange={(value) => setMode(value as "chart" | "table")}
          />
        </div>
      </div>

      {fragment.stale && fragment.data ? (
        <OperatorStalenessBadge
          className="self-start"
          label={copy.staleDataNote}
          reason={fragment.error ?? undefined}
        />
      ) : null}

      {fragment.phase === "loading" && fragment.data === null ? (
        <Skeleton className="h-64 rounded-md" />
      ) : fragment.phase === "error" && fragment.data === null ? (
        <OperatorErrorState
          testId="main-chart-error"
          title={copy.windowUnavailable}
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
        />
      ) : chartData.length === 0 ? (
        // No data draws no empty axes.
        <OperatorEmptyState title={copy.noData} description={copy.adjustFiltersHint} />
      ) : mode === "table" ? (
        <SeriesTable fragment={fragment} metric={metric} formatBucket={formatBucket} />
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1">
            {legendEntries.map((entry) => {
              const isHidden = hidden.has(entry.key);
              return (
                <button
                  key={entry.key}
                  type="button"
                  aria-pressed={!isHidden}
                  title={copy.seriesToggleHint}
                  onClick={() =>
                    setHidden((current) => {
                      const next = new Set(current);
                      if (next.has(entry.key)) next.delete(entry.key);
                      else next.add(entry.key);
                      return next;
                    })
                  }
                  className={cn(
                    "inline-flex items-center gap-1.5 text-xs",
                    isHidden ? "text-text-disabled" : "text-muted-foreground",
                  )}
                >
                  <span
                    aria-hidden="true"
                    className="h-0.5 w-3 rounded-full"
                    style={{ backgroundColor: entry.color, opacity: isHidden ? 0.35 : 1 }}
                  />
                  {entry.label}
                </button>
              );
            })}
          </div>
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              {metric === "ttft" ? (
                <LineChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
                  <CartesianGrid vertical={false} stroke="var(--chart-grid)" />
                  <XAxis
                    dataKey="bucket"
                    tick={axisTick}
                    tickLine={false}
                    interval="preserveStartEnd"
                    tickFormatter={formatBucket}
                    label={{ value: copy.axisTimezone(timezoneLabel), position: "insideBottom", offset: -4, fill: "var(--chart-axis)", fontSize: 11 }}
                  />
                  <YAxis tick={axisTick} tickLine={false} axisLine={false} width={52} unit={unit ? ` ${unit}` : undefined} />
                  <Tooltip cursor={{ stroke: "var(--chart-cursor)" }} content={<SeriesTooltip formatBucket={formatBucket} unit={unit} />} />
                  {series.flatMap((item, index) =>
                    [
                      { dataKey: `${item.key}-p50`, index: index * 2, name: `${item.label} P50` },
                      { dataKey: `${item.key}-p95`, index: index * 2 + 1, name: `${item.label} P95` },
                    ].map((line) =>
                      hidden.has(line.dataKey) ? null : (
                        <Line
                          key={line.dataKey}
                          type="linear"
                          dataKey={line.dataKey}
                          name={line.name}
                          dot={false}
                          strokeWidth={1.5}
                          stroke={seriesStroke(line.index)}
                          strokeDasharray={seriesDash(line.index)}
                        />
                      ),
                    ),
                  )}
                </LineChart>
              ) : (
                <BarChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
                  <CartesianGrid vertical={false} stroke="var(--chart-grid)" />
                  <XAxis
                    dataKey="bucket"
                    tick={axisTick}
                    tickLine={false}
                    interval="preserveStartEnd"
                    tickFormatter={formatBucket}
                    label={{ value: copy.axisTimezone(timezoneLabel), position: "insideBottom", offset: -4, fill: "var(--chart-axis)", fontSize: 11 }}
                  />
                  <YAxis tick={axisTick} tickLine={false} axisLine={false} width={52} unit={unit ? ` ${unit}` : undefined} />
                  <Tooltip cursor={{ fill: "var(--chart-cursor-fill)" }} content={<SeriesTooltip formatBucket={formatBucket} unit={unit} />} />
                  {showStacked ? (
                    <>
                      {hidden.has("total-success") ? null : (
                        <Bar dataKey="total-success" name={copy.httpSuccessShort} stackId="a" fill="var(--chart-3)" />
                      )}
                      {hidden.has("total-failed") ? null : (
                        <Bar dataKey="total-failed" name={copy.httpFailedShort} stackId="a" fill="var(--chart-5)" />
                      )}
                    </>
                  ) : (
                    series.map((item, index) =>
                      hidden.has(item.key) ? null : (
                        <Bar key={item.key} dataKey={item.key} name={item.label} fill={seriesStroke(index)} />
                      ),
                    )
                  )}
                </BarChart>
              )}
            </ResponsiveContainer>
          </div>
        </>
      )}
      <p className="text-xs text-muted-foreground">{formatNumber(chartData.length)} {copy.buckets}</p>
    </section>
  );
}

function Segmented({
  ariaLabel,
  onChange,
  options,
  value,
}: {
  ariaLabel: string;
  onChange: (value: string) => void;
  options: readonly { label: string; value: string }[];
  value: string;
}) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className="flex w-fit items-center gap-0.5 rounded-md border border-border bg-inset p-0.5"
    >
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          onClick={() => onChange(option.value)}
          aria-pressed={value === option.value}
          className={cn(
            "rounded-[4px] px-2 py-1 text-xs font-medium transition-colors",
            value === option.value
              ? "bg-primary-soft text-on-primary-soft"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

/** Series name left, value right in mono, on `raised`. */
function SeriesTooltip({
  active,
  formatBucket,
  label,
  payload,
  unit,
}: {
  active?: boolean;
  formatBucket: (value: string) => string;
  label?: string;
  payload?: { color?: string; name?: string; value?: number | string | null }[];
  unit: string;
}) {
  if (!active || !payload?.length) return null;
  return (
    <div className="operator-overlay-surface min-w-44 rounded-lg border p-2 text-xs">
      <p className="mb-1 font-mono tabular-nums text-muted-foreground">
        {label ? formatBucket(label) : ""}
      </p>
      <ul className="flex flex-col gap-0.5">
        {payload.map((entry, index) => (
          <li key={`${entry.name}-${index}`} className="flex items-center gap-2">
            <span aria-hidden="true" className="size-1.5 shrink-0 rounded-full" style={{ backgroundColor: entry.color }} />
            <span className="min-w-0 flex-1 truncate">{entry.name}</span>
            <span className="shrink-0 font-mono tabular-nums">
              {entry.value === null || entry.value === undefined ? "—" : `${entry.value}${unit ? ` ${unit}` : ""}`}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * The window total and the last bucket are two different bases, so they get
 * their own column headers rather than sitting in one row unlabelled.
 */
function SeriesTable({
  formatBucket,
  fragment,
  metric,
}: {
  formatBucket: (value: string) => string;
  fragment: FragmentState<UsageSeriesResponse>;
  metric: ObserveMetric;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;
  if (fragment.data === null || fragment.data.series.length === 0) return null;

  const items = fragment.data.series;
  const lastBucketStart = items[0]?.points.at(-1)?.bucket_start;
  const lastBucketLabel = lastBucketStart ? copy.lastBucketColumn(formatBucket(lastBucketStart)) : copy.lastBucketColumn("—");

  return (
    <div className="overflow-hidden rounded-md border border-border">
      <div className="overflow-x-auto">
        <Table aria-label={copy.semanticTable}>
          <TableHeader>
            <TableRow>
              <TableHead>{copy.seriesLabel}</TableHead>
              <TableHead className="text-right">
                {copy.windowTotalColumn} · {copy.requests}
              </TableHead>
              {metric === "requests" ? (
                <>
                  <TableHead className="text-right">{lastBucketLabel} · {copy.httpSuccessShort}</TableHead>
                  <TableHead className="text-right">{lastBucketLabel} · {copy.httpFailedShort}</TableHead>
                </>
              ) : null}
              {metric === "ttft" ? (
                <>
                  <TableHead className="text-right">{lastBucketLabel} · P50</TableHead>
                  <TableHead className="text-right">{lastBucketLabel} · P95</TableHead>
                </>
              ) : null}
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => {
              const lastPoint = item.points.at(-1);
              return (
                <TableRow key={item.key}>
                  <TableCell>{item.label}</TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {formatNumber(item.request_count)}
                  </TableCell>
                  {metric === "requests" ? (
                    <>
                      <TableCell className="text-right font-mono tabular-nums">
                        <Cell value={lastPoint?.http_success_count} />
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        <Cell value={lastPoint?.http_failed_count} />
                      </TableCell>
                    </>
                  ) : null}
                  {metric === "ttft" ? (
                    <>
                      <TableCell className="text-right font-mono tabular-nums">
                        <Cell value={lastPoint?.p50_ttft_ms} />
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        <Cell value={lastPoint?.p95_ttft_ms} />
                      </TableCell>
                    </>
                  ) : null}
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

function Cell({ value }: { value: number | null | undefined }) {
  const { formatNumber, messages } = useLocale();
  if (value === null || value === undefined) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  return <>{formatNumber(value)}</>;
}
