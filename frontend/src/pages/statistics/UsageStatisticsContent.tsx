import { UsageStatisticsPageSkeleton } from "./UsageStatisticsPageSkeleton";
import { UsageControlsBar } from "./sections/UsageControlsBar";
import { UsageErrorBanner } from "./sections/UsageErrorBanner";
import { UsageOverviewSection } from "./sections/UsageOverviewSection";
import { UsageModelLineSelectorSection } from "./sections/UsageModelLineSelectorSection";
import { UsageTablesSection } from "./sections/UsageTablesSection";
import { UsageTrendsSection } from "./sections/UsageTrendsSection";
import { UsageBreakdownSection } from "./sections/UsageBreakdownSection";
import type { UsageStatisticsPageDataResult } from "./useUsageStatisticsPageData";
import type { UsageStatisticsPageActions } from "./useUsageStatisticsPageState";

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

interface UsageStatisticsContentProps {
  data: UsageStatisticsPageDataResult;
  state: UsageStatisticsPageActions;
}

export function UsageStatisticsContent({ data, state }: UsageStatisticsContentProps) {
  const snapshot = data.snapshot;

  return (
    <div className="flex flex-col gap-[var(--density-page-gap)]">
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
        <div className="flex flex-col gap-[var(--density-page-gap)]">
          <UsageOverviewSection
            costSummary={data.costSummary ?? snapshot.cost_overview}
            currency={snapshot.currency}
            overview={data.overview ?? snapshot.overview}
            requestTrendSeries={data.requestTrendSeries}
            tokenUsageTrendSeries={data.tokenUsageTrendSeries}
          />

          <UsageModelLineSelectorSection
            availableModelLineIds={data.availableModelLineIds}
            onSetSelectedModelLines={state.setSelectedModelLines}
            selectedModelLineIds={data.selectedModelLineIds}
          />

          <UsageTrendsSection
            chartGranularity={{
              latencyTrends: state.state.chartGranularity.latencyTrends,
              requestTrends: state.state.chartGranularity.requestTrends,
              tokenUsageTrends: state.state.chartGranularity.tokenUsageTrends,
            }}
            latencyTrendSeries={data.latencyTrendSeries}
            onSetChartGranularity={state.setChartGranularity}
            requestTrendSeries={data.requestTrendSeries}
            tokenUsageTrendSeries={data.tokenUsageTrendSeries}
          />

          <UsageBreakdownSection
            chartGranularity={{
              costOverview: state.state.chartGranularity.costOverview,
              tokenTypeBreakdown: state.state.chartGranularity.tokenTypeBreakdown,
            }}
            costSummary={data.costSummary ?? snapshot.cost_overview}
            costOverviewSeries={data.costOverviewSeries}
            currency={snapshot.currency}
            endpointStatistics={snapshot.endpoint_statistics}
            modelStatistics={data.modelStatistics}
            onSetChartGranularity={state.setChartGranularity}
            tokenTypeBreakdown={data.tokenTypeBreakdown}
          />

          <UsageTablesSection
            currency={snapshot.currency}
            endpointModelStatisticsByEndpointId={data.endpointModelStatisticsByEndpointId}
            endpointModelStatisticsErrors={data.endpointModelStatisticsErrors}
            endpointModelStatisticsLoading={data.endpointModelStatisticsLoading}
            endpointStatistics={snapshot.endpoint_statistics}
            modelStatistics={data.modelStatistics}
            onLoadEndpointModelStatistics={data.loadEndpointModelStatistics}
            proxyApiKeyStatistics={snapshot.proxy_api_key_statistics}
            tableResetKey={String(data.endpointModelStatisticsScopeKey)}
          />
        </div>
      ) : null}
    </div>
  );
}
