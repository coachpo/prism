import { useId } from "react";
import { Area, AreaChart, XAxis, YAxis } from "recharts";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";
import { cn } from "@/lib/utils";

export interface UsageSparklinePoint {
  label: string;
  value: number;
}

interface UsageSparklineProps {
  ariaLabel: string;
  className?: string;
  color?: string;
  points: UsageSparklinePoint[];
}

export function UsageSparkline({
  ariaLabel,
  className,
  color = "var(--color-chart-1)",
  points,
}: UsageSparklineProps) {
  const gradientId = `${useId().replace(/:/g, "")}-usage-sparkline-fill`;
  const config: ChartConfig = {
    value: {
      color,
      label: ariaLabel,
    },
  };

  return (
    <ChartContainer
      aria-label={ariaLabel}
      className={cn("aspect-auto h-14 w-full", className)}
      config={config}
    >
      <AreaChart accessibilityLayer data={points} margin={{ bottom: 0, left: 4, right: 4, top: 4 }}>
        <defs>
          <linearGradient id={gradientId} x1="0" x2="0" y1="0" y2="1">
            <stop offset="5%" stopColor={color} stopOpacity={0.38} />
            <stop offset="95%" stopColor={color} stopOpacity={0.02} />
          </linearGradient>
        </defs>
        <XAxis allowDataOverflow dataKey="label" hide padding={{ left: 4, right: 4 }} />
        <YAxis allowDataOverflow domain={[0, "dataMax"]} hide padding={{ top: 6 }} />
        <Area
          dataKey="value"
          fill={`url(#${gradientId})`}
          isAnimationActive={false}
          stroke={color}
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={1.75}
          type="monotone"
        />
      </AreaChart>
    </ChartContainer>
  );
}
