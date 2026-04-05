import { useState } from "react";
import { Activity, RefreshCw } from "lucide-react";
import { LoadbalanceEventDetailSheet } from "@/components/loadbalance/LoadbalanceEventDetailSheet";
import { LoadbalanceEventsTable } from "@/components/loadbalance/LoadbalanceEventsTable";
import { EmptyState } from "@/components/EmptyState";
import { Button } from "@/components/ui/button";
import { useProfileContext } from "@/context/ProfileContext";
import { useLocale } from "@/i18n/useLocale";
import { useModelLoadbalanceEvents } from "./useModelLoadbalanceEvents";

interface LoadbalanceEventsTabProps {
  modelId: string;
}

export function LoadbalanceEventsTab({ modelId }: LoadbalanceEventsTabProps) {
  const { revision } = useProfileContext();
  return <LoadbalanceEventsTabContent key={`${modelId}:${revision}`} modelId={modelId} revision={revision} />;
}

interface LoadbalanceEventsTabContentProps {
  modelId: string;
  revision: number;
}

function LoadbalanceEventsTabContent({ modelId, revision }: LoadbalanceEventsTabContentProps) {
  const { formatNumber, messages } = useLocale();
  const [selectedEventId, setSelectedEventId] = useState<number | null>(null);
  const copy = messages.loadbalanceEvents;
  const {
    events,
    loading,
    total,
    offset,
    limit,
    refresh,
    goToPreviousPage,
    goToNextPage,
  } = useModelLoadbalanceEvents(modelId, revision);

  return (
    <>
      <div className="space-y-4">
        <div className="flex flex-col gap-3 rounded-xl border bg-card p-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <h2 className="text-base font-semibold">
              {copy.tabTitle}
              <span className="ml-2 text-xs font-normal text-muted-foreground">({formatNumber(total)})</span>
            </h2>
            <p className="mt-1 text-xs text-muted-foreground">
              {copy.tabDescription}
            </p>
          </div>

          <Button
            type="button"
            size="icon"
            variant="outline"
            onClick={() => void refresh()}
            disabled={loading}
            aria-label={copy.refresh}
            title={copy.refresh}
          >
            <RefreshCw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
          </Button>
        </div>

        {!loading && total === 0 ? (
          <div className="rounded-xl border bg-card px-4">
            <EmptyState
              icon={<Activity className="h-6 w-6" />}
              title={copy.emptyTitle}
              description={copy.emptyDescription}
            />
          </div>
        ) : (
          <LoadbalanceEventsTable
            events={events}
            loading={loading}
            total={total}
            offset={offset}
            limit={limit}
            onSelectEvent={setSelectedEventId}
            onPreviousPage={goToPreviousPage}
            onNextPage={goToNextPage}
          />
        )}
      </div>

      <LoadbalanceEventDetailSheet
        eventId={selectedEventId}
        onClose={() => setSelectedEventId(null)}
      />
    </>
  );
}
