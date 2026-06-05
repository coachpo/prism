import { useMemo } from "react";
import { ResponsiveContainer, Sankey, Tooltip as RechartsTooltip } from "recharts";
import { RoutingDiagramChartShell } from "./RoutingDiagramChartShell";
import { RoutingDiagramLegend } from "./RoutingDiagramLegend";
import { RoutingDiagramLinkShape } from "./RoutingDiagramLinkShape";
import { RoutingDiagramNodeShape } from "./RoutingDiagramNodeShape";
import { RoutingDiagramTooltip } from "./RoutingDiagramTooltip";
import type {
  RoutingDiagramChartProps,
  RoutingLinkShapeProps,
  RoutingNodeShapeProps,
} from "./routingDiagramChartTypes";
import type {
  RoutingDiagramGraph,
  RoutingDiagramGraphEdge,
  RoutingDiagramGraphNode,
} from "../routingDiagram";

type RoutingDiagramSankeyNode = RoutingDiagramGraphNode & { value: number };
type RoutingDiagramSankeyLink = RoutingDiagramGraphEdge & {
  source: number;
  target: number;
  value: number;
};

type RoutingDiagramSankeyData = {
  nodes: RoutingDiagramSankeyNode[];
  links: RoutingDiagramSankeyLink[];
};

export function RoutingDiagramChart({
  graphData,
  chartHeight,
  isCompact,
  onActivateNode,
}: RoutingDiagramChartProps) {
  const chartData = useMemo(() => getRoutingDiagramSankeyData(graphData), [graphData]);

  return (
    <RoutingDiagramChartShell
      visualization={
        <div data-testid="routing-diagram-sankey" style={{ height: chartHeight }}>
          <ResponsiveContainer width="100%" height="100%">
            <Sankey
              data={chartData}
              nodePadding={isCompact ? 18 : 24}
              nodeWidth={isCompact ? 14 : 18}
              margin={{
                top: 12,
                right: isCompact ? 84 : 148,
                bottom: isCompact ? 28 : 36,
                left: isCompact ? 84 : 148,
              }}
              sort={false}
              node={(props: RoutingNodeShapeProps) => (
                <RoutingDiagramNodeShape compact={isCompact} onActivate={onActivateNode} props={props} />
              )}
              link={(props: RoutingLinkShapeProps) => <RoutingDiagramLinkShape props={props} />}
            >
              <RechartsTooltip
                cursor={false}
                wrapperStyle={{ outline: "none" }}
                content={<RoutingDiagramTooltip />}
              />
            </Sankey>
          </ResponsiveContainer>
        </div>
      }
    >
      <RoutingDiagramLegend />
    </RoutingDiagramChartShell>
  );
}

function getRoutingDiagramSankeyData(graphData: RoutingDiagramGraph): RoutingDiagramSankeyData {
  const nodes = graphData.nodes.map<RoutingDiagramSankeyNode>((node) => ({
    ...node,
    value: Math.max(node.activeTerminalTargetCount, 1),
  }));
  const nodeIndex = new Map(nodes.map((node, index) => [node.id, index]));

  return {
    nodes,
    links: graphData.edges
      .filter((edge) => nodeIndex.has(edge.sourceNodeId) && nodeIndex.has(edge.targetNodeId))
      .map<RoutingDiagramSankeyLink>((edge) => ({
        ...edge,
        source: nodeIndex.get(edge.sourceNodeId) ?? 0,
        target: nodeIndex.get(edge.targetNodeId) ?? 0,
        value: Math.max(edge.activeTerminalTargetCount, 1),
      })),
  };
}
