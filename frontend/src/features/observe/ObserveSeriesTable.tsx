import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  isLatencyMetric,
  lastObservedBucket,
} from "@/features/observe/observeChartRows";
import type { ObserveMetric } from "@/features/observe/observeSearch";
import {
  bucketCacheBasisPartialCoverage,
  bucketCacheReadShare,
  bucketOutputRate,
  bucketOutputRatePartialCoverage,
} from "@/features/observe/seriesMetricStates";
import type { FragmentState } from "@/features/observe/useObserveFragments";
import { useLocale } from "@/i18n/useLocale";
import { formatMoneyMicros } from "@/lib/costing";
import type { UsageSeriesResponse } from "@/lib/api/observability";
import { getActiveReportingCurrency } from "@/lib/reportingCurrency";
import {
  OperatorClippedBadge,
  OperatorMissingValue,
} from "@/shared/design-system";

type SeriesPoint = UsageSeriesResponse["series"][number]["points"][number];

/**
 * Semantic table equivalent of the main chart. Additive metrics use window
 * totals; non-additive rate/share metrics use the explicitly labelled last
 * bucket and always expose their sample/comparable denominator.
 */
export function ObserveSeriesTable({
  formatBucket,
  fragment,
  metric,
}: {
  formatBucket: (value: string) => string;
  fragment: FragmentState<UsageSeriesResponse>;
  metric: ObserveMetric;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.observe;
  if (fragment.data === null || fragment.data.series.length === 0) return null;

  const items = fragment.data.series;
  const observationLabel =
    fragment.data.caliber.scope === "route_attempt"
      ? copy.metricName("attempts")
      : copy.requests;
  const lastBucketStart = lastObservedBucket(items);
  const lastBucketLabel = lastBucketStart
    ? copy.lastBucketColumn(formatBucket(lastBucketStart))
    : copy.lastBucketColumn("—");

  return (
    <div className="overflow-x-auto">
      <Table aria-label={copy.semanticTable}>
        <TableHeader>
          <TableRow>
            <TableHead>{copy.seriesLabel}</TableHead>
            <TableHead className="text-right">
              {copy.windowTotalColumn} · {observationLabel}
            </TableHead>
            {metric === "errors" ? (
              <TableHead className="text-right">
                {copy.windowTotalColumn} · {copy.errorCount}
              </TableHead>
            ) : null}
            {metric === "tokens" ? (
              <TableHead className="text-right">
                {copy.windowTotalColumn} · {copy.tokenCount}
              </TableHead>
            ) : null}
            {metric === "cost" ? (
              <TableHead className="text-right">
                {copy.windowTotalColumn} · {copy.cost}
              </TableHead>
            ) : null}
            {metric === "requests" ? (
              <>
                <TableHead className="text-right">
                  {lastBucketLabel} · {copy.httpSuccessShort}
                </TableHead>
                <TableHead className="text-right">
                  {lastBucketLabel} · {copy.httpFailedShort}
                </TableHead>
              </>
            ) : null}
            {isLatencyMetric(metric) ? (
              <>
                <TableHead className="text-right">
                  {lastBucketLabel} · P50
                </TableHead>
                <TableHead className="text-right">
                  {lastBucketLabel} · P95
                </TableHead>
              </>
            ) : null}
            {metric === "output_rate" ? (
              <>
                <TableHead className="text-right">
                  {lastBucketLabel} · {copy.outputRate}
                </TableHead>
                <TableHead className="text-right">
                  {lastBucketLabel} · {copy.samplesColumn}
                </TableHead>
              </>
            ) : null}
            {metric === "cache_read_share" ? (
              <>
                <TableHead className="text-right">
                  {lastBucketLabel} · {copy.cacheReadShare}
                </TableHead>
                <TableHead className="text-right">
                  {lastBucketLabel} · {copy.comparableColumn}
                </TableHead>
              </>
            ) : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {items.map((item) => {
            const lastPoint = item.points.find(
              (point) => point.bucket_start === lastBucketStart,
            );
            return (
              <TableRow key={item.key}>
                <TableCell>{item.label}</TableCell>
                <TableCell className="text-right font-mono tabular-nums">
                  {formatNumber(item.request_count)}
                </TableCell>
                {metric === "errors" ? (
                  <TableCell className="text-right font-mono tabular-nums">
                    <Cell
                      value={item.points.reduce(
                        (total, point) =>
                          total +
                          point.failed_count +
                          point.client_disconnected_count,
                        0,
                      )}
                    />
                  </TableCell>
                ) : null}
                {metric === "tokens" ? (
                  <TableCell className="text-right font-mono tabular-nums">
                    <Cell
                      value={sumWindowValues(
                        item.points.map((point) => point.total_tokens),
                      )}
                    />
                  </TableCell>
                ) : null}
                {metric === "cost" ? (
                  <TableCell className="text-right font-mono tabular-nums">
                    <MoneyCell
                      micros={sumWindowValues(
                        item.points.map((point) =>
                          point.known_cost_micros === null
                            ? null
                            : Number(point.known_cost_micros),
                        ),
                      )}
                    />
                  </TableCell>
                ) : null}
                {metric === "requests" ? (
                  <>
                    <TableCell className="text-right font-mono tabular-nums">
                      <Cell value={lastPoint?.http_success_count} />
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      <Cell value={lastPoint?.http_failed_count} />
                    </TableCell>
                  </>
                ) : null}
                {isLatencyMetric(metric) ? (
                  <>
                    <TableCell className="text-right font-mono tabular-nums">
                      <Cell value={lastPoint?.p50_ttft_ms} />
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      <Cell value={lastPoint?.p95_ttft_ms} />
                    </TableCell>
                  </>
                ) : null}
                {metric === "output_rate" ? (
                  <>
                    <TableCell className="text-right font-mono tabular-nums">
                      <OutputRateCell point={lastPoint} />
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      <SampleBasisCell
                        measured={lastPoint?.output_rate_sample_count}
                        requests={lastPoint?.request_count}
                      />
                    </TableCell>
                  </>
                ) : null}
                {metric === "cache_read_share" ? (
                  <>
                    <TableCell className="text-right font-mono tabular-nums">
                      <CacheShareCell point={lastPoint} />
                    </TableCell>
                    <TableCell className="text-right font-mono tabular-nums">
                      <SampleBasisCell
                        measured={lastPoint?.cache_basis_request_count}
                        requests={lastPoint?.request_count}
                      />
                    </TableCell>
                  </>
                ) : null}
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}

function OutputRateCell({ point }: { point: SeriesPoint | undefined }) {
  const { messages } = useLocale();
  const copy = messages.observe;
  if (!point) return <OperatorMissingValue reason={messages.honesty.noValue} />;
  const rate = bucketOutputRate(point);
  if (rate.kind === "no_sample") {
    return <OperatorMissingValue reason={copy.noSampleReason} />;
  }
  return (
    <span className="inline-flex items-center justify-end gap-1">
      {`${rate.tps.toFixed(1)} ${copy.metricUnit("output_rate")}`}
      {bucketOutputRatePartialCoverage(point) ? (
        <OperatorClippedBadge
          label={copy.partialCoverage}
          reason={copy.outputRatePartialReason}
        />
      ) : null}
    </span>
  );
}

function CacheShareCell({ point }: { point: SeriesPoint | undefined }) {
  const { messages } = useLocale();
  const copy = messages.observe;
  if (!point) return <OperatorMissingValue reason={messages.honesty.noValue} />;
  const share = bucketCacheReadShare(point);
  if (share.kind === "no_comparable_rows") {
    return <OperatorMissingValue reason={copy.bucketNoComparableReason} />;
  }
  if (share.kind === "no_denominator") {
    return <OperatorMissingValue reason={copy.bucketNoDenominatorReason} />;
  }
  return (
    <span className="inline-flex items-center justify-end gap-1">
      {`${(share.share * 100).toFixed(1)}%`}
      {bucketCacheBasisPartialCoverage(point) ? (
        <OperatorClippedBadge
          label={copy.cacheReadSharePartial}
          reason={copy.cacheReadSharePartialReason}
        />
      ) : null}
    </span>
  );
}

function SampleBasisCell({
  measured,
  requests,
}: {
  measured: number | null | undefined;
  requests: number | null | undefined;
}) {
  const { formatNumber, messages } = useLocale();
  if (
    measured === null ||
    measured === undefined ||
    requests === null ||
    requests === undefined
  ) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  return <>{`${formatNumber(measured)} / ${formatNumber(requests)}`}</>;
}

function Cell({ value }: { value: number | null | undefined }) {
  const { formatNumber, messages } = useLocale();
  if (value === null || value === undefined || Number.isNaN(value)) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  return <>{formatNumber(value)}</>;
}

function sumWindowValues(
  values: readonly (number | null | undefined)[],
): number | null {
  let total = 0;
  let sawValue = false;
  for (const value of values) {
    if (value === null || value === undefined || Number.isNaN(value)) continue;
    total += value;
    sawValue = true;
  }
  return sawValue ? total : null;
}

function MoneyCell({ micros }: { micros: number | null }) {
  const { messages } = useLocale();
  if (micros === null) {
    return <OperatorMissingValue reason={messages.honesty.noValue} />;
  }
  return <>{formatMoneyMicros(micros, getActiveReportingCurrency().symbol)}</>;
}
