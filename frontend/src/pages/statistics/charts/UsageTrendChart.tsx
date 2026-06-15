import { useId, useMemo } from "react";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
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
import { useLocale } from "@/i18n/useLocale";
import type { UsageChartGranularity } from "@/lib/types";
import { OperatorEmptyState } from "@/shared/design-system";

export interface UsageTrendChartSeriesPoint {
  bucket_start: string;
  value: number;
}

export interface UsageTrendChartSeries {
  key: string;
  label: string;
  points: UsageTrendChartSeriesPoint[];
}

interface UsageTrendChartProps {
  axisFormatValue?: (value: number) => string;
  description: string;
  emptyDescription: string;
  emptyTitle: string;
  formatValue?: (value: number) => string;
  granularity: UsageChartGranularity;
  onGranularityChange: (granularity: UsageChartGranularity) => void;
  series: UsageTrendChartSeries[];
  title: string;
}

const SERIES_COLORS = [
  "var(--color-chart-1)",
  "var(--color-chart-2)",
  "var(--color-chart-3)",
  "var(--color-chart-4)",
  "var(--color-chart-5)",
  "var(--color-primary)",
  "var(--color-success)",
  "var(--color-warning)",
  "var(--color-info)",
];

type ChartSeries = UsageTrendChartSeries & {
  gradientId: string;
  isPrimary: boolean;
  safeKey: string;
};

export function UsageTrendChart({
  axisFormatValue,
  description,
  emptyDescription,
  emptyTitle,
  formatValue,
  granularity,
  onGranularityChange,
  series,
  title,
}: UsageTrendChartProps) {
  const { locale, messages } = useLocale();
  const chartId = useId().replace(/:/g, "");

  const chartSeries = useMemo<ChartSeries[]>(
    () =>
      series.map((item, index) => ({
        ...item,
        gradientId: `${chartId}-series-${index}`,
        isPrimary: item.key === "all",
        safeKey: `series_${index}`,
      })),
    [chartId, series],
  );

  const renderedSeries = useMemo(
    () => [...chartSeries].sort((left, right) => Number(left.isPrimary) - Number(right.isPrimary)),
    [chartSeries],
  );

  const chartData = useMemo(() => {
    const rows = new Map<string, Record<string, number | string>>();

    for (const item of chartSeries) {
      for (const point of item.points) {
        const row = rows.get(point.bucket_start) ?? { bucket_start: point.bucket_start };
        row[item.safeKey] = point.value;
        rows.set(point.bucket_start, row);
      }
    }

    return [...rows.values()].sort((left, right) => {
      const leftValue = typeof left.bucket_start === "string" ? left.bucket_start : "";
      const rightValue = typeof right.bucket_start === "string" ? right.bucket_start : "";
      return leftValue.localeCompare(rightValue);
    });
  }, [chartSeries]);

  const config = useMemo<ChartConfig>(
    () =>
      chartSeries.reduce<ChartConfig>((accumulator, item, index) => {
        accumulator[item.safeKey] = {
          color: SERIES_COLORS[index % SERIES_COLORS.length],
          label: item.label,
        };
        return accumulator;
      }, {}),
    [chartSeries],
  );

  const formatBucket = (value: string) => {
    const date = new Date(value);
    return new Intl.DateTimeFormat(locale, {
      day: "numeric",
      hour: granularity === "hourly" ? "numeric" : undefined,
      month: "short",
    }).format(date);
  };

  const isEmpty = chartSeries.length === 0 || chartData.length === 0;

  return (
    <Card className="@container/card operator-section-surface">
      <CardHeader className="gap-3 border-b">
        <div className="grid flex-1 gap-1">
          <CardTitle className="text-base">
            <h3>{title}</h3>
          </CardTitle>
          <CardDescription className="max-w-[48ch]">{description}</CardDescription>
        </div>
        <CardAction className="flex items-center">
          <div className="inline-flex items-center gap-1 rounded-lg border border-outline-variant bg-surface-container-low p-1">
            <Button
              aria-pressed={granularity === "hourly"}
              onClick={() => onGranularityChange("hourly")}
              size="sm"
              type="button"
              variant={granularity === "hourly" ? "secondary" : "ghost"}
            >
              {messages.statistics.byHour}
            </Button>
            <Button
              aria-pressed={granularity === "daily"}
              onClick={() => onGranularityChange("daily")}
              size="sm"
              type="button"
              variant={granularity === "daily" ? "secondary" : "ghost"}
            >
              {messages.statistics.byDay}
            </Button>
          </div>
        </CardAction>
      </CardHeader>

      {isEmpty ? (
        <CardContent className="pt-2">
          <OperatorEmptyState className="py-10" description={emptyDescription} title={emptyTitle} />
        </CardContent>
      ) : (
        <CardContent className="pt-4 sm:pt-6">
          <ChartContainer className="aspect-auto h-72 w-full" config={config}>
            <AreaChart
              accessibilityLayer
              data={chartData}
              margin={{ bottom: 4, left: 12, right: 16, top: 16 }}
            >
              <defs>
                {chartSeries.map((item) => {
                  const startOpacity = item.isPrimary ? 0.5 : 0.22;
                  const endOpacity = item.isPrimary ? 0.06 : 0.01;

                  return (
                    <linearGradient id={item.gradientId} key={item.gradientId} x1="0" x2="0" y1="0" y2="1">
                      <stop offset="5%" stopColor={`var(--color-${item.safeKey})`} stopOpacity={startOpacity} />
                      <stop offset="95%" stopColor={`var(--color-${item.safeKey})`} stopOpacity={endOpacity} />
                    </linearGradient>
                  );
                })}
              </defs>
              <CartesianGrid vertical={false} />
              <XAxis
                allowDataOverflow
                axisLine={false}
                dataKey="bucket_start"
                minTickGap={32}
                padding={{ left: 8, right: 8 }}
                tickFormatter={(value) => formatBucket(String(value))}
                tickLine={false}
                tickMargin={8}
              />
              <YAxis
                allowDataOverflow
                axisLine={false}
                domain={[0, "dataMax"]}
                padding={{ top: 12 }}
                tickFormatter={(value) => {
                  const numericValue = Number(value);
                  if (axisFormatValue) {
                    return axisFormatValue(numericValue);
                  }

                  return formatValue ? formatValue(numericValue) : String(value);
                }}
                tickLine={false}
                tickMargin={10}
                tickCount={4}
                width={72}
              />
              <ChartTooltip
                content={
                  <ChartTooltipContent
                    formatter={(value, name) => (
                      <div className="flex w-full items-center justify-between gap-3">
                        <span className="text-muted-foreground">{name}</span>
                        <span className="font-medium text-foreground">
                          {formatValue ? formatValue(Number(value)) : String(value)}
                        </span>
                      </div>
                    )}
                    indicator={chartSeries.length > 1 ? "dot" : "line"}
                    labelFormatter={(value) => formatBucket(String(value))}
                  />
                }
                cursor={false}
              />
              {renderedSeries.map((item) => (
                <Area
                  activeDot={{ r: item.isPrimary ? 4 : 3 }}
                  dataKey={item.safeKey}
                  fill={`url(#${item.gradientId})`}
                  isAnimationActive={false}
                  key={item.safeKey}
                  name={item.label}
                  stroke={`var(--color-${item.safeKey})`}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={item.isPrimary ? 2.5 : 2}
                  type="monotone"
                />
              ))}
            </AreaChart>
          </ChartContainer>
        </CardContent>
      )}
    </Card>
  );
}
