import * as React from "react";
import { ScrollArea, ScrollBar } from "@/components/ui/scroll-area";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { useTimezone } from "@/hooks/useTimezone";
import { useLocale } from "@/i18n/useLocale";
import type { UsageServiceHealthCell } from "@/lib/types";

interface UsageHealthHeatmapProps {
  cells: UsageServiceHealthCell[];
  intervalMinutes: number;
}

const HEATMAP_ROW_COUNT = 7;
const HEATMAP_COLUMN_COUNT = 96;
const HEATMAP_CELL_COUNT = HEATMAP_ROW_COUNT * HEATMAP_COLUMN_COUNT;
const BUCKET_LABEL_FORMAT_OPTIONS = {
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
  month: "short",
  second: undefined,
  year: undefined,
} satisfies Intl.DateTimeFormatOptions;
const AVAILABILITY_PALETTE = [
  "color-mix(in oklab, var(--muted-foreground) 16%, var(--card))",
  "color-mix(in oklab, var(--unhealthy) 78%, var(--card))",
  "color-mix(in oklab, var(--warning) 82%, var(--card))",
  "color-mix(in oklab, var(--chart-4) 84%, var(--card))",
  "color-mix(in oklab, var(--success) 84%, var(--card))",
];

type HeatmapCell = {
  bucket: UsageServiceHealthCell;
  key: string;
  label: string;
  level: number;
  rangeLabel: string;
};

export function UsageHealthHeatmap({ cells, intervalMinutes }: UsageHealthHeatmapProps) {
  const { formatNumber, messages } = useLocale();
  const { format: formatTime } = useTimezone();

  const statusLabels: Record<UsageServiceHealthCell["status"], string> = {
    degraded: messages.statistics.healthStatusDegraded,
    down: messages.statistics.healthStatusDown,
    empty: messages.statistics.healthStatusIdle,
    ok: messages.statistics.healthStatusOk,
  };

  const rows = React.useMemo(
    () => buildHeatmapRows({ cells, formatBucketTime: formatTime, intervalMinutes }),
    [cells, formatTime, intervalMinutes],
  );

  return (
    <div className="flex flex-col gap-4" data-testid="usage-health-heatmap">
      <div className="flex flex-col gap-4" data-testid="usage-health-heatmap-calendar">
        <div className="overflow-hidden rounded-xl border border-border/60 bg-muted/20">
          <ScrollArea className="w-full" data-testid="usage-health-grid-scroll">
            <div className="flex w-full justify-center px-3 py-3 sm:px-4">
              <TooltipProvider delayDuration={0} disableHoverableContent>
                <section
                  aria-label={messages.statistics.serviceHealthTitle}
                  className="flex min-w-max flex-col gap-1"
                  data-testid="usage-health-grid"
                >
                  {rows.map((row) => (
                    <div
                      className="flex flex-row gap-1"
                      data-testid="usage-health-row"
                      key={row[0]?.key ?? "usage-health-row"}
                    >
                      {row.map((cell) => (
                        <Tooltip key={cell.key}>
                          <TooltipTrigger asChild>
                            <button
                              aria-label={`${cell.label} ${statusLabels[cell.bucket.status]} ${formatAvailabilityLine(cell.bucket, formatNumber, messages.statistics.availability)}`}
                              className="size-3 rounded-sm outline-none ring-offset-background transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                              data-level={String(cell.level)}
                              data-status={cell.bucket.status}
                              data-testid="usage-health-cell"
                              style={{
                                backgroundColor: AVAILABILITY_PALETTE[clampLevel(cell.level, AVAILABILITY_PALETTE.length)],
                              }}
                              type="button"
                            />
                          </TooltipTrigger>
                          <TooltipContent
                            className="rounded-lg border border-border/70 bg-popover/95 px-2.5 py-1.5 text-xs shadow-sm backdrop-blur-sm"
                            data-testid="usage-health-tooltip"
                            side="top"
                            sideOffset={4}
                          >
                            <div className="flex flex-col gap-1">
                              <p className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                                {cell.rangeLabel}
                              </p>
                              <p className="font-medium text-foreground">{statusLabels[cell.bucket.status]}</p>
                              <p className="font-medium">
                                {formatAvailabilityLine(cell.bucket, formatNumber, messages.statistics.availability)}
                              </p>
                              <p>
                                {messages.statistics.requests} {formatNumber(cell.bucket.request_count)} · {" "}
                                {messages.statistics.successfulCount(formatNumber(cell.bucket.success_count))} · {" "}
                                {messages.statistics.failedCount(formatNumber(cell.bucket.failed_count))}
                              </p>
                            </div>
                          </TooltipContent>
                        </Tooltip>
                      ))}
                    </div>
                  ))}
                </section>
              </TooltipProvider>
            </div>
            <ScrollBar orientation="horizontal" />
          </ScrollArea>
        </div>

        <div
          className="flex flex-wrap items-center justify-center gap-3 text-center text-xs text-muted-foreground"
          data-testid="usage-health-legend"
        >
          <span className="font-medium" data-testid="usage-health-legend-oldest">
            {messages.statistics.oldest}
          </span>

          <div className="flex flex-wrap items-center justify-center gap-2 rounded-xl border border-border/60 bg-muted/20 px-3 py-2">
            <span className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              {messages.statistics.heatmapLegendLessAvailability}
            </span>
            <div className="flex items-center justify-center gap-1">
              {AVAILABILITY_PALETTE.map((color, level) => (
                <div
                  aria-hidden="true"
                  className="size-3 rounded-sm"
                  data-level={String(level)}
                  data-testid="usage-health-legend-swatch"
                  key={`usage-health-legend-${color}`}
                  style={{ backgroundColor: color }}
                />
              ))}
            </div>
            <span className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
              {messages.statistics.heatmapLegendMoreAvailability}
            </span>
          </div>

          <span className="font-medium" data-testid="usage-health-legend-latest">
            {messages.statistics.latest}
          </span>
        </div>
      </div>
    </div>
  );
}

function buildHeatmapRows({
  cells,
  formatBucketTime,
  intervalMinutes,
}: {
  cells: UsageServiceHealthCell[];
  formatBucketTime: (bucketStart: string, options?: Intl.DateTimeFormatOptions) => string;
  intervalMinutes: number;
}) {
  const resolvedIntervalMinutes = normalizeIntervalMinutes(intervalMinutes);
  const orderedCells = normalizeOrderedCells({
    cells,
    intervalMinutes: resolvedIntervalMinutes,
  });

  return Array.from({ length: HEATMAP_ROW_COUNT }, (_, rowIndex) =>
    Array.from({ length: HEATMAP_COLUMN_COUNT }, (_, columnIndex) => {
      const bucket = orderedCells[rowIndex * HEATMAP_COLUMN_COUNT + columnIndex];

      return {
        bucket,
        key: bucket.bucket_start,
        label: formatBucketLabel(bucket.bucket_start, formatBucketTime),
        level: resolveAvailabilityLevel(bucket.availability_percentage),
        rangeLabel: formatBucketRange(bucket.bucket_start, intervalMinutes, formatBucketTime),
      } satisfies HeatmapCell;
    }),
  );
}

function normalizeOrderedCells({
  cells,
  intervalMinutes,
}: {
  cells: UsageServiceHealthCell[];
  intervalMinutes: number;
}) {
  const normalizedCells = cells
    .slice(0, HEATMAP_CELL_COUNT)
    .map((cell) => ({
      ...cell,
      bucket_start: normalizeBucketStart(cell.bucket_start),
    }));

  if (normalizedCells.length === 0) {
    return buildFallbackCells({
      count: HEATMAP_CELL_COUNT,
      intervalMinutes,
    });
  }

  const orderedCells = [...normalizedCells];
  while (orderedCells.length < HEATMAP_CELL_COUNT) {
    const lastBucketStart = new Date(
      orderedCells[orderedCells.length - 1].bucket_start,
    ).getTime();
    orderedCells.push(
      buildEmptyCell(
        new Date(lastBucketStart + intervalMinutes * 60_000).toISOString(),
      ),
    );
  }

  return orderedCells;
}

function buildFallbackCells({
  count,
  intervalMinutes,
}: {
  count: number;
  intervalMinutes: number;
}) {
  const latestCompletedBucketStart = Math.floor(
    (Date.now() - 1) / (intervalMinutes * 60_000),
  ) * intervalMinutes * 60_000;
  const firstBucketStart =
    latestCompletedBucketStart - (count - 1) * intervalMinutes * 60_000;

  return Array.from({ length: count }, (_, index) =>
    buildEmptyCell(new Date(firstBucketStart + index * intervalMinutes * 60_000).toISOString()),
  );
}

function buildEmptyCell(bucketStart: string): UsageServiceHealthCell {
  return {
    availability_percentage: null,
    bucket_start: normalizeBucketStart(bucketStart),
    failed_count: 0,
    request_count: 0,
    status: "empty",
    success_count: 0,
  };
}

function clampLevel(level: number, levelCount: number) {
  return Math.max(0, Math.min(levelCount - 1, level));
}

function resolveAvailabilityLevel(availabilityPercentage: number | null) {
  if (availabilityPercentage === null || availabilityPercentage === undefined) {
    return 0;
  }

  if (availabilityPercentage <= 0) {
    return 1;
  }

  if (availabilityPercentage < 50) {
    return 2;
  }

  if (availabilityPercentage < 100) {
    return 3;
  }

  return 4;
}

function formatAvailabilityLine(
  cell: UsageServiceHealthCell,
  formatNumber: ReturnType<typeof useLocale>["formatNumber"],
  availabilityLabel: string,
) {
  if (cell.availability_percentage === null || cell.availability_percentage === undefined) {
    return `${availabilityLabel} —`;
  }

  return `${availabilityLabel} ${formatNumber(cell.availability_percentage, {
    maximumFractionDigits: 1,
    minimumFractionDigits: 1,
  })}%`;
}

function formatBucketLabel(
  bucketStart: string,
  formatBucketTime: (bucketStart: string, options?: Intl.DateTimeFormatOptions) => string,
) {
  return formatBucketTime(bucketStart, BUCKET_LABEL_FORMAT_OPTIONS);
}

function formatBucketRange(
  bucketStart: string,
  intervalMinutes: number,
  formatBucketTime: (bucketStart: string, options?: Intl.DateTimeFormatOptions) => string,
) {
  const bucketStartMs = Date.parse(bucketStart);
  const scheduledBucketEndMs = bucketStartMs + intervalMinutes * 60_000;
  const now = Date.now();
  const bucketEndMs =
    now > bucketStartMs && now < scheduledBucketEndMs ? now : scheduledBucketEndMs;

  return `${formatBucketLabel(bucketStart, formatBucketTime)} - ${formatBucketLabel(new Date(bucketEndMs).toISOString(), formatBucketTime)}`;
}

function normalizeBucketStart(bucketStart: string) {
  return new Date(bucketStart).toISOString().replace(".000Z", "Z");
}

function normalizeIntervalMinutes(value?: number) {
  if (!value || !Number.isFinite(value) || value <= 0) {
    return 24 * 60;
  }

  return Math.max(1, Math.floor(value));
}
