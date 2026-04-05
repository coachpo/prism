import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import type { UsageServiceHealth } from "@/lib/types";
import { UsageHealthHeatmap } from "../charts/UsageHealthHeatmap";

interface UsageServiceHealthSectionProps {
  serviceHealth: UsageServiceHealth;
}

export function UsageServiceHealthSection({ serviceHealth }: UsageServiceHealthSectionProps) {
  const { formatNumber, messages } = useLocale();
  const availabilityPercent =
    serviceHealth.availability_percentage === null || serviceHealth.availability_percentage === undefined
      ? null
      : formatNumber(serviceHealth.availability_percentage, {
          minimumFractionDigits: 1,
          maximumFractionDigits: 1,
        });
  const availabilitySummary = availabilityPercent === null ? "—" : `${availabilityPercent}%`;
  const windowDayCount = resolveWindowDayCount(serviceHealth);

  return (
    <section>
      <Card className="border-border/70 bg-card/95 shadow-none" data-testid="usage-service-health-card">
        <CardHeader className="grid-cols-[1fr_auto] grid-rows-1 items-start gap-4 pb-2">
          <div className="min-w-0">
            <h2 className="text-base font-semibold tracking-tight">{messages.statistics.serviceHealthTitle}</h2>
          </div>

          <div className="flex flex-wrap items-center justify-end gap-2" data-testid="usage-health-header-meta">
            <p
              className="text-[11px] uppercase tracking-[0.18em] text-muted-foreground"
              data-testid="usage-health-window-label"
            >
              {messages.statistics.serviceHealthWindowDays(windowDayCount)}
            </p>
            <div
              className="inline-flex items-center rounded-full border border-border/50 bg-muted/20 px-3 py-1 text-sm font-semibold tabular-nums"
              data-testid="usage-health-availability-badge"
            >
              {availabilitySummary}
            </div>
          </div>
        </CardHeader>

        <CardContent className="pt-5">
          <UsageHealthHeatmap cells={serviceHealth.cells} intervalMinutes={serviceHealth.interval_minutes} />
        </CardContent>
      </Card>
    </section>
  );
}

function resolveWindowDayCount(serviceHealth: UsageServiceHealth) {
  const totalWindowMinutes = serviceHealth.cells.length * serviceHealth.interval_minutes;
  if (!Number.isFinite(totalWindowMinutes) || totalWindowMinutes <= 0) {
    return 1;
  }

  return Math.max(1, Math.ceil(totalWindowMinutes / (24 * 60)));
}
