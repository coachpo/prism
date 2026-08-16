import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { OperatorFreshnessBar, OperatorMissingValue } from "@/shared/design-system";
import { deriveRequestLogAuditWindow } from "./requestLogAuditWindow";

/**
 * This page does not poll a time window, so a freshness bar in the usual sense
 * would say nothing. What does need permanent disclosure is the query bound
 * the frontend picks on its own: audit records are fetched for the request
 * time plus or minus twelve hours. Anything outside that is simply not
 * queried, and an operator who does not know that will read an empty table as
 * "no audit records exist".
 */
export function RequestLogAuditWindowBar({ requestCreatedAt }: { requestCreatedAt: string }) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.requestLogs;
  const window = deriveRequestLogAuditWindow(requestCreatedAt);

  return (
    <OperatorFreshnessBar
      data-testid="audit-window-bar"
      updatedAt={
        window ? (
          `${copy.auditWindowLabel} ${copy.auditWindowRange(format(window.from), format(window.to))}`
        ) : (
          <OperatorMissingValue reason={copy.auditWindowUnavailable} />
        )
      }
      basis={copy.auditWindowBasis}
    />
  );
}
