import { useLocale } from "@/i18n/useLocale";
import { GlobalCurrentStatePresentation } from "./GlobalCurrentStatePresentation";
import { LoadbalanceEventsFragment } from "./LoadbalanceEventsFragment";
import {
  type RoutingHealthSearch,
  type RoutingHealthSearchUpdater,
} from "./routingHealthSearch";
import { useRoutingHealthCurrentStateRead } from "./useRoutingHealthCurrentStateRead";
import { useRoutingHealthCurrentStateReset } from "./useRoutingHealthCurrentStateReset";

export type { RoutingHealthSearchUpdater } from "./routingHealthSearch";

/**
 * Compose the independent global current-state and routing-event cards. The
 * cards keep their own read, pagination, reset, context, and selection state.
 */
export function RoutingHealthTab({
  onSearchChange,
  search,
}: {
  search: RoutingHealthSearch;
  onSearchChange: RoutingHealthSearchUpdater;
}) {
  const { messages } = useLocale();
  const currentStateRead = useRoutingHealthCurrentStateRead({
    loadFailedMessage: messages.routingHealth.loadFailed,
    onSearchChange,
    search,
  });
  const currentStateReset = useRoutingHealthCurrentStateReset({
    applyResetSnapshot: currentStateRead.applyResetSnapshot,
    load: currentStateRead.load,
    resetFailedMessage: messages.routingHealth.resetFailed,
    resetNothingToClearMessage: messages.routingHealth.resetNothingToClear,
  });

  return (
    <div
      className="flex flex-col gap-[var(--density-page-gap)]"
      data-testid="routing-health-tab"
    >
      <GlobalCurrentStatePresentation
        read={currentStateRead}
        reset={currentStateReset}
      />
      <LoadbalanceEventsFragment
        search={search}
        onSearchChange={onSearchChange}
      />
      <p className="text-xs text-muted-foreground">
        {messages.routingHealth.sourceBoundaryNote}
      </p>
    </div>
  );
}
