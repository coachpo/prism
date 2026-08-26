import { useLocale } from "@/i18n/useLocale";
import type { ObserveMetric } from "@/features/observe/observeSearch";
import {
  bucketCacheBasisPartialCoverage,
  bucketCacheReadShare,
  bucketOutputRate,
  bucketOutputRatePartialCoverage,
} from "@/features/observe/seriesMetricStates";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import {
  OperatorClippedBadge,
  OperatorMissingValue,
} from "@/shared/design-system";

type SeriesPoint = UsageSeriesResponse["series"][number]["points"][number];

export type ObservePointIndex = Map<string, Map<string, SeriesPoint>>;

type TooltipEntry = {
  color?: string;
  dataKey?: string | number;
  name?: string;
  value?: number | string | null;
};

export function ObserveSeriesTooltip({
  active,
  formatBucket,
  label,
  metric,
  payload,
  pointIndex,
}: {
  active?: boolean;
  formatBucket: (value: string) => string;
  label?: string;
  metric: ObserveMetric;
  payload?: TooltipEntry[];
  pointIndex: ObservePointIndex;
}) {
  if (!active || !label) return null;

  const visibleEntries = (payload ?? [])
    .map((entry) => {
      const point =
        typeof entry.dataKey === "string"
          ? pointIndex.get(label)?.get(entry.dataKey)
          : undefined;
      return { entry, point };
    })
    // filterNull=false exposes explicit nulls. Keep those only when the source
    // point exists; an entity absent from this bucket is not a missing sample.
    .filter(({ entry, point }) => point || entry.value !== null && entry.value !== undefined);

  if (visibleEntries.length === 0) return null;

  return (
    <div className="operator-overlay-surface min-w-56 rounded-lg border p-2 text-xs">
      <p className="mb-1 font-mono tabular-nums text-muted-foreground">
        {formatBucket(label)}
      </p>
      <ul className="flex flex-col gap-2">
        {visibleEntries.map(({ entry, point }, index) => (
          <li key={`${entry.name}-${index}`} className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <span
                aria-hidden="true"
                className="size-1.5 shrink-0 rounded-full"
                style={{ backgroundColor: entry.color }}
              />
              <span className="min-w-0 flex-1 truncate">{entry.name}</span>
              <TooltipMetricValue
                entryValue={entry.value}
                metric={metric}
                point={point}
              />
            </div>
            {point && metric === "output_rate" ? (
              <OutputRateDetails point={point} />
            ) : null}
            {point && metric === "cache_read_share" ? (
              <CacheReadDetails point={point} />
            ) : null}
          </li>
        ))}
      </ul>
    </div>
  );
}

function TooltipMetricValue({
  entryValue,
  metric,
  point,
}: {
  entryValue: number | string | null | undefined;
  metric: ObserveMetric;
  point: SeriesPoint | undefined;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;

  if (point && metric === "output_rate") {
    const rate = bucketOutputRate(point);
    if (rate.kind === "no_sample") {
      return <span title={copy.noSampleReason}>{copy.noSampleShort}</span>;
    }
    return <span className="font-mono tabular-nums">{`${rate.tps.toFixed(1)} ${copy.metricUnit(metric)}`}</span>;
  }

  if (point && metric === "cache_read_share") {
    const share = bucketCacheReadShare(point);
    if (share.kind === "no_comparable_rows") {
      return (
        <span title={copy.bucketNoComparableReason}>{copy.noComparableShort}</span>
      );
    }
    if (share.kind === "no_denominator") {
      return (
        <span title={copy.bucketNoDenominatorReason}>{copy.noDenominatorShort}</span>
      );
    }
    return <span className="font-mono tabular-nums">{`${(share.share * 100).toFixed(1)}%`}</span>;
  }

  if (entryValue === null || entryValue === undefined) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  const rendered =
    typeof entryValue === "number" ? formatNumber(entryValue) : String(entryValue);
  const unit = copy.metricUnit(metric);
  return (
    <span className="font-mono tabular-nums">
      {rendered}
      {unit ? ` ${unit}` : ""}
    </span>
  );
}

function OutputRateDetails({ point }: { point: SeriesPoint }) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;
  return (
    <div className="ml-3.5 flex flex-wrap items-center gap-1 text-muted-foreground">
      <span>
        {copy.outputRateSamplesHint(
          formatNumber(point.output_rate_sample_count),
          formatNumber(point.request_count),
        )}
      </span>
      {bucketOutputRatePartialCoverage(point) ? (
        <OperatorClippedBadge
          label={copy.partialCoverage}
          reason={copy.outputRatePartialReason}
        />
      ) : null}
    </div>
  );
}

function CacheReadDetails({ point }: { point: SeriesPoint }) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;
  const tokenUnit = copy.metricUnit("tokens");
  return (
    <div className="ml-3.5 flex flex-col gap-0.5 text-muted-foreground">
      <TooltipDetailRow
        label={copy.cacheReadShareRead}
        value={point.cache_basis_cache_read_tokens}
        unit={tokenUnit}
      />
      <TooltipDetailRow
        label={copy.cacheReadShareCreation}
        value={point.cache_basis_cache_creation_tokens}
        unit={tokenUnit}
      />
      <TooltipDetailRow
        label={copy.cacheReadShareUncached}
        value={point.cache_basis_input_tokens}
        unit={tokenUnit}
      />
      <div className="flex flex-wrap items-center justify-between gap-1">
        <span>
          {copy.cacheBasisCoverageHint(
            formatNumber(point.cache_basis_request_count),
            formatNumber(point.request_count),
          )}
        </span>
        {bucketCacheBasisPartialCoverage(point) ? (
          <OperatorClippedBadge
            label={copy.cacheReadSharePartial}
            reason={copy.cacheReadSharePartialReason}
          />
        ) : null}
      </div>
    </div>
  );
}

function TooltipDetailRow({
  label,
  unit,
  value,
}: {
  label: string;
  unit: string;
  value: number | null;
}) {
  const { formatNumber, messages } = useLocale();
  return (
    <div className="flex items-center justify-between gap-3">
      <span>{label}</span>
      {value === null ? (
        <OperatorMissingValue reason={messages.honesty.noValue} />
      ) : (
        <span className="font-mono tabular-nums">
          {formatNumber(value)} {unit}
        </span>
      )}
    </div>
  );
}
