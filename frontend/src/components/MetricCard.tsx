import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Card, CardContent } from "@/components/ui/card";

interface MetricCardProps {
  label: ReactNode;
  value: ReactNode;
  detail?: ReactNode;
  icon?: ReactNode;
  trend?: { value: string; positive?: boolean };
  className?: string;
  onClick?: () => void;
}

export function MetricCard({ label, value, detail, icon, trend, className, onClick }: MetricCardProps) {
  return (
    <Card
      data-slot="metric-card"
      className={cn(
        "overflow-hidden transition-colors duration-150",
        onClick && "cursor-pointer hover:border-primary/30",
        className
      )}
      onClick={onClick}
    >
      <CardContent className="overflow-hidden p-[var(--density-metric-pad)]">
        <div className="flex min-w-0 items-start justify-between gap-3 overflow-hidden">
          <div className="min-w-0 flex-1 space-y-2">
            <div
              data-slot="metric-label"
              className="flex min-w-0 flex-wrap items-center gap-2 overflow-hidden text-sm font-medium text-muted-foreground [&_[data-slot=badge]]:max-w-full [&_[data-slot=badge]]:truncate"
            >
              {label}
            </div>
            <div className="flex min-w-0 flex-wrap items-baseline gap-2">
              <span
                data-slot="metric-value"
                className="min-w-0 break-words text-2xl font-bold leading-tight tracking-tight"
              >
                {value}
              </span>
              {trend && (
                <span
                  className={cn(
                    "max-w-full break-words text-xs font-medium",
                    trend.positive ? "text-success" : "text-destructive"
                  )}
                >
                  {trend.value}
                </span>
              )}
            </div>
            {detail && (
              <p data-slot="metric-detail" className="text-xs text-muted-foreground">
                {detail}
              </p>
            )}
          </div>
          {icon && (
            <div
              data-slot="icon"
              className="flex h-[var(--density-control-h)] w-[var(--density-control-h)] shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"
            >
              {icon}
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
