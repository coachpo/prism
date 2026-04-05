import { useEffect, useMemo, useRef, useState } from "react";
import {
  getRoutingDiagramChartData,
  getRoutingDiagramEmptyState,
  type RoutingDiagramData,
  type RoutingDiagramNode,
} from "./routingDiagram";
import { RoutingDiagramChart } from "./routing-diagram/RoutingDiagramChart";
import { useLocale } from "@/i18n/useLocale";
import { RoutingDiagramShell } from "./RoutingDiagramShell";

interface RoutingDiagramCardProps {
  data: RoutingDiagramData | null;
  loading: boolean;
  error: string | null;
  onSelectModel: (modelConfigId: number) => void;
  onDrillDownRequests?: (params: { endpoint_id?: number; model_id?: string }) => void;
}

export function RoutingDiagramCard({
  data,
  loading,
  error,
  onSelectModel,
  onDrillDownRequests,
}: RoutingDiagramCardProps) {
  const { formatNumber, messages } = useLocale();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [containerWidth, setContainerWidth] = useState(0);

  useEffect(() => {
    const element = containerRef.current;
    if (!element || typeof ResizeObserver === "undefined") {
      return;
    }

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) {
        return;
      }

      setContainerWidth(entry.contentRect.width);
    });

    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const isCompact = containerWidth > 0 && containerWidth < 640;
  const chartHeight = isCompact ? 320 : 420;

  const chartData = useMemo(() => {
    return data ? getRoutingDiagramChartData(data) : { nodes: [], links: [] };
  }, [data]);

  const emptyState = useMemo(() => {
    if (!data) {
      return null;
    }

    const baseEmptyState = getRoutingDiagramEmptyState(data);
    if (baseEmptyState.kind === "no_active_routes") {
      return {
        title: messages.dashboard.routingNoActiveRoutes,
        description: messages.dashboard.routingNoActiveRoutesDescription,
      };
    }

    return {
      title: messages.dashboard.routingNoRecentTraffic,
      description: messages.dashboard.routingNoRecentTrafficDescription,
    };
  }, [data, messages.dashboard.routingNoActiveRoutes, messages.dashboard.routingNoActiveRoutesDescription, messages.dashboard.routingNoRecentTraffic, messages.dashboard.routingNoRecentTrafficDescription]);

  const activateNode = (node: RoutingDiagramNode) => {
    if (node.kind === "model" && node.modelConfigId !== null) {
      onSelectModel(node.modelConfigId);
    }
    if (node.kind === "endpoint" && node.endpointId !== null && onDrillDownRequests) {
      onDrillDownRequests({ endpoint_id: node.endpointId });
    }
  };

  return (
    <div ref={containerRef}>
      <RoutingDiagramShell
        chartContent={
          data && chartData.links.length > 0 ? (
            <RoutingDiagramChart
              chartData={chartData}
              chartHeight={chartHeight}
              isCompact={isCompact}
              onActivateNode={activateNode}
            />
          ) : null
        }
        emptyState={
          emptyState
            ? {
                description: emptyState.description,
                title: emptyState.title,
              }
            : undefined
        }
        error={error}
        headerContent={
          data ? (
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground" aria-live="polite">
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.endpointCount(formatNumber(data.endpointCount))}
              </span>
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.modelCount(formatNumber(data.modelCount))}
              </span>
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.activeRoutes(formatNumber(data.activeConnectionTotal))}
              </span>
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.successfulRequests24h(formatNumber(data.trafficRequestTotal24h))}
              </span>
            </div>
          ) : null
        }
        loading={loading}
      />
    </div>
  );
}
