import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { OperatorCallout, OperatorRetryButton, OperatorStalenessBadge } from "@/shared/design-system";
import type { PiCatalogWire } from "@/lib/types";

export function CatalogReadFeedback({ catalog, onRetry, pending }: {
  catalog: PiCatalogWire;
  onRetry: () => void;
  pending: boolean;
}) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.clientReadiness;
  if (catalog.status === "fresh") return null;
  const reason = (copy.failures as Record<string, string>)[catalog.failure_code ?? "unavailable"] ?? copy.failures.unavailable;
  return (
    <OperatorCallout intent="warning" action={<OperatorRetryButton onClick={onRetry} disabled={pending}>{messages.common.retry}</OperatorRetryButton>}>
      <p>{reason}</p>
      {catalog.status === "stale" ? (
        <OperatorStalenessBadge label={copy.lastGood} reason={catalog.checked_at ? messages.honesty.lastSuccessful(format(catalog.checked_at)) : undefined} />
      ) : <p>{copy.noCatalog}</p>}
    </OperatorCallout>
  );
}
