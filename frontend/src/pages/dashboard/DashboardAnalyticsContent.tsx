import { useProfileContext } from "@/context/ProfileContext";
import { UsageStatisticsContent } from "@/pages/statistics/UsageStatisticsContent";
import { useUsageStatisticsPageData } from "@/pages/statistics/useUsageStatisticsPageData";
import { useUsageStatisticsPageState } from "@/pages/statistics/useUsageStatisticsPageState";

export function DashboardAnalyticsContent() {
  const { revision, selectedProfile } = useProfileContext();
  const state = useUsageStatisticsPageState();
  const data = useUsageStatisticsPageData({
    revision,
    selectedProfileId: selectedProfile?.id ?? null,
    state: state.state,
  });

  return (
    <div className="flex flex-col gap-[var(--density-page-gap)]">
      <UsageStatisticsContent data={data} state={state} />
    </div>
  );
}
