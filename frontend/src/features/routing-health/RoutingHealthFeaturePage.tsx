import { useCallback } from "react";
import { useNavigate, useSearch } from "@tanstack/react-router";

import { routingHealthRoute } from "@/app/router/appRouter";
import { RoutingHealthTab } from "@/features/observe/RoutingHealthTab";
import { useLocale } from "@/i18n/useLocale";
import { OperatorPageHeader } from "@/shared/design-system";

/**
 * Routing health is a triage entry point, so it is a page rather than the
 * dashboard's third tab. The H1 is fixed: switching a filter here never
 * changes what the page claims to be.
 */
export function RoutingHealthFeaturePage() {
  const { messages } = useLocale();
  const navigate = useNavigate();
  const search = useSearch({ from: routingHealthRoute.id });

  const onSearchChange = useCallback(
    (patch: Record<string, unknown>, replace = false) => {
      void navigate({
        to: "/observe/routing-health",
        search: { ...search, ...patch } as never,
        replace,
      });
    },
    [navigate, search],
  );

  return (
    <div data-testid="routing-health-page" className="flex flex-col gap-[var(--density-page-gap)]">
      <OperatorPageHeader
        title={messages.dashboard.routingHealthTitle}
        description={messages.dashboard.routingHealthDescription}
      />
      <RoutingHealthTab
        search={search as unknown as Record<string, unknown>}
        onSearchChange={onSearchChange}
      />
    </div>
  );
}

export default RoutingHealthFeaturePage;
