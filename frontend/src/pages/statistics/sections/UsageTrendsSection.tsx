import { useLocale } from "@/i18n/useLocale";
import type {
  UsageChartGranularity,
  UsageRequestTrendSeries,
  UsageStatisticsChartKey,
  UsageTokenTrendSeries,
} from "@/lib/types";
import { UsageTrendChart } from "../charts/UsageTrendChart";

interface UsageTrendsSectionProps {
  chartGranularity: {
    requestTrends: UsageChartGranularity;
    tokenUsageTrends: UsageChartGranularity;
  };
  onSetChartGranularity: (key: UsageStatisticsChartKey, granularity: UsageChartGranularity) => void;
  requestTrendSeries: UsageRequestTrendSeries[];
  tokenUsageTrendSeries: UsageTokenTrendSeries[];
}

export function UsageTrendsSection({
  chartGranularity,
  onSetChartGranularity,
  requestTrendSeries,
  tokenUsageTrendSeries,
}: UsageTrendsSectionProps) {
  const { formatNumber, messages } = useLocale();

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold tracking-tight">{messages.statistics.requestTrendsTitle}</h2>
        <p className="text-sm text-muted-foreground">{messages.statistics.requestsPerMinuteOverTime}</p>
      </div>

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
