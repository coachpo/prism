import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { useLocale } from "@/i18n/useLocale";
import { formatApiFamily } from "@/lib/utils";
import { formatMoneyMicros } from "@/lib/costing";
import { getAdaptiveRoutingObjectiveLabel } from "@/lib/loadbalanceRoutingPolicy";
import { useTimezone } from "@/hooks/useTimezone";
import { Coins, FileText } from "lucide-react";
import type { ModelConfig, SpendingSummary } from "@/lib/types";

interface OverviewCardsProps {
  model: ModelConfig;
  spending: SpendingSummary | null;
  spendingLoading: boolean;
  spendingCurrencySymbol: string;
  spendingCurrencyCode: string;
  proxyTargetSummary?: {
    targetCount: number;
    firstTargetId: string | null;
    firstTargetLabel: string | null;
    routePolicyLabel: string;
  };
  onViewRequestLogs?: () => void;
}

export function OverviewCards({
  model,
  spending,
  spendingLoading,
  spendingCurrencySymbol,
  spendingCurrencyCode,
  proxyTargetSummary,
  onViewRequestLogs,
}: OverviewCardsProps) {
  const { format: formatTime } = useTimezone();
  const { formatNumber, locale, messages } = useLocale();
  const copy = messages.modelDetail;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const fieldCopy = messages.common;
  const apiFamily = model.api_family ?? "openai";
  const vendorLabel = model.vendor?.name ?? formatApiFamily(apiFamily);
  const strategyTypeLabel = model.loadbalance_strategy
    ? model.loadbalance_strategy.strategy_type === "adaptive"
      ? strategyCopy.adaptiveFamilyLabel
      : model.loadbalance_strategy.legacy_strategy_type === "single"
        ? strategyCopy.singleLabel
        : model.loadbalance_strategy.legacy_strategy_type === "fill-first"
          ? strategyCopy.fillFirstLabel
          : strategyCopy.roundRobinLabel
    : null;
  const strategyAssignmentLabel = model.loadbalance_strategy
    ? model.loadbalance_strategy.strategy_type === "adaptive"
      ? `${strategyCopy.adaptiveFamilyLabel} • ${getAdaptiveRoutingObjectiveLabel(model.loadbalance_strategy.routing_policy.routing_objective, strategyCopy)}`
      : `${strategyCopy.legacyFamilyLabel} • ${strategyTypeLabel}`
    : null;
  const strategyRoutingObjectiveLabel = model.loadbalance_strategy
    ? model.loadbalance_strategy.strategy_type === "adaptive"
      ? getAdaptiveRoutingObjectiveLabel(model.loadbalance_strategy.routing_policy.routing_objective, strategyCopy)
      : strategyTypeLabel
    : null;

  return (
    <>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardContent className="p-4">
            <h3 className="mb-4 font-semibold">{copy.configuration}</h3>
            <div className="grid gap-4 sm:grid-cols-2">
              <div>
                <p className="text-xs text-muted-foreground mb-1">{fieldCopy.vendor}</p>
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium">{vendorLabel}</span>
                </div>
              </div>
              <div>
                <p className="text-xs text-muted-foreground mb-1">{fieldCopy.apiFamily}</p>
                <div className="flex items-center gap-2">
                  <ApiFamilyIcon apiFamily={apiFamily} size={14} />
                  <span className="text-sm font-medium">{formatApiFamily(apiFamily)}</span>
                </div>
              </div>
              <div>
                <p className="text-xs text-muted-foreground mb-1">
                  {model.model_type === "proxy" ? copy.proxyRouting : copy.loadbalanceStrategy}
                </p>
                <div className="text-sm font-medium">
                  {model.model_type === "proxy" ? (
                    <div className="space-y-0.5">
                      <div>{proxyTargetSummary?.routePolicyLabel ?? copy.orderedPriorityRouting}</div>
                      <div className="text-xs font-normal text-muted-foreground">
                        {copy.targets(formatNumber(proxyTargetSummary?.targetCount ?? 0))}
                        {proxyTargetSummary?.firstTargetId
                          ? ` · ${copy.firstTarget(proxyTargetSummary.firstTargetId)}`
                          : ""}
                      </div>
                    </div>
                  ) : model.loadbalance_strategy ? (
                    <div className="space-y-0.5">
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
                <p className="text-xs text-muted-foreground mb-1">{copy.routingObjective}</p>
                <span className="text-sm font-medium">
                  {model.model_type === "native" && strategyRoutingObjectiveLabel ? (
                    strategyRoutingObjectiveLabel
                  ) : (
                    <span className="text-muted-foreground">{messages.common.notApplicable}</span>
                  )}
                </span>
              </div>
              <div>
                <p className="text-xs text-muted-foreground mb-1">{copy.created}</p>
                <span className="text-sm font-medium">
                  {formatTime(model.created_at, { year: "numeric", month: "numeric", day: "numeric" })}
                </span>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="p-4">
            <h3 className="mb-4 flex items-center gap-2 font-semibold">
              <Coins className="h-4 w-4" />
              {copy.costOverview}
            </h3>
            {spendingLoading ? (
              <div className="space-y-2">
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
                  <div className="space-y-1">
                    <p className="text-sm font-medium">{copy.successfulRequests(formatNumber(spending.successful_request_count))}</p>
                    <p className="text-xs text-muted-foreground">
                      {copy.totalTokens(formatNumber(spending.total_tokens))}
                    </p>
                  </div>
                </div>
                <div className="col-span-2 pt-2 border-t">
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
              <div className="flex flex-col items-center justify-center h-[100px] text-muted-foreground">
                <p className="text-sm">{copy.noCostDataAvailable}</p>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {onViewRequestLogs && (
        <Card>
          <CardContent className="p-4">
            <Button variant="outline" size="sm" className="mt-3 w-full gap-1.5 text-xs" onClick={onViewRequestLogs}>
              <FileText className="h-3.5 w-3.5" />
              {copy.viewRequestLogs}
            </Button>
          </CardContent>
        </Card>
      )}
    </>
  );
}
