import type { ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { UsageSparkline, type UsageSparklinePoint } from "./UsageSparkline";

interface UsageKpiCardProps {
  accentClassName?: string;
  detail?: string;
  icon?: ReactNode;
  label: string;
  sparkline?: UsageSparklinePoint[];
  sparklineColor?: string;
  value: string;
}

export function UsageKpiCard({
  accentClassName,
  detail,
  icon,
  label,
  sparkline,
  sparklineColor,
  value,
}: UsageKpiCardProps) {
  return (
    <Card className="overflow-hidden border-border/70 bg-card/95">
      <CardContent className="space-y-4 p-[var(--density-metric-pad)]">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 flex-1 space-y-2">
            <p data-slot="metric-label" className="text-sm font-medium text-muted-foreground">
              {label}
            </p>
            <div className="flex min-w-0 items-end gap-3">
              <span
                data-slot="metric-value"
                className="min-w-0 break-words text-2xl font-bold leading-tight tracking-tight"
              >
                {value}
              </span>
            </div>
            {detail ? (
              <p data-slot="metric-detail" className="text-xs text-muted-foreground">
                {detail}
              </p>
            ) : null}
          </div>
          {icon ? (
            <div
              className={cn(
                "flex h-[var(--density-control-h)] w-[var(--density-control-h)] shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary",
                accentClassName,
              )}
              data-slot="icon"
            >
              {icon}
            </div>
          ) : null}
        </div>

        {sparkline && sparkline.length > 1 ? (
          <UsageSparkline
            ariaLabel={label}
            color={sparklineColor}
            points={sparkline}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}
