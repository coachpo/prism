import { useId, useMemo, useState } from "react";
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
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import {
  metricsForScope,
  OBSERVE_INTERVAL_OPTIONS,
  unavailableGroupsForScope,
  unavailableMetricsForScope,
  type ObserveGroupBy,
  type ObserveMetric,
  type ObserveScope,
  groupsForScope,
} from "@/features/observe/observeSearch";
import {
  buildObserveChartRows,
  isLineMetric,
  isStackedRequestChart,
  lineMetricDomain,
  observeChartMarks,
} from "@/features/observe/observeChartRows";
import { ObserveSeriesTable } from "@/features/observe/ObserveSeriesTable";
import {
  type ObservePointIndex,
  ObserveSeriesTooltip,
} from "@/features/observe/ObserveSeriesTooltip";
import {
  bucketCacheReadShare,
  bucketOutputRate,
} from "@/features/observe/seriesMetricStates";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import type { FragmentState } from "@/features/observe/useObserveFragments";
import { cn } from "@/lib/utils";
import {
  OperatorClippedBadge,
  OperatorEmptyState,
  OperatorErrorState,
  OperatorRetryButton,
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

/**
 * The bottom margin is the room the x-axis caption sits in; without it the
 * caption renders below the plot area and the container clips it. The top
 * margin holds the half of the topmost y-axis tick that sits above its
 * gridline — two lines tall once the unit wraps.
 */
const CHART_MARGIN = { top: 16, right: 8, bottom: 20, left: 0 } as const;

function buildObservePointIndex(
  series: UsageSeriesResponse["series"],
): ObservePointIndex {
  const byBucket: ObservePointIndex = new Map();
  for (const item of series) {
    for (const point of item.points) {
      const bucket =
        byBucket.get(point.bucket_start) ??
        new Map<
          string,
          UsageSeriesResponse["series"][number]["points"][number]
        >();
      bucket.set(item.key, point);
      byBucket.set(point.bucket_start, bucket);
    }
  }
  return byBucket;
}

export function ObserveMainChart({
  fragment,
  metric,
  groupBy,
  interval = "auto",
  onMetricChange,
  onGroupByChange,
  onIntervalChange,
  onRetry,
  onViewChange,
  scope = "ingress",
  view = "chart",
}: {
  fragment: FragmentState<UsageSeriesResponse>;
  metric: ObserveMetric;
  groupBy: ObserveGroupBy;
  interval?: string;
  onMetricChange: (metric: ObserveMetric) => void;
  onGroupByChange: (groupBy: ObserveGroupBy) => void;
  onIntervalChange?: (interval: string) => void;
  onRetry?: () => void;
  /** 图/表切换写进 URL，链接才带得走「他看的是哪一屏」。 */
  onViewChange?: (view: "chart" | "table") => void;
  scope?: ObserveScope;
  view?: "chart" | "table";
}) {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime, timezone } = useTimezone();
  const copy = messages.observe;
  const mode = view;
  const [hidden, setHidden] = useState<ReadonlySet<string>>(() => new Set());
  const intervalLabelId = useId();
  // A deep link may carry a width the strip does not offer; appending it keeps
  // the active bucket visible and re-selectable instead of clamping the URL.
  const intervalOptions = (
    OBSERVE_INTERVAL_OPTIONS as readonly string[]
  ).includes(interval)
    ? OBSERVE_INTERVAL_OPTIONS
    : [...OBSERVE_INTERVAL_OPTIONS, interval];

  const series = useMemo(
    () =>
      (fragment.data?.series ?? []).map((item) => ({
        ...item,
        label: localizedSeriesLabel(
          groupBy,
          item.entity_id ?? item.key,
          item.label,
          messages.requestLogs,
        ),
      })),
    [fragment.data, groupBy, messages.requestLogs],
  );
  const showStacked = isStackedRequestChart(metric, groupBy);
  const chartData = useMemo(
    () => buildObserveChartRows(series, metric, groupBy),
    [groupBy, metric, series],
  );
  const pointIndex = useMemo(() => buildObservePointIndex(series), [series]);

  /**
   * Honest emptiness for the two component-based metrics: when no bucket in
   * the whole window carries a measurable reading, drawing empty axes would
   * suggest a flat line the data never reported. Each gets its own empty
   * state naming what is missing.
   */
  const metricMissingState = useMemo(() => {
    const points = series.flatMap((item) => item.points);
    if (metric === "output_rate") {
      return points.some((point) => bucketOutputRate(point).kind === "value")
        ? null
        : "output_no_sample";
    }
    if (metric === "cache_read_share") {
      if (
        points.some((point) => bucketCacheReadShare(point).kind === "value")
      ) {
        return null;
      }
      return points.some(
        (point) => bucketCacheReadShare(point).kind === "no_denominator",
      )
        ? "cache_no_denominator"
        : "cache_no_comparable";
    }
    return null;
  }, [metric, series]);

  /** Window-level sample and coverage totals across every drawn series. */
  const coverageTotals = useMemo(() => {
    let samples = 0;
    let comparable = 0;
    let requests = 0;
    for (const item of series) {
      for (const point of item.points) {
        requests += point.request_count;
        samples += point.output_rate_sample_count;
        comparable += point.cache_basis_request_count;
      }
    }
    return {
      samples,
      comparable,
      partialSamples: samples > 0 && samples < requests,
      partialBasis: comparable > 0 && comparable < requests,
      requests,
    };
  }, [series]);

  const unit = copy.metricUnit(metric);
  const timezoneLabel = timezone ?? "";

  /**
   * The legend and the marks read one list, so a toggle always names a bar or
   * line that exists and every bar is bound to a field the rows carry.
   */
  const marks = useMemo(
    () =>
      observeChartMarks(series, metric, groupBy, {
        failed: copy.httpFailedShort,
        success: copy.httpSuccessShort,
      }),
    [copy.httpFailedShort, copy.httpSuccessShort, groupBy, metric, series],
  );

  const axisTick = { fill: "var(--chart-axis)", fontSize: 11 };
  const formatBucket = (value: string) =>
    formatTime(value, { hour: "2-digit", minute: "2-digit" });

  return (
    <section className="flex flex-col gap-3" data-testid="observe-main-chart">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          spacing={2}
          value={metric}
          aria-label={copy.metricLabel}
          className="max-w-full flex-wrap"
          onValueChange={(value) => {
            // Radix emits "" when the active item is activated again; the
            // main chart always shows exactly one metric, so ignore it.
            if (value) onMetricChange(value as ObserveMetric);
          }}
        >
          {metricsForScope(scope).map((value) => (
            <ToggleGroupItem
              key={value}
              value={value}
              aria-label={
                value === "cache_read_share" ? copy.cacheReadShare : undefined
              }
            >
              {copy.metricName(value)}
            </ToggleGroupItem>
          ))}
          {/* 本口径不支持的指标禁用并带理由，而不是删掉：删掉之后操作者
              只会看到曲线换了一根，不会知道自己选的指标去了哪里。 */}
          {unavailableMetricsForScope(scope).map((value) => (
            <ToggleGroupItem
              key={value}
              value={value}
              disabled
              title={copy.metricUnavailableReason(value, scope)}
            >
              {copy.metricName(value)}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        {/* 指标、分组、时间桶在窄卡上必须换行：不换行时最后一个分段控件
            会被卡片裁掉，操作者看到的是半个按钮。 */}
        <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
          {fragment.data?.truncated ? (
            <OperatorClippedBadge
              label={copy.seriesTruncatedLabel}
              reason={copy.seriesTruncatedReason(fragment.data.series_limit)}
            />
          ) : null}
          <ToggleGroup
            type="single"
            variant="outline"
            size="sm"
            spacing={2}
            value={groupBy}
            aria-label={copy.groupLabel}
            className="max-w-full flex-wrap"
            onValueChange={(value) => {
              if (value) onGroupByChange(value as ObserveGroupBy);
            }}
          >
            {groupsForScope(scope).map((value) => (
              <ToggleGroupItem key={value} value={value}>
                {copy.groupName(value)}
              </ToggleGroupItem>
            ))}
            {unavailableGroupsForScope(scope).map((value) => (
              <ToggleGroupItem
                key={value}
                value={value}
                disabled
                title={copy.groupUnavailableReason(scope)}
              >
                {copy.groupName(value)}
              </ToggleGroupItem>
            ))}
          </ToggleGroup>
          {/* 桶宽是基准，控件必须挨着它重新定基的那张图；卡副标题写出
              服务端实际生效的宽度，而不是这里请求的 auto。 */}
          {onIntervalChange ? (
            <div className="flex min-w-0 items-center gap-1.5">
              <span
                id={intervalLabelId}
                className="shrink-0 text-xs text-muted-foreground"
              >
                {copy.intervalLabel}
              </span>
              <ToggleGroup
                type="single"
                variant="outline"
                size="sm"
                spacing={2}
                value={interval}
                aria-labelledby={intervalLabelId}
                onValueChange={(value) => {
                  if (value) onIntervalChange(value);
                }}
              >
                {intervalOptions.map((value) => (
                  <ToggleGroupItem key={value} value={value}>
                    {copy.intervalName(value)}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
          ) : null}
        </div>
      </div>

      {/* 指标、分组、时间桶点一下就进 URL，这个开关只换同一份数据的呈现方式、
          不重新定基也不进 URL。留在同一排里，五枚外观一致的分段按钮就会有
          两种持久化语义，而界面不给任何线索，所以它单独占一行。 */}
      <div className="flex justify-end">
        <ToggleGroup
          type="single"
          variant="outline"
          size="sm"
          spacing={2}
          value={mode}
          aria-label={copy.chartTableSwitcherLabel}
          onValueChange={(value) => {
            if (value) onViewChange?.(value as "chart" | "table");
          }}
        >
          <ToggleGroupItem value="chart">{copy.chartView}</ToggleGroupItem>
          <ToggleGroupItem value="table">{copy.tableView}</ToggleGroupItem>
        </ToggleGroup>
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
        // 读取失败：整块替换为错误态并保留重试，绝不降级为空态。
        <OperatorErrorState
          testId="main-chart-error"
          title={copy.windowUnavailable}
          description={messages.honesty.readFailedDescription}
          details={fragment.error}
          detailsLabel={messages.honesty.viewDetails}
          action={
            onRetry ? (
              <OperatorRetryButton onClick={onRetry}>
                {copy.retry}
              </OperatorRetryButton>
            ) : undefined
          }
        />
      ) : chartData.length === 0 ? (
        // No data draws no empty axes.
        <OperatorEmptyState
          title={copy.noData}
          description={copy.adjustFiltersHint}
        />
      ) : mode === "table" ? (
        <ObserveSeriesTable
          fragment={fragment}
          metric={metric}
          formatBucket={formatBucket}
        />
      ) : metricMissingState ? (
        metricMissingState === "output_no_sample" ? (
          <OperatorEmptyState
            testId="output-rate-empty"
            title={copy.outputRateEmptyTitle}
            description={copy.outputRateEmptyDescription}
          />
        ) : metricMissingState === "cache_no_denominator" ? (
          <OperatorEmptyState
            testId="cache-read-share-zero-denominator-empty"
            title={copy.cacheShareNoDenominatorTitle}
            description={copy.cacheReadShareNoDenominator}
          />
        ) : (
          <OperatorEmptyState
            testId="cache-read-share-empty"
            title={copy.cacheShareEmptyTitle}
            description={copy.cacheShareEmptyDescription}
          />
        )
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1">
            {marks.map((entry) => {
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
                    className="w-3 border-t-2"
                    style={{
                      borderTopColor: seriesStroke(entry.colorIndex),
                      borderTopStyle: seriesDash(entry.colorIndex)
                        ? "dashed"
                        : "solid",
                      opacity: isHidden ? 0.35 : 1,
                    }}
                  />
                  {entry.label}
                </button>
              );
            })}
          </div>
          {metric === "output_rate" || metric === "cache_read_share" ? (
            <div
              className="flex flex-wrap items-center gap-x-3 gap-y-1 self-start text-xs text-muted-foreground"
              data-testid={`${metric}-coverage-hint`}
            >
              {metric === "output_rate" ? (
                <>
                  <span>
                    {copy.outputRateSamplesHint(
                      formatNumber(coverageTotals.samples),
                      formatNumber(coverageTotals.requests),
                    )}
                  </span>
                  {coverageTotals.partialSamples ? (
                    <OperatorClippedBadge
                      label={copy.outputRatePartial}
                      reason={copy.outputRatePartialReason}
                    />
                  ) : null}
                </>
              ) : (
                <>
                  <span>
                    {copy.cacheBasisCoverageHint(
                      formatNumber(coverageTotals.comparable),
                      formatNumber(coverageTotals.requests),
                    )}
                  </span>
                  {coverageTotals.partialBasis ? (
                    <OperatorClippedBadge
                      label={copy.cacheReadSharePartial}
                      reason={copy.cacheReadSharePartialReason}
                    />
                  ) : null}
                </>
              )}
            </div>
          ) : null}
          <div className="h-64">
            <ResponsiveContainer width="100%" height="100%">
              {isLineMetric(metric) ? (
                <LineChart data={chartData} margin={CHART_MARGIN}>
                  <CartesianGrid vertical={false} stroke="var(--chart-grid)" />
                  <XAxis
                    dataKey="bucket"
                    tick={axisTick}
                    tickLine={false}
                    // 契约里横线只有一种：网格线。recharts 默认基线是写死的
                    // #666666，两个主题都不跟令牌，亮色下比网格线重 4 倍。
                    axisLine={false}
                    interval="preserveStartEnd"
                    tickFormatter={formatBucket}
                    label={{
                      value: copy.axisTimezone(timezoneLabel),
                      position: "insideBottom",
                      offset: -4,
                      fill: "var(--chart-axis)",
                      fontSize: 11,
                    }}
                  />
                  <YAxis
                    tick={axisTick}
                    tickLine={false}
                    axisLine={false}
                    width={52}
                    unit={unit ? ` ${unit}` : undefined}
                    domain={lineMetricDomain(metric)}
                  />
                  <Tooltip
                    filterNull={false}
                    cursor={{ stroke: "var(--chart-cursor)" }}
                    content={
                      <ObserveSeriesTooltip
                        formatBucket={formatBucket}
                        pointIndex={pointIndex}
                        metric={metric}
                      />
                    }
                  />
                  {/* An array, never a fragment: recharts collects its graphical
                      children through react-is, which does not recognise a
                      React 19 element as a fragment, so anything wrapped in one
                      is invisible to the chart and silently never drawn. */}
                  {marks.map((mark) =>
                    hidden.has(mark.key) ? null : (
                      <Line
                        key={mark.key}
                        type="linear"
                        dataKey={mark.key}
                        name={mark.label}
                        dot={false}
                        strokeWidth={1.5}
                        stroke={seriesStroke(mark.colorIndex)}
                        strokeDasharray={seriesDash(mark.colorIndex)}
                        connectNulls={false}
                      />
                    ),
                  )}
                </LineChart>
              ) : (
                <BarChart data={chartData} margin={CHART_MARGIN}>
                  <CartesianGrid vertical={false} stroke="var(--chart-grid)" />
                  <XAxis
                    dataKey="bucket"
                    tick={axisTick}
                    tickLine={false}
                    // 契约里横线只有一种：网格线。recharts 默认基线是写死的
                    // #666666，两个主题都不跟令牌，亮色下比网格线重 4 倍。
                    axisLine={false}
                    interval="preserveStartEnd"
                    tickFormatter={formatBucket}
                    label={{
                      value: copy.axisTimezone(timezoneLabel),
                      position: "insideBottom",
                      offset: -4,
                      fill: "var(--chart-axis)",
                      fontSize: 11,
                    }}
                  />
                  <YAxis
                    tick={axisTick}
                    tickLine={false}
                    axisLine={false}
                    width={52}
                    unit={unit ? ` ${unit}` : undefined}
                  />
                  <Tooltip
                    filterNull={false}
                    cursor={{ fill: "var(--chart-cursor-fill)" }}
                    content={
                      <ObserveSeriesTooltip
                        formatBucket={formatBucket}
                        pointIndex={pointIndex}
                        metric={metric}
                      />
                    }
                  />
                  {/* An array, never a fragment: recharts collects its graphical
                      children through react-is, which does not recognise a
                      React 19 element as a fragment, so anything wrapped in one
                      is invisible to the chart and silently never drawn. */}
                  {marks.map((mark) =>
                    hidden.has(mark.key) ? null : (
                      <Bar
                        key={mark.key}
                        dataKey={mark.key}
                        name={mark.label}
                        stackId={showStacked ? "a" : undefined}
                        fill={seriesStroke(mark.colorIndex)}
                      />
                    ),
                  )}
                </BarChart>
              )}
            </ResponsiveContainer>
          </div>
        </>
      )}
      {/* Only where buckets were actually read. A failed read leaves chartData
          empty, and "0 个时间桶" under the error card would state a count the
          window never reported. */}
      {chartData.length > 0 ? (
        <p className="text-xs text-muted-foreground">
          {formatNumber(chartData.length)} {copy.buckets}
        </p>
      ) : null}
    </section>
  );
}

function localizedSeriesLabel(
  groupBy: ObserveGroupBy,
  key: string,
  fallback: string,
  copy: ReturnType<typeof useLocale>["messages"]["requestLogs"],
): string {
  if (groupBy === "attempt_trigger") {
    return (
      {
        initial: copy.attemptTriggerInitial,
        retry_same_target: copy.attemptTriggerRetrySameTarget,
        hedge: copy.attemptTriggerHedge,
        failover: copy.attemptTriggerFailover,
      }[key] ?? copy.attemptTriggerUnavailable
    );
  }
  if (groupBy === "attempt_result") {
    return (
      {
        completed: copy.attemptResultCompleted,
        http_error: copy.attemptResultHttpError,
        stream_error: copy.attemptResultStreamError,
        transport_error: copy.attemptResultTransportError,
        cancelled: copy.attemptResultCancelled,
        client_disconnected: copy.attemptResultClientDisconnected,
      }[key] ?? copy.attemptResultUnknown
    );
  }
  return fallback;
}
