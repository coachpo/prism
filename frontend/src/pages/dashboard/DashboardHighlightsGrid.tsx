import { Activity, ArrowUpRight, DollarSign, FileText } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { EmptyState } from "@/components/EmptyState";
import { ApiFamilyIcon } from "@/components/ApiFamilyIcon";
import { CompactMetricTile } from "@/components/CompactMetricTile";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { cn, formatApiFamily } from "@/lib/utils";
import type { StatGroup } from "@/lib/types";
import type {
  DashboardMetricSnapshot,
  DashboardStrategyFamilySummary,
} from "./useDashboardPageData";

interface DashboardHighlightsGridProps {
  highlighted: boolean;
  onInspectSpending: () => void;
  onOpenStatistics: () => void;
  onReviewRequests: () => void;
  apiFamilyRows: StatGroup[];
  snapshot: DashboardMetricSnapshot;
  strategyFamilySummary: DashboardStrategyFamilySummary;
}

export function DashboardHighlightsGrid({
  highlighted,
  onInspectSpending,
  onOpenStatistics,
  onReviewRequests,
  apiFamilyRows,
  snapshot,
  strategyFamilySummary,
}: DashboardHighlightsGridProps) {
  const { formatNumber, messages } = useLocale();
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const performanceTiles = [
    { label: messages.dashboard.avgLatency, value: `${formatNumber(snapshot.avgLatency)}ms` },
    { label: messages.dashboard.p95Latency, value: `${formatNumber(snapshot.p95Latency)}ms` },
    {
      label: messages.dashboard.errorRate,
      value: `${formatNumber(snapshot.errorRate, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%`,
    },
    {
      label: messages.dashboard.streamingShare,
      value: `${formatNumber(snapshot.streamShare, { minimumFractionDigits: 1, maximumFractionDigits: 1 })}%`,
    },
  ];

  return (
    <div className="grid gap-4 lg:grid-cols-3">
      <Card>
        <CardHeader>
          <CardTitle>{messages.dashboard.performanceSnapshot}</CardTitle>
          <CardDescription>{messages.dashboard.performanceSnapshotDescription}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-3">
              {performanceTiles.map((tile) => (
                <CompactMetricTile
                  key={tile.label}
                  className={cn(highlighted && "ws-value-updated")}
                  label={tile.label}
                  value={tile.value}
                />
              ))}
            </div>
          <Button variant="outline" className="w-full" onClick={onOpenStatistics}>
            {messages.dashboard.openStatistics}
            <ArrowUpRight className="ml-2 h-4 w-4" />
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{messages.dashboard.apiFamilyMix}</CardTitle>
          <CardDescription>{messages.dashboard.apiFamilyMixDescription}</CardDescription>
        </CardHeader>
        <CardContent>
          {apiFamilyRows.length === 0 ? (
            <EmptyState
              icon={<Activity className="h-6 w-6" />}
              title={messages.dashboard.noApiFamilyActivity}
              description={messages.dashboard.noApiFamilyActivityDescription}
            />
          ) : (
            <div className="space-y-3">
              {apiFamilyRows.slice(0, 3).map((apiFamily) => {
                const ratio =
                  snapshot.totalRequests > 0
                    ? (apiFamily.total_requests / snapshot.totalRequests) * 100
                    : 0;

                return (
                  <div key={apiFamily.key} className="space-y-1.5">
                    <div className="flex items-center justify-between">
                      <div className="flex min-w-0 items-center gap-1.5">
                        <ApiFamilyIcon apiFamily={apiFamily.key} size={14} />
                        <p className="truncate text-sm font-medium">
                          {formatApiFamily(apiFamily.key)}
                        </p>
                      </div>
                      <p className="text-xs text-muted-foreground tabular-nums">
                        {formatNumber(apiFamily.total_requests)} req
                      </p>
                    </div>
                    <div className="h-2 overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full bg-primary"
                        style={{ width: `${Math.max(4, ratio)}%` }}
                      />
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          <div className="mt-4 border-t pt-4">
            <p className="text-xs font-medium text-muted-foreground">
              {messages.dashboard.routingStrategyMix}
            </p>
            <div className="mt-2 flex flex-wrap gap-2 text-xs text-muted-foreground">
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.strategyFamilyCount(
                  strategyCopy.legacyFamilyLabel,
                  formatNumber(strategyFamilySummary.legacyCount),
                )}
              </span>
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.strategyFamilyCount(
                  strategyCopy.adaptiveFamilyLabel,
                  formatNumber(strategyFamilySummary.adaptiveCount),
                )}
              </span>
              {strategyFamilySummary.unassignedCount > 0 ? (
                <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                  {messages.dashboard.strategyFamilyCount(
                    messages.modelsUi.strategyNotConfigured,
                    formatNumber(strategyFamilySummary.unassignedCount),
                  )}
                </span>
              ) : null}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>{messages.dashboard.quickActions}</CardTitle>
          <CardDescription>{messages.dashboard.quickActionsDescription}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <Button variant="outline" className="w-full justify-start" onClick={onReviewRequests}>
            <FileText className="mr-2 h-4 w-4" />
            {messages.dashboard.reviewRequests}
          </Button>
          <Button variant="outline" className="w-full justify-start" onClick={onInspectSpending}>
            <DollarSign className="mr-2 h-4 w-4" />
            {messages.dashboard.inspectSpendingBreakdown}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
