import { Network } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { EmptyState } from "@/components/EmptyState";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

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
      <Card>
        <CardHeader>
          <CardTitle>{messages.dashboard.routingTitle}</CardTitle>
          <CardDescription>{messages.dashboard.routingLoadingDescription}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-wrap items-center gap-3">
            <Skeleton className="h-9 w-36 rounded-lg" />
            <Skeleton className="h-6 w-40 rounded-full" />
            <Skeleton className="h-6 w-48 rounded-full" />
          </div>
          <Skeleton className="h-[320px] rounded-2xl sm:h-[420px]" />
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="gap-4">
        <div className="space-y-2">
          <CardTitle>{messages.dashboard.routingTitle}</CardTitle>
          <CardDescription>{messages.dashboard.routingDescription}</CardDescription>
        </div>

        {headerContent}
      </CardHeader>

      <CardContent className="space-y-4">
        {error ? (
          <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-700 dark:text-amber-300">
            {error}
          </div>
        ) : null}

        {chartContent ? (
          chartContent
        ) : (
          <EmptyState
            icon={<Network className="h-6 w-6" />}
            title={emptyState?.title ?? messages.dashboard.routingNoData}
            description={
              emptyState?.description ?? messages.dashboard.routingNoDataDescription
            }
          />
        )}
      </CardContent>
    </Card>
  );
}
