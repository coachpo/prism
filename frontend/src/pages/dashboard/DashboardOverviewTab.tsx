import { DashboardHighlightsGrid } from "@/pages/dashboard/DashboardHighlightsGrid";
import { DashboardMetricsGrid } from "@/pages/dashboard/DashboardMetricsGrid";
import { DashboardPageSkeleton } from "@/pages/dashboard/DashboardPageSkeleton";
import { RecentActivityCard } from "@/pages/dashboard/RecentActivityCard";
import { TopSpendingModelsCard } from "@/pages/dashboard/TopSpendingModelsCard";
import type { DashboardOverviewData } from "@/pages/dashboard/useDashboardPageData";

interface DashboardOverviewTabProps {
  clearRecentRequestHighlight: (requestId: number) => void;
  loading: boolean;
  metricsHighlighted: boolean;
  overviewData: DashboardOverviewData;
  recentNewIds: Set<number>;
  formatTime: (value: string) => string;
  onOpenAnalytics: () => void;
  onInspectSpending: () => void;
  onReviewRequests: () => void;
  onSelectRecentActivity: (requestId: number) => void;
}

export function DashboardOverviewTab({
  clearRecentRequestHighlight,
  loading,
  metricsHighlighted,
  overviewData,
  recentNewIds,
  formatTime,
  onOpenAnalytics,
  onInspectSpending,
  onReviewRequests,
  onSelectRecentActivity,
}: DashboardOverviewTabProps) {
  const {
    apiFamilyRows,
    metricSnapshot,
    modelDisplayNames,
    recentActivityItems,
    topSpendingModels,
  } = overviewData;

  if (loading) {
    return <DashboardPageSkeleton />;
  }

  return (
    <div className="flex flex-col gap-[var(--density-page-gap)]">
      <DashboardMetricsGrid snapshot={metricSnapshot} highlighted={metricsHighlighted} />

      <DashboardHighlightsGrid
        snapshot={metricSnapshot}
        apiFamilyRows={apiFamilyRows}
        highlighted={metricsHighlighted}
        onOpenAnalytics={onOpenAnalytics}
        onInspectSpending={onInspectSpending}
        onReviewRequests={onReviewRequests}
      />

      <div className="grid gap-[var(--density-card-gap)] md:grid-cols-2 lg:grid-cols-7">
        <RecentActivityCard
          recentActivityItems={recentActivityItems}
          recentNewIds={recentNewIds}
          clearRecentRequestHighlight={clearRecentRequestHighlight}
          modelDisplayNames={modelDisplayNames}
          formatTime={formatTime}
          onSelectRequest={onSelectRecentActivity}
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

