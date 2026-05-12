import { Loader2, Radar, RefreshCw, XCircle } from "lucide-react";
import { StatusBadge, TypeBadge } from "@/components/StatusBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useLocale } from "@/i18n/useLocale";
import type { SidecarQuotaScanRun } from "@/lib/types";

interface QuotaInventoryPanelProps {
  loading: boolean;
  mutating: "start" | "cancel" | null;
  onCancelScan: (scan: SidecarQuotaScanRun) => Promise<void>;
  onStartScan: () => Promise<void>;
  scans: SidecarQuotaScanRun[];
}

function isActiveScan(scan: SidecarQuotaScanRun) {
  return scan.status === "queued" || scan.status === "running";
}

function scanProgress(scan: SidecarQuotaScanRun | null) {
  if (!scan || scan.planned_count <= 0) {
    return scan?.status === "completed" ? 100 : 0;
  }
  return Math.min(100, Math.round((scan.attempted_count / scan.planned_count) * 100));
}

function labelFor(labels: Record<string, string>, value: string) {
  return labels[value] ?? value;
}

export function QuotaInventoryPanel({ loading, mutating, onCancelScan, onStartScan, scans }: QuotaInventoryPanelProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.sidecarsPage;
  const activeScan = scans.find(isActiveScan) ?? null;
  const displayedScan = activeScan ?? scans[0] ?? null;
  const progress = scanProgress(displayedScan);
  const canCancelManualScan = activeScan?.scan_type === "manual";

  return (
    <Card data-testid="quota-scan-progress">
      <CardHeader className="pb-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-sm">
              <Radar className="h-4 w-4" />
              {copy.quotaScanTitle}
            </CardTitle>
            <CardDescription className="text-xs">{copy.quotaScanDescription}</CardDescription>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              size="sm"
              onClick={() => void onStartScan()}
              disabled={loading || mutating !== null || Boolean(activeScan)}
              data-testid="scan-quota-now"
            >
              {mutating === "start" ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
              {copy.quotaScanStart}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => activeScan ? void onCancelScan(activeScan) : undefined}
              disabled={loading || mutating !== null || !canCancelManualScan}
              data-testid="cancel-quota-scan"
            >
              {mutating === "cancel" ? <Loader2 className="h-4 w-4 animate-spin" /> : <XCircle className="h-4 w-4" />}
              {copy.quotaScanCancel}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        {loading ? (
          <div className="space-y-2">
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
            <div className="h-14 animate-pulse rounded-md bg-muted/50" />
          </div>
        ) : displayedScan ? (
          <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge label={labelFor(copy.quotaScanStatusLabels, displayedScan.status)} intent={displayedScan.status === "failed" ? "danger" : displayedScan.status === "completed" ? "success" : "info"} />
              <TypeBadge label={labelFor(copy.quotaScanTypeLabels, displayedScan.scan_type)} intent="muted" preserveLabel />
              {activeScan && !canCancelManualScan ? <span className="text-xs text-muted-foreground">{copy.quotaScanAutomaticActive}</span> : null}
            </div>
            <div className="space-y-2">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span>{copy.quotaScanProgressLabel}</span>
                <span>{copy.quotaScanProgressValue(formatNumber(displayedScan.attempted_count), formatNumber(displayedScan.planned_count))}</span>
              </div>
              <Progress value={progress} />
            </div>
            <div className="grid gap-3 text-xs text-muted-foreground sm:grid-cols-3">
              <span>{copy.quotaScanSucceeded(formatNumber(displayedScan.succeeded_count))}</span>
              <span>{copy.quotaScanQuotaExceeded(formatNumber(displayedScan.quota_exceeded_count))}</span>
              <span>{copy.quotaScanFailed(formatNumber(displayedScan.failed_count + displayedScan.unsupported_count + displayedScan.missing_index_count))}</span>
            </div>
            {displayedScan.last_error_code ? <p className="text-xs text-destructive">{copy.quotaScanLastError(displayedScan.last_error_code)}</p> : null}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{copy.quotaScanEmpty}</p>
        )}
      </CardContent>
    </Card>
  );
}
