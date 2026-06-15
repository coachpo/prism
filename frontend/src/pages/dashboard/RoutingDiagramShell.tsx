import { Network } from "lucide-react";
import { Skeleton } from "@/components/ui/skeleton";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout, OperatorEmptyState } from "@/shared/design-system";

interface RoutingDiagramShellProps {
  chartContent: React.ReactNode | null;
  emptyState?: {
    description: string;
    title: string;
  };
  error: string | null;
  headerContent: React.ReactNode;
  loading: boolean;
}

export function RoutingDiagramShell({
  chartContent,
  emptyState,
  error,
  headerContent,
  loading,
}: RoutingDiagramShellProps) {
  const { messages } = useLocale();

  if (loading) {
    return (
      <section data-testid="routing-diagram-card" className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h2 className="text-xl font-semibold tracking-tight text-foreground">
            {messages.dashboard.routingTitle}
          </h2>
          <p className="text-sm text-muted-foreground">
            {messages.dashboard.routingLoadingDescription}
          </p>
        </div>
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-3">
            <Skeleton className="h-9 w-36 rounded-lg" />
            <Skeleton className="h-6 w-40 rounded-full" />
            <Skeleton className="h-6 w-48 rounded-full" />
          </div>
          <Skeleton className="h-[320px] rounded-xl sm:h-[420px]" />
        </div>
      </section>
    );
  }

  return (
    <section data-testid="routing-diagram-card" className="flex flex-col gap-4">
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-2">
          <h2 className="text-xl font-semibold tracking-tight text-foreground">
            {messages.dashboard.routingTitle}
          </h2>
        </div>

        {headerContent}
      </div>

      <div className="flex flex-col gap-4">
        {error ? (
          <OperatorCallout intent="warning" description={error} />
        ) : null}

        {chartContent ? (
          chartContent
        ) : (
          <OperatorEmptyState
            icon={<Network className="size-6" />}
            title={emptyState?.title ?? messages.dashboard.routingNoData}
            description={
              emptyState?.description ?? messages.dashboard.routingNoDataDescription
            }
          />
        )}
      </div>
    </section>
  );
}
