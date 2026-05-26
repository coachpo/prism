import { useMemo } from "react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { Link } from "react-router-dom";
import { EmptyState } from "@/components/EmptyState";
import { SpendTrustNote } from "@/components/SpendTrustIndicator";
import { TopSpendingCard } from "@/components/statistics/TopSpendingCard";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { useLocale } from "@/i18n/useLocale";
import { formatMoneyMicros, resolveSpendTrustState } from "@/lib/costing";
import type {
  UsageChartGranularity,
  UsageCostOverviewPoint,
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
  costSummary: {
    priced_request_count: number | null;
    total_cost_micros: number;
    unpriced_request_count: number | null;
  };
  costOverviewSeries: UsageCostOverviewPoint[];
  currency: UsageSnapshotCurrency;
  modelStatistics: UsageModelStatistic[];
  onSetChartGranularity: (key: UsageStatisticsChartKey, granularity: UsageChartGranularity) => void;
  topEndpointSpendStatistics: Array<{
    endpoint_label: string;
    total_cost_micros: number;
  }>;
  tokenTypeBreakdown: UsageTokenTypeBreakdownPoint[];
}

interface ChartGranularityToggleProps {
  activeGranularity: UsageChartGranularity;
  dayLabel: string;
  hourLabel: string;
  onChange: (granularity: UsageChartGranularity) => void;
}

function ChartGranularityToggle({
  activeGranularity,
  dayLabel,
  hourLabel,
  onChange,
}: ChartGranularityToggleProps) {
  return (
    <div className="inline-flex items-center gap-1 rounded-lg border border-border/60 bg-muted/40 p-1">
      <Button
        aria-pressed={activeGranularity === "hourly"}
        className="shadow-none"
        onClick={() => onChange("hourly")}
        size="sm"
        type="button"
        variant={activeGranularity === "hourly" ? "secondary" : "ghost"}
      >
        {hourLabel}
      </Button>
      <Button
        aria-pressed={activeGranularity === "daily"}
        className="shadow-none"
        onClick={() => onChange("daily")}
        size="sm"
        type="button"
        variant={activeGranularity === "daily" ? "secondary" : "ghost"}
      >
        {dayLabel}
      </Button>
    </div>
  );
}

export function UsageBreakdownSection({
  chartGranularity,
  costSummary,
  costOverviewSeries,
  currency,
  modelStatistics,
  onSetChartGranularity,
  topEndpointSpendStatistics,
  tokenTypeBreakdown,
}: UsageBreakdownSectionProps) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatNumber, locale, messages } = useLocale();
  const spendTrust = resolveSpendTrustState(
    {
      costMicros: costSummary.total_cost_micros,
      pricedRequestCount: costSummary.priced_request_count,
      unpricedRequestCount: costSummary.unpriced_request_count,
    },
    currencyState,
  );
  const tokenBreakdownConfig = useMemo<ChartConfig>(
    () => ({
      cached_tokens: { color: "var(--color-chart-4)", label: messages.statistics.cachedPrefix },
      input_tokens: { color: "var(--color-chart-1)", label: messages.statistics.input },
      output_tokens: { color: "var(--color-chart-2)", label: messages.statistics.output },
      reasoning_tokens: { color: "var(--color-chart-3)", label: messages.requestLogs.reasoning },
    }),
    [
      messages.requestLogs.reasoning,
      messages.statistics.cachedPrefix,
      messages.statistics.input,
      messages.statistics.output,
    ],
  );
  const costConfig = useMemo<ChartConfig>(
    () => ({
      total_cost_micros: { color: "var(--color-chart-3)", label: messages.statistics.totalSpend },
    }),
    [messages.statistics.totalSpend],
  );

  const tokenBreakdownDescription = `${messages.statistics.input} + ${messages.statistics.output} + ${messages.statistics.cachedPrefix} + ${messages.requestLogs.reasoning}`;
  const tokenData = tokenTypeBreakdown;
  const topEndpointItems = useMemo(
    () =>
      [...topEndpointSpendStatistics]
        .sort((left, right) => right.total_cost_micros - left.total_cost_micros)
        .slice(0, 5)
        .map((item) => ({ label: item.endpoint_label, costMicros: item.total_cost_micros })),
    [topEndpointSpendStatistics],
  );
  const topEndpointTotalCostMicros = useMemo(
    () => topEndpointSpendStatistics.reduce((sum, item) => sum + item.total_cost_micros, 0),
    [topEndpointSpendStatistics],
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

  const hasPricingCoverage =
    costSummary.total_cost_micros > 0 || (costSummary.priced_request_count ?? 0) > 0;
  const hasMissingPricing = !hasPricingCoverage && (costSummary.unpriced_request_count ?? 0) > 0;

  return (
    <section className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold tracking-tight">{messages.statistics.tokenTypeBreakdownTitle}</h2>
        <p className="text-sm text-muted-foreground">{messages.statistics.costOverviewTitle}</p>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="border-border/70 bg-card/95 shadow-none">
          <CardHeader className="gap-3 border-b">
            <div className="grid flex-1 gap-1">
              <CardTitle className="text-base">
                <h3>{messages.statistics.tokenTypeBreakdownTitle}</h3>
              </CardTitle>
              <CardDescription>{tokenBreakdownDescription}</CardDescription>
            </div>
            <CardAction className="flex items-center">
              <ChartGranularityToggle
                activeGranularity={chartGranularity.tokenTypeBreakdown}
                dayLabel={messages.statistics.byDay}
                hourLabel={messages.statistics.byHour}
                onChange={(granularity) => onSetChartGranularity("tokenTypeBreakdown", granularity)}
              />
            </CardAction>
          </CardHeader>

          {tokenData.length === 0 ? (
            <CardContent className="pt-2">
              <EmptyState
                className="py-10"
                description={messages.statistics.adjustFiltersOrTimeRange}
                title={messages.statistics.noTokenUsage}
              />
            </CardContent>
          ) : (
            <CardContent className="pt-4 sm:pt-6">
              <ChartContainer className="aspect-auto h-80 w-full" config={tokenBreakdownConfig}>
                <BarChart
                  accessibilityLayer
                  barCategoryGap="28%"
                  data={tokenData}
                  margin={{ bottom: 0, left: 12, right: 12, top: 8 }}
                >
                  <CartesianGrid vertical={false} />
                  <XAxis
                    axisLine={false}
                    dataKey="bucket_start"
                    minTickGap={32}
                    tickFormatter={(value) => formatBucket(String(value), chartGranularity.tokenTypeBreakdown)}
                    tickLine={false}
                    tickMargin={8}
                  />
                  <YAxis
                    axisLine={false}
                    tickCount={4}
                    tickFormatter={(value) => formatNumber(Number(value))}
                    tickLine={false}
                    tickMargin={10}
                    width={72}
                  />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        labelFormatter={(value) => formatBucket(String(value), chartGranularity.tokenTypeBreakdown)}
                      />
                    }
                    cursor={false}
                  />
                  <Bar
                    dataKey="input_tokens"
                    fill="var(--color-chart-1)"
                    isAnimationActive={false}
                    radius={[4, 4, 0, 0]}
                    stackId="tokens"
                    stroke="var(--background)"
                    strokeWidth={1}
                  />
                  <Bar
                    dataKey="output_tokens"
                    fill="var(--color-chart-2)"
                    isAnimationActive={false}
                    radius={[4, 4, 0, 0]}
                    stackId="tokens"
                    stroke="var(--background)"
                    strokeWidth={1}
                  />
                  <Bar
                    dataKey="cached_tokens"
                    fill="var(--color-chart-4)"
                    isAnimationActive={false}
                    radius={[4, 4, 0, 0]}
                    stackId="tokens"
                    stroke="var(--background)"
                    strokeWidth={1}
                  />
                  <Bar
                    dataKey="reasoning_tokens"
                    fill="var(--color-chart-3)"
                    isAnimationActive={false}
                    radius={[4, 4, 0, 0]}
                    stackId="tokens"
                    stroke="var(--background)"
                    strokeWidth={1}
                  />
                </BarChart>
              </ChartContainer>
            </CardContent>
          )}
        </Card>

        <Card className="border-border/70 bg-card/95 shadow-none">
          <CardHeader className="gap-3 border-b">
            <div className="grid flex-1 gap-1">
              <CardTitle className="text-base">{messages.statistics.costOverviewTitle}</CardTitle>
              <CardDescription>{messages.statistics.requestBasedSpend}</CardDescription>
            </div>
            <CardAction className="flex items-center">
              <ChartGranularityToggle
                activeGranularity={chartGranularity.costOverview}
                dayLabel={messages.statistics.byDay}
                hourLabel={messages.statistics.byHour}
                onChange={(granularity) => onSetChartGranularity("costOverview", granularity)}
              />
            </CardAction>
          </CardHeader>

          {hasMissingPricing ? (
            <CardContent className="pt-2">
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
            </CardContent>
          ) : !hasPricingCoverage || costOverviewSeries.length === 0 ? (
            <CardContent className="pt-2">
              <EmptyState
                className="py-10"
                description={messages.statistics.adjustFiltersOrTimeRange}
                title={messages.statistics.noCostRecordsFound}
              />
            </CardContent>
          ) : (
            <CardContent className="flex flex-col gap-6 pt-4 sm:pt-6">
              <div
                className="rounded-xl border border-border/60 bg-muted/20 p-4"
                data-testid="usage-cost-summary-card"
              >
                <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
                  <div className="flex min-w-0 flex-col gap-2">
                    <p className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground">
                      {messages.statistics.totalSpend}
                    </p>
                    <p
                      className="text-3xl font-semibold tracking-tight sm:text-[2rem]"
                      data-testid="usage-cost-summary-total"
                    >
                      {spendTrust === "unpriced"
                        ? messages.spendTrust.unpriced
                        : formatMoneyMicros(costSummary.total_cost_micros, currency.symbol, currency.code, 2, 6, locale)}
                    </p>
                  </div>
                  <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                    {costSummary.priced_request_count !== null ? (
                      <Badge className="font-medium tabular-nums" variant="secondary">
                        {messages.statistics.pricedRequests(String(costSummary.priced_request_count))}
                      </Badge>
                    ) : null}
                    {(costSummary.unpriced_request_count ?? 0) > 0 ? (
                      <Badge className="font-medium tabular-nums" variant="outline">
                        {messages.statistics.unpriced(String(costSummary.unpriced_request_count))}
                      </Badge>
                    ) : null}
                  </div>
                </div>
                {spendTrust !== "verified" ? (
                  <SpendTrustNote
                    className="mt-3"
                    spendTrust={spendTrust}
                    showPricingTemplatesLink={spendTrust === "unpriced"}
                  />
                ) : null}
              </div>

              <ChartContainer className="aspect-auto h-56 w-full" config={costConfig}>
                <AreaChart
                  accessibilityLayer
                  data={costOverviewSeries}
                  margin={{ bottom: 0, left: 12, right: 12, top: 8 }}
                >
                  <defs>
                    <linearGradient id="usage-cost-fill" x1="0" x2="0" y1="0" y2="1">
                      <stop offset="0%" stopColor="var(--color-chart-3)" stopOpacity={0.35} />
                      <stop offset="100%" stopColor="var(--color-chart-3)" stopOpacity={0.04} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid vertical={false} />
                  <XAxis
                    allowDataOverflow
                    axisLine={false}
                    dataKey="bucket_start"
                    minTickGap={32}
                    padding={{ left: 8, right: 8 }}
                    tickFormatter={(value) => formatBucket(String(value), chartGranularity.costOverview)}
                    tickLine={false}
                    tickMargin={8}
                  />
                  <YAxis
                    allowDataOverflow
                    axisLine={false}
                    domain={[0, "dataMax"]}
                    padding={{ top: 12 }}
                    tickCount={4}
                    tickFormatter={(value) =>
                      formatMoneyMicros(Number(value), currency.symbol, currency.code, 0, 3, locale)
                    }
                    tickLine={false}
                    tickMargin={10}
                    width={92}
                  />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        formatter={(value) => (
                          <div className="flex w-full items-center justify-between gap-3">
                            <span className="text-muted-foreground">{messages.statistics.totalSpend}</span>
                            <span className="font-medium text-foreground">
                              {formatMoneyMicros(Number(value), currency.symbol, currency.code, 2, 6, locale)}
                            </span>
                          </div>
                        )}
                        labelFormatter={(value) => formatBucket(String(value), chartGranularity.costOverview)}
                      />
                    }
                    cursor={false}
                  />
                  <Area
                    activeDot={{ r: 4 }}
                    dataKey="total_cost_micros"
                    fill="url(#usage-cost-fill)"
                    isAnimationActive={false}
                    stroke="var(--color-chart-3)"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={2.5}
                    type="monotone"
                  />
                </AreaChart>
              </ChartContainer>

              <div className="grid gap-4 lg:grid-cols-2">
                <div data-testid="usage-top-endpoints-card">
                  <TopSpendingCard
                    currencyCode={currency.code}
                    currencySymbol={currency.symbol}
                    items={topEndpointItems}
                    title={messages.statistics.topEndpointsByCost}
                    totalCostMicros={topEndpointTotalCostMicros}
                  />
                </div>
                <div data-testid="usage-top-models-card">
                  <TopSpendingCard
                    currencyCode={currency.code}
                    currencySymbol={currency.symbol}
                    items={topModelItems}
                    title={messages.statistics.topModelsByCost}
                    totalCostMicros={costSummary.total_cost_micros}
                  />
                </div>
              </div>
            </CardContent>
          )}
        </Card>
      </div>
    </section>
  );
}
