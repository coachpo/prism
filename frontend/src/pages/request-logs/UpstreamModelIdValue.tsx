import { useLocale } from "@/i18n/useLocale";
import { cn } from "@/lib/utils";
import { OperatorMissingValue } from "@/shared/design-system";

/** Renders a retained upstream identity without inferring a missing snapshot. */
export function UpstreamModelIdValue({
  value,
  missingReason,
  showLabel = false,
  className,
  testId,
}: {
  value: string | null;
  missingReason: string;
  showLabel?: boolean;
  className?: string;
  testId?: string;
}) {
  const { messages } = useLocale();
  return (
    <span
      className={cn("font-mono", className)}
      data-testid={testId}
      title={value ?? undefined}
    >
      {showLabel ? `${messages.requestLogs.upstreamModelIdColumn}: ` : null}
      {value ?? <OperatorMissingValue reason={missingReason} />}
    </span>
  );
}
