import { useMemo } from "react";
import { Line, LineChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart";
import type { UsageChartGranularity } from "@/lib/types";
import { useLocale } from "@/i18n/useLocale";

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
  description: string;
  emptyDescription: string;
  emptyTitle: string;
  formatValue?: (value: number) => string;
  granularity: UsageChartGranularity;
  onGranularityChange: (granularity: UsageChartGranularity) => void;
  series: UsageTrendChartSeries[];
  title: string;
}

const LINE_COLORS = [
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

export function UsageTrendChart({
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

  const chartSeries = useMemo(
    () => series.map((item, index) => ({ ...item, safeKey: `series_${index}` })),
    [series],
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
          color: LINE_COLORS[index % LINE_COLORS.length],
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

  return (
    <div className="rounded-xl border border-border/70 bg-card/95 p-[var(--density-card-pad-x)] shadow-none">
      <div className="flex flex-col gap-3 border-b border-border/60 pb-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="space-y-1">
          <h3 className="text-base font-semibold tracking-tight">{title}</h3>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            onClick={() => onGranularityChange("hourly")}
            size="sm"
            variant={granularity === "hourly" ? "default" : "outline"}
          >
            {messages.statistics.byHour}
          </Button>
          <Button
            onClick={() => onGranularityChange("daily")}
            size="sm"
            variant={granularity === "daily" ? "default" : "outline"}
          >
            {messages.statistics.byDay}
          </Button>
        </div>
      </div>

      {chartSeries.length === 0 || chartData.length === 0 ? (
        <EmptyState className="py-10" description={emptyDescription} title={emptyTitle} />
      ) : (
        <ChartContainer className="mt-6 h-72 w-full" config={config}>
          <LineChart data={chartData} margin={{ bottom: 0, left: 0, right: 12, top: 8 }}>
            <CartesianGrid strokeDasharray="3 3" vertical={false} />
            <XAxis
              axisLine={false}
              dataKey="bucket_start"
              minTickGap={24}
              tickFormatter={(value) => formatBucket(String(value))}
              tickLine={false}
            />
            <YAxis
              axisLine={false}
              tickFormatter={(value) => (formatValue ? formatValue(Number(value)) : String(value))}
              tickLine={false}
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
                  labelFormatter={(value) => formatBucket(String(value))}
                />
              }
            />
            {chartSeries.map((item, index) => (
              <Line
                dataKey={item.safeKey}
                dot={false}
                isAnimationActive={false}
                key={item.safeKey}
                name={item.label}
                stroke={LINE_COLORS[index % LINE_COLORS.length]}
                strokeLinecap="round"
                strokeWidth={item.key === "all" ? 3 : 2}
                type="monotone"
              />
            ))}
          </LineChart>
        </ChartContainer>
      )}
    </div>
  );
}
