import { useCallback, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  getRoutingDiagramEmptyState,
  getRoutingDiagramGraph,
  getRoutingDiagramMobileData,
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
  const { messages } = useLocale();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [containerWidth, setContainerWidth] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(() =>
    typeof window === "undefined" ? 0 : window.innerHeight,
  );

  useLayoutEffect(() => {
    const element = containerRef.current;
    if (!element) {
      return;
    }

    const updateMeasurements = () => {
      setContainerWidth(element.getBoundingClientRect().width);
      setViewportHeight(window.innerHeight);
    };

    updateMeasurements();
    window.addEventListener("resize", updateMeasurements);

    if (typeof ResizeObserver === "undefined") {
      return () => window.removeEventListener("resize", updateMeasurements);
    }

    const observer = new ResizeObserver(() => {
      updateMeasurements();
    });

    observer.observe(element);
    return () => {
      window.removeEventListener("resize", updateMeasurements);
      observer.disconnect();
    };
  }, []);

  const hasMeasuredContainer = containerWidth > 0;
  const isCompact = hasMeasuredContainer && containerWidth < 640;
  const chartHeight = isCompact ? 320 : Math.max(760, viewportHeight - 120);

  const graphData = useMemo(() => {
    return data ? getRoutingDiagramGraph(data) : { nodes: [], edges: [] };
  }, [data]);

  const mobileData = useMemo(() => {
    return getRoutingDiagramMobileData(graphData);
  }, [graphData]);

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
        headerContent={null}
        loading={loading}
      />
    </div>
  );
}
