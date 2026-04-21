import type { RoutingDiagramData } from "@/pages/dashboard/routingDiagram";
import { DashboardHighlightsGrid } from "@/pages/dashboard/DashboardHighlightsGrid";
import { DashboardMetricsGrid } from "@/pages/dashboard/DashboardMetricsGrid";
import { DashboardPageSkeleton } from "@/pages/dashboard/DashboardPageSkeleton";
import { RecentActivityCard } from "@/pages/dashboard/RecentActivityCard";
import { RoutingDiagramCard } from "@/pages/dashboard/RoutingDiagramCard";
import { TopSpendingModelsCard } from "@/pages/dashboard/TopSpendingModelsCard";
import type {
  DashboardMetricSnapshot,
  DashboardStrategyFamilySummary,
} from "@/pages/dashboard/useDashboardPageData";
import type { RequestLogListItem, SpendingTopModel, StatGroup } from "@/lib/types";

interface DashboardOverviewTabProps {
  apiFamilyRows: StatGroup[];
  clearRecentRequestHighlight: (requestId: number) => void;
  loading: boolean;
  metricSnapshot: DashboardMetricSnapshot;
  metricsHighlighted: boolean;
  modelDisplayNames: Map<string, string>;
  recentNewIds: Set<number>;
  recentRequests: RequestLogListItem[];
  routingDiagramData: RoutingDiagramData | null;
  routingDiagramError: string | null;
  routingDiagramLoading: boolean;
  strategyFamilySummary: DashboardStrategyFamilySummary;
  topSpendingModels: SpendingTopModel[];
  formatTime: (value: string) => string;
  onOpenAnalytics: () => void;
  onInspectSpending: () => void;
  onReviewRequests: () => void;
  onSelectModel: (modelConfigId: number) => void;
  onDrillDownRequests: (params: { endpoint_id?: number; model_id?: string }) => void;
}

export function DashboardOverviewTab({
  apiFamilyRows,
  clearRecentRequestHighlight,
  loading,
  metricSnapshot,
  metricsHighlighted,
  modelDisplayNames,
  recentNewIds,
  recentRequests,
  routingDiagramData,
  routingDiagramError,
  routingDiagramLoading,
  strategyFamilySummary,
  topSpendingModels,
  formatTime,
  onOpenAnalytics,
  onInspectSpending,
  onReviewRequests,
  onSelectModel,
  onDrillDownRequests,
}: DashboardOverviewTabProps) {
  if (loading) {
    return <DashboardPageSkeleton />;
  }

  return (
    <div className="flex flex-col gap-[var(--density-page-gap)]">
      <DashboardMetricsGrid snapshot={metricSnapshot} highlighted={metricsHighlighted} />

      <DashboardHighlightsGrid
        snapshot={metricSnapshot}
        apiFamilyRows={apiFamilyRows}
        strategyFamilySummary={strategyFamilySummary}
        highlighted={metricsHighlighted}
        onOpenAnalytics={onOpenAnalytics}
        onInspectSpending={onInspectSpending}
        onReviewRequests={onReviewRequests}
      />

      <RoutingDiagramCard
        data={routingDiagramData}
        loading={routingDiagramLoading}
        error={routingDiagramError}
        onSelectModel={onSelectModel}
        onDrillDownRequests={onDrillDownRequests}
      />

      <div className="grid gap-[var(--density-card-gap)] md:grid-cols-2 lg:grid-cols-7">
        <RecentActivityCard
          recentRequests={recentRequests}
          recentNewIds={recentNewIds}
          clearRecentRequestHighlight={clearRecentRequestHighlight}
          modelDisplayNames={modelDisplayNames}
          formatTime={formatTime}
        />

        <TopSpendingModelsCard
          topSpendingModels={topSpendingModels}
          modelDisplayNames={modelDisplayNames}
          onViewFullReport={onInspectSpending}
        />
      </div>
    </div>
  );
}

