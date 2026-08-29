import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import type {
  ObserveCoverage,
  ObserveScope,
} from "@/lib/api/observability";
import { OperatorCallout } from "@/shared/design-system";

type ScopedCoverageNoticeKind =
  | "final_usage"
  | "final_latency"
  | "route_attempt";

type ScopedCoverageNotice = {
  kind: ScopedCoverageNoticeKind;
  coverage: ObserveCoverage;
};

/**
 * Coverage follows the dataset that owns each scoped claim. Final-execution
 * latency is joined from request logs, while its other aggregate facts come
 * from usage events; merging those lanes would overstate the impact of either
 * gap.
 */
function scopedCoverageNotices(
  scope: ObserveScope,
  usageCoverage: ObserveCoverage,
  requestCoverage: ObserveCoverage,
): ScopedCoverageNotice[] {
  if (scope === "final_execution") {
    const notices: ScopedCoverageNotice[] = [];
    if (!usageCoverage.complete) {
      notices.push({ kind: "final_usage", coverage: usageCoverage });
    }
    if (!requestCoverage.complete) {
      notices.push({ kind: "final_latency", coverage: requestCoverage });
    }
    return notices;
  }

  if (scope === "route_attempt" && !requestCoverage.complete) {
    return [{ kind: "route_attempt", coverage: requestCoverage }];
  }

  return [];
}

export function ObserveScopedCoverageWarnings({
  requestCoverage,
  scope,
  usageCoverage,
}: {
  requestCoverage: ObserveCoverage;
  scope: ObserveScope;
  usageCoverage: ObserveCoverage;
}) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.observe;
  const notices = scopedCoverageNotices(
    scope,
    usageCoverage,
    requestCoverage,
  );

  return notices.map((notice) => {
    const title =
      notice.kind === "final_usage"
        ? copy.finalUsageCoverageTitle
        : notice.kind === "final_latency"
          ? copy.finalLatencyCoverageTitle
          : copy.routeAttemptCoverageTitle;
    const description =
      notice.kind === "final_usage"
        ? copy.finalUsageCoverageDescription
        : notice.kind === "final_latency"
          ? copy.finalLatencyCoverageDescription
          : copy.routeAttemptCoverageDescription;

    return (
      <OperatorCallout
        key={notice.kind}
        data-testid={`${notice.kind}-coverage-warning`}
        intent="warning"
        title={title}
      >
        <div className="flex flex-col gap-2">
          <p>{description}</p>
          {notice.coverage.gaps.length > 0 ? (
            <div>
              <p className="text-xs font-medium text-muted-foreground">
                {copy.coverageGapDetailsTitle}
              </p>
              <ul className="mt-1 flex flex-col gap-1 text-xs text-muted-foreground">
                {notice.coverage.gaps.map((gap, index) => {
                  const from = format(gap.from_time);
                  const to = format(gap.to_time);
                  return (
                    <li key={`${gap.from_time}-${gap.to_time}-${gap.reason}-${index}`}>
                      {copy.coverageGapDetail(
                        from,
                        to,
                        copy.coverageGapReason(gap.reason),
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>
          ) : null}
        </div>
      </OperatorCallout>
    );
  });
}
