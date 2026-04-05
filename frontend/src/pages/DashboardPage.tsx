import { RefreshCw } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { WebSocketStatusIndicator } from "@/components/WebSocketStatusIndicator";
import { PageHeader } from "@/components/PageHeader";
import { Button } from "@/components/ui/button";
import { useProfileContext } from "@/context/ProfileContext";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { DashboardHighlightsGrid } from "@/pages/dashboard/DashboardHighlightsGrid";
import { DashboardMetricsGrid } from "@/pages/dashboard/DashboardMetricsGrid";
import { DashboardPageSkeleton } from "@/pages/dashboard/DashboardPageSkeleton";
import { RecentActivityCard } from "@/pages/dashboard/RecentActivityCard";
import { RoutingDiagramCard } from "@/pages/dashboard/RoutingDiagramCard";
import { TopSpendingModelsCard } from "@/pages/dashboard/TopSpendingModelsCard";
import { useDashboardPageData } from "@/pages/dashboard/useDashboardPageData";

export function DashboardPage() {
  const navigate = useNavigate();
  const { revision, selectedProfile } = useProfileContext();
  const { format: formatTime } = useTimezone();
  const { messages } = useLocale();
  const data = useDashboardPageData({
    revision,
    selectedProfileId: selectedProfile?.id ?? null,
  });

  if (data.loading) {
    return <DashboardPageSkeleton />;
  }

  return (
    <div className="space-y-6">
      <PageHeader title={messages.dashboard.dashboardTitle} description={messages.dashboard.dashboardDescription}>
        <Button
          variant="outline"
          size="icon"
          className="h-9 w-9"
          onClick={() => void data.refreshDashboard()}
          disabled={data.isRefreshing}
          aria-label={messages.dashboard.refreshDashboard}
          title={messages.dashboard.refreshDashboard}
        >
          <RefreshCw className={`h-4 w-4 ${data.isRefreshing ? "animate-spin" : ""}`} />
        </Button>
        <WebSocketStatusIndicator
          connectionState={data.connectionState}
          isSyncing={data.isSyncing}
        />
      </PageHeader>

      <DashboardMetricsGrid
        snapshot={data.metricSnapshot}
        highlighted={data.metricsHighlighted}
      />

      <DashboardHighlightsGrid
        snapshot={data.metricSnapshot}
        apiFamilyRows={data.apiFamilyRows}
        strategyFamilySummary={data.strategyFamilySummary}
        highlighted={data.metricsHighlighted}
        onOpenStatistics={() => navigate("/statistics")}
        onInspectSpending={() => navigate("/statistics?tab=spending")}
        onReviewRequests={() => navigate("/request-logs")}
      />

      <RoutingDiagramCard
        data={data.routingDiagramData}
        loading={data.routingDiagramLoading}
        error={data.routingDiagramError}
        onSelectModel={(modelConfigId) => navigate(`/models/${modelConfigId}`)}
        onDrillDownRequests={(params) => {
          const sp = new URLSearchParams();
          if (params.endpoint_id) sp.set("endpoint_id", String(params.endpoint_id));
          if (params.model_id) sp.set("model_id", params.model_id);
          navigate(`/request-logs?${sp.toString()}`);
        }}
      />

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-7">
        <RecentActivityCard
          recentRequests={data.recentRequests}
          recentNewIds={data.recentNewIds}
          clearRecentRequestHighlight={data.clearRecentRequestHighlight}
          modelDisplayNames={data.modelDisplayNames}
          formatTime={formatTime}
        />

        <TopSpendingModelsCard
          topSpendingModels={data.topSpendingModels}
          modelDisplayNames={data.modelDisplayNames}
          onViewFullReport={() => navigate("/statistics?tab=spending")}
        />
      </div>
    </div>
  );
}
