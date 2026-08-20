import { useLocale } from "@/i18n/useLocale";
import type { UsageSummaryResponse } from "@/lib/api/observability";
import type { FragmentState } from "@/features/observe/useObserveFragments";
import {
  cacheBasisPartialCoverage,
  windowCacheReadShare,
} from "./cacheReadShare";
import {
  OperatorCallout,
  OperatorClippedBadge,
  OperatorErrorState,
  OperatorKpiCard,
  OperatorMissingValue,
  OperatorRetryButton,
} from "@/shared/design-system";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const PRICING_SEGMENT_CLASS = {
  priced: "bg-healthy",
  unpriced: "bg-degraded",
  ineligible: "bg-idle",
  unknown: "bg-failing",
} as const;

type PricingSegmentKey = keyof typeof PRICING_SEGMENT_CLASS;

export function WindowKpiGrid({
  fragment,
  onRetry,
}: {
  fragment: FragmentState<UsageSummaryResponse>;
  onRetry?: () => void;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;

  if (fragment.phase === "loading" && fragment.data === null) {
    return (
      <section
        aria-busy="true"
        aria-label={copy.windowLabel}
        className="grid grid-cols-7 gap-[var(--density-card-gap)]"
      >
        {Array.from({ length: 6 }, (_, index) => (
          <Skeleton
            key={index}
            className={cn(
              "h-[5.25rem] rounded-lg",
              index === 5 && "col-span-2",
            )}
          />
        ))}
      </section>
    );
  }

  // A failed read is never an empty grid of zeros.
  if (fragment.data === null) {
    return (
      <OperatorErrorState
        testId="window-kpi-error"
        title={copy.windowUnavailable}
        description={messages.honesty.readFailedDescription}
        details={fragment.error}
        detailsLabel={messages.honesty.viewDetails}
        action={
          onRetry ? (
            <OperatorRetryButton onClick={onRetry}>
              {copy.retry}
            </OperatorRetryButton>
          ) : undefined
        }
      />
    );
  }

  const data = fragment.data;
  const segment = data.cost_segments[0];
  const pricing = data.pricing_reconciliation;
  const symbol = segment?.display_symbol ?? "$";
  const coverageClipped = !data.coverage.complete;
  const cacheBasis = {
    requestCount: data.request_count,
    basisRequestCount: data.cache_basis_request_count,
    basisInputTokens: data.cache_basis_input_tokens,
    basisCacheReadTokens: data.cache_basis_cache_read_tokens,
    basisCacheCreationTokens: data.cache_basis_cache_creation_tokens,
  };
  const cacheShare = windowCacheReadShare(cacheBasis);
  const cacheBasisPartial = cacheBasisPartialCoverage(cacheBasis);
  // A measured basis (at least one eligible row) guarantees non-null sums;
  // the breakdown is never fabricated from zeros for a missing basis.
  const basisComponents =
    cacheBasis.basisRequestCount > 0 &&
    cacheBasis.basisInputTokens !== null &&
    cacheBasis.basisCacheReadTokens !== null &&
    cacheBasis.basisCacheCreationTokens !== null
      ? {
          read: cacheBasis.basisCacheReadTokens,
          creation: cacheBasis.basisCacheCreationTokens,
          uncached: cacheBasis.basisInputTokens,
        }
      : null;

  return (
    <div className="flex flex-col gap-[var(--density-card-gap)]">
      {coverageClipped && data.coverage.gaps.length > 0 ? (
        <OperatorCallout
          intent="warning"
          data-testid="window-coverage-incomplete"
        >
          {messages.routingHealth.coverageIncompleteDescription}
        </OperatorCallout>
      ) : null}
      <section
        aria-label={copy.windowLabel}
        data-testid="window-kpi-grid"
        className="grid grid-cols-7 gap-[var(--density-card-gap)]"
      >
        <OperatorKpiCard
          label={copy.requests}
          value={<Count value={data.request_count} />}
          detail={copy.windowBasis(data.coverage.requested_preset)}
          badges={
            coverageClipped ? (
              <OperatorClippedBadge
                label={messages.honesty.coverageIncomplete}
                reason={messages.honesty.coverageIncompleteReason}
              />
            ) : null
          }
        />
        <OperatorKpiCard
          label={copy.httpSuccessRate}
          value={
            data.http_success_rate === null ? (
              <OperatorMissingValue reason={messages.honesty.noValue} />
            ) : (
              `${data.http_success_rate.toFixed(1)}%`
            )
          }
          detail={`${formatNumber(data.http_success_count)} / ${formatNumber(data.request_count)}`}
        />
        <OperatorKpiCard
          label={copy.ttftP95}
          value={
            data.p95_ttft_ms === null ? (
              <OperatorMissingValue reason={messages.honesty.noValue} />
            ) : (
              `${formatNumber(data.p95_ttft_ms)} ms`
            )
          }
          detail={`${copy.samples}：${formatNumber(data.ttft_sample_count)}`}
        />
        <OperatorKpiCard
          label={copy.outputRate}
          value={
            data.avg_output_rate_tps === null ? (
              <OperatorMissingValue reason={messages.honesty.noValue} />
            ) : (
              `${data.avg_output_rate_tps.toFixed(1)} tok/s`
            )
          }
          detail={`${copy.samples}：${formatNumber(data.output_rate_sample_count)}`}
        />
        <OperatorKpiCard
          label={copy.cacheReadShare}
          value={
            cacheShare.kind === "value" ? (
              `${(cacheShare.share * 100).toFixed(1)}%`
            ) : cacheShare.kind === "empty_window" ? (
              <OperatorMissingValue reason={copy.cacheReadShareEmptyWindow} />
            ) : cacheShare.kind === "no_comparable_rows" ? (
              <OperatorMissingValue
                reason={copy.cacheReadShareNoComparable(data.request_count)}
              />
            ) : (
              <OperatorMissingValue reason={copy.cacheReadShareNoDenominator} />
            )
          }
          detail={copy.cacheReadShareDetail(
            data.cache_basis_request_count,
            data.request_count,
          )}
          badges={
            <>
              {basisComponents ? (
                <CacheBasisBreakdown {...basisComponents} />
              ) : null}
              {cacheBasisPartial ? (
                <OperatorClippedBadge
                  label={copy.cacheReadSharePartial}
                  reason={copy.cacheReadSharePartialReason}
                />
              ) : null}
              {coverageClipped ? (
                <OperatorClippedBadge
                  label={messages.honesty.coverageIncomplete}
                  reason={messages.honesty.coverageIncompleteReason}
                />
              ) : null}
            </>
          }
        />
        <OperatorKpiCard
          className="col-span-2"
          label={copy.cost}
          value={<Money micros={segment?.known_cost_micros} symbol={symbol} />}
          detail={copy.knownCostCaption(
            copy.coverageLabel(pricing.pricing_coverage_state),
          )}
          badges={
            <div className="flex flex-col gap-1">
              <PricingBreakdown pricing={pricing} />
              <span className="text-[11px] text-muted-foreground">
                {copy.pricingSelectorUnresolved}：
                <span className="font-mono text-foreground">
                  {formatNumber(data.pricing_selector_unresolved_count)}
                </span>
              </span>
            </div>
          }
        />
      </section>
    </div>
  );
}

function Count({ value }: { value: number | null | undefined }) {
  const { formatNumber, messages } = useLocale();
  if (value === null || value === undefined || Number.isNaN(value)) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  return <>{formatNumber(value)}</>;
}

function Money({
  micros,
  symbol,
}: {
  micros: string | null | undefined;
  symbol: string;
}) {
  const { messages } = useLocale();
  if (micros === null || micros === undefined) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  const amount = Number(micros) / 1_000_000;
  if (Number.isNaN(amount)) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  return (
    <>
      {symbol}
      {amount.toFixed(4)}
    </>
  );
}

/**
 * The three cache-basis components as one proportional bar plus a legend.
 * Colors come from the `--chart-N` series tokens (DESIGN.md: data-encoding
 * context), never from status colors. A component measured as zero still
 * appears in the legend as a real `0`. With no rows at all the segments are
 * empty and the legend reads the true zeros.
 */
function CacheBasisBreakdown({
  read,
  creation,
  uncached,
}: {
  read: number;
  creation: number;
  uncached: number;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;
  const segments = [
    {
      key: "read",
      value: read,
      label: copy.cacheReadShareRead,
      color: "var(--chart-1)",
    },
    {
      key: "creation",
      value: creation,
      label: copy.cacheReadShareCreation,
      color: "var(--chart-2)",
    },
    {
      key: "uncached",
      value: uncached,
      label: copy.cacheReadShareUncached,
      color: "var(--chart-3)",
    },
  ] as const;
  const total = read + creation + uncached;

  return (
    <div
      className="flex w-full min-w-0 flex-col gap-1"
      data-testid="cache-basis-breakdown"
    >
      <div
        role="img"
        aria-label={`${copy.cacheReadShare}：${segments.map((s) => `${s.label} ${formatNumber(s.value)}`).join("，")}`}
        className="flex h-1.5 w-full overflow-hidden rounded-full bg-inset"
      >
        {total > 0
          ? segments.map((segment) =>
              segment.value > 0 ? (
                <span
                  key={segment.key}
                  data-testid={`cache-basis-${segment.key}-segment`}
                  className="h-full"
                  style={{
                    width: `${(segment.value / total) * 100}%`,
                    background: segment.color,
                  }}
                />
              ) : null,
            )
          : null}
      </div>
      <div className="flex flex-wrap gap-x-2.5 gap-y-0.5 text-[11px] text-muted-foreground">
        {segments.map((segment) => (
          <span
            key={segment.key}
            className="inline-flex items-center gap-1"
            data-testid={`cache-basis-${segment.key}`}
          >
            <span
              aria-hidden="true"
              className="size-1.5 rounded-full"
              style={{ background: segment.color }}
            />
            {segment.label}
            <span className="font-mono tabular-nums text-foreground">
              {formatNumber(segment.value)}
            </span>
          </span>
        ))}
      </div>
    </div>
  );
}

/**
 * The four pricing states as one proportional bar plus a legend. The backend
 * enum keys never reach the screen, and a state with zero requests still
 * appears in the legend as a real `0` rather than vanishing.
 */
function PricingBreakdown({
  pricing,
}: {
  pricing: UsageSummaryResponse["pricing_reconciliation"];
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;
  const segments: { count: number; key: PricingSegmentKey; label: string }[] = [
    {
      count: pricing.priced_request_count,
      key: "priced",
      label: copy.pricingPriced,
    },
    {
      count: pricing.unpriced_request_count,
      key: "unpriced",
      label: copy.pricingUnpriced,
    },
    {
      count: pricing.pricing_ineligible_request_count,
      key: "ineligible",
      label: copy.pricingIneligible,
    },
    {
      count: pricing.pricing_unknown_request_count,
      key: "unknown",
      label: copy.pricingUnknown,
    },
  ];
  const total = segments.reduce((sum, segment) => sum + segment.count, 0);

  return (
    <div
      className="flex w-full min-w-0 flex-col gap-1"
      data-testid="pricing-breakdown"
    >
      <div
        role="img"
        aria-label={`${copy.pricingBreakdownLabel}：${segments.map((s) => `${s.label} ${formatNumber(s.count)}`).join("，")}`}
        className="flex h-1.5 w-full overflow-hidden rounded-full bg-inset"
      >
        {total > 0
          ? segments.map((segment) =>
              segment.count > 0 ? (
                <span
                  key={segment.key}
                  data-testid={`pricing-${segment.key}-segment`}
                  className={cn("h-full", PRICING_SEGMENT_CLASS[segment.key])}
                  style={{ width: `${(segment.count / total) * 100}%` }}
                />
              ) : null,
            )
          : null}
      </div>
      <div className="flex flex-wrap gap-x-2.5 gap-y-0.5 text-[11px] text-muted-foreground">
        {segments.map((segment) => (
          <span
            key={segment.key}
            data-testid={`pricing-${segment.key}`}
            className="inline-flex items-center gap-1"
          >
            <span
              aria-hidden="true"
              className={cn(
                "size-1.5 rounded-full",
                PRICING_SEGMENT_CLASS[segment.key],
              )}
            />
            {segment.label}
            <span className="font-mono tabular-nums text-foreground">
              {formatNumber(segment.count)}
            </span>
          </span>
        ))}
      </div>
    </div>
  );
}
