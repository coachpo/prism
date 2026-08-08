import { Skeleton } from "@/components/ui/skeleton";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { useLocale } from "@/i18n/useLocale";
import { formatApiFamily } from "@/lib/utils";
import { formatMoneyMicros } from "@/lib/costing";
import { getLoadbalanceStrategyDetailLabel } from "@/lib/loadbalanceRoutingPolicy";
import { useTimezone } from "@/hooks/useTimezone";
import { Coins } from "lucide-react";
import type { ModelConfig, SpendingSummary } from "@/lib/types";
import { OperatorEmptyState, OperatorSectionCard } from "@/shared/design-system";
import type { AccessTargetSummary } from "./useModelDetailDataSupport";

interface OverviewCardsProps {
  model: ModelConfig;
  spending: SpendingSummary | null;
  spendingLoading: boolean;
  spendingCurrencySymbol: string;
  spendingCurrencyCode: string;
  accessTargetSummary?: AccessTargetSummary;
}

export function OverviewCards({
  model,
  spending,
  spendingLoading,
  spendingCurrencySymbol,
  spendingCurrencyCode,
  accessTargetSummary,
}: OverviewCardsProps) {
  const { format: formatTime } = useTimezone();
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.modelDetail;
  const modelsUiCopy = messages.modelsUi;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;
  const apiFamily = model.api_family ?? "openai";
  const strategyAssignmentLabel = model.loadbalance_strategy
    ? getLoadbalanceStrategyDetailLabel(model.loadbalance_strategy, strategyCopy)
    : null;
  const hasEnabledAccessTarget = (accessTargetSummary?.enabledTargetCount ?? 0) > 0;
  // Only the position-smallest enabled mixed row may be presented as the first
  // target; per-type counts are plain statistics and never imply a priority tier.
  const accessTargetLabel = hasEnabledAccessTarget && accessTargetSummary
    ? accessTargetSummary.firstEnabledTargetLabel
      ? modelsUiCopy.targetsFirst(formatNumber(accessTargetSummary.enabledTargetCount), accessTargetSummary.firstEnabledTargetLabel)
      : modelsUiCopy.accessTargets + ": " + formatNumber(accessTargetSummary.enabledTargetCount)
    : modelsUiCopy.needsTarget;
  const spendingTokenDetail = spending
    ? [
        `${messages.requestLogs.input} ${formatNumber(spending.total_input_tokens)}`,
        `${messages.requestLogs.output} ${formatNumber(spending.total_output_tokens)}`,
        `${messages.statistics.cachedPrefix} ${formatNumber(spending.total_cache_read_input_tokens + spending.total_cache_creation_input_tokens)}`,
        `${messages.requestLogs.reasoning} ${formatNumber(spending.total_reasoning_tokens)}`,
      ].join(" · ")
    : null;

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <OperatorSectionCard title={copy.configuration}>
        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <p className="text-xs text-muted-foreground mb-1">{fieldCopy.apiFamily}</p>
            <div className="flex items-center gap-2">
              <ApiFamilyIcon apiFamily={apiFamily} size={14} />
              <span className="text-sm font-medium">{formatApiFamily(apiFamily)}</span>
            </div>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">{copy.loadbalanceStrategy}</p>
            <div className="text-sm font-medium">
              {model.loadbalance_strategy ? (
                <div className="flex flex-col gap-0.5">
                  <div>{model.loadbalance_strategy.name}</div>
                  <div className="text-xs font-normal text-muted-foreground">
                    {strategyAssignmentLabel}
                  </div>
                </div>
              ) : (
                <span className="text-muted-foreground">{copy.unassigned}</span>
              )}
            </div>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">{modelsUiCopy.accessTargets}</p>
            <span className="text-sm font-medium">{accessTargetLabel}</span>
          </div>
          <div>
            <p className="text-xs text-muted-foreground mb-1">{copy.created}</p>
            <span className="text-sm font-medium">
              {formatTime(model.created_at, { year: "numeric", month: "numeric", day: "numeric" })}
            </span>
          </div>
        </div>
      </OperatorSectionCard>

      <OperatorSectionCard
        title={copy.costOverview}
        icon={<Coins />}
      >
        {spendingLoading ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-3/4" />
          </div>
        ) : spending ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <div>
              <p className="text-xs text-muted-foreground mb-1">{copy.totalCost(spendingCurrencyCode)}</p>
              <p className="text-2xl font-bold tracking-tight">
                {formatMoneyMicros(
                  spending.total_cost_micros,
                  spendingCurrencySymbol,
                  spendingCurrencyCode,
                  2,
                  6,
                  locale,
                )}
              </p>
            </div>
            <div>
              <p className="text-xs text-muted-foreground mb-1">{copy.requestsLabel}</p>
              <div className="flex flex-col gap-1">
                <p className="text-sm font-medium">
                  {copy.successfulRequests(formatNumber(spending.successful_request_count))}
                </p>
                <p className="text-xs text-muted-foreground">
                  {copy.totalTokens(formatNumber(spending.total_tokens))}
                </p>
                {spendingTokenDetail ? (
                  <p className="text-xs text-muted-foreground">
                    {spendingTokenDetail}
                  </p>
                ) : null}
              </div>
            </div>
            <div className="col-span-2 border-t border-outline-variant pt-2">
              <div className="flex justify-between text-xs">
                <span className="text-muted-foreground">{copy.avgCostPerRequest}</span>
                <span className="font-medium font-mono">
                  {formatMoneyMicros(
                    spending.avg_cost_per_successful_request_micros,
                    spendingCurrencySymbol,
                    spendingCurrencyCode,
                    4,
                    6,
                    locale,
                  )}
                </span>
              </div>
            </div>
          </div>
        ) : (
          <OperatorEmptyState
            title={copy.noCostDataAvailable}
            className="min-h-[100px]"
          />
        )}
      </OperatorSectionCard>
    </div>
  );
}
