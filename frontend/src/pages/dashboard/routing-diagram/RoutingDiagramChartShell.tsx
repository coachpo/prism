import type { ReactNode } from "react";
import { Network } from "lucide-react";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
} from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";

interface RoutingDiagramChartShellProps {
  children?: ReactNode;
  visualization: ReactNode;
}

export function RoutingDiagramChartShell({
  children,
  visualization,
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
        {visualization}
      </CardContent>
    </Card>
  );
}
