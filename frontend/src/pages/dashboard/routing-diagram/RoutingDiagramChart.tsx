import { RoutingDiagramChartShell } from "./RoutingDiagramChartShell";
import { RoutingDiagramLegend } from "./RoutingDiagramLegend";
import type { RoutingDiagramChartProps } from "./routingDiagramChartTypes";

export function RoutingDiagramChart({
  chartData,
  chartHeight,
  isCompact,
  onActivateNode,
}: RoutingDiagramChartProps) {
  return (
    <RoutingDiagramChartShell
      chartData={chartData}
      chartHeight={chartHeight}
      isCompact={isCompact}
      onActivateNode={onActivateNode}
    >
      <RoutingDiagramLegend />
    </RoutingDiagramChartShell>
  );
}
