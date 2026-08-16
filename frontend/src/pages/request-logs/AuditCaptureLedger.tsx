import { useLocale } from "@/i18n/useLocale";
import { OperatorClippedBadge } from "@/shared/design-system";

/**
 * What the gateway saw, what it kept, and what it dropped.
 *
 * The backend has always returned these byte counts and the reason it stopped
 * capturing; without them on screen a truncated payload reads as the whole
 * payload. The strip renders under each payload title so the ledger sits next
 * to the thing it describes.
 */
export function AuditCaptureLedger({
  limitReason,
  observed,
  stored,
  truncated,
}: {
  limitReason: string | null | undefined;
  observed: number;
  stored: number;
  truncated: number;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.requestLogs;

  return (
    <div
      data-testid="audit-capture-ledger"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-border bg-inset px-2 py-1 text-[11px] text-muted-foreground"
    >
      <span>
        {copy.captureObserved}
        <span className="ml-1 font-mono tabular-nums text-foreground">{formatNumber(observed)}</span>
      </span>
      <span>
        {copy.captureStored}
        <span className="ml-1 font-mono tabular-nums text-foreground">{formatNumber(stored)}</span>
      </span>
      <span>
        {copy.captureTruncated}
        <span
          className={`ml-1 font-mono tabular-nums ${truncated > 0 ? "text-degraded" : "text-foreground"}`}
        >
          {formatNumber(truncated)}
        </span>
      </span>
      {limitReason ? (
        <span className="font-mono">{copy.captureLimitReason(limitReason)}</span>
      ) : null}
      {truncated > 0 ? (
        <OperatorClippedBadge
          label={copy.payloadTruncated}
          reason={copy.payloadTruncatedReason(formatNumber(truncated))}
        />
      ) : null}
    </div>
  );
}
