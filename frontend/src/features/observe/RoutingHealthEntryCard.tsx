import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { OperatorSectionCard } from "@/shared/design-system";

/**
 * Routing health is a page of its own now. The dashboard keeps a triage entry
 * here, carrying the best-effort caveat so nobody reads the event ledger as
 * complete before they open it.
 */
export function RoutingHealthEntryCard() {
  const { messages } = useLocale();
  const copy = messages.observe;

  return (
    <OperatorSectionCard
      data-testid="routing-health-entry"
      title={copy.routingHealthEntryTitle}
      description={copy.routingHealthEntryDescription}
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to="/observe/routing-health">
            {copy.routingHealthEntryAction}
            <ArrowRight data-icon="inline-end" />
          </Link>
        </Button>
      }
    >
      <p className="text-xs text-muted-foreground">{copy.routingHealthBestEffortNote}</p>
    </OperatorSectionCard>
  );
}
