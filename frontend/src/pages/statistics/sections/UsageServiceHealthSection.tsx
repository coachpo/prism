import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
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
      <Card
        className="@container/card border-border/70 bg-card/95 shadow-none"
        data-testid="usage-service-health-card"
      >
        <CardHeader className="gap-3 border-b">
          <div className="grid min-w-0 flex-1 gap-1">
            <CardTitle className="text-base">
              <h2>{messages.statistics.serviceHealthTitle}</h2>
            </CardTitle>
          </div>

          <CardAction className="flex items-center">
            <div
              className="flex flex-wrap items-center justify-end gap-2"
              data-testid="usage-health-header-meta"
            >
              <p
                className="text-[11px] font-medium uppercase tracking-[0.18em] text-muted-foreground"
                data-testid="usage-health-window-label"
              >
                {messages.statistics.serviceHealthWindowDays(windowDayCount)}
              </p>
              <div
                className="inline-flex items-center rounded-lg border border-border/60 bg-muted/40 px-3 py-1 text-sm font-semibold tabular-nums"
                data-testid="usage-health-availability-badge"
              >
                {availabilitySummary}
              </div>
            </div>
          </CardAction>
        </CardHeader>

        <CardContent className="pt-4 sm:pt-5">
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
