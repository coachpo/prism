import { useId, useMemo } from "react";
import { Area, AreaChart, CartesianGrid, Cell, Pie, PieChart, XAxis, YAxis } from "recharts";
import { Link } from "react-router-dom";
import { SpendTrustNote } from "@/components/SpendTrustIndicator";
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
import { OperatorEmptyState } from "@/shared/design-system";
import type {
  UsageChartGranularity,
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
  costSummary: {
    priced_request_count: number | null;
    total_cost_micros: number;
    unpriced_request_count: number | null;
  };
  costOverviewSeries: UsageCostOverviewPoint[];
  currency: UsageSnapshotCurrency;
  endpointStatistics: UsageEndpointStatistic[];
  modelStatistics: UsageModelStatistic[];
  onSetChartGranularity: (key: UsageStatisticsChartKey, granularity: UsageChartGranularity) => void;
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
    <div className="inline-flex items-center gap-1 rounded-lg border border-outline-variant bg-surface-container p-1">
      <Button
        aria-pressed={activeGranularity === "hourly"}
        onClick={() => onChange("hourly")}
        size="sm"
        type="button"
        variant={activeGranularity === "hourly" ? "secondary" : "ghost"}
      >
        {hourLabel}
      </Button>
      <Button
        aria-pressed={activeGranularity === "daily"}
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

interface RequestBreakdownItem {
  color: string;
  id: string;
  label: string;
  requestCount: number;
}

const REQUEST_BREAKDOWN_COLORS = [
  "var(--color-chart-1)",
  "var(--color-chart-2)",
  "var(--color-chart-3)",
  "var(--color-chart-4)",
  "var(--color-chart-5)",
];

function buildModelRequestItems(modelStatistics: UsageModelStatistic[]): RequestBreakdownItem[] {
  return [...modelStatistics]
    .filter((item) => item.request_count > 0)
    .sort((left, right) => right.request_count - left.request_count || left.model_label.localeCompare(right.model_label))
    .slice(0, 5)
    .map((item, index) => ({
      color: REQUEST_BREAKDOWN_COLORS[index % REQUEST_BREAKDOWN_COLORS.length],
      id: item.model_id,
      label: item.model_label,
      requestCount: item.request_count,
    }));
}

function buildEndpointRequestItems(endpointStatistics: UsageEndpointStatistic[]): RequestBreakdownItem[] {
  return [...endpointStatistics]
    .filter((item) => item.request_count > 0)
    .sort((left, right) => right.request_count - left.request_count || left.endpoint_label.localeCompare(right.endpoint_label))
    .slice(0, 5)
    .map((item, index) => ({
      color: REQUEST_BREAKDOWN_COLORS[index % REQUEST_BREAKDOWN_COLORS.length],
      id: `${item.endpoint_id ?? item.endpoint_label}`,
      label: item.endpoint_label,
      requestCount: item.request_count,
    }));
}

interface RequestBreakdownPieCardProps {
  config: ChartConfig;
  emptyTitle: string;
  formatNumber: ReturnType<typeof useLocale>["formatNumber"];
  items: RequestBreakdownItem[];
  requestLabel: string;
  title: string;
}

function RequestBreakdownPieCard({
  config,
  emptyTitle,
  formatNumber,
  items,
  requestLabel,
  title,
}: RequestBreakdownPieCardProps) {
  const totalRequests = items.reduce((sum, item) => sum + item.requestCount, 0);

  return (
    <Card className="operator-section-surface">
      <CardHeader className="gap-1 border-b">
        <CardTitle className="text-base">
          <h3>{title}</h3>
        </CardTitle>
        <CardDescription>{requestLabel}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 pt-4 sm:pt-6">
        {items.length === 0 ? (
          <OperatorEmptyState className="py-10" description={emptyTitle} title={emptyTitle} />
        ) : (
          <>
            <ChartContainer
              className="aspect-auto h-56 w-full [&_.recharts-pie-sector]:[pointer-events:all]"
              config={config}
            >
              <PieChart accessibilityLayer>
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      formatter={(value, name) => (
                        <div className="flex w-full items-center justify-between gap-3">
                          <span className="max-w-[12rem] truncate text-muted-foreground">{String(name)}</span>
                          <span className="font-medium text-foreground">
                            {formatNumber(Number(value))}
                          </span>
                        </div>
                      )}
                    />
                  }
                  cursor={false}
                />
                <Pie
                  data={items}
                  dataKey="requestCount"
                  isAnimationActive={false}
                  nameKey="label"
                  outerRadius={86}
                  paddingAngle={2}
                  strokeWidth={2}
                >
                  {items.map((item) => (
                    <Cell fill={item.color} key={item.id} />
                  ))}
                </Pie>
              </PieChart>
            </ChartContainer>

            <div className="flex flex-col gap-2">
              {items.map((item) => {
                const percentage = totalRequests > 0 ? (item.requestCount / totalRequests) * 100 : 0;
                return (
                  <div className="flex items-center justify-between gap-3 text-sm" key={item.id}>
                    <div className="flex min-w-0 items-center gap-2">
                      <span
                        aria-hidden="true"
                        className="size-2.5 shrink-0 rounded-full"
                        style={{ backgroundColor: item.color }}
                      />
                      <span className="truncate font-medium text-foreground">{item.label}</span>
                    </div>
                    <span className="shrink-0 text-muted-foreground tabular-nums">
                      {formatNumber(item.requestCount)} · {formatNumber(percentage, { maximumFractionDigits: 1 })}%
                    </span>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

export function UsageBreakdownSection({
  chartGranularity,
  costSummary,
  costOverviewSeries,
  currency,
  endpointStatistics,
  modelStatistics,
  onSetChartGranularity,
  tokenTypeBreakdown,
}: UsageBreakdownSectionProps) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatCompactNumber, formatNumber, locale, messages } = useLocale();
  const chartId = useId().replace(/:/g, "");
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
  const requestBreakdownConfig = useMemo<ChartConfig>(
    () => ({
      requestCount: { label: messages.statistics.requests },
    }),
    [messages.statistics.requests],
  );
  const tokenBreakdownSeries = useMemo(
    () => [
      {
        color: "var(--color-chart-1)",
        dataKey: "input_tokens",
        gradientId: `${chartId}-input-tokens`,
        label: messages.statistics.input,
      },
      {
        color: "var(--color-chart-2)",
        dataKey: "output_tokens",
        gradientId: `${chartId}-output-tokens`,
        label: messages.statistics.output,
      },
      {
        color: "var(--color-chart-4)",
        dataKey: "cached_tokens",
        gradientId: `${chartId}-cached-tokens`,
        label: messages.statistics.cachedPrefix,
      },
      {
        color: "var(--color-chart-3)",
        dataKey: "reasoning_tokens",
        gradientId: `${chartId}-reasoning-tokens`,
        label: messages.requestLogs.reasoning,
      },
    ] as const,
    [
      chartId,
      messages.requestLogs.reasoning,
      messages.statistics.cachedPrefix,
      messages.statistics.input,
      messages.statistics.output,
    ],
  );
  const modelRequestItems = useMemo(() => buildModelRequestItems(modelStatistics), [modelStatistics]);
  const endpointRequestItems = useMemo(() => buildEndpointRequestItems(endpointStatistics), [endpointStatistics]);

  const tokenBreakdownDescription = `${messages.statistics.input} + ${messages.statistics.output} + ${messages.statistics.cachedPrefix} + ${messages.requestLogs.reasoning}`;
  const tokenData = tokenTypeBreakdown;

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
      <div className="grid gap-4 xl:grid-cols-2" data-testid="usage-request-breakdown-grid">
        <RequestBreakdownPieCard
          config={requestBreakdownConfig}
          emptyTitle={messages.statistics.noModelStatisticsTitle}
          formatNumber={formatNumber}
          items={modelRequestItems}
          requestLabel={messages.statistics.requests}
          title={messages.statistics.topModelsByCost}
        />
        <RequestBreakdownPieCard
          config={requestBreakdownConfig}
          emptyTitle={messages.statistics.noEndpointStatisticsTitle}
          formatNumber={formatNumber}
          items={endpointRequestItems}
          requestLabel={messages.statistics.requests}
          title={messages.statistics.topEndpointsByCost}
        />
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card className="operator-section-surface">
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
              <OperatorEmptyState
                className="py-10"
                description={messages.statistics.adjustFiltersOrTimeRange}
                title={messages.statistics.noTokenUsage}
              />
            </CardContent>
          ) : (
            <CardContent className="pt-4 sm:pt-6">
              <ChartContainer className="aspect-auto h-80 w-full" config={tokenBreakdownConfig}>
                <AreaChart
                  accessibilityLayer
                  data={tokenData}
                  margin={{ bottom: 0, left: 12, right: 12, top: 8 }}
                >
                  <defs>
                    {tokenBreakdownSeries.map((series) => (
                      <linearGradient id={series.gradientId} key={series.gradientId} x1="0" x2="0" y1="0" y2="1">
                        <stop offset="5%" stopColor={series.color} stopOpacity={0.28} />
                        <stop offset="95%" stopColor={series.color} stopOpacity={0.03} />
                      </linearGradient>
                    ))}
                  </defs>
                  <CartesianGrid vertical={false} />
                  <XAxis
                    axisLine={false}
                    dataKey="bucket_start"
                    minTickGap={32}
                    padding={{ left: 8, right: 8 }}
                    tickFormatter={(value) => formatBucket(String(value), chartGranularity.tokenTypeBreakdown)}
                    tickLine={false}
                    tickMargin={8}
                  />
                  <YAxis
                    allowDataOverflow
                    axisLine={false}
                    domain={[0, "dataMax"]}
                    padding={{ top: 12 }}
                    tickCount={4}
                    tickFormatter={(value) => formatCompactNumber(Number(value))}
                    tickLine={false}
                    tickMargin={10}
                    width={72}
                  />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        indicator="dot"
                        labelFormatter={(value) => formatBucket(String(value), chartGranularity.tokenTypeBreakdown)}
                      />
                    }
                    cursor={false}
                  />
                  {tokenBreakdownSeries.map((series) => (
                    <Area
                      activeDot={{ r: 4 }}
                      dataKey={series.dataKey}
                      fill={`url(#${series.gradientId})`}
                      isAnimationActive={false}
                      key={series.dataKey}
                      name={series.label}
                      stroke={series.color}
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      type="monotone"
                    />
                  ))}
                </AreaChart>
              </ChartContainer>
            </CardContent>
          )}
        </Card>

        <Card className="operator-section-surface">
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
              <OperatorEmptyState
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
              <OperatorEmptyState
                className="py-10"
                description={messages.statistics.adjustFiltersOrTimeRange}
                title={messages.statistics.noCostRecordsFound}
              />
            </CardContent>
          ) : (
            <CardContent className="flex flex-col gap-6 pt-4 sm:pt-6">
              <div
                className="rounded-xl border border-outline-variant bg-surface-container-low p-4"
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


            </CardContent>
          )}
        </Card>
      </div>
    </section>
  );
}
