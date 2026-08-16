import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import type { DashboardNowResponse, UsageSummaryResponse } from "@/lib/api/observability";
import type { FragmentState } from "@/features/observe/useObserveFragments";
import {
  OperatorClippedBadge,
  OperatorFreshnessBar,
  OperatorMissingValue,
  OperatorStalenessBadge,
  OperatorTypeBadge,
} from "@/shared/design-system";

/**
 * The dashboard's answer to "when is this from". A failed refresh keeps the
 * previous numbers and says so here rather than repainting them as fresh, and
 * the backend's own cache lag is drawn instead of being hidden.
 */
export function ObserveFreshnessBar({
  basis,
  nowFragment,
  onRefresh,
  refreshing,
  summaryFragment,
}: {
  basis: string;
  nowFragment: FragmentState<DashboardNowResponse>;
  onRefresh: () => void;
  refreshing: boolean;
  summaryFragment: FragmentState<UsageSummaryResponse>;
}) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.freshness;

  const generatedAt = nowFragment.data?.generated_at ?? summaryFragment.data?.generated_at ?? null;
  const health = nowFragment.data?.health;
  const stale = nowFragment.stale || summaryFragment.stale;
  const staleReason = nowFragment.error ?? summaryFragment.error ?? undefined;
  const coverageIncomplete = summaryFragment.data?.coverage.complete === false;

  return (
    <OperatorFreshnessBar
      data-testid="observe-freshness-bar"
      updatedAt={
        generatedAt ? (
          copy.updatedAt(format(generatedAt, { hour: "2-digit", minute: "2-digit", second: "2-digit" }))
        ) : (
          <OperatorMissingValue reason={copy.neverLoaded} />
        )
      }
      basis={basis}
      refresh={{ label: copy.refresh, onRefresh, pending: refreshing }}
      badges={
        <>
          {stale && generatedAt ? (
            <OperatorStalenessBadge
              label={messages.honesty.lastSuccessful(
                format(generatedAt, { hour: "2-digit", minute: "2-digit", second: "2-digit" }),
              )}
              reason={staleReason}
            />
          ) : null}
          {health?.cache_lag_ms != null && health.cache_lag_ms > 0 ? (
            <OperatorTypeBadge
              data-testid="cache-lag-badge"
              intent={health.stale ? "degraded" : "muted"}
              label={`◐ ${copy.cacheLag((health.cache_lag_ms / 1000).toFixed(1))}`}
              preserveLabel
              title={copy.cacheLagReason}
              className="font-normal"
            />
          ) : null}
          {coverageIncomplete ? (
            <OperatorClippedBadge
              label={messages.honesty.outsideRetention}
              reason={messages.honesty.outsideRetentionReason}
            />
          ) : null}
        </>
      }
    />
  );
}
