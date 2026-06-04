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

export function RoutingDiagramChart({
  chartData,
  chartHeight,
  isCompact,
  onActivateNode,
}: RoutingDiagramChartProps) {
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
