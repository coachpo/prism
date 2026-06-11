import { useCallback } from "react";
import { RefreshCw } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { PageHeader } from "@/components/PageHeader";
import { WebSocketStatusIndicator } from "@/components/WebSocketStatusIndicator";
import { Button } from "@/components/ui/button";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useProfileContext } from "@/context/ProfileContext";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { DashboardAnalyticsContent } from "@/pages/dashboard/DashboardAnalyticsContent";
import { DashboardOverviewTab } from "@/pages/dashboard/DashboardOverviewTab";
import { RoutingDiagramCard } from "@/pages/dashboard/RoutingDiagramCard";
import { DASHBOARD_TAB_OPTIONS, type DashboardTab } from "@/pages/dashboard/queryParams";
import { useDashboardPageData } from "@/pages/dashboard/useDashboardPageData";
import { useDashboardPageState } from "@/pages/dashboard/useDashboardPageState";

function isDashboardTab(value: string): value is DashboardTab {
  return DASHBOARD_TAB_OPTIONS.some((tab) => tab === value);
}

export function DashboardPage() {
  const pageState = useDashboardPageState();
  const activeTab = pageState.state.tab;

  return (
    <div data-testid="observe-dashboard" className="flex flex-col gap-6">
      {activeTab === "analytics" ? (
        <DashboardAnalyticsSection pageState={pageState} />
      ) : (
        <DashboardAggregateSection pageState={pageState} activeTab={activeTab} />
      )}
    </div>
  );
}

function DashboardAggregateSection({
  pageState,
  activeTab,
}: {
  pageState: ReturnType<typeof useDashboardPageState>;
  activeTab: Extract<DashboardTab, "overview" | "routing">;
}) {
  const navigate = useNavigate();
  const { revision, selectedProfile } = useProfileContext();
  const { format: formatTime } = useTimezone();
  const { messages } = useLocale();
  const data = useDashboardPageData({
    revision,
    selectedProfileId: selectedProfile?.id ?? null,
  });
  const openAnalyticsTab = useCallback(() => {
    pageState.setTab("analytics");
  }, [pageState]);
  const handleReviewRequests = useCallback(() => {
    navigate("/request-logs");
  }, [navigate]);
  const handleSelectRecentRequest = useCallback(
    (requestId: number) => {
      const searchParams = new URLSearchParams({ request_id: String(requestId) });
      navigate(`/request-logs?${searchParams.toString()}`);
    },
    [navigate],
  );
  const handleSelectModel = useCallback(
    (modelConfigId: number) => {
      navigate(`/models/${modelConfigId}`);
    },
    [navigate],
  );
  const handleDrillDownRequests = useCallback(
    (params: { endpoint_id?: number; model_id?: string }) => {
      const searchParams = new URLSearchParams();

      if (params.endpoint_id) {
        searchParams.set("endpoint_id", String(params.endpoint_id));
      }

      if (params.model_id) {
        searchParams.set("model_id", params.model_id);
      }

      navigate(`/request-logs?${searchParams.toString()}`);
    },
    [navigate],
  );

  return (
    <>
      <PageHeader title={messages.dashboard.dashboardTitle} description={messages.dashboard.dashboardDescription}>
        <Button
          variant="outline"
          size="icon"
          className="h-9 w-9"
          onClick={() => {
            void data.refreshDashboard();
          }}
          disabled={data.isRefreshing}
          aria-label={messages.dashboard.refreshDashboard}
          title={messages.dashboard.refreshDashboard}
        >
          <RefreshCw className={`h-4 w-4 ${data.isRefreshing ? "animate-spin" : ""}`} />
        </Button>
        <WebSocketStatusIndicator connectionState={data.connectionState} isSyncing={data.isSyncing} />
      </PageHeader>

      <DashboardTabs pageState={pageState} />

      {activeTab === "overview" ? (
        <DashboardOverviewTab
          clearRecentRequestHighlight={data.clearRecentRequestHighlight}
          loading={data.loading}
          metricsHighlighted={data.metricsHighlighted}
          overviewData={data.overviewData}
          recentNewIds={data.recentNewIds}
          formatTime={formatTime}
          onOpenAnalytics={openAnalyticsTab}
          onInspectSpending={openAnalyticsTab}
          onReviewRequests={handleReviewRequests}
          onSelectRecentRequest={handleSelectRecentRequest}
        />
      ) : (
        <RoutingDiagramCard
          data={data.overviewData.routingDiagramData}
          loading={data.overviewData.routingDiagramLoading}
          error={data.overviewData.routingDiagramError}
          onSelectModel={handleSelectModel}
          onDrillDownRequests={handleDrillDownRequests}
        />
      )}
    </>
  );
}

function DashboardAnalyticsSection({ pageState }: { pageState: ReturnType<typeof useDashboardPageState> }) {
  const { messages } = useLocale();

  return (
    <>
      <PageHeader title={messages.dashboard.dashboardTitle} description={messages.dashboard.dashboardDescription} />
      <DashboardTabs pageState={pageState} />
      <DashboardAnalyticsContent />
    </>
  );
}

function DashboardTabs({ pageState }: { pageState: ReturnType<typeof useDashboardPageState> }) {
  const { messages } = useLocale();

  return (
    <Tabs
      value={pageState.state.tab}
      onValueChange={(value) => {
        if (isDashboardTab(value)) {
          pageState.setTab(value);
        }
      }}
      className="flex flex-col gap-4"
    >
      <TabsList className="grid h-10 w-full max-w-md grid-cols-3 rounded-xl bg-muted/70 p-0.5">
        <TabsTrigger value="overview" className="rounded-lg text-sm font-medium">
          {messages.dashboard.overviewTab}
        </TabsTrigger>
        <TabsTrigger value="analytics" className="rounded-lg text-sm font-medium">
          {messages.dashboard.analyticsTab}
        </TabsTrigger>
        <TabsTrigger value="routing" className="rounded-lg text-sm font-medium">
          {messages.dashboard.routingTab}
        </TabsTrigger>
      </TabsList>
    </Tabs>
  );
}
