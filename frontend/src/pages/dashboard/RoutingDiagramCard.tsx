import { useEffect, useMemo, useRef, useState } from "react";
import {
  getRoutingDiagramEmptyState,
  getRoutingDiagramGraph,
  getRoutingDiagramMobileData,
  getRoutingDiagramSummary,
  type RoutingDiagramData,
  type RoutingDiagramNode,
} from "./routingDiagram";
import { RoutingDiagramChart } from "./routing-diagram/RoutingDiagramChart";
import { RoutingDiagramMobileList } from "./routing-diagram/RoutingDiagramMobileList";
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

  const graphData = useMemo(() => {
    return data ? getRoutingDiagramGraph(data) : { nodes: [], edges: [] };
  }, [data]);

  const mobileData = useMemo(() => {
    return getRoutingDiagramMobileData(graphData);
  }, [graphData]);

  const summary = useMemo(() => {
    return data ? getRoutingDiagramSummary(data) : null;
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
  }, [
    data,
    messages.dashboard.routingNoActiveRoutes,
    messages.dashboard.routingNoActiveRoutesDescription,
    messages.dashboard.routingNoRecentTraffic,
    messages.dashboard.routingNoRecentTrafficDescription,
  ]);

  const hasChartContent = graphData.nodes.length > 0 && graphData.edges.length > 0;

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
          data && hasChartContent ? (
            isCompact ? (
              <RoutingDiagramMobileList mobileData={mobileData} onActivateNode={activateNode} />
            ) : (
              <RoutingDiagramChart
                graphData={graphData}
                chartHeight={chartHeight}
                isCompact={isCompact}
                onActivateNode={activateNode}
              />
            )
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
          summary ? (
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground" aria-live="polite">
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.endpointCount(formatNumber(summary.endpointCount))}
              </span>
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.modelCount(formatNumber(summary.modelCount))}
              </span>
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.activeTargets(formatNumber(summary.activeTargetCount))}
              </span>
              <span className="rounded-full border bg-muted/40 px-2.5 py-1">
                {messages.dashboard.successfulRequests24h(
                  formatNumber(summary.recentRequestTotal24h),
                )}
              </span>
            </div>
          ) : null
        }
        loading={loading}
      />
    </div>
  );
}
