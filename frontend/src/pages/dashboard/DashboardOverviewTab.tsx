import { AlertTriangle, ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { DashboardHighlightsGrid } from "@/pages/dashboard/DashboardHighlightsGrid";
import { DashboardMetricsGrid } from "@/pages/dashboard/DashboardMetricsGrid";
import { DashboardPageSkeleton } from "@/pages/dashboard/DashboardPageSkeleton";
import { RecentActivityCard } from "@/pages/dashboard/RecentActivityCard";
import { TopSpendingModelsCard } from "@/pages/dashboard/TopSpendingModelsCard";
import type { DashboardOverviewData } from "@/pages/dashboard/useDashboardPageData";
import { OperatorCallout } from "@/shared/design-system";

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
  onViewLoadbalanceEvents: () => void;
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
  onViewLoadbalanceEvents,
  onSelectRecentActivity,
}: DashboardOverviewTabProps) {
  const {
    apiFamilyRows,
    incidents,
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
      <IncidentBannerCard incidents={incidents} onViewEvents={onViewLoadbalanceEvents} />

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

function IncidentBannerCard({
  incidents,
  onViewEvents,
}: {
  incidents: DashboardOverviewData["incidents"];
  onViewEvents: () => void;
}) {
  const { formatNumber, messages } = useLocale();
  const activeBanCount = incidents?.active_bans.length ?? 0;
  const recentFailoverCount =
    incidents?.recent_events.filter((event) =>
      event.event_type === "banned" || event.event_type === "retry_exhausted"
    ).length ?? 0;

  if (activeBanCount === 0 && recentFailoverCount === 0) {
    return null;
  }

  return (
    <OperatorCallout
      intent="warning"
      icon={<AlertTriangle />}
      title={messages.dashboard.incidentBannerTitle}
      description={(
        <span className="flex flex-wrap gap-x-3 gap-y-1">
          <span>{messages.dashboard.incidentActiveBans(formatNumber(activeBanCount))}</span>
          <span>{messages.dashboard.incidentRecentFailovers(formatNumber(recentFailoverCount))}</span>
        </span>
      )}
      action={(
        <Button variant="outline" size="sm" className="h-8 gap-1.5" onClick={onViewEvents}>
          {messages.dashboard.incidentViewEvents}
          <ArrowRight data-icon="inline-end" />
        </Button>
      )}
      className="py-2.5"
    />
  );
}
