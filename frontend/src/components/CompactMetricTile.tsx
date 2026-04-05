import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface CompactMetricTileProps {
  className?: string;
  detail?: ReactNode;
  label: ReactNode;
  value: ReactNode;
  valueClassName?: string;
}

export function CompactMetricTile({
  className,
  detail,
  label,
  value,
  valueClassName,
}: CompactMetricTileProps) {
  return (
    <div
      data-slot="compact-metric-tile"
      className={cn(
        "rounded-md border border-border/70 bg-muted/30 p-3 transition-colors duration-300",
        className,
      )}
    >
      <p data-slot="metric-label" className="text-xs text-muted-foreground">
        {label}
      </p>
      <div
        data-slot="metric-value"
        className={cn("mt-1 text-lg font-semibold tabular-nums", valueClassName)}
      >
        {value}
      </div>
      {detail ? (
        <div data-slot="metric-detail" className="mt-1 text-xs text-muted-foreground">
          {detail}
        </div>
      ) : null}
    </div>
  );
}
