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

interface RoutingDiagramVisualizationShellProps {
  children?: ReactNode;
  visualization: ReactNode;
}

export function RoutingDiagramVisualizationShell({
  children,
  visualization,
}: RoutingDiagramVisualizationShellProps) {
  const { messages } = useLocale();

  return (
    <Card className="overflow-hidden border-border/70 bg-card/95 shadow-none">
      <CardHeader className="gap-3 border-b has-data-[slot=card-action]:grid-cols-1 sm:has-data-[slot=card-action]:grid-cols-[1fr_auto]">
        <div className="grid min-w-0 flex-1 gap-1">
          <CardDescription className="flex min-w-0 items-start gap-2 text-xs leading-relaxed">
            <Network className="mt-0.5 size-3.5 shrink-0" />
            <span className="min-w-0">{messages.dashboard.routingChartHint}</span>
          </CardDescription>
        </div>

        <CardAction className="col-start-1 row-start-2 justify-self-start sm:col-start-2 sm:row-span-2 sm:row-start-1 sm:justify-self-end">
          <span className="inline-flex max-w-full items-center rounded-lg border border-border/60 bg-muted/40 px-3 py-1 text-left text-xs font-medium text-foreground whitespace-normal">
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
