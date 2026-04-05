import { Activity, CheckCircle2, XCircle } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { AnimatedListItem } from "@/components/AnimatedListItem";
import { EmptyState } from "@/components/EmptyState";
import { ValueBadge } from "@/components/StatusBadge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { formatMoneyMicros } from "@/lib/costing";
import { cn } from "@/lib/utils";
import type { RequestLogListItem } from "@/lib/types";

interface RecentActivityCardProps {
  clearRecentRequestHighlight: (requestId: number) => void;
  formatTime: (isoString: string, options?: Intl.DateTimeFormatOptions) => string;
  modelDisplayNames: Map<string, string>;
  recentNewIds: Set<number>;
  recentRequests: RequestLogListItem[];
}

export function RecentActivityCard({
  clearRecentRequestHighlight,
  formatTime,
  modelDisplayNames,
  recentNewIds,
  recentRequests,
}: RecentActivityCardProps) {
  const { formatNumber, locale, messages } = useLocale();

  return (
    <Card className="col-span-4">
      <CardHeader>
        <CardTitle>{messages.dashboard.recentActivity}</CardTitle>
        <CardDescription>{messages.dashboard.recentActivityDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        {recentRequests.length === 0 ? (
          <EmptyState
            icon={<ActivityEmptyIcon />}
            title={messages.dashboard.noRecentActivity}
            description={messages.dashboard.noRecentActivityDescription}
          />
        ) : (
          <div className="space-y-4">
            {recentRequests.map((request) => (
              <AnimatedListItem
                key={request.id}
                isNew={recentNewIds.has(request.id)}
                animation="left"
                onAnimationEnd={() => clearRecentRequestHighlight(request.id)}
                className="flex items-center justify-between border-b pb-4 last:border-0 last:pb-0"
              >
                <div className="flex items-center gap-4">
                  <div
                    className={cn(
                      "flex h-9 w-9 items-center justify-center rounded-full border",
                      request.status_code >= 200 && request.status_code < 300
                        ? "border-emerald-500/20 bg-emerald-500/10 text-emerald-500"
                        : "border-red-500/20 bg-red-500/10 text-red-500"
                    )}
                  >
                    {request.status_code >= 200 && request.status_code < 300 ? (
                      <CheckCircle2 className="h-5 w-5" />
                    ) : (
                      <XCircle className="h-5 w-5" />
                    )}
                  </div>
                  <div className="space-y-1">
                    <div className="space-y-1">
                      <p className="text-sm font-medium leading-none">
                        {modelDisplayNames.get(request.model_id) || request.model_id}
                      </p>
                      <p className="text-xs text-muted-foreground">{request.model_id}</p>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      {formatTime(request.created_at, {
                        hour: "numeric",
                        minute: "numeric",
                        second: "numeric",
                      })}{" "}
                      - {formatNumber(request.response_time_ms)}ms
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-2 text-right">
                  <div className="hidden sm:block">
                    <p className="text-sm font-medium">
                      {messages.requestLogs.totalTokens}: {formatNumber(request.total_tokens || 0)}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {formatMoneyMicros(
                        request.total_cost_user_currency_micros,
                        request.report_currency_symbol ?? "$",
                        undefined,
                        2,
                        6,
                        locale,
                      )}
                    </p>
                  </div>
                  <ValueBadge
                    label={String(request.status_code)}
                    intent={request.status_code >= 200 && request.status_code < 300 ? "success" : "danger"}
                  />
                </div>
              </AnimatedListItem>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function ActivityEmptyIcon() {
  return <Activity className="h-6 w-6" />;
}
