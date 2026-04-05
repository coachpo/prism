import { PageHeader } from "@/components/PageHeader";
import { useProfileContext } from "@/context/ProfileContext";
import { useLocale } from "@/i18n/useLocale";
import { UsageStatisticsPageSkeleton } from "./statistics/UsageStatisticsPageSkeleton";
import { UsageControlsBar } from "./statistics/sections/UsageControlsBar";
import { UsageErrorBanner } from "./statistics/sections/UsageErrorBanner";
import { UsageOverviewSection } from "./statistics/sections/UsageOverviewSection";
import { UsageModelLineSelectorSection } from "./statistics/sections/UsageModelLineSelectorSection";
import { UsageServiceHealthSection } from "./statistics/sections/UsageServiceHealthSection";
import { UsageTablesSection } from "./statistics/sections/UsageTablesSection";
import { UsageTrendsSection } from "./statistics/sections/UsageTrendsSection";
import { UsageBreakdownSection } from "./statistics/sections/UsageBreakdownSection";
import { useUsageStatisticsPageData } from "./statistics/useUsageStatisticsPageData";
import { useUsageStatisticsPageState } from "./statistics/useUsageStatisticsPageState";

function downloadSnapshotJson(snapshot: unknown) {
  const blob = new Blob([JSON.stringify(snapshot, null, 2)], {
    type: "application/json",
  });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = "prism-usage-snapshot.json";
  anchor.click();
  URL.revokeObjectURL(url);
}

export function StatisticsPage() {
  const { revision, selectedProfile } = useProfileContext();
  const { messages } = useLocale();
  const state = useUsageStatisticsPageState();
  const data = useUsageStatisticsPageData({
    revision,
    selectedProfileId: selectedProfile?.id ?? null,
    state: state.state,
  });

  const snapshot = data.snapshot;

  return (
    <div className="space-y-5">
      <PageHeader
        description={messages.statistics.statisticsDescription}
        title={messages.statistics.statisticsTitle}
      />

      <UsageControlsBar
        generatedAt={snapshot?.generated_at ?? null}
        loading={data.loading}
        onExportSnapshot={() => {
          if (!snapshot) {
            return;
          }
          downloadSnapshotJson(snapshot);
        }}
        onRefresh={() => {
          void data.refresh();
        }}
        onSelectTimeRange={state.setSelectedTimeRange}
        selectedTimeRange={state.state.selectedTimeRange}
      />

      {data.loading && snapshot === null ? <UsageStatisticsPageSkeleton /> : null}

      {data.error ? <UsageErrorBanner error={data.error} /> : null}

      {snapshot ? (
        <div className="space-y-6">
          <UsageOverviewSection
            currency={snapshot.currency}
            overview={snapshot.overview}
            requestTrendSeries={data.requestTrendSeries}
            tokenUsageTrendSeries={data.tokenUsageTrendSeries}
          />

          <UsageModelLineSelectorSection
            availableModelLineIds={data.availableModelLineIds}
            onSetSelectedModelLines={state.setSelectedModelLines}
            selectedModelLineIds={data.selectedModelLineIds}
          />

          <UsageServiceHealthSection serviceHealth={snapshot.service_health} />

          <UsageTrendsSection
            chartGranularity={{
              requestTrends: state.state.chartGranularity.requestTrends,
              tokenUsageTrends: state.state.chartGranularity.tokenUsageTrends,
            }}
            onSetChartGranularity={state.setChartGranularity}
            requestTrendSeries={data.requestTrendSeries}
            tokenUsageTrendSeries={data.tokenUsageTrendSeries}
          />

          <UsageBreakdownSection
            chartGranularity={{
              costOverview: state.state.chartGranularity.costOverview,
              tokenTypeBreakdown: state.state.chartGranularity.tokenTypeBreakdown,
            }}
            costOverview={snapshot.cost_overview}
            costOverviewSeries={data.costOverviewSeries}
            currency={snapshot.currency}
            endpointStatistics={snapshot.endpoint_statistics}
            modelStatistics={snapshot.model_statistics}
            onSetChartGranularity={state.setChartGranularity}
            tokenTypeBreakdown={data.tokenTypeBreakdown}
          />

          <UsageTablesSection
            currency={snapshot.currency}
            endpointModelStatisticsByEndpointId={data.endpointModelStatisticsByEndpointId}
            endpointModelStatisticsErrors={data.endpointModelStatisticsErrors}
            endpointModelStatisticsLoading={data.endpointModelStatisticsLoading}
            endpointStatistics={snapshot.endpoint_statistics}
            modelStatistics={snapshot.model_statistics}
            onLoadEndpointModelStatistics={data.loadEndpointModelStatistics}
            proxyApiKeyStatistics={snapshot.proxy_api_key_statistics}
            tableResetKey={String(data.endpointModelStatisticsScopeKey)}
          />
        </div>
      ) : null}
    </div>
  );
}
