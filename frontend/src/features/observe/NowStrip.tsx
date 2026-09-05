import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import type { DashboardNowResponse } from "@/lib/api/observability";
import type { FragmentState } from "@/features/observe/useObserveFragments";
import {
  OperatorErrorState,
  OperatorMetricTile,
  OperatorMissingValue,
  OperatorRetryButton,
} from "@/shared/design-system";

/**
 * "Right now" is a different basis from the page window, so it states its own
 * basis and never inherits the preset.
 */
export function NowStrip({
  fragment,
  onRetry,
}: {
  fragment: FragmentState<DashboardNowResponse>;
  onRetry?: () => void;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;

  if (fragment.phase === "loading" && fragment.data === null) {
    return (
      <section aria-busy="true" aria-label={copy.nowLabel} className="grid grid-cols-4 gap-[var(--density-card-gap)]">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} className="h-16 rounded-md" />
        ))}
      </section>
    );
  }

  if (fragment.data === null) {
    return (
      <OperatorErrorState
        testId="now-strip-error"
        title={copy.nowUnavailable}
        description={messages.honesty.readFailedDescription}
        details={fragment.error}
        detailsLabel={messages.honesty.viewDetails}
        action={onRetry ? <OperatorRetryButton onClick={onRetry}>{copy.retry}</OperatorRetryButton> : undefined}
      />
    );
  }

  const rolling = fragment.data.rolling;

  return (
    <section
      aria-label={copy.nowLabel}
      data-testid="now-strip"
      className="grid grid-cols-4 gap-[var(--density-card-gap)]"
    >
      <OperatorMetricTile
        label={copy.currentRpm}
        value={
          <Rate
            value={rolling.rpm}
            digits={2}
            reason={copy.readMissingField}
          />
        }
        detail={`${formatNumber(rolling.window_minutes)} 分钟`}
      />
      <OperatorMetricTile
        label={copy.currentTpm}
        // 同一时间窗、同样零流量，RPM 渲染 0 而 TPM 渲染缺席，是把真实的零
        // 画成了缺席。窗口内没有任何请求时，每分钟令牌数就是零。
        value={
          rolling.tpm === null && rolling.request_count === 0 ? (
            formatNumber(0, {
              minimumFractionDigits: 0,
              maximumFractionDigits: 0,
            })
          ) : (
            <Rate
              value={rolling.tpm}
              digits={0}
              reason={copy.nowTpmNoTokenSample}
            />
          )
        }
        detail={
          rolling.token_coverage_complete
            ? `${formatNumber(rolling.token_sample_count)} ${copy.samples}`
            : messages.honesty.coverageIncomplete
        }
      />
      <OperatorMetricTile
        label={copy.rollingRequests}
        value={formatNumber(rolling.request_count)}
        detail={`${formatNumber(rolling.window_minutes)} 分钟`}
      />
      <OperatorMetricTile
        label={copy.enabledModels}
        value={formatNumber(fragment.data.enabled_model_count)}
        detail={copy.enabledModelsBasis}
      />
    </section>
  );
}

function Rate({
  value,
  digits,
  reason,
}: {
  value: number | null | undefined;
  digits: number;
  reason: string;
}) {
  const { formatNumber } = useLocale();
  if (value === null || value === undefined || Number.isNaN(value)) {
    return <OperatorMissingValue reason={reason} />;
  }
  return <>{formatNumber(value, { minimumFractionDigits: digits, maximumFractionDigits: digits })}</>;
}
