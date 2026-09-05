import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import { formatTimezoneOffset } from "@/lib/timezone";
import {
  OperatorClippedBadge,
  OperatorFreshnessBar,
  OperatorHelpHint,
  OperatorMissingValue,
} from "@/shared/design-system";
import type { QueryCoverage } from "./requestLogAuditLanes";
import { deriveRequestLogAuditWindow } from "./requestLogAuditWindow";

/**
 * The page's answer to "when is this from".
 *
 * Two facts share this row and must not be confused. `updatedAt` is the read
 * instant of the lanes below it — an audit row written after the request
 * finished only shows up on a re-read, so a list that looks empty must be
 * re-readable from here. `basis` carries the query bound the frontend picks on
 * its own (request time ±12 hours) plus the coverage the backend actually
 * served; anything outside that is simply not queried, and an operator who
 * does not know that reads an empty table as "no audit records exist".
 */
export function RequestLogAuditWindowBar({
  coverage,
  lastFetchedAt,
  onRefresh,
  refreshing,
  requestCreatedAt,
}: {
  coverage: QueryCoverage | null;
  lastFetchedAt: string | null;
  onRefresh: () => void;
  refreshing: boolean;
  requestCreatedAt: string;
}) {
  const { messages } = useLocale();
  const { format, timezone } = useTimezone();
  const copy = messages.requestLogs;
  const window = deriveRequestLogAuditWindow(requestCreatedAt);
  const retentionClipped = coverage
    ? coverage.gaps.length > 0 ||
      coverage.effective_from_time > coverage.requested_from_time
    : false;

  return (
    <OperatorFreshnessBar
      data-testid="audit-window-bar"
      updatedAt={
        lastFetchedAt ? (
          <>
            {/* 中文标签不进等宽：混排会撕开字形，只有时刻用 mono 对齐。 */}
            <span className="font-sans">{copy.updatedAtLabel}</span>
            {`${format(lastFetchedAt, {
              hour: "2-digit",
              minute: "2-digit",
              second: "2-digit",
            })} (${formatTimezoneOffset(timezone ?? "UTC")})`}
          </>
        ) : (
          <OperatorMissingValue reason={messages.freshness.neverLoaded} />
        )
      }
      refresh={{
        label: messages.freshness.refresh,
        onRefresh,
        pending: refreshing,
      }}
      basis={
        window ? (
          <span className="inline-flex min-w-0 items-center gap-1">
            <span>
              {`${copy.auditWindowLabel} ${copy.auditWindowRange(format(window.from), format(window.to))}`}
              {coverage
                ? ` · ${copy.auditCoverageEffective(
                    format(coverage.effective_from_time),
                    format(coverage.effective_to_time),
                  )}`
                : null}
            </span>
            <OperatorHelpHint label={copy.auditWindowBasis} />
          </span>
        ) : (
          copy.auditWindowUnavailable
        )
      }
      badges={
        <>
          {retentionClipped ? (
            <OperatorClippedBadge
              data-testid="audit-coverage-retention-badge"
              label={messages.honesty.outsideRetention}
              reason={messages.honesty.outsideRetentionReason}
            />
          ) : null}
          {coverage?.complete === false ? (
            <OperatorClippedBadge
              data-testid="audit-coverage-incomplete-badge"
              label={messages.honesty.coverageIncomplete}
              reason={messages.honesty.coverageIncompleteReason}
            />
          ) : null}
        </>
      }
    />
  );
}
