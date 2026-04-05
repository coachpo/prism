import { Activity, Coins, DollarSign, Gauge, TrendingUp } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { formatMoneyMicros } from "@/lib/costing";
import type {
  UsageRequestTrendSeries,
  UsageSnapshotCurrency,
  UsageSnapshotOverview,
  UsageTokenTrendSeries,
} from "@/lib/types";
import { UsageKpiCard } from "../charts/UsageKpiCard";

interface UsageOverviewSectionProps {
  currency: UsageSnapshotCurrency;
  overview: UsageSnapshotOverview;
  requestTrendSeries: UsageRequestTrendSeries[];
  tokenUsageTrendSeries: UsageTokenTrendSeries[];
}

function resolveTrendSeriesLabel<T extends { key: string }>(series: T[]): T | null {
  return series.find((item) => item.key === "all") ?? series[0] ?? null;
}

export function UsageOverviewSection({
  currency,
  overview,
  requestTrendSeries,
  tokenUsageTrendSeries,
}: UsageOverviewSectionProps) {
  const { formatNumber, locale, messages } = useLocale();
  const formattedSuccessRate = formatNumber(overview.success_rate, {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  });
  const rollingWindowLabel = overview.rolling_window_minutes
    ? `${formatNumber(overview.rolling_window_minutes)}m`
    : null;

  const requestSparkline = resolveTrendSeriesLabel(requestTrendSeries)?.points.map((point) => ({
    label: point.bucket_start,
    value: point.request_count,
  }));

  const tokenSparkline = resolveTrendSeriesLabel(tokenUsageTrendSeries)?.points.map((point) => ({
    label: point.bucket_start,
    value: point.total_tokens,
  }));

  const formattedAverageRpm = formatNumber(overview.average_rpm, { maximumFractionDigits: 2 });
  const formattedAverageTpm = formatNumber(overview.average_tpm, { maximumFractionDigits: 2 });
  const formattedRollingRpm = overview.rolling_rpm == null
    ? null
    : formatNumber(overview.rolling_rpm, { maximumFractionDigits: 2 });
  const formattedRollingTpm = overview.rolling_tpm == null
    ? null
    : formatNumber(overview.rolling_tpm, { maximumFractionDigits: 2 });

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold tracking-tight">{messages.statistics.overviewTitle}</h2>
        <p className="text-sm text-muted-foreground">{messages.statistics.statisticsDescription}</p>
      </div>

      <div className="grid gap-4 xl:grid-cols-12" data-testid="usage-kpi-grid">
        <div className="rounded-[calc(var(--radius-xl)+2px)] bg-linear-to-br from-primary/12 via-primary/4 to-transparent p-px md:col-span-2 xl:col-span-6" data-testid="usage-kpi-dominant-card">
          <div data-testid="usage-kpi-card">
            <UsageKpiCard
              accentClassName="bg-primary/10 text-primary"
              detail={`${messages.statistics.successRate} ${formattedSuccessRate}% · ${messages.statistics.totalRequests(formatNumber(overview.total_requests))}`}
              icon={<Activity className="h-4 w-4" />}
              label={messages.statistics.requests}
              sparkline={requestSparkline}
              sparklineColor="var(--color-chart-1)"
              value={formatNumber(overview.total_requests)}
            />
          </div>
        </div>

        <div className="rounded-[calc(var(--radius-xl)+2px)] bg-linear-to-br from-success/14 via-success/5 to-transparent p-px md:col-span-2 xl:col-span-6" data-testid="usage-kpi-dominant-card">
          <div data-testid="usage-kpi-card">
            <UsageKpiCard
              accentClassName="bg-success/10 text-success"
              detail={`${messages.requestLogs.input} ${formatNumber(overview.input_tokens)} · ${messages.requestLogs.output} ${formatNumber(overview.output_tokens)} · ${messages.requestLogs.cacheRead} ${formatNumber(overview.cached_tokens)}`}
              icon={<Coins className="h-4 w-4" />}
              label={messages.statistics.totalTokens}
              sparkline={tokenSparkline}
              sparklineColor="var(--color-chart-2)"
              value={formatNumber(overview.total_tokens)}
            />
          </div>
        </div>

        <div className="md:col-span-1 xl:col-span-4" data-testid="usage-kpi-supporting-card">
          <div data-testid="usage-kpi-card">
            <UsageKpiCard
              accentClassName="bg-info/10 text-info"
              detail={
                formattedRollingRpm && rollingWindowLabel
                  ? `${messages.statistics.currentRpm}: ${formattedRollingRpm} · ${rollingWindowLabel}`
                  : messages.statistics.averageRpm
              }
              icon={<Gauge className="h-4 w-4" />}
              label="RPM"
              sparkline={requestSparkline}
              sparklineColor="var(--color-info)"
              value={formattedAverageRpm}
            />
          </div>
        </div>

        <div className="md:col-span-1 xl:col-span-4" data-testid="usage-kpi-supporting-card">
          <div data-testid="usage-kpi-card">
            <UsageKpiCard
              accentClassName="bg-warning/15 text-warning-foreground dark:text-warning"
              detail={
                formattedRollingTpm && rollingWindowLabel
                  ? `${messages.statistics.tokenThroughput}: ${formattedRollingTpm} · ${rollingWindowLabel}`
                  : messages.statistics.tokenThroughput
              }
              icon={<TrendingUp className="h-4 w-4" />}
              label="TPM"
              sparkline={tokenSparkline}
              sparklineColor="var(--color-warning)"
              value={formattedAverageTpm}
            />
          </div>
        </div>

        <div className="md:col-span-2 xl:col-span-4" data-testid="usage-kpi-supporting-card">
          <div data-testid="usage-kpi-card">
            <UsageKpiCard
              accentClassName="bg-chart-3/10 text-foreground"
              detail={messages.statistics.successful(formatNumber(overview.success_requests))}
              icon={<DollarSign className="h-4 w-4" />}
              label={messages.statistics.totalSpend}
              sparkline={tokenSparkline}
              sparklineColor="var(--color-chart-3)"
              value={formatMoneyMicros(
                overview.total_cost_micros,
                currency.symbol,
                currency.code,
                2,
                6,
                locale,
              )}
            />
          </div>
        </div>
      </div>
    </section>
  );
}
