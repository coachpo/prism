import { PageHeader } from "@/components/PageHeader";
import { useProfileContext } from "@/context/ProfileContext";
import { useLocale } from "@/i18n/useLocale";
import { UsageStatisticsContent } from "./statistics/UsageStatisticsContent";
import { useUsageStatisticsPageData } from "./statistics/useUsageStatisticsPageData";
import { useUsageStatisticsPageState } from "./statistics/useUsageStatisticsPageState";

export function StatisticsPage() {
  const { revision, selectedProfile } = useProfileContext();
  const { messages } = useLocale();
  const state = useUsageStatisticsPageState();
  const data = useUsageStatisticsPageData({
    revision,
    selectedProfileId: selectedProfile?.id ?? null,
    state: state.state,
  });

  return (
    <div className="flex flex-col gap-5">
      <PageHeader
        description={messages.statistics.statisticsDescription}
        title={messages.statistics.statisticsTitle}
      />

      <UsageStatisticsContent data={data} state={state} />
    </div>
  );
}
