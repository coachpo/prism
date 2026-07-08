import { UsageStatisticsContent } from "@/pages/statistics/UsageStatisticsContent";
import { useUsageStatisticsPageData } from "@/pages/statistics/useUsageStatisticsPageData";
import { useUsageStatisticsPageState } from "@/pages/statistics/useUsageStatisticsPageState";

export function DashboardAnalyticsContent() {
  const state = useUsageStatisticsPageState();
  const data = useUsageStatisticsPageData({
    revision: 0,
    selectedProfileId: 1,
    state: state.state,
  });

  return (
    <div className="flex flex-col gap-[var(--density-page-gap)]">
      <UsageStatisticsContent data={data} state={state} />
    </div>
  );
}
