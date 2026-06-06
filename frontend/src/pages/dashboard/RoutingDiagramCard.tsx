import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  getRoutingDiagramEmptyState,
  getRoutingDiagramGraph,
  getRoutingDiagramMobileData,
  getRoutingDiagramSummary,
  RoutingDiagramFlow,
  RoutingDiagramMobileList,
  type RoutingDiagramData,
  type RoutingDiagramNode,
} from "./routingDiagram";
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

  useLayoutEffect(() => {
    const element = containerRef.current;
    if (!element) {
      return;
    }

    const updateWidth = () => {
      setContainerWidth(element.getBoundingClientRect().width);
    };

    updateWidth();

    if (typeof ResizeObserver === "undefined") {
      return;
    }

    const observer = new ResizeObserver(() => {
      updateWidth();
    });

    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const hasMeasuredContainer = containerWidth > 0;
  const isCompact = hasMeasuredContainer && containerWidth < 640;
  const chartHeight = isCompact ? 320 : 560;

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

  const activateNode = useCallback(
    (node: RoutingDiagramNode) => {
      if (node.kind === "model" && node.modelConfigId !== null) {
        onSelectModel(node.modelConfigId);
      }
      if (node.kind === "endpoint" && node.endpointId !== null && onDrillDownRequests) {
        onDrillDownRequests({ endpoint_id: node.endpointId });
      }
    },
    [onDrillDownRequests, onSelectModel],
  );

  return (
    <div ref={containerRef}>
      <RoutingDiagramShell
        chartContent={
          data && hasChartContent ? (
            isCompact ? (
              <RoutingDiagramMobileList mobileData={mobileData} onActivateNode={activateNode} />
            ) : hasMeasuredContainer ? (
              <RoutingDiagramFlow
                graphData={graphData}
                chartHeight={chartHeight}
                onActivateNode={activateNode}
              />
            ) : (
              <div
                data-testid="routing-diagram-desktop-pending"
                className="w-full rounded-xl border border-border/70 bg-background/60"
                style={{ height: chartHeight }}
                aria-hidden="true"
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
