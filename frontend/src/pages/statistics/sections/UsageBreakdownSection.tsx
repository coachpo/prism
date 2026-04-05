import { useMemo } from "react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { Link } from "react-router-dom";
import { EmptyState } from "@/components/EmptyState";
import { TopSpendingCard } from "@/components/statistics/TopSpendingCard";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { useLocale } from "@/i18n/useLocale";
import { formatMoneyMicros } from "@/lib/costing";
import type {
  UsageChartGranularity,
  UsageCostOverview,
  UsageCostOverviewPoint,
  UsageEndpointStatistic,
  UsageModelStatistic,
  UsageSnapshotCurrency,
  UsageStatisticsChartKey,
  UsageTokenTypeBreakdownPoint,
} from "@/lib/types";

interface UsageBreakdownSectionProps {
  chartGranularity: {
    costOverview: UsageChartGranularity;
    tokenTypeBreakdown: UsageChartGranularity;
  };
  costOverview: UsageCostOverview;
  costOverviewSeries: UsageCostOverviewPoint[];
  currency: UsageSnapshotCurrency;
  endpointStatistics: UsageEndpointStatistic[];
  modelStatistics: UsageModelStatistic[];
  onSetChartGranularity: (key: UsageStatisticsChartKey, granularity: UsageChartGranularity) => void;
  tokenTypeBreakdown: UsageTokenTypeBreakdownPoint[];
}

export function UsageBreakdownSection({
  chartGranularity,
  costOverview,
  costOverviewSeries,
  currency,
  endpointStatistics,
  modelStatistics,
  onSetChartGranularity,
  tokenTypeBreakdown,
}: UsageBreakdownSectionProps) {
  const { formatNumber, locale, messages } = useLocale();
  const tokenBreakdownConfig = useMemo<ChartConfig>(
    () => ({
      cached_tokens: { color: "var(--color-chart-4)", label: messages.statistics.cachedPrefix },
      input_tokens: { color: "var(--color-chart-1)", label: messages.statistics.input },
      output_tokens: { color: "var(--color-chart-2)", label: messages.statistics.output },
      reasoning_tokens: { color: "var(--color-chart-3)", label: messages.requestLogs.reasoning },
    }),
    [messages.requestLogs.reasoning, messages.statistics.cachedPrefix, messages.statistics.input, messages.statistics.output],
  );
  const costConfig = useMemo<ChartConfig>(
    () => ({
      total_cost_micros: { color: "var(--color-chart-3)", label: messages.statistics.totalSpend },
    }),
    [messages.statistics.totalSpend],
  );

  const tokenData = tokenTypeBreakdown;
  const topEndpointItems = useMemo(
    () =>
      [...endpointStatistics]
        .sort((left, right) => right.total_cost_micros - left.total_cost_micros)
        .slice(0, 5)
        .map((item) => ({ label: item.endpoint_label, costMicros: item.total_cost_micros })),
    [endpointStatistics],
  );
  const topModelItems = useMemo(
    () =>
      [...modelStatistics]
        .sort((left, right) => right.total_cost_micros - left.total_cost_micros)
        .slice(0, 5)
        .map((item) => ({ label: item.model_label, costMicros: item.total_cost_micros })),
    [modelStatistics],
  );

  const formatBucket = (value: string, granularity: UsageChartGranularity) => {
    const date = new Date(value);
    return new Intl.DateTimeFormat(locale, {
      day: "numeric",
      hour: granularity === "hourly" ? "numeric" : undefined,
      month: "short",
    }).format(date);
  };

  const hasPricingCoverage = costOverview.priced_request_count > 0;
  const hasMissingPricing = !hasPricingCoverage && costOverview.unpriced_request_count > 0;

  return (
    <section className="space-y-4">
      <div className="space-y-1">
        <h2 className="text-lg font-semibold tracking-tight">{messages.statistics.tokenTypeBreakdownTitle}</h2>
        <p className="text-sm text-muted-foreground">{messages.statistics.costOverviewTitle}</p>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <div className="rounded-xl border border-border/70 bg-card/95 p-[var(--density-card-pad-x)] shadow-none">
          <div className="flex items-center justify-between gap-3 border-b border-border/60 pb-4">
            <div className="space-y-1">
              <h3 className="text-base font-semibold tracking-tight">{messages.statistics.tokenTypeBreakdownTitle}</h3>
              <p className="text-sm text-muted-foreground">{messages.statistics.inputOutputSpecial}</p>
            </div>
            <div className="flex items-center gap-2">
              <Button
                onClick={() => onSetChartGranularity("tokenTypeBreakdown", "hourly")}
                size="sm"
                variant={chartGranularity.tokenTypeBreakdown === "hourly" ? "default" : "outline"}
              >
                {messages.statistics.byHour}
              </Button>
              <Button
                onClick={() => onSetChartGranularity("tokenTypeBreakdown", "daily")}
                size="sm"
                variant={chartGranularity.tokenTypeBreakdown === "daily" ? "default" : "outline"}
              >
                {messages.statistics.byDay}
              </Button>
            </div>
          </div>
          {tokenData.length === 0 ? (
            <EmptyState className="py-10" description={messages.statistics.adjustFiltersOrTimeRange} title={messages.statistics.noTokenUsage} />
          ) : (
            <ChartContainer className="mt-6 h-80 w-full" config={tokenBreakdownConfig}>
              <BarChart data={tokenData} margin={{ bottom: 0, left: 0, right: 12, top: 8 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} />
                <XAxis
                  axisLine={false}
                  dataKey="bucket_start"
                  minTickGap={24}
                  tickFormatter={(value) => formatBucket(String(value), chartGranularity.tokenTypeBreakdown)}
                  tickLine={false}
                />
                <YAxis axisLine={false} tickFormatter={(value) => formatNumber(Number(value))} tickLine={false} width={72} />
                <ChartTooltip
                  content={<ChartTooltipContent labelFormatter={(value) => formatBucket(String(value), chartGranularity.tokenTypeBreakdown)} />}
                />
                <Bar dataKey="input_tokens" fill="var(--color-chart-1)" isAnimationActive={false} radius={[4, 4, 0, 0]} stackId="tokens" />
                <Bar dataKey="output_tokens" fill="var(--color-chart-2)" isAnimationActive={false} radius={[4, 4, 0, 0]} stackId="tokens" />
                <Bar dataKey="cached_tokens" fill="var(--color-chart-4)" isAnimationActive={false} radius={[4, 4, 0, 0]} stackId="tokens" />
                <Bar dataKey="reasoning_tokens" fill="var(--color-chart-3)" isAnimationActive={false} radius={[4, 4, 0, 0]} stackId="tokens" />
              </BarChart>
            </ChartContainer>
          )}
        </div>

        <div className="rounded-xl border border-border/70 bg-card/95 p-[var(--density-card-pad-x)] shadow-none">
          <div className="flex items-center justify-between gap-3 border-b border-border/60 pb-4">
            <div className="space-y-1">
              <h3 className="text-base font-semibold tracking-tight">{messages.statistics.costOverviewTitle}</h3>
              <p className="text-sm text-muted-foreground">{messages.statistics.totalSpend}</p>
            </div>
            <div className="flex items-center gap-2">
              <Button
                onClick={() => onSetChartGranularity("costOverview", "hourly")}
                size="sm"
                variant={chartGranularity.costOverview === "hourly" ? "default" : "outline"}
              >
                {messages.statistics.byHour}
              </Button>
              <Button
                onClick={() => onSetChartGranularity("costOverview", "daily")}
                size="sm"
                variant={chartGranularity.costOverview === "daily" ? "default" : "outline"}
              >
                {messages.statistics.byDay}
              </Button>
            </div>
          </div>

          {hasMissingPricing ? (
            <EmptyState
              action={
                <Button asChild size="sm">
                  <Link to="/pricing-templates">{messages.statistics.openPricingTemplates}</Link>
                </Button>
              }
              className="py-10"
              description={messages.statistics.pricingDataMissingDescription}
              title={messages.statistics.pricingDataMissingTitle}
            />
          ) : !hasPricingCoverage || costOverviewSeries.length === 0 ? (
            <EmptyState className="py-10" description={messages.statistics.adjustFiltersOrTimeRange} title={messages.statistics.noCostRecordsFound} />
          ) : (
            <div className="space-y-6 pt-6">
              <div className="flex flex-wrap items-end justify-between gap-3">
                <div>
                  <p className="text-xs uppercase tracking-[0.2em] text-muted-foreground">{messages.statistics.totalSpend}</p>
                  <p className="mt-2 text-3xl font-semibold tracking-tight">
                    {formatMoneyMicros(costOverview.total_cost_micros, currency.symbol, currency.code, 2, 6, locale)}
                  </p>
                </div>
                <div className="text-right text-sm text-muted-foreground">
                  <p>{messages.statistics.totalRequests(String(costOverview.priced_request_count))}</p>
                  {costOverview.unpriced_request_count > 0 ? (
                    <p>{messages.statistics.unpriced(String(costOverview.unpriced_request_count))}</p>
                  ) : null}
                </div>
              </div>

              <ChartContainer className="h-56 w-full" config={costConfig}>
                <AreaChart data={costOverviewSeries} margin={{ bottom: 0, left: 0, right: 12, top: 8 }}>
                  <defs>
                    <linearGradient id="usage-cost-fill" x1="0" x2="0" y1="0" y2="1">
                      <stop offset="0%" stopColor="var(--color-chart-3)" stopOpacity={0.35} />
                      <stop offset="100%" stopColor="var(--color-chart-3)" stopOpacity={0.04} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} />
                  <XAxis
                    axisLine={false}
                    dataKey="bucket_start"
                    minTickGap={24}
                    tickFormatter={(value) => formatBucket(String(value), chartGranularity.costOverview)}
                    tickLine={false}
                  />
                  <YAxis
                    axisLine={false}
                    tickFormatter={(value) =>
                      formatMoneyMicros(Number(value), currency.symbol, currency.code, 0, 3, locale)
                    }
                    tickLine={false}
                    width={90}
                  />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        formatter={(value) => (
                          <span className="font-medium text-foreground">
                            {formatMoneyMicros(Number(value), currency.symbol, currency.code, 2, 6, locale)}
                          </span>
                        )}
                        labelFormatter={(value) => formatBucket(String(value), chartGranularity.costOverview)}
                      />
                    }
                  />
                  <Area
                    dataKey="total_cost_micros"
                    fill="url(#usage-cost-fill)"
                    isAnimationActive={false}
                    stroke="var(--color-chart-3)"
                    strokeWidth={2}
                    type="monotone"
                  />
                </AreaChart>
              </ChartContainer>

              <div className="grid gap-4 lg:grid-cols-2">
                <TopSpendingCard
                  currencyCode={currency.code}
                  currencySymbol={currency.symbol}
                  items={topEndpointItems}
                  title={messages.statistics.topEndpointsByCost}
                  totalCostMicros={costOverview.total_cost_micros}
                />
                <TopSpendingCard
                  currencyCode={currency.code}
                  currencySymbol={currency.symbol}
                  items={topModelItems}
                  title={messages.statistics.topModelsByCost}
                  totalCostMicros={costOverview.total_cost_micros}
                />
              </div>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
