import { Download, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import type { Messages } from "@/i18n/messages/en";
import { useLocale } from "@/i18n/useLocale";
import type { UsageSnapshotPreset } from "@/lib/types";

type StatisticsMessages = Messages["statistics"];

const TIME_RANGE_LABELS: Record<
  UsageSnapshotPreset,
  (messages: { statistics: StatisticsMessages }) => string
> = {
  "1h": (messages) => messages.statistics.lastHour,
  "6h": (messages) => messages.statistics.last6Hours,
  "24h": (messages) => messages.statistics.last24Hours,
  "7d": (messages) => messages.statistics.last7Days,
  "30d": (messages) => messages.statistics.last30Days,
  all: (messages) => messages.statistics.allTime,
};

interface UsageControlsBarProps {
  generatedAt: string | null;
  loading: boolean;
  onExportSnapshot: () => void;
  onRefresh: () => void;
  onSelectTimeRange: (preset: UsageSnapshotPreset) => void;
  selectedTimeRange: UsageSnapshotPreset;
}

export function UsageControlsBar({
  generatedAt,
  loading,
  onExportSnapshot,
  onRefresh,
  onSelectTimeRange,
  selectedTimeRange,
}: UsageControlsBarProps) {
  const { formatRelativeTimeFromNow, messages } = useLocale();

  return (
    <Card className="overflow-hidden border-border/70 bg-card/95 shadow-none">
      <CardContent
        className="flex flex-col gap-2.5 p-2.5 lg:flex-row lg:items-center lg:justify-between"
        data-testid="usage-controls-toolbar"
      >
        <div className="flex flex-col gap-2 xl:flex-row xl:items-center">
          <span className="text-xs font-medium uppercase tracking-[0.18em] text-muted-foreground">
            {messages.statistics.timeWindow}
          </span>

          <div className="flex flex-wrap items-center gap-1 rounded-xl border border-border/70 bg-muted/35 p-1">
            {(["1h", "6h", "24h", "7d", "30d", "all"] as const).map((preset) => (
              <Button
                key={preset}
                onClick={() => onSelectTimeRange(preset)}
                size="xs"
                variant={selectedTimeRange === preset ? "default" : "ghost"}
              >
                {TIME_RANGE_LABELS[preset](messages)}
              </Button>
            ))}
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 lg:justify-end">
          <Button onClick={onExportSnapshot} size="sm" variant="outline">
            <Download className="mr-2 h-4 w-4" />
            {messages.statistics.exportSnapshotJson}
          </Button>
          <Button
            aria-label={messages.statistics.refreshUsageStatistics}
            disabled={loading}
            onClick={onRefresh}
            size="sm"
            title={messages.statistics.refreshUsageStatistics}
            variant="outline"
          >
            <RefreshCw className={`mr-2 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            {messages.statistics.refreshUsageStatistics}
          </Button>
          <div
            className="rounded-xl border border-border/70 bg-background/80 px-3 py-1.5 text-sm text-muted-foreground"
            data-testid="usage-controls-updated"
          >
            <span className="mr-1 text-[11px] uppercase tracking-[0.18em]">{messages.statistics.updated}</span>
            <span>{generatedAt ? formatRelativeTimeFromNow(generatedAt) : "—"}</span>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
