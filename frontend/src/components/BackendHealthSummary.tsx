import { StatusDot } from "@/components/ui/status-dot";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import type { BackendHealthResponse } from "@/lib/types";

interface BackendHealthSummaryProps {
  error?: boolean;
  health: BackendHealthResponse | null;
  loading: boolean;
}

type HealthStatusField = "liveness" | "readiness" | "startup" | "status";

const STATUS_FIELD_ORDER: HealthStatusField[] = ["status", "liveness", "readiness", "startup"];

export function BackendHealthSummary({ error = false, health, loading }: BackendHealthSummaryProps) {
  const { messages } = useLocale();
  const copy = messages.healthSummary;
  const common = messages.common;

  return (
    <Card className="border-border/70 bg-card/95 shadow-none" data-testid="backend-health-summary">
      <CardContent className="flex flex-col gap-4 py-[var(--density-card-pad-y)]">
        <div className="flex flex-col gap-1">
          <h2 className="text-sm font-semibold">{copy.title}</h2>
          <p className="text-xs text-muted-foreground">{copy.description}</p>
        </div>

        <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-5">
          {loading
            ? STATUS_FIELD_ORDER.map((field) => <HealthSummarySkeleton key={field} dataTestId={`backend-health-${field}`} />)
            : STATUS_FIELD_ORDER.map((field) => (
                <HealthStatusItem
                  key={field}
                  label={copy[field]}
                  value={health?.[field] ?? common.unavailable}
                  dataTestId={`backend-health-${field}`}
                  intent={resolveHealthIntent(field, health?.[field], error)}
                />
              ))}
          {loading ? (
            <HealthSummarySkeleton dataTestId="backend-health-version" />
          ) : (
            <HealthVersionItem
              label={copy.version}
              value={health?.version ?? common.unavailable}
              dataTestId="backend-health-version"
            />
          )}
        </div>
      </CardContent>
    </Card>
  );
}

function HealthStatusItem({
  dataTestId,
  intent,
  label,
  value,
}: {
  dataTestId: string;
  intent: "healthy" | "muted" | "unhealthy" | "warning";
  label: string;
  value: string;
}) {
  return (
    <div
      className="flex min-w-0 flex-col gap-2 rounded-lg border border-border/60 bg-muted/30 px-3 py-2"
      data-testid={dataTestId}
    >
      <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">{label}</p>
      <div className="flex min-w-0 items-center gap-2">
        <StatusDot intent={intent} />
        <span className="truncate font-mono text-sm font-medium">{value}</span>
      </div>
    </div>
  );
}

function HealthVersionItem({ dataTestId, label, value }: { dataTestId: string; label: string; value: string }) {
  return (
    <div
      className="flex min-w-0 flex-col gap-2 rounded-lg border border-border/60 bg-muted/30 px-3 py-2"
      data-testid={dataTestId}
    >
      <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">{label}</p>
      <span className="truncate font-mono text-sm font-medium text-foreground">{value}</span>
    </div>
  );
}

function HealthSummarySkeleton({ dataTestId }: { dataTestId: string }) {
  return (
    <div
      className="flex min-w-0 flex-col gap-2 rounded-lg border border-border/60 bg-muted/30 px-3 py-2"
      data-testid={dataTestId}
    >
      <Skeleton className="h-3 w-20" />
      <Skeleton className="h-5 w-24" />
    </div>
  );
}

function resolveHealthIntent(
  field: HealthStatusField,
  value: string | null | undefined,
  hasError: boolean,
): "healthy" | "muted" | "unhealthy" | "warning" {
  if (hasError || !value) {
    return "muted";
  }

  const normalizedValue = value.trim().toLowerCase();
  if ((field === "status" || field === "liveness") && normalizedValue === "ok") {
    return "healthy";
  }
  if (field === "readiness" && normalizedValue === "ready") {
    return "healthy";
  }
  if (field === "startup" && normalizedValue === "complete") {
    return "healthy";
  }

  return field === "status" || field === "liveness" ? "unhealthy" : "warning";
}
