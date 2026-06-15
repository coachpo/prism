import { Activity, DollarSign, Server } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { SpendTrustNote } from "@/components/SpendTrustIndicator";
import { useReportingCurrencyContext } from "@/context/ReportingCurrencyContext";
import { formatMoneyMicros, resolveSpendTrustState } from "@/lib/costing";
import { cn } from "@/lib/utils";
import { OperatorMetricCard } from "@/shared/design-system";
import type { DashboardMetricSnapshot } from "./useDashboardPageData";

function formatSpendCoverageDetail(
  pricedRequestCount: number,
  unpricedRequestCount: number,
  messages: ReturnType<typeof useLocale>["messages"],
) {
  const detailParts = [messages.statistics.requestBasedSpend];

  if (pricedRequestCount > 0) {
    detailParts.push(messages.statistics.pricedRequests(String(pricedRequestCount)));
  }

  if (unpricedRequestCount > 0) {
    detailParts.push(messages.statistics.unpriced(String(unpricedRequestCount)));
  }

  return detailParts.join(" · ");
}

interface DashboardMetricsGridProps {
  highlighted: boolean;
  snapshot: DashboardMetricSnapshot;
}

export function DashboardMetricsGrid({
  highlighted,
  snapshot,
}: DashboardMetricsGridProps) {
  const { currencyState } = useReportingCurrencyContext();
  const { formatNumber, locale, messages } = useLocale();
  const spendTrust = resolveSpendTrustState(
    {
      costMicros: snapshot.totalCost,
      pricedRequestCount: snapshot.pricedRequestCount,
      unpricedRequestCount: snapshot.unpricedRequestCount,
    },
    currencyState,
  );
  const spendMetricValue =
    spendTrust === "unpriced"
      ? "—"
      : formatMoneyMicros(snapshot.totalCost, undefined, undefined, 2, 6, locale);

  return (
    <div className="grid gap-[var(--density-card-gap)] md:grid-cols-2 lg:grid-cols-4">
      <OperatorMetricCard
        label={messages.dashboard.activeModels}
        value={snapshot.activeModels}
        detail={messages.dashboard.totalConfigured(formatNumber(snapshot.totalModels))}
        icon={<Server />}
      />
      <OperatorMetricCard
        label={messages.dashboard.requests24h}
        value={formatNumber(snapshot.totalRequests)}
        detail={messages.dashboard.successRate(
          formatNumber(snapshot.successRate, { minimumFractionDigits: 1, maximumFractionDigits: 1 })
        )}
        icon={<Activity />}
        className={cn(
          "[&_[data-slot=icon]]:bg-info/10 [&_[data-slot=icon]]:text-info",
          snapshot.successRate < 95 && "[&_[data-slot=metric-value]]:text-warning",
          highlighted && "ws-value-updated"
        )}
      />
      <OperatorMetricCard
        label={messages.dashboard.spending30d}
        value={spendMetricValue}
        detail={(
          <div className="flex flex-col gap-1">
            <p>
              {formatSpendCoverageDetail(
                snapshot.pricedRequestCount,
                snapshot.unpricedRequestCount,
                messages,
              )}
            </p>
            {spendTrust !== "verified" ? (
              <SpendTrustNote
                spendTrust={spendTrust}
                showPricingTemplatesLink={spendTrust === "unpriced"}
              />
            ) : null}
          </div>
        )}
        icon={<DollarSign />}
        className={cn(
          "[&_[data-slot=icon]]:bg-success/10 [&_[data-slot=icon]]:text-success",
          highlighted && "ws-value-updated"
        )}
      />
      <OperatorMetricCard
        label={messages.dashboard.averageRpm}
        value={formatNumber(snapshot.averageRpm, { minimumFractionDigits: 3, maximumFractionDigits: 3 })}
        detail={messages.dashboard.totalRequests(formatNumber(snapshot.averageRpmRequestTotal))}
        icon={<Activity />}
        className={cn(
          highlighted && "ws-value-updated"
        )}
      />
    </div>
  );
}
