import type { ReactNode } from "react";
import { Network } from "lucide-react";
import { ResponsiveContainer, Sankey, Tooltip as RechartsTooltip } from "recharts";
import { RoutingDiagramLinkShape } from "./RoutingDiagramLinkShape";
import { RoutingDiagramNodeShape } from "./RoutingDiagramNodeShape";
import { RoutingDiagramTooltip } from "./RoutingDiagramTooltip";
import type {
  RoutingDiagramChartProps,
  RoutingLinkShapeProps,
  RoutingNodeShapeProps,
} from "./routingDiagramChartTypes";
import { getChartPayload, isRoutingDiagramNode } from "./routingDiagramChartUtils";
import { useLocale } from "@/i18n/useLocale";

interface RoutingDiagramChartShellProps extends RoutingDiagramChartProps {
  children?: ReactNode;
}

export function RoutingDiagramChartShell({
  chartData,
  chartHeight,
  isCompact,
  onActivateNode,
  children,
}: RoutingDiagramChartShellProps) {
  const { messages } = useLocale();

  return (
    <div className="rounded-2xl border bg-gradient-to-br from-background via-background to-muted/35 p-3 sm:p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
        <div className="flex items-center gap-2">
          <Network className="h-3.5 w-3.5" />
          <span>{messages.dashboard.routingChartHint}</span>
        </div>
        <span className="rounded-full border bg-background/80 px-2.5 py-1 font-medium text-foreground">
          {messages.dashboard.routingChartActionHint}
        </span>
      </div>

      {children}

      <div style={{ height: chartHeight }}>
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
            onClick={(item: unknown, elementType: unknown) => {
              if (elementType === "node") {
                const node = getChartPayload(item);
                if (isRoutingDiagramNode(node) && node.kind === "model") {
                  onActivateNode(node);
                }
              }
            }}
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
    </div>
  );
}
