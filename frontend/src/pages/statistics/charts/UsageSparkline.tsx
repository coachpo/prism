import { Area, AreaChart } from "recharts";
import { ChartContainer, type ChartConfig } from "@/components/ui/chart";

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
  const config: ChartConfig = {
    value: {
      color,
      label: ariaLabel,
    },
  };

  return (
    <ChartContainer aria-label={ariaLabel} className={className ?? "h-14 w-full"} config={config}>
      <AreaChart data={points} margin={{ bottom: 0, left: 0, right: 0, top: 0 }}>
        <defs>
          <linearGradient id="usage-sparkline-fill" x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.35} />
            <stop offset="100%" stopColor={color} stopOpacity={0.02} />
          </linearGradient>
        </defs>
        <Area
          dataKey="value"
          fill="url(#usage-sparkline-fill)"
          isAnimationActive={false}
          stroke={color}
          strokeWidth={2}
          type="monotone"
        />
      </AreaChart>
    </ChartContainer>
  );
}
