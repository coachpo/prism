import { useLocale } from "@/i18n/useLocale";
import { LoadbalanceEventsPresentation } from "./LoadbalanceEventsPresentation";
import {
  type RoutingHealthSearch,
  type RoutingHealthSearchUpdater,
} from "./routingHealthSearch";
import { useRoutingHealthEventPage } from "./useRoutingHealthEventPage";
import { useRoutingHealthQueryContext } from "./useRoutingHealthQueryContext";

/**
 * Compose the signed query-context, event-page, and event-row/presentation
 * owners. URL search remains the only cross-owner source of truth.
 */
export function LoadbalanceEventsFragment({
  search,
  onSearchChange,
}: {
  search: RoutingHealthSearch;
  onSearchChange: RoutingHealthSearchUpdater;
}) {
  const { messages } = useLocale();
  const context = useRoutingHealthQueryContext({
    loadFailedMessage: messages.routingHealth.loadFailed,
    onSearchChange,
    search,
  });
  const eventPage = useRoutingHealthEventPage({
    contextState: context.contextState,
    issueContext: context.issueContext,
    loadFailedMessage: messages.routingHealth.loadFailed,
    onSearchChange,
    search,
    windowKey: context.windowKey,
  });

  return (
    <LoadbalanceEventsPresentation
      contextState={context.contextState}
      eventPage={eventPage}
      preset={context.preset}
      search={search}
    />
  );
}
