import {
  formatSuccessRate,
  getRouteHealthState,
  isRoutingDiagramLink,
  isRoutingDiagramNode,
} from "./routingDiagramChartUtils";
import type { RoutingDiagramTooltipProps } from "./routingDiagramChartTypes";
import { useLocale } from "@/i18n/useLocale";

export function RoutingDiagramTooltip({ active, payload }: RoutingDiagramTooltipProps) {
  const { formatNumber, messages } = useLocale();

  if (!active || !payload?.length) {
    return null;
  }

  const chartPayload = payload[0]?.payload?.payload;

  if (isRoutingDiagramNode(chartPayload)) {
    return (
      <div className="min-w-[14rem] rounded-xl border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-xl">
        <p className="text-sm font-semibold">{chartPayload.label}</p>
        {chartPayload.sublabel ? <p className="mt-1 text-muted-foreground">{chartPayload.sublabel}</p> : null}
        <div className="mt-3 space-y-1.5">
          <TooltipRow label={messages.dashboard.routingNodeType} value={getNodeTypeLabel(chartPayload.kind, messages)} />
          <TooltipRow label={messages.dashboard.routingActiveConnections} value={formatNumber(chartPayload.activeTerminalTargetCount)} />
          <TooltipRow
            label={messages.dashboard.routing24hSuccessRate}
            value={
              formatSuccessRate(chartPayload.successRate24h, chartPayload.requestCount24h) ??
              messages.dashboard.routingLegendNoData
            }
          />
          <TooltipRow label={messages.dashboard.routing24hTotalRequests} value={formatNumber(chartPayload.requestCount24h)} />
          {chartPayload.kind === "terminal_target" ? (
            <TooltipRow
              label={messages.dashboard.routing24hHealth}
              value={getHealthLabel(chartPayload.healthStatus, chartPayload.requestCount24h, messages)}
            />
          ) : null}
          {chartPayload.kind === "model" ? (
            <TooltipRow label={messages.requestLogs.view} value={messages.dashboard.routingActionOpenModelDetail} />
          ) : null}
          {chartPayload.kind === "endpoint" ? (
            <TooltipRow label={messages.requestLogs.view} value={messages.dashboard.reviewRequests} />
          ) : null}
        </div>
      </div>
    );
  }

  if (isRoutingDiagramLink(chartPayload)) {
    const routeHealthState = getRouteHealthState(
      chartPayload.successRate24h,
      chartPayload.requestCount24h,
    );
    const routeHealthLabel =
      routeHealthState === "healthy"
        ? messages.dashboard.routingLegendHealthy
        : routeHealthState === "degraded"
          ? messages.dashboard.routingLegendDegraded
          : routeHealthState === "failing"
            ? messages.dashboard.routingLegendFailing
            : messages.dashboard.routingLegendNoData;
    const formattedSuccessRate =
      formatSuccessRate(chartPayload.successRate24h, chartPayload.requestCount24h) ??
      messages.dashboard.routingLegendNoData;

    return (
      <div className="min-w-[16rem] rounded-xl border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-xl">
        <p className="text-sm font-semibold">{chartPayload.sourceLabel} → {chartPayload.targetLabel}</p>
        <div className="mt-3 space-y-1.5">
          <TooltipRow label={messages.dashboard.routing24hHealth} value={routeHealthLabel} />
          <TooltipRow label={messages.dashboard.routing24hSuccessRate} value={formattedSuccessRate} />
          <TooltipRow label={messages.dashboard.routing24hTotalRequests} value={formatNumber(chartPayload.requestCount24h)} />
          <TooltipRow label={messages.dashboard.routingActiveConnections} value={formatNumber(chartPayload.activeTerminalTargetCount)} />
          <TooltipRow label={messages.dashboard.routing24hSuccessfulRequests} value={formatNumber(chartPayload.successCount24h)} />
          <TooltipRow label={messages.dashboard.routing24hErrors} value={formatNumber(chartPayload.errorCount24h)} />
        </div>
      </div>
    );
  }

  return null;
}

function getNodeTypeLabel(
  kind: "endpoint" | "model" | "terminal_target",
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (kind === "endpoint") {
    return messages.dashboard.routingEndpointNodeType;
  }

  if (kind === "terminal_target") {
    return messages.modelDetail.connections;
  }

  return messages.dashboard.routingModelNodeType;
}

function getHealthLabel(
  healthStatus: string | null,
  requestCount: number,
  messages: ReturnType<typeof useLocale>["messages"],
): string {
  if (!healthStatus || requestCount <= 0) {
    return messages.dashboard.routingLegendNoData;
  }

  if (healthStatus === "healthy") {
    return messages.modelDetail.healthHealthy;
  }

  if (healthStatus === "unknown") {
    return messages.modelDetail.healthUnknown;
  }

  return messages.modelDetail.healthUnhealthy;
}

function TooltipRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-4">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right font-medium">{value}</span>
    </div>
  );
}
