import { Activity, DollarSign, Server } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { MetricCard } from "@/components/MetricCard";
import { formatMoneyMicros } from "@/lib/costing";
import { cn } from "@/lib/utils";
import type { DashboardMetricSnapshot } from "./useDashboardPageData";

interface DashboardMetricsGridProps {
  highlighted: boolean;
  snapshot: DashboardMetricSnapshot;
}

export function DashboardMetricsGrid({
  highlighted,
  snapshot,
}: DashboardMetricsGridProps) {
  const { formatNumber, locale, messages } = useLocale();

  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <MetricCard
        label={messages.dashboard.activeModels}
        value={snapshot.activeModels}
        detail={messages.dashboard.totalConfigured(formatNumber(snapshot.totalModels))}
        icon={<Server className="h-4 w-4" />}
      />
      <MetricCard
        label={messages.dashboard.requests24h}
        value={formatNumber(snapshot.totalRequests)}
        detail={messages.dashboard.successRate(
          formatNumber(snapshot.successRate, { minimumFractionDigits: 1, maximumFractionDigits: 1 })
        )}
        icon={<Activity className="h-4 w-4" />}
        className={cn(
          "[&_[data-slot=icon]]:bg-blue-500/10 [&_[data-slot=icon]]:text-blue-500",
          snapshot.successRate < 95 && "text-amber-600",
          highlighted && "ws-value-updated"
        )}
      />
      <MetricCard
        label={messages.dashboard.spending30d}
        value={formatMoneyMicros(snapshot.totalCost, "$", undefined, 2, 6, locale)}
        detail={messages.dashboard.estimatedCost}
        icon={<DollarSign className="h-4 w-4" />}
        className={cn(
          "[&_[data-slot=icon]]:bg-emerald-500/10 [&_[data-slot=icon]]:text-emerald-500",
          highlighted && "ws-value-updated"
        )}
      />
      <MetricCard
        label={messages.dashboard.averageRpm}
        value={formatNumber(snapshot.averageRpm, { minimumFractionDigits: 3, maximumFractionDigits: 3 })}
        detail={messages.dashboard.totalRequests(formatNumber(snapshot.averageRpmRequestTotal))}
        icon={<Activity className="h-4 w-4" />}
        className={cn(
          highlighted && "ws-value-updated"
        )}
      />
    </div>
  );
}
