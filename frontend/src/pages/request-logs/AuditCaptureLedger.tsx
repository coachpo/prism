import { useLocale } from "@/i18n/useLocale";
import { OperatorClippedBadge, OperatorMissingValue } from "@/shared/design-system";

function presentBytes(value: number | null | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

/**
 * What the gateway saw, what it kept, and what it dropped.
 *
 * The audit v2 API exposes per-body byte metadata (`*_bytes_observed` /
 * `*_bytes_stored`), a truncation flag, and a capture status. The strip renders
 * under each payload title so the ledger sits next to the thing it describes:
 * without it a truncated payload reads as the whole payload, and an
 * ingress-budget omission reads as having been captured at all.
 */
export function AuditCaptureLedger({
  bytesObserved,
  bytesStored,
  captureStatus,
  truncated,
}: {
  bytesObserved: number | null | undefined;
  bytesStored: number | null | undefined;
  captureStatus: string | null | undefined;
  truncated: boolean;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.requestLogs;
  const observed = presentBytes(bytesObserved);
  const stored = presentBytes(bytesStored);
  const dropped = truncated && observed !== null && stored !== null ? observed - stored : null;
  const omittedByBudget = captureStatus === "omitted_ingress_budget";

  const renderCount = (value: number | null) => (
    value === null ? <OperatorMissingValue reason={messages.honesty.noValue} /> : formatNumber(value)
  );

  return (
    // 截断字节数是「这段 body 不是全部」的唯一量化证据，11px 只属于 Label 角色、
    // 不承载数值：整块提到 Caption 下限的 12px。
    <div
      data-testid="audit-capture-ledger"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-border bg-inset px-2 py-1 text-xs text-muted-foreground"
    >
      <span>
        {copy.captureObserved}
        <span className="ml-1 font-mono tabular-nums text-foreground">{renderCount(observed)}</span>
      </span>
      <span>
        {copy.captureStored}
        <span className="ml-1 font-mono tabular-nums text-foreground">{renderCount(stored)}</span>
      </span>
      {dropped !== null && dropped > 0 ? (
        <span>
          {copy.captureTruncated}
          <span className="ml-1 font-mono tabular-nums text-degraded">{formatNumber(dropped)}</span>
        </span>
      ) : null}
      {dropped !== null && dropped > 0 ? (
        <OperatorClippedBadge
          label={copy.payloadTruncated}
          reason={copy.payloadTruncatedReason(formatNumber(dropped))}
        />
      ) : null}
      {omittedByBudget ? (
        <OperatorClippedBadge label={copy.captureOmittedBudget} />
      ) : null}
    </div>
  );
}