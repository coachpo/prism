import { useLocale } from "@/i18n/useLocale";
import type {
  UsageChartGranularity,
  UsageLatencyTrendSeries,
  UsageRequestTrendSeries,
  UsageStatisticsChartKey,
  UsageTokenTrendSeries,
} from "@/lib/types";
import { UsageTrendChart } from "../charts/UsageTrendChart";

interface UsageTrendsSectionProps {
  chartGranularity: {
    latencyTrends: UsageChartGranularity;
    requestTrends: UsageChartGranularity;
    tokenUsageTrends: UsageChartGranularity;
  };
  latencyTrendSeries: UsageLatencyTrendSeries[];
  onSetChartGranularity: (key: UsageStatisticsChartKey, granularity: UsageChartGranularity) => void;
  requestTrendSeries: UsageRequestTrendSeries[];
  tokenUsageTrendSeries: UsageTokenTrendSeries[];
}

export function UsageTrendsSection({
  chartGranularity,
  latencyTrendSeries,
  onSetChartGranularity,
  requestTrendSeries,
  tokenUsageTrendSeries,
}: UsageTrendsSectionProps) {
  const { formatCompactNumber, formatNumber, messages } = useLocale();
  const latencySeries = latencyTrendSeries.find((series) => series.key === "all") ?? latencyTrendSeries[0];
  const formatLatency = (value: number) => `${formatNumber(value, { maximumFractionDigits: 0 })}ms`;

  return (
    <section className="flex flex-col gap-4">
      <div className="grid gap-4 xl:grid-cols-2" data-testid="usage-trends-grid">
        <UsageTrendChart
          description={messages.statistics.requestsPerMinuteOverTime}
          emptyDescription={messages.statistics.adjustFiltersOrTimeRange}
          emptyTitle={messages.statistics.noDataAvailable}
          formatValue={(value) => formatNumber(value, { maximumFractionDigits: 2 })}
          granularity={chartGranularity.requestTrends}
          onGranularityChange={(granularity) => onSetChartGranularity("requestTrends", granularity)}
          series={requestTrendSeries.map((series) => ({
            key: series.key,
            label: series.label,
            points: series.points.map((point) => ({
              bucket_start: point.bucket_start,
              value: point.request_count,
            })),
          }))}
          title={messages.statistics.requestTrendsTitle}
        />

        <UsageTrendChart
          axisFormatValue={formatLatency}
          description={messages.statistics.latencyOverTime}
          emptyDescription={messages.statistics.adjustFiltersOrTimeRange}
          emptyTitle={messages.statistics.noLatencyData}
          formatValue={formatLatency}
          granularity={chartGranularity.latencyTrends}
          onGranularityChange={(granularity) => onSetChartGranularity("latencyTrends", granularity)}
          series={
            latencySeries
              ? [
                  {
                    key: "p50",
                    label: messages.statistics.p50Label,
                    points: latencySeries.points
                      .filter((point) => point.p50_ms !== null)
                      .map((point) => ({
                        bucket_start: point.bucket_start,
                        value: point.p50_ms ?? 0,
                      })),
                  },
                  {
                    key: "p95",
                    label: messages.statistics.p95Label,
                    points: latencySeries.points
                      .filter((point) => point.p95_ms !== null)
                      .map((point) => ({
                        bucket_start: point.bucket_start,
                        value: point.p95_ms ?? 0,
                      })),
                  },
                ].filter((series) => series.points.length > 0)
              : []
          }
          title={messages.statistics.latencyTrendsTitle}
        />

        <UsageTrendChart
          axisFormatValue={(value) => formatCompactNumber(value)}
          description={messages.statistics.tokenThroughput}
          emptyDescription={messages.statistics.adjustFiltersOrTimeRange}
          emptyTitle={messages.statistics.noTokenUsage}
          formatValue={(value) => formatNumber(value)}
          granularity={chartGranularity.tokenUsageTrends}
          onGranularityChange={(granularity) => onSetChartGranularity("tokenUsageTrends", granularity)}
          series={tokenUsageTrendSeries.map((series) => ({
            key: series.key,
            label: series.label,
            points: series.points.map((point) => ({
              bucket_start: point.bucket_start,
              value: point.total_tokens,
            })),
          }))}
          title={messages.statistics.tokenUsageTrendsTitle}
        />
      </div>
    </section>
  );
}
