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
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
} from "@/components/ui/card";
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
    <Card className="overflow-hidden border-border/70 bg-card/95 shadow-none">
      <CardHeader className="gap-3 border-b">
        <div className="grid min-w-0 flex-1 gap-1">
          <CardDescription className="flex items-start gap-2 text-xs leading-relaxed">
            <Network className="mt-0.5 size-3.5 shrink-0" />
            <span>{messages.dashboard.routingChartHint}</span>
          </CardDescription>
        </div>

        <CardAction className="flex items-center">
          <span className="inline-flex items-center rounded-lg border border-border/60 bg-muted/40 px-3 py-1 text-xs font-medium text-foreground">
            {messages.dashboard.routingChartActionHint}
          </span>
        </CardAction>
      </CardHeader>

      <CardContent className="flex flex-col gap-4 pt-4 sm:pt-5">
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
      </CardContent>
    </Card>
  );
}
