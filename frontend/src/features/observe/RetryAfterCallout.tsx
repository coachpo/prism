import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout } from "@/shared/design-system";

/**
 * Shared Retry-After guidance: rendered by Observe fragments when the backend
 * replied 503 with a Retry-After header (admission/overload protection). The
 * parsed milliseconds come from ApiError.retryAfterMs.
 */
export function RetryAfterCallout({ retryAfterMs }: { retryAfterMs: number }) {
  const { messages } = useLocale();
  const seconds = Math.max(1, Math.round(retryAfterMs / 1000));
  return (
    <OperatorCallout intent="warning" data-testid="retry-after-callout">
      {messages.observe.retryAfterNotice(seconds)}
    </OperatorCallout>
  );
}
